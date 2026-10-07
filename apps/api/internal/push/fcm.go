package push

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const fcmScope = "https://www.googleapis.com/auth/firebase.messaging"

// FCM sends Firebase Cloud Messaging data messages through the HTTP v1 API.
type FCM struct {
	projectID string
	tokens    oauth2.TokenSource
	client    *http.Client
	endpoint  string // overridable in tests
}

// NewFCM builds a sender from a service-account JSON document.
func NewFCM(ctx context.Context, credentialsJSON []byte, projectID string) (*FCM, error) {
	creds, err := google.CredentialsFromJSONWithType(ctx, credentialsJSON, google.ServiceAccount, fcmScope)
	if err != nil {
		return nil, fmt.Errorf("parse FCM service account: %w", err)
	}
	if projectID == "" {
		projectID = creds.ProjectID
	}
	return &FCM{
		projectID: projectID,
		tokens:    creds.TokenSource,
		client:    &http.Client{Timeout: 15 * time.Second},
		endpoint:  "https://fcm.googleapis.com",
	}, nil
}

// Send delivers a high-priority data message to a registration token.
func (f *FCM) Send(ctx context.Context, token string, data map[string]string, ttl time.Duration) error {
	tok, err := f.tokens.Token()
	if err != nil {
		return fmt.Errorf("fetch FCM access token: %w", err)
	}
	body, err := json.Marshal(map[string]any{
		"message": map[string]any{
			"token": token,
			"data":  data,
			"android": map[string]any{
				"priority": "HIGH",
				"ttl":      fmt.Sprintf("%ds", int(ttl.Seconds())),
			},
		},
	})
	if err != nil {
		return err
	}
	url := fmt.Sprintf("%s/v1/projects/%s/messages:send", f.endpoint, f.projectID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	resp, err := f.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode < 300 {
		return nil
	}
	// The registration token is no longer valid; the device must re-register.
	if resp.StatusCode == http.StatusNotFound || strings.Contains(string(respBody), "UNREGISTERED") {
		return ErrGone
	}
	if resp.StatusCode == http.StatusBadRequest && strings.Contains(string(respBody), "registration token") {
		return ErrInvalidTarget
	}
	return fmt.Errorf("FCM returned %s", resp.Status)
}
