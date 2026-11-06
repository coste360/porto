---
paths:
  - "internal/**"
  - "cmd/**"
  - "web/**"
  - "deploy/**"
---
# Security and privacy (Porto)

Spec: 30 (security, privacy, GDPR), 26.2 (authentication); INV-70.

- No personal data or secrets in logs, SSE messages, outbox events or webhook payloads: no emails, phone numbers,
  names, documents, tokens, passwords, keys. Events carry ids and enums only (`TestEvents_DataHasNoPII`,
  `TestSSE_PayloadHasNoPII`, `TestLogs_NoPII`).
- Secrets come from `PORTO_SECRET_KEY` through the KeyRing (`internal/crypto`); derive purpose keys with HKDF,
  never reuse a key for two purposes, never hard-code a secret. Validation errors name the variable, never echo
  the value.
- Passwords: Argon2id. Tokens (sessions, API keys, device tokens, reset tokens) are random, stored as hashes,
  compared in constant time, shown once at creation.
- Login: same error and comparable work for unknown email and wrong password (dummy hash); rate limited per
  account and per IP prefix. Sessions rotate on login and on TOTP confirmation.
- Outbound HTTP (webhooks, later providers) only through the egress client: https only, private, loopback,
  link-local and metadata addresses refused after DNS resolution (rebinding safe), redirects re-checked.
  `PORTO_WEBHOOKS_ALLOW_PRIVATE` exists for dev and tests only.
- Every operational query is scoped by `marina_id` from the auth context; cross-marina access is 404.
- Cookies `porto_session` / `porto_csrf`: Secure (except dev over http), SameSite=Lax, session HttpOnly.
- Containers run as non-root on a read-only filesystem with all capabilities dropped (deploy/).
- New dependencies: MIT, BSD-2/3, Apache-2.0, ISC or MPL-2.0 only (`make licenses`).
