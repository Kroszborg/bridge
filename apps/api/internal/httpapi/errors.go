package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// Error codes are part of the public API contract. Add new codes; never
// change the meaning of an existing one.
const (
	CodeInvalidRequest          = "invalid_request"
	CodeValidationFailed        = "validation_failed"
	CodeUnauthenticated         = "unauthenticated"
	CodeInvalidAPIKey           = "invalid_api_key"
	CodeInvalidDeviceCredential = "invalid_device_credential"
	CodeDeviceRevoked           = "device_revoked"
	CodeInvalidPairingToken     = "invalid_pairing_token"
	CodeForbidden               = "forbidden"
	CodeNotFound                = "not_found"
	CodeConflict                = "conflict"
	CodeRateLimited             = "rate_limited"
	CodeOTPBlocked              = "otp_blocked"
	CodeOptedOut                = "opted_out"
	CodePlanLimitReached        = "plan_limit_reached"
	// Account verification (email and phone codes).
	CodeAlreadyVerified              = "already_verified"
	CodeInvalidCode                  = "invalid_code"
	CodeCodeExpired                  = "code_expired"
	CodePhoneInUse                   = "phone_in_use"
	CodeEmailInUse                   = "email_in_use"
	CodeEmailVerificationUnavailable = "email_verification_unavailable"
	CodePhoneVerificationUnavailable = "phone_verification_unavailable"
	// Abuse controls on the account forms (sign-up, sign-in, password reset).
	CodeCaptchaRequired    = "captcha_required"
	CodeCaptchaFailed      = "captcha_failed"
	CodeCaptchaUnavailable = "captcha_unavailable"
	CodeEmailNotAllowed    = "email_not_allowed"
	CodeInternal           = "internal_error"
	CodeUnavailable        = "service_unavailable"
)

// ErrorBody is the JSON shape of every error response.
type ErrorBody struct {
	Code      string              `json:"code" doc:"Stable, machine-readable error code." example:"invalid_request"`
	Message   string              `json:"message" doc:"Human-readable explanation, safe to show to developers." example:"The request body is not valid JSON."`
	Details   []*huma.ErrorDetail `json:"details,omitempty" doc:"Per-field problems for validation errors."`
	RequestID string              `json:"request_id,omitempty" doc:"Include this when contacting support or searching logs." example:"req_01j9tq4m2xk3v8c7e5r2n0w6yb"`
}

// APIError is returned by handlers and rendered as {"error": {...}}.
type APIError struct {
	status int
	Body   ErrorBody `json:"error"`
}

func (e *APIError) Error() string  { return e.Body.Message }
func (e *APIError) GetStatus() int { return e.status }

// Errorf builds an APIError with an explicit code.
func Errorf(status int, code, message string) *APIError {
	return &APIError{status: status, Body: ErrorBody{Code: code, Message: message}}
}

func notFound(what string) *APIError {
	return Errorf(http.StatusNotFound, CodeNotFound, what+" was not found, or you do not have access to it.")
}

func rateLimited(retryAfter time.Duration) error {
	secs := int(retryAfter.Seconds())
	if secs < 1 {
		secs = 1
	}
	err := Errorf(http.StatusTooManyRequests, CodeRateLimited,
		"Too many requests. Retry after "+strconv.Itoa(secs)+" seconds.")
	return huma.ErrorWithHeaders(err, http.Header{"Retry-After": {strconv.Itoa(secs)}})
}

func codeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnsupportedMediaType, http.StatusNotAcceptable:
		return CodeInvalidRequest
	case http.StatusUnprocessableEntity:
		return CodeValidationFailed
	case http.StatusUnauthorized:
		return CodeUnauthenticated
	case http.StatusForbidden:
		return CodeForbidden
	case http.StatusNotFound:
		return CodeNotFound
	case http.StatusConflict:
		return CodeConflict
	case http.StatusTooManyRequests:
		return CodeRateLimited
	case http.StatusServiceUnavailable:
		return CodeUnavailable
	}
	if status >= 500 {
		return CodeInternal
	}
	return CodeInvalidRequest
}

// installErrorModel replaces Huma's default RFC 9457 errors with Bridge's
// envelope. Server errors never expose internal details to clients; the
// cause is logged with the request ID instead.
func installErrorModel(logger *slog.Logger) {
	build := func(requestID string, status int, msg string, errs ...error) huma.StatusError {
		if status >= 500 {
			logger.Error("request failed", "request_id", requestID, "status", status, "error", errors.Join(errs...))
			return &APIError{status: status, Body: ErrorBody{
				Code:      CodeInternal,
				Message:   "Bridge hit an unexpected error. It has been logged; retry the request or contact the operator with the request ID.",
				RequestID: requestID,
			}}
		}
		var details []*huma.ErrorDetail
		for _, err := range errs {
			if err == nil {
				continue
			}
			if d, ok := err.(huma.ErrorDetailer); ok {
				details = append(details, d.ErrorDetail())
			} else {
				details = append(details, &huma.ErrorDetail{Message: err.Error()})
			}
		}
		if status == http.StatusUnprocessableEntity && msg == "validation failed" {
			msg = "The request did not pass validation. See details for each field."
		}
		return &APIError{status: status, Body: ErrorBody{Code: codeForStatus(status), Message: msg, Details: details, RequestID: requestID}}
	}
	huma.NewError = func(status int, msg string, errs ...error) huma.StatusError {
		return build("", status, msg, errs...)
	}
	huma.NewErrorWithContext = func(ctx huma.Context, status int, msg string, errs ...error) huma.StatusError {
		return build(RequestIDFrom(ctx.Context()), status, msg, errs...)
	}
}

// requestIDTransformer stamps the request ID onto error bodies returned
// directly by handlers.
func requestIDTransformer(ctx huma.Context, _ string, v any) (any, error) {
	if e, ok := v.(*APIError); ok && e.Body.RequestID == "" {
		e.Body.RequestID = RequestIDFrom(ctx.Context())
	}
	return v, nil
}

// writeRawError renders an error outside of Huma (router-level middleware).
func writeRawError(w http.ResponseWriter, r *http.Request, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(APIError{Body: ErrorBody{Code: code, Message: msg, RequestID: RequestIDFrom(r.Context())}})
}
