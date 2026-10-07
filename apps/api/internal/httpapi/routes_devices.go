package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"

	"bridge/internal/auth"
	"bridge/internal/db/dbq"
	"bridge/internal/gateway"
	"bridge/internal/id"
	"bridge/internal/push"
)

const pairingTokenTTL = 10 * time.Minute

// Device is a paired Android gateway.
type Device struct {
	ID                       string      `json:"id" example:"dev_01j9tq4m2xk3v8c7e5r2n0w6yb"`
	ProjectID                string      `json:"project_id"`
	Name                     string      `json:"name" example:"Pixel 7"`
	Status                   string      `json:"status" enum:"online,offline,disabled" doc:"disabled means the device was removed and its credential revoked."`
	LastSeenAt               *time.Time  `json:"last_seen_at" nullable:"true"`
	LastHeartbeatAt          *time.Time  `json:"last_heartbeat_at" nullable:"true"`
	ConnectedAt              *time.Time  `json:"connected_at" nullable:"true"`
	HeartbeatIntervalSeconds int         `json:"heartbeat_interval_seconds" doc:"How often the device promised to check in."`
	BatteryLevel             *int16      `json:"battery_level" nullable:"true" minimum:"0" maximum:"100"`
	IsCharging               *bool       `json:"is_charging" nullable:"true"`
	NetworkType              *string     `json:"network_type" nullable:"true" example:"wifi"`
	CarrierName              *string     `json:"carrier_name" nullable:"true" example:"Jio"`
	SimCount                 *int16      `json:"sim_count" nullable:"true"`
	DeviceModel              *string     `json:"device_model" nullable:"true" example:"Google Pixel 7"`
	AndroidVersion           *string     `json:"android_version" nullable:"true" example:"15"`
	AppVersion               *string     `json:"app_version" nullable:"true" example:"0.1.0"`
	AppFlavor                *string     `json:"app_flavor" nullable:"true" enum:"foss,gms"`
	PushProvider             *string     `json:"push_provider" nullable:"true" enum:"unifiedpush,fcm" doc:"How Bridge wakes the app when its connection is down."`
	PreferredSimSlot         *int16      `json:"preferred_sim_slot" nullable:"true" doc:"SIM used for sending. Null means the phone's default SMS SIM."`
	Sims                     []DeviceSIM `json:"sims" doc:"SIMs the phone reports. Phone numbers are never collected."`
	SendLimitCount           int32       `json:"send_limit_count" doc:"Messages this phone may send per window. Android asks for approval above about 30 per 30 minutes."`
	SendLimitWindowSeconds   int32       `json:"send_limit_window_seconds"`
	ForwardInbound           bool        `json:"forward_inbound" doc:"Whether the phone forwards the SMS it receives to Bridge (message.received webhooks)."`
	RecentSends              int         `json:"recent_sends" doc:"Messages assigned to this phone in the current window."`
	TotalSent                int         `json:"total_sent"`
	TotalFailed              int         `json:"total_failed"`
	CreatedAt                time.Time   `json:"created_at"`
	RevokedAt                *time.Time  `json:"revoked_at" nullable:"true"`
}

type DeviceSIM struct {
	Slot        int16  `json:"slot"`
	Carrier     string `json:"carrier,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
}

type deviceCounts struct{ recent, sent, failed int }

func toDevice(d dbq.Device, counts ...deviceCounts) Device {
	push := d.PushProvider
	if d.PushEndpoint == nil {
		push = nil
	}
	sims := []DeviceSIM{}
	_ = json.Unmarshal(d.Sims, &sims)
	var c deviceCounts
	if len(counts) > 0 {
		c = counts[0]
	}
	return Device{
		PreferredSimSlot: d.PreferredSimSlot, Sims: sims, SendLimitCount: d.SendLimitCount,
		SendLimitWindowSeconds: d.SendLimitWindowSeconds, ForwardInbound: d.ForwardInbound, RecentSends: c.recent, TotalSent: c.sent, TotalFailed: c.failed,
		ID: d.ID, ProjectID: d.ProjectID, Name: d.Name, Status: string(d.Status),
		LastSeenAt: d.LastSeenAt, LastHeartbeatAt: d.LastHeartbeatAt, ConnectedAt: d.ConnectedAt,
		HeartbeatIntervalSeconds: int(d.HeartbeatInterval),
		BatteryLevel:             d.BatteryLevel, IsCharging: d.IsCharging, NetworkType: d.NetworkType,
		CarrierName: d.CarrierName, SimCount: d.SimCount, DeviceModel: d.DeviceModel,
		AndroidVersion: d.AndroidVersion, AppVersion: d.AppVersion, AppFlavor: d.AppFlavor,
		PushProvider: push, CreatedAt: d.CreatedAt, RevokedAt: d.RevokedAt,
	}
}

type PairingToken struct {
	ID         string    `json:"id"`
	Token      string    `json:"token" doc:"Single-use pairing token. Shown once."`
	ExpiresAt  time.Time `json:"expires_at"`
	APIURL     string    `json:"api_url" doc:"The API URL the device will connect to."`
	PairingURI string    `json:"pairing_uri" example:"bridge://pair?api=https%3A%2F%2Fapi.example.com&token=bp_…" doc:"Encode this as a QR code for the Bridge app to scan."`
}

type ProjectDevicePath struct {
	ProjectPath
	DeviceID string `path:"deviceId" pattern:"^dev_[0-9a-z]{26}$" example:"dev_01j9tq4m2xk3v8c7e5r2n0w6yb"`
}

type DevicePath struct {
	DeviceID string `path:"deviceId" pattern:"^dev_[0-9a-z]{26}$" example:"dev_01j9tq4m2xk3v8c7e5r2n0w6yb"`
}

type WakeResult struct {
	Via     string `json:"via" enum:"websocket,push,none"`
	Message string `json:"message"`
}

func (s *Server) registerDevices(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "createPairingToken", Metadata: adminOnly, Method: http.MethodPost, Path: "/v1/projects/{projectId}/pairing-tokens", Tags: []string{"Devices"},
		Summary:     "Create a pairing code",
		Description: "Returns a single-use token, valid for 10 minutes, that the Bridge Android app exchanges for a device credential.",
		Security:    sessionAuth, DefaultStatus: http.StatusCreated, Errors: []int{http.StatusNotFound, http.StatusTooManyRequests},
	}, func(ctx context.Context, in *ProjectPath) (*struct{ Body PairingToken }, error) {
		p, err := s.projectForUser(ctx, in.ProjectID)
		if err != nil {
			return nil, err
		}
		user := principalFrom(ctx).User
		if err := s.limit(ctx, "pairing_token:user:"+user.ID, 30, time.Hour); err != nil {
			return nil, err
		}
		token := auth.NewPairingToken()
		row, err := s.q.CreatePairingToken(ctx, dbq.CreatePairingTokenParams{
			ID: id.New(id.PairingToken), ProjectID: p.ID, TokenHash: auth.HashToken(token),
			CreatedBy: &user.ID, ExpiresAt: time.Now().Add(pairingTokenTTL),
		})
		if err != nil {
			return nil, err
		}
		apiURL := s.cfg.PublicURL.String()
		uri := "bridge://pair?" + url.Values{"api": {apiURL}, "token": {token}}.Encode()
		return &struct{ Body PairingToken }{Body: PairingToken{ID: row.ID, Token: token, ExpiresAt: row.ExpiresAt, APIURL: apiURL, PairingURI: uri}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "listProjectDevices", Method: http.MethodGet, Path: "/v1/projects/{projectId}/devices", Tags: []string{"Devices"},
		Summary: "List devices", Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *ProjectPath) (*struct{ Body ListResponse[Device] }, error) {
		if _, err := s.projectForUser(ctx, in.ProjectID); err != nil {
			return nil, err
		}
		return s.listDevices(ctx, in.ProjectID)
	})

	huma.Register(api, huma.Operation{
		OperationID: "getProjectDevice", Method: http.MethodGet, Path: "/v1/projects/{projectId}/devices/{deviceId}", Tags: []string{"Devices"},
		Summary: "Get a device", Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *ProjectDevicePath) (*struct{ Body Device }, error) {
		if _, err := s.projectForUser(ctx, in.ProjectID); err != nil {
			return nil, err
		}
		d, err := s.deviceInProject(ctx, in.ProjectID, in.DeviceID)
		if err != nil {
			return nil, err
		}
		return &struct{ Body Device }{Body: toDevice(d)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "updateDevice", Metadata: adminOnly, Method: http.MethodPatch, Path: "/v1/projects/{projectId}/devices/{deviceId}", Tags: []string{"Devices"},
		Summary: "Update a device", Description: "Rename it, choose the SIM it sends from, change its send limit, or turn incoming SMS forwarding on or off. Omitted fields stay unchanged.",
		Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *struct {
		ProjectDevicePath
		Body struct {
			Name             *string `json:"name,omitempty" minLength:"1" maxLength:"80"`
			PreferredSimSlot *int16  `json:"preferred_sim_slot,omitempty" minimum:"0" maximum:"2" doc:"1 or 2. 0 uses the phone's default SMS SIM."`
			SendLimitCount   *int32  `json:"send_limit_count,omitempty" minimum:"1" maximum:"10000" doc:"Raise only after lifting Android's limit on the phone (see the Android guide)."`
			ForwardInbound   *bool   `json:"forward_inbound,omitempty" doc:"Forward every SMS this phone receives to Bridge. Off by default; the phone also needs the Receive SMS permission."`
		}
	}) (*struct{ Body Device }, error) {
		p, err := s.projectForUser(ctx, in.ProjectID)
		if err != nil {
			return nil, err
		}
		before, err := s.deviceInProject(ctx, in.ProjectID, in.DeviceID)
		if err != nil {
			return nil, err
		}
		params := dbq.UpdateDeviceSettingsParams{
			ID: in.DeviceID, ProjectID: in.ProjectID, SendLimitCount: in.Body.SendLimitCount, ForwardInbound: in.Body.ForwardInbound,
		}
		if in.Body.Name != nil {
			name := strings.TrimSpace(*in.Body.Name)
			if name == "" {
				return nil, blankName()
			}
			params.Name = &name
		}
		if in.Body.PreferredSimSlot != nil {
			params.SetSim = true
			if *in.Body.PreferredSimSlot != 0 {
				params.PreferredSimSlot = in.Body.PreferredSimSlot
			}
		}
		d, err := s.q.UpdateDeviceSettings(ctx, params)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, notFound("Device " + in.DeviceID)
		}
		if err != nil {
			return nil, err
		}
		if in.Body.SendLimitCount != nil || in.Body.PreferredSimSlot != nil {
			s.msgs.KickProject(ctx, in.ProjectID)
		}
		if d.ForwardInbound != before.ForwardInbound {
			action := "device.inbound_forwarding_disabled"
			if d.ForwardInbound {
				action = "device.inbound_forwarding_enabled"
			}
			if err := s.audit(ctx, s.q, auditEntry{
				OrganizationID: p.OrganizationID, ProjectID: p.ID, Action: action, TargetType: "device", TargetID: d.ID,
			}); err != nil {
				return nil, err
			}
			if s.hub != nil {
				if err := s.hub.Send(ctx, d.ID, gateway.Outbound{Type: gateway.TypeConfig, ForwardInbound: &d.ForwardInbound}); err != nil {
					s.log.Warn("could not send settings to device", "device_id", d.ID, "error", err)
				}
			}
		}
		return &struct{ Body Device }{Body: toDevice(d)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "removeDevice", Metadata: adminOnly, Method: http.MethodDelete, Path: "/v1/projects/{projectId}/devices/{deviceId}", Tags: []string{"Devices"},
		Summary:     "Remove a device",
		Description: "Revokes the device credential and disconnects it. The phone can be paired again later.",
		Security:    sessionAuth, DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *ProjectDevicePath) (*struct{}, error) {
		p, err := s.projectForUser(ctx, in.ProjectID)
		if err != nil {
			return nil, err
		}
		return nil, s.revokeDevice(ctx, p.OrganizationID, p.ID, in.DeviceID)
	})

	huma.Register(api, huma.Operation{
		OperationID: "wakeDevice", Metadata: adminOnly, Method: http.MethodPost, Path: "/v1/projects/{projectId}/devices/{deviceId}/wake", Tags: []string{"Devices"},
		Summary:     "Wake a device",
		Description: "Asks the device to check in now: over its live connection if it has one, otherwise through its push registration.",
		Security:    sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *ProjectDevicePath) (*struct{ Body WakeResult }, error) {
		if _, err := s.projectForUser(ctx, in.ProjectID); err != nil {
			return nil, err
		}
		d, err := s.deviceInProject(ctx, in.ProjectID, in.DeviceID)
		if err != nil {
			return nil, err
		}
		res, err := s.wakeDevice(ctx, d, "manual")
		if err != nil {
			return nil, err
		}
		return &struct{ Body WakeResult }{Body: res}, nil
	})

	// Developer API: the same device data, scoped by the API key's project.
	huma.Register(api, huma.Operation{
		OperationID: "listDevices", Method: http.MethodGet, Path: "/v1/devices", Tags: []string{"Developer API"},
		Summary: "List devices", Description: "Devices paired to the API key's project.",
		Security: apiKeyAuth, Errors: []int{http.StatusUnauthorized},
	}, func(ctx context.Context, _ *struct{}) (*struct{ Body ListResponse[Device] }, error) {
		return s.listDevices(ctx, principalFrom(ctx).APIKey.ProjectID)
	})

	huma.Register(api, huma.Operation{
		OperationID: "getDevice", Method: http.MethodGet, Path: "/v1/devices/{deviceId}", Tags: []string{"Developer API"},
		Summary: "Get a device", Security: apiKeyAuth, Errors: []int{http.StatusUnauthorized, http.StatusNotFound},
	}, func(ctx context.Context, in *DevicePath) (*struct{ Body Device }, error) {
		d, err := s.deviceInProject(ctx, principalFrom(ctx).APIKey.ProjectID, in.DeviceID)
		if err != nil {
			return nil, err
		}
		return &struct{ Body Device }{Body: toDevice(d)}, nil
	})
}

func (s *Server) listDevices(ctx context.Context, projectID string) (*struct{ Body ListResponse[Device] }, error) {
	rows, err := s.q.ListDevices(ctx, projectID)
	if err != nil {
		return nil, err
	}
	counts, err := s.deviceCounts(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := &struct{ Body ListResponse[Device] }{Body: ListResponse[Device]{Data: make([]Device, 0, len(rows))}}
	for _, d := range rows {
		out.Body.Data = append(out.Body.Data, toDevice(d, counts[d.ID]))
	}
	return out, nil
}

func (s *Server) deviceCounts(ctx context.Context, projectID string) (map[string]deviceCounts, error) {
	rows, err := s.q.DeviceWindowCounts(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]deviceCounts, len(rows))
	for _, r := range rows {
		out[r.ID] = deviceCounts{recent: int(r.RecentSends), sent: int(r.TotalSent), failed: int(r.TotalFailed)}
	}
	return out, nil
}

func (s *Server) deviceInProject(ctx context.Context, projectID, deviceID string) (dbq.Device, error) {
	d, err := s.q.GetDevice(ctx, dbq.GetDeviceParams{ID: deviceID, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return d, notFound("Device " + deviceID)
	}
	return d, err
}

// revokeDevice removes a device's access and disconnects it wherever it is connected.
func (s *Server) revokeDevice(ctx context.Context, orgID, projectID, deviceID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)
	d, err := q.RevokeDevice(ctx, dbq.RevokeDeviceParams{ID: deviceID, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		if _, getErr := q.GetDevice(ctx, dbq.GetDeviceParams{ID: deviceID, ProjectID: projectID}); getErr == nil {
			return nil // already removed
		}
		return notFound("Device " + deviceID)
	}
	if err != nil {
		return err
	}
	if err := s.audit(ctx, q, auditEntry{
		OrganizationID: orgID, ProjectID: projectID, Action: "device.removed", TargetType: "device", TargetID: d.ID,
		Metadata: map[string]any{"name": d.Name},
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if err := s.hub.Send(ctx, d.ID, gateway.Outbound{Type: gateway.TypeUnpaired, Reason: "removed"}); err != nil {
		s.log.Warn("could not notify removed device", "device_id", d.ID, "error", err)
	}
	return nil
}

// wakeDevice asks a device to check in: over its WebSocket when connected,
// otherwise through push.
func (s *Server) wakeDevice(ctx context.Context, d dbq.Device, reason string) (WakeResult, error) {
	if d.RevokedAt != nil {
		return WakeResult{Via: "none", Message: "The device was removed. Pair it again first."}, nil
	}
	if d.Status == dbq.DeviceStatusOnline && d.ConnectionID != nil {
		if err := s.hub.Send(ctx, d.ID, gateway.Outbound{Type: gateway.TypeSync, Reason: reason}); err != nil {
			return WakeResult{}, err
		}
		return WakeResult{Via: "websocket", Message: "Sent over the device's live connection."}, nil
	}
	if d.PushProvider == nil || d.PushEndpoint == nil {
		return WakeResult{Via: "none", Message: "The device is offline and has no push registration. Open the Bridge app on the phone."}, nil
	}
	target := push.Target{Provider: *d.PushProvider, Endpoint: *d.PushEndpoint}
	if d.PushP256dh != nil && d.PushAuth != nil {
		target.P256dh, target.Auth = *d.PushP256dh, *d.PushAuth
	}
	err := s.push.Wake(ctx, target, reason)
	switch {
	case err == nil:
		return WakeResult{Via: "push", Message: "Push sent. The device should reconnect within seconds if it has network."}, nil
	case errors.Is(err, push.ErrGone), errors.Is(err, push.ErrInvalidTarget):
		_ = s.q.UpdateDevicePush(ctx, dbq.UpdateDevicePushParams{ID: d.ID})
		return WakeResult{Via: "none", Message: "The device's push registration expired. It re-registers the next time the app runs."}, nil
	case errors.Is(err, push.ErrNotConfigured):
		return WakeResult{Via: "none", Message: "This server has no Firebase credentials, so it cannot wake the Google build of the app."}, nil
	}
	s.log.Warn("push wake failed", "device_id", d.ID, "provider", target.Provider, "error", err)
	return WakeResult{Via: "none", Message: "The push service did not accept the wake-up. The device will reconnect on its next scheduled check."}, nil
}

// deviceConnect upgrades an authenticated device to the gateway WebSocket.
func (s *Server) deviceConnect(w http.ResponseWriter, r *http.Request) {
	d, err := s.authDevice(r.Context(), r.Header.Get("Authorization"))
	if err != nil {
		var ae *APIError
		if errors.As(err, &ae) {
			var he huma.HeadersError
			if errors.As(err, &he) {
				for k, v := range he.GetHeaders() {
					w.Header()[k] = v
				}
			}
			writeRawError(w, r, ae.GetStatus(), ae.Body.Code, ae.Body.Message)
			return
		}
		s.log.Error("device auth failed", "request_id", RequestIDFrom(r.Context()), "error", err)
		writeRawError(w, r, http.StatusInternalServerError, CodeInternal, "Bridge hit an unexpected error.")
		return
	}
	s.hub.Serve(w, r, d.Device)
}
