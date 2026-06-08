package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// App holds the wired-up dependencies shared across handlers.
type App struct {
	store   *Store
	tokens  *TokenManager
	limiter *RateLimiter
	kem     *KEM
	cfg     Config
}

type Config struct {
	ListenAddr   string
	DataDir      string
	WebDir       string
	AdminToken   string
	UploadMax    int64
	TokenTTL     time.Duration
	MinUnlockDur time.Duration
	// UI shape, served to the client via GET /api/config. Set CRYPTEX_RINGS to
	// match your combination's length, and CRYPTEX_ALPHABET to the characters
	// the rings can dial. Neither reveals the password value.
	Rings    int
	Alphabet string
}

func loadConfig() Config {
	return Config{
		ListenAddr:   env("LISTEN_ADDR", ":8080"),
		DataDir:      env("DATA_DIR", "./data"),
		WebDir:       env("WEB_DIR", ""),
		AdminToken:   os.Getenv("ADMIN_TOKEN"),
		UploadMax:    envInt64("MAX_UPLOAD_BYTES", 10<<20), // 10 MiB
		TokenTTL:     time.Duration(envInt64("TOKEN_TTL_SECONDS", 600)) * time.Second,
		MinUnlockDur: time.Duration(envInt64("MIN_UNLOCK_MS", 250)) * time.Millisecond,
		Rings:        int(envInt64("CRYPTEX_RINGS", 5)),
		Alphabet:     env("CRYPTEX_ALPHABET", "ABCDEFGHIJKLMNOPQRSTUVWXYZ"),
	}
}

func main() {
	cfg := loadConfig()

	store, err := NewStore(cfg.DataDir)
	if err != nil {
		log.Fatalf("store: %v", err)
	}

	// Fold any legacy single-secret layout into a "default" entry.
	if err := migrateLegacy(store); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	// Optional one-time bootstrap of a default combination from the environment,
	// so a fresh deploy is unlockable without a manual admin call.
	if init := os.Getenv("CRYPTEX_INIT_PASSWORD"); init != "" && store.EntryCount() == 0 {
		h, err := HashPassword(init)
		if err != nil {
			log.Fatalf("init password: %v", err)
		}
		id, err := NewEntryID()
		if err != nil {
			log.Fatalf("init password: %v", err)
		}
		if err := store.CreateEntry(id, "default", h, len([]rune(init))); err != nil {
			log.Fatalf("init password: %v", err)
		}
		log.Print("initialized default entry from CRYPTEX_INIT_PASSWORD")
	}
	if store.EntryCount() == 0 {
		log.Print("WARNING: no entries set; unlock will always 401. Add one in the admin panel (admin token) or set CRYPTEX_INIT_PASSWORD.")
	}

	kem, err := NewKEM(cfg.DataDir)
	if err != nil {
		log.Fatalf("kem: %v", err)
	}

	signingKey := tokenSigningKey()
	app := &App{
		store:   store,
		tokens:  NewTokenManager(signingKey, cfg.TokenTTL),
		limiter: NewRateLimiter(),
		kem:     kem,
		cfg:     cfg,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/kem", app.handleKEM)
	mux.HandleFunc("POST /api/unlock", app.handleUnlock)
	mux.HandleFunc("GET /api/photo", app.handlePhotoGet)
	mux.HandleFunc("GET /api/entries", app.handleEntriesList)
	mux.HandleFunc("POST /api/entries", app.handleEntryCreate)
	mux.HandleFunc("PUT /api/entries/{id}/file", app.handleEntryFile)
	mux.HandleFunc("DELETE /api/entries/{id}", app.handleEntryDelete)
	mux.HandleFunc("GET /api/config", app.handleConfig)
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Serve the built Svelte SPA, if present, with history-fallback routing.
	if cfg.WebDir != "" {
		mux.Handle("/", spaHandler(cfg.WebDir))
		log.Printf("serving web UI from %s", cfg.WebDir)
	}

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           securityHeaders(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("listening on %s", cfg.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	log.Print("shut down")
}

// spaHandler serves static files from dir, falling back to index.html for
// client-side routes (anything that isn't an existing file or an /api path).
func spaHandler(dir string) http.Handler {
	fs := http.FileServer(http.Dir(dir))
	index := filepath.Join(dir, "index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		clean := filepath.Join(dir, filepath.Clean(r.URL.Path))
		if st, err := os.Stat(clean); err == nil && !st.IsDir() {
			fs.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, index)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// tokenSigningKey reads TOKEN_SIGNING_KEY, or generates an ephemeral one.
// An ephemeral key means a restart invalidates all outstanding unlock tokens —
// set a persistent value in production if you don't want that.
func tokenSigningKey() []byte {
	if k := os.Getenv("TOKEN_SIGNING_KEY"); k != "" {
		return []byte(k)
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		log.Fatalf("signing key: %v", err)
	}
	log.Print("WARNING: TOKEN_SIGNING_KEY not set; using an ephemeral key (tokens invalidated on restart)")
	return b
}

func constTimeEqualStr(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt64(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}
