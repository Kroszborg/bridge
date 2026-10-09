package automation

import (
	"context"
	"errors"
	"net/textproto"

	bmail "bridge/internal/mail"
)

// sendEmail delivers a plain-text email through the configured SMTP server.
// Errors are forwardErrors: a 5xx answer is permanent, anything else retried.
func (s *Service) sendEmail(ctx context.Context, to, subject, body string) error {
	if s.smtp == nil {
		return permanent("Email forwarding is not configured on this server (BRIDGE_SMTP_HOST).", 0)
	}
	err := bmail.SendSMTP(ctx, *s.smtp, to, subject, body)
	if err == nil {
		return nil
	}
	var te *textproto.Error
	if errors.As(err, &te) && te.Code >= 500 {
		return permanent("The mail server refused the message: "+clip(err.Error(), 300), 0)
	}
	return retryable("Could not send the email: "+clip(err.Error(), 300), 0)
}
