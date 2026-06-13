# API Reference

Base path: `/api`. All responses use status codes as the primary signal. No
endpoint ever returns the password, its plaintext, a hint, or partial-match
information.

## POST /api/unlock

Submit a cryptex combination guess.

- Auth: none — the guess itself is the test.
- Request body: `{ "guess": "<string>" }`
- `200 OK` — correct. Returns a short-lived, single-use token (body or
  `Set-Cookie`). No secret in the response.
- `401 Unauthorized` — wrong. Identical body and timing for every wrong guess.
- Subject to rate limiting / exponential backoff.

Server logic: hash the guess, constant-time compare to the stored hash.

## GET /api/photo

Download the protected file (any type — image, document, archive, …).

- Auth: a valid unlock token (any scope).
- `200 OK` — streams the file bytes with
  `Content-Disposition: attachment; filename="..."` (plus an RFC 5987
  `filename*` for non-ASCII names) and `X-Content-Type-Options: nosniff`, so the
  browser always saves the file rather than rendering it inline.
- `401 Unauthorized` — missing/invalid token.

## PUT /api/photo

Replace the protected file. Any file type is accepted.

- Auth: an unlock token **with write scope**.
- Request body: the new file bytes.
- Original filename: sent in the `X-Filename` header, percent-encoded. The
  server sanitizes it (strips path components, control characters, and quotes)
  and uses it for the eventual download; it falls back to a generic name if
  absent. The extension/bytes determine the stored content type.
- Server: cap file size, write to a temp file, atomically rename over the old
  file. Files are only ever served as `attachment` with `nosniff`, so accepting
  arbitrary types does not expose an inline-render/XSS vector.
- `200 OK` — replaced.
- `400 Bad Request` — empty body.
- `401 Unauthorized` — bad/missing token.
- `403 Forbidden` — token is read-only.

## POST /api/password

Change the cryptex combination.

- Auth: stronger than a read — unlocked session, admin token, or re-solving the
  cryptex. An unlock alone or a leaked read token must not be sufficient.
- Request body: the new combination (and whatever stronger-auth proof the design
  requires).
- Server: hash the new value, overwrite the stored hash.
- `200 OK` — changed.
- `401 / 403` — insufficient auth.

## Token semantics

- Issued by `POST /api/unlock` on success.
- Carry a `scope` claim: `read` or `read+write`.
- `GET /api/photo` accepts any valid token; `PUT /api/photo` requires write scope.
- Prefer `Authorization: Bearer <token>` over query-string tokens (the latter
  land in history/logs).

Decisions to lock down per requirements:
- Lifetime: single-use vs session-lived.
- Whether minting a write-scoped token requires a higher bar than a read token.
- Whether the signing key persists across restarts (regenerating it invalidates
  all outstanding tokens).
