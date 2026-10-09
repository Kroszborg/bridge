// Package mail sends Bridge's own email: account verification, password
// resets, invitations and usage notices. It uses the SMTP server configured
// with BRIDGE_SMTP_*, the same one email forwarding uses.
package mail

import (
	"context"
	"errors"

	"bridge/internal/config"
)

// Message is one email. Text is required; HTML is an optional alternative.
type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string
}

// Sender delivers email. A nil Sender means the server has no mail configured.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// ErrNotConfigured is returned when an email is needed but no SMTP server is set.
var ErrNotConfigured = errors.New("email is not configured on this server (BRIDGE_SMTP_HOST)")

// NewSMTP returns a Sender for the configured SMTP server, or nil when none is set.
func NewSMTP(c *config.SMTPConfig) Sender {
	if c == nil {
		return nil
	}
	return smtpSender{c: *c}
}

type smtpSender struct{ c config.SMTPConfig }

// Send delivers the plain-text part; Bridge's emails read well without HTML.
func (s smtpSender) Send(ctx context.Context, m Message) error {
	return SendSMTP(ctx, s.c, m.To, m.Subject, m.Text)
}
