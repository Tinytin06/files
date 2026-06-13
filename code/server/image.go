package main

// File handling for uploads. Any file type is accepted: the download endpoint
// always serves with `Content-Disposition: attachment` + `X-Content-Type-Options:
// nosniff`, so a stored file is saved by the browser, never rendered inline.
// We still never trust the upload blindly — the filename is sanitized against
// path traversal/header injection and the body is size-capped by the handler.

import (
	"bytes"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
)

var imageMagic = []struct {
	ct    string
	ext   string
	magic []byte
}{
	{"image/jpeg", ".jpg", []byte{0xFF, 0xD8, 0xFF}},
	{"image/png", ".png", []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}},
	{"image/gif", ".gif", []byte("GIF87a")},
	{"image/gif", ".gif", []byte("GIF89a")},
}

// detectImage returns the content type and extension if data is a supported
// image, or ok=false otherwise. Still used by the legacy migration to label an
// old photo that predates stored metadata. WEBP is the RIFF container case.
func detectImage(data []byte) (ct, ext string, ok bool) {
	for _, m := range imageMagic {
		if bytes.HasPrefix(data, m.magic) {
			return m.ct, m.ext, true
		}
	}
	if len(data) >= 12 && bytes.Equal(data[0:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")) {
		return "image/webp", ".webp", true
	}
	return "", "", false
}

func extForType(ct string) string {
	switch ct {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ".bin"
	}
}

// contentTypeFor picks a content type for an arbitrary uploaded file. It prefers
// the filename extension's registered MIME type (stable and predictable for the
// eventual download) and falls back to sniffing the bytes, then octet-stream.
func contentTypeFor(filename string, data []byte) string {
	if ext := filepath.Ext(filename); ext != "" {
		if ct := mime.TypeByExtension(ext); ct != "" {
			// Strip any "; charset=..." parameter for a clean stored value.
			if i := strings.IndexByte(ct, ';'); i >= 0 {
				ct = strings.TrimSpace(ct[:i])
			}
			return ct
		}
	}
	return http.DetectContentType(data) // never empty; octet-stream when unknown
}

// sanitizeFilename reduces a client-supplied name to a single safe path segment:
// no directory components, no control characters, and no quotes that could break
// out of the Content-Disposition header. Returns "" if nothing usable remains.
func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	// Keep only the final path segment, regardless of separator style.
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '"' {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return ""
	}
	if r := []rune(name); len(r) > 200 {
		name = string(r[:200])
	}
	return name
}
