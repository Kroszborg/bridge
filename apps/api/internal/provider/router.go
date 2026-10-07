package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"bridge/internal/db/dbq"
	"bridge/internal/secretbox"
)

// Account is a ready-to-use provider account of a project.
type Account struct {
	ID          string
	Kind        Kind
	Client      Client
	CallbackURL string // empty for providers whose reports are configured in their dashboard
}

// Router loads a project's provider accounts and builds their clients.
type Router struct {
	q         *dbq.Queries
	box       *secretbox.Box
	publicURL string
	http      *http.Client
	baseURLs  map[Kind]string
}

// RouterOptions: BaseURLs and HTTP are for tests.
type RouterOptions struct {
	PublicURL string
	HTTP      *http.Client
	BaseURLs  map[Kind]string
}

func NewRouter(q *dbq.Queries, box *secretbox.Box, o RouterOptions) *Router {
	return &Router{q: q, box: box, publicURL: strings.TrimRight(o.PublicURL, "/"), http: o.HTTP, baseURLs: o.BaseURLs}
}

// Ready reports whether credentials can be stored and read (BRIDGE_SECRET_KEY is set).
func (r *Router) Ready() bool { return r.box.Ready() }

// Accounts returns the project's enabled accounts in priority order. An
// account whose credentials cannot be read is skipped and its error recorded.
func (r *Router) Accounts(ctx context.Context, projectID string) ([]Account, error) {
	rows, err := r.q.EnabledProviderAccounts(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]Account, 0, len(rows))
	for _, row := range rows {
		c, err := r.Client(row)
		if err != nil {
			r.Record(ctx, row.ID, err)
			continue
		}
		a := Account{ID: row.ID, Kind: Kind(row.Kind), Client: c}
		if spec, _ := SpecFor(a.Kind); spec.Callbacks == "per_message" {
			a.CallbackURL = r.CallbackURL(row)
		}
		out = append(out, a)
	}
	return out, nil
}

// Client decrypts an account's credentials and builds its client.
func (r *Router) Client(row dbq.ProviderAccount) (Client, error) {
	plain, err := r.box.Open(row.ID, row.Credentials)
	if err != nil {
		return nil, err
	}
	var creds, config map[string]string
	if err := json.Unmarshal(plain, &creds); err != nil {
		return nil, errors.New("stored credentials are unreadable")
	}
	_ = json.Unmarshal(row.Config, &config)
	return New(Kind(row.Kind), creds, config, Options{HTTP: r.http, BaseURL: r.baseURLs[Kind(row.Kind)]})
}

// CallbackURL is where an account's delivery reports go.
func (r *Router) CallbackURL(row dbq.ProviderAccount) string {
	return r.publicURL + "/v1/provider-callbacks/" + row.ID + "/" + row.CallbackToken
}

// Seal encrypts credentials for an account row.
func (r *Router) Seal(accountID string, creds map[string]string) ([]byte, error) {
	plain, err := json.Marshal(creds)
	if err != nil {
		return nil, err
	}
	return r.box.Seal(accountID, plain)
}

// SealBytes and OpenBytes encrypt other secrets bound to a row, such as an
// integration's signing secret.
func (r *Router) SealBytes(rowID string, plain []byte) ([]byte, error) {
	return r.box.Seal(rowID, plain)
}

func (r *Router) OpenBytes(rowID string, sealed []byte) ([]byte, error) {
	return r.box.Open(rowID, sealed)
}

// Open decrypts an account's credentials.
func (r *Router) Open(row dbq.ProviderAccount) (map[string]string, error) {
	plain, err := r.box.Open(row.ID, row.Credentials)
	if err != nil {
		return nil, err
	}
	var creds map[string]string
	return creds, json.Unmarshal(plain, &creds)
}

// Record notes the outcome of using an account, for the dashboard.
func (r *Router) Record(ctx context.Context, accountID string, err error) {
	var msg *string
	if err != nil {
		s := err.Error()
		var pe *Error
		if errors.As(err, &pe) {
			s = pe.Message
		}
		s = clip(s, 300)
		msg = &s
	}
	_ = r.q.RecordProviderResult(ctx, dbq.RecordProviderResultParams{ID: accountID, Error: msg})
}
