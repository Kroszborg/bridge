package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type recorded struct {
	Method, Path, Query, Body, Auth, AuthKey, ContentType string
}

// fake answers every request with status and body, recording the request.
func fake(t *testing.T, status int, body string) (*httptest.Server, *recorded) {
	t.Helper()
	rec := &recorded{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		user, pass, _ := r.BasicAuth()
		*rec = recorded{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Body: string(raw),
			Auth: user + ":" + pass, AuthKey: r.Header.Get("authkey"), ContentType: r.Header.Get("Content-Type")}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func client(t *testing.T, kind Kind, creds, config map[string]string, base string) Client {
	t.Helper()
	c, err := New(kind, creds, config, Options{BaseURL: base})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

var msg = Outgoing{MessageID: "msg_1", To: "+919876543210", Body: "482913 is your Acme code.", Purpose: "otp",
	Vars: map[string]string{"code": "482913"}, CallbackURL: "https://bridge.example/cb"}

func TestTwilio(t *testing.T) {
	srv, rec := fake(t, 201, `{"sid":"SM123","status":"queued"}`)
	c := client(t, Twilio, map[string]string{"account_sid": "AC1", "auth_token": "tok"}, map[string]string{"from": "+15005550006"}, srv.URL)
	got, err := c.Send(context.Background(), msg)
	if err != nil || got.ExternalID != "SM123" {
		t.Fatalf("send = %+v, %v", got, err)
	}
	form, _ := url.ParseQuery(rec.Body)
	if rec.Path != "/2010-04-01/Accounts/AC1/Messages.json" || rec.Auth != "AC1:tok" || form.Get("To") != "+919876543210" ||
		form.Get("From") != "+15005550006" || form.Get("StatusCallback") != "https://bridge.example/cb" {
		t.Fatalf("request = %+v", rec)
	}

	srv, _ = fake(t, 400, `{"code":21211,"message":"Invalid 'To' Phone Number"}`)
	c = client(t, Twilio, map[string]string{"account_sid": "AC1", "auth_token": "tok"}, map[string]string{"messaging_service_sid": "MG1"}, srv.URL)
	_, err = c.Send(context.Background(), msg)
	var pe *Error
	if !errors.As(err, &pe) || pe.Retryable || pe.Code != "twilio_21211" {
		t.Fatalf("bad number: %v", err)
	}

	srv, _ = fake(t, 503, `{}`)
	c = client(t, Twilio, map[string]string{"account_sid": "AC1", "auth_token": "tok"}, map[string]string{"from": "+1"}, srv.URL)
	if _, err = c.Send(context.Background(), msg); !errors.As(err, &pe) || !pe.Retryable {
		t.Fatalf("503: %v", err)
	}
}

func TestVonage(t *testing.T) {
	srv, rec := fake(t, 200, `{"message-count":"1","messages":[{"message-id":"V1","status":"0"}]}`)
	c := client(t, Vonage, map[string]string{"api_key": "k", "api_secret": "s"}, map[string]string{"from": "Acme"}, srv.URL)
	got, err := c.Send(context.Background(), Outgoing{To: "+919876543210", Body: "नमस्ते", Unicode: true, CallbackURL: "https://cb"})
	if err != nil || got.ExternalID != "V1" {
		t.Fatalf("send = %+v, %v", got, err)
	}
	form, _ := url.ParseQuery(rec.Body)
	if form.Get("to") != "919876543210" || form.Get("type") != "unicode" || form.Get("callback") != "https://cb" {
		t.Fatalf("form = %v", form)
	}
	srv, _ = fake(t, 200, `{"messages":[{"status":"1","error-text":"Throttled"}]}`)
	c = client(t, Vonage, map[string]string{"api_key": "k", "api_secret": "s"}, map[string]string{"from": "Acme"}, srv.URL)
	var pe *Error
	if _, err = c.Send(context.Background(), msg); !errors.As(err, &pe) || !pe.Retryable || pe.Code != "vonage_1" {
		t.Fatalf("throttled: %v", err)
	}
}

func TestPlivo(t *testing.T) {
	srv, rec := fake(t, 202, `{"message_uuid":["P1"],"api_id":"x"}`)
	c := client(t, Plivo, map[string]string{"auth_id": "MA1", "auth_token": "t"}, map[string]string{"from": "+14155550100"}, srv.URL)
	got, err := c.Send(context.Background(), msg)
	if err != nil || got.ExternalID != "P1" {
		t.Fatalf("send = %+v, %v", got, err)
	}
	var body map[string]string
	_ = json.Unmarshal([]byte(rec.Body), &body)
	if rec.Path != "/v1/Account/MA1/Message/" || body["dst"] != "919876543210" || body["url"] != "https://bridge.example/cb" {
		t.Fatalf("request = %+v %v", rec, body)
	}
}

func TestMSG91(t *testing.T) {
	srv, rec := fake(t, 200, `{"type":"success","message":"3763646c3058373530393938"}`)
	c := client(t, MSG91, map[string]string{"auth_key": "key"},
		map[string]string{"otp_template_id": "tpl_otp", "message_template_id": "tpl_msg", "sender_id": "ACMEIN"}, srv.URL)
	got, err := c.Send(context.Background(), msg)
	if err != nil || got.ExternalID != "3763646c3058373530393938" {
		t.Fatalf("send = %+v, %v", got, err)
	}
	var body struct {
		TemplateID string              `json:"template_id"`
		Sender     string              `json:"sender"`
		Recipients []map[string]string `json:"recipients"`
	}
	_ = json.Unmarshal([]byte(rec.Body), &body)
	if rec.AuthKey != "key" || body.TemplateID != "tpl_otp" || body.Sender != "ACMEIN" ||
		body.Recipients[0]["mobiles"] != "919876543210" || body.Recipients[0]["OTP"] != "482913" {
		t.Fatalf("otp request = %s", rec.Body)
	}

	// Ordinary messages use the message template; without one MSG91 cannot send them.
	if _, err := c.Send(context.Background(), Outgoing{To: "+919876543210", Body: "Order shipped", Purpose: "message"}); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal([]byte(rec.Body), &body)
	if body.TemplateID != "tpl_msg" || body.Recipients[0]["message"] != "Order shipped" {
		t.Fatalf("message request = %s", rec.Body)
	}
	otpOnly := client(t, MSG91, map[string]string{"auth_key": "key"}, map[string]string{"otp_template_id": "tpl_otp"}, srv.URL)
	var pe *Error
	if _, err := otpOnly.Send(context.Background(), Outgoing{To: "+919876543210", Body: "hi", Purpose: "message"}); !errors.As(err, &pe) || pe.Code != "msg91_no_template" {
		t.Fatalf("no template: %v", err)
	}

	srv, _ = fake(t, 200, `{"type":"error","message":"Invalid template"}`)
	c = client(t, MSG91, map[string]string{"auth_key": "key"}, map[string]string{"otp_template_id": "x"}, srv.URL)
	if _, err := c.Send(context.Background(), msg); !errors.As(err, &pe) || pe.Retryable || !strings.Contains(pe.Message, "Invalid template") {
		t.Fatalf("error type: %v", err)
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		kind          Kind
		creds, config map[string]string
		field         string
	}{
		{Twilio, map[string]string{"account_sid": "AC1"}, map[string]string{"from": "+1"}, "credentials.auth_token"},
		{Twilio, map[string]string{"account_sid": "AC1", "auth_token": "t"}, map[string]string{}, "config.from"},
		{MSG91, map[string]string{"auth_key": "k"}, map[string]string{}, "config.otp_template_id"},
		{Vonage, map[string]string{"api_key": "k", "api_secret": "s", "extra": "x"}, map[string]string{"from": "A"}, "credentials.extra"},
		{"sinch", nil, nil, "kind"},
	}
	for _, tc := range cases {
		var fe *FieldError
		if err := Validate(tc.kind, tc.creds, tc.config); !errors.As(err, &fe) || fe.Field != tc.field {
			t.Errorf("%s: %v, want field %s", tc.kind, err, tc.field)
		}
	}
	if Hint(MSG91, map[string]string{"auth_key": "abcdef123456"}) != "…3456" {
		t.Error("msg91 hint")
	}
}

func TestCallbacks(t *testing.T) {
	post := func(contentType, body string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/cb", strings.NewReader(body))
		r.Header.Set("Content-Type", contentType)
		return r
	}
	form := "application/x-www-form-urlencoded"
	cases := []struct {
		kind Kind
		req  *http.Request
		want []Update
	}{
		{Twilio, post(form, "MessageSid=SM1&MessageStatus=delivered"), []Update{{ExternalID: "SM1", Status: StatusDelivered}}},
		{Twilio, post(form, "MessageSid=SM1&MessageStatus=undelivered&ErrorCode=30003"),
			[]Update{{ExternalID: "SM1", Status: StatusFailed, ErrorCode: "twilio_30003", ErrorMessage: "The provider reported that the message was not delivered."}}},
		{Twilio, post(form, "MessageSid=SM1&MessageStatus=queued"), nil},
		{Vonage, httptest.NewRequest(http.MethodGet, "/cb?messageId=V1&status=delivered", nil), []Update{{ExternalID: "V1", Status: StatusDelivered}}},
		{Plivo, post(form, "MessageUUID=P1&Status=sent"), []Update{{ExternalID: "P1", Status: StatusSent}}},
		{MSG91, post("application/json", `{"requestId":"R1","status":"1","telNum":"919876543210"}`), []Update{{ExternalID: "R1", Status: StatusDelivered}}},
		{MSG91, post("application/json", `[{"requestId":"R2","status":"17","failureReason":"Blocked number"}]`),
			[]Update{{ExternalID: "R2", Status: StatusFailed, ErrorCode: "msg91_17", ErrorMessage: "Blocked number"}}},
		{MSG91, post("application/json", `{"requestId":"R3","status":"0"}`), nil},
	}
	for i, tc := range cases {
		got, err := ParseCallback(tc.kind, tc.req)
		if err != nil {
			t.Fatal(err)
		}
		gb, _ := json.Marshal(got)
		wb, _ := json.Marshal(tc.want)
		if string(gb) != string(wb) {
			t.Errorf("case %d (%s): got %s, want %s", i, tc.kind, gb, wb)
		}
	}
}
