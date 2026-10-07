// Package push wakes Android gateways whose WebSocket connection is down.
//
// The "foss" app flavor registers a UnifiedPush endpoint (WebPush); the "gms"
// flavor registers a Firebase Cloud Messaging token. A wake-up carries no
// secrets: it only tells the app to reconnect and fetch its work.
package push

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"

	"bridge/internal/config"
	"bridge/internal/db/dbq"
)

const (
	ProviderUnifiedPush = "unifiedpush"
	ProviderFCM         = "fcm"

	wakeTTL = time.Hour
)

var (
	// ErrGone means the push registration expired; clear it and wait for the device to re-register.
	ErrGone = errors.New("push registration is no longer valid")
	// ErrInvalidTarget means the stored registration is malformed.
	ErrInvalidTarget = errors.New("push registration is invalid")
	// ErrNotConfigured means this server cannot send through the device's provider.
	ErrNotConfigured = errors.New("push provider is not configured on this server")
)

// Target is a device's push registration.
type Target struct {
	Provider string
	Endpoint string // WebPush endpoint URL, or FCM registration token
	P256dh   string
	Auth     string
}

// Service sends wake-ups through whichever provider a device registered.
type Service struct {
	webpush *WebPush
	fcm     *FCM // nil when FCM is not configured
}

// New loads (or creates on first use) the server's VAPID key and, if
// configured, the FCM service account.
func New(ctx context.Context, q *dbq.Queries, cfg *config.Config) (*Service, error) {
	vapid, err := loadOrCreateVAPID(ctx, q)
	if err != nil {
		return nil, err
	}
	s := &Service{webpush: NewWebPush(vapid, cfg.VAPIDSubject, SafeClient(cfg.PushAllowPrivate))}
	if cfg.FCM != nil {
		raw, err := os.ReadFile(cfg.FCM.CredentialsFile)
		if err != nil {
			return nil, fmt.Errorf("read BRIDGE_FCM_CREDENTIALS_FILE: %w", err)
		}
		if s.fcm, err = NewFCM(ctx, raw, cfg.FCM.ProjectID); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// VAPIDPublicKey is handed to devices so their distributor can authenticate this server.
func (s *Service) VAPIDPublicKey() string { return s.webpush.PublicKey() }

// FCMEnabled reports whether FCM wake-ups can be sent.
func (s *Service) FCMEnabled() bool { return s.fcm != nil }

// Wake asks a device to reconnect. reason is a short machine-readable hint.
func (s *Service) Wake(ctx context.Context, t Target, reason string) error {
	switch t.Provider {
	case ProviderUnifiedPush:
		payload, _ := json.Marshal(map[string]string{"t": "wake", "r": reason})
		return s.webpush.Send(ctx, Subscription{Endpoint: t.Endpoint, P256dh: t.P256dh, Auth: t.Auth}, payload, wakeTTL)
	case ProviderFCM:
		if s.fcm == nil {
			return ErrNotConfigured
		}
		return s.fcm.Send(ctx, t.Endpoint, map[string]string{"t": "wake", "r": reason}, wakeTTL)
	}
	return ErrInvalidTarget
}

const vapidKeyName = "vapid"

func loadOrCreateVAPID(ctx context.Context, q *dbq.Queries) (*ecdsa.PrivateKey, error) {
	der, err := q.GetServerKey(ctx, vapidKeyName)
	if errors.Is(err, pgx.ErrNoRows) {
		_, fresh, genErr := GenerateVAPIDKey()
		if genErr != nil {
			return nil, genErr
		}
		// Another instance may win the race; re-read whatever was stored.
		if err := q.InsertServerKey(ctx, dbq.InsertServerKeyParams{Name: vapidKeyName, PrivateKey: fresh}); err != nil {
			return nil, err
		}
		der, err = q.GetServerKey(ctx, vapidKeyName)
	}
	if err != nil {
		return nil, fmt.Errorf("load VAPID key: %w", err)
	}
	return ParseVAPIDKey(der)
}
