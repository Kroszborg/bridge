package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
- For login or sign-up codes use send_verification_code then check_verification_code instead of composing a code yourself.`

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
