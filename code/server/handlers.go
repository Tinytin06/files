package main

// HTTP handlers. The server is a binary oracle: every wrong guess returns an
// identical, empty 401 with a uniform minimum response time. No endpoint ever
// returns the password, a hint, or partial-match information.

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

type unlockResponse struct {
	Token string `json:"token"`
	Scope string `json:"scope"`
}

// handleKEM serves the ML-KEM-768 public key the client encapsulates to.
func (a *App) handleKEM(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"public_key": a.kem.PublicKeyB64()})
}

// POST /api/unlock — the guess itself is the test. The guess arrives sealed in
// an ML-KEM-768 envelope; we decapsulate + decrypt before comparing.
func (a *App) handleUnlock(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	ip := clientIP(r)

	if ok, wait := a.limiter.Allowed(ip); !ok {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(wait.Seconds())+1))
		http.Error(w, "", http.StatusTooManyRequests)
		return
	}

	var env envelope
	if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&env); err != nil {
		a.unauthorized(w, start)
		return
	}
	guessBytes, err := a.kem.Open(env)
	if err != nil {
		a.limiter.Fail(ip)
		a.unauthorized(w, start)
		return
	}
	guess := string(guessBytes)

	if !a.store.HasPassword() {
		// No secret set yet: nothing can unlock. Still uniform 401.
		a.limiter.Fail(ip)
		a.unauthorized(w, start)
		return
	}

	hash, err := a.store.ReadPasswordHash()
	if err != nil {
		a.limiter.Fail(ip)
		a.unauthorized(w, start)
		return
	}

	ok, err := VerifyPassword(guess, hash)
	if err != nil || !ok {
		a.limiter.Fail(ip)
		a.unauthorized(w, start)
		return
	}

	// Correct. Solving the cryptex is the bar for a read+write token.
	a.limiter.Reset(ip)
	token, err := a.tokens.Issue(ScopeReadWrite)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	a.sleepFloor(start)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(unlockResponse{Token: token, Scope: ScopeReadWrite})
}

// GET /api/photo — any valid token may download.
func (a *App) handlePhotoGet(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireToken(w, r, false); !ok {
		return
	}
	data, meta, err := a.store.ReadPhoto()
	if err != nil {
		http.Error(w, "", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", meta.ContentType)
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", meta.Filename))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(data)
}

// PUT /api/photo — requires a write-scoped unlock token OR the admin token.
// The admin path lets the owner seed/replace the photo without solving the
// cryptex first.
func (a *App) handlePhotoPut(w http.ResponseWriter, r *http.Request) {
	if !a.adminAuthorized(r) {
		if _, ok := a.requireToken(w, r, true); !ok {
			return
		}
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, a.cfg.UploadMax))
	if err != nil {
		http.Error(w, "", http.StatusRequestEntityTooLarge)
		return
	}
	ct, ext, valid := detectImage(body)
	if !valid {
		http.Error(w, "unsupported image type", http.StatusUnsupportedMediaType)
		return
	}
	meta := photoMeta{ContentType: ct, Filename: "secret" + ext}
	if err := a.store.WritePhoto(body, meta); err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// POST /api/password — change the combination. Behind the admin token (a
// stronger bar than a read), and the new combination arrives sealed in an
// ML-KEM-768 envelope, never as plaintext.
func (a *App) handlePassword(w http.ResponseWriter, r *http.Request) {
	if !a.adminAuthorized(r) {
		http.Error(w, "", http.StatusForbidden)
		return
	}
	var env envelope
	if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&env); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	plain, err := a.kem.Open(env)
	if err != nil || len(plain) == 0 {
		http.Error(w, "could not decrypt new combination", http.StatusBadRequest)
		return
	}
	newCombo := string(plain)
	hash, err := HashPassword(newCombo)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	if err := a.store.WritePasswordHash(hash, len([]rune(newCombo))); err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

type configResponse struct {
	Rings    int    `json:"rings"`
	Alphabet string `json:"alphabet"`
}

// GET /api/config — UI shape only (ring count + dialable characters). Carries
// no secret: it describes the cryptex, not the combination.
func (a *App) handleConfig(w http.ResponseWriter, _ *http.Request) {
	// Ring count follows the stored combination's length when known, so setting
	// a new combination automatically reshapes the cryptex. Falls back to the
	// CRYPTEX_RINGS default before any combination is set.
	rings := a.cfg.Rings
	if n, ok := a.store.ReadPasswordLen(); ok {
		rings = n
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(configResponse{Rings: rings, Alphabet: a.cfg.Alphabet})
}

// --- helpers ---

// unauthorized emits the single canonical 401: empty body, uniform timing.
func (a *App) unauthorized(w http.ResponseWriter, start time.Time) {
	a.sleepFloor(start)
	w.WriteHeader(http.StatusUnauthorized)
}

// sleepFloor pads the response so every unlock attempt takes at least the
// configured minimum, keeping wrong guesses indistinguishable by timing.
func (a *App) sleepFloor(start time.Time) {
	if d := a.cfg.MinUnlockDur - time.Since(start); d > 0 {
		time.Sleep(d)
	}
}

func (a *App) requireToken(w http.ResponseWriter, r *http.Request, needWrite bool) (*claims, bool) {
	tok := bearer(r)
	if tok == "" {
		http.Error(w, "", http.StatusUnauthorized)
		return nil, false
	}
	c, err := a.tokens.Verify(tok)
	if err != nil {
		http.Error(w, "", http.StatusUnauthorized)
		return nil, false
	}
	if needWrite && !c.CanWrite() {
		http.Error(w, "", http.StatusForbidden)
		return nil, false
	}
	return c, true
}

func (a *App) adminAuthorized(r *http.Request) bool {
	if a.cfg.AdminToken == "" {
		return false // admin endpoint disabled until a token is configured
	}
	return constTimeEqualStr(bearer(r), a.cfg.AdminToken)
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if v, ok := strings.CutPrefix(h, "Bearer "); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// clientIP prefers the left-most X-Forwarded-For entry (set by the reverse
// proxy that terminates TLS) and falls back to the socket address.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
