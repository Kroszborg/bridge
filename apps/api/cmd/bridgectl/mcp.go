package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpInstructions tell an AI assistant how to use the tools well.
const mcpInstructions = `Bridge sends SMS through Android phones the user owns, and checks one-time passwords.

- Phone numbers are E.164 with a country code, for example +919876543210.
- With a test key (bk_test_) nothing is really sent: messages are simulated and send_verification_code returns the code, so a whole flow can be tried safely. whoami tells you which kind of key is in use.
- Sending a real SMS reaches a real person and may cost money. Confirm the number and text with the user before sending with a live key.
- send_sms returns status queued; use get_message to follow it to delivered or failed. Failed messages carry error_code and error_message.
- For login or sign-up codes use send_verification_code then check_verification_code instead of composing a code yourself.
- To text many people at once use create_broadcast: one template with {placeholders} filled from each recipient's vars. Call it with dry_run true first and show the user the preview (recipients, opted-out numbers left out, segments, sample texts); create it only after they confirm. Follow it with get_broadcast.
- Numbers on the opt-out list (people who replied STOP) are refused for ordinary messages with error opted_out. check_opt_out tells you before you send; never try to work around an opt-out.`

// Tool inputs. Field docs become the JSON schema the assistant sees.
type sendSMSInput struct {
	To       string `json:"to" jsonschema:"destination in E.164, e.g. +919876543210"`
	Message  string `json:"message" jsonschema:"the text to send, at most 1600 characters"`
	DeviceID string `json:"device_id,omitempty" jsonschema:"optional: send through this paired phone only (dev_...)"`
}

type messageIDInput struct {
	ID string `json:"id" jsonschema:"message ID, msg_..."`
}

type listMessagesInput struct {
	Status    string `json:"status,omitempty" jsonschema:"optional filter: queued, sending, sent, delivered, failed or received"`
	Direction string `json:"direction,omitempty" jsonschema:"optional filter: outbound or inbound"`
	To        string `json:"to,omitempty" jsonschema:"optional filter by recipient (E.164)"`
	Limit     int    `json:"limit,omitempty" jsonschema:"how many, 1 to 100 (default 20)"`
}

type sendCodeInput struct {
	To  string `json:"to" jsonschema:"phone number to verify, E.164"`
	App string `json:"app,omitempty" jsonschema:"optional Verify app ID or slug; default is the project's default app"`
}

type checkCodeInput struct {
	To   string `json:"to,omitempty" jsonschema:"the number the code was sent to (or pass id)"`
	ID   string `json:"id,omitempty" jsonschema:"the verification ID, otp_... (or pass to)"`
	Code string `json:"code" jsonschema:"the code the person entered"`
	App  string `json:"app,omitempty" jsonschema:"optional Verify app ID or slug, when checking by number"`
}

type broadcastRecipientInput struct {
	To   string            `json:"to" jsonschema:"recipient in E.164"`
	Vars map[string]string `json:"vars,omitempty" jsonschema:"values for the template's {placeholders}, e.g. {\"name\": \"Asha\"}"`
}

type createBroadcastInput struct {
	Template    string                    `json:"template" jsonschema:"the message, with {placeholders} filled from each recipient's vars; at most 1600 characters"`
	Recipients  []broadcastRecipientInput `json:"recipients" jsonschema:"1 to 10000 recipients; repeated numbers are sent once and opted-out numbers are skipped"`
	Name        string                    `json:"name,omitempty" jsonschema:"optional name to find the broadcast by"`
	ScheduledAt string                    `json:"scheduled_at,omitempty" jsonschema:"optional RFC 3339 time to start later, at most a year ahead"`
	DryRun      bool                      `json:"dry_run,omitempty" jsonschema:"true: validate and preview only; nothing is created or sent"`
}

type broadcastIDInput struct {
	ID string `json:"id" jsonschema:"broadcast ID, brd_..."`
}

type numberInput struct {
	Number string `json:"number" jsonschema:"phone number in E.164, e.g. +919876543210"`
}

type listSchedulesInput struct {
	Limit int `json:"limit,omitempty" jsonschema:"how many, 1 to 100 (default 25)"`
}

type noInput struct{}

type object = map[string]any

func cmdMCP(ctx context.Context, args []string) error {
	// Nothing may be printed to stdout: it carries the protocol.
	c, _, _, err := setup("mcp", args, nil)
	if err != nil {
		return err
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "bridge", Title: "Bridge SMS", Version: version},
		&mcp.ServerOptions{Instructions: mcpInstructions})
	addMCPTools(server, c)
	return server.Run(ctx, &mcp.StdioTransport{})
}

func addMCPTools(s *mcp.Server, c *client) {
	yes := true
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true}
	sends := &mcp.ToolAnnotations{OpenWorldHint: &yes}

	mcp.AddTool(s, &mcp.Tool{Name: "whoami", Title: "Which project and key",
		Description: "The project and environment (live or test) of the configured API key.", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, object, error) {
			var out object
			return nil, out, c.get(ctx, "/v1/whoami", nil, &out)
		})

	mcp.AddTool(s, &mcp.Tool{Name: "send_sms", Title: "Send an SMS",
		Description: "Queue an SMS through the user's phones. With a live key this reaches a real person: confirm first. Returns the message with status queued.",
		Annotations: sends},
		func(ctx context.Context, _ *mcp.CallToolRequest, in sendSMSInput) (*mcp.CallToolResult, object, error) {
			body := object{"to": in.To, "message": in.Message}
			if in.DeviceID != "" {
				body["device_id"] = in.DeviceID
			}
			var out object
			// A fresh key per call: a retried request never sends twice.
			_, err := c.request(ctx, "POST", "/v1/messages", nil, body, map[string]string{"Idempotency-Key": "mcp-" + randomHex(12)}, &out)
			return nil, out, err
		})

	mcp.AddTool(s, &mcp.Tool{Name: "get_message", Title: "Get a message",
		Description: "A message's status and full timeline (queued, sent, delivered or failed with a reason).", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in messageIDInput) (*mcp.CallToolResult, object, error) {
			var out object
			return nil, out, c.get(ctx, "/v1/messages/"+url.PathEscape(in.ID), nil, &out)
		})

	mcp.AddTool(s, &mcp.Tool{Name: "list_messages", Title: "List messages",
		Description: "Recent messages, newest first, sent and received.", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in listMessagesInput) (*mcp.CallToolResult, object, error) {
			q := url.Values{}
			limit := in.Limit
			if limit <= 0 || limit > 100 {
				limit = 20
			}
			q.Set("limit", strconv.Itoa(limit))
			for k, v := range map[string]string{"status": in.Status, "direction": in.Direction, "to": in.To} {
				if v != "" {
					q.Set(k, v)
				}
			}
			var out object
			return nil, out, c.get(ctx, "/v1/messages", q, &out)
		})

	mcp.AddTool(s, &mcp.Tool{Name: "send_verification_code", Title: "Send a verification code",
		Description: "Bridge generates a one-time code and texts it. With a test key nothing is sent and the response includes the code. One code per number every 30 seconds.",
		Annotations: sends},
		func(ctx context.Context, _ *mcp.CallToolRequest, in sendCodeInput) (*mcp.CallToolResult, object, error) {
			var out object
			body := object{"to": in.To}
			if in.App != "" {
				body["app"] = in.App
			}
			_, err := c.request(ctx, "POST", "/v1/otp", nil, body, nil, &out)
			return nil, out, err
		})

	mcp.AddTool(s, &mcp.Tool{Name: "check_verification_code", Title: "Check a verification code",
		Description: "Checks a code. valid is true only when it is right; a wrong code uses one attempt.",
		Annotations: &mcp.ToolAnnotations{}},
		func(ctx context.Context, _ *mcp.CallToolRequest, in checkCodeInput) (*mcp.CallToolResult, object, error) {
			body := object{"code": in.Code}
			if in.App != "" {
				body["app"] = in.App
			}
			if in.ID != "" {
				body["id"] = in.ID
			} else {
				body["to"] = in.To
			}
			var out object
			_, err := c.request(ctx, "POST", "/v1/otp/verify", nil, body, nil, &out)
			return nil, out, err
		})

	mcp.AddTool(s, &mcp.Tool{Name: "create_broadcast", Title: "Send a broadcast",
		Description: "Send one template to many numbers, filling {placeholders} from each recipient's vars. With dry_run true nothing is created and a preview comes back; " +
			"without it, a live key texts real people: preview and confirm first. Returns the broadcast with status sending or scheduled.",
		Annotations: sends},
		func(ctx context.Context, _ *mcp.CallToolRequest, in createBroadcastInput) (*mcp.CallToolResult, object, error) {
			body := object{"template": in.Template, "recipients": in.Recipients}
			if in.Name != "" {
				body["name"] = in.Name
			}
			if in.ScheduledAt != "" {
				body["scheduled_at"] = in.ScheduledAt
			}
			if in.DryRun {
				body["dry_run"] = true
			}
			var out object
			_, err := c.request(ctx, "POST", "/v1/broadcasts", nil, body, nil, &out)
			return nil, out, err
		})

	mcp.AddTool(s, &mcp.Tool{Name: "get_broadcast", Title: "Get a broadcast",
		Description: "A broadcast's status (scheduled, sending, completed or canceled) and counts: queued, sent, delivered, failed, canceled, skipped.", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in broadcastIDInput) (*mcp.CallToolResult, object, error) {
			var out object
			return nil, out, c.get(ctx, "/v1/broadcasts/"+url.PathEscape(in.ID), nil, &out)
		})

	mcp.AddTool(s, &mcp.Tool{Name: "list_schedules", Title: "List scheduled messages",
		Description: "Scheduled and repeating messages, newest first, with their timing, next run and the reason the last run sent nothing (last_error).", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in listSchedulesInput) (*mcp.CallToolResult, object, error) {
			limit := in.Limit
			if limit <= 0 || limit > 100 {
				limit = 25
			}
			var out object
			return nil, out, c.get(ctx, "/v1/schedules", url.Values{"limit": {strconv.Itoa(limit)}}, &out)
		})

	mcp.AddTool(s, &mcp.Tool{Name: "check_opt_out", Title: "Check the opt-out list",
		Description: "Whether a number opted out of messages from this project (for example by replying STOP). Ordinary messages to opted-out numbers are refused; one-time passwords still go.",
		Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in numberInput) (*mcp.CallToolResult, object, error) {
			var entry object
			err := c.get(ctx, "/v1/opt-outs/"+url.PathEscape(in.Number), nil, &entry)
			var ae *apiError
			if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
				return nil, object{"number": in.Number, "opted_out": false}, nil
			}
			if err != nil {
				return nil, nil, err
			}
			return nil, object{"number": entry["number"], "opted_out": true, "opt_out": entry}, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "list_devices", Title: "List phones",
		Description: "The project's paired phones with online status, battery, SIMs and send limits.", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, object, error) {
			var out object
			return nil, out, c.get(ctx, "/v1/devices", nil, &out)
		})

	mcp.AddTool(s, &mcp.Tool{Name: "get_usage", Title: "Usage",
		Description: "Message counts and delivery rate for the last 24 hours and 30 days.", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, object, error) {
			var out object
			return nil, out, c.get(ctx, "/v1/usage", nil, &out)
		})
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
