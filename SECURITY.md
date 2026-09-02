# Security policy

## Supported versions

Codex Gateway is pre-release software. Only the latest revision of the default branch receives security fixes.

## Reporting a vulnerability

Do not open a public issue containing credentials, authentication material, exploit details, private source code, prompts, or responses. Once the repository is public, use GitHub's **Security** tab to submit a private vulnerability report. Until then, contact the repository owner privately.

Include a concise description, sanitized reproduction steps, the affected revision, expected impact, and suggested mitigation if available. You should receive an acknowledgement within seven days.

## Deployment boundary

Codex Gateway is designed for trusted local use. Do not expose it publicly without TLS termination, network access controls, hardened secret management, monitoring, and an explicit threat-model review.

Dashboard access accepts only Google identities verified against the configured Firebase project and then checks the local administrator email allowlist. Keep `.env` private, review `FIREBASE_ALLOWED_EMAILS` before each deployment, and restart the gateway after changing access.

Treat the gateway data volume as sensitive. It contains Codex authentication material and the SQLite database, including completed Dashboard Chat transcripts scoped by Google email. Public compatibility request/response bodies, partial dashboard streams, and opaque encrypted reasoning are not stored in SQLite.
