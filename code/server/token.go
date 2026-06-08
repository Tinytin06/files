package main

// Minimal HMAC-signed tokens (no external JWT dependency). A token is
// base64url(payload) + "." + base64url(HMAC-SHA256(payload)). The payload
// carries a scope and an expiry. The token never contains the password and is
// only a proof that a guess succeeded.

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const (
	ScopeRead      = "read"
	ScopeReadWrite = "read+write"
)

type claims struct {
	Scope string `json:"scope"`
	Exp   int64  `json:"exp"` // unix seconds
	Jti   string `json:"jti"`
}

type TokenManager struct {
	key []byte
	ttl time.Duration
}

func NewTokenManager(key []byte, ttl time.Duration) *TokenManager {
	return &TokenManager{key: key, ttl: ttl}
}

var b64u = base64.RawURLEncoding

// Issue mints a token with the given scope, valid for the manager's TTL.
func (t *TokenManager) Issue(scope string) (string, error) {
	jti := make([]byte, 12)
	if _, err := rand.Read(jti); err != nil {
		return "", err
	}
	c := claims{Scope: scope, Exp: time.Now().Add(t.ttl).Unix(), Jti: b64u.EncodeToString(jti)}
	payload, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	body := b64u.EncodeToString(payload)
	return body + "." + b64u.EncodeToString(t.sign([]byte(body))), nil
}

var errBadToken = errors.New("invalid token")

// Verify checks the signature and expiry and returns the claims.
func (t *TokenManager) Verify(tok string) (*claims, error) {
	body, sig, ok := strings.Cut(tok, ".")
	if !ok {
		return nil, errBadToken
	}
	gotSig, err := b64u.DecodeString(sig)
	if err != nil {
		return nil, errBadToken
	}
	if subtle.ConstantTimeCompare(gotSig, t.sign([]byte(body))) != 1 {
		return nil, errBadToken
	}
	payload, err := b64u.DecodeString(body)
	if err != nil {
		return nil, errBadToken
	}
	var c claims
	if err := json.Unmarshal(payload, &c); err != nil {
		return nil, errBadToken
	}
	if time.Now().Unix() >= c.Exp {
		return nil, errBadToken
	}
	return &c, nil
}

func (c *claims) CanWrite() bool { return c.Scope == ScopeReadWrite }

func (t *TokenManager) sign(body []byte) []byte {
	m := hmac.New(sha256.New, t.key)
	m.Write(body)
	return m.Sum(nil)
}
