# Project instructions

These instructions apply to the entire repository.

## Product identity

- The project name is **Codex Gateway**.
- The package and image identifier is `codex-gateway`.
- Describe it as a local compatibility gateway backed by Codex, not as a way to convert subscription credits into OpenAI API credits.

## Architecture

- Go is the only production application runtime.
- React is compiled by Bun and embedded into the Go executable.
- Bun is for frontend development, tests, formatting, and build tooling only; no Node or Bun process may be required in production.
- Implement the ChatGPT Codex OAuth and Responses transport directly in Go; no `codex` binary or child process may be required in production.
- Keep the undocumented ChatGPT Codex transport isolated behind `internal/codex`, independently implemented, and covered by contract tests against recorded shapes. Use the pinned OpenCode source under `.experiments/` only as behavioral reference.
- Keep dependencies focused; prefer the Go standard library when it provides a clear, maintainable solution.

## Required workflow

- Practice test-driven development for every behavior change: first create a failing test, then implement the minimum change, then refactor with tests green.
- Add a regression test before fixing a bug.
- Read `docs/codex-proxy/README.md` before changing or live-testing the Codex transport, compatibility streams, caching, OAuth, or token accounting.
- Run the narrowest relevant test during development and `bun run verify` before declaring work complete.
- Do not weaken tests, coverage thresholds, strict TypeScript settings, Go checks, lint rules, or validation to make a change pass.
- Keep public API schemas and `/doc` synchronized with behavior.
- Preserve the one-command `docker compose up` workflow.

## Research and experiments

- Use `.experiments/` for disposable, repository-local research such as cloning upstream source code, building focused reproductions, and inspecting external implementations.
- `.experiments/` must remain gitignored. Never commit its contents or make production code depend on files inside it.
- Treat cloned code as reference material: understand and adapt relevant ideas rather than copying implementations into Codex Gateway.
- Keep experimental commands scoped to `.experiments/` and avoid modifying cloned repositories unless the experiment specifically requires it.
- Delete or replace experimental artifacts only when their exact path under `.experiments/` has been verified.

## Frontend design system

- Use HeroUI v3 as the source of UI components and interaction patterns.
- Before designing or changing a webpage, query the project-configured `heroui-react` MCP server for comparable components, patterns, and reference implementations.
- Compose interfaces from HeroUI components whenever an equivalent exists. Do not invent replacement components.
- Do not edit, override, or target HeroUI component CSS unless the user explicitly requests that customization or approves it after being asked.
- Custom CSS may define page layout and application-owned surfaces, but it must not restyle HeroUI component internals.
- Keep Swagger UI as the library-owned renderer for `/doc`; do not recreate API documentation components.

## Commits

- Use Conventional Commits.
- Allowed types are `feat`, `fix`, `docs`, `test`, `refactor`, `perf`, `build`, `ci`, `chore`, and `revert`.
- Keep commits focused and independently verifiable.
- Do not create commits unless the user explicitly asks for one.

## Security and privacy

- Bind network services to loopback by default.
- Never commit or print OpenAI credentials, Codex authentication state, administrator passwords, session tokens, or plaintext project API keys.
- Store only hashes of project API keys and show plaintext once at creation.
- Do not persist public compatibility prompt or response content. Dashboard Chat is an explicit product exception: persist only completed visible transcripts and terminal usage, scoped to the authenticated Google email; never persist partial output or opaque encrypted reasoning.
- Treat all API input, upstream stream events, OAuth responses, database values, and environment variables as untrusted at their boundary.

## Code conventions

- Use idiomatic Go and strict TypeScript with ESM.
- Avoid `any`, `@ts-ignore`, non-null assertions, unchecked casts, and unnecessary global state.
- Prefer Huma input/output structures as the runtime source of truth for HTTP contracts.
- Keep route handlers thin; put policy and orchestration in independently tested services.
- Use structured logging and never attach sensitive headers or bodies.
- Use injected clocks, identifiers, and external clients in code that needs deterministic tests.
- Use Bun as the sole JavaScript package manager. Do not add npm, pnpm, or Yarn lockfiles.
