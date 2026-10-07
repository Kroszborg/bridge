package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"time"
)

type verification struct {
	ID                string     `json:"id"`
	Status            string     `json:"status"`
	To                string     `json:"to"`
	Environment       string     `json:"environment"`
	Attempts          int        `json:"attempts"`
	AttemptsRemaining int        `json:"attempts_remaining"`
	ExpiresAt         time.Time  `json:"expires_at"`
	ResendAvailableAt time.Time  `json:"resend_available_at"`
	VerifiedAt        *time.Time `json:"verified_at"`
	MessageID         *string    `json:"message_id"`
	MessageStatus     *string    `json:"message_status"`
	Code              *string    `json:"code"`
	CreatedAt         time.Time  `json:"created_at"`
}

func cmdOTP(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: bridgectl otp send TO | otp verify TO CODE | otp verify --id ID CODE | otp get ID")
	}
	switch args[0] {
	case "send":
		return cmdOTPSend(ctx, args[1:])
	case "verify", "check":
		return cmdOTPVerify(ctx, args[1:])
	case "get":
		return cmdOTPGet(ctx, args[1:])
	}
	return fmt.Errorf("unknown otp command %q; use send, verify or get", args[0])
}

func cmdOTPSend(ctx context.Context, args []string) error {
	var hash, app string
	c, g, pos, err := setup("otp send", args, func(fs *flag.FlagSet) {
		fs.StringVar(&hash, "android-hash", "", "your Android app's 11-character SMS Retriever hash")
		fs.StringVar(&app, "app", "", "Verify app ID or slug (default: the project's default app)")
	})
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("usage: bridgectl otp send TO, e.g. bridgectl otp send +919876543210")
	}
	body := map[string]any{"to": pos[0]}
	if hash != "" {
		body["android_app_hash"] = hash
	}
	if app != "" {
		body["app"] = app
	}
	var v verification
	if _, err := c.request(ctx, "POST", "/v1/otp", nil, body, nil, &v); err != nil {
		return err
	}
	if g.json {
		return printJSON(v)
	}
	fmt.Printf("%s %s %s · expires %s\n", paint(bold, v.ID), paint(otpColor(v.Status), v.Status), v.To,
		v.ExpiresAt.Local().Format("15:04:05"))
	if v.Code != nil {
		fmt.Printf("  code %s %s\n", paint(bold, *v.Code), paint(dim, "(shown because this is a test key)"))
	}
	fmt.Printf("  %s\n", paint(dim, "check it with: bridgectl otp verify "+v.To+" CODE"))
	return nil
}

func cmdOTPVerify(ctx context.Context, args []string) error {
	var otpID, app string
	c, g, pos, err := setup("otp verify", args, func(fs *flag.FlagSet) {
		fs.StringVar(&otpID, "id", "", "check this verification instead of the latest code sent to a number")
		fs.StringVar(&app, "app", "", "only codes of this Verify app (ID or slug)")
	})
	if err != nil {
		return err
	}
	body := map[string]any{}
	switch {
	case otpID != "" && len(pos) == 1:
		body["id"], body["code"] = otpID, pos[0]
	case otpID == "" && len(pos) == 2:
		body["to"], body["code"] = pos[0], pos[1]
	default:
		return errors.New("usage: bridgectl otp verify TO CODE, or bridgectl otp verify --id ID CODE")
	}
	if app != "" {
		body["app"] = app
	}
	var res struct {
		Valid        bool         `json:"valid"`
		Verification verification `json:"verification"`
	}
	if _, err := c.request(ctx, "POST", "/v1/otp/verify", nil, body, nil, &res); err != nil {
		return err
	}
	if g.json {
		if err := printJSON(res); err != nil {
			return err
		}
	} else {
		v := res.Verification
		switch {
		case res.Valid:
			fmt.Printf("%s %s %s\n", paint(green, "valid"), paint(dim, v.ID), v.To)
		case v.Status == "pending":
			fmt.Printf("%s %d attempt(s) left\n", paint(red, "wrong code"), v.AttemptsRemaining)
		default:
			fmt.Printf("%s the verification is %s; send a new code\n", paint(red, "not valid:"), paint(otpColor(v.Status), v.Status))
		}
	}
	if !res.Valid {
		os.Exit(1)
	}
	return nil
}

func cmdOTPGet(ctx context.Context, args []string) error {
	c, g, pos, err := setup("otp get", args, nil)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("usage: bridgectl otp get ID")
	}
	var v verification
	if err := c.get(ctx, "/v1/otp/"+url.PathEscape(pos[0]), nil, &v); err != nil {
		return err
	}
	if g.json {
		return printJSON(v)
	}
	fmt.Printf("%s  %s  %s\n", paint(bold, v.ID), paint(otpColor(v.Status), v.Status), v.To)
	fmt.Printf("  attempts %d, %d left · expires %s\n", v.Attempts, v.AttemptsRemaining, v.ExpiresAt.Local().Format("15:04:05"))
	if v.MessageID != nil {
		fmt.Printf("  sms %s %s\n", *v.MessageID, paint(statusColor(deref(v.MessageStatus, "")), deref(v.MessageStatus, "")))
	}
	return nil
}

func otpColor(status string) string {
	switch status {
	case "verified":
		return green
	case "failed", "expired":
		return red
	case "canceled":
		return dim
	}
	return yellow
}
