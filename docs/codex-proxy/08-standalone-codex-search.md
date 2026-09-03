# Standalone Codex search

## Audited implementation

The implementation was checked against OpenAI Codex source commit
`728cb12fe5794b0c3a8e776fb4994b1650b973a8`. The disposable checkout lives under
the gitignored `.experiments/codex` directory.

Current Codex has two web-search mechanisms:

- a hosted `web_search` tool attached to a Responses request; and
- a standalone, non-streaming `POST alpha/search` endpoint used by the Codex web
  search extension.

Codex Gateway exposes the latter as `POST /v1/alpha/search`. For a ChatGPT-backed
provider, Codex resolves that relative path to
`https://chatgpt.com/backend-api/codex/alpha/search`. The gateway sends the
connected account's bearer token, optional ChatGPT account identifier, the
`codex_gateway` originator, and its versioned user agent. Callers never receive or
supply those credentials.

## Public contract

Clients authenticate with a project Bearer key. The request mirrors the typed
Codex search contract:

- `id`, `model`, optional reasoning controls, prior input, and output-token budget;
- batched `search_query`, `image_query`, `open`, `click`, `find`, `screenshot`,
  `finance`, `weather`, `sports`, and `time` commands;
- response length, location, search-context size, domain filters, image settings,
  allowed caller classes, and external-web access mode.

The response contains `output`, optional `encrypted_output`, and optional
structured `results`. Result objects are deliberately opaque maps: Codex itself
uses this design so newly introduced result variants and fields pass through
without requiring a gateway release.

The endpoint is fully described in the generated OpenAPI 3.1 document and Swagger
UI at `/doc`.

## Usage accounting

Standalone Codex search does not currently return input, output, cached, reasoning,
or total token counts. A successful call is therefore recorded against the
project/API key as one request with zero tokens. The gateway never invents token
usage.

## Verification

Unit tests verify authentication, the upstream path and headers, full request
forwarding, opaque result preservation, schema publication, and request
accounting. The opt-in live integration test sends one low-context Luna request
through the public gateway route and asserts that the upstream returns search
output and that the project records exactly one request:

```powershell
$env:CODEX_GATEWAY_LIVE_SEARCH = '1'
$env:CODEX_GATEWAY_LIVE_DATA_DIR = '<authenticated gateway data directory>'
go test -tags=integration ./internal/app -run TestLiveStandaloneSearchThroughGateway -count=1 -v
```

The live test never logs the query, response body, credentials, or plaintext API
key.
