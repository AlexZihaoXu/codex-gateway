# Contributing to Codex Gateway

## Development workflow

Every behavior change follows test-driven development:

1. **Red:** add or change a test that expresses the desired behavior and confirm it fails for the expected reason.
2. **Green:** implement the smallest production change that makes the test pass.
3. **Refactor:** improve the design without changing behavior while keeping all tests green.
4. Run `bun run verify` before opening a pull request.

A pull request that changes behavior without an accompanying test is incomplete. Documentation-only, formatting-only, and build-metadata changes may omit tests when the reason is clear.

## Conventional Commits

All commits must use this structure:

```text
<type>(optional-scope): <imperative summary>
```

Allowed types are `feat`, `fix`, `docs`, `test`, `refactor`, `perf`, `build`, `ci`, `chore`, and `revert`.

Examples:

```text
feat(auth): add first-run administrator setup
fix(streaming): emit a terminal chunk after interruption
test(quotas): cover concurrent token reservations
docs(api): document unsupported chat parameters
```

Use `!` or a `BREAKING CHANGE:` footer for breaking changes. Commit messages are checked by a Git hook and CI.

## Quality requirements

- Keep Go idiomatic and TypeScript strict.
- Define runtime schemas for all externally sourced data.
- Preserve OpenAI-compatible error and streaming behavior where support is claimed.
- Keep route handlers small and orchestration independently testable.
- Do not log credentials, authorization headers, prompt content, or generated content.
- Add a regression test before fixing a defect.
- Keep the application usable through the documented one-command Compose profiles.
- Use Bun as the only JavaScript package manager.

## Pull requests

Keep pull requests focused. Include the outcome, tests, compatibility notes, security/privacy considerations, and screenshots for meaningful dashboard changes.
