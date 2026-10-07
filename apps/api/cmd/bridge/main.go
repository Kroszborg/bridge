// Command bridge runs the Bridge API server, background worker and migrations.
//
//	bridge serve [--worker]   HTTP API (optionally with the worker in-process)
//	bridge worker             background job processor
//	bridge migrate            apply database migrations
//	bridge openapi            print the OpenAPI document
//	bridge healthcheck        probe a running server (for container health checks)
//	bridge version
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // usage reports use IANA time zones; distroless images have no zoneinfo

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"

	"bridge/internal/config"
	"bridge/internal/db"
	"bridge/internal/db/dbq"
	"bridge/internal/events"
	"bridge/internal/gateway"
	"bridge/internal/httpapi"
	"bridge/internal/messaging"
	"bridge/internal/provider"
	"bridge/internal/push"
	"bridge/internal/reqlog"
	"bridge/internal/secretbox"
	"bridge/internal/status"
	"bridge/internal/tools"
	"bridge/internal/webhook"
	"bridge/internal/worker"
)

// version is set at build time with -ldflags "-X main.version=…".
var version = "dev"

const usage = `Bridge — open-source SMS and phone verification infrastructure.

Usage:
  bridge serve [--worker]   Run the HTTP API. --worker also runs background jobs in-process.
  bridge worker             Run background jobs.
  bridge migrate            Apply database migrations, then exit.
  bridge openapi [--out f]  Print the OpenAPI 3.1 document.
  bridge healthcheck        Exit 0 if the local server is healthy.
  bridge version            Print the version.

Configuration is read from BRIDGE_* environment variables; see .env.example.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch cmd, args := os.Args[1], os.Args[2:]; cmd {
	case "serve":
		err = serve(ctx, args)
	case "worker":
		err = runWorker(ctx)
	case "migrate":
		err = migrate(ctx)
	case "openapi":
		err = openapi(args)
	case "healthcheck":
		err = healthcheck(args)
	case "version", "--version", "-v":
		fmt.Println(version)
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		slog.Error("bridge exited with an error", "error", err)
		os.Exit(1)
	}
}

func setup() (*config.Config, *slog.Logger, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, fmt.Errorf("invalid configuration:\n%w", err)
	}
	logger := newLogger(cfg, os.Stdout)
	slog.SetDefault(logger)
	return cfg, logger, nil
}

func newLogger(cfg *config.Config, w io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}
	var h slog.Handler = slog.NewTextHandler(w, opts)
	if cfg.LogFormat == "json" {
		h = slog.NewJSONHandler(w, opts)
	}
	return slog.New(h).With("service", "bridge")
}

func serve(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	withWorker := fs.Bool("worker", false, "also run background jobs in this process")
	_ = fs.Parse(args)

	cfg, logger, err := setup()
	if err != nil {
		return err
	}
	pool, err := db.Connect(ctx, cfg.DatabaseURL, cfg.DBMaxConns)
	if err != nil {
		return err
	}
	defer pool.Close()

	pushService, err := push.New(ctx, dbq.New(pool), cfg)
	if err != nil {
		return err
	}
	g, gctx := errgroup.WithContext(ctx)
	hub := gateway.NewHub(gctx, pool, logger)
	jobs, err := worker.NewInsertOnlyClient(pool, logger)
	if err != nil {
		return err
	}
	hooks := webhook.New(webhook.Options{Pool: pool, Logger: logger, AllowPrivate: cfg.WebhookAllowPrivate})
	hooks.SetJobInserter(jobs)
	providers, err := providerRouter(cfg, pool)
	if err != nil {
		return err
	}
	msgs := messaging.New(messaging.Options{
		Pool: pool, Logger: logger, Publisher: hub.Send, Waker: pushService, Emitter: hooks,
		Providers: providers, Config: messaging.Config{Retention: cfg.MessageRetention},
	})
	msgs.SetJobInserter(jobs)
	hub.SetHandler(msgs)
	kit, err := tools.New(tools.Options{Config: cfg, Pool: pool, Logger: logger, Messaging: msgs})
	if err != nil {
		return err
	}
	kit.SetJobInserter(jobs)

	requests := reqlog.New(pool, logger, time.Second)
	broker := events.NewBroker(pool, logger)
	health := status.New(pool, logger, version)
	srv := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: httpapi.New(httpapi.Options{
			Config: cfg, Pool: pool, Logger: logger, Version: version, Hub: hub, Push: pushService, Messaging: msgs, Webhooks: hooks, Providers: providers,
			RequestLog: requests, Events: broker, Status: health, Tools: kit,
		}).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}

	g.Go(func() error { return hub.Run(gctx) })
	g.Go(func() error { return broker.Run(gctx) })
	g.Go(func() error { health.Heartbeat(gctx, "api"); return nil })
	// The request log flushes after the HTTP server stops so late requests are kept.
	logCtx, stopLogs := context.WithCancel(context.Background())
	logsDone := make(chan struct{})
	go func() { _ = requests.Run(logCtx); close(logsDone) }()
	defer func() { stopLogs(); <-logsDone }()
	g.Go(func() error {
		logger.Info("API listening", "version", version, "addr", cfg.HTTPAddr, "public_url", cfg.PublicURL.String(), "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	})
	g.Go(func() error {
		<-gctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		logger.Info("shutting down API")
		return srv.Shutdown(shutdownCtx)
	})
	if *withWorker {
		g.Go(func() error { return startWorker(gctx, pool, logger, msgs, hooks, health, kit, cfg) })
	}
	return g.Wait()
}

func runWorker(ctx context.Context) error {
	cfg, logger, err := setup()
	if err != nil {
		return err
	}
	pool, err := db.Connect(ctx, cfg.DatabaseURL, cfg.DBMaxConns)
	if err != nil {
		return err
	}
	defer pool.Close()
	pushService, err := push.New(ctx, dbq.New(pool), cfg)
	if err != nil {
		return err
	}
	hooks := webhook.New(webhook.Options{Pool: pool, Logger: logger, AllowPrivate: cfg.WebhookAllowPrivate})
	providers, err := providerRouter(cfg, pool)
	if err != nil {
		return err
	}
	msgs := messaging.New(messaging.Options{
		Pool: pool, Logger: logger, Waker: pushService, Emitter: hooks, Providers: providers,
		// The worker holds no device connections; frames go through NOTIFY.
		Publisher: func(ctx context.Context, deviceID string, f gateway.Outbound) error {
			return gateway.Publish(ctx, pool, deviceID, f)
		},
		Config: messaging.Config{Retention: cfg.MessageRetention},
	})
	kit, err := tools.New(tools.Options{Config: cfg, Pool: pool, Logger: logger, Messaging: msgs})
	if err != nil {
		return err
	}
	return startWorker(ctx, pool, logger, msgs, hooks, status.New(pool, logger, version), kit, cfg)
}

func startWorker(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger, msgs *messaging.Service, hooks *webhook.Service,
	health *status.Service, kit *tools.Services, cfg *config.Config,
) error {
	client, err := worker.NewClient(pool, logger, msgs, hooks, health, worker.Options{
		Retention: worker.Retention{RequestLogs: cfg.RequestLogRetention}, Tools: kit,
	})
	if err != nil {
		return err
	}
	if err := client.Start(ctx); err != nil {
		return err
	}
	logger.Info("worker started")
	go health.Heartbeat(ctx, "worker")
	<-ctx.Done()
	stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	logger.Info("stopping worker; waiting for running jobs")
	return client.Stop(stopCtx)
}

func migrate(ctx context.Context) error {
	cfg, logger, err := setup()
	if err != nil {
		return err
	}
	pool, err := db.Connect(ctx, cfg.DatabaseURL, 2)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool, logger); err != nil {
		return err
	}
	logger.Info("database is up to date")
	return nil
}

func openapi(args []string) error {
	fs := flag.NewFlagSet("openapi", flag.ExitOnError)
	out := fs.String("out", "", "write to this file instead of stdout")
	_ = fs.Parse(args)
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	doc, err := httpapi.OpenAPI(version)
	if err != nil {
		return err
	}
	doc = append(doc, '\n')
	if *out == "" {
		_, err = os.Stdout.Write(doc)
		return err
	}
	return os.WriteFile(*out, doc, 0o644)
}

func healthcheck(args []string) error {
	fs := flag.NewFlagSet("healthcheck", flag.ExitOnError)
	url := fs.String("url", "http://127.0.0.1:8080/healthz", "health endpoint to probe")
	_ = fs.Parse(args)
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(*url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check returned %s", resp.Status)
	}
	return nil
}

// providerRouter manages SMS provider accounts. Without BRIDGE_SECRET_KEY it
// works, but refuses to store or read credentials.
func providerRouter(cfg *config.Config, pool *pgxpool.Pool) (*provider.Router, error) {
	box, err := secretbox.New(cfg.SecretKey)
	if err != nil {
		return nil, err
	}
	return provider.NewRouter(dbq.New(pool), box, provider.RouterOptions{PublicURL: cfg.PublicURL.String()}), nil
}
