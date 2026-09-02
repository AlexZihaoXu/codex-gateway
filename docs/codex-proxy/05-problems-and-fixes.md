# Problems encountered and fixes

This incident log records symptoms, confirmed causes, and regression boundaries.

## Architecture correction

### We assumed OpenCode used `codex app-server`

**Symptom:** the production image downloaded Codex and the Go process supervised an
app-server child even though the desired gateway needed only login and inference.

**Cause:** similarity at the product level was mistaken for similarity at the
transport boundary.

**Investigation:** OpenCode was shallow-cloned at a pinned commit under ignored
`.experiments/`. Its OAuth plugin, provider transform, Responses lowering, stream
parser, and tests were traced end to end. It directly authenticates and requests the
ChatGPT Codex Responses endpoint.

**Fix:** replace the child process with an independently implemented Go OAuth/SSE
adapter. The production Dockerfile now contains no Codex installer, `CODEX_HOME`,
Node, or Bun runtime. See the [audit](07-opencode-direct-transport-audit.md).

### A direct clone risked becoming a hidden dependency

**Fix:** `.experiments/` is always ignored, production code cannot import it, and
the audit records the exact upstream commit and inspected files. Ideas are captured
as local tests before being implemented.

## OAuth

### A request-scoped login poll would stop immediately

**Cause:** the HTTP context ends after the dashboard receives the device code, while
the user still needs time to authorize in another page.

**Fix:** device polling has its own cancelable background context. The login ID maps
to its cancel function; cancel, logout, and shutdown are idempotent.

### Concurrent requests could refresh the same token repeatedly

**Fix:** one refresh operation owns a shared completion channel. Other callers wait
for it. A concurrent regression holds the refresh response and proves only one
token request occurs.

### Account metadata disappeared after refresh

**Cause:** ID and access tokens can hold different useful claims, and refresh
responses can omit a new refresh token or metadata.

**Fix:** merge identity metadata across tokens and preserve prior metadata/token
fields when absent. Tokens are committed atomically before use.

## Streaming and usage

### Stream output appeared buffered

**Fix:** the upstream body is parsed incrementally and downstream SSE writers flush
each record. Contract tests force multiple upstream flushes and verify deltas before
completion.

### A truncated stream looked successful

**Fix:** success requires one recognized terminal response with usage. EOF after a
delta, malformed JSON, `[DONE]` before a terminal event, and explicit failure events
all fail.

### Totals and cached usage looked wrong

**Cause:** cached input and reasoning output were treated as extra tokens, or an
upstream cumulative/inconsistent total was trusted.

**Fix:** treat them as subsets, use terminal usage, and normalize total to input plus
output. Dashboard and storage consume the same completed result.

### Cache identity alone did not prove caching

**Fix:** retain a stable per-conversation key and replay the completed encrypted
reasoning/message items for `store: false` follow-ups. Tests distinguish the routing
key from authoritative terminal `cached_tokens`.

### Cache-eligible HTTP follow-ups still reported zero

**Symptom:** a repeated 3,557-token Luna prefix used the same cache and session IDs,
but its HTTP follow-up still reported zero cached tokens. Adding the public GPT-5.6
`prompt_cache_options` object made the subscription backend reject the request with
HTTP 400.

**Investigation:** a protocol-matched experiment reused one Responses WebSocket for
the same two turns and reported an 8,960-token cache read. The first Go end-to-end
probe then reported a 2,816-token read for a 3,577-token follow-up. The short
quantization, Flappy Bird, and play-instructions scenario remained at 39, 52, and 61
input tokens, correctly below OpenAI's 1,024-token cache minimum.

**Fix:** pool one Go WebSocket per active conversation, send `response.create`
messages on it, retry setup once, and use HTTP SSE only as a setup fallback. Close
pooled sockets on invalid terminal events, logout, and shutdown. Continue displaying
only terminal `cached_tokens`; never synthesize a hit from cache identity or context
length.

### Reasoning activity headings ran together

**Cause:** the stream parser appended reasoning-summary deltas but discarded their
`summary_index`, and the terminal fallback concatenated summary array entries with
no boundary.

**Fix:** preserve each upstream summary-part boundary as a Markdown paragraph break
in both incremental and terminal-only paths. Contract tests reconstruct emitted
deltas and verify the same separated text stored in the completed result.

### A fast follow-up could race terminal cleanup

**Symptom:** the first Dashboard Chat request completed, but an immediate second
turn could receive a sanitized 502 because the in-memory conversation still looked
active. This also made live cache probes inconsistent.

**Cause:** the transport published `GenerationCompleted` before its deferred state
commit cleared the active flag and retained the completed Responses output items.

**Fix:** commit successful conversation state before publishing the terminal event.
A live two-turn Luna test now observes a cold first request, a cached follow-up, and
a dashboard total equal to both terminal totals.

## Compatibility and UI

### Chat history was flattened into one synthetic user message

**Cause:** the app-server adapter previously accepted a transcript string.

**Fix:** retain the fallback transcript but pass typed user and assistant messages
to the direct adapter, matching Responses input roles.

### A refreshed Chat page lost its conversation

**Cause:** completed display messages lived only in browser `sessionStorage`, so
they were tied to one browser tab and were lost outside that storage lifetime.

**Fix:** persist only successful completed Dashboard Chat transcripts in SQLite,
keyed by normalized authenticated Google email and session ID. Reload them through
a typed administrator endpoint. Keep partial streams and opaque encrypted
reasoning in memory, and preserve authoritative assistant usage when a later turn
resubmits the visible transcript without historic usage fields.

### Saved chat controls and rendering were incomplete

**Fix:** add owner-scoped rename and confirmed delete operations, server-persisted
reasoning summaries, streaming Markdown rendering, real-delta entrance transitions,
and edit-and-regenerate. The composer clears synchronously on submit and restores
the submitted text only after a failed request, without overwriting a newer draft.
Editing truncates later visible turns and explicitly restarts the matching in-memory
continuation before replaying the edited history.

The reasoning activity label remains generic unless the emitted summary begins with
a standalone bold heading. That real heading can become the label; no model activity
title or hidden reasoning text is synthesized by the UI. The live renderer defers an
incomplete trailing GFM table until its delimiter row arrives, so it does not flash
between paragraph and table layouts.

Completed fenced HTML blocks expose a preview action. The document opens beside the
conversation and can expand into a full-size HeroUI dialog. It runs in an iframe
without same-origin access; a gateway-injected content security policy blocks
network requests, frames, objects, forms, and non-inline resources while retaining
inline scripts for interactive local examples.

Each assistant answer labels its terminal token record as `Turn`. A separate
`Conversation` summary adds every saved assistant record, including the exact
upstream cached-input subsets, so cumulative usage is visible without changing API
response semantics.

### Refreshing a dashboard section returned to Overview

**Cause:** the selected HeroUI tab existed only in React component state.

**Fix:** map each tab to a stable client path, initialize from `location.pathname`,
push history on selection, and handle `popstate`. The Go SPA fallback serves the
embedded dashboard for direct requests to each client path.

### `/docs` could not parse and the API page later crashed

**Fix:** Huma owns one OpenAPI 3.1 document, Swagger UI's published renderer owns
`/doc`, and `/docs` redirects. Tests parse the generated documents and mount the
real library rather than a custom docs page.

## Remaining version-sensitive areas

- The ChatGPT Codex endpoint and OAuth/device shapes are not a documented public
  contract.
- Model IDs and supported efforts change over time.
- Exact fixed prompt overhead, cache thresholds, and terminal event additions are
  service behavior.
- The public OAuth client may evolve its required flags or headers.

Capture drift as a failing synthetic contract test, re-audit a pinned upstream
source, and change the smallest isolated adapter surface.
