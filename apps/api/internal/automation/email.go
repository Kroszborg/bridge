package automation

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"bridge/internal/config"
)

// sendEmail delivers a plain-text email through the configured SMTP server.
// Errors are forwardErrors: a 5xx answer is permanent, anything else retried.
func (s *Service) sendEmail(ctx context.Context, to, subject, body string) error {
	if s.smtp == nil {
		return permanent("Email forwarding is not configured on this server (BRIDGE_SMTP_HOST).", 0)
	}
	err := SendMail(ctx, *s.smtp, to, subject, body)
	if err == nil {
		return nil
	}
	var te *textproto.Error
	if errors.As(err, &te) && te.Code >= 500 {
		return permanent("The mail server refused the message: "+clip(err.Error(), 300), 0)
	}
	return retryable("Could not send the email: "+clip(err.Error(), 300), 0)
}

// SendMail sends one plain-text message with net/smtp, using STARTTLS,
// implicit TLS or no encryption as configured. Credentials are only sent over
// TLS (or to localhost), as net/smtp's PLAIN auth enforces.
func SendMail(ctx context.Context, c config.SMTPConfig, to, subject, body string) error {
	from, err := mail.ParseAddress(c.From)
	if err != nil {
		return errors.New("BRIDGE_SMTP_FROM is not a valid address")
	}
	addr := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	deadline := time.Now().Add(30 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	dialer := &net.Dialer{Deadline: deadline}
	tlsConfig := &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}
	var conn net.Conn
	if c.TLS == config.SMTPTLS {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: tlsConfig}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return err
	}
	_ = conn.SetDeadline(deadline)
	client, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		conn.Close()
		return err
	}
	defer client.Close()
	if err := client.Hello(helloName()); err != nil {
		return err
	}
	if c.TLS == config.SMTPStartTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("the mail server does not offer STARTTLS; set BRIDGE_SMTP_TLS=tls or none")
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return err
		}
	}
	if c.Username != "" {
		if ok, _ := client.Extension("AUTH"); !ok {
			return errors.New("the mail server does not offer authentication")
		}
		if err := client.Auth(smtp.PlainAuth("", c.Username, c.Password, c.Host)); err != nil {
			return err
		}
	}
	if err := client.Mail(from.Address); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(buildMessage(from, to, subject, body)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func helloName() string {
	return "bridge.localhost"
}

// buildMessage renders headers and a quoted-printable UTF-8 body. Header
// values are stripped of line breaks so a sender ID cannot inject headers.
func buildMessage(from *mail.Address, to, subject, body string) []byte {
	clean := func(s string) string { return strings.NewReplacer("\r", " ", "\n", " ").Replace(s) }
	var idBytes [12]byte
	_, _ = rand.Read(idBytes[:])
	domain := "bridge.localhost"
	if _, d, ok := strings.Cut(from.Address, "@"); ok {
		domain = d
	}
	var b bytes.Buffer
	h := func(k, v string) { b.WriteString(k + ": " + v + "\r\n") }
	h("From", from.String())
	h("To", clean(to))
	h("Subject", mime.QEncoding.Encode("utf-8", clean(subject)))
	h("Date", time.Now().UTC().Format(time.RFC1123Z))
	h("Message-ID", "<"+hex.EncodeToString(idBytes[:])+"@"+domain+">")
	h("MIME-Version", "1.0")
	h("Content-Type", "text/plain; charset=utf-8")
	h("Content-Transfer-Encoding", "quoted-printable")
	h("Auto-Submitted", "auto-generated")
	b.WriteString("\r\n")
	qp := quotedprintable.NewWriter(&b)
	_, _ = qp.Write([]byte(strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n")))
	_ = qp.Close()
	return b.Bytes()
}
