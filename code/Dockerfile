# Multi-stage build: compile the Svelte SPA, build the Go API, then assemble a
# tiny distroless image that serves both. The result has no shell and runs as a
# non-root user — small attack surface for the binary oracle.

# --- 1. Build the web UI ---
FROM node:20-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json* ./
RUN npm install
COPY web/ ./
RUN npm run build

# --- 2. Build the Go server (static binary) ---
FROM golang:1.23-alpine AS server
WORKDIR /src
COPY server/go.mod server/go.sum* ./
RUN go mod download
COPY server/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /cryptex .

# --- 3. Final image ---
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=server /cryptex /app/cryptex
COPY --from=web /web/build /app/web
ENV LISTEN_ADDR=:8080 \
    DATA_DIR=/data \
    WEB_DIR=/app/web
EXPOSE 8080
# /data is the mounted TrueNAS dataset: password hash + photo live here, never
# in the image.
VOLUME ["/data"]
USER nonroot:nonroot
ENTRYPOINT ["/app/cryptex"]
