# CLAUDE.md

Guidance for Claude Code when working in this repository.

## What this project is

A "cryptex" web app. The client renders a rotatable cryptex (like the puzzle
cylinder); the user rotates the rings to enter a combination and submits a
guess. The server validates the guess and responds with HTTP status codes only
— `401` for every wrong guess, `200` on success — and never sends the password
(or its plaintext) back to the client. On a successful unlock the client can
download a protected photo and, with a write-capable token, replace it. The
combination itself can also be changed through a protected endpoint.

It is a client/server system communicating over REST, intended to run as a
custom Docker container on a TrueNAS SCALE host.

## Core security principle (do not violate)

The password never leaves the server. The server is a **binary oracle**: it only
ever reveals whether a guess was right or wrong via the HTTP status code. No
endpoint returns the stored secret, a hint, partial-match info, or anything that
narrows the keyspace.

Concretely:
- The combination is stored only as a salted hash (bcrypt or argon2), never plaintext.
- Validation hashes the incoming guess and does a **constant-time** comparison
  against the stored hash. Constant-time matters so response timing can't leak
  partial correctness.
- All `401` responses are identical in body and timing — wrong guesses must be
  indistinguishable from one another.
- The protected photo is reachable **only** through a token-checked endpoint,
  never by a guessable/static URL. The URL must not become the secret.

If a change request would weaken any of the above, flag it rather than
implementing silently.

## Architecture overview

```
Browser client  ──REST──>  API service (in container)  ──>  Dataset (photo + hash)
   Cryptex UI                 - validates guess               on mounted TrueNAS volume
   REST client                - issues unlock tokens
   (status codes only)        - serves / replaces photo
```

- Client: web app. Cryptex rotation widget holds ring positions in local state
  only. REST client serializes the guess as JSON and POSTs it. Client only ever
  sees status codes; it never receives or stores the real password.
- API service: small REST service. Endpoints below. Runs as the app process
  inside a custom Docker image.
- Secret store / photo: live on a **mounted dataset**, not inside the container
  image. Survives restarts and image rebuilds. Kept outside any web root.

See `docs/ARCHITECTURE.md` for the request flows and `docs/API.md` for the
endpoint contract.

## REST API (summary)

| Method | Path             | Auth                | Success | Failure |
|--------|------------------|---------------------|---------|---------|
| POST   | `/api/unlock`    | none (the guess IS the test) | `200` + token | `401` |
| GET    | `/api/photo`     | unlock token        | `200` + image bytes | `401` |
| PUT    | `/api/photo`     | unlock token (write scope) | `200` | `401` / `403` |
| POST   | `/api/password`  | stronger auth (see below) | `200` | `401` / `403` |

- `POST /api/unlock` accepts `{ "guess": "..." }`. Returns `200` with a
  short-lived, single-use token on success; `401` otherwise. Body carries no secret.
- Download (`GET /api/photo`) streams the image with
  `Content-Disposition: attachment; filename="..."` so the browser saves it
  rather than rendering inline.
- Change photo (`PUT /api/photo`) requires the **write scope** on the token.
  Any file type is accepted. Cap file size, sanitize the client-supplied
  filename (no path components, control chars, or quotes), write to a temp file
  and atomically rename over the old one. Arbitrary types are safe because the
  download endpoint always serves `Content-Disposition: attachment` with
  `X-Content-Type-Options: nosniff` — files are saved, never rendered inline.
- Change password (`POST /api/password`) must sit behind stronger auth than a
  plain read — an unlocked session, an admin token, or re-solving the cryptex.
  The new value is hashed and overwrites the stored hash.

## Token model

Unlock issues a token. Prefer **scoped tokens**: a `scope` claim of `read` or
`read+write`. `GET /api/photo` accepts any valid token; `PUT /api/photo` checks
for write scope and returns `403` without it. Consider requiring a higher bar
(re-solve, or separate admin token) before minting a write-capable token.

Open decisions (pick per requirements, don't assume):
- Token lifetime: single-use vs session-lived.
- Whether the token signing key persists across container restarts. If it is
  regenerated on restart, all outstanding tokens are invalidated — sometimes
  desired, sometimes not.
- Whether unlocking should also start a session for follow-up actions.

## Deployment: custom Docker container on TrueNAS SCALE

The application code is identical to a non-container deployment. Containerizing
affects three things only:

1. **Persistent storage** — containers are ephemeral. The photo and the
   password hash MUST live on a mounted volume (a TrueNAS dataset mounted as a
   host path), never inside the image. The `PUT /api/photo` upload writes to
   this mounted path.
2. **Networking** — the container listens internally; publish that port to a
   host port. For TLS + a clean hostname, put a reverse proxy in front
   (Traefik / Caddy / nginx, or TrueNAS's built-in options). TLS terminates
   there so guesses aren't sent in plaintext.
3. **Secrets / config** — the hash is on the mounted dataset, not baked into the
   image. Dataset path, listen port, and token signing key come in via env vars
   or a mounted config file.

See `docs/DEPLOYMENT.md` for the Dockerfile sketch and TrueNAS app config notes.

## Hard requirements checklist (enforce on every change)

- [ ] Password / plaintext never appears in any response body, log, or URL.
- [ ] Wrong guess → `401`; correct guess → `200`. No other signal.
- [ ] Stored secret is a salted hash; comparison is constant-time.
- [ ] Photo only reachable via token-checked endpoint.
- [ ] Rate limiting / backoff on `/api/unlock` (small keyspace invites brute force).
- [ ] TLS in front of the API.
- [ ] `/api/password` behind stronger auth than a read.
- [ ] Uploads handled safely (filename sanitized, size cap, atomic replace, served as attachment + nosniff).
- [ ] Photo + hash on a mounted volume, outside the image and any web root.

## Conventions

- Keep the API surface small and verb-based (GET reads, PUT/POST writes the same
  resource).
- No secret-bearing data in query strings where it lands in history/logs; prefer
  `Authorization` headers.
- Treat `docs/` as the source of truth for behavior; update it alongside code.
