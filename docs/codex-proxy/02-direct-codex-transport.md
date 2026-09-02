# Direct Codex transport

The adapter in [`internal/codex/client.go`](../../internal/codex/client.go) converts
device OAuth plus Responses streaming events into a small Go interface.

## Account lifecycle

The dashboard retains four stable operations:

| Gateway action     | Direct operation                                         |
| ------------------ | -------------------------------------------------------- |
| Read status        | Read the protected local credential snapshot             |
| Start device login | Request a device/user code and start a background poll   |
| Cancel login       | Cancel the matching poll context                         |
| Disconnect         | Delete local tokens and all in-memory conversation state |

Device login starts with a JSON request to the issuer's device-code endpoint. The
gateway returns only a login ID, verification URL, and short-lived user code. It
polls in the background because the HTTP request that initiated login ends before
the user completes the browser interaction.

Polling statuses 403 and 404 mean “pending.” A successful poll yields an
authorization code and PKCE verifier, which are exchanged using a form-encoded
OAuth token request. Login cancellation and client shutdown cancel that work.

## Refresh and persistence

The client refreshes shortly before token expiry. One caller performs a refresh;
concurrent callers wait on the same in-flight operation and receive the same
result. A successful refresh is written atomically before it becomes active in
memory. The previous refresh token and account metadata are retained if the refresh
response omits replacements.

The token response can distribute metadata across tokens. The gateway prefers ID
token identity fields and fills missing account, plan, and email fields from the
access token. Compute residency comes from the access token and is sent only to the
Codex backend.

## Model catalog

The direct backend has no app-server `model/list` method. The gateway therefore
ships a narrow catalog audited from the pinned OpenCode model source:

- `gpt-5.6-sol`
- `gpt-5.6-terra`
- `gpt-5.6-luna`

The pinned OpenCode catalog exposes `none`, `low`, `medium`, `high`, `xhigh`, and
`max`. Codex Gateway also retains its previously advertised `ultra` compatibility
value for GPT-5.6, with `medium` as the default. Treat this list as compatibility
configuration: update the pinned audit and tests before changing it. A listed model
or effort can still be unavailable to a particular account; upstream remains
authoritative at request time.

## Request lowering

The gateway lowers one structured request for the Codex Responses endpoint:

```json
{
  "model": "gpt-5.6-sol",
  "input": [
    {
      "role": "user",
      "content": [{ "type": "input_text", "text": "Hello" }]
    }
  ],
  "instructions": "explicit gateway and caller instructions",
  "stream": true,
  "store": false,
  "prompt_cache_key": "stable opaque session id",
  "include": ["reasoning.encrypted_content"],
  "reasoning": { "effort": "medium", "summary": "auto" },
  "text": { "verbosity": "low" }
}
```

HTTP requests include `stream: true`. A conversation request normally opens or
reuses a Responses WebSocket and sends the same body as a `response.create` message,
without the HTTP-only `stream` field. If WebSocket setup fails twice, that request
falls back to HTTP SSE. A stream failure after events begin is never replayed.

The adapter sets bearer authorization, `ChatGPT-Account-Id` when known,
`originator: codex_gateway`, a versioned user agent, and `session-id`. A constrained
compute-residency claim is forwarded through the upstream residency header. The
WebSocket handshake also sends the audited `responses_websockets=2026-02-06` beta
header.

## Conversation state

An empty `ConversationID` creates one stateless request. A non-empty ID selects an
in-memory conversation:

- only one generation can be active for that ID;
- changing model or instructions replaces its state;
- successful terminal output items are retained as raw JSON;
- a later input replays earlier user/assistant output plus encrypted reasoning;
- a failed request leaves the previous completed state unchanged; and
- successful state is committed before the completion event is exposed; and
- shutdown or logout clears every conversation and closes every pooled socket.

This mirrors OpenCode's `store: false` continuation principle without importing its
agent runtime or persisting content.

## Failure behavior

Non-2xx responses, malformed events, explicit failure events, missing terminal
usage, oversized lines, and EOF before a terminal event all fail the generation.
HTTP handlers convert those failures into sanitized gateway errors; raw response
bodies and credentials are never forwarded.
