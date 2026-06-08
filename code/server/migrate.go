package main

// One-time migration from the legacy single-secret layout to the multi-entry
// layout. If the old files exist and no entries do yet, fold them into a single
// "default" entry, then remove the old files. Idempotent: a no-op once migrated.
//
//	password.hash + password.json + photo.bin + photo.json
//	  -> entries/<id>/{combo.hash, meta.json, file.bin}

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
)

func migrateLegacy(s *Store) error {
	oldHash := filepath.Join(s.dir, "password.hash")
	b, err := os.ReadFile(oldHash)
	if err != nil {
		return nil // nothing to migrate
	}
	if s.EntryCount() > 0 {
		return nil // already have entries; leave legacy files alone
	}

	hash := strings.TrimSpace(string(b))

	// Length from the legacy password.json, falling back to the hash being set.
	length := 0
	if lb, err := os.ReadFile(filepath.Join(s.dir, "password.json")); err == nil {
		var m struct {
			Len int `json:"len"`
		}
		if json.Unmarshal(lb, &m) == nil {
			length = m.Len
		}
	}

	id, err := NewEntryID()
	if err != nil {
		return err
	}
	if err := s.CreateEntry(id, "default", hash, length); err != nil {
		return err
	}

	// Carry over the photo, if any, with its content type + filename.
	if data, err := os.ReadFile(filepath.Join(s.dir, "photo.bin")); err == nil {
		ct, filename := "", ""
		if pb, err := os.ReadFile(filepath.Join(s.dir, "photo.json")); err == nil {
			var pm struct {
				ContentType string `json:"content_type"`
				Filename    string `json:"filename"`
			}
			if json.Unmarshal(pb, &pm) == nil {
				ct, filename = pm.ContentType, pm.Filename
			}
		}
		if ct == "" {
			if detected, _, ok := detectImage(data); ok {
				ct = detected
			}
		}
		if filename == "" {
			filename = "secret" + extForType(ct)
		}
		if err := s.WriteEntryFile(id, data, ct, filename); err != nil {
			return err
		}
	}

	// Remove the legacy files now that the entry owns the data.
	for _, name := range []string{"password.hash", "password.json", "photo.bin", "photo.json"} {
		_ = os.Remove(filepath.Join(s.dir, name))
	}
	log.Printf("migrated legacy combination + photo into entry %s (label \"default\")", id)
	return nil
}
