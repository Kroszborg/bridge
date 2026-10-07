package httpapi

import (
	"context"
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

type HeartbeatPolicy struct {
	MinSeconds       int `json:"min_seconds"`
	MaxSeconds       int `json:"max_seconds"`
	ChargingSeconds  int `json:"charging_seconds" doc:"Suggested interval while charging."`
	OnBatterySeconds int `json:"on_battery_seconds" doc:"Suggested interval on battery."`
}

type UnifiedPushConfig struct {
	VAPIDPublicKey string `json:"vapid_public_key" doc:"Pass to the UnifiedPush distributor when registering."`
}

type FCMClientConfig struct {
	ProjectID string `json:"project_id"`
	AppID     string `json:"app_id"`
	APIKey    string `json:"api_key"`
	SenderID  string `json:"sender_id"`
}

type PushConfig struct {
	UnifiedPush UnifiedPushConfig `json:"unifiedpush"`
	FCM         *FCMClientConfig  `json:"fcm,omitempty" doc:"Present only when this server can send Firebase messages."`
}

type PairResponse struct {
	DeviceID     string          `json:"device_id"`
	ProjectID    string          `json:"project_id"`
	ProjectName  string          `json:"project_name"`
	Credential   string          `json:"credential" doc:"Long-lived device credential. Store it in the Android Keystore; it is shown once."`
	APIURL       string          `json:"api_url"`
	WebSocketURL string          `json:"websocket_url"`
	Heartbeat    HeartbeatPolicy `json:"heartbeat"`
	Push         PushConfig      `json:"push"`
}

type pairInput struct {
	Body struct {
		Token          string `json:"token" minLength:"10" maxLength:"128"`
		InstallationID string `json:"installation_id" pattern:"^[A-Za-z0-9-]{8,64}$" doc:"Random ID generated once per app install. Never a hardware identifier."`
		Name           string `json:"name,omitempty" maxLength:"80"`
		DeviceModel    string `json:"device_model,omitempty" maxLength:"80"`
		AndroidVersion string `json:"android_version,omitempty" maxLength:"32"`
		AppVersion     string `json:"app_version,omitempty" maxLength:"32"`
		AppFlavor      string `json:"app_flavor,omitempty" enum:"foss,gms"`
	}
}

type deviceHeartbeatInput struct {
	Body struct {
		NextIn int             `json:"next_in" minimum:"0" maximum:"86400" doc:"Seconds until the device's next check-in."`
		Status *gateway.Status `json:"status,omitempty"`
	}
}

type DeviceHeartbeatResponse struct {
	ServerTime     time.Time `json:"server_time"`
	PendingJobs    int       `json:"pending_jobs" doc:"Work waiting for this device. Connect the WebSocket to receive it."`
	ForwardInbound bool      `json:"forward_inbound" doc:"Whether to forward received SMS."`
}

type registerPushInput struct {
	Body struct {
		Provider string `json:"provider" enum:"unifiedpush,fcm"`
		Endpoint string `json:"endpoint" minLength:"1" maxLength:"2048" doc:"UnifiedPush endpoint URL, or the FCM registration token."`
		P256dh   string `json:"p256dh,omitempty" maxLength:"128" doc:"UnifiedPush only: receiver public key (base64url)."`
		Auth     string `json:"auth,omitempty" maxLength:"64" doc:"UnifiedPush only: auth secret (base64url)."`
	}
}

type SelfDevice struct {
	Device
	ProjectName string `json:"project_name"`
}

func (s *Server) registerDeviceSelf(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "pairDevice", Method: http.MethodPost, Path: "/v1/device/pair", Tags: []string{"Gateway"},
		Summary:     "Pair a device",
		Description: "Exchanges a pairing token from the dashboard for a device credential. Pairing the same installation again rotates its credential.",
		Errors:      []int{http.StatusUnauthorized, http.StatusTooManyRequests},
	}, s.pairDevice)

	huma.Register(api, huma.Operation{
		OperationID: "getSelfDevice", Method: http.MethodGet, Path: "/v1/device", Tags: []string{"Gateway"},
		Summary: "Get this device", Security: deviceAuth, Errors: []int{http.StatusUnauthorized},
	}, func(ctx context.Context, _ *struct{}) (*struct{ Body SelfDevice }, error) {
		d := principalFrom(ctx).Device
		return &struct{ Body SelfDevice }{Body: SelfDevice{Device: toDevice(d.Device), ProjectName: d.ProjectName}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "deviceHeartbeat", Method: http.MethodPost, Path: "/v1/device/heartbeat", Tags: []string{"Gateway"},
		Summary:     "Check in over HTTP",
		Description: "Fallback for background refresh when the WebSocket is down. Reports status and returns how much work is waiting.",
		Security:    deviceAuth, Errors: []int{http.StatusUnauthorized},
	}, func(ctx context.Context, in *deviceHeartbeatInput) (*struct{ Body DeviceHeartbeatResponse }, error) {
		d := principalFrom(ctx).Device
		if err := gateway.RecordStatus(ctx, s.q, d.Device.ID, gateway.ClampHeartbeat(in.Body.NextIn), in.Body.Status); err != nil {
			return nil, err
		}
		return &struct{ Body DeviceHeartbeatResponse }{Body: DeviceHeartbeatResponse{ServerTime: time.Now().UTC(), ForwardInbound: d.Device.ForwardInbound}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "registerDevicePush", Method: http.MethodPut, Path: "/v1/device/push", Tags: []string{"Gateway"},
		Summary:     "Register for push wake-ups",
		Description: "Stores the device's UnifiedPush endpoint or FCM token so Bridge can wake it when its connection is down.",
		Security:    deviceAuth, DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusUnauthorized},
	}, func(ctx context.Context, in *registerPushInput) (*struct{}, error) {
		d := principalFrom(ctx).Device
		b := in.Body
		params := dbq.UpdateDevicePushParams{ID: d.Device.ID, PushProvider: &b.Provider, PushEndpoint: &b.Endpoint}
		if b.Provider == push.ProviderUnifiedPush {
			u, err := url.Parse(b.Endpoint)
			if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
				return nil, huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{Location: "body.endpoint", Message: "UnifiedPush endpoint must be an http(s) URL."})
			}
			if b.P256dh == "" || b.Auth == "" {
				return nil, huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{Location: "body.p256dh", Message: "UnifiedPush registrations need p256dh and auth keys for message encryption."})
			}
			params.PushP256dh, params.PushAuth = &b.P256dh, &b.Auth
		}
		return nil, s.q.UpdateDevicePush(ctx, params)
	})

	huma.Register(api, huma.Operation{
		OperationID: "unpairSelf", Method: http.MethodDelete, Path: "/v1/device", Tags: []string{"Gateway"},
		Summary:     "Unpair this device",
		Description: "Called by the app when the user disconnects it. Revokes the credential.",
		Security:    deviceAuth, DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusUnauthorized},
	}, func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		d := principalFrom(ctx).Device
		return nil, s.revokeDevice(ctx, d.OrganizationID, d.Device.ProjectID, d.Device.ID)
	})
}

func (s *Server) pairDevice(ctx context.Context, in *pairInput) (*struct{ Body PairResponse }, error) {
	if err := s.limit(ctx, "pair:ip:"+ClientIPFrom(ctx).String(), 20, time.Hour); err != nil {
		return nil, err
	}
	b := in.Body
	invalid := Errorf(http.StatusUnauthorized, CodeInvalidPairingToken,
		"This pairing code is invalid, expired or already used. Create a new one in the dashboard under Devices.")
	if !strings.HasPrefix(b.Token, auth.PairingPrefix) {
		return nil, invalid
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)

	tok, err := q.ClaimPairingToken(ctx, auth.HashToken(b.Token))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, invalid
	}
	if err != nil {
		return nil, err
	}
	project, err := q.GetProjectByID(ctx, tok.ProjectID)
	if err != nil {
		return nil, err
	}

	name := strings.TrimSpace(b.Name)
	if name == "" {
		name = strings.TrimSpace(b.DeviceModel)
	}
	if name == "" {
		name = "Android device"
	}
	credential := auth.NewDeviceCredential()
	opt := func(v string) *string {
		if v == "" {
			return nil
		}
		return &v
	}

	var device dbq.Device
	existing, err := q.GetDeviceByInstallation(ctx, dbq.GetDeviceByInstallationParams{ProjectID: tok.ProjectID, InstallationID: b.InstallationID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		device, err = q.CreateDevice(ctx, dbq.CreateDeviceParams{
			ID: id.New(id.Device), ProjectID: tok.ProjectID, Name: name, InstallationID: b.InstallationID,
			CredentialHash: auth.HashToken(credential), DeviceModel: opt(b.DeviceModel),
			AndroidVersion: opt(b.AndroidVersion), AppVersion: opt(b.AppVersion), AppFlavor: opt(b.AppFlavor),
		})
	case err == nil:
		device, err = q.RepairDevice(ctx, dbq.RepairDeviceParams{
			ID: existing.ID, CredentialHash: auth.HashToken(credential), Name: name, DeviceModel: opt(b.DeviceModel),
			AndroidVersion: opt(b.AndroidVersion), AppVersion: opt(b.AppVersion), AppFlavor: opt(b.AppFlavor),
		})
	}
	if err != nil {
		return nil, err
	}
	if err := q.MarkPairingTokenUsed(ctx, dbq.MarkPairingTokenUsedParams{ID: tok.ID, DeviceID: &device.ID}); err != nil {
		return nil, err
	}
	if err := s.audit(ctx, q, auditEntry{
		OrganizationID: project.OrganizationID, ProjectID: project.ID, Action: "device.paired", TargetType: "device", TargetID: device.ID,
		Metadata: map[string]any{"name": device.Name, "model": b.DeviceModel, "app_version": b.AppVersion, "repaired": existing.ID != ""},
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	// A re-paired device may still hold an old connection with the previous credential.
	if existing.ID != "" {
		_ = s.hub.Send(ctx, device.ID, gateway.Outbound{Type: gateway.TypeUnpaired, Reason: "credential rotated"})
	}
	s.log.Info("device paired", "request_id", RequestIDFrom(ctx), "device_id", device.ID, "project_id", project.ID)

	resp := PairResponse{
		DeviceID: device.ID, ProjectID: project.ID, ProjectName: project.Name, Credential: credential,
		APIURL: s.cfg.PublicURL.String(), WebSocketURL: websocketURL(s.cfg.PublicURL) + "/v1/device/connect",
		Heartbeat: HeartbeatPolicy{
			MinSeconds: int(gateway.MinHeartbeat.Seconds()), MaxSeconds: int(gateway.MaxHeartbeat.Seconds()),
			ChargingSeconds: 60, OnBatterySeconds: 300,
		},
		Push: PushConfig{UnifiedPush: UnifiedPushConfig{VAPIDPublicKey: s.push.VAPIDPublicKey()}},
	}
	if f := s.cfg.FCM; f != nil && s.push.FCMEnabled() {
		resp.Push.FCM = &FCMClientConfig{ProjectID: f.ProjectID, AppID: f.AppID, APIKey: f.APIKey, SenderID: f.SenderID}
	}
	return &struct{ Body PairResponse }{Body: resp}, nil
}

func websocketURL(u *url.URL) string {
	scheme := "ws"
	if u.Scheme == "https" {
		scheme = "wss"
	}
	return scheme + "://" + u.Host + u.Path
}
