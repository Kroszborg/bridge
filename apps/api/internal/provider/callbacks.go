package provider

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ParseCallback reads a delivery report. Reports Bridge does not act on (such
// as "queued") produce no updates.
func ParseCallback(kind Kind, r *http.Request) ([]Update, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	switch kind {
	case Twilio:
		v := params(r, raw)
		return one(v.Get("MessageSid"), twilioStatus(v.Get("MessageStatus")), "twilio_"+v.Get("ErrorCode"), ""), nil
	case Vonage:
		v := params(r, raw)
		return one(v.Get("messageId"), vonageStatus(v.Get("status")), "vonage_"+v.Get("err-code"), ""), nil
	case Plivo:
		v := params(r, raw)
		return one(v.Get("MessageUUID"), plivoStatus(v.Get("Status")), "plivo_"+v.Get("ErrorCode"), ""), nil
	case MSG91:
		return msg91Updates(raw), nil
	}
	return nil, nil
}

// params merges the query string with a form or flat JSON body.
func params(r *http.Request, raw []byte) url.Values {
	v := r.URL.Query()
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var m map[string]any
		if json.Unmarshal(raw, &m) == nil {
			for k, val := range m {
				if s, ok := val.(string); ok {
					v.Set(k, s)
				}
			}
		}
		return v
	}
	if form, err := url.ParseQuery(string(raw)); err == nil {
		for k, vals := range form {
			v[k] = vals
		}
	}
	return v
}

func one(id string, st Status, code, msg string) []Update {
	if id == "" || st == "" {
		return nil
	}
	u := Update{ExternalID: id, Status: st}
	if st == StatusFailed {
		u.ErrorCode = strings.TrimSuffix(code, "_")
		u.ErrorMessage = or(msg, "The provider reported that the message was not delivered.")
	}
	return []Update{u}
}

func twilioStatus(s string) Status {
	switch s {
	case "sent":
		return StatusSent
	case "delivered":
		return StatusDelivered
	case "undelivered", "failed":
		return StatusFailed
	}
	return ""
}

func vonageStatus(s string) Status {
	switch s {
	case "delivered":
		return StatusDelivered
	case "failed", "rejected", "expired":
		return StatusFailed
	}
	return ""
}

func plivoStatus(s string) Status {
	switch s {
	case "sent":
		return StatusSent
	case "delivered":
		return StatusDelivered
	case "failed", "undelivered", "rejected":
		return StatusFailed
	}
	return ""
}

// msg91Updates accepts one report, a list, or a list under "data".
func msg91Updates(raw []byte) []Update {
	type report struct {
		RequestID     string `json:"requestId"`
		Status        any    `json:"status"`
		FailureReason string `json:"failureReason"`
	}
	var list []report
	if json.Unmarshal(raw, &list) != nil {
		var wrapped struct {
			Data []report `json:"data"`
		}
		if json.Unmarshal(raw, &wrapped) == nil && len(wrapped.Data) > 0 {
			list = wrapped.Data
		} else {
			var single report
			if json.Unmarshal(raw, &single) == nil {
				list = []report{single}
			}
		}
	}
	var out []Update
	for _, r := range list {
		code := strings.TrimSpace(strings.Trim(jsonString(r.Status), `"`))
		var st Status
		switch code {
		case "1":
			st = StatusDelivered
		case "2", "9", "16", "17", "20", "25":
			st = StatusFailed
		}
		out = append(out, one(r.RequestID, st, "msg91_"+code, r.FailureReason)...)
	}
	return out
}

func jsonString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		b, _ := json.Marshal(t)
		return string(b)
	}
	return ""
}
