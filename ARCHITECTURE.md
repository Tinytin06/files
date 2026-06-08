# Architecture

## Components

- **Client (browser)** — a web app rendering the cryptex. The rotation widget
  tracks the current ring positions in local state only. On submit, the REST
  client serializes the guess to JSON and POSTs it. The client never receives or
  stores the real password; it only ever observes HTTP status codes.
- **REST API layer** — a small service (the app process inside the container).
  Exposes the unlock, photo, and password endpoints. Stateless with respect to
  guesses: every guess is validated independently.
- **Validator** — hashes the incoming guess with the same algorithm + salt used
  at storage time, then does a constant-time comparison against the stored hash.
- **Secret store** — holds only a salted hash (bcrypt / argon2), never plaintext.
  A full read of this store does not reveal the password. Lives on an encrypted
  dataset.
- **Photo storage** — the protected image file. Reachable only through the
  token-checked endpoint, kept outside any web root.

## Flow 1 — guess validation ("codes until success")

1. User rotates the rings and submits a combination.
2. Client sends `POST /api/unlock` with `{ "guess": "..." }`.
3. Server hashes the guess and compares it (constant-time) to the stored hash.
4. No match → `401 Unauthorized`, no detail, retry allowed.
5. Match → `200 OK`, plus a short-lived token. No password in the response.

The server is a binary oracle: every wrong guess yields an identical `401`
(same body, same timing); the only other possible signal is `200`.

## Flow 2 — unlock to photo download

1. On `200`, the unlock response carries a short-lived, single-use token
   (in the body or a `Set-Cookie`).
2. Client calls `GET /api/photo` with the token (e.g. `Authorization: Bearer`).
3. Server verifies the token and streams the photo bytes with
   `Content-Disposition: attachment; filename="..."`.
4. The browser's download manager saves the file to disk.
5. Without a valid token, `/api/photo` returns `401` — the URL itself is not
   the secret.

### Why token, not raw URL

The naive alternative — returning the photo's URL directly and letting the
client navigate to it — makes the URL the secret. Anyone who sees it in network
logs, browser history, or a shared link can download the photo without solving
the cryptex, making the unlock check decorative. The token approach keeps the
file unreachable without a successful unlock.

## Flow 3 — change the photo

1. Client already holds an unlock token (from a `200` unlock).
2. Client sends `PUT /api/photo` with the new image + token.
3. Server verifies the token **and its scope** — does it allow writes?
4. If allowed: validate (content type, magic bytes, size), then atomically
   replace the stored file (temp file + rename).
5. Returns `200` on success; `401` for a bad/missing token, `403` for a
   read-only token.

## Flow 4 — change the combination

`POST /api/password` accepts a new value, hashes it, and overwrites the stored
hash. This endpoint sits behind stronger auth than a read (unlocked session,
admin token, or re-solving the cryptex), so an unlock alone — or a leaked
read token — can't silently reset the secret.

## Client-side download/upload triggers

Download (fetch the protected endpoint, hand the blob to a programmatic anchor):

```javascript
async function downloadPhoto(token) {
  const res = await fetch('/api/photo', {
    headers: { 'Authorization': `Bearer ${token}` }
  });
  if (!res.ok) return;
  const blob = await res.blob();
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = 'secret.jpg';
  a.click();
  URL.revokeObjectURL(url);
}
```

The `download` attribute plus the server's `Content-Disposition: attachment`
header together guarantee the browser treats it as a download, not an inline
preview.

Change photo (send the new bytes instead of receiving them):

```javascript
async function changePhoto(token, file) {
  const res = await fetch('/api/photo', {
    method: 'PUT',
    headers: { 'Authorization': `Bearer ${token}` },
    body: file
  });
  return res.ok;
}
```
