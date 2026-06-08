package main

// Store is the only thing that touches the mounted data volume. It holds the
// password hash and the protected photo. Nothing here is reachable except
// through the token-checked handlers; the files live outside any web root.

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Store struct {
	dir string
	mu  sync.RWMutex
}

type photoMeta struct {
	ContentType string `json:"content_type"`
	Filename    string `json:"filename"`
}

func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) hashPath() string  { return filepath.Join(s.dir, "password.hash") }
func (s *Store) photoPath() string { return filepath.Join(s.dir, "photo.bin") }
func (s *Store) metaPath() string  { return filepath.Join(s.dir, "photo.json") }

// --- password hash ---

func (s *Store) HasPassword() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, err := os.Stat(s.hashPath())
	return err == nil
}

func (s *Store) ReadPasswordHash() (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, err := os.ReadFile(s.hashPath())
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// WritePasswordHash stores the hash plus the combination's character length.
// The length (not the value) lets the UI render one ring per character. The
// plaintext is never written — only its length.
func (s *Store) WritePasswordHash(encoded string, length int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := atomicWrite(s.hashPath(), []byte(encoded+"\n"), 0o600); err != nil {
		return err
	}
	mb, err := json.Marshal(pwMeta{Len: length})
	if err != nil {
		return err
	}
	return atomicWrite(s.pwMetaPath(), mb, 0o600)
}

type pwMeta struct {
	Len int `json:"len"`
}

func (s *Store) pwMetaPath() string { return filepath.Join(s.dir, "password.json") }

// ReadPasswordLen returns the stored combination length, if known.
func (s *Store) ReadPasswordLen() (int, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, err := os.ReadFile(s.pwMetaPath())
	if err != nil {
		return 0, false
	}
	var m pwMeta
	if json.Unmarshal(b, &m) != nil || m.Len <= 0 {
		return 0, false
	}
	return m.Len, true
}

// --- photo ---

var errNoPhoto = errors.New("no photo stored")

func (s *Store) ReadPhoto() (data []byte, meta photoMeta, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, err = os.ReadFile(s.photoPath())
	if err != nil {
		if os.IsNotExist(err) {
			err = errNoPhoto
		}
		return
	}
	mb, merr := os.ReadFile(s.metaPath())
	if merr == nil {
		_ = json.Unmarshal(mb, &meta)
	}
	if meta.ContentType == "" {
		meta.ContentType = http.DetectContentType(data)
	}
	if meta.Filename == "" {
		meta.Filename = "secret" + extForType(meta.ContentType)
	}
	return
}

func (s *Store) WritePhoto(data []byte, meta photoMeta) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	mb, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	if err := atomicWrite(s.photoPath(), data, 0o600); err != nil {
		return err
	}
	return atomicWrite(s.metaPath(), mb, 0o600)
}

// atomicWrite writes to a temp file in the same dir then renames over the
// target so a crash mid-write can never leave a half-written secret/photo.
func atomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op if the rename succeeded
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		// Windows can't rename over an existing file; retry after removing.
		_ = os.Remove(path)
		return os.Rename(tmpName, path)
	}
	return nil
}
