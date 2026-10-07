package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"bridge/internal/db/dbq"
	"bridge/internal/messaging"
	"bridge/internal/otp"
	"bridge/internal/secretbox"
)

// Verification is a one-time password sent to a number.
type Verification = otp.Verification

type VerificationList struct {
	Data    []Verification `json:"data"`
	HasMore bool           `json:"has_more" doc:"Pass the last verification's ID as starting_after to fetch the next page."`
}

type VerifyResult struct {
	Valid        bool         `json:"valid" doc:"true only when this request's code was right."`
	Verification Verification `json:"verification"`
}

// OTPSettingsInput is what the dashboard saves.
type OTPSettingsInput struct {
	AppName      *string `json:"app_name" nullable:"true" required:"false" maxLength:"40" doc:"Replaces {app}. Null uses the project name."`
	Template     *string `json:"template" nullable:"true" required:"false" maxLength:"300" doc:"The SMS text with {code}, and optionally {app} and {minutes}. Null uses the default."`
	CodeLength   int     `json:"code_length" minimum:"4" maximum:"10" default:"6"`
	TTLSeconds   int     `json:"ttl_seconds" minimum:"60" maximum:"3600" default:"600" doc:"How long a code stays valid."`
	MaxAttempts  int     `json:"max_attempts" minimum:"1" maximum:"10" default:"5" doc:"Wrong codes allowed before the verification fails."`
	WebOTPDomain *string `json:"web_otp_domain" nullable:"true" required:"false" maxLength:"253" doc:"Adds \"@domain #code\" as the last line so browsers can autofill the code (WebOTP)."`
}

// OTPSettings are a project's settings with the values they resolve to.
type OTPSettings struct {
	OTPSettingsInput
	DefaultTemplate  string `json:"default_template" readOnly:"true"`
	EffectiveAppName string `json:"effective_app_name" readOnly:"true"`
	Preview          string `json:"preview" readOnly:"true" doc:"The SMS these settings produce, with an example code."`
}

type otpSendBody struct {
	To             string         `json:"to" minLength:"3" maxLength:"32" example:"+919876543210" doc:"Destination in E.164 format."`
	App            string         `json:"app,omitempty" maxLength:"40" example:"default" doc:"The Verify app: its ID (vap_…) or slug. Leave out for the default app."`
	ClientIP       string         `json:"client_ip,omitempty" maxLength:"45" example:"203.0.113.7" doc:"Your end user's IP address. Enables the app's per-IP hourly limit."`
	AndroidAppHash string         `json:"android_app_hash,omitempty" pattern:"^[A-Za-z0-9+/]{11}$" doc:"Your Android app's 11-character SMS Retriever hash. Added as the last line so the app can read the code without SMS permission."`
	Metadata       map[string]any `json:"metadata,omitempty" doc:"Your own key-value data, returned with the verification. At most 32 keys and 4 KB."`
}

type otpVerifyBody struct {
	ID   string `json:"id,omitempty" pattern:"^otp_[0-9a-z]{26}$" doc:"The verification to check. Or pass to."`
	To   string `json:"to,omitempty" maxLength:"32" example:"+919876543210" doc:"Checks the latest pending code sent to this number. Or pass id."`
	App  string `json:"app,omitempty" maxLength:"40" doc:"Only match verifications of this Verify app (ID or slug). With to, picks among several apps' pending codes."`
	Code string `json:"code" minLength:"4" maxLength:"10" example:"482913"`
}

type OTPPath struct {
	OTPID string `path:"otpId" pattern:"^otp_[0-9a-z]{26}$" example:"otp_01ja8z3k5wq2v7c9e4r2n0w6yb"`
}

type ListOTPQuery struct {
	Limit         int    `query:"limit" minimum:"1" maximum:"100" default:"25"`
	App           string `query:"app" doc:"Filter by Verify app: its ID or slug."`
	StartingAfter string `query:"starting_after" doc:"A verification ID; returns verifications created before it."`
	Status        string `query:"status" enum:"pending,verified,expired,failed,canceled"`
	To            string `query:"to" doc:"Filter by number (E.164)."`
}

func (s *Server) registerOTP(api huma.API) {
	// ---- Developer API -------------------------------------------------
	huma.Register(api, huma.Operation{
		OperationID: "sendVerification", Method: http.MethodPost, Path: "/v1/otp", Tags: []string{"Developer API"},
		Summary: "Send a verification code",
		Description: "Generates a code, sends it by SMS and returns the verification. Sending a new code to the same number " +
			"cancels the previous one of the same app. One code per number every 30 seconds, and at most 5 per hour. " +
			"The app's fraud protection may refuse the code with `otp_blocked` (403 for a country that is not allowed, " +
			"429 for the hourly IP, number-range and country limits). " +
			"Test keys send nothing and return the code in `code`.",
		Security: apiKeyAuth, DefaultStatus: http.StatusCreated,
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusTooManyRequests},
	}, func(ctx context.Context, in *struct{ Body otpSendBody }) (*struct{ Body Verification }, error) {
		k := principalFrom(ctx).APIKey
		project, err := s.q.GetProjectByID(ctx, k.ProjectID)
		if err != nil {
			return nil, err
		}
		return s.sendOTP(ctx, otp.SendRequest{
			ProjectID: k.ProjectID, ProjectName: project.Name, Environment: k.Environment, APIKeyID: &k.ID,
			To: in.Body.To, AndroidAppHash: in.Body.AndroidAppHash, Metadata: in.Body.Metadata, App: in.Body.App,
		}, in.Body.ClientIP)
	})

	huma.Register(api, huma.Operation{
		OperationID: "checkVerification", Method: http.MethodPost, Path: "/v1/otp/verify", Tags: []string{"Developer API"},
		Summary: "Check a verification code",
		Description: "Returns `valid: true` when the code is right. A wrong code uses one attempt; when none are left the verification " +
			"fails and the user needs a new code. Pass `id`, or `to` to check the latest pending code for a number.",
		Security: apiKeyAuth, Errors: []int{http.StatusUnauthorized, http.StatusNotFound},
	}, func(ctx context.Context, in *struct{ Body otpVerifyBody }) (*struct{ Body VerifyResult }, error) {
		k := principalFrom(ctx).APIKey
		return s.verifyOTP(ctx, k.ProjectID, k.Environment, in.Body)
	})

	huma.Register(api, huma.Operation{
		OperationID: "getVerification", Method: http.MethodGet, Path: "/v1/otp/{otpId}", Tags: []string{"Developer API"},
		Summary: "Get a verification", Security: apiKeyAuth, Errors: []int{http.StatusUnauthorized, http.StatusNotFound},
	}, func(ctx context.Context, in *OTPPath) (*struct{ Body Verification }, error) {
		k := principalFrom(ctx).APIKey
		v, err := s.otp.Get(ctx, k.ProjectID, &k.Environment, in.OTPID)
		if err != nil {
			return nil, otpError(err)
		}
		return &struct{ Body Verification }{Body: v}, nil
	})

	// ---- Dashboard -----------------------------------------------------
	huma.Register(api, huma.Operation{
		OperationID: "listProjectVerifications", Method: http.MethodGet, Path: "/v1/projects/{projectId}/otp", Tags: []string{"Verify"},
		Summary: "List verifications", Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *struct {
		ProjectPath
		ListOTPQuery
		Environment string `query:"environment" enum:"live,test"`
	}) (*struct{ Body VerificationList }, error) {
		if _, err := s.projectForUser(ctx, in.ProjectID); err != nil {
			return nil, err
		}
		var env *dbq.APIEnvironment
		if in.Environment != "" {
			e := dbq.APIEnvironment(in.Environment)
			env = &e
		}
		appID, err := s.appFilter(ctx, in.ProjectID, in.App)
		if err != nil {
			return nil, err
		}
		items, more, err := s.otp.List(ctx, otp.ListRequest{
			ProjectID: in.ProjectID, AppID: deref(appID), Environment: env, Status: in.Status, To: in.To,
			StartingAfter: in.StartingAfter, Limit: in.Limit,
		})
		if err != nil {
			return nil, otpError(err)
		}
		return &struct{ Body VerificationList }{Body: VerificationList{Data: items, HasMore: more}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "getProjectVerificationStats", Method: http.MethodGet, Path: "/v1/projects/{projectId}/otp/stats", Tags: []string{"Verify"},
		Summary: "Verification statistics for the last 30 days", Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *struct {
		ProjectPath
		Environment string `query:"environment" enum:"live,test" default:"live"`
		App         string `query:"app" doc:"Only this Verify app: its ID or slug."`
	}) (*struct{ Body otp.VerificationStats }, error) {
		if _, err := s.projectForUser(ctx, in.ProjectID); err != nil {
			return nil, err
		}
		appID, err := s.appFilter(ctx, in.ProjectID, in.App)
		if err != nil {
			return nil, err
		}
		st, err := s.otp.Stats(ctx, in.ProjectID, appID, dbq.APIEnvironment(in.Environment), time.Now().Add(-30*24*time.Hour))
		if err != nil {
			return nil, err
		}
		return &struct{ Body otp.VerificationStats }{Body: st}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "getProjectOTPSettings", Method: http.MethodGet, Path: "/v1/projects/{projectId}/otp/settings", Tags: []string{"Verify"},
		Summary: "Get verification settings", Description: "The code settings of the project's default Verify app.",
		Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *ProjectPath) (*struct{ Body OTPSettings }, error) {
		p, err := s.projectForUser(ctx, in.ProjectID)
		if err != nil {
			return nil, err
		}
		st, err := s.otp.LoadSettings(ctx, in.ProjectID)
		if err != nil {
			return nil, err
		}
		return &struct{ Body OTPSettings }{Body: toOTPSettings(st, p.Name)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "updateProjectOTPSettings", Metadata: adminOnly, Method: http.MethodPut, Path: "/v1/projects/{projectId}/otp/settings", Tags: []string{"Verify"},
		Summary: "Update verification settings", Description: "Sets the code settings of the project's default Verify app; its other settings stay.",
		Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *struct {
		ProjectPath
		Body OTPSettingsInput
	}) (*struct{ Body OTPSettings }, error) {
		p, err := s.projectForUser(ctx, in.ProjectID)
		if err != nil {
			return nil, err
		}
		st, err := s.otp.SaveSettings(ctx, in.ProjectID, otp.Settings{
			AppName: deref(in.Body.AppName), Template: deref(in.Body.Template), CodeLength: in.Body.CodeLength,
			TTL: time.Duration(in.Body.TTLSeconds) * time.Second, MaxAttempts: in.Body.MaxAttempts,
			WebOTPDomain: deref(in.Body.WebOTPDomain),
		})
		if err != nil {
			return nil, otpError(err)
		}
		if err := s.audit(ctx, s.q, auditEntry{
			OrganizationID: p.OrganizationID, ProjectID: p.ID, Action: "otp.settings_updated", TargetType: "project", TargetID: p.ID,
			Metadata: map[string]any{
				"code_length": st.CodeLength, "ttl_seconds": int(st.TTL / time.Second), "max_attempts": st.MaxAttempts,
				"custom_template": st.Template != "", "web_otp_domain": st.WebOTPDomain,
			},
		}); err != nil {
			return nil, err
		}
		return &struct{ Body OTPSettings }{Body: toOTPSettings(st, p.Name)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "sendProjectVerification", Method: http.MethodPost, Path: "/v1/projects/{projectId}/otp", Tags: []string{"Verify"},
		Summary:     "Send a verification code from the dashboard",
		Description: "Same as `POST /v1/otp`, authenticated by the session, with the environment chosen per request.",
		Security:    sessionAuth, DefaultStatus: http.StatusCreated, Errors: []int{http.StatusForbidden, http.StatusNotFound, http.StatusTooManyRequests},
	}, func(ctx context.Context, in *struct {
		ProjectPath
		Environment string `query:"environment" enum:"live,test" default:"test"`
		Body        otpSendBody
	}) (*struct{ Body Verification }, error) {
		p, env, err := s.playgroundProject(ctx, in.ProjectID, in.Environment)
		if err != nil {
			return nil, err
		}
		return s.sendOTP(ctx, otp.SendRequest{
			ProjectID: in.ProjectID, ProjectName: p.Name, Environment: env, To: in.Body.To,
			AndroidAppHash: in.Body.AndroidAppHash, Metadata: in.Body.Metadata, App: in.Body.App,
		}, in.Body.ClientIP)
	})

	huma.Register(api, huma.Operation{
		OperationID: "checkProjectVerification", Method: http.MethodPost, Path: "/v1/projects/{projectId}/otp/verify", Tags: []string{"Verify"},
		Summary: "Check a verification code from the dashboard", Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *struct {
		ProjectPath
		Environment string `query:"environment" enum:"live,test" default:"test"`
		Body        otpVerifyBody
	}) (*struct{ Body VerifyResult }, error) {
		if _, err := s.projectForUser(ctx, in.ProjectID); err != nil {
			return nil, err
		}
		return s.verifyOTP(ctx, in.ProjectID, dbq.APIEnvironment(in.Environment), in.Body)
	})
}

// playgroundProject checks access for a dashboard send; live sends reach real
// people, so they need an admin.
func (s *Server) playgroundProject(ctx context.Context, projectID, environment string) (dbq.GetProjectForUserRow, dbq.APIEnvironment, error) {
	p, err := s.projectForUser(ctx, projectID)
	if err != nil {
		return p, "", err
	}
	if environment == "live" {
		if !hasRole(p.Role, dbq.MemberRoleAdmin) {
			return p, "", roleError(dbq.MemberRoleAdmin)
		}
		return p, dbq.ApiEnvironmentLive, nil
	}
	return p, dbq.ApiEnvironmentTest, nil
}

func (s *Server) sendOTP(ctx context.Context, req otp.SendRequest, clientIP string) (*struct{ Body Verification }, error) {
	if clientIP != "" {
		ip, err := netip.ParseAddr(clientIP)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{
				Location: "body.client_ip", Message: "Use an IPv4 or IPv6 address, such as 203.0.113.7.", Value: clientIP})
		}
		req.ClientIP = ip.Unmap()
	}
	v, err := s.otp.Send(ctx, req)
	if err != nil {
		return nil, otpError(err)
	}
	setResource(ctx, v.ID)
	return &struct{ Body Verification }{Body: v}, nil
}

func (s *Server) verifyOTP(ctx context.Context, projectID string, env dbq.APIEnvironment, b otpVerifyBody) (*struct{ Body VerifyResult }, error) {
	appID, err := s.appFilter(ctx, projectID, b.App)
	if err != nil {
		return nil, err
	}
	res, err := s.otp.Verify(ctx, otp.VerifyRequest{ProjectID: projectID, Environment: env, AppID: deref(appID), ID: b.ID, To: b.To, Code: b.Code})
	if err != nil {
		return nil, otpError(err)
	}
	setResource(ctx, res.Verification.ID)
	return &struct{ Body VerifyResult }{Body: VerifyResult{Valid: res.Valid, Verification: res.Verification}}, nil
}

func toOTPSettings(st otp.Settings, projectName string) OTPSettings {
	out := OTPSettings{
		CodeLength: st.CodeLength, TTLSeconds: int(st.TTL / time.Second), MaxAttempts: st.MaxAttempts,
		DefaultTemplate: otp.DefaultTemplate, EffectiveAppName: st.EffectiveAppName(projectName),
	}
	if st.AppName != "" {
		out.AppName = &st.AppName
	}
	if st.Template != "" {
		out.Template = &st.Template
	}
	if st.WebOTPDomain != "" {
		out.WebOTPDomain = &st.WebOTPDomain
	}
	example := "482913"[:min(6, st.CodeLength)] + "0123"[:max(0, st.CodeLength-6)]
	out.Preview = otp.Render(st.EffectiveTemplate(), out.EffectiveAppName, example, st.TTL, "", st.WebOTPDomain)
	return out
}

// appFilter resolves an optional app reference (ID or slug) to an app ID.
func (s *Server) appFilter(ctx context.Context, projectID, ref string) (*string, error) {
	if ref == "" {
		return nil, nil
	}
	app, err := s.otp.ResolveApp(ctx, projectID, ref)
	if err != nil {
		return nil, otpError(err)
	}
	return &app.ID, nil
}

func otpError(err error) error {
	var rl *messaging.RateLimitError
	var blocked *otp.BlockedError
	switch {
	case errors.Is(err, otp.ErrNotFound):
		return Errorf(http.StatusNotFound, CodeNotFound,
			"No matching verification. If you passed to, no code is pending for that number: send a new one.")
	case errors.Is(err, otp.ErrAppNotFound):
		return Errorf(http.StatusNotFound, CodeNotFound, "No Verify app with this ID or slug in the project.")
	case errors.As(err, &blocked):
		return blockedError(blocked)
	case errors.Is(err, otp.ErrCaptchaUnavailable):
		return Errorf(http.StatusServiceUnavailable, CodeUnavailable, "The CAPTCHA could not be checked right now. Try again in a moment.")
	case errors.Is(err, secretbox.ErrNoKey):
		return Errorf(http.StatusConflict, CodeConflict,
			"This server cannot store Verify secrets yet. Set BRIDGE_SECRET_KEY (openssl rand -base64 32) and restart Bridge.")
	case errors.Is(err, otp.ErrDefaultApp):
		return Errorf(http.StatusConflict, CodeConflict, "The default app cannot be deleted. Change its settings instead.")
	case errors.Is(err, otp.ErrTooManyApps):
		return Errorf(http.StatusConflict, CodeConflict, "A project can have at most 50 Verify apps. Delete one first.")
	case errors.As(err, &rl) && (rl.Scope == "otp_resend" || rl.Scope == "otp_destination"):
		return rateLimitedError(rl)
	}
	return messagingError(err)
}

// blockedError explains which fraud check refused a code.
func blockedError(b *otp.BlockedError) error {
	country := "this country's"
	if b.Block.Country != nil {
		country = *b.Block.Country
	}
	var status int
	var msg string
	switch b.Block.Reason {
	case otp.BlockCountryNotAllowed:
		status, msg = http.StatusForbidden, "This app does not send codes to "+country+" numbers. Allow the country in the app's fraud settings."
	case otp.BlockCaptchaFailed:
		status, msg = http.StatusForbidden, "The CAPTCHA check failed. Complete the challenge and try again."
	case otp.BlockIPLimit:
		status, msg = http.StatusTooManyRequests, "Too many codes were requested from this IP address in the last hour."
	case otp.BlockRangeBurst:
		status, msg = http.StatusTooManyRequests, "Too many codes were requested for numbers in this range in the last hour."
	default:
		status, msg = http.StatusTooManyRequests, "Too many codes were sent to "+country+" numbers in the last hour."
	}
	apiErr := Errorf(status, CodeOTPBlocked, msg)
	if status != http.StatusTooManyRequests {
		return apiErr
	}
	secs := max(1, int(b.RetryAfter.Seconds()))
	apiErr.Body.Message += " Retry after " + strconv.Itoa(secs) + " seconds."
	return huma.ErrorWithHeaders(apiErr, http.Header{"Retry-After": {strconv.Itoa(secs)}})
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
