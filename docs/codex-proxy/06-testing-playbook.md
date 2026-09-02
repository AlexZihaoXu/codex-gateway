# Testing playbook

Most changes are proven against `httptest` servers and in-memory databases. Live
tests are explicit opt-ins because they contact Codex and consume account usage.

## Before testing

1. Read [`AGENTS.md`](../../AGENTS.md) and this series.
2. Inspect `git status --short` and preserve unrelated changes.
3. Identify the boundary: OAuth, request lowering, upstream SSE, public mapping,
   accounting, policy, or browser parsing.
4. Add a failing regression before implementation.
5. Never print tokens, credential files, Bearer keys, prompts, or generated text.

## Test layers

| Layer              | Primary files                               | Proves                                      |
| ------------------ | ------------------------------------------- | ------------------------------------------- |
| Direct transport   | `internal/codex/direct_client_test.go`      | OAuth, refresh, headers/body, SSE, state    |
| HTTP compatibility | `internal/app/openai_compatibility_test.go` | auth, schema mapping, terminal usage        |
| Dashboard Chat     | `internal/app/chat_playground_test.go`      | admin auth, continuation, attribution       |
| Chat persistence   | `internal/chats/*_test.go`                  | SQLite restart, owner isolation, usage      |
| Policy/storage     | `internal/projects/*_test.go`               | hashes, limits, immutable usage, aggregates |
| OpenAPI/app        | `internal/app/app_test.go`                  | paths, fields, aliases, routing             |
| Browser            | `web/src/*test.ts*`                         | strict SSE parsing and interactions         |
| Docker contract    | `scripts/check-compose.ts`                  | profiles and no removed child runtime       |
| Live transport     | `internal/codex/live_test.go`               | authenticated direct request and cache      |
| Live HTTP matrix   | `internal/app/live_test.go`                 | end-to-end SSE and persisted usage          |

## Narrow commands

```powershell
go test ./internal/codex -run '^TestDirectClientStreamsResponsesAndCarriesConversationState$' -count=1
go test ./internal/app -run '^TestChatCompletionsEndpointStreamsChunksAndDoneSentinel$' -count=1
go test ./internal/projects -run '^TestUsageSummaryAggregatesProjectsWithoutContent$' -count=1
go test ./internal/chats -run '^TestSessionsPersistAcrossRestartAndRemainOwnerScoped$' -count=1
bun run test:web -- --run web/src/chat-api.test.ts
bun run test:compose
```

Before completion:

```powershell
bun run verify
```

## Direct transport fixture rules

A good `httptest` regression:

1. Asserts method, safe header presence, and decoded request structure without
   printing authorization.
2. Returns realistic device/token JSON or Responses SSE.
3. Splits events across flushes and mixes CRLF/LF where framing matters.
4. Includes terminal usage with cached/reasoning subsets.
5. Supplies an inconsistent upstream total when testing normalization.
6. Covers explicit failure and premature EOF beside success.
7. Uses synthetic JWTs and placeholder secrets only.

Never commit a real trace. If diagnosing live behavior, translate only the necessary
shape into a sanitized fixture.

## Stream checklist

- At least one upstream delta is observable before the terminal event.
- Responses creation precedes public deltas and completion follows done records.
- Chat role/content/stop/usage/`[DONE]` order is correct.
- A terminal usage object is required and stored exactly once.
- Dashboard Chat history is written only after terminal usage succeeds.
- Reloaded assistant messages retain each turn's terminal usage metadata.
- Failure paths emit no successful terminal event or usage row.
- Context cancellation stops the request and releases the conversation lock.

For every completed request:

```text
input >= 0
0 <= cached input <= input
output >= 0
0 <= reasoning output <= output
total = input + output
```

## Opt-in live tests

Live tests load `codex-auth.json` from `CODEX_GATEWAY_LIVE_DATA_DIR`. Do not copy or
print the file. In the development container the directory is `/data/gateway`.

```powershell
$env:CODEX_GATEWAY_LIVE_DATA_DIR = 'path-to-gateway-data'
$env:CODEX_GATEWAY_LIVE_TEST = '1'
go test -tags=integration ./internal/codex -run '^TestLiveModelListAndEphemeralGeneration$' -count=1 -v
Remove-Item Env:CODEX_GATEWAY_LIVE_TEST
Remove-Item Env:CODEX_GATEWAY_LIVE_DATA_DIR
```

Run the full GPT-5.6 compatibility matrix only with explicit intent:

```powershell
$env:CODEX_GATEWAY_LIVE_DATA_DIR = 'path-to-gateway-data'
$env:CODEX_GATEWAY_LIVE_MATRIX = '1'
go test -tags=integration ./internal/app -run '^TestLiveCompatibilityStreamingMatrix$' -count=1 -v
Remove-Item Env:CODEX_GATEWAY_LIVE_MATRIX
Remove-Item Env:CODEX_GATEWAY_LIVE_DATA_DIR
```

For credentials held only in the Compose volume:

```powershell
docker compose --profile dev run --rm `
  -e CODEX_GATEWAY_LIVE_DATA_DIR=/data/gateway `
  -e CODEX_GATEWAY_LIVE_DASHBOARD=1 `
  gateway-dev `
  go test -tags=integration ./internal/app `
  -run '^TestLiveDashboardLunaConversationCachingAndTotals$' -count=1 -v
```

This consumes live usage. Normal verification never does.

## Container smoke test

```powershell
docker compose --profile dev up --build -d
bun run test:container
```

Do not use `down -v`; it would remove persisted local state.

## Debugging safely

- Login: test synthetic pending/success/error statuses; never dump the credential
  file.
- Refresh: confirm a single request with counters, not token values.
- Missing model: compare the pinned audit and account rejection; do not assume every
  account has every listed model.
- Stalled stream: distinguish upstream event delivery from downstream flushing with
  a streaming client.
- Wrong usage: inspect sanitized terminal field names and subset arithmetic.
- Flat caching: prove stable cache identity and replayed prefix before blaming
  service caching.
- Drift: update the pinned source audit, then a failing fixture, then production.

## Definition of done

- Regression was red before implementation and is green afterward.
- Narrow and integration-build tests pass.
- Huma/OpenAPI is synchronized if the public contract changed.
- Terminal usage equals persisted attribution.
- Production Docker contains no Codex, Node, or Bun runtime.
- No logs, tests, or diffs reveal sensitive material.
- `bun run verify` passes.
