# Dashboard web-search tool loop

Dashboard Chat can opt into model-directed web search without exposing a general
tool runtime. Public `/v1/*` compatibility requests remain tool-free.

## Request loop

1. The administrator enables **Web Search** for a Dashboard Chat turn.
2. The gateway sends the Responses request with the namespaced `web.run` tool
   schema used by current Codex clients. Because this is a reserved tool, its
   name, descriptions, and parameter schema must match the upstream wire contract
   exactly; an approximate schema is rejected.
3. Codex may emit a `function_call` whose namespace is `web` and name is `run`.
4. The gateway validates its JSON arguments and calls
   `/backend-api/codex/alpha/search` with the authenticated Codex transport.
5. The search response is returned to Codex as a `function_call_output` item.
6. Codex continues the same logical turn and produces the user-facing answer.

The loop permits at most eight search calls per turn. Unknown tools, malformed
arguments, and oversized arguments fail closed. The browser receives structured
search lifecycle events and source metadata, but not the raw upstream search
payload.

Completed tool-call items can arrive in `response.output_item.done` even when the
terminal response has an empty `output` array. The stream parser retains those
items so the tool loop does not mistake the first model round for a final answer.

## Streaming contract

`POST /api/chat/stream` may interleave these gateway-owned SSE events with its
selected OpenAI-compatible stream:

- `gateway.web_search.started` identifies the call and display query.
- `gateway.web_search.completed` adds validated source titles, URLs, and snippets.

The Chat UI renders an in-progress search indicator, followed by a disclosure with
safe `http` or `https` source links. Search is disabled by default and its setting
is stored with the owner-scoped chat session.

## Usage and persistence

All model rounds in one user turn are aggregated into one authoritative usage
record. Search itself has no token usage object and does not create a second
project request. Completed source metadata is stored with the assistant message;
raw search output, partial model output, and opaque reasoning are not persisted.

## Verification

Unit and HTTP contract tests use captured wire shapes. The explicitly gated live
integration test `TestLiveLunaWebSearchToolLoop` verifies the complete transport
sequence against an authenticated account: model tool call, standalone search,
tool output, continued streamed answer, and aggregated terminal usage.
`TestLiveDashboardWebSearchStreamsPersistsAndTracksUsage` additionally drives the
administrator HTTP handler and verifies search SSE, SQLite history, and dashboard
usage attribution end to end.
