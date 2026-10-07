package otp

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/nyaruka/phonenumbers"

	"bridge/internal/db/dbq"
	"bridge/internal/id"
	"bridge/internal/messaging"
)

// Reasons a send attempt is blocked.
const (
	BlockCountryNotAllowed = "country_not_allowed"
	BlockIPLimit           = "ip_limit"
	BlockRangeBurst        = "range_burst"
	BlockCountryLimit      = "country_limit"
	BlockCaptchaFailed     = "captcha_failed"

	// BlockRetention is how long blocked attempts are kept.
	BlockRetention = 30 * 24 * time.Hour
)

// BlockedError is returned by Send when fraud protection refuses a code.
type BlockedError struct {
	Block      Block
	RetryAfter time.Duration // for the hourly limits
}

func (e *BlockedError) Error() string { return "verification blocked: " + e.Block.Reason }

// ErrCaptchaUnavailable means Turnstile could not be reached to check a token.
var ErrCaptchaUnavailable = errors.New("the CAPTCHA could not be checked")

// Block is a send attempt that fraud protection refused.
type Block struct {
	ID          string    `json:"id" example:"blk_01ja8z3k5wq2v7c9e4r2n0w6yb"`
	AppID       string    `json:"app_id" example:"vap_01ja8z3k5wq2v7c9e4r2n0w6yb"`
	Environment string    `json:"environment" enum:"live,test"`
	To          string    `json:"to" example:"+919876543210"`
	ClientIP    *string   `json:"client_ip" nullable:"true" doc:"The end user's IP address, when known."`
	Country     *string   `json:"country" nullable:"true" example:"IN" doc:"ISO 3166-1 alpha-2 country of the number."`
	Reason      string    `json:"reason" enum:"country_not_allowed,ip_limit,range_burst,country_limit,captcha_failed"`
	CreatedAt   time.Time `json:"created_at"`
}

func blockView(b dbq.OtpBlock) Block {
	out := Block{
		ID: b.ID, AppID: b.AppID, Environment: string(b.Environment), To: b.Recipient, Country: b.Country,
		Reason: b.Reason, CreatedAt: b.CreatedAt,
	}
	if b.ClientIp != nil {
		ip := b.ClientIp.String()
		out.ClientIP = &ip
	}
	return out
}

// CountryOf returns the ISO country of an E.164 number, or "".
func CountryOf(e164 string) string {
	num, err := phonenumbers.Parse(e164, "")
	if err != nil {
		return ""
	}
	if cc := phonenumbers.GetRegionCodeForNumber(num); cc != "" && cc != "ZZ" {
		return cc
	}
	return ""
}

// numberRange is the number without its last three digits, so sequential
// numbers (pumping a range of premium numbers) share a counter.
func numberRange(e164 string) string {
	if len(e164) <= 6 {
		return e164
	}
	return e164[:len(e164)-3]
}

// fraudCheck is one attempt to send a code.
type fraudCheck struct {
	app            dbq.VerifyApp
	env            dbq.APIEnvironment
	to             string
	clientIP       netip.Addr
	captcha        bool // the request must carry a valid Turnstile token
	turnstileToken string
}

// checkFraud applies the app's fraud protection. A refused attempt is stored
// and announced as otp.blocked, and returned as a *BlockedError.
func (s *Service) checkFraud(ctx context.Context, f fraudCheck) error {
	country := CountryOf(f.to)
	if f.captcha {
		secret, err := s.turnstileSecret(f.app)
		if err != nil {
			return err
		}
		ip := ""
		if f.clientIP.IsValid() {
			ip = f.clientIP.String()
		}
		res, err := s.turnstile.Verify(ctx, secret, f.turnstileToken, ip)
		if err != nil {
			s.log.Warn("turnstile check failed", "app_id", f.app.ID, "error", err)
			return ErrCaptchaUnavailable
		}
		if !res.Success {
			return s.block(ctx, f, country, BlockCaptchaFailed, 0)
		}
	}
	if len(f.app.AllowedCountries) > 0 && !slices.Contains(f.app.AllowedCountries, country) {
		return s.block(ctx, f, country, BlockCountryNotAllowed, 0)
	}
	scope := "otp:" + f.app.ID + ":" + string(f.env) + ":"
	if f.clientIP.IsValid() && f.app.IpHourlyLimit > 0 {
		if blocked, retry := s.overLimit(ctx, scope+"ip:"+f.clientIP.String(), int(f.app.IpHourlyLimit)); blocked {
			return s.block(ctx, f, country, BlockIPLimit, retry)
		}
	}
	if f.app.RangeHourlyLimit > 0 {
		if blocked, retry := s.overLimit(ctx, scope+"range:"+numberRange(f.to), int(f.app.RangeHourlyLimit)); blocked {
			return s.block(ctx, f, country, BlockRangeBurst, retry)
		}
	}
	if f.app.CountryHourlyLimit != nil && country != "" {
		if blocked, retry := s.overLimit(ctx, scope+"country:"+country, int(*f.app.CountryHourlyLimit)); blocked {
			return s.block(ctx, f, country, BlockCountryLimit, retry)
		}
	}
	return nil
}

func (s *Service) overLimit(ctx context.Context, key string, limit int) (bool, time.Duration) {
	res, err := s.limiter.Hit(ctx, key, limit, time.Hour)
	if err != nil {
		s.log.Warn("rate limiter unavailable; allowing code", "error", err)
		return false, 0
	}
	return !res.Allowed, res.RetryAfter
}

func (s *Service) block(ctx context.Context, f fraudCheck, country, reason string, retry time.Duration) error {
	params := dbq.InsertOTPBlockParams{
		ID: id.New(id.OTPBlock), ProjectID: f.app.ProjectID, AppID: f.app.ID, Environment: f.env,
		Recipient: f.to, Country: nonEmpty(country), Reason: reason,
	}
	if f.clientIP.IsValid() {
		ip := f.clientIP
		params.ClientIp = &ip
	}
	row, err := s.q.InsertOTPBlock(ctx, params)
	if err != nil {
		return err
	}
	b := blockView(row)
	s.log.Info("verification blocked", "app_id", f.app.ID, "project_id", f.app.ProjectID, "reason", reason, "country", country)
	if s.emitter != nil {
		if err := s.emitter.Emit(ctx, f.app.ProjectID, EventBlocked, b); err != nil {
			s.log.Warn("could not announce blocked verification", "block_id", b.ID, "error", err)
		}
	}
	return &BlockedError{Block: b, RetryAfter: retry}
}

// EventBlocked announces a blocked send attempt.
const EventBlocked = "otp.blocked"

// ListBlocksRequest pages through an app's blocked attempts, newest first.
type ListBlocksRequest struct {
	AppID         string
	Environment   *dbq.APIEnvironment
	Reason        string
	StartingAfter string
	Limit         int
}

func (s *Service) ListBlocks(ctx context.Context, r ListBlocksRequest) ([]Block, bool, error) {
	params := dbq.ListOTPBlocksParams{AppID: r.AppID, Environment: r.Environment, Reason: nonEmpty(r.Reason), RowLimit: int32(r.Limit + 1)}
	if r.StartingAfter != "" {
		cursor, err := s.q.GetOTPBlock(ctx, dbq.GetOTPBlockParams{ID: r.StartingAfter, AppID: r.AppID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, &messaging.ValidationError{Field: "starting_after", Message: "Unknown block ID."}
		}
		if err != nil {
			return nil, false, err
		}
		params.BeforeCreated, params.BeforeID = &cursor.CreatedAt, &cursor.ID
	}
	rows, err := s.q.ListOTPBlocks(ctx, params)
	if err != nil {
		return nil, false, err
	}
	more := len(rows) > r.Limit
	if more {
		rows = rows[:r.Limit]
	}
	out := make([]Block, len(rows))
	for i, row := range rows {
		out[i] = blockView(row)
	}
	return out, more, nil
}

// BlockStats counts blocked attempts by reason.
type BlockStats struct {
	Total             int `json:"total"`
	CountryNotAllowed int `json:"country_not_allowed"`
	IPLimit           int `json:"ip_limit"`
	RangeBurst        int `json:"range_burst"`
	CountryLimit      int `json:"country_limit"`
	CaptchaFailed     int `json:"captcha_failed"`
}

func (s *Service) blockStats(ctx context.Context, projectID string, appID *string, env dbq.APIEnvironment, since time.Time) (BlockStats, error) {
	rows, err := s.q.OTPBlockCounts(ctx, dbq.OTPBlockCountsParams{ProjectID: projectID, AppID: appID, Environment: env, Since: since})
	if err != nil {
		return BlockStats{}, err
	}
	var st BlockStats
	for _, r := range rows {
		n := int(r.Blocks)
		st.Total += n
		switch r.Reason {
		case BlockCountryNotAllowed:
			st.CountryNotAllowed = n
		case BlockIPLimit:
			st.IPLimit = n
		case BlockRangeBurst:
			st.RangeBurst = n
		case BlockCountryLimit:
			st.CountryLimit = n
		case BlockCaptchaFailed:
			st.CaptchaFailed = n
		}
	}
	return st, nil
}
