# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository overview

CLIProxyAPI is a Go service that exposes OpenAI/Gemini/Claude-compatible HTTP APIs for CLI-oriented model providers, with OAuth-based account flows and multi-account routing.

README highlights to keep in mind:
- OpenAI/Gemini/Claude-compatible endpoints
- OAuth login flows (Codex, Claude, Qwen, iFlow, etc.)
- Round-robin/fill-first multi-account routing
- SDK is reusable/embeddable (`docs/sdk-usage.md`, `docs/sdk-advanced.md`)
- Amp-specific provider routing and model mapping support

## Common development commands

## Prerequisites
- Go version from `go.mod`: **Go 1.26.0**

## Build
```bash
# Local build (same target as CI package)
go build -o ./CLIProxyAPI ./cmd/server

# PR CI build step
# (from .github/workflows/pr-test-build.yml)
go build -o test-output ./cmd/server
```

## Run
```bash
# Run from source
go run ./cmd/server

# Run with explicit config path
go run ./cmd/server -config config.yaml

# Run built binary
./CLIProxyAPI -config config.yaml

# TUI client mode (expects server already running)
./CLIProxyAPI -tui

# TUI standalone mode (starts embedded local server)
./CLIProxyAPI -tui -standalone
```

## OAuth / account login commands
```bash
./CLIProxyAPI -login
./CLIProxyAPI -codex-login
./CLIProxyAPI -codex-device-login
./CLIProxyAPI -claude-login
./CLIProxyAPI -qwen-login
./CLIProxyAPI -iflow-login
./CLIProxyAPI -antigravity-login
./CLIProxyAPI -kimi-login
```

## Tests
```bash
# Run all tests
go test ./...

# Run package tests
go test ./test

# Run a single test (example)
go test ./test -run TestGetAmpCode
```

## Formatting / static checks
```bash
go fmt ./...
go vet ./...
```

## Docker workflow
```bash
# Use pre-built image
docker compose up -d --remove-orphans --no-build

# Build image from source with version metadata
docker compose build \
  --build-arg VERSION="$(git describe --tags --always --dirty)" \
  --build-arg COMMIT="$(git rev-parse --short HEAD)" \
  --build-arg BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# Start local-built image
docker compose up -d --remove-orphans --pull never

# Logs
docker compose logs -f
```

Helper script:
```bash
./docker-build.sh
# or with usage stats migration support
./docker-build.sh --with-usage
```

## High-level architecture

## Entry and mode selection
- `cmd/server/main.go` is the main entrypoint.
- It parses CLI flags (login modes, TUI mode, config path), loads config, sets token store backend, registers access providers, then either:
  - runs provider login flows (`internal/cmd/*login*.go`), or
  - starts the proxy service (`internal/cmd/run.go`).

## Service assembly and lifecycle
- `internal/cmd/run.go` builds/runs the service through `sdk/cliproxy.NewBuilder()`.
- `sdk/cliproxy/builder.go` wires defaults for:
  - token/API-key client providers,
  - request access manager,
  - runtime auth manager,
  - routing selector (round-robin vs fill-first from config),
  - server options.
- `sdk/cliproxy/service.go` manages runtime lifecycle:
  - starts HTTP server,
  - starts watcher,
  - propagates auth/config updates,
  - integrates websocket runtime providers.

## API layer and request path
- `internal/api/server.go` constructs Gin engine, middleware, access checks, management routes, and main API routes.
- `sdk/api/handlers/*` provides provider-compatible handler surface (`openai`, `claude`, `gemini`) on top of a shared base handler.
- Execution path is generally:
  1. inbound HTTP request (Gin)
  2. request authentication/access validation (`sdk/access`, `internal/access/config_access`)
  3. model/provider resolution and auth selection
  4. request translation to upstream format (`internal/translator`, `sdk/translator`)
  5. provider execution (`internal/runtime/executor/*`)
  6. response translation + streaming/non-streaming return
  7. usage/quota bookkeeping (`internal/usage`, `internal/registry`)

## Configuration and hot reload
- `internal/config/config.go` handles config loading/defaulting/sanitization and secure secret handling.
- Runtime file watching (`internal/watcher`) drives hot reload behavior for config/auth updates.
- Management endpoints (`internal/api/handlers/management`) mutate runtime config and related state.

## Key directories
- `cmd/server` — CLI entrypoint
- `internal/api` — HTTP server, middleware, management integration
- `internal/runtime/executor` — upstream provider executors
- `internal/translator` — provider/openai translation logic
- `internal/config` — config model/load/sanitize
- `sdk/cliproxy` — embeddable service builder/lifecycle
- `sdk/api` — API handlers
- `sdk/auth` and `sdk/cliproxy/auth` — auth persistence and runtime auth orchestration
- `test` — integration-style behavior tests

## Assistant-instruction files

At current repo state, these files are not present:
- `.cursorrules`
- `.cursor/rules/*`
- `.github/copilot-instructions.md`
