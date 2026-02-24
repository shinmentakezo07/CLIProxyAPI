# CLIProxyAPI Technical Report

## Executive Summary

CLIProxyAPI is a sophisticated Go-based proxy server that bridges CLI-based AI services with standard API interfaces. It provides OpenAI, Gemini, and Claude compatible API endpoints, enabling users to leverage CLI authentication mechanisms (OAuth, API keys) with any tool or SDK designed for standard AI APIs. The project supports multi-account load balancing, credential rotation, and format translation between different AI provider schemas.

---

## 1. Project Purpose

CLIProxyAPI solves a critical integration challenge: allowing AI coding tools and SDKs designed for standard REST APIs to work with CLI-based authentication flows. Key capabilities include:

- **Multi-Provider Support**: OpenAI Codex, Claude Code, Gemini CLI, Qwen Code, iFlow, Antigravity, and Kimi
- **OAuth Integration**: Seamless OAuth login flows for all supported providers
- **API Compatibility**: OpenAI `/v1/chat/completions`, Claude `/v1/messages`, and Gemini `/v1beta/models` endpoints
- **Multi-Account Load Balancing**: Round-robin credential selection with quota tracking
- **Format Translation**: Automatic translation between OpenAI, Claude, and Gemini request/response schemas
- **Amp CLI Support**: Special routing for Amp CLI and IDE extensions with model mapping

---

## 2. Directory Structure

```
CLIProxyAPI/
├── cmd/
│   └── server/
│       └── main.go              # Application entry point
├── internal/
│   ├── api/
│   │   ├── server.go            # HTTP server setup and routing
│   │   ├── handlers/
│   │   │   └── management/      # Management API handlers
│   │   ├── middleware/          # Request logging, CORS, auth
│   │   └── modules/
│   │       └── amp/             # Amp CLI integration module
│   ├── auth/                    # Provider-specific auth implementations
│   │   ├── antigravity/
│   │   ├── claude/
│   │   ├── codex/
│   │   ├── gemini/
│   │   ├── iflow/
│   │   ├── kimi/
│   │   └── qwen/
│   ├── config/
│   │   └── config.go            # Configuration loading and validation
│   ├── runtime/
│   │   └── executor/            # Provider request executors
│   ├── registry/
│   │   └── model_definitions.go # Model registry and definitions
│   └── translator/              # Format translation (internal)
├── sdk/
│   ├── api/
│   │   └── handlers/            # OpenAI, Claude, Gemini handlers
│   ├── auth/
│   │   └── manager.go           # Auth lifecycle management
│   ├── cliproxy/
│   │   ├── auth/
│   │   │   ├── conductor.go    # Core auth orchestration
│   │   │   ├── selector.go     # Credential selection logic
│   │   │   └── types.go        # Auth type definitions
│   │   ├── executor/
│   │   │   └── types.go        # Executor interfaces
│   │   └── service.go          # Service orchestration
│   └── translator/
│       └── registry.go          # Translation registry
├── examples/
│   ├── custom-provider/         # Custom provider example
│   ├── http-request/            # HTTP request example
│   └── translator/              # Translator example
├── go.mod                       # Go module definition
├── go.sum                       # Dependency checksums
├── config.example.yaml          # Example configuration
├── Dockerfile                   # Container build
└── docker-compose.yml           # Container orchestration
```

---

## 3. Key Dependencies

### Core Web Framework
| Package | Version | Purpose |
|---------|---------|---------|
| `github.com/gin-gonic/gin` | v1.10.1 | HTTP web framework |
| `github.com/gorilla/websocket` | v1.5.3 | WebSocket support for streaming |

### Authentication & Security
| Package | Version | Purpose |
|---------|---------|---------|
| `golang.org/x/oauth2` | v0.30.0 | OAuth 2.0 client flows |
| `golang.org/x/crypto` | v0.45.0 | bcrypt password hashing |
| `github.com/refraction-networking/utls` | v1.8.2 | TLS fingerprinting bypass |

### Configuration & Logging
| Package | Version | Purpose |
|---------|---------|---------|
| `gopkg.in/yaml.v3` | v3.0.1 | YAML configuration parsing |
| `github.com/sirupsen/logrus` | v1.9.3 | Structured logging |
| `gopkg.in/natefinch/lumberjack.v2` | v2.2.1 | Log rotation |

### Data Processing
| Package | Version | Purpose |
|---------|---------|---------|
| `github.com/tidwall/gjson` | v1.18.0 | Fast JSON path queries |
| `github.com/tidwall/sjson` | v1.2.5 | JSON modification |
| `github.com/tiktoken-go/tokenizer` | v0.7.0 | Token counting |

### Storage Backends
| Package | Version | Purpose |
|---------|---------|---------|
| `github.com/jackc/pgx/v5` | v5.7.6 | PostgreSQL token store |
| `github.com/minio/minio-go/v7` | v7.0.66 | Object storage token store |
| `github.com/go-git/go-git/v6` | v6.0.0 | Git-backed token store |

### UI Components
| Package | Version | Purpose |
|---------|---------|---------|
| `github.com/charmbracelet/bubbletea` | v1.3.10 | Terminal UI framework |
| `github.com/charmbracelet/lipgloss` | v1.1.0 | Terminal styling |

---

## 4. Core Architectural Patterns

### 4.1 Layered Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    HTTP Layer (Gin)                         │
│  /v1/chat/completions  /v1/messages  /v1beta/models        │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                   Handler Layer (SDK)                       │
│  OpenAIHandlers  ClaudeHandlers  GeminiHandlers            │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                 Translation Layer (SDK)                     │
│  Request/Response format translation between providers      │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│               Auth Manager Layer (SDK)                      │
│  Credential selection, rotation, refresh, quota tracking   │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                Executor Layer (Internal)                    │
│  Provider-specific HTTP request execution                   │
└─────────────────────────────────────────────────────────────┘
```

### 4.2 Provider/Executor Pattern

The system uses a provider-agnostic executor interface defined in [`sdk/cliproxy/executor/types.go`](sdk/cliproxy/executor/types.go):

```go
type ProviderExecutor interface {
    Identifier() string
    Execute(ctx context.Context, auth *Auth, req Request, opts Options) (Response, error)
    ExecuteStream(ctx context.Context, auth *Auth, req Request, opts Options) (*StreamResult, error)
    Refresh(ctx context.Context, auth *Auth) (*Auth, error)
    CountTokens(ctx context.Context, auth *Auth, req Request, opts Options) (Response, error)
    HttpRequest(ctx context.Context, auth *Auth, req *http.Request) (*http.Response, error)
}
```

Each provider (Claude, Gemini, Codex, etc.) implements this interface with provider-specific logic in [`internal/runtime/executor/`](internal/runtime/executor/).

### 4.3 Translator Registry Pattern

Format translation is handled through a registry pattern in [`sdk/translator/registry.go`](sdk/translator/registry.go):

```go
type Registry struct {
    requests  map[Format]map[Format]RequestTransform
    responses map[Format]map[Format]ResponseTransform
}
```

This allows bidirectional translation between:
- OpenAI format (`openai`)
- Claude format (`claude`)
- Gemini format (`gemini`)
- Provider-native formats

### 4.4 Auth Conductor Pattern

The [`sdk/cliproxy/auth/conductor.go`](sdk/cliproxy/auth/conductor.go) implements the core auth orchestration:

```go
type Manager struct {
    store     Store
    executors map[string]ProviderExecutor
    selector  Selector
    hook      Hook
    auths     map[string]*Auth
    // ...
}
```

Key responsibilities:
- **Credential Selection**: Round-robin or quota-aware selection via [`selector.go`](sdk/cliproxy/auth/selector.go)
- **Refresh Management**: Automatic token refresh before expiration
- **Quota Tracking**: Cooldown scheduling when quotas are exceeded
- **Persistence**: Pluggable storage backends (file, PostgreSQL, Git, S3)

---

## 5. Primary Logic Flow

### 5.1 Application Startup

```
main() [cmd/server/main.go]
    │
    ├── Parse command-line flags
    │
    ├── Load configuration (YAML)
    │   ├── PostgreSQL store (if configured)
    │   ├── Object store (if configured)
    │   ├── Git store (if configured)
    │   └── Local file (default)
    │
    ├── Initialize token store
    │   └── sdkAuth.RegisterTokenStore()
    │
    ├── Register access providers
    │   └── configaccess.Register()
    │
    └── Handle command mode:
        ├── OAuth login flows (--login, --codex-login, etc.)
        ├── TUI mode (--tui)
        └── Server mode (default)
            └── cmd.StartService()
```

### 5.2 Request Processing Flow

```
HTTP Request
    │
    ▼
┌─────────────────────────────────────────────────────────────┐
│ Gin Middleware Stack                                        │
│  1. logging.GinLogrusLogger() - Request logging            │
│  2. logging.GinLogrusRecovery() - Panic recovery           │
│  3. middleware.RequestLoggingMiddleware() - Audit logs     │
│  4. corsMiddleware() - CORS handling                        │
│  5. AuthMiddleware() - API key validation                  │
└─────────────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────────┐
│ Handler (e.g., OpenAIHandlers.ChatCompletions)             │
│  1. Parse request body                                     │
│  2. Extract model name                                     │
│  3. Call baseHandler.Execute() or ExecuteStream()          │
└─────────────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────────┐
│ BaseAPIHandler.Execute() [sdk/api/handlers/handlers.go]    │
│  1. Resolve provider from model name                       │
│  2. Call authManager.Execute() with provider context       │
└─────────────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────────┐
│ auth.Manager.Execute() [sdk/cliproxy/auth/conductor.go]    │
│  1. Select credential via Selector.Pick()                  │
│  2. Check credential validity/refresh if needed            │
│  3. Call providerExecutor.Execute() or ExecuteStream()     │
│  4. Handle retries on failure                              │
│  5. Update credential state (quota, errors)                │
└─────────────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────────┐
│ Provider Executor (e.g., ClaudeExecutor)                   │
│  1. Translate request format (OpenAI → Claude)             │
│  2. Build HTTP request with auth headers                   │
│  3. Execute HTTP request to upstream API                   │
│  4. Translate response format (Claude → OpenAI)            │
│  5. Return response                                        │
└─────────────────────────────────────────────────────────────┘
    │
    ▼
HTTP Response (streaming or non-streaming)
```

### 5.3 Credential Selection Algorithm

The [`selector.go`](sdk/cliproxy/auth/selector.go) implements intelligent credential selection:

1. **Filter available credentials**: Exclude cooling-down, invalid, or quota-exceeded
2. **Apply model exclusions**: Skip credentials with excluded models
3. **Round-robin rotation**: Maintain fair distribution across credentials
4. **Provider fallback**: Support multi-provider routing with offset tracking

---

## 6. Component Interaction Diagram

```
┌──────────────────────────────────────────────────────────────────────────┐
│                              Client                                       │
│                    (Claude Code, Cline, SDK, etc.)                       │
└──────────────────────────────────────────────────────────────────────────┘
                                    │
                                    │ HTTP Request (OpenAI/Claude/Gemini format)
                                    ▼
┌──────────────────────────────────────────────────────────────────────────┐
│                           CLIProxyAPI Server                             │
│  ┌────────────────────────────────────────────────────────────────────┐  │
│  │                      Gin HTTP Engine                               │  │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐             │  │
│  │  │   /v1/*      │  │  /v1beta/*   │  │  /manage/*   │             │  │
│  │  │  (OpenAI)    │  │  (Gemini)    │  │ (Management) │             │  │
│  │  └──────────────┘  └──────────────┘  └──────────────┘             │  │
│  └────────────────────────────────────────────────────────────────────┘  │
│                                    │                                     │
│                                    ▼                                     │
│  ┌────────────────────────────────────────────────────────────────────┐  │
│  │                    SDK Handler Layer                               │  │
│  │  ┌──────────────────────────────────────────────────────────────┐  │  │
│  │  │  BaseAPIHandler                                              │  │  │
│  │  │  ├── OpenAIAPIHandler (chat, completions, responses)         │  │  │
│  │  │  ├── ClaudeCodeAPIHandler (messages, count_tokens)           │  │  │
│  │  │  └── GeminiAPIHandler (models, generateContent)              │  │  │
│  │  └──────────────────────────────────────────────────────────────┘  │  │
│  └────────────────────────────────────────────────────────────────────┘  │
│                                    │                                     │
│                                    ▼                                     │
│  ┌────────────────────────────────────────────────────────────────────┐  │
│  │                   Auth Manager (Conductor)                         │  │
│  │  ┌────────────────────────────────────────────────────────────┐    │  │
│  │  │  Selector          │  Credential Store (File/PG/Git/S3)    │    │  │
│  │  │  (Round-Robin)     │  (Token Persistence)                  │    │  │
│  │  └────────────────────────────────────────────────────────────┘    │  │
│  └────────────────────────────────────────────────────────────────────┘  │
│                                    │                                     │
│                                    ▼                                     │
│  ┌────────────────────────────────────────────────────────────────────┐  │
│  │                    Provider Executors                              │  │
│  │  ┌─────────────┐ ┌─────────────┐ ┌─────────────┐ ┌─────────────┐  │  │
│  │  │   Claude    │ │   Gemini    │ │   Codex     │ │   Qwen      │  │  │
│  │  │  Executor   │ │  Executor   │ │  Executor   │ │  Executor   │  │  │
│  │  └─────────────┘ └─────────────┘ └─────────────┘ └─────────────┘  │  │
│  └────────────────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────────────┘
                                    │
                                    │ HTTPS (Provider APIs)
                                    ▼
┌──────────────────────────────────────────────────────────────────────────┐
│                        Upstream AI Providers                             │
│  ┌─────────────┐ ┌─────────────┐ ┌─────────────┐ ┌─────────────┐        │
│  │  Anthropic  │ │   Google    │ │   OpenAI    │ │   Alibaba   │        │
│  │  (Claude)   │ │  (Gemini)   │ │  (Codex)    │ │   (Qwen)    │        │
│  └─────────────┘ └─────────────┘ └─────────────┘ └─────────────┘        │
└──────────────────────────────────────────────────────────────────────────┘
```

---

## 7. Key Components Deep Dive

### 7.1 Configuration System ([`internal/config/config.go`](internal/config/config.go))

The configuration system supports:

- **YAML-based configuration**: Human-readable config files
- **Hot-reloading**: Runtime configuration updates without restart
- **Multiple storage backends**: File, PostgreSQL, Git, Object Storage
- **Environment variable overrides**: `MANAGEMENT_PASSWORD`, `DEPLOY`, etc.

Key configuration sections:
- `port`/`host`: Server binding
- `auth-dir`: Token storage directory
- `gemini-api-key`, `codex-api-key`, `claude-api-key`: API key credentials
- `openai-compatibility`: External OpenAI-compatible providers
- `routing`: Credential selection behavior
- `quota-exceeded`: Behavior when quotas are exceeded

### 7.2 Auth Manager ([`sdk/cliproxy/auth/conductor.go`](sdk/cliproxy/auth/conductor.go))

The auth manager is the core orchestration component:

**Key Methods:**
- `Execute()`: Non-streaming request execution with credential selection
- `ExecuteStream()`: Streaming request execution
- `Register()`: Add new credentials
- `Refresh()`: Update credential tokens
- `OnResult()`: Handle execution results for quota tracking

**Credential States:**
- `active`: Available for selection
- `cooling`: Temporarily unavailable due to quota limits
- `expired`: Needs refresh
- `invalid`: Authentication failed

### 7.3 Provider Executors ([`internal/runtime/executor/`](internal/runtime/executor/))

Each provider has a dedicated executor:

| Executor | File | Purpose |
|----------|------|---------|
| `ClaudeExecutor` | `claude_executor.go` | Anthropic Claude API |
| `GeminiExecutor` | `aistudio_executor.go` | Google AI Studio/Gemini |
| `CodexExecutor` | `codex_executor.go` | OpenAI Codex/GPT |
| `CodexWebsocketsExecutor` | `codex_websockets_executor.go` | OpenAI WebSocket streaming |
| `AntigravityExecutor` | `antigravity_executor.go` | Antigravity provider |
| `QwenExecutor` | Base executor pattern | Alibaba Qwen |

### 7.4 Translator System ([`sdk/translator/`](sdk/translator/))

The translator system handles format conversion:

**Supported Translations:**
- OpenAI → Claude (request/response)
- OpenAI → Gemini (request/response)
- Claude → OpenAI (request/response)
- Gemini → OpenAI (request/response)

**Translation Process:**
1. Parse incoming request format
2. Transform to target provider format
3. Execute request
4. Transform response back to client format

### 7.5 Management API ([`internal/api/handlers/management/`](internal/api/handlers/management/))

Management endpoints for runtime administration:

| Endpoint | Handler | Purpose |
|----------|---------|---------|
| `GET /manage/config` | `GetConfig` | Retrieve current configuration |
| `POST /manage/config` | `UpdateConfig` | Update configuration |
| `GET /manage/auths` | `ListAuths` | List all credentials |
| `POST /manage/auths` | `AddAuth` | Add new credential |
| `DELETE /manage/auths/:id` | `DeleteAuth` | Remove credential |
| `GET /manage/logs` | `StreamLogs` | Real-time log streaming |
| `GET /manage/usage` | `GetUsage` | Usage statistics |

---

## 8. Security Considerations

### 8.1 Authentication

- **API Key Validation**: Incoming requests validated against configured API keys
- **Management Password**: Separate password for management endpoints
- **Localhost Restriction**: Management endpoints restricted to localhost by default
- **OAuth PKCE**: Proof Key for Code Exchange for OAuth flows

### 8.2 TLS/HTTPS

- Optional TLS support via configuration
- Certificate and key file paths configurable
- uTLS for TLS fingerprinting bypass (anti-detection)

### 8.3 Token Storage

- Tokens stored in encrypted files or external stores
- PostgreSQL backend for enterprise deployments
- Git backend for version-controlled token storage
- Object storage (S3-compatible) for distributed deployments

---

## 9. Deployment Options

### 9.1 Docker

```bash
docker build -t cliproxyapi .
docker run -p 8080:8080 -v ./config.yaml:/app/config.yaml cliproxyapi
```

### 9.2 Docker Compose

```yaml
services:
  cliproxyapi:
    build: .
    ports:
      - "8080:8080"
    volumes:
      - ./config.yaml:/app/config.yaml
      - ./auths:/app/auths
```

### 9.3 Binary

```bash
go build -o cliproxyapi ./cmd/server
./cliproxyapi -config config.yaml
```

### 9.4 Cloud Deploy

Set `DEPLOY=cloud` environment variable for cloud-optimized behavior:
- Waits for valid configuration before starting
- Optimized for containerized environments

---

## 10. Extensibility

### 10.1 Custom Providers

The SDK supports custom provider implementations:

1. Implement `ProviderExecutor` interface
2. Register with auth manager
3. Add model definitions to registry
4. Configure in `config.yaml`

See [`examples/custom-provider/`](examples/custom-provider/) for a complete example.

### 10.2 Custom Translators

Add new format translations:

1. Implement `RequestTransform` and `ResponseTransform`
2. Register with translator registry
3. Specify format in executor configuration

### 10.3 Storage Backends

Implement custom token storage:

1. Implement `Store` interface from [`sdk/auth/interfaces.go`](sdk/auth/interfaces.go)
2. Register with `sdkAuth.RegisterTokenStore()`

---

## 11. Monitoring & Observability

### 11.1 Logging

- Structured logging via logrus
- Request/response logging with configurable retention
- Log rotation via lumberjack
- Real-time log streaming via management API

### 11.2 Metrics

- Usage statistics tracking
- Quota monitoring
- Request counting
- Token usage aggregation

### 11.3 Health Checks

- Root endpoint (`/`) returns server status
- Management API provides detailed health information
- Pprof endpoint for profiling (optional)

---

## 12. Summary

CLIProxyAPI is a well-architected Go application that solves the complex problem of bridging CLI-based AI authentication with standard API interfaces. Its modular design, extensive provider support, and robust credential management make it suitable for both individual developers and enterprise deployments.

**Key Strengths:**
- Clean separation of concerns (SDK vs internal packages)
- Pluggable architecture (executors, translators, storage)
- Comprehensive provider support
- Production-ready features (logging, monitoring, hot-reload)
- Active community and ecosystem

**Architecture Highlights:**
- Provider/Executor pattern for extensibility
- Translator registry for format conversion
- Auth conductor for credential orchestration
- Layered design for maintainability
