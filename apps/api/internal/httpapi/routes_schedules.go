package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"bridge/internal/db/dbq"
	"bridge/internal/messaging"
	"bridge/internal/schedule"
)

// ScheduleTiming is when a scheduled message is sent.
type ScheduleTiming struct {
	Kind       string   `json:"kind" enum:"once,daily,weekly,monthly"`
	At         string   `json:"at" example:"09:30" doc:"24-hour wall-clock time in time_zone."`
	Days       []string `json:"days" enum:"mon,tue,wed,thu,fri,sat,sun" doc:"weekly: the weekdays it is sent on."`
	DayOfMonth *int     `json:"day_of_month" nullable:"true" doc:"monthly: the day, 1 to 31; months without that day use their last day."`
	Date       *string  `json:"date" nullable:"true" example:"2026-12-24" doc:"once: the date, YYYY-MM-DD."`
	TimeZone   string   `json:"time_zone" example:"Asia/Kolkata" doc:"IANA time zone. Times follow its daylight-saving rules."`
}

// ScheduleTimingInput is the timing of a new or changed schedule.
type ScheduleTimingInput struct {
	Kind       string   `json:"kind" enum:"once,daily,weekly,monthly"`
	At         string   `json:"at" pattern:"^([01][0-9]|2[0-3]):[0-5][0-9]$" example:"09:30" doc:"24-hour wall-clock time in time_zone."`
	Days       []string `json:"days,omitempty" maxItems:"7" enum:"mon,tue,wed,thu,fri,sat,sun" doc:"weekly (required): the weekdays to send on."`
	DayOfMonth int      `json:"day_of_month,omitempty" minimum:"1" maximum:"31" doc:"monthly (required): 1 to 31; months without that day use their last day."`
	Date       string   `json:"date,omitempty" pattern:"^[0-9]{4}-[0-9]{2}-[0-9]{2}$" example:"2026-12-24" doc:"once (required): YYYY-MM-DD."`
	TimeZone   string   `json:"time_zone" minLength:"1" maxLength:"64" example:"Asia/Kolkata" doc:"IANA time zone name."`
}

func (t ScheduleTimingInput) spec() schedule.Spec {
	return schedule.Spec{Kind: t.Kind, At: t.At, Days: t.Days, DayOfMonth: t.DayOfMonth, Date: t.Date, TimeZone: t.TimeZone}
}

// Schedule is a message sent at set times.
type Schedule struct {
	ID            string         `json:"id" example:"sch_01ja8z3k5wq2v7c9e4r2n0w6yb"`
	Name          string         `json:"name"`
	Environment   string         `json:"environment" enum:"live,test"`
	To            string         `json:"to" example:"+919876543210"`
	Message       string         `json:"message"`
	DeviceID      *string        `json:"device_id" nullable:"true"`
	Schedule      ScheduleTiming `json:"schedule"`
	Description   string         `json:"description" example:"Every Mon, Fri at 09:30 (Asia/Kolkata)"`
	Status        string         `json:"status" enum:"active,paused,completed" doc:"completed: nothing left to send (a past one-off, or after ends_at)."`
	Paused        bool           `json:"paused"`
	EndsAt        *time.Time     `json:"ends_at" nullable:"true" doc:"No runs after this time."`
	NextRunAt     *time.Time     `json:"next_run_at" nullable:"true"`
	LastRunAt     *time.Time     `json:"last_run_at" nullable:"true"`
	LastMessageID *string        `json:"last_message_id" nullable:"true"`
	LastError     *string        `json:"last_error" nullable:"true" doc:"Why the last run sent nothing, for example an opted-out number."`
	RunCount      int            `json:"run_count"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

type ScheduleList struct {
	Data    []Schedule `json:"data"`
	HasMore bool       `json:"has_more" doc:"Pass the last schedule's ID as starting_after to fetch the next page."`
}

type ScheduleCreateInput struct {
	Name     string              `json:"name,omitempty" maxLength:"100" example:"Weekly reminder"`
	To       string              `json:"to" minLength:"3" maxLength:"32" example:"+919876543210"`
	Message  string              `json:"message" minLength:"1" maxLength:"1600"`
	DeviceID string              `json:"device_id,omitempty" pattern:"^dev_[0-9a-z]{26}$" doc:"Send through this phone. Leave out to let Bridge pick."`
	Schedule ScheduleTimingInput `json:"schedule"`
	EndsAt   *time.Time          `json:"ends_at,omitempty" doc:"Stop repeating after this time."`
	Paused   bool                `json:"paused,omitempty" doc:"Create it paused."`
}

type ScheduleUpdateInput struct {
	Name     *string              `json:"name,omitempty" maxLength:"100"`
	To       *string              `json:"to,omitempty" minLength:"3" maxLength:"32"`
	Message  *string              `json:"message,omitempty" minLength:"1" maxLength:"1600"`
	DeviceID *string              `json:"device_id,omitempty" pattern:"^(dev_[0-9a-z]{26})?$" doc:"An empty string lets Bridge pick the phone."`
	Schedule *ScheduleTimingInput `json:"schedule,omitempty" doc:"Replaces the whole timing; the next run is computed again."`
	EndsAt   *string              `json:"ends_at,omitempty" doc:"RFC 3339 time; an empty string removes the end."`
	Paused   *bool                `json:"paused,omitempty"`
}

type SchedulePath struct {
	ScheduleID string `path:"scheduleId" pattern:"^sch_[0-9a-z]{26}$" example:"sch_01ja8z3k5wq2v7c9e4r2n0w6yb"`
}

type ProjectSchedulePath struct {
	ProjectPath
	SchedulePath
}

type ListSchedulesQuery struct {
	Limit         int    `query:"limit" minimum:"1" maximum:"100" default:"25"`
	StartingAfter string `query:"starting_after" doc:"A schedule ID; returns schedules created before it."`
}

func toSchedule(m dbq.ScheduledMessage) Schedule {
	sp := schedule.SpecOf(m)
	t := ScheduleTiming{Kind: sp.Kind, At: sp.At, Days: sp.Days, TimeZone: sp.TimeZone}
	if t.Days == nil {
		t.Days = []string{}
	}
	if sp.DayOfMonth > 0 {
		t.DayOfMonth = &sp.DayOfMonth
	}
	if sp.Date != "" {
		t.Date = &sp.Date
	}
	status := "active"
	switch {
	case m.Paused:
		status = "paused"
	case m.NextRunAt == nil:
		status = "completed"
	}
	return Schedule{
		ID: m.ID, Name: m.Name, Environment: string(m.Environment), To: m.Recipient, Message: m.Body, DeviceID: m.DeviceID,
		Schedule: t, Description: sp.Describe(), Status: status, Paused: m.Paused, EndsAt: m.EndsAt, NextRunAt: m.NextRunAt,
		LastRunAt: m.LastRunAt, LastMessageID: m.LastMessageID, LastError: m.LastError, RunCount: int(m.RunCount),
		CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

func scheduleError(err error, scheduleID string) error {
	if errors.Is(err, schedule.ErrNotFound) {
		return notFound("Schedule " + scheduleID)
	}
	return messagingError(err)
}

// scheduleAction is one of a schedule's commands.
type scheduleAction func(ctx context.Context, m dbq.ScheduledMessage) (dbq.ScheduledMessage, error)

func (s *Server) registerSchedules(api huma.API) {
	// ---- Developer API -------------------------------------------------
	huma.Register(api, huma.Operation{
		OperationID: "createSchedule", Method: http.MethodPost, Path: "/v1/schedules", Tags: []string{"Schedules"},
		Summary: "Schedule a message",
		Description: "Sends a message once at a date and time, or repeating daily, weekly (on chosen weekdays) or monthly, " +
			"at a wall-clock time in an IANA time zone. Each run creates an ordinary message with `metadata.schedule_id`.",
		Security: apiKeyAuth, DefaultStatus: http.StatusCreated, Errors: []int{http.StatusUnauthorized},
	}, func(ctx context.Context, in *struct{ Body ScheduleCreateInput }) (*struct{ Body Schedule }, error) {
		k := principalFrom(ctx).APIKey
		m, err := s.createSchedule(ctx, schedule.CreateRequest{ProjectID: k.ProjectID, Environment: k.Environment, APIKeyID: &k.ID}, in.Body)
		if err != nil {
			return nil, err
		}
		setResource(ctx, m.ID)
		return &struct{ Body Schedule }{Body: toSchedule(m)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "listSchedules", Method: http.MethodGet, Path: "/v1/schedules", Tags: []string{"Schedules"},
		Summary: "List scheduled messages", Description: "Newest first, in the key's environment.", Security: apiKeyAuth, Errors: []int{http.StatusUnauthorized},
	}, func(ctx context.Context, in *ListSchedulesQuery) (*struct{ Body ScheduleList }, error) {
		k := principalFrom(ctx).APIKey
		return s.listSchedules(ctx, k.ProjectID, k.Environment, in)
	})

	keySchedule := func(ctx context.Context, scheduleID string) (dbq.ScheduledMessage, error) {
		k := principalFrom(ctx).APIKey
		setResource(ctx, scheduleID)
		m, err := s.tools.Schedules.Get(ctx, k.ProjectID, scheduleID)
		if err != nil || m.Environment != k.Environment {
			return m, scheduleError(cmpErr(err, schedule.ErrNotFound), scheduleID)
		}
		return m, nil
	}

	huma.Register(api, huma.Operation{
		OperationID: "getSchedule", Method: http.MethodGet, Path: "/v1/schedules/{scheduleId}", Tags: []string{"Schedules"},
		Summary: "Get a scheduled message", Security: apiKeyAuth, Errors: []int{http.StatusUnauthorized, http.StatusNotFound},
	}, func(ctx context.Context, in *SchedulePath) (*struct{ Body Schedule }, error) {
		m, err := keySchedule(ctx, in.ScheduleID)
		if err != nil {
			return nil, err
		}
		return &struct{ Body Schedule }{Body: toSchedule(m)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "updateSchedule", Method: http.MethodPatch, Path: "/v1/schedules/{scheduleId}", Tags: []string{"Schedules"},
		Summary: "Update a scheduled message", Description: "Omitted fields stay unchanged. Changing the timing computes the next run again from now.",
		Security: apiKeyAuth, Errors: []int{http.StatusUnauthorized, http.StatusNotFound},
	}, func(ctx context.Context, in *struct {
		SchedulePath
		Body ScheduleUpdateInput
	}) (*struct{ Body Schedule }, error) {
		m, err := keySchedule(ctx, in.ScheduleID)
		if err != nil {
			return nil, err
		}
		if m, _, err = s.updateSchedule(ctx, m, in.Body); err != nil {
			return nil, err
		}
		return &struct{ Body Schedule }{Body: toSchedule(m)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "deleteSchedule", Method: http.MethodDelete, Path: "/v1/schedules/{scheduleId}", Tags: []string{"Schedules"},
		Summary: "Delete a scheduled message", Description: "Messages it already sent are kept.",
		Security: apiKeyAuth, DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusUnauthorized, http.StatusNotFound},
	}, func(ctx context.Context, in *SchedulePath) (*struct{}, error) {
		m, err := keySchedule(ctx, in.ScheduleID)
		if err != nil {
			return nil, err
		}
		return nil, scheduleError(s.tools.Schedules.Delete(ctx, m.ProjectID, m.ID), m.ID)
	})

	actions := []struct {
		verb, summary, desc string
		run                 scheduleAction
	}{
		{"pause", "Pause a scheduled message", "Nothing is sent until it is resumed.",
			func(ctx context.Context, m dbq.ScheduledMessage) (dbq.ScheduledMessage, error) {
				return s.tools.Schedules.SetPaused(ctx, m, true)
			}},
		{"resume", "Resume a scheduled message", "Runs missed while paused are skipped; the next run is computed from now.",
			func(ctx context.Context, m dbq.ScheduledMessage) (dbq.ScheduledMessage, error) {
				return s.tools.Schedules.SetPaused(ctx, m, false)
			}},
		{"run", "Send a scheduled message now", "Sends the message once, now, without changing the next regular run. Works while paused.",
			func(ctx context.Context, m dbq.ScheduledMessage) (dbq.ScheduledMessage, error) {
				return s.tools.Schedules.RunNow(ctx, m)
			}},
	}
	for _, a := range actions {
		huma.Register(api, huma.Operation{
			OperationID: a.verb + "Schedule", Method: http.MethodPost, Path: "/v1/schedules/{scheduleId}/" + a.verb, Tags: []string{"Schedules"},
			Summary: a.summary, Description: a.desc, Security: apiKeyAuth, Errors: []int{http.StatusUnauthorized, http.StatusNotFound},
		}, func(ctx context.Context, in *SchedulePath) (*struct{ Body Schedule }, error) {
			m, err := keySchedule(ctx, in.ScheduleID)
			if err != nil {
				return nil, err
			}
			if m, err = a.run(ctx, m); err != nil {
				return nil, scheduleError(err, in.ScheduleID)
			}
			return &struct{ Body Schedule }{Body: toSchedule(m)}, nil
		})
	}

	// ---- Dashboard ------------------------------------------------------
	huma.Register(api, huma.Operation{
		OperationID: "createProjectSchedule", Method: http.MethodPost, Path: "/v1/projects/{projectId}/schedules", Tags: []string{"Schedules"},
		Summary: "Schedule a message", Description: "Same as `POST /v1/schedules`. Live schedules need an admin.",
		Security: sessionAuth, DefaultStatus: http.StatusCreated, Errors: []int{http.StatusForbidden, http.StatusNotFound},
	}, func(ctx context.Context, in *struct {
		ProjectPath
		Environment string `query:"environment" enum:"live,test" default:"test"`
		Body        ScheduleCreateInput
	}) (*struct{ Body Schedule }, error) {
		p, err := s.projectForUser(ctx, in.ProjectID)
		if err != nil {
			return nil, err
		}
		env := dbq.APIEnvironment(in.Environment)
		if err := liveNeedsAdmin(p, env); err != nil {
			return nil, err
		}
		userID := principalFrom(ctx).User.ID
		m, err := s.createSchedule(ctx, schedule.CreateRequest{ProjectID: p.ID, Environment: env, UserID: &userID}, in.Body)
		if err != nil {
			return nil, err
		}
		if err := s.auditSchedule(ctx, p, "schedule.created", m, nil); err != nil {
			return nil, err
		}
		return &struct{ Body Schedule }{Body: toSchedule(m)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "listProjectSchedules", Method: http.MethodGet, Path: "/v1/projects/{projectId}/schedules", Tags: []string{"Schedules"},
		Summary: "List scheduled messages", Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *struct {
		ProjectPath
		ListSchedulesQuery
		Environment string `query:"environment" enum:"live,test" default:"live"`
	}) (*struct{ Body ScheduleList }, error) {
		if _, err := s.projectForUser(ctx, in.ProjectID); err != nil {
			return nil, err
		}
		return s.listSchedules(ctx, in.ProjectID, dbq.APIEnvironment(in.Environment), &in.ListSchedulesQuery)
	})

	// projectSchedule loads a schedule for the dashboard; write checks the role for live ones.
	projectSchedule := func(ctx context.Context, in *ProjectSchedulePath, write bool) (dbq.GetProjectForUserRow, dbq.ScheduledMessage, error) {
		p, err := s.projectForUser(ctx, in.ProjectID)
		if err != nil {
			return p, dbq.ScheduledMessage{}, err
		}
		m, err := s.tools.Schedules.Get(ctx, p.ID, in.ScheduleID)
		if err != nil {
			return p, m, scheduleError(err, in.ScheduleID)
		}
		if write {
			if err := liveNeedsAdmin(p, m.Environment); err != nil {
				return p, m, err
			}
		}
		return p, m, nil
	}

	huma.Register(api, huma.Operation{
		OperationID: "getProjectSchedule", Method: http.MethodGet, Path: "/v1/projects/{projectId}/schedules/{scheduleId}", Tags: []string{"Schedules"},
		Summary: "Get a scheduled message", Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *ProjectSchedulePath) (*struct{ Body Schedule }, error) {
		_, m, err := projectSchedule(ctx, in, false)
		if err != nil {
			return nil, err
		}
		return &struct{ Body Schedule }{Body: toSchedule(m)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "updateProjectSchedule", Method: http.MethodPatch, Path: "/v1/projects/{projectId}/schedules/{scheduleId}", Tags: []string{"Schedules"},
		Summary: "Update a scheduled message", Description: "Omitted fields stay unchanged. Live schedules need an admin.",
		Security: sessionAuth, Errors: []int{http.StatusForbidden, http.StatusNotFound},
	}, func(ctx context.Context, in *struct {
		ProjectSchedulePath
		Body ScheduleUpdateInput
	}) (*struct{ Body Schedule }, error) {
		p, m, err := projectSchedule(ctx, &in.ProjectSchedulePath, true)
		if err != nil {
			return nil, err
		}
		m, changed, err := s.updateSchedule(ctx, m, in.Body)
		if err != nil {
			return nil, err
		}
		if err := s.auditSchedule(ctx, p, "schedule.updated", m, map[string]any{"changed": changed}); err != nil {
			return nil, err
		}
		return &struct{ Body Schedule }{Body: toSchedule(m)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "deleteProjectSchedule", Method: http.MethodDelete, Path: "/v1/projects/{projectId}/schedules/{scheduleId}", Tags: []string{"Schedules"},
		Summary: "Delete a scheduled message", Description: "Live schedules need an admin.",
		Security: sessionAuth, DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusForbidden, http.StatusNotFound},
	}, func(ctx context.Context, in *ProjectSchedulePath) (*struct{}, error) {
		p, m, err := projectSchedule(ctx, in, true)
		if err != nil {
			return nil, err
		}
		if err := s.tools.Schedules.Delete(ctx, p.ID, m.ID); err != nil {
			return nil, scheduleError(err, m.ID)
		}
		return nil, s.auditSchedule(ctx, p, "schedule.deleted", m, nil)
	})

	for _, a := range actions {
		huma.Register(api, huma.Operation{
			OperationID: a.verb + "ProjectSchedule", Method: http.MethodPost, Path: "/v1/projects/{projectId}/schedules/{scheduleId}/" + a.verb,
			Tags: []string{"Schedules"}, Summary: a.summary, Description: a.desc + " Live schedules need an admin.",
			Security: sessionAuth, Errors: []int{http.StatusForbidden, http.StatusNotFound},
		}, func(ctx context.Context, in *ProjectSchedulePath) (*struct{ Body Schedule }, error) {
			p, m, err := projectSchedule(ctx, in, true)
			if err != nil {
				return nil, err
			}
			if m, err = a.run(ctx, m); err != nil {
				return nil, scheduleError(err, in.ScheduleID)
			}
			action := map[string]string{"pause": "schedule.paused", "resume": "schedule.resumed", "run": "schedule.run_now"}[a.verb]
			if err := s.auditSchedule(ctx, p, action, m, nil); err != nil {
				return nil, err
			}
			return &struct{ Body Schedule }{Body: toSchedule(m)}, nil
		})
	}
}

func (s *Server) auditSchedule(ctx context.Context, p dbq.GetProjectForUserRow, action string, m dbq.ScheduledMessage, extra map[string]any) error {
	meta := map[string]any{"environment": m.Environment, "name": m.Name, "schedule": schedule.SpecOf(m).Describe()}
	for k, v := range extra {
		meta[k] = v
	}
	return s.audit(ctx, s.q, auditEntry{
		OrganizationID: p.OrganizationID, ProjectID: p.ID, Action: action, TargetType: "schedule", TargetID: m.ID, Metadata: meta,
	})
}

func (s *Server) createSchedule(ctx context.Context, req schedule.CreateRequest, in ScheduleCreateInput) (dbq.ScheduledMessage, error) {
	req.Fields = schedule.Fields{
		Name: in.Name, To: in.To, Body: in.Message, DeviceID: optString(in.DeviceID), Spec: in.Schedule.spec(),
		EndsAt: in.EndsAt, Paused: in.Paused,
	}
	m, err := s.tools.Schedules.Create(ctx, req)
	if err != nil {
		return m, messagingError(err)
	}
	return m, nil
}

// updateSchedule applies a PATCH and returns the names of the changed fields.
func (s *Server) updateSchedule(ctx context.Context, m dbq.ScheduledMessage, in ScheduleUpdateInput) (dbq.ScheduledMessage, []string, error) {
	f := schedule.FieldsOf(m)
	changed := []string{}
	set := func(name string, ok bool) {
		if ok {
			changed = append(changed, name)
		}
	}
	if in.Name != nil {
		f.Name = *in.Name
	}
	set("name", in.Name != nil)
	if in.To != nil {
		f.To = *in.To
	}
	set("to", in.To != nil)
	if in.Message != nil {
		f.Body = *in.Message
	}
	set("message", in.Message != nil)
	if in.DeviceID != nil {
		f.DeviceID = optString(*in.DeviceID)
	}
	set("device_id", in.DeviceID != nil)
	if in.Schedule != nil {
		f.Spec = in.Schedule.spec()
	}
	set("schedule", in.Schedule != nil)
	if in.EndsAt != nil {
		f.EndsAt = nil
		if *in.EndsAt != "" {
			t, err := time.Parse(time.RFC3339, *in.EndsAt)
			if err != nil {
				return m, nil, huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{
					Location: "body.ends_at", Message: "Use an RFC 3339 time such as 2026-12-31T23:59:00Z, or an empty string to remove the end.",
				})
			}
			f.EndsAt = &t
		}
	}
	set("ends_at", in.EndsAt != nil)
	if in.Paused != nil {
		f.Paused = *in.Paused
	}
	set("paused", in.Paused != nil)
	updated, err := s.tools.Schedules.Update(ctx, m, f)
	if err != nil {
		return m, nil, messagingError(err)
	}
	return updated, changed, nil
}

func (s *Server) listSchedules(ctx context.Context, projectID string, env dbq.APIEnvironment, in *ListSchedulesQuery) (*struct{ Body ScheduleList }, error) {
	rows, more, err := s.tools.Schedules.List(ctx, schedule.ListRequest{
		ProjectID: projectID, Environment: env, StartingAfter: in.StartingAfter, Limit: cmpOrInt(in.Limit, 25),
	})
	if err != nil {
		return nil, queryError(err)
	}
	out := ScheduleList{Data: make([]Schedule, 0, len(rows)), HasMore: more}
	for _, m := range rows {
		out.Data = append(out.Data, toSchedule(m))
	}
	return &struct{ Body ScheduleList }{Body: out}, nil
}

// queryError reports a validation error about a query parameter.
func queryError(err error) error {
	var ve *messaging.ValidationError
	if errors.As(err, &ve) {
		return huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{Location: "query." + ve.Field, Message: ve.Message})
	}
	return err
}
