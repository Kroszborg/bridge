package automation

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"bridge/internal/db/dbq"
	"bridge/internal/id"
	"bridge/internal/messaging"
)

// Opt-out sources.
const (
	SourceKeyword = "keyword" // the person texted a keyword such as STOP
	SourceManual  = "manual"  // added in the dashboard
	SourceAPI     = "api"     // added with an API key
)

// LookupNumber is the form a number is stored in: E.164 when it parses, and
// otherwise the trimmed text (a sender that is not an international number).
func LookupNumber(raw string) string {
	if n, err := messaging.NormalizeE164(raw); err == nil {
		return n
	}
	return strings.TrimSpace(raw)
}

// AddOptOut puts a number on the project's opt-out list. created is false
// when it was already there; the existing entry is returned unchanged.
func (s *Service) AddOptOut(ctx context.Context, projectID, number, source string, keyword *string) (dbq.OptOut, bool, error) {
	o, err := s.q.InsertOptOut(ctx, dbq.InsertOptOutParams{
		ID: id.New(id.OptOut), ProjectID: projectID, Number: number, Source: source, Keyword: keyword,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		o, err = s.q.GetOptOut(ctx, dbq.GetOptOutParams{ProjectID: projectID, Number: number})
		return o, false, err
	}
	return o, err == nil, err
}

// RemoveOptOut takes a number off the list.
func (s *Service) RemoveOptOut(ctx context.Context, projectID, number string) (dbq.OptOut, error) {
	o, err := s.q.DeleteOptOut(ctx, dbq.DeleteOptOutParams{ProjectID: projectID, Number: number})
	if errors.Is(err, pgx.ErrNoRows) {
		return o, ErrNotFound
	}
	return o, err
}

// GetOptOut returns a number's entry, or ErrNotFound.
func (s *Service) GetOptOut(ctx context.Context, projectID, number string) (dbq.OptOut, error) {
	o, err := s.q.GetOptOut(ctx, dbq.GetOptOutParams{ProjectID: projectID, Number: number})
	if errors.Is(err, pgx.ErrNoRows) {
		return o, ErrNotFound
	}
	return o, err
}

// ListOptOutsRequest pages through a project's opt-outs, newest first.
type ListOptOutsRequest struct {
	ProjectID     string
	Source        string
	StartingAfter string // an opt-out ID
	Limit         int
}

func (s *Service) ListOptOuts(ctx context.Context, r ListOptOutsRequest) ([]dbq.OptOut, bool, error) {
	params := dbq.ListOptOutsParams{ProjectID: r.ProjectID, RowLimit: int32(r.Limit + 1)}
	if r.Source != "" {
		params.Source = &r.Source
	}
	if r.StartingAfter != "" {
		cursor, err := s.q.GetOptOutByID(ctx, dbq.GetOptOutByIDParams{ID: r.StartingAfter, ProjectID: r.ProjectID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, fieldErr("starting_after", "No opt-out with this ID in the project.")
		}
		if err != nil {
			return nil, false, err
		}
		params.BeforeCreated, params.BeforeID = &cursor.CreatedAt, &cursor.ID
	}
	rows, err := s.q.ListOptOuts(ctx, params)
	if err != nil {
		return nil, false, err
	}
	more := len(rows) > r.Limit
	if more {
		rows = rows[:r.Limit]
	}
	return rows, more, nil
}
