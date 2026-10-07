# Bridge for AI assistants (MCP)

`bridgectl mcp` runs a [Model Context Protocol](https://modelcontextprotocol.io) server on stdio,
so assistants such as Claude, Cursor and other MCP clients can send SMS, run phone verifications
and check delivery through your Bridge, using an API key you choose.

```bash
bridgectl login --url https://api.sms.example.com   # once; or set BRIDGE_URL and BRIDGE_API_KEY
bridgectl mcp                                        # the client starts this for you
```

Start with a **test key** (`bk_test_…`): nothing is sent, messages are simulated, and verification
codes are returned to the assistant, so it can try whole flows safely. Switch to a live key when
you want real SMS.

## Tools

| Tool | What it does | Changes anything |
| --- | --- | --- |
| `whoami` | The key's project and environment (live or test) | No |
| `send_sms` | Queues an SMS (`to`, `message`, optional `device_id`) | Sends an SMS |
| `get_message` | A message's status and timeline | No |
| `list_messages` | Recent messages, filtered by status, direction or recipient | No |
| `send_verification_code` | Generates and sends a one-time code (returned with test keys) | Sends an SMS |
| `check_verification_code` | Checks a code by number or verification ID | Uses an attempt |
| `list_devices` | Paired phones: online status, battery, SIMs, send limits | No |
| `get_usage` | Counts and delivery rate for 24 hours and 30 days | No |

The verification tools take an optional `app` (a [Verify app](../otp/README.md#verify-apps) ID or
slug) and use the project's default app without it. `check_verification_code` with a number and no
`app` checks that number's latest pending code of any app.

Read-only tools are marked as such, so clients that ask before acting on the world will ask before
`send_sms` and `send_verification_code`. The server also tells the assistant to confirm the number
and text before sending with a live key. Each `send_sms` call carries a fresh idempotency key, so a
retried call never sends twice.

## Set it up in your client

The server needs `bridgectl` on your PATH (see [the CLI guide](../cli/README.md)) and either a saved
`bridgectl login` or these environment variables.

**Claude Code**

```bash
claude mcp add bridge --env BRIDGE_URL=https://api.sms.example.com --env BRIDGE_API_KEY=bk_test_… -- bridgectl mcp
```

**Claude Desktop, Cursor and other clients** that use a JSON config (`claude_desktop_config.json`,
`.cursor/mcp.json`, and so on):

```json
{
  "mcpServers": {
    "bridge": {
      "command": "bridgectl",
      "args": ["mcp"],
      "env": {
        "BRIDGE_URL": "https://api.sms.example.com",
        "BRIDGE_API_KEY": "bk_test_…"
      }
    }
  }
}
```

On Windows use the full path to `bridgectl.exe` if it is not on the PATH the client sees.

## Good to know

- The key decides what the assistant can reach: one project, one environment. Create a separate
  key for the assistant so you can revoke it on its own, and see its requests under **Logs**.
- Errors come back as tool errors with Bridge's message (for example `rate_limited` with how long to
  wait), so the assistant can explain them instead of failing silently.
- Live sends follow the project's [routing](../providers/README.md): phones first, then providers if
  you enabled them.
- Everything the tools do is also in the [REST API](../messages/README.md) and the
  [TypeScript SDK](../../packages/sdk/README.md).
