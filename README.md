# Cryptex

A client/server web app where the client rotates a cryptex to enter a
combination and submits guesses to a REST API. The server replies with HTTP
status codes only — `401` for wrong guesses, `200` on success — and never sends
the password back. On a successful unlock the client can download a protected
photo and (with a write-capable token) replace it. The combination can also be
changed through a protected endpoint. Designed to run as a custom Docker
container on TrueNAS SCALE.

## The one rule that shapes everything

The password never leaves the server. The server only ever reveals right-or-wrong
via the status code. The stored secret is a salted hash; comparison is
constant-time; the protected photo is reachable only through a token-checked
endpoint.

## Docs

- `CLAUDE.md` — context + hard requirements for Claude Code.
- `docs/ARCHITECTURE.md` — components and the four request flows.
- `docs/API.md` — endpoint contract.
- `docs/DEPLOYMENT.md` — Docker image + TrueNAS SCALE setup.

## Endpoints at a glance

| Method | Path            | Purpose            |
|--------|-----------------|--------------------|
| POST   | `/api/unlock`   | Submit a guess     |
| GET    | `/api/photo`    | Download the photo |
| PUT    | `/api/photo`    | Replace the photo  |
| POST   | `/api/password` | Change the combo   |

## Status

Specification / context only — no implementation yet. Language and framework are
open; the design assumes a small REST service plus a browser client.
