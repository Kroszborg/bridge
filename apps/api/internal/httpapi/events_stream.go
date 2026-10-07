package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"bridge/internal/webhook"
)

const streamPingInterval = 15 * time.Second

// eventStream serves GET /v1/events/stream: the project's events, as
// Server-Sent Events, for the key's environment. Device events are sent to
// both environments. `bridgectl listen` and `messages tail` use it.
//
//	id: evt_…
//	event: message.delivered
//	data: {"type":"message.delivered","timestamp":"…","data":{…}}
func (s *Server) eventStream(w http.ResponseWriter, r *http.Request) {
	if s.events == nil {
		writeRawError(w, r, http.StatusServiceUnavailable, CodeUnavailable, "The event stream is not available on this server.")
		return
	}
	key, err := s.apiKeyFromHeader(r.Context(), r.Header.Get("Authorization"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	var types []string
	if raw := r.URL.Query().Get("types"); raw != "" {
		for _, t := range strings.Split(raw, ",") {
			t = strings.TrimSpace(t)
			if !webhook.ValidEventType(t) {
				writeRawError(w, r, http.StatusUnprocessableEntity, CodeValidationFailed,
					fmt.Sprintf("Unknown event type %q in types. Use %s.", t, strings.Join(webhook.EventTypes, ", ")))
				return
			}
			types = append(types, t)
		}
	}

	// Streams outlive the server's write timeout.
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no") // nginx: do not buffer
	w.WriteHeader(http.StatusOK)

	sub := s.events.Subscribe(key.ProjectID, string(key.Environment))
	defer s.events.Unsubscribe(sub)
	s.log.Info("event stream opened", "project_id", key.ProjectID, "api_key_id", key.ID, "environment", key.Environment)
	defer s.log.Info("event stream closed", "project_id", key.ProjectID, "api_key_id", key.ID)

	if _, err := fmt.Fprintf(w, "retry: 3000\n: connected to %s events for project %s\n\n", key.Environment, key.ProjectID); err != nil {
		return
	}
	_ = rc.Flush()

	ping := time.NewTicker(streamPingInterval)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			_ = rc.Flush()
		case ev, ok := <-sub.C:
			if !ok {
				return // dropped as too slow, or the server is shutting down
			}
			if len(types) > 0 && !slices.Contains(types, ev.Type) {
				continue
			}
			data, _ := json.Marshal(webhook.Envelope{Type: ev.Type, Timestamp: ev.Timestamp, Data: ev.Data})
			if _, err := fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", ev.ID, ev.Type, data); err != nil {
				return
			}
			_ = rc.Flush()
		}
	}
}

// writeErr writes an error from shared auth code outside Huma, keeping its
// status, code and headers (such as Retry-After).
func writeErr(w http.ResponseWriter, r *http.Request, err error) {
	var withHeaders interface{ GetHeaders() http.Header }
	if errors.As(err, &withHeaders) {
		for k, v := range withHeaders.GetHeaders() {
			w.Header()[k] = v
		}
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		writeRawError(w, r, apiErr.GetStatus(), apiErr.Body.Code, apiErr.Body.Message)
		return
	}
	s := http.StatusInternalServerError
	writeRawError(w, r, s, CodeInternal, "Bridge hit an unexpected error. It has been logged.")
}
