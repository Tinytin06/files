# Cryptex — implementation

One Svelte UI delivered two ways, plus a Go REST API, packaged as a single
Docker image for TrueNAS SCALE.

```
code/                     <- make THIS the git repo root
├── web/                  Svelte (SvelteKit, static SPA)
│   ├── src/lib/Cryptex.svelte   the rotatable cryptex widget
│   ├── src/lib/api.ts           REST client (status codes only)
│   ├── src/lib/crypto.ts        ML-KEM-768 sealing of the secret
│   └── src/routes/+page.svelte  unlock / download / change-combo UI
├── server/               Go REST API (the Docker process)
│   ├── handlers.go       /api/unlock, /api/photo (GET/PUT), /api/password
│   ├── kem.go            ML-KEM-768 key + envelope decryption
│   ├── hash.go           argon2id + constant-time verify
│   ├── token.go          HMAC-signed scoped tokens (no JWT dep)
│   ├── store.go          password hash + photo on the mounted volume
│   ├── ratelimit.go      per-client exponential backoff on /api/unlock
│   └── image.go          magic-byte image validation
├── Dockerfile            builds web + server into one tiny distroless image
├── docker-compose.yml    local run + TrueNAS reference (ports/volume/env)
└── .github/workflows/    CI: build & push image to GHCR for TrueNAS to pull
```

## How "website + mobile from one codebase" works

The UI is built once (`web/build`, a static SPA). That same output is:

- **Website** — served by the Go container at `/`, calling `/api` on the same
  origin. This is what runs on TrueNAS.
- **Mobile apps** — wrapped by **Capacitor** into native iOS/Android shells. The
  shell has no server, so set `VITE_API_BASE_URL` to your deployed HTTPS API
  before building (see `web/.env.example`).

Both only ever observe HTTP status codes. The password never reaches the client.

## The security contract (kept by the server)

- **Post-quantum sealed transport.** The guess (and a new combination) are
  never sent as plaintext. The client fetches the server's **ML-KEM-768**
  (FIPS 203) public key from `GET /api/kem`, encapsulates a 32-byte shared
  secret to it, encrypts the value with **AES-256-GCM** under that secret, and
  posts `{ kem, nonce, ciphertext }`. Only the server's private key can
  decapsulate. This protects the password even if TLS were stripped.
- `POST /api/unlock` → `200` + scoped token on the right guess, identical empty
  `401` on every wrong one, with a uniform minimum response time. After
  decryption the guess is argon2id-hashed and compared in constant time. No
  hint, no partial match.
- Combination is stored only as a salted argon2id hash on the mounted volume.
- The photo is reachable **only** via the token-checked `/api/photo` — never a
  static/guessable URL.
- `PUT /api/photo` needs a write-scoped unlock token **or** the `ADMIN_TOKEN`
  (so the owner can seed the photo without unlocking). It validates by magic
  bytes (not the extension), caps size, and replaces atomically (temp + rename).
  The admin upload control lives in the UI's "admin" panel.
- `POST /api/password` sits behind the `ADMIN_TOKEN` **and** takes the new
  combination as an ML-KEM-768 sealed envelope. The UI's admin panel forces the
  combination to uppercase (matching the default A–Z rings).
- Per-client exponential backoff on `/api/unlock`.

> The ML-KEM key seed persists on the mounted volume (`kem.seed`) so the public
> key is stable across restarts. TLS still belongs in front (see Deploy); the
> sealing is defence-in-depth on top of it.

## Run it locally

Two terminals:

```bash
# 1. API
cd server
DATA_DIR=./data ADMIN_TOKEN=dev TOKEN_SIGNING_KEY=devkey CRYPTEX_INIT_PASSWORD=APPLE \
  go run .

# 2. UI (proxies /api to :8080)
cd web
npm install
npm run dev   # http://localhost:5173
```

Dial `A P P L E` and hit Unlock → `200`. Anything else → `401`.

Or the whole thing as the container does it:

```bash
docker compose up --build   # http://localhost:8080
```

(Set `ADMIN_TOKEN`, `TOKEN_SIGNING_KEY`, and optionally `CRYPTEX_INIT_PASSWORD`
in `docker-compose.yml` first.)

## Build the mobile apps

```bash
cd web
cp .env.example .env            # set VITE_API_BASE_URL to your HTTPS API
npm run build
npx cap add android             # and/or: npx cap add ios   (macOS + Xcode)
npm run cap:sync
npx cap open android            # builds/installs from Android Studio / Xcode
```

`web/android` and `web/ios` are generated locally and git-ignored — regenerate
with `cap add` on any machine.

## Configuration (env vars)

| Var | Default | Purpose |
|-----|---------|---------|
| `LISTEN_ADDR` | `:8080` | Address the API listens on |
| `DATA_DIR` | `/data` | Mounted volume: password hash + photo |
| `WEB_DIR` | `/app/web` | Built SPA to serve (set in the image) |
| `TOKEN_SIGNING_KEY` | *(ephemeral)* | **Set a stable secret** or restarts invalidate tokens |
| `ADMIN_TOKEN` | *(unset → endpoint disabled)* | Auth for `POST /api/password` |
| `CRYPTEX_INIT_PASSWORD` | — | One-time combo bootstrap on first start |
| `CRYPTEX_RINGS` | `5` | Number of rings — set to your combination's length |
| `CRYPTEX_ALPHABET` | `A–Z` | Characters each ring can dial (e.g. `0123456789`) |
| `MAX_UPLOAD_BYTES` | `10485760` | Upload size cap |
| `TOKEN_TTL_SECONDS` | `600` | Unlock token lifetime |
| `MIN_UNLOCK_MS` | `250` | Uniform minimum unlock response time |

## Deploy to TrueNAS SCALE (via GitHub → GHCR)

1. **Push `code/` to GitHub** as its own repo:
   ```bash
   cd code
   git init && git add . && git commit -m "cryptex"
   git branch -M main
   git remote add origin https://github.com/<you>/cryptex.git
   git push -u origin main
   ```
2. The **build workflow** runs automatically and publishes
   `ghcr.io/<you>/cryptex:latest`. Open Packages → `cryptex` → make it **public**
   (so TrueNAS can pull without credentials), or add a registry login in TrueNAS.
3. On TrueNAS: create a **dataset** for persistent data (the photo + hash).
4. **Apps → Discover → Custom App** (YAML install) — reuse `docker-compose.yml`:
   - **Image**: `ghcr.io/<you>/cryptex:latest`
   - **Storage**: host path = your dataset → mount path `/data`
   - **Port**: host port → container `8080`
   - **Env**: `TOKEN_SIGNING_KEY` (stable random), `ADMIN_TOKEN`, and
     `CRYPTEX_INIT_PASSWORD` on first deploy
5. Put a **reverse proxy with TLS** in front (Traefik/Caddy/nginx or TrueNAS's
   built-in) so the public URL is HTTPS. The app honors `X-Forwarded-For` for
   rate limiting.

After deploy, change the combination from the **admin panel in the web UI**
(expand "Change combination (admin)", paste the `ADMIN_TOKEN`, type the new
combination — it's forced to uppercase, sealed with ML-KEM-768, and sent). A
raw `curl` no longer works because the endpoint expects a sealed envelope, not
plaintext JSON.

See the specs in the parent folder (`../API.md`, `../ARCHITECTURE.md`,
`../DEPLOYMENT.md`) for the full contract this implements.
