# Contributing to Bridge

Thanks for helping. Bridge aims to stay small, boring and reliable, so a focused pull request with
tests is worth more than a large one without them.

## Prerequisites

| Tool | Version |
| --- | --- |
| Go | 1.27 (any Go ≥ 1.21 fetches 1.27 automatically via `GOTOOLCHAIN`) |
| Node.js | 24 LTS |
| pnpm | 12 (pinned in `package.json`; Corepack or pnpm itself switches to it) |
| Docker | with Compose, for PostgreSQL |

## Local setup

```bash
pnpm install
pnpm infra:up                      # PostgreSQL 18 on 127.0.0.1:5432

# API + worker (terminal 1)
cd apps/api
export BRIDGE_DATABASE_URL="postgres://bridge:bridge@localhost:5432/bridge?sslmode=disable"
go run ./cmd/bridge migrate
go run ./cmd/bridge serve --worker

# Dashboard (terminal 2)
pnpm --filter @bridge/dashboard dev
```

Open http://localhost:3000 and create an account. The API reference is at http://localhost:8080/docs.

No Android phone? Pair a simulated one. In the dashboard open **Devices → Pair device → Cannot
scan?**, copy the pairing code, then:

```bash
cd apps/api
go run ./cmd/devicesim -code bp_…        # holds a gateway connection and sends heartbeats
```

The Android app lives in `android/gateway`; see [docs/android/README.md](docs/android/README.md).

## Repository layout

See [docs/architecture/era-0-plan.md](docs/architecture/era-0-plan.md) for the architecture and every
technology decision. In short:

* `apps/api` is a Go module. Handlers live in `internal/httpapi`, SQL in `internal/db/queries`
  (compiled to Go by sqlc), and migrations in `internal/db/migrations` (goose).
* `apps/dashboard` is Next.js. It talks to the API only through its `/api/*` proxy.
* `packages/api-types` holds TypeScript types generated from the Go API's OpenAPI document.

## Making changes

### API changes

1. Edit SQL in `internal/db/queries/*.sql` or add a migration in `internal/db/migrations`.
   Never edit an applied migration; add a new one.
2. Regenerate Go code from SQL (runs sqlc in Docker; no local install needed):
   ```bash
   docker run --rm -v "$PWD/apps/api:/src" -w /src sqlc/sqlc:1.31.1 generate
   ```
3. Change request/response structs in `internal/httpapi`. These **are** the public contract.
4. Regenerate the TypeScript types so the dashboard sees the change:
   ```bash
   pnpm api:generate
   ```

### Message status changes

All message status transitions go through `internal/message`. Do not update `messages.status`
anywhere else.

## Tests and checks

Run these before opening a pull request. CI runs the same set.

```bash
# Go: unit + integration (integration tests create and drop throwaway databases)
cd apps/api
export BRIDGE_TEST_DATABASE_URL="postgres://bridge:bridge@localhost:5432/postgres?sslmode=disable"
gofmt -l . && go vet ./... && go test ./...

# TypeScript
pnpm lint          # Biome
pnpm typecheck     # TypeScript 7
pnpm build
pnpm test          # SDK unit tests (Vitest)

# Android (JDK 17+)
cd android/gateway
./gradlew testFossDebugUnitTest testGmsDebugUnitTest lintFossDebug lintGmsDebug
```

Without `BRIDGE_TEST_DATABASE_URL`, integration tests are skipped, not failed. CI always sets it.

## Conventions

* **Security first.** Never log secrets, tokens, passwords, OTPs or message bodies. Store only
  hashes of credentials. Return 404 (not 403) for resources in other tenants.
* **Errors help the developer.** Say what failed, why, and what to do next. Every error carries a
  stable `code` and the `request_id`.
* **No new dependencies without a reason** stated in the pull request.
* **Commits:** [Conventional Commits](https://www.conventionalcommits.org/) (`feat:`, `fix:`,
  `docs:`, `refactor:`, `test:`, `chore:`). Keep the subject under 72 characters.
* Go is formatted with `gofmt`; TypeScript, JSON and CSS with Biome (`pnpm format`).

## Pull requests

* One logical change per pull request, with tests for behaviour changes.
* Describe what changed and why, and how you verified it.
* Update docs and `CHANGELOG.md` (under *Unreleased*) when behaviour changes.
* Out-of-scope features for the current era (see the plan) will be closed or parked; open an
  issue first if unsure.

Maintainers cut releases by pushing a tag; see [docs/releasing.md](docs/releasing.md).

By contributing you agree that your contributions are licensed under the project's licenses
(AGPL-3.0 for the server, dashboard and Android app; MIT for SDKs).
