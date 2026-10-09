package httpapi

import (
	"time"

	"bridge/internal/db/dbq"
)

// Response models. These are the public JSON contract; field names are snake_case
// and never change meaning once released.

type User struct {
	ID            string    `json:"id" example:"usr_01j9tq4m2xk3v8c7e5r2n0w6yb"`
	Operator      bool      `json:"operator" doc:"Whether you operate this Bridge instance and can see System health."`
	Email         string    `json:"email" example:"ada@example.com"`
	Name          string    `json:"name" example:"Ada Lovelace"`
	EmailVerified bool      `json:"email_verified" doc:"Whether you proved the address with a code sent to it."`
	Phone         *string   `json:"phone" nullable:"true" example:"+919876543210" doc:"Your verified phone number in E.164 format; null until you verify one."`
	PhoneVerified bool      `json:"phone_verified" doc:"Whether you proved the phone number with a code sent to it."`
	CreatedAt     time.Time `json:"created_at"`
}

type Organization struct {
	ID        string    `json:"id" example:"org_01j9tq4m2xk3v8c7e5r2n0w6yb"`
	Name      string    `json:"name" example:"Acme"`
	Slug      string    `json:"slug" example:"acme"`
	Role      string    `json:"role" enum:"owner,admin,member" doc:"Your role in this organization."`
	CreatedAt time.Time `json:"created_at"`
}

type Project struct {
	ID             string    `json:"id" example:"prj_01j9tq4m2xk3v8c7e5r2n0w6yb"`
	OrganizationID string    `json:"organization_id"`
	Name           string    `json:"name" example:"Checkout"`
	Slug           string    `json:"slug" example:"checkout"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type APIKey struct {
	ID          string     `json:"id" example:"key_01j9tq4m2xk3v8c7e5r2n0w6yb"`
	ProjectID   string     `json:"project_id"`
	Name        string     `json:"name" example:"Production server"`
	Environment string     `json:"environment" enum:"live,test" doc:"Test keys never send real SMS."`
	Prefix      string     `json:"prefix" example:"bk_live_7Hq2Xc" doc:"The first characters of the key. Safe to display."`
	Status      string     `json:"status" enum:"active,revoked,expired"`
	CreatedAt   time.Time  `json:"created_at"`
	LastUsedAt  *time.Time `json:"last_used_at" nullable:"true"`
	ExpiresAt   *time.Time `json:"expires_at" nullable:"true"`
	RevokedAt   *time.Time `json:"revoked_at" nullable:"true"`
}

type CreatedAPIKey struct {
	APIKey
	Secret string `json:"secret" example:"bk_live_7Hq2XcR4…" doc:"The full secret key. Shown once; Bridge stores only a hash."`
}

type ListResponse[T any] struct {
	Data []T `json:"data"`
}

func toUser(u *dbq.User) User {
	out := User{ID: u.ID, Email: u.Email, Name: u.Name, EmailVerified: u.EmailVerifiedAt != nil, CreatedAt: u.CreatedAt}
	if u.Phone != nil && u.PhoneVerifiedAt != nil {
		out.Phone, out.PhoneVerified = u.Phone, true
	}
	return out
}

func toOrganization(id, name, slug string, role dbq.MemberRole, createdAt time.Time) Organization {
	return Organization{ID: id, Name: name, Slug: slug, Role: string(role), CreatedAt: createdAt}
}

func toProject(p dbq.Project) Project {
	return Project{ID: p.ID, OrganizationID: p.OrganizationID, Name: p.Name, Slug: p.Slug, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt}
}

func toAPIKey(k dbq.APIKey) APIKey {
	status := "active"
	switch {
	case k.RevokedAt != nil:
		status = "revoked"
	case k.ExpiresAt != nil && time.Now().After(*k.ExpiresAt):
		status = "expired"
	}
	return APIKey{
		ID: k.ID, ProjectID: k.ProjectID, Name: k.Name, Environment: string(k.Environment), Prefix: k.KeyPrefix,
		Status: status, CreatedAt: k.CreatedAt, LastUsedAt: k.LastUsedAt, ExpiresAt: k.ExpiresAt, RevokedAt: k.RevokedAt,
	}
}
