package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

type requestLog struct {
	ID         string    `json:"id"`
	RequestID  string    `json:"request_id"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	Status     int       `json:"status"`
	ErrorCode  *string   `json:"error_code"`
	DurationMs int       `json:"duration_ms"`
	ResourceID *string   `json:"resource_id"`
	CreatedAt  time.Time `json:"created_at"`
}

// cmdLogs lists the API requests made with the project's keys of this key's environment.
func cmdLogs(ctx context.Context, args []string) error {
	var status, method, path string
	var limit int
	c, g, pos, err := setup("logs", args, func(fs *flag.FlagSet) {
		fs.StringVar(&status, "status", "", "success, error, 2xx, 4xx or 5xx")
		fs.StringVar(&method, "method", "", "GET, POST, PUT, PATCH or DELETE")
		fs.StringVar(&path, "path", "", "only paths starting with this, e.g. /v1/messages")
		fs.IntVar(&limit, "limit", 20, "how many entries (1 to 100)")
	})
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return errors.New("usage: bridgectl logs [--status S] [--method M] [--path P] [--limit N]")
	}
	q := url.Values{"limit": {strconv.Itoa(limit)}}
	for k, v := range map[string]string{"status": status, "method": method, "path": path} {
		if v != "" {
			q.Set(k, v)
		}
	}
	var page struct {
		Data    []requestLog `json:"data"`
		HasMore bool         `json:"has_more"`
	}
	if err := c.get(ctx, "/v1/request-logs", q, &page); err != nil {
		return err
	}
	if g.json {
		return printJSON(page)
	}
	if len(page.Data) == 0 {
		fmt.Println(paint(dim, "No requests match."))
		return nil
	}
	for _, l := range page.Data {
		color := green
		switch {
		case l.Status >= 500:
			color = red
		case l.Status >= 400:
			color = yellow
		}
		detail := deref(l.ResourceID, "")
		if l.ErrorCode != nil {
			detail = paint(red, *l.ErrorCode)
		}
		fmt.Printf("%s  %s %-6s %-28s %5s  %s\n", paint(dim, l.CreatedAt.Local().Format("15:04:05")),
			paint(color, strconv.Itoa(l.Status)), l.Method, truncate(l.Path, 28),
			strconv.Itoa(l.DurationMs)+"ms", detail)
	}
	if page.HasMore {
		fmt.Println(paint(dim, "More entries exist; raise --limit or narrow the filters."))
	}
	return nil
}
