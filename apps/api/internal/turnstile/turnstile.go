// Package turnstile checks Cloudflare Turnstile tokens server-side.
package turnstile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultURL is Cloudflare's siteverify endpoint.
const DefaultURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

// Verifier calls siteverify.
type Verifier struct {
	url    string
	client *http.Client
}

// New returns a Verifier. An empty url uses DefaultURL (tests point it at a fake).
func New(verifyURL string, client *http.Client) *Verifier {
	if verifyURL == "" {
		verifyURL = DefaultURL
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Verifier{url: verifyURL, client: client}
}

// Result is siteverify's answer.
type Result struct {
	Success    bool     `json:"success"`
	ErrorCodes []string `json:"error-codes"`
	Hostname   string   `json:"hostname"`
	Action     string   `json:"action"`
}

// ErrUnavailable means siteverify could not be reached or answered oddly.
var ErrUnavailable = errors.New("turnstile siteverify is unavailable")

// Verify checks token against secret. remoteIP may be empty. A token that
// Cloudflare rejects returns a Result with Success false and no error.
func (v *Verifier) Verify(ctx context.Context, secret, token, remoteIP string) (Result, error) {
	if strings.TrimSpace(token) == "" || len(token) > 2048 {
		return Result{ErrorCodes: []string{"missing-input-response"}}, nil
	}
	form := url.Values{"secret": {secret}, "response": {token}}
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.url, strings.NewReader(form.Encode()))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := v.client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	var r Result
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 64<<10)).Decode(&r); err != nil {
		return Result{}, fmt.Errorf("%w: status %d", ErrUnavailable, resp.StatusCode)
	}
	return r, nil
}
