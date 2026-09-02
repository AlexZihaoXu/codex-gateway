# Streaming, caching, and token accounting

## The upstream stream is real

Codex Gateway reads upstream events as they arrive over a pooled Responses
WebSocket for conversations, or from an HTTP SSE body after setup fallback.
`response.output_text.delta` becomes an internal answer delta and reasoning-summary
delta events remain a separate internal stream; the public SSE writer flushes each
mapped record. No timer or client-side slicing simulates tokens.

The request asks upstream for `reasoning.summary: "auto"`. Most summaries arrive as
`response.reasoning_summary_text.delta` events. If Codex supplies a summary only on
the terminal reasoning output item, the adapter emits that real summary as one final
reasoning delta before completion. It never fabricates or exposes hidden chain of
thought.

Every summary-text delta includes a `summary_index`. When that index changes, the
adapter inserts a Markdown paragraph boundary before forwarding the next real
summary part. Terminal-only summary arrays use the same boundary rule. Without it,
adjacent bold activity headings render as one run-on heading.

The parser tolerates CRLF and LF framing, event-name lines, multiple `data:` lines,
and arbitrary network chunk boundaries. `response.completed` and
`response.incomplete` are terminal finishes. `response.failed`, `error`, malformed
JSON, a missing usage object, or EOF before a terminal event are failures.

## Authoritative usage

Only terminal response usage is recorded:

- `input_tokens`
- `input_tokens_details.cached_tokens`
- `output_tokens`
- `output_tokens_details.reasoning_tokens`
- `total_tokens`

Cached input is a subset of input; reasoning is a subset of output. The gateway
clamps invalid subset values and normalizes the public/stored total:

```text
total_tokens = input_tokens + output_tokens
```

It never computes input + cached + output + reasoning. A deliberately inconsistent
upstream total is covered by a regression test.

Usage is inserted immediately before the successful public terminal event or
non-streaming response. A failed/interrupted request is not recorded. Unique request
IDs prevent duplicate accounting.

Per-turn terminal usage remains immutable. Dashboard Chat additionally sums the
saved assistant-turn usage records and labels that value `Conversation`; it does not
rewrite an individual OpenAI-compatible response into a cumulative response. This
keeps compatibility semantics intact while making the total usage of a saved chat
visible.

## Why a small message can have large input usage

Input usage includes upstream-managed context and fixed service instructions, not
only visible conversation characters. Never infer protocol behavior from a model's
explanation of its own hidden context, and never assert an exact first-turn token
count in a live test.

## Cache identity versus cache result

`prompt_cache_key` and `session-id` keep one dashboard conversation on a stable
routing/cache identity. They do not report a cache hit. Only terminal
`cached_tokens` does.

The Codex subscription backend currently rejects the public API's
`prompt_cache_options` field. The gateway therefore follows OpenCode's audited
transport: a stable key plus a persistent Responses WebSocket. A bounded handshake
retry precedes HTTP fallback. HTTP remains functionally correct, but a cache-eligible
live probe reported no reads over HTTP and a real read after WebSocket reuse.

GPT-5.6 requires at least 1,024 visible input tokens in an identical reusable
prefix. A three-turn conversation made only of short prompts can correctly remain
at zero cached tokens even when one generated answer is long: generated output must
first appear in a subsequent input before it can be part of a later matching cache
prefix. Never infer a cache defect from a below-threshold conversation.

For `store: false` continuation, a completed response's output array can include an
opaque reasoning item. The gateway requests `reasoning.encrypted_content` and
replays that item with prior user/assistant messages in process memory. This is the
stateless continuation pattern OpenCode uses.

Prompt caching remains prefix- and service-dependent. A robust live probe uses a
long unique prefix, an identical conversation ID/model/instructions, then a short
follow-up. It asserts valid subset arithmetic and expects the later cached count to
increase, without requiring an exact number.

The completed conversation state is committed before the terminal event becomes
observable to a caller. This matters because a UI can submit its next turn as soon
as it sees completion; publishing completion first could briefly reject that valid
follow-up as an overlapping generation. The live Luna regression exercises this
boundary and verifies both the cache hit and the sum of the two dashboard turns.

## Attribution

Every completion carries a project ID and a source/API-key ID. Dashboard Chat uses
a deterministic internal source ID rather than a usable secret. General dashboard
totals are current-month views; each project limit also calculates its own fixed
UTC window from the same immutable request records.
