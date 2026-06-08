package main

// Store is the only thing that touches the mounted data volume. It holds a set
// of entries, each its own folder under entries/<id>/ with a combination hash,
// metadata, and (optionally) the protected file. Nothing here is reachable
// except through the token-checked handlers; the files live outside any web root.
//
//	entries/<id>/combo.hash   argon2id PHC string
//	entries/<id>/meta.json    { label, len, content_type, filename }
//	entries/<id>/file.bin     protected file (absent until uploaded)

import (
	"crypto/rand"
	"encoding/hex"
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

// EntryMeta is the per-entry metadata stored next to the file.
type EntryMeta struct {
	Label       string `json:"label"`
	Len         int    `json:"len"`
	ContentType string `json:"content_type"`
	Filename    string `json:"filename"`
}

// EntryInfo is the non-secret summary returned to the admin listing.
type EntryInfo struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Len     int    `json:"len"`
	HasFile bool   `json:"has_file"`
}

func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(dir, "entries"), 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) entriesDir() string        { return filepath.Join(s.dir, "entries") }
func (s *Store) entryDir(id string) string { return filepath.Join(s.entriesDir(), id) }
func (s *Store) hashPath(id string) string { return filepath.Join(s.entryDir(id), "combo.hash") }
func (s *Store) metaPath(id string) string { return filepath.Join(s.entryDir(id), "meta.json") }
func (s *Store) filePath(id string) string { return filepath.Join(s.entryDir(id), "file.bin") }

// NewEntryID returns a random hex id safe for use as a directory name.
func NewEntryID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// validID guards the {id} path parameter against traversal; ids we mint are hex.
func validID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

func (s *Store) listIDs() ([]string, error) {
	ents, err := os.ReadDir(s.entriesDir())
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range ents {
		if e.IsDir() && validID(e.Name()) {
			ids = append(ids, e.Name())
		}
	}
	return ids, nil
}

func (s *Store) readMeta(id string) (EntryMeta, error) {
	var m EntryMeta
	b, err := os.ReadFile(s.metaPath(id))
	if err != nil {
		return m, err
	}
	err = json.Unmarshal(b, &m)
	return m, err
}

// --- listing & counts ---

// ListEntries returns the non-secret summary of every entry.
func (s *Store) ListEntries() ([]EntryInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids, err := s.listIDs()
	if err != nil {
		return nil, err
	}
	out := make([]EntryInfo, 0, len(ids))
	for _, id := range ids {
		m, err := s.readMeta(id)
		if err != nil {
			continue
		}
		_, statErr := os.Stat(s.filePath(id))
		out = append(out, EntryInfo{ID: id, Label: m.Label, Len: m.Len, HasFile: statErr == nil})
	}
	return out, nil
}

// EntryCount reports how many entries exist.
func (s *Store) EntryCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids, _ := s.listIDs()
	return len(ids)
}

// IDHash pairs an entry id with its stored combination hash.
type IDHash struct {
	ID   string
	Hash string
}

// EntryHashes returns every entry's id + combination hash for unlock matching.
func (s *Store) EntryHashes() ([]IDHash, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids, err := s.listIDs()
	if err != nil {
		return nil, err
	}
	out := make([]IDHash, 0, len(ids))
	for _, id := range ids {
		b, err := os.ReadFile(s.hashPath(id))
		if err != nil {
			continue
		}
		out = append(out, IDHash{ID: id, Hash: strings.TrimSpace(string(b))})
	}
	return out, nil
}

// UniformLen returns the combination length shared by all entries, if any exist.
func (s *Store) UniformLen() (int, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids, err := s.listIDs()
	if err != nil || len(ids) == 0 {
		return 0, false
	}
	m, err := s.readMeta(ids[0])
	if err != nil || m.Len <= 0 {
		return 0, false
	}
	return m.Len, true
}

// --- mutation ---

// CreateEntry writes a new entry's hash + metadata (no file yet).
func (s *Store) CreateEntry(id, label, hash string, length int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.entryDir(id), 0o700); err != nil {
		return err
	}
	if err := atomicWrite(s.hashPath(id), []byte(hash+"\n"), 0o600); err != nil {
		return err
	}
	return s.writeMetaLocked(id, EntryMeta{Label: label, Len: length})
}

func (s *Store) EntryExists(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, err := os.Stat(s.hashPath(id))
	return err == nil
}

// DeleteEntry removes an entry's folder (hash + meta + file).
func (s *Store) DeleteEntry(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Stat(s.entryDir(id)); err != nil {
		return errNoEntry
	}
	return os.RemoveAll(s.entryDir(id))
}

var (
	errNoEntry = errors.New("no such entry")
	errNoFile  = errors.New("entry has no file")
)

// ReadEntryFile returns an entry's file bytes and metadata.
func (s *Store) ReadEntryFile(id string) ([]byte, EntryMeta, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	meta, err := s.readMeta(id)
	if err != nil {
		return nil, meta, errNoEntry
	}
	data, err := os.ReadFile(s.filePath(id))
	if err != nil {
		return nil, meta, errNoFile
	}
	if meta.ContentType == "" {
		meta.ContentType = http.DetectContentType(data)
	}
	if meta.Filename == "" {
		meta.Filename = "secret" + extForType(meta.ContentType)
	}
	return data, meta, nil
}

// WriteEntryFile stores/replaces an entry's file and updates its metadata.
func (s *Store) WriteEntryFile(id string, data []byte, ct, filename string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	meta, err := s.readMeta(id)
	if err != nil {
		return errNoEntry
	}
	meta.ContentType = ct
	meta.Filename = filename
	if err := atomicWrite(s.filePath(id), data, 0o600); err != nil {
		return err
	}
	return s.writeMetaLocked(id, meta)
}

func (s *Store) writeMetaLocked(id string, meta EntryMeta) error {
	mb, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return atomicWrite(s.metaPath(id), mb, 0o600)
}

// atomicWrite writes to a temp file in the same dir then renames over the
// target so a crash mid-write can never leave a half-written secret/file.
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
