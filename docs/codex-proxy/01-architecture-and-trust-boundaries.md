# Architecture and trust boundaries

## Process topology

The production container runs one process: the Go gateway. React is compiled by Bun
at build time and embedded into that executable. No Codex, Node, or Bun process is
required at runtime.

```text
OpenAI-compatible client             Browser dashboard
          |                                 |
          | project Bearer key              | HTTP-only admin session
          +----------------+----------------+
                           |
                           v
                 Go HTTP gateway (Huma)
                  |       |        |
                  |       |        +--> embedded React assets
                  |       +-----------> SQLite policy + usage data
                  +-------------------> local Codex OAuth store
                  |
                  | HTTPS + real SSE
                  v
       ChatGPT Codex Responses transport
```

[`cmd/gateway/main.go`](../../cmd/gateway/main.go) opens the local authentication,
project, and dashboard-chat stores, creates the direct Codex client, and starts the HTTP server.
[`internal/codex/client.go`](../../internal/codex/client.go) is the only package that
knows OAuth URLs, upstream headers, or upstream stream shapes.

## Why the direct transport is isolated

The ChatGPT Codex transport is not a documented public OpenAI Platform API. It was
implemented from a pinned behavioral audit of OpenCode, described in
[the OpenCode audit](07-opencode-direct-transport-audit.md). Isolation limits drift:

- The HTTP layer sees typed models, account state, and three generation events.
- Local policy never handles OAuth tokens.
- Upstream field changes are captured first as adapter contract tests.
- Public Huma schemas do not expose private transport fields.

## Trust boundaries

### Public compatibility API

Every `/v1/*` request requires `Authorization: Bearer <project key>`. Only the
SHA-256 hash and identifying prefix of a key are stored. Authentication and every
configured usage limit are checked before inference. Completed usage is attributed
to both the project and the exact key.

### Dashboard API

Management and Chat routes require the administrator's HTTP-only local session.
Firebase verifies Google identity; the configured primary administrator and
explicit email allowlist control access. Dashboard chat uses a non-exportable local
usage identity and never needs a project API key.

The dashboard's upstream continuation/cache identity is an opaque SHA-256-derived
value scoped by normalized Google email and browser session ID. Neither source
value appears in that upstream identifier, and two approved users cannot share
in-memory continuation state by choosing the same session ID.

### Codex OAuth

The public OAuth client identifier is compiled into the adapter. Access tokens,
refresh tokens, account metadata, and expiry are stored atomically in
`DATA_DIR/codex-auth.json` with owner-only permissions where the platform supports
POSIX modes. The data volume is the credential boundary. Tokens are never returned
by an HTTP route or attached to logs.

JWT claims are decoded only to discover account, email, plan, and compute-residency
metadata. The gateway does not use an unverified decoded claim to authorize a local
user; dashboard authorization remains Firebase-backed.

### Inference

Requests use `store: false`, no tools, an explicit text-only safety instruction,
and a structured message array. Dashboard continuation state, including opaque
encrypted reasoning, exists only in process memory. Public calls remain stateless.

### Persistence and logs

SQLite stores local sessions, projects, key hashes, policies, request identifiers,
and token counts. Public compatibility prompts and model responses are not persisted.
Dashboard Chat is an explicit, user-facing exception: after a successful terminal
event, SQLite stores its visible user/assistant messages, instructions, selected
model/project/endpoint/reasoning effort, and terminal usage under the normalized
authenticated Google email. Partial output is not saved, and a different approved
Google user cannot list or delete another user's conversations.

Opaque encrypted reasoning and live continuation state remain process-memory only.
OAuth credentials stay in the owner-only Codex credential store. Structured logs
omit authorization, headers, and request/response bodies.

## Request path

```text
HTTP request
  -> Huma validates the typed body
  -> authenticate project key or administrator session
  -> enforce active project limits
  -> obtain or refresh the Codex OAuth access token
  -> lower text/messages into a stateless Responses request
  -> consume and validate upstream SSE
  -> emit normalized incremental OpenAI-compatible SSE
  -> record authoritative terminal usage exactly once
  -> persist a completed Dashboard Chat transcript when applicable
  -> emit the protocol-specific successful terminal event
```

Quota checks are not reservations. Concurrent requests close to a limit can all
pass before any completes, so limits are safeguards rather than strict concurrent
spend locks.
