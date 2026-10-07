package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// client calls the Bridge developer API with an API key.
type client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func newClient(cfg resolved) *client {
	return &client{baseURL: strings.TrimRight(cfg.URL, "/"), apiKey: cfg.APIKey, http: &http.Client{Timeout: 30 * time.Second}}
}

// apiError is the API's error envelope.
type apiError struct {
	Status    int
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Details   []struct {
		Location string `json:"location"`
		Message  string `json:"message"`
	} `json:"details"`
}

func (e *apiError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s", e.Code, e.Message)
	for _, d := range e.Details {
		fmt.Fprintf(&b, "\n  %s: %s", d.Location, d.Message)
	}
	if e.RequestID != "" {
		fmt.Fprintf(&b, "\n  (request %s)", e.RequestID)
	}
	return b.String()
}

func (c *client) request(ctx context.Context, method, path string, query url.Values, body any, headers map[string]string, out any) (http.Header, error) {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "bridgectl/"+version)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach Bridge at %s: %w", c.baseURL, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		var env struct {
			Error apiError `json:"error"`
		}
		if json.Unmarshal(raw, &env) != nil || env.Error.Code == "" {
			return nil, fmt.Errorf("Bridge responded %s", resp.Status)
		}
		env.Error.Status = resp.StatusCode
		return nil, &env.Error
	}
	if out != nil {
		if raw2, ok := out.(*json.RawMessage); ok {
			*raw2 = raw
		} else if err := json.Unmarshal(raw, out); err != nil {
			return nil, fmt.Errorf("unexpected response from Bridge: %w", err)
		}
	}
	return resp.Header, nil
}

func (c *client) get(ctx context.Context, path string, query url.Values, out any) error {
	_, err := c.request(ctx, http.MethodGet, path, query, nil, nil, out)
	return err
}

// sseEvent is one Server-Sent Event from /v1/events/stream.
type sseEvent struct {
	ID   string
	Type string
	Data []byte // the webhook envelope, exactly as sent
}

// stream reads the event stream until ctx ends or the connection drops.
// connected is called once the server accepts the stream.
func (c *client) stream(ctx context.Context, types []string, connected func(), handle func(sseEvent)) error {
	u := c.baseURL + "/v1/events/stream"
	if len(types) > 0 {
		u += "?types=" + url.QueryEscape(strings.Join(types, ","))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", "bridgectl/"+version)
	// No client timeout: the stream is long-lived; the server pings every 15 s.
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return fmt.Errorf("could not reach Bridge at %s: %w", c.baseURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		var env struct {
			Error apiError `json:"error"`
		}
		if json.Unmarshal(raw, &env) == nil && env.Error.Code != "" {
			env.Error.Status = resp.StatusCode
			return &permanentError{&env.Error}
		}
		return fmt.Errorf("event stream: Bridge responded %s", resp.Status)
	}
	connected()

	// Pings arrive every 15 s; silence for much longer means the connection is dead.
	alive := make(chan struct{}, 1)
	go func() {
		t := time.NewTimer(45 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-alive:
				t.Reset(45 * time.Second)
			case <-t.C:
				resp.Body.Close()
				return
			}
		}
	}()

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	var ev sseEvent
	var data [][]byte
	for sc.Scan() {
		select {
		case alive <- struct{}{}:
		default:
		}
		line := sc.Bytes()
		switch {
		case len(line) == 0:
			if ev.Type != "" {
				ev.Data = bytes.Join(data, []byte("\n"))
				handle(ev)
			}
			ev, data = sseEvent{}, nil
		case bytes.HasPrefix(line, []byte(":")):
			// comment or ping
		case bytes.HasPrefix(line, []byte("id: ")):
			ev.ID = string(line[4:])
		case bytes.HasPrefix(line, []byte("event: ")):
			ev.Type = string(line[7:])
		case bytes.HasPrefix(line, []byte("data: ")):
			data = append(data, append([]byte(nil), line[6:]...))
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("event stream: %w", err)
	}
	return errors.New("event stream closed by the server")
}

// permanentError is not worth retrying (bad key, bad input).
type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }
