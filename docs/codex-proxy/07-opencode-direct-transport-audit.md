# OpenCode direct transport audit

## Audit identity

On September 2, 2026, OpenCode was shallow-cloned into the ignored
`.experiments/opencode/` directory at upstream commit
`3a9d4e78b6b4509c2f7e91812a735e568e7f3f84`. The clone is research material and
is not a build input.

The relevant upstream files were:

- `packages/opencode/src/plugin/openai/codex.ts`
- `packages/opencode/src/provider/transform.ts`
- `packages/llm/src/providers/openai-options.ts`
- `packages/llm/src/protocols/openai-responses.ts`
- `packages/opencode/test/plugin/codex.test.ts`
- `packages/llm/test/provider/openai-responses.test.ts`

This document records observed behavior, not copied implementation. OpenCode is a
moving upstream and the ChatGPT Codex backend used by it is not a documented public
OpenAI Platform API. Every adopted behavior therefore needs a local contract test.

## What OpenCode actually does

OpenCode does not launch `codex app-server` for its Codex subscription inference
path. Its OpenAI plugin owns four responsibilities:

1. Authenticate against `https://auth.openai.com` using OAuth with PKCE.
2. Persist access and refresh material, refresh expired tokens, and coalesce
   concurrent refreshes.
3. Rewrite OpenAI Responses and Chat Completions provider requests to
   `https://chatgpt.com/backend-api/codex/responses`.
4. Attach the bearer token, `ChatGPT-Account-Id`, originator, user-agent, and a
   stable session identifier before forwarding the request.

Browser login uses a loopback callback. Headless login uses the Codex device flow:

```text
POST /api/accounts/deviceauth/usercode
  -> device_auth_id + user_code
  -> user visits https://auth.openai.com/codex/device
poll POST /api/accounts/deviceauth/token
  -> authorization_code + code_verifier
POST /oauth/token with the authorization code
  -> access_token + refresh_token + id_token + expires_in
```

HTTP 403 and 404 while polling mean “not authorized yet.” Other unexpected
statuses are errors. Refresh uses the OAuth `refresh_token` grant. The account ID
is read from the ID token first, then the access token, preferring the root
`chatgpt_account_id`, then its OpenAI auth namespace, then the first organization
identifier. JWT claims are metadata only; they are not locally treated as proof of
authorization.

## Request shape

OpenCode lowers model input to a Responses-shaped request and always streams it.
For GPT-5 reasoning models its important defaults are:

```json
{
  "model": "gpt-5.6-sol",
  "input": [],
  "stream": true,
  "store": false,
  "prompt_cache_key": "stable-session-id",
  "include": ["reasoning.encrypted_content"],
  "reasoning": {
    "effort": "medium",
    "summary": "auto"
  }
}
```

`prompt_cache_key` is a routing/cache identity, not a cached-token counter. The
actual cache result comes from terminal usage. With `store: false`, encrypted
reasoning output is carried forward as an opaque input item on a later turn. Any
reasoning item without encrypted state is excluded from a stateless continuation.

OpenCode uses a structured `input` array. It preserves user, assistant, tool, and
reasoning items rather than flattening a conversation into one prompt. Codex
Gateway only exposes text inference today, so its first independent implementation
will preserve user and assistant messages plus opaque encrypted reasoning, while
continuing to omit tools.

## Stream and accounting rules

The upstream response is real server-sent events. OpenCode handles, among other
events:

- `response.created` for response identity.
- `response.output_text.delta` for incremental visible text.
- `response.output_item.done` for completed message/reasoning items.
- `response.completed` for the final response and authoritative usage.
- `response.failed`, incomplete responses, malformed events, and premature EOF as
  failures.

The terminal usage object has these relationships:

```text
input_tokens_details.cached_tokens <= input_tokens
output_tokens_details.reasoning_tokens <= output_tokens
total_tokens = input_tokens + output_tokens
```

Cached input and reasoning output are subsets and must never be added to total a
second time. The gateway records usage only after one valid terminal completion.
If upstream supplies an inconsistent total, the compatibility layer normalizes it
to input plus output while retaining the subset fields.

## What Codex Gateway will adopt

- A Go-native device OAuth client matching the existing dashboard login UX.
- Atomic, owner-only local storage for OAuth state in the gateway data volume.
- Refresh-before-use with one in-flight refresh shared by concurrent callers.
- Direct streaming to the Codex Responses backend.
- A persistent Responses WebSocket per conversation, with bounded HTTP fallback,
  after live testing proved that cache-eligible HTTP continuation reported no cache
  read while socket reuse did.
- Stable per-conversation cache identity.
- Stateless `store: false` requests with encrypted reasoning requested and retained
  in memory for dashboard conversation continuity.
- Exact terminal usage accounting and real downstream SSE flushing.
- A configured, locally described model catalog because the direct transport does
  not provide the app-server model-list method. The gateway retains its prior
  `ultra` effort as a compatibility extension beyond the audited OpenCode catalog.

## What Codex Gateway will not adopt initially

- OpenCode's full WebSocket lifecycle policy. The gateway adopts only the required
  per-conversation connection reuse and bounded setup fallback; it does not copy
  OpenCode's environment switches, title-request routing, retry budget, or agent
  integration.
- OpenCode's general agent/tool loop, filesystem context, plugin context, or system
  prompt. The gateway sends only its explicit safety instruction, caller
  instructions, and caller conversation.
- OpenCode's TypeScript libraries or auth storage format. The implementation is
  independent Go code using the standard library.
- Server-side persistence of public compatibility prompts/responses or encrypted
  reasoning. Dashboard Chat later added explicit owner-scoped persistence for
  completed visible transcripts and terminal usage; continuation state remains
  process memory only.

## Migration acceptance criteria

- The production image contains one Go application process and no Codex, Node, or
  Bun runtime.
- Existing dashboard account routes retain their public schema.
- Device login, cancellation, logout, refresh, and restart persistence are covered
  without exposing token material.
- Direct request contract tests assert headers and body without contacting OpenAI.
- Stream tests split records across arbitrary reads and cover terminal success,
  upstream failure, malformed JSON, premature EOF, usage subsets, and total-token
  normalization.
- Public Responses and Chat Completions output retain their current schemas.
- Dashboard multi-turn tests prove stable cache identity and encrypted reasoning
  carryover, while separate storage tests prove completed transcript persistence
  without persisting encrypted reasoning.
- Docker development and production profiles preserve one-command startup.
