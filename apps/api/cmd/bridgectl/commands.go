package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"golang.org/x/term"
)

type whoAmI struct {
	ProjectID   string `json:"project_id"`
	ProjectName string `json:"project_name"`
	Environment string `json:"environment"`
	APIKeyName  string `json:"api_key_name"`
}

type messageEvent struct {
	Type      string         `json:"type"`
	Detail    map[string]any `json:"detail"`
	CreatedAt time.Time      `json:"created_at"`
}

type message struct {
	ID           string         `json:"id"`
	Status       string         `json:"status"`
	Direction    string         `json:"direction"`
	Environment  string         `json:"environment"`
	To           string         `json:"to"`
	From         *string        `json:"from"`
	Body         *string        `json:"body"`
	Segments     *int           `json:"segments"`
	DeviceID     *string        `json:"device_id"`
	SimSlot      *int           `json:"sim_slot"`
	ErrorCode    *string        `json:"error_code"`
	ErrorMessage *string        `json:"error_message"`
	CreatedAt    time.Time      `json:"created_at"`
	Events       []messageEvent `json:"events"`
}

func (m message) party() string {
	if m.Direction == "inbound" && m.From != nil {
		return "← " + *m.From
	}
	return "→ " + m.To
}

func deref[T any](p *T, fallback T) T {
	if p == nil {
		return fallback
	}
	return *p
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// ---- login / logout / whoami ------------------------------------------

func cmdLogin(ctx context.Context, args []string) error {
	var g globals
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	g.register(fs)
	parse(fs, args)
	file, err := loadFile()
	if err != nil {
		return err
	}
	in := bufio.NewReader(os.Stdin)
	interactive := term.IsTerminal(int(os.Stdin.Fd()))

	apiURL := g.url
	if apiURL == "" && interactive {
		def := file.URL
		if def == "" {
			def = "http://localhost:8080"
		}
		fmt.Printf("Bridge API URL [%s]: ", def)
		line, _ := in.ReadString('\n')
		apiURL = strings.TrimSpace(line)
		if apiURL == "" {
			apiURL = def
		}
	}
	if apiURL == "" {
		apiURL = file.URL
	}
	if apiURL == "" {
		apiURL = "http://localhost:8080"
	}
	if u, err := url.Parse(apiURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%q is not an http(s) URL", apiURL)
	}

	key := g.apiKey
	if key == "" {
		if interactive {
			fmt.Print("API key (bk_live_… or bk_test_…, input hidden): ")
			raw, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Println()
			if err != nil {
				return err
			}
			key = string(raw)
		} else {
			line, _ := in.ReadString('\n') // e.g. echo "$KEY" | bridgectl login --url …
			key = line
		}
	}
	key = strings.TrimSpace(key)
	if !strings.HasPrefix(key, "bk_live_") && !strings.HasPrefix(key, "bk_test_") {
		return errors.New("API keys start with bk_live_ or bk_test_. Create one in the dashboard under API keys")
	}

	c := &client{baseURL: strings.TrimRight(apiURL, "/"), apiKey: key, http: newClient(resolved{}).http}
	var me whoAmI
	if err := c.get(ctx, "/v1/whoami", nil, &me); err != nil {
		return err
	}
	path, err := saveFile(fileConfig{URL: c.baseURL, APIKey: key})
	if err != nil {
		return err
	}
	fmt.Printf("Logged in to %s (%s) at %s.\nSaved to %s.\n", paint(bold, me.ProjectName), envLabel(me.Environment), c.baseURL, path)
	return nil
}

func cmdLogout() error {
	path, err := removeFile()
	if err != nil {
		return err
	}
	fmt.Printf("Removed %s.\n", path)
	return nil
}

func envLabel(env string) string {
	if env == "test" {
		return paint(yellow, "test mode: nothing is sent")
	}
	return paint(green, "live")
}

func cmdWhoami(ctx context.Context, args []string) error {
	c, g, _, err := setup("whoami", args, nil)
	if err != nil {
		return err
	}
	var me whoAmI
	if err := c.get(ctx, "/v1/whoami", nil, &me); err != nil {
		return err
	}
	if g.json {
		return printJSON(me)
	}
	fmt.Printf("%s (%s)\nproject %s · key %q · %s\n", paint(bold, me.ProjectName), envLabel(me.Environment), me.ProjectID, me.APIKeyName, c.baseURL)
	return nil
}

// ---- send / messages --------------------------------------------------

func cmdSend(ctx context.Context, args []string) error {
	var device, idem string
	var sim int
	var wait bool
	c, g, pos, err := setup("send", args, func(fs *flag.FlagSet) {
		fs.StringVar(&device, "device", "", "send through this device only")
		fs.IntVar(&sim, "sim", 0, "SIM slot (1 or 2)")
		fs.StringVar(&idem, "idempotency-key", "", "retrying with the same key never sends twice")
		fs.BoolVar(&wait, "wait", false, "wait until delivered or failed and print the timeline")
	})
	if err != nil {
		return err
	}
	if len(pos) < 2 {
		return errors.New(`usage: bridgectl send TO MESSAGE, e.g. bridgectl send +919876543210 "Your order has shipped."`)
	}
	body := map[string]any{"to": pos[0], "message": strings.Join(pos[1:], " ")}
	if device != "" {
		body["device_id"] = device
	}
	if sim != 0 {
		body["sim_slot"] = sim
	}
	headers := map[string]string{}
	if idem != "" {
		headers["Idempotency-Key"] = idem
	}
	var m message
	h, err := c.request(ctx, "POST", "/v1/messages", nil, body, headers, &m)
	if err != nil {
		return err
	}
	if !wait {
		if g.json {
			return printJSON(m)
		}
		note := ""
		if h.Get("Idempotent-Replayed") == "true" {
			note = paint(dim, " (already sent with this idempotency key)")
		}
		fmt.Printf("%s %s %s · %d segment(s)%s\n", m.ID, paint(statusColor(m.Status), m.Status), m.party(), deref(m.Segments, 0), note)
		return nil
	}
	final, err := waitFinal(ctx, c, m.ID)
	if err != nil {
		return err
	}
	if g.json {
		return printJSON(final)
	}
	printMessage(final)
	if final.Status == "failed" {
		os.Exit(1)
	}
	return nil
}

func waitFinal(ctx context.Context, c *client, id string) (message, error) {
	deadline := time.Now().Add(5 * time.Minute)
	for {
		var m message
		if err := c.get(ctx, "/v1/messages/"+url.PathEscape(id), nil, &m); err != nil {
			return m, err
		}
		switch m.Status {
		case "delivered", "failed", "received":
			return m, nil
		}
		if time.Now().After(deadline) {
			fmt.Fprintf(os.Stderr, "still %s after 5 minutes; stopped waiting\n", m.Status)
			return m, nil
		}
		select {
		case <-ctx.Done():
			return m, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func printMessage(m message) {
	fmt.Printf("%s  %s  %s\n", paint(bold, m.ID), paint(statusColor(m.Status), m.Status), m.party())
	if m.Body != nil {
		fmt.Printf("  %q\n", truncate(*m.Body, 200))
	}
	if m.ErrorCode != nil {
		fmt.Printf("  %s %s\n", paint(red, *m.ErrorCode), deref(m.ErrorMessage, ""))
	}
	for _, e := range m.Events {
		note := ""
		if n, ok := e.Detail["device_name"].(string); ok {
			note = n
		}
		if s, ok := e.Detail["segments"].(float64); ok {
			note = strconv.Itoa(int(s)) + " segment(s)"
		}
		fmt.Printf("  %s  %-22s %s\n", paint(dim, e.CreatedAt.Local().Format("15:04:05")), e.Type, paint(dim, note))
	}
}

func cmdMessages(ctx context.Context, args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "get":
			return cmdMessageGet(ctx, args[1:])
		case "tail":
			return cmdTail(ctx, args[1:])
		case "list":
			args = args[1:]
		}
	}
	var status, direction, to, from string
	var limit int
	c, g, _, err := setup("messages", args, func(fs *flag.FlagSet) {
		fs.StringVar(&status, "status", "", "queued, sending, sent, delivered, failed or received")
		fs.StringVar(&direction, "direction", "", "inbound or outbound")
		fs.StringVar(&to, "to", "", "recipient (E.164)")
		fs.StringVar(&from, "from", "", "sender of incoming messages")
		fs.IntVar(&limit, "limit", 20, "how many (1–100)")
	})
	if err != nil {
		return err
	}
	q := url.Values{"limit": {strconv.Itoa(limit)}}
	for k, v := range map[string]string{"status": status, "direction": direction, "to": to, "from": from} {
		if v != "" {
			q.Set(k, v)
		}
	}
	var page struct {
		Data    []message `json:"data"`
		HasMore bool      `json:"has_more"`
	}
	if err := c.get(ctx, "/v1/messages", q, &page); err != nil {
		return err
	}
	if g.json {
		return printJSON(page)
	}
	if len(page.Data) == 0 {
		fmt.Println("No messages.")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, paint(dim, "ID\tSTATUS\tNUMBER\tMESSAGE\tCREATED"))
	for _, m := range page.Data {
		body := paint(dim, "(removed after retention)")
		if m.Body != nil {
			body = truncate(*m.Body, 40)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", m.ID, paint(statusColor(m.Status), m.Status), m.party(), body, m.CreatedAt.Local().Format("Jan 2 15:04"))
	}
	_ = tw.Flush()
	if page.HasMore {
		fmt.Println(paint(dim, "More messages exist; raise --limit or use the dashboard."))
	}
	return nil
}

func cmdMessageGet(ctx context.Context, args []string) error {
	c, g, pos, err := setup("messages get", args, nil)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("usage: bridgectl messages get MESSAGE_ID")
	}
	var m message
	if err := c.get(ctx, "/v1/messages/"+url.PathEscape(pos[0]), nil, &m); err != nil {
		return err
	}
	if g.json {
		return printJSON(m)
	}
	printMessage(m)
	return nil
}

// cmdTail prints events from the stream as they happen.
func cmdTail(ctx context.Context, args []string) error {
	var types string
	c, g, _, err := setup("messages tail", args, func(fs *flag.FlagSet) {
		fs.StringVar(&types, "types", "", "comma-separated event types, e.g. message.delivered,message.received")
	})
	if err != nil {
		return err
	}
	return followEvents(ctx, c, splitList(types), func(ev sseEvent) {
		if g.json {
			fmt.Println(string(ev.Data))
			return
		}
		fmt.Println(describeEvent(ev))
	})
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

type envelope struct {
	Type      string          `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

func describeEvent(ev sseEvent) string {
	var env envelope
	_ = json.Unmarshal(ev.Data, &env)
	at := paint(dim, env.Timestamp.Local().Format("15:04:05"))
	color := cyan
	switch {
	case strings.HasSuffix(env.Type, "failed") || env.Type == "device.offline":
		color = red
	case env.Type == "otp.expired":
		color = yellow
	case strings.HasSuffix(env.Type, "delivered") || env.Type == "device.online" || env.Type == "otp.verified":
		color = green
	}
	label := paint(color, fmt.Sprintf("%-18s", env.Type))
	if strings.HasPrefix(env.Type, "message.") {
		var m message
		_ = json.Unmarshal(env.Data, &m)
		extra := ""
		if m.ErrorCode != nil {
			extra = " " + paint(red, *m.ErrorCode)
		}
		if env.Type == "message.received" && m.Body != nil {
			extra = " " + strconv.Quote(truncate(*m.Body, 60))
		}
		return fmt.Sprintf("%s %s %s %s%s", at, label, m.ID, m.party(), extra)
	}
	if strings.HasPrefix(env.Type, "otp.") {
		var v verification
		_ = json.Unmarshal(env.Data, &v)
		return fmt.Sprintf("%s %s %s %s", at, label, v.ID, v.To)
	}
	var d struct {
		ID, Name string
	}
	_ = json.Unmarshal(env.Data, &d)
	return fmt.Sprintf("%s %s %s (%s)", at, label, d.Name, d.ID)
}

// followEvents keeps the stream open, reconnecting after drops.
func followEvents(ctx context.Context, c *client, types []string, handle func(sseEvent)) error {
	backoff := time.Second
	first := true
	for {
		err := c.stream(ctx, types, func() {
			if first {
				fmt.Fprintln(os.Stderr, paint(dim, "Listening for events. Ctrl-C to stop."))
				first = false
			} else {
				fmt.Fprintln(os.Stderr, paint(dim, "Reconnected."))
			}
			backoff = time.Second
		}, handle)
		if ctx.Err() != nil {
			return nil
		}
		var perm *permanentError
		if errors.As(err, &perm) {
			return perm.err
		}
		fmt.Fprintf(os.Stderr, "%s %v; reconnecting in %s\n", paint(yellow, "!"), err, backoff)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

// ---- devices / usage ----------------------------------------------------

type device struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Status       string     `json:"status"`
	BatteryLevel *int       `json:"battery_level"`
	IsCharging   *bool      `json:"is_charging"`
	NetworkType  *string    `json:"network_type"`
	LastSeenAt   *time.Time `json:"last_seen_at"`
	RecentSends  int        `json:"recent_sends"`
	SendLimit    int        `json:"send_limit_count"`
	Forward      bool       `json:"forward_inbound"`
}

func cmdDevices(ctx context.Context, args []string) error {
	c, g, _, err := setup("devices", args, nil)
	if err != nil {
		return err
	}
	var list struct {
		Data []device `json:"data"`
	}
	if err := c.get(ctx, "/v1/devices", nil, &list); err != nil {
		return err
	}
	active := list.Data[:0]
	for _, d := range list.Data {
		if d.Status != "disabled" { // removed from the project
			active = append(active, d)
		}
	}
	list.Data = active
	if g.json {
		return printJSON(list.Data)
	}
	if len(list.Data) == 0 {
		fmt.Println("No paired phones. Pair one in the dashboard under Devices.")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, paint(dim, "ID\tNAME\tSTATUS\tBATTERY\tNETWORK\tWINDOW\tINCOMING\tLAST SEEN"))
	for _, d := range list.Data {
		color := red
		if d.Status == "online" {
			color = green
		}
		battery := "—"
		if d.BatteryLevel != nil {
			battery = strconv.Itoa(*d.BatteryLevel) + "%"
			if deref(d.IsCharging, false) {
				battery += " ⚡"
			}
		}
		seen := "never"
		if d.LastSeenAt != nil {
			seen = since(*d.LastSeenAt)
		}
		incoming := "off"
		if d.Forward {
			incoming = "forwarded"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%d/%d\t%s\t%s\n", d.ID, d.Name, paint(color, d.Status), battery,
			deref(d.NetworkType, "—"), d.RecentSends, d.SendLimit, incoming, seen)
	}
	return tw.Flush()
}

func since(t time.Time) string {
	d := time.Since(t).Round(time.Second)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return t.Local().Format("Jan 2")
}

func cmdUsage(ctx context.Context, args []string) error {
	var days int
	var tz string
	c, g, _, err := setup("usage", args, func(fs *flag.FlagSet) {
		fs.IntVar(&days, "days", 7, "number of days (1–90)")
		fs.StringVar(&tz, "tz", localZone(), "IANA time zone for day boundaries")
	})
	if err != nil {
		return err
	}
	var h struct {
		Environment string `json:"environment"`
		TimeZone    string `json:"time_zone"`
		Days        []struct {
			Date      string `json:"date"`
			Outbound  int    `json:"outbound"`
			Delivered int    `json:"delivered"`
			Sent      int    `json:"sent"`
			Failed    int    `json:"failed"`
			Inbound   int    `json:"inbound"`
			Segments  int    `json:"segments"`
			Requests  int    `json:"requests"`
		} `json:"days"`
	}
	if err := c.get(ctx, "/v1/usage/history", url.Values{"days": {strconv.Itoa(days)}, "tz": {tz}}, &h); err != nil {
		return err
	}
	if g.json {
		return printJSON(h)
	}
	peak := 1
	for _, d := range h.Days {
		peak = max(peak, d.Outbound)
	}
	envName := "Live"
	if h.Environment == "test" {
		envName = "Test"
	}
	fmt.Printf("%s usage, last %d days (%s)\n", paint(bold, envName), len(h.Days), h.TimeZone)
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, paint(dim, "DATE\tSENT\t\tDELIVERED\tFAILED\tINCOMING\tSEGMENTS\tREQUESTS"))
	var tot struct{ out, del, sent, fail, in, seg, req int }
	for _, d := range h.Days {
		bar := strings.Repeat("█", d.Outbound*20/peak)
		if d.Outbound > 0 && bar == "" {
			bar = "▏"
		}
		fmt.Fprintf(tw, "%s\t%d\t%s\t%d\t%s\t%d\t%d\t%d\n", d.Date, d.Outbound, paint(cyan, bar), d.Delivered,
			failText(d.Failed), d.Inbound, d.Segments, d.Requests)
		tot.out += d.Outbound
		tot.del += d.Delivered
		tot.sent += d.Sent
		tot.fail += d.Failed
		tot.in += d.Inbound
		tot.seg += d.Segments
		tot.req += d.Requests
	}
	_ = tw.Flush()
	if finished := tot.del + tot.sent + tot.fail; finished > 0 {
		fmt.Printf("%d sent · %.1f%% delivered or accepted by the carrier · %d failed · %d incoming\n",
			tot.out, float64(tot.del+tot.sent)*100/float64(finished), tot.fail, tot.in)
	}
	return nil
}

func failText(n int) string {
	if n == 0 {
		return "0"
	}
	return paint(red, strconv.Itoa(n))
}

// localZone returns the IANA name of the local time zone when known.
func localZone() string {
	if tz := os.Getenv("TZ"); tz != "" {
		return tz
	}
	if name := time.Local.String(); name != "Local" && name != "" {
		return name
	}
	return "UTC"
}
