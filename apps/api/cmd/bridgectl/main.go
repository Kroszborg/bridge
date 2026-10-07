// Command bridgectl is the Bridge command-line tool: send SMS, follow
// messages, watch events, and forward webhooks to a local server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

// version is set at build time with -ldflags "-X main.version=…".
var version = "dev"

const usage = `bridgectl — the Bridge command-line tool.

Usage:
  bridgectl login [--url URL] [--api-key KEY]      Save your server and API key
  bridgectl logout                                 Forget the saved API key
  bridgectl whoami                                 Show the key's project and environment

  bridgectl send TO MESSAGE [--device ID] [--sim N] [--idempotency-key K] [--wait]
  bridgectl messages [--status S] [--direction inbound|outbound] [--to N] [--from S] [--limit N]
  bridgectl messages get ID                        One message and its timeline
  bridgectl messages tail [--types T,…]            Print events as they happen

  bridgectl devices                                Paired phones and their health
  bridgectl usage [--days N] [--tz Area/City]      Daily volume and delivery rate

  bridgectl listen --forward-to URL [--types T,…] [--secret whsec_…]
                                                   Forward live events to a local webhook endpoint

  bridgectl version

Global flags (any command):
  --url URL        Bridge API URL (default: saved, then BRIDGE_URL, then http://localhost:8080)
  --api-key KEY    API key (default: saved, then BRIDGE_API_KEY)
  --json           Print raw JSON

Test keys (bk_test_…) simulate everything; nothing is sent.
`

type globals struct {
	url, apiKey string
	json        bool
}

func (g *globals) register(fs *flag.FlagSet) {
	fs.StringVar(&g.url, "url", "", "Bridge API URL")
	fs.StringVar(&g.apiKey, "api-key", "", "API key")
	fs.BoolVar(&g.json, "json", false, "print raw JSON")
}

// parse parses flags anywhere among the arguments and returns the positional ones.
func parse(fs *flag.FlagSet, args []string) []string {
	fs.SetOutput(io.Discard)
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			fail(fmt.Errorf("%s: %w\nRun `bridgectl help` for usage.", fs.Name(), err))
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return pos
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "login":
		err = cmdLogin(ctx, args)
	case "logout":
		err = cmdLogout()
	case "whoami":
		err = cmdWhoami(ctx, args)
	case "send":
		err = cmdSend(ctx, args)
	case "messages", "message", "msg":
		err = cmdMessages(ctx, args)
	case "devices", "device":
		err = cmdDevices(ctx, args)
	case "usage":
		err = cmdUsage(ctx, args)
	case "listen":
		err = cmdListen(ctx, args)
	case "version", "--version", "-v":
		fmt.Println("bridgectl", version)
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, paint(red, "error: ")+err.Error())
	os.Exit(1)
}

// setup parses a command's flags and resolves the configuration.
func setup(name string, args []string, extra func(*flag.FlagSet)) (*client, globals, []string, error) {
	var g globals
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	g.register(fs)
	if extra != nil {
		extra(fs)
	}
	pos := parse(fs, args)
	cfg, err := resolve(g.url, g.apiKey)
	if err != nil {
		return nil, g, nil, err
	}
	if err := cfg.requireKey(); err != nil {
		return nil, g, nil, err
	}
	return newClient(cfg), g, pos, nil
}

// ---- terminal output ----------------------------------------------------

const (
	reset  = "\033[0m"
	bold   = "\033[1m"
	dim    = "\033[2m"
	red    = "\033[31m"
	green  = "\033[32m"
	yellow = "\033[33m"
	cyan   = "\033[36m"
)

var colorful = func() bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}()

func paint(color, s string) string {
	if !colorful {
		return s
	}
	return color + s + reset
}

func statusColor(status string) string {
	switch status {
	case "delivered", "received":
		return green
	case "failed":
		return red
	case "sent":
		return cyan
	default:
		return yellow
	}
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
