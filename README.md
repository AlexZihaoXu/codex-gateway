# Codex Gateway

[![CI required](https://img.shields.io/badge/CI-required-2088FF?style=flat-square&logo=githubactions&logoColor=white)](.github/workflows/ci.yml)
[![Coverage 80%+](https://img.shields.io/badge/coverage-80%25%2B-2F855A?style=flat-square&logo=go&logoColor=white)](#engineering-standards)
[![Conventional Commits](https://img.shields.io/badge/commits-conventional-FE5196?style=flat-square&logo=conventionalcommits&logoColor=white)](CONTRIBUTING.md#conventional-commits)
[![Local first](https://img.shields.io/badge/network-local--first-6E7681?style=flat-square&logo=shield&logoColor=white)](#security-posture)

[![Go](https://img.shields.io/badge/Go-runtime-00ADD8?style=flat-square&logo=go&logoColor=white)](go.mod)
[![Bun](https://img.shields.io/badge/Bun-build%20tooling-FBF0DF?style=flat-square&logo=bun&logoColor=14151A)](package.json)
[![React](https://img.shields.io/badge/React-dashboard-61DAFB?style=flat-square&logo=react&logoColor=111827)](web/src/App.tsx)
[![HeroUI](https://img.shields.io/badge/HeroUI-v3-000000?style=flat-square)](https://heroui.com/)
[![Firebase](https://img.shields.io/badge/Firebase-Google%20auth-FFCA28?style=flat-square&logo=firebase&logoColor=111827)](https://firebase.google.com/docs/auth/web/google-signin)
[![Docker Compose](https://img.shields.io/badge/Docker-Compose-2496ED?style=flat-square&logo=docker&logoColor=white)](compose.yaml)
[![OpenAPI 3.1](https://img.shields.io/badge/OpenAPI-3.1-6BA539?style=flat-square&logo=openapiinitiative&logoColor=white)](internal/app/app.go)

Codex Gateway is a self-hosted compatibility gateway that exposes a focused OpenAI-compatible API backed by an authenticated Codex account.

The intended experience is one command and one local service:

```powershell
docker compose --profile prod up
```

Then open:

- Dashboard: <http://localhost:11435>
- API reference: <http://localhost:11435/doc>
- OpenAPI schema: <http://localhost:11435/openapi.json>

> [!IMPORTANT]
> Codex Gateway is an independent compatibility project. It does not convert ChatGPT subscription credits into OpenAI Platform API credits. It uses the same direct ChatGPT Codex OAuth and Responses transport pattern audited in OpenCode and is intended for trusted, private environments. That transport is not a documented public OpenAI Platform API and may change.

## Current capabilities

- A single Go production process with the compiled React dashboard embedded
- OpenAI-compatible model listing, Responses, Chat Completions, and streaming SSE routes
- A Go-native ChatGPT Codex OAuth client and direct Responses SSE transport
- Browser-assisted Codex device login and account readiness status
- Google-only Firebase authentication with an administrator account, an explicit email allowlist, and persistent local sessions
- Project-scoped, revocable API keys whose plaintext is shown only at creation
- Multiple per-project token limits across minute, hourly, daily, weekly, and monthly windows
- Detailed input, cached-input, output, reasoning-output, and total-token accounting
- A keyless, administrator-session Chat playground with project-scoped usage tracking and owner-scoped SQLite history
- A HeroUI dashboard with Overview, Projects, API keys, Usage, Chat, and Settings tabs
- Generated OpenAPI 3.1 schemas and a Swagger UI reference at `/doc`
- Loopback-only Docker publishing by default
- Dev and production Compose profiles
- Test, coverage, formatting, lint, type-check, commit-message, and CI gates

This is an early preview. The compatibility layer intentionally supports a smaller surface than the complete OpenAI API; see [Compatibility scope](#compatibility-scope).

## Runtime design

```text
Build time                              Production
----------                              ----------
Bun + Vite --> React assets ----+
                                 +--> Go executable --> HTTP API + embedded UI
Go compiler ---------------------+            |
                                              +--> ChatGPT Codex HTTPS/SSE
```

React runs in the browser. Bun is used only for frontend development, tests, formatting, and the container build stage. The production image runs one Go application process and contains no Codex, Node, or Bun runtime.

## Architecture

```text
OpenAI-compatible client
          |
          | Bearer project API key
          v
Go API + authentication + quota policy ------> SQLite
          |                                      |
          |                                      +--> sessions, projects, key hashes, usage, dashboard chats
          +------------------------------------> owner-only Codex OAuth store
          |
          | HTTPS + server-sent events
          v
ChatGPT Codex Responses transport
```

The dashboard Chat tab reaches the same inference and streaming pipeline through the administrator session. It selects a usage project instead of accepting a plaintext API key. Each dashboard conversation keeps opaque encrypted reasoning and active continuation state in gateway memory, uses a stable owner-scoped cache identity, and sends `store: false`; public compatibility requests remain stateless. After a turn completes successfully, its visible user/assistant messages, instructions, settings, and terminal usage are stored in SQLite under the authenticated Google email. Incomplete streams are not saved.

Inference requests contain no tools or execution environment and include a narrow text-only safety instruction. Public compatibility request and response content is not persisted. Dashboard Chat is the explicit exception: its completed visible transcript is persisted so conversations survive refresh and gateway restarts. OAuth tokens and opaque encrypted reasoning are never placed in SQLite, and structured logs omit request/response bodies.

## Quick start

Create the local environment file once, enter the public Firebase web application settings, and set the primary administrator email:

```powershell
Copy-Item .env.example .env
```

In the Firebase console, enable the Google sign-in provider. `FIREBASE_ADMIN_EMAIL` is the primary administrator. Additional Google accounts have no access unless their normalized email addresses are listed explicitly in the comma-separated `FIREBASE_ALLOWED_EMAILS` value. Restart the gateway after changing the allowlist.

Start the production profile:

```powershell
docker compose --profile prod up
```

Open <http://localhost:11435> and select **Continue with Google**. After the approved Google account is authenticated, select **Connect account** for Codex. The gateway requests a short-lived device code and the dashboard opens the official Codex device-login page.

After the account is connected:

1. Create a project and optionally add one or more usage limits.
2. Create an API key for that project.
3. Copy the key when it is shown. It cannot be retrieved later.

To test the gateway from the dashboard, open **Chat** and select the usage project. No project API key is required there.

Limits use fixed UTC windows. Each limit has a token budget, interval count, and unit, so a project can combine safeguards such as 50,000 tokens every hour, 500,000 every day, and 5,000,000 every month. Every configured limit is enforced.

Set the key in your shell without placing it in source control:

```powershell
$env:CODEX_GATEWAY_API_KEY = 'cg_replace_with_your_key'
```

List the available models:

```powershell
curl.exe http://127.0.0.1:11435/v1/models `
  -H "Authorization: Bearer $env:CODEX_GATEWAY_API_KEY"
```

Create a response:

```powershell
curl.exe http://127.0.0.1:11435/v1/responses `
  -H "Authorization: Bearer $env:CODEX_GATEWAY_API_KEY" `
  -H "Content-Type: application/json" `
  -d '{"model":"MODEL_FROM_V1_MODELS","input":"Reply with exactly OK."}'
```

Stream a chat completion:

```powershell
curl.exe --no-buffer http://127.0.0.1:11435/v1/chat/completions `
  -H "Authorization: Bearer $env:CODEX_GATEWAY_API_KEY" `
  -H "Content-Type: application/json" `
  -d '{"model":"MODEL_FROM_V1_MODELS","messages":[{"role":"user","content":"Reply with exactly OK."}],"stream":true,"stream_options":{"include_usage":true}}'
```

Use <http://localhost:11435/doc> for interactive schemas and request examples.

## Compatibility scope

Implemented compatibility routes:

```text
GET  /v1/models
POST /v1/responses
POST /v1/chat/completions
```

The current Responses API accepts string `input`, optional string `instructions`, and text streaming. Chat Completions accepts string-content `system`, `developer`, `user`, and `assistant` messages and preserves user/assistant roles as structured Responses input. Completed JSON objects include terminal usage; chat streams include usage when `stream_options.include_usage` is true. `total_tokens` is normalized to `input_tokens + output_tokens`; cached input and reasoning output are subsets of those totals and are never added a second time.

Tool calls, image/audio input, structured outputs, embeddings, files, fine-tuning, and the rest of the full OpenAI API are not implemented. Unsupported fields are rejected by the typed request schema instead of being silently ignored.

Project limits use UTC-aligned fixed windows measured in minutes, hours, days, weeks, or months. Multi-unit windows such as six hours or three months are anchored consistently, weekly windows begin Monday at 00:00 UTC, and the Usage tab shows the active window and reset time for every limit. A request is rejected once any recorded window total reaches its limit; simultaneous requests can finish slightly beyond a configured budget.

The Chat tab is an in-browser compatibility playground authenticated by the administrator session. Models and projects load automatically, sample prompts can seed the composer, and no API key or pre-created project is required. By default, usage lands in the built-in **Dashboard Chat** bucket; selecting a project attributes the turn there and enforces its limits. A random `session_id` links turns to in-memory encrypted continuation state and a stable cache key. Visible completed messages, chat settings, and authoritative per-turn usage are stored in SQLite, scoped to the authenticated Google email, and reloaded after refresh or restart. The encrypted continuation state remains memory-only; after a restart, the gateway reconstructs context from the saved visible messages.

Dashboard sections have stable paths: `/chat`, `/overview`, `/projects`, `/api-keys`, `/usage`, and `/settings`. Refreshing a section or using browser back/forward navigation preserves the selected tab.

## Local development

Requirements:

- Go 1.24.5 or later in the Go 1.24 release line
- Bun 1.3.6 or later
- Docker Desktop or Docker Engine with Compose

Install the frontend and repository tooling:

```powershell
bun install
```

Start the Go API and Vite dashboard directly:

```powershell
bun run dev
```

Or use the containerized development profile with live frontend updates:

```powershell
docker compose --profile dev up --build
```

The development and production profiles use separate data volumes and cannot bind the same host port at the same time. Stop one profile before starting the other.

When running directly, the dashboard is available at <http://localhost:5173> and proxies API requests to the Go server at <http://127.0.0.1:8080>.

### HeroUI design workflow

The dashboard uses HeroUI v3 and Tailwind CSS v4. This repository includes a project-scoped HeroUI MCP server configuration at [`.codex/config.toml`](.codex/config.toml). After `bun install`, restart Codex in this trusted repository so it discovers the server.

Before changing a webpage, use the `heroui-react` MCP server to inspect comparable components and reference implementations. Compose with HeroUI components instead of creating substitutes, and do not restyle HeroUI component internals without explicit approval. Swagger UI remains responsible for `/doc`.

Verify the local MCP package and its tools independently with:

```powershell
bun run mcp:heroui:check
```

The MCP server is development tooling only and is not included in the production runtime.

### Quality gate

Run the complete local gate:

```powershell
bun run verify
```

| Command                    | Purpose                                                   |
| -------------------------- | --------------------------------------------------------- |
| `bun run dev`              | Run the Go API and Vite dashboard                         |
| `bun run test:compose`     | Validate the development and production Compose profiles  |
| `bun run test:container`   | Check a running development container and its API surface |
| `bun run test:go`          | Run sequential Go tests with coverage                     |
| `bun run test:web`         | Run React tests with coverage                             |
| `bun run test:watch`       | Run the frontend TDD watch loop                           |
| `bun run format`           | Format Go, TypeScript, CSS, Markdown, and configuration   |
| `bun run format:check`     | Verify formatting without changing files                  |
| `bun run lint`             | Run `go vet` and ESLint                                   |
| `bun run mcp:heroui:check` | Verify the project-local HeroUI MCP server and tools      |
| `bun run typecheck`        | Run strict TypeScript checking                            |
| `bun run build`            | Build React and the production-tagged Go executable       |
| `bun run verify`           | Run every required local quality gate                     |

## Engineering standards

This repository requires:

- Test-driven development using red, green, refactor
- Conventional Commits for every commit
- Idiomatic Go and strict TypeScript
- Huma-backed runtime contracts for public HTTP endpoints
- At least 80% statement coverage for Go and frontend code
- Formatting, linting, type checking, tests, coverage, and production builds in CI
- Regression tests for every defect fix
- No credentials, authentication state, plaintext API keys, prompts, or responses in source control

See [CONTRIBUTING.md](CONTRIBUTING.md) and [AGENTS.md](AGENTS.md) before making changes.

Maintainers and testing agents should also read the
[Codex proxy knowledge series](docs/codex-proxy/README.md) before changing the
direct transport, OAuth, compatibility streams, caching, or token accounting.

## Service API

Administrator routes use the browser's HTTP-only session cookie:

```text
GET  /healthz
GET  /readyz

GET  /api/auth/config
GET  /api/auth/status
POST /api/auth/google
POST /api/auth/logout

GET  /api/codex/account
POST /api/codex/login/device
POST /api/codex/login/cancel
POST /api/codex/logout

GET  /api/projects
POST /api/projects
GET  /api/api-keys
POST /api/api-keys
DELETE /api/api-keys/{id}
GET  /api/usage
GET  /api/chat/sessions
DELETE /api/chat/sessions/{id}
GET  /api/chat/models
POST /api/chat/stream

GET  /doc
GET  /openapi.json
GET  /openapi.yaml
```

## Security posture

- Compose publishes the gateway on `127.0.0.1` by default.
- The production container runs as an unprivileged user with all Linux capabilities dropped.
- The container root filesystem is read-only; persistent state is restricted to `/data`.
- Dashboard access requires a verified Firebase ID token created through Google sign-in and an email present in the configured administrator allowlist.
- Firebase ID tokens are exchanged for an opaque, hashed, HTTP-only local session; Firebase tokens are not retained.
- API key plaintext is displayed once; only a SHA-256 hash and identifying prefix are stored.
- Codex OAuth credentials are stored atomically in the gateway data volume with owner-only file permissions and are never exposed through the API.
- Public compatibility request and response bodies are not retained. Completed Dashboard Chat transcripts are stored in owner-scoped SQLite history until deleted.
- Prompts sent through the gateway still leave the machine as part of the upstream Codex request.

Please report vulnerabilities privately as described in [SECURITY.md](SECURITY.md).

## Public repository and license

Contributor, issue, pull-request, security, dependency-update, and CI templates are included so the repository can be published cleanly.

No open-source license has been selected. The package is marked `UNLICENSED`, and all rights remain reserved until a license file is deliberately added.
