# Codex proxy knowledge series

This series explains how Codex Gateway adapts the ChatGPT Codex Responses transport
into a focused OpenAI-compatible HTTP API. It is written for maintainers and
testing agents who need to change or verify the proxy without rediscovering its
protocol assumptions.

Codex Gateway is a local compatibility gateway backed by Codex. It does not turn a
ChatGPT plan into OpenAI Platform API credits, and it must not be described or
tested as if it does.

## Read in this order

1. [Architecture and trust boundaries](01-architecture-and-trust-boundaries.md)
2. [Direct Codex transport](02-direct-codex-transport.md)
3. [OpenAI compatibility translation](03-openai-compatibility-translation.md)
4. [Streaming, caching, and token accounting](04-streaming-and-token-accounting.md)
5. [Problems encountered and their fixes](05-problems-and-fixes.md)
6. [Testing playbook](06-testing-playbook.md)
7. [OpenCode direct transport audit and migration decision](07-opencode-direct-transport-audit.md)
8. [Standalone Codex search](08-standalone-codex-search.md)
9. [Dashboard web-search tool loop](09-dashboard-web-search-tool-loop.md)

## Sources of truth

When sources disagree, use this order:

1. Contract tests in [`internal/codex`](../../internal/codex) captured from the
   currently audited transport shapes.
2. HTTP contract tests in [`internal/app`](../../internal/app).
3. Current official OpenAI Responses and OAuth documentation where it applies.
4. The pinned OpenCode source identified in the direct transport audit.
5. These notes.

The ChatGPT Codex transport is not a documented public OpenAI Platform contract and
can evolve. Re-audit a pinned upstream implementation under `.experiments/`, update
the transport audit, then update tests before changing production parsing.

## Non-negotiable invariants

- The production application is one Go process. React is precompiled and embedded;
  no Codex, Node, or Bun child process runs in production.
- Public `/v1/*` inference requires a project Bearer key. Dashboard chat requires an
  administrator session and never exposes its internal usage identity.
- Standalone search also requires a project Bearer key. Its successful calls count
  as project requests, but not as tokens because the upstream search response has
  no token-usage object.
- Public `/v1/*` inference requests are stateless (`store: false`), contain no
  tools, and carry only explicitly supplied text plus in-memory continuation
  state. Dashboard Chat can explicitly opt into the single namespaced `web.run`
  tool; the gateway executes it through standalone Codex search and returns the
  result to the same model turn.
- Public compatibility prompt and response content is not persisted. Dashboard
  Chat explicitly stores only completed visible transcripts, settings, and
  terminal usage in owner-scoped SQLite rows; partial output and opaque encrypted
  reasoning are never persisted.
- Usage is recorded only after a successfully completed turn.
- `cached_input_tokens` is a subset of input, and reasoning tokens are a subset of
  output. Neither is added to `total_tokens` again.
- OpenAI-compatible `total_tokens` is always `input_tokens + output_tokens`.
- Streaming output is flushed after every SSE record and ends with the correct
  protocol-specific terminal event.
- Tests never print credentials, authentication state, full prompts, or generated
  responses.

## Documentation status

These files describe the repository as of September 3, 2026. Assertions labeled
"live" still require an explicitly enabled integration test against an authenticated
Codex installation; the normal quality gate intentionally does not spend live
account usage.
