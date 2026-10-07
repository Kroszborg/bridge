package main

import (
	"context"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

// ---- opt-outs ---------------------------------------------------------------

type optOut struct {
	ID        string    `json:"id"`
	Number    string    `json:"number"`
	Source    string    `json:"source"`
	Keyword   *string   `json:"keyword"`
	CreatedAt time.Time `json:"created_at"`
}

const optOutsUsage = "usage: bridgectl optouts [--source keyword|manual|api] | optouts add NUMBER | optouts remove NUMBER | optouts check NUMBER"

// cmdOptOuts lists, adds, removes and checks numbers on the project's opt-out list.
func cmdOptOuts(ctx context.Context, args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "add", "remove", "rm", "check":
			return cmdOptOutNumber(ctx, args[0], args[1:])
		case "list":
			args = args[1:]
		}
	}
	var source string
	var limit int
	c, g, pos, err := setup("optouts", args, func(fs *flag.FlagSet) {
		fs.StringVar(&source, "source", "", "keyword, manual or api")
		fs.IntVar(&limit, "limit", 50, "how many (1 to 100)")
	})
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return errors.New(optOutsUsage)
	}
	q := url.Values{"limit": {strconv.Itoa(limit)}}
	if source != "" {
		q.Set("source", source)
	}
	var page struct {
		Data    []optOut `json:"data"`
		HasMore bool     `json:"has_more"`
	}
	if err := c.get(ctx, "/v1/opt-outs", q, &page); err != nil {
		return err
	}
	if g.json {
		return printJSON(page)
	}
	if len(page.Data) == 0 {
		fmt.Println("No opted-out numbers.")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, paint(dim, "NUMBER\tSOURCE\tKEYWORD\tSINCE"))
	for _, o := range page.Data {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", o.Number, o.Source, deref(o.Keyword, "-"), o.CreatedAt.Local().Format("Jan 2 2006 15:04"))
	}
	_ = tw.Flush()
	if page.HasMore {
		fmt.Println(paint(dim, "More numbers exist; raise --limit or export the list from the dashboard."))
	}
	return nil
}

func cmdOptOutNumber(ctx context.Context, verb string, args []string) error {
	c, g, pos, err := setup("optouts "+verb, args, nil)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New(optOutsUsage)
	}
	number := pos[0]
	path := "/v1/opt-outs/" + url.PathEscape(number)
	switch verb {
	case "add":
		var o optOut
		if _, err := c.request(ctx, http.MethodPost, "/v1/opt-outs", nil, map[string]any{"number": number}, nil, &o); err != nil {
			return err
		}
		if g.json {
			return printJSON(o)
		}
		fmt.Printf("%s is opted out (%s). Ordinary messages to it are refused; one-time passwords still go.\n", o.Number, o.Source)
	case "remove", "rm":
		if _, err := c.request(ctx, http.MethodDelete, path, nil, nil, nil, nil); err != nil {
			return err
		}
		if g.json {
			return printJSON(map[string]any{"number": number, "opted_out": false})
		}
		fmt.Printf("%s can receive messages again.\n", number)
	case "check":
		var o optOut
		err := c.get(ctx, path, nil, &o)
		var ae *apiError
		if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
			if g.json {
				return printJSON(map[string]any{"number": number, "opted_out": false})
			}
			fmt.Printf("%s is not opted out.\n", number)
			return nil
		}
		if err != nil {
			return err
		}
		if g.json {
			return printJSON(map[string]any{"number": o.Number, "opted_out": true, "opt_out": o})
		}
		fmt.Printf("%s opted out on %s (%s%s).\n", o.Number, o.CreatedAt.Local().Format("Jan 2 2006 15:04"), o.Source, keywordNote(o.Keyword))
	}
	return nil
}

func keywordNote(k *string) string {
	if k == nil {
		return ""
	}
	return ": " + *k
}

// ---- broadcasts -------------------------------------------------------------

type broadcastCounts struct {
	Recipients int `json:"recipients"`
	Queued     int `json:"queued"`
	Sent       int `json:"sent"`
	Delivered  int `json:"delivered"`
	Failed     int `json:"failed"`
	Canceled   int `json:"canceled"`
	Skipped    int `json:"skipped"`
	Duplicates int `json:"duplicates"`
}

type broadcast struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Environment   string          `json:"environment"`
	Status        string          `json:"status"`
	ScheduledAt   *time.Time      `json:"scheduled_at"`
	Counts        broadcastCounts `json:"counts"`
	TotalSegments int             `json:"total_segments"`
	CreatedAt     time.Time       `json:"created_at"`
}

type broadcastPreview struct {
	Recipients      int `json:"recipients"`
	SkippedOptedOut int `json:"skipped_opted_out"`
	Duplicates      int `json:"duplicates"`
	TotalSegments   int `json:"total_segments"`
	Samples         []struct {
		To       string `json:"to"`
		Text     string `json:"text"`
		Segments int    `json:"segments"`
	} `json:"samples"`
}

type broadcastRecipient struct {
	To   string            `json:"to"`
	Vars map[string]string `json:"vars,omitempty"`
}

// maxBroadcastRecipients matches the API's limit per broadcast.
const maxBroadcastRecipients = 10000

// placeholderRE matches {name} the way the API does.
var placeholderRE = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]{0,39})\}`)

const broadcastUsage = `usage: bridgectl broadcast send --csv FILE --template "Hi {name}" [--name N] [--at TIME] [--device ID] [--dry-run] [--test]
       bridgectl broadcast get ID | broadcast cancel ID | broadcasts [--status S]`

func cmdBroadcast(ctx context.Context, args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "send", "create":
			return cmdBroadcastSend(ctx, args[1:])
		case "get":
			return cmdBroadcastOne(ctx, "get", args[1:])
		case "cancel":
			return cmdBroadcastOne(ctx, "cancel", args[1:])
		case "list":
			args = args[1:]
		}
	}
	var status string
	var limit int
	c, g, pos, err := setup("broadcasts", args, func(fs *flag.FlagSet) {
		fs.StringVar(&status, "status", "", "scheduled, sending, completed or canceled")
		fs.IntVar(&limit, "limit", 20, "how many (1 to 100)")
	})
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return errors.New(broadcastUsage)
	}
	q := url.Values{"limit": {strconv.Itoa(limit)}}
	if status != "" {
		q.Set("status", status)
	}
	var page struct {
		Data    []broadcast `json:"data"`
		HasMore bool        `json:"has_more"`
	}
	if err := c.get(ctx, "/v1/broadcasts", q, &page); err != nil {
		return err
	}
	if g.json {
		return printJSON(page)
	}
	if len(page.Data) == 0 {
		fmt.Println("No broadcasts.")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, paint(dim, "ID\tSTATUS\tNAME\tRECIPIENTS\tDELIVERED\tFAILED\tCREATED"))
	for _, b := range page.Data {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\t%d\t%s\n", b.ID, paint(broadcastColor(b.Status), b.Status), truncate(b.Name, 30),
			b.Counts.Recipients, b.Counts.Delivered, b.Counts.Failed, b.CreatedAt.Local().Format("Jan 2 15:04"))
	}
	_ = tw.Flush()
	if page.HasMore {
		fmt.Println(paint(dim, "More broadcasts exist; raise --limit."))
	}
	return nil
}

func broadcastColor(status string) string {
	switch status {
	case "completed":
		return green
	case "canceled":
		return red
	case "sending":
		return cyan
	}
	return yellow
}

func cmdBroadcastOne(ctx context.Context, verb string, args []string) error {
	c, g, pos, err := setup("broadcast "+verb, args, nil)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New(broadcastUsage)
	}
	path := "/v1/broadcasts/" + url.PathEscape(pos[0])
	var b broadcast
	if verb == "cancel" {
		_, err = c.request(ctx, http.MethodPost, path+"/cancel", nil, nil, nil, &b)
	} else {
		err = c.get(ctx, path, nil, &b)
	}
	if err != nil {
		return err
	}
	if g.json {
		return printJSON(b)
	}
	printBroadcast(b)
	return nil
}

func printBroadcast(b broadcast) {
	name := b.Name
	if name == "" {
		name = "(no name)"
	}
	fmt.Printf("%s  %s  %s  %s\n", paint(bold, b.ID), paint(broadcastColor(b.Status), b.Status), name, envLabel(b.Environment))
	if b.ScheduledAt != nil && b.Status == "scheduled" {
		fmt.Printf("  starts %s\n", b.ScheduledAt.Local().Format("Mon Jan 2 2006 15:04 MST"))
	}
	n := b.Counts
	fmt.Printf("  %d recipient(s), %d segment(s) in all\n", n.Recipients, b.TotalSegments)
	fmt.Printf("  queued %d · sent %d · delivered %d · failed %d · canceled %d · skipped %d · duplicates %d\n",
		n.Queued, n.Sent, n.Delivered, n.Failed, n.Canceled, n.Skipped, n.Duplicates)
}

func cmdBroadcastSend(ctx context.Context, args []string) error {
	var file, tmpl, name, at, device string
	var dryRun, testOnly bool
	c, g, pos, err := setup("broadcast send", args, func(fs *flag.FlagSet) {
		fs.StringVar(&file, "csv", "", "CSV file: a to (or phone) column, then one column per template variable")
		fs.StringVar(&tmpl, "template", "", "the message, with {column} placeholders")
		fs.StringVar(&name, "name", "", "a name to find the broadcast by")
		fs.StringVar(&at, "at", "", "start later, at this RFC 3339 time (e.g. 2026-11-01T09:00:00+05:30)")
		fs.StringVar(&device, "device", "", "send every message through this device")
		fs.BoolVar(&dryRun, "dry-run", false, "validate and preview; create nothing")
		fs.BoolVar(&testOnly, "test", false, "refuse to run unless the API key is a test key (bk_test_)")
	})
	if err != nil {
		return err
	}
	if len(pos) > 0 || file == "" || strings.TrimSpace(tmpl) == "" {
		return errors.New(broadcastUsage)
	}
	if testOnly && !strings.HasPrefix(c.apiKey, "bk_test_") {
		return errors.New("--test needs a test key (bk_test_…); this key sends real SMS")
	}
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	recipients, err := readRecipients(f, tmpl)
	if err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	body := map[string]any{"template": tmpl, "recipients": recipients}
	if name != "" {
		body["name"] = name
	}
	if device != "" {
		body["device_id"] = device
	}
	if at != "" {
		t, err := time.Parse(time.RFC3339, at)
		if err != nil {
			return errors.New("--at needs an RFC 3339 time, e.g. 2026-11-01T09:00:00+05:30")
		}
		body["scheduled_at"] = t.Format(time.RFC3339)
	}
	if dryRun {
		body["dry_run"] = true
		var p broadcastPreview
		if _, err := c.request(ctx, http.MethodPost, "/v1/broadcasts", nil, body, nil, &p); err != nil {
			return err
		}
		if g.json {
			return printJSON(p)
		}
		fmt.Printf("%s nothing was created.\n", paint(yellow, "Dry run:"))
		fmt.Printf("  %d recipient(s) · %d segment(s) in all · %d opted out · %d duplicate(s) removed\n",
			p.Recipients, p.TotalSegments, p.SkippedOptedOut, p.Duplicates)
		for _, s := range p.Samples {
			fmt.Printf("  %s %s %q\n", paint(dim, s.To), paint(dim, "("+strconv.Itoa(s.Segments)+" seg)"), truncate(s.Text, 160))
		}
		return nil
	}
	var b broadcast
	if _, err := c.request(ctx, http.MethodPost, "/v1/broadcasts", nil, body, nil, &b); err != nil {
		return err
	}
	if g.json {
		return printJSON(b)
	}
	printBroadcast(b)
	fmt.Printf("  %s\n", paint(dim, "follow it with: bridgectl broadcast get "+b.ID))
	return nil
}

// readRecipients reads a CSV with a header row. The first column, named to
// or phone, holds the numbers; the other columns are template variables. Only
// the variables the template uses are sent.
func readRecipients(r io.Reader, tmpl string) ([]broadcastRecipient, error) {
	cr := csv.NewReader(r)
	cr.TrimLeadingSpace = true
	header, err := cr.Read()
	if errors.Is(err, io.EOF) {
		return nil, errors.New("the file is empty; the first row must name the columns, starting with to")
	}
	if err != nil {
		return nil, err
	}
	for i := range header {
		header[i] = strings.TrimSpace(strings.TrimPrefix(header[i], "\uFEFF"))
	}
	if first := strings.ToLower(header[0]); first != "to" && first != "phone" {
		return nil, fmt.Errorf("the first column must be named to or phone, not %q", header[0])
	}
	cols := map[string]int{}
	for i, h := range header[1:] {
		cols[h] = i + 1
	}
	var used []string
	for _, m := range placeholderRE.FindAllStringSubmatch(tmpl, -1) {
		if slices.Contains(used, m[1]) {
			continue
		}
		if _, ok := cols[m[1]]; !ok {
			return nil, fmt.Errorf("the template uses {%s} but the file has no %s column", m[1], m[1])
		}
		used = append(used, m[1])
	}
	var out []broadcastRecipient
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		line, _ := cr.FieldPos(0)
		if strings.TrimSpace(strings.Join(rec, "")) == "" {
			continue // blank line
		}
		to := strings.TrimSpace(rec[0])
		if to == "" {
			return nil, fmt.Errorf("line %d has no number", line)
		}
		rc := broadcastRecipient{To: to}
		if len(used) > 0 {
			rc.Vars = make(map[string]string, len(used))
			for _, name := range used {
				rc.Vars[name] = rec[cols[name]]
			}
		}
		out = append(out, rc)
		if len(out) > maxBroadcastRecipients {
			return nil, fmt.Errorf("more than %d recipients; split the file into several broadcasts", maxBroadcastRecipients)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no recipients below the header row")
	}
	return out, nil
}

// ---- schedules --------------------------------------------------------------

type scheduleItem struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	To          string     `json:"to"`
	Message     string     `json:"message"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	NextRunAt   *time.Time `json:"next_run_at"`
	LastRunAt   *time.Time `json:"last_run_at"`
	LastError   *string    `json:"last_error"`
	RunCount    int        `json:"run_count"`
}

// cmdSchedules lists scheduled and repeating messages.
func cmdSchedules(ctx context.Context, args []string) error {
	if len(args) > 0 && args[0] == "list" {
		args = args[1:]
	}
	var limit int
	c, g, pos, err := setup("schedules", args, func(fs *flag.FlagSet) {
		fs.IntVar(&limit, "limit", 25, "how many (1 to 100)")
	})
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return errors.New("usage: bridgectl schedules [--limit N]")
	}
	var page struct {
		Data    []scheduleItem `json:"data"`
		HasMore bool           `json:"has_more"`
	}
	if err := c.get(ctx, "/v1/schedules", url.Values{"limit": {strconv.Itoa(limit)}}, &page); err != nil {
		return err
	}
	if g.json {
		return printJSON(page)
	}
	if len(page.Data) == 0 {
		fmt.Println("No scheduled messages.")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, paint(dim, "ID\tSTATUS\tTO\tWHEN\tNEXT RUN\tRUNS\tNOTE"))
	for _, s := range page.Data {
		color := green
		switch s.Status {
		case "paused":
			color = yellow
		case "completed":
			color = dim
		}
		next := "-"
		if s.NextRunAt != nil {
			next = s.NextRunAt.Local().Format("Jan 2 15:04")
		}
		note := truncate(s.Name, 30)
		if s.LastError != nil {
			note = paint(red, truncate(*s.LastError, 50))
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%d\t%s\n", s.ID, paint(color, s.Status), s.To, s.Description, next, s.RunCount, note)
	}
	_ = tw.Flush()
	if page.HasMore {
		fmt.Println(paint(dim, "More schedules exist; raise --limit."))
	}
	return nil
}
