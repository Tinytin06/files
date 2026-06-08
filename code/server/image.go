package main

// Image type detection by magic bytes — we never trust a client-supplied
// extension or Content-Type header when accepting an upload.

import "bytes"

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
// image, or ok=false otherwise. WEBP is handled separately (RIFF container).
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
