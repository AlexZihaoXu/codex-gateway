# TODO

## UI issues

- [x] Make the desktop header a single compact row.
  - Keep the Codex Gateway brand on the left.
  - Place the existing HeroUI tabs in the same row without a full-width lower navigation bar.
  - Keep the loopback status and sign-out action on the right.
  - Allow a responsive fallback only when the viewport is too narrow for one row.
  - Add a regression test for the header structure before changing the implementation.
  - Use the HeroUI Tabs reference implementation and do not override component internals.
- [x] Center empty-state content consistently.
  - Center the icon, title, and supporting copy as one visual group.
  - Apply the correction through the shared empty-state composition so every dashboard tab is consistent.
  - Add a regression test before changing the implementation.
- [x] Add project row operations.
  - Add an Edit action for the project name and usage limits.
  - Add a Delete action with an explicit HeroUI confirmation dialog.
  - Explain whether associated API keys and usage records will be removed before confirmation.
  - Add typed Huma update/delete contracts and keep `/doc` synchronized.
  - Add service, handler, and interaction tests before implementing each operation.
- [x] Complete Codex account operations and plan labeling in Settings.
  - Add a Codex logout action with a HeroUI confirmation dialog.
  - Map Codex account plan identifiers to user-facing names: Free, Go, Plus, Pro 5x, and Pro 20x.
  - Treat unknown future identifiers as sanitized display labels rather than hard failures.
  - Add regression tests for the `prolite` to `Pro 5x` mapping and logout interaction.
  - Do not persist or expose account identifiers beyond the authenticated dashboard.
- [x] Remove revoked API keys from active management views.
  - Remove a key row immediately after successful revocation.
  - Exclude revoked keys from subsequent API key list responses.
  - Add service, handler, and interaction regression tests.
- [x] Keep dashboard views on the same content rail as the header tabs.
  - Make management sections and tables fill the available 1440px dashboard container.
  - Preserve horizontal table scrolling at narrow viewport widths.
  - Add a layout regression test and verify the updated UI and schema through the running dev profile.
- [x] Prevent dashboard navigation labels from wrapping.
  - Keep icon and label together at desktop widths.
  - Retain the HeroUI tab list's built-in overflow behavior for constrained viewports.
  - Add a regression test without overriding HeroUI component internals.
- [x] Present API key usage with the same table treatment as Projects.
  - Use the default primary HeroUI Table composition.
  - Preserve detailed usage columns and horizontal overflow.
  - Add a regression test before changing the variant.

## Compatibility verification

- [x] Run the live GPT-5.6 streaming matrix through the compatibility endpoints.
  - Test every available GPT-5.6 model with medium reasoning effort.
  - Test GPT-5.6 Sol with every reasoning effort reported as supported by Codex.
  - Verify normal incremental delivery and terminal events for Responses and Chat Completions SSE.
  - Validate the terminal usage payload, including cached-input and reasoning-output token details.
  - Verify completed usage is persisted against both the authenticated project and API key.
  - Use ephemeral, read-only turns and sanitized test output.
  - Do not persist prompts or reveal credentials or generated content.

## Configurable usage policies

- [x] Replace the single monthly safeguard with multiple project usage limits.
  - Support fixed UTC windows measured in minutes, hours, days, weeks, or months.
  - Allow a positive interval count so policies can express N-hour and other custom windows.
  - Enforce every configured policy and reject a request when any active window is exhausted.
  - Cap and validate policy counts, token ceilings, duplicates, and policies per project.
  - Add boundary, multiple-policy, validation, and enforcement tests before implementation.
- [x] Migrate existing monthly limits without losing project or usage data.
  - Convert each legacy monthly value to a one-month policy exactly once.
  - Keep the migration safe to rerun.
  - Add a regression test that opens a legacy database and verifies the converted policy.
- [x] Expose usage policies through the management API and OpenAPI document.
  - Accept and return typed `limits` arrays on project create/update/list operations.
  - Return current usage, start time, and reset time for each configured window.
  - Keep the general usage and API-key breakdown explicitly scoped to the current UTC month.
  - Add handler and live OpenAPI regression tests.
- [x] Replace monthly-only project controls with a HeroUI policy editor.
  - Compose documented HeroUI NumberField, Select, and Button controls.
  - Support adding and removing multiple policies in create and edit flows.
  - Summarize configured policies in the Projects table and their live progress in Usage.
  - Keep unlimited projects clear and preserve narrow-screen behavior.
  - Add interaction and parser regressions before implementation.
- [x] Update public documentation for configurable limits and fixed-window semantics.
- [x] Revise the Settings card layout.
  - Give account and network cards consistent header, content, and footer rhythm.
  - Keep the plan status aligned with identity and place disconnect in a stable footer action area.
  - Add a layout regression before changing the HeroUI Card composition.

## Chat playground

- [x] Persist completed dashboard chats in owner-scoped SQLite storage.
  - Save visible user/assistant messages, chat settings, and terminal usage only after a successful completed turn.
  - Keep OAuth credentials and opaque encrypted reasoning out of the database.
  - Reload history after browser refresh and gateway restart without storing transcript content in browser storage.
  - Isolate list and delete operations by the authenticated Google email.
  - Scope opaque in-memory continuation/cache identities by owner and session ID.
  - Add service, HTTP contract, API-boundary, and React hydration regressions before implementation.
- [x] Give every dashboard tab a stable browser path.
  - Use `/chat`, `/overview`, `/projects`, `/api-keys`, `/usage`, and `/settings`.
  - Restore the selected view on refresh and follow browser back/forward navigation.
  - Preserve the Go dashboard fallback for direct client-route requests.
  - Add shell and server fallback regressions before implementation.

- [x] Add a Chat tab backed by the real compatibility endpoints.
  - Authenticate dashboard Chat with the administrator session without requesting a project API key.
  - Attribute each turn to the built-in dashboard bucket or an optional selected project through a non-exportable Dashboard Chat usage identity.
  - Require no pre-created project, enforce selected-project limits, and include terminal usage in project and per-source dashboard totals.
  - Load projects and models automatically and offer concise sample prompts.
  - Load models through the administrator-only `/api/chat/models` route.
  - Support both `/v1/chat/completions` and `/v1/responses` streaming requests.
  - Provide model, reasoning effort, instructions, and prompt controls using documented HeroUI components.
  - Render incremental output, request state, errors, and terminal detailed usage.
  - Add strict SSE parser, API-client, and interaction tests before implementation.
- [x] Verify the complete feature through `bun run verify` and both Compose profiles.
- [x] Reuse one stateless Codex continuation per in-memory dashboard conversation.
  - Replay structured messages and opaque encrypted reasoning while sending only the newest user message on continuation.
  - Use a random browser-generated session identifier and persist only completed dashboard transcripts under their authenticated owner.
  - Restore completed chats from validated server responses without saving partial streams.
  - Omit tools and execution environments from gateway-only text requests.
  - Verify prompt-prefix reuse end to end with `gpt-5.6-luna` at medium reasoning.
- [x] Normalize terminal token accounting.
  - Define total tokens as input plus output for Responses, Chat Completions, dashboard usage, and project aggregation.
  - Treat cached input as part of input and reasoning output as part of output so neither subset is double-counted.
  - Cover inconsistent upstream cumulative totals with a regression test.
- [x] Polish the live chat interaction without changing gateway semantics.
  - Stop reading immediately after an SSE `[DONE]` sentinel, even if the transport remains open.
  - Batch token rendering to animation frames so long responses do not cause one React render per delta.
  - Follow new output while the reader is near the bottom and offer a jump-to-latest action after manual scrolling.
  - Submit with Enter while preserving Shift+Enter for multiline prompts.
  - Keep incomplete chats in browser memory only and retain terminal usage as the accounting source of truth.
- [x] Complete saved-chat management and rich response rendering.
  - Add owner-scoped rename and confirmed delete operations backed by SQLite.
  - Render assistant Markdown and separately streamed reasoning summaries.
  - Allow editing a user turn by truncating later turns and restarting continuation state.
  - Keep per-turn terminal usage immutable and show an explicit cumulative conversation total.
  - Preserve reasoning summaries and terminal usage across refreshes.
- [x] Make prompt-cache reuse observable and race-free.
  - Commit completed continuation state before exposing the terminal stream event.
  - Keep cached input as an authoritative upstream subset rather than deriving it locally.
  - Verify a two-turn `gpt-5.6-luna` dashboard conversation reports a live second-turn cache hit.

## Dashboard authentication

- [x] Replace the local password gate with Google-only Firebase authentication.
  - Verify Firebase ID-token signatures and claims in Go before creating a local session.
  - Admit the configured administrator plus only explicitly allowlisted Google email addresses.
  - Re-check the allowlist for every local session lookup and store only hashed session tokens.
  - Keep Firebase web configuration public while excluding the administrator allowlist from the API response.
  - Remove password setup and login routes from the implementation and OpenAPI document.
  - Document Firebase setup without committing project-specific configuration.
- [x] Keep the Windows development profile reproducible after dependency changes.
  - Reconcile the persistent `node_modules` volume from the Bun lockfile at container startup.
  - Cover the startup command in the Compose configuration check.
  - Install the OS CA bundle before the development image downloads Go modules.
  - Verify the running container resolves Firebase and passes the runtime smoke test.
