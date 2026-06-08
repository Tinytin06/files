# Deployment — custom Docker container on TrueNAS SCALE

The application code is the same as any other deployment. Containerizing changes
three things: persistent storage, networking, and how secrets/config get in.

## 1. Persistent storage (most important)

Containers are ephemeral — anything written to the container's own filesystem is
lost on restart or rebuild. So:

- The **photo** and the **password hash** must live on a **mounted volume**, not
  inside the image.
- On TrueNAS SCALE, create a dataset and mount it into the container as a host
  path (in the app's storage config).
- The `PUT /api/photo` upload writes to this mounted path so changes survive
  restarts and image updates.
- Keep the photo and hash **outside any web root**; they should only be reachable
  through the token-checked endpoint.
- Never bake the photo or hash into the image.

## 2. Networking

- The container listens on an internal port; **publish** it to a host port so the
  browser can reach the API.
- For TLS + a clean hostname (instead of `host:port`), put a **reverse proxy** in
  front — another container (Traefik, Caddy, nginx) or TrueNAS's built-in
  options.
- Terminate HTTPS at the proxy so guesses are never sent in plaintext.

## 3. Secrets and config

- The password is not in the image — it's a salted hash on the mounted dataset,
  set at runtime via `POST /api/password` or an init step.
- Environment-specific values (dataset path, listen port, token signing key)
  come in as **environment variables** or a **mounted config file**, not
  hardcoded.

## Dockerfile sketch

Short, because the cryptex behavior needs no special container support — it's
all just HTTP. Roughly:

```dockerfile
FROM <slim base image for your language>
WORKDIR /app
COPY . .
RUN <install dependencies>
EXPOSE <internal port>
CMD ["<start command>"]
```

## TrueNAS SCALE app config (custom app)

- **Image**: build locally and store it, or push to a registry TrueNAS pulls from.
- **Storage mount**: dataset → container mount path (where the photo + hash live).
- **Port mapping**: host port → container's internal port.
- **Env vars**: dataset path, listen port, token signing key.

## Container-specific decisions

- Build-and-store-locally vs push-to-registry for the image.
- Whether the token signing key persists across container restarts. If it is
  regenerated on every restart, all outstanding unlock tokens become invalid —
  decide whether that's the behavior you want.
