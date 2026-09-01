# AGENTS.md

## Project overview

This repository is an email-related application. Treat changes as security-sensitive by default, especially code that handles authentication, authorization, user identity, email sending/receiving, templates, attachments, deployment, and environment variables.

## Review guidelines

Codex should automatically review pull requests for this repository when automatic review is enabled in Codex settings. Apply these guidelines to every PR review; the author should not need to add an `@codex review` comment for normal reviews.

When reviewing pull requests for this repository, prioritize finding issues that are actionable and likely to affect production behavior.

### Security checks

- Check authentication and authorization boundaries for bypasses, privilege escalation, insecure direct object references, and missing ownership checks.
- Verify that authentication and authorization middleware wraps every protected route and API handler.
- Check email-related flows for spoofing, header injection, unsafe template rendering, open redirect links, phishing-prone behavior, and unintended recipient disclosure.
- Check input handling for SQL/NoSQL injection, command injection, path traversal, SSRF, XSS, CSRF, unsafe deserialization, and unsafe file upload or attachment handling.
- Check that secrets, tokens, passwords, API keys, SMTP credentials, verification codes, session IDs, cookies, PII, and personal data are never committed, logged, returned in API responses, or exposed to the client unnecessarily.
- Check cryptography and token logic for weak randomness, missing expiration, missing audience/issuer validation, replay risk, and insecure storage.

### API and backend checks

- Verify API changes include proper validation, error handling, status codes, rate limiting where appropriate, and consistent authorization checks.
- Verify database queries and migrations preserve data integrity, are backward-compatible when needed, and do not risk data loss without an explicit migration plan.
- Watch for race conditions, duplicate email sends, retry storms, queue/idempotency bugs, and missing transaction boundaries.
- Check background jobs, scheduled tasks, and webhook handlers for safe retries, idempotency, signature verification, and failure logging.

### Frontend and UX checks

- Check that user-controlled content rendered in the UI is escaped or sanitized.
- Check forms for validation, clear error handling, and no leakage of sensitive implementation details.
- Check permission-dependent UI so hidden actions are also enforced by the backend.

### Deployment and configuration checks

- Review Docker, CI/CD, environment, and deployment changes for exposed ports, overly broad permissions, insecure defaults, missing health checks, and accidental secret exposure.
- Prefer least privilege for service accounts, containers, filesystem mounts, and network access.
- Flag production-impacting config changes that lack rollback notes or operational context.

### Documentation checks

- Flag misleading or dangerous deployment instructions.
- Treat spelling mistakes in user-facing documentation as review comments only when they could confuse setup, security, or production operation.

### Review style

- Be concise and specific. Include file paths and the exact risky behavior.
- Prefer high-confidence findings over speculative comments.
- If suggesting a fix, explain the minimal safe change.
- Do not approve changes solely because tests pass; still inspect security, correctness, and operational risk.
- Avoid commenting on formatting-only issues unless they hide a real bug.


## Fix guidelines

When a maintainer asks Codex to fix review findings in a pull request, such as `@codex fix the P1 issue`, Codex should use the pull request context and apply the smallest safe patch that resolves the requested issue.

- Fix only the issue requested unless another change is required to make the fix correct.
- Preserve existing public behavior and APIs unless the requested fix explicitly requires a breaking change.
- Add or update focused tests when practical, especially for security, authorization, validation, and email-delivery behavior.
- Do not introduce new dependencies, schema changes, deployment changes, or broad refactors unless clearly necessary.
- Keep commits focused and explain the security/correctness impact in the PR response.
- If the issue cannot be safely fixed without more information, explain the blocker and the exact decision needed.

## Verification expectations
For non-trivial changes, look for relevant tests or manual verification notes. If they are missing, mention the specific behavior that should be tested rather than requesting generic test coverage.

