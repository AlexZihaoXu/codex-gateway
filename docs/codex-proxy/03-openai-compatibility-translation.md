# OpenAI compatibility translation

[`internal/app/openai_compatibility.go`](../../internal/app/openai_compatibility.go)
contains the public Huma contracts and maps internal generation events to a focused
OpenAI-compatible surface.

## Supported surface

| Route                       | Upstream behavior                        |
| --------------------------- | ---------------------------------------- |
| `GET /v1/models`            | Return the locally audited Codex catalog |
| `POST /v1/responses`        | Stream a direct Responses request        |
| `POST /v1/chat/completions` | Lower text messages to Responses input   |

Tool calls, attachments, audio, structured output, files, embeddings, fine-tuning,
and the remainder of the complete OpenAI API are intentionally unsupported.

## Authorization before inference

The Bearer header is parsed strictly. Invalid or revoked keys return 401, exhausted
project policy returns 429, local failures return 500, and Codex transport failures
return a sanitized 502. Unauthorized or limited calls never reach the transport.

## Responses mapping

The public Responses body accepts string input, optional instructions, reasoning
effort, and a stream flag. It becomes one structured user item. The internal
`started`, `delta`, and `completed` events produce either one completed response or
this SSE order:

```text
response.created
response.in_progress
response.output_item.added
response.content_part.added
response.output_text.delta *
response.output_text.done
response.content_part.done
response.output_item.done
response.completed
```

Every record is flushed. If the transport or usage insert fails, the stream ends in
`response.failed` and no successful terminal record is emitted.

## Chat Completions mapping

System and developer messages become explicit instructions. User and assistant
messages remain separate structured input items; they are not flattened for the
upstream request. The plain transcript string retained in the internal request is a
compatibility fallback only.

A non-streaming result has one assistant choice. Streaming emits an assistant-role
chunk, content delta chunks, a stop chunk, an optional empty-choice usage chunk,
and `[DONE]`.

## Dashboard Chat mapping

`/api/chat/stream` reuses the same encoders. Administrator session authentication
replaces the project key; an optional project selects attribution. Its opaque
`session_id` is combined with the normalized authenticated owner and hashed to
derive the stable upstream cache key and in-memory continuation key.
Chat Completions usage is always included so the browser can render it.

After terminal usage has been recorded, the dashboard completion callback writes
the completed visible transcript and per-assistant-message usage to owner-scoped
SQLite storage. `GET /api/chat/sessions` restores that history and
`DELETE /api/chat/sessions/{id}` removes one owned session. Both routes require the
administrator session. The browser treats their JSON as untrusted and validates
the full shape before rendering it.

Historical assistant usage is not sent back in later generation requests. During
an update, the chat store preserves usage for unchanged message positions and adds
the new terminal assistant usage atomically. No successful terminal browser event
is emitted when the completed transcript cannot be persisted.

## OpenAPI and `/doc`

Huma generates `/openapi.json` and `/openapi.yaml` from runtime types. `/doc` uses
the published Swagger UI renderer and `/docs` redirects there. Do not maintain a
second schema or recreate Swagger components. Every public behavior change needs a
Huma type update and an OpenAPI regression test.
