# LanQin Email AI Review Rules

Review this repository as a security-sensitive email application. Focus on high-confidence issues that can affect production behavior.

## Priorities

- Authentication, authorization, ownership checks, session handling, and 2FA flows.
- Email sending/receiving behavior, including spoofing, header injection, unsafe templates, recipient disclosure, duplicate sends, retries, and idempotency.
- Input handling risks: SQL/NoSQL injection, command injection, path traversal, SSRF, XSS, CSRF, unsafe deserialization, and unsafe attachment uploads.
- Secret and privacy exposure: tokens, passwords, SMTP credentials, verification codes, session IDs, cookies, PII, and logs/API responses that leak sensitive data.
- Deployment changes: Docker, GitHub Actions, exposed ports, overly broad permissions, insecure defaults, missing health checks, and rollback-sensitive config.

## Comment style

- Prefer actionable, high-confidence findings over speculative comments.
- Include the risky file path/line, trigger condition, impact, and minimal safe fix.
- Do not comment on formatting-only issues unless they hide a real bug.
- Treat tests passing as useful signal, not as approval by itself.
