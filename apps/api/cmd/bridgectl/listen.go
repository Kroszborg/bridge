package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"bridge/internal/webhook"
)

// cmdListen forwards live events to a local endpoint, signed like real
// webhooks, so a webhook handler can be developed without a public URL.
func cmdListen(ctx context.Context, args []string) error {
	var forward, types, secret string
	c, g, _, err := setup("listen", args, func(fs *flag.FlagSet) {
		fs.StringVar(&forward, "forward-to", "", "local URL to POST events to, e.g. http://localhost:3000/webhooks/bridge")
		fs.StringVar(&types, "types", "", "comma-separated event types (default: all)")
		fs.StringVar(&secret, "secret", "", "signing secret to use (default: a new one, printed below)")
	})
	if err != nil {
		return err
	}
	for _, t := range splitList(types) {
		if !webhook.ValidEventType(t) {
			return fmt.Errorf("unknown event type %q; use %s", t, strings.Join(webhook.EventTypes, ", "))
		}
	}
	if forward != "" {
		u, err := url.Parse(forward)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("--forward-to must be an http(s) URL, e.g. http://localhost:3000/webhooks/bridge")
		}
	}
	if secret == "" {
		secret = webhook.NewSecret()
	} else if !strings.HasPrefix(secret, webhook.SecretPrefix) {
		return errors.New("--secret must start with whsec_")
	}

	if forward != "" {
		fmt.Fprintf(os.Stderr, "Forwarding events to %s\n", paint(bold, forward))
		fmt.Fprintf(os.Stderr, "Signing secret for this session: %s\n", paint(cyan, secret))
		fmt.Fprintln(os.Stderr, paint(dim, "Set it as your app's BRIDGE_WEBHOOK_SECRET while testing. It differs from your endpoints' real secrets."))
	}
	httpc := &http.Client{
		Timeout:       15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return followEvents(ctx, c, splitList(types), func(ev sseEvent) {
		if g.json {
			fmt.Println(string(ev.Data))
		} else {
			fmt.Println(describeEvent(ev))
		}
		if forward == "" {
			return
		}
		status, dur, err := deliver(ctx, httpc, forward, secret, ev)
		switch {
		case err != nil:
			fmt.Fprintf(os.Stderr, "  %s %v\n", paint(red, "✗"), err)
		case status >= 200 && status < 300:
			fmt.Fprintf(os.Stderr, "  %s %d %s\n", paint(green, "→"), status, paint(dim, dur.Round(time.Millisecond).String()))
		default:
			fmt.Fprintf(os.Stderr, "  %s %d %s\n", paint(red, "→"), status, paint(dim, dur.Round(time.Millisecond).String()))
		}
	})
}

// deliver POSTs one event with Standard Webhooks headers, exactly like Bridge does.
func deliver(ctx context.Context, httpc *http.Client, target, secret string, ev sseEvent) (int, time.Duration, error) {
	now := time.Now()
	sig, err := webhook.Sign(secret, ev.ID, now, ev.Data)
	if err != nil {
		return 0, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(ev.Data))
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "bridgectl-listen/"+version)
	req.Header.Set(webhook.HeaderID, ev.ID)
	req.Header.Set(webhook.HeaderTimestamp, strconv.FormatInt(now.Unix(), 10))
	req.Header.Set(webhook.HeaderSignature, sig)
	start := time.Now()
	resp, err := httpc.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, time.Since(start), nil
}
