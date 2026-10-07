package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// ---- Twilio ----------------------------------------------------------------

var twilioSpec = ProviderSpec{
	Kind: Twilio, Name: "Twilio", Description: "Global SMS with delivery reports.", Callbacks: "per_message",
	Credentials: []ProviderField{
		{Key: "account_sid", Label: "Account SID", Help: "Starts with AC. Twilio Console → Account info.", Required: true},
		{Key: "auth_token", Label: "Auth token", Secret: true, Required: true},
	},
	Config: []ProviderField{
		{Key: "from", Label: "From", Help: "A Twilio number in E.164, or an alphanumeric sender ID where allowed."},
		{Key: "messaging_service_sid", Label: "Messaging Service SID", Help: "Starts with MG. Used instead of From when set."},
	},
}

type twilio struct {
	base
	creds, config map[string]string
}

func (t *twilio) auth(req *http.Request) {
	req.SetBasicAuth(t.creds["account_sid"], t.creds["auth_token"])
}

func (t *twilio) Send(ctx context.Context, m Outgoing) (Accepted, error) {
	form := url.Values{"To": {m.To}, "Body": {m.Body}}
	if sid := t.config["messaging_service_sid"]; sid != "" {
		form.Set("MessagingServiceSid", sid)
	} else {
		form.Set("From", t.config["from"])
	}
	if m.CallbackURL != "" {
		form.Set("StatusCallback", m.CallbackURL)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		t.url+"/2010-04-01/Accounts/"+url.PathEscape(t.creds["account_sid"])+"/Messages.json", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	t.auth(req)
	var out struct {
		SID     string `json:"sid"`
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	status, err := t.do(req, &out)
	if err != nil {
		return Accepted{}, err
	}
	if status >= 300 || out.SID == "" {
		return Accepted{}, &Error{Kind: Twilio, Code: "twilio_" + strconv.Itoa(out.Code), Message: or(out.Message, "Twilio refused the message.")}
	}
	return Accepted{ExternalID: out.SID}, nil
}

func (t *twilio) Check(ctx context.Context) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		t.url+"/2010-04-01/Accounts/"+url.PathEscape(t.creds["account_sid"])+".json", nil)
	t.auth(req)
	var out struct {
		FriendlyName string `json:"friendly_name"`
		Status       string `json:"status"`
		Message      string `json:"message"`
	}
	status, err := t.do(req, &out)
	if err != nil {
		return "", err
	}
	if status >= 300 {
		return "", &Error{Kind: Twilio, Code: "twilio_auth", Message: or(out.Message, "Twilio rejected the credentials.")}
	}
	return fmt.Sprintf("Account %q is %s.", out.FriendlyName, out.Status), nil
}

// ---- Vonage ----------------------------------------------------------------

var vonageSpec = ProviderSpec{
	Kind: Vonage, Name: "Vonage", Description: "Global SMS (Vonage SMS API) with delivery receipts.", Callbacks: "per_message",
	Credentials: []ProviderField{
		{Key: "api_key", Label: "API key", Required: true},
		{Key: "api_secret", Label: "API secret", Secret: true, Required: true},
	},
	Config: []ProviderField{
		{Key: "from", Label: "From", Help: "A Vonage number, or an alphanumeric sender ID where allowed.", Required: true},
	},
}

type vonage struct {
	base
	creds, config map[string]string
}

func (v *vonage) Send(ctx context.Context, m Outgoing) (Accepted, error) {
	form := url.Values{
		"api_key": {v.creds["api_key"]}, "api_secret": {v.creds["api_secret"]},
		"from": {v.config["from"]}, "to": {digits(m.To)}, "text": {m.Body},
		"client-ref": {m.MessageID},
	}
	if m.Unicode {
		form.Set("type", "unicode")
	}
	if m.CallbackURL != "" {
		form.Set("callback", m.CallbackURL)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, v.url+"/sms/json", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var out struct {
		Messages []struct {
			ID        string `json:"message-id"`
			Status    string `json:"status"`
			ErrorText string `json:"error-text"`
		} `json:"messages"`
	}
	if _, err := v.do(req, &out); err != nil {
		return Accepted{}, err
	}
	if len(out.Messages) == 0 {
		return Accepted{}, &Error{Kind: Vonage, Retryable: true, Code: "vonage_empty", Message: "Vonage returned no message status."}
	}
	first := out.Messages[0]
	if first.Status != "0" {
		// 1 throttled and 5 internal error are worth retrying.
		return Accepted{}, &Error{Kind: Vonage, Retryable: first.Status == "1" || first.Status == "5",
			Code: "vonage_" + first.Status, Message: or(first.ErrorText, "Vonage refused the message.")}
	}
	return Accepted{ExternalID: first.ID}, nil
}

func (v *vonage) Check(ctx context.Context) (string, error) {
	q := url.Values{"api_key": {v.creds["api_key"]}, "api_secret": {v.creds["api_secret"]}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, v.url+"/account/get-balance?"+q.Encode(), nil)
	var out struct {
		Value *float64 `json:"value"`
	}
	status, err := v.do(req, &out)
	if err != nil {
		return "", err
	}
	if status >= 300 || out.Value == nil {
		return "", &Error{Kind: Vonage, Code: "vonage_auth", Message: "Vonage rejected the API key or secret."}
	}
	return fmt.Sprintf("Balance %.2f EUR.", *out.Value), nil
}

// ---- Plivo -----------------------------------------------------------------

var plivoSpec = ProviderSpec{
	Kind: Plivo, Name: "Plivo", Description: "Global SMS with delivery reports.", Callbacks: "per_message",
	Credentials: []ProviderField{
		{Key: "auth_id", Label: "Auth ID", Required: true},
		{Key: "auth_token", Label: "Auth token", Secret: true, Required: true},
	},
	Config: []ProviderField{
		{Key: "from", Label: "From", Help: "A Plivo number in E.164, or a sender ID where allowed.", Required: true},
	},
}

type plivo struct {
	base
	creds, config map[string]string
}

func (p *plivo) auth(req *http.Request) { req.SetBasicAuth(p.creds["auth_id"], p.creds["auth_token"]) }

func (p *plivo) Send(ctx context.Context, m Outgoing) (Accepted, error) {
	body := map[string]any{"src": p.config["from"], "dst": digits(m.To), "text": m.Body}
	if m.CallbackURL != "" {
		body["url"], body["method"] = m.CallbackURL, "POST"
	}
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		p.url+"/v1/Account/"+url.PathEscape(p.creds["auth_id"])+"/Message/", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	p.auth(req)
	var out struct {
		MessageUUID []string `json:"message_uuid"`
		Error       string   `json:"error"`
	}
	status, err := p.do(req, &out)
	if err != nil {
		return Accepted{}, err
	}
	if status >= 300 || len(out.MessageUUID) == 0 {
		return Accepted{}, &Error{Kind: Plivo, Code: "plivo_" + strconv.Itoa(status), Message: or(out.Error, "Plivo refused the message.")}
	}
	return Accepted{ExternalID: out.MessageUUID[0]}, nil
}

func (p *plivo) Check(ctx context.Context) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, p.url+"/v1/Account/"+url.PathEscape(p.creds["auth_id"])+"/", nil)
	p.auth(req)
	var out struct {
		Name        string `json:"name"`
		CashCredits string `json:"cash_credits"`
		Error       string `json:"error"`
	}
	status, err := p.do(req, &out)
	if err != nil {
		return "", err
	}
	if status >= 300 {
		return "", &Error{Kind: Plivo, Code: "plivo_auth", Message: or(out.Error, "Plivo rejected the credentials.")}
	}
	return fmt.Sprintf("Account %q, credit %s.", out.Name, out.CashCredits), nil
}

// ---- MSG91 -----------------------------------------------------------------

var msg91Spec = ProviderSpec{
	Kind: MSG91, Name: "MSG91", Callbacks: "account",
	Description: "India-first. Sends DLT-registered templates through the Flow API; set the delivery-report webhook in MSG91 once.",
	Credentials: []ProviderField{
		{Key: "auth_key", Label: "Auth key", Secret: true, Required: true, Help: "MSG91 → API → Auth key."},
	},
	Config: []ProviderField{
		{Key: "sender_id", Label: "Sender ID", Help: "Your DLT-approved 6-character header, if the template does not set one."},
		{Key: "otp_template_id", Label: "OTP template ID", Help: "MSG91 template for one-time passwords."},
		{Key: "otp_variable", Label: "OTP variable", Help: "The template variable that holds the code. Default OTP."},
		{Key: "message_template_id", Label: "Message template ID", Help: "MSG91 template for other messages. Its variable receives the whole text."},
		{Key: "message_variable", Label: "Message variable", Help: "Default message."},
	},
}

type msg91 struct {
	base
	creds, config map[string]string
}

func (g *msg91) Send(ctx context.Context, m Outgoing) (Accepted, error) {
	recipient := map[string]string{"mobiles": digits(m.To)}
	var template string
	if m.Purpose == "otp" && g.config["otp_template_id"] != "" && m.Vars["code"] != "" {
		template = g.config["otp_template_id"]
		recipient[or(g.config["otp_variable"], "OTP")] = m.Vars["code"]
	} else if g.config["message_template_id"] != "" {
		template = g.config["message_template_id"]
		recipient[or(g.config["message_variable"], "message")] = m.Body
	} else {
		return Accepted{}, &Error{Kind: MSG91, Code: "msg91_no_template",
			Message: "No MSG91 template fits this message. Add a message template ID to send messages other than one-time passwords."}
	}
	body := map[string]any{"template_id": template, "short_url": "0", "recipients": []map[string]string{recipient}}
	if s := g.config["sender_id"]; s != "" {
		body["sender"] = s
	}
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, g.url+"/api/v5/flow", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("authkey", g.creds["auth_key"])
	var out struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}
	status, err := g.do(req, &out)
	if err != nil {
		return Accepted{}, err
	}
	if status >= 300 || out.Type != "success" || out.Message == "" {
		return Accepted{}, &Error{Kind: MSG91, Code: "msg91_" + strconv.Itoa(status), Message: or(clip(out.Message, 300), "MSG91 refused the message.")}
	}
	// On success MSG91 returns the request ID in "message"; delivery reports refer to it.
	return Accepted{ExternalID: out.Message}, nil
}

func (g *msg91) Check(ctx context.Context) (string, error) {
	// Best effort: MSG91's balance endpoint answers a bare number for a valid key.
	q := url.Values{"authkey": {g.creds["auth_key"]}, "type": {"4"}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, g.url+"/api/balance.php?"+q.Encode(), nil)
	res, err := g.http.Do(req)
	if err != nil {
		return "", &Error{Kind: MSG91, Retryable: true, Code: "msg91_unreachable", Message: "Could not reach MSG91: " + err.Error()}
	}
	defer res.Body.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(res.Body)
	text := strings.TrimSpace(buf.String())
	if _, err := strconv.ParseFloat(text, 64); err == nil && res.StatusCode < 300 {
		return "Transactional balance " + text + ".", nil
	}
	if strings.Contains(strings.ToLower(text), "auth") {
		return "", &Error{Kind: MSG91, Code: "msg91_auth", Message: "MSG91 rejected the auth key."}
	}
	return "MSG91 answered, but the key could not be confirmed. Send a test message to be sure.", nil
}
