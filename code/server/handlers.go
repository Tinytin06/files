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
	"net/url"
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
// an ML-KEM-768 envelope; we decapsulate + decrypt, then compare it against
// every entry. A match issues a token bound to that entry; nothing about which
// entry (or how many) is revealed.
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

	entries, err := a.store.EntryHashes()
	if err != nil || len(entries) == 0 {
		a.limiter.Fail(ip)
		a.unauthorized(w, start)
		return
	}

	// Check every entry without an early break, so response time doesn't reveal
	// which entry matched (or how far down the list it was).
	matched := ""
	for _, e := range entries {
		if ok, verr := VerifyPassword(guess, e.Hash); verr == nil && ok {
			matched = e.ID
		}
	}
	if matched == "" {
		a.limiter.Fail(ip)
		a.unauthorized(w, start)
		return
	}

	// Correct. Solving the cryptex is the bar for a read+write token.
	a.limiter.Reset(ip)
	token, err := a.tokens.Issue(ScopeReadWrite, matched)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	a.sleepFloor(start)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(unlockResponse{Token: token, Scope: ScopeReadWrite})
}

// GET /api/photo — download the file for the entry the token unlocked.
func (a *App) handlePhotoGet(w http.ResponseWriter, r *http.Request) {
	c, ok := a.requireToken(w, r, false)
	if !ok {
		return
	}
	data, meta, err := a.store.ReadEntryFile(c.Entry)
	if err != nil {
		http.Error(w, "", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", meta.ContentType)
	w.Header().Set("Content-Disposition", contentDisposition(meta.Filename))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(data)
}

// --- admin entry management (all behind the admin token) ---

// GET /api/entries — list entries (non-secret summary: id, label, length, file?).
func (a *App) handleEntriesList(w http.ResponseWriter, r *http.Request) {
	if !a.adminAuthorized(r) {
		http.Error(w, "", http.StatusForbidden)
		return
	}
	list, err := a.store.ListEntries()
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}

type createEntryRequest struct {
	Label string   `json:"label"`
	Combo envelope `json:"combo"`
}

// POST /api/entries — create a new combination (sealed) with a label. Enforces
// the uniform length and rejects a combination that already matches an entry.
func (a *App) handleEntryCreate(w http.ResponseWriter, r *http.Request) {
	if !a.adminAuthorized(r) {
		http.Error(w, "", http.StatusForbidden)
		return
	}
	var req createEntryRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	plain, err := a.kem.Open(req.Combo)
	if err != nil || len(plain) == 0 {
		http.Error(w, "could not decrypt combination", http.StatusBadRequest)
		return
	}
	combo := string(plain)
	length := len([]rune(combo))

	// Uniform length: must match existing entries once any exist.
	if want, ok := a.store.UniformLen(); ok && length != want {
		http.Error(w, fmt.Sprintf("combination must be %d characters", want),
			http.StatusUnprocessableEntity)
		return
	}

	// Reject a duplicate combination (would make unlock ambiguous).
	if existing, err := a.store.EntryHashes(); err == nil {
		for _, e := range existing {
			if ok, verr := VerifyPassword(combo, e.Hash); verr == nil && ok {
				http.Error(w, "combination already exists", http.StatusConflict)
				return
			}
		}
	}

	hash, err := HashPassword(combo)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	id, err := NewEntryID()
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	if err := a.store.CreateEntry(id, req.Label, hash, length); err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id})
}

// PUT /api/entries/{id}/file — upload/replace an entry's file.
func (a *App) handleEntryFile(w http.ResponseWriter, r *http.Request) {
	if !a.adminAuthorized(r) {
		http.Error(w, "", http.StatusForbidden)
		return
	}
	id := r.PathValue("id")
	if !validID(id) || !a.store.EntryExists(id) {
		http.Error(w, "", http.StatusNotFound)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, a.cfg.UploadMax))
	if err != nil {
		http.Error(w, "", http.StatusRequestEntityTooLarge)
		return
	}
	if len(body) == 0 {
		http.Error(w, "empty file", http.StatusBadRequest)
		return
	}
	// Any file type is allowed. The client sends the original name in X-Filename
	// (percent-encoded so non-ASCII names survive the header). We sanitize it
	// against traversal/header injection and fall back to a generic name.
	filename := sanitizeFilename(decodeFilename(r.Header.Get("X-Filename")))
	ct := contentTypeFor(filename, body)
	if filename == "" {
		filename = "secret" + extForType(ct)
	}
	if err := a.store.WriteEntryFile(id, body, ct, filename); err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// DELETE /api/entries/{id} — remove a combination and its file.
func (a *App) handleEntryDelete(w http.ResponseWriter, r *http.Request) {
	if !a.adminAuthorized(r) {
		http.Error(w, "", http.StatusForbidden)
		return
	}
	id := r.PathValue("id")
	if !validID(id) {
		http.Error(w, "", http.StatusNotFound)
		return
	}
	if err := a.store.DeleteEntry(id); err != nil {
		http.Error(w, "", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
}

type configResponse struct {
	Rings    int    `json:"rings"`
	Alphabet string `json:"alphabet"`
}

// GET /api/config — UI shape only (ring count + dialable characters). Carries
// no secret: it describes the cryptex, not any combination.
func (a *App) handleConfig(w http.ResponseWriter, _ *http.Request) {
	// Ring count follows the entries' uniform combination length when set, so
	// adding the first combination reshapes the cryptex. Falls back to the
	// CRYPTEX_RINGS default before any entry exists.
	rings := a.cfg.Rings
	if n, ok := a.store.UniformLen(); ok {
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

// contentDisposition builds an attachment header with both a plain ASCII
// filename (broad compatibility) and an RFC 5987 filename* (so non-ASCII names
// survive). The stored name is already sanitized of quotes/control characters.
func contentDisposition(name string) string {
	ascii := strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e {
			return '_'
		}
		return r
	}, name)
	if ascii == "" {
		ascii = "download"
	}
	return fmt.Sprintf("attachment; filename=%q; filename*=UTF-8''%s",
		ascii, url.PathEscape(name))
}

// decodeFilename undoes the percent-encoding the client applies to X-Filename.
// Falls back to the raw header if it isn't valid encoding.
func decodeFilename(raw string) string {
	if raw == "" {
		return ""
	}
	if dec, err := url.QueryUnescape(raw); err == nil {
		return dec
	}
	return raw
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
