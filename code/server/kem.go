package main

// Post-quantum transport for the secret. The guess (and a new combination) are
// never sent as plaintext JSON, even over TLS: the client encapsulates a shared
// secret to the server's ML-KEM-768 public key, encrypts the value with
// AES-256-GCM under that secret, and sends the sealed envelope. The server
// decapsulates with its private key and decrypts. A tampered or wrong-key
// envelope simply fails to open -> treated as a bad request / wrong guess.

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/mlkem"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
)

type KEM struct {
	dk     *mlkem.DecapsulationKey768
	pubB64 string
}

// envelope is the sealed payload posted by the client.
type envelope struct {
	Kem        string `json:"kem"`        // ML-KEM ciphertext (base64)
	Nonce      string `json:"nonce"`      // AES-GCM nonce (base64)
	Ciphertext string `json:"ciphertext"` // AES-GCM ciphertext||tag (base64)
}

// NewKEM loads the decapsulation key seed from the data volume, or generates and
// persists one so the published public key is stable across restarts.
func NewKEM(dataDir string) (*KEM, error) {
	path := filepath.Join(dataDir, "kem.seed")
	dk, err := loadKEMSeed(path)
	if err != nil {
		return nil, err
	}
	pub := dk.EncapsulationKey().Bytes()
	return &KEM{dk: dk, pubB64: base64.StdEncoding.EncodeToString(pub)}, nil
}

func loadKEMSeed(path string) (*mlkem.DecapsulationKey768, error) {
	if seed, err := os.ReadFile(path); err == nil {
		return mlkem.NewDecapsulationKey768(seed)
	}
	dk, err := mlkem.GenerateKey768()
	if err != nil {
		return nil, err
	}
	if err := atomicWrite(path, dk.Bytes(), 0o600); err != nil {
		return nil, err
	}
	return dk, nil
}

// PublicKeyB64 is the ML-KEM-768 encapsulation (public) key, base64-encoded.
func (k *KEM) PublicKeyB64() string { return k.pubB64 }

var errSeal = errors.New("could not open sealed envelope")

// Open decapsulates and decrypts a sealed envelope, returning the plaintext.
func (k *KEM) Open(env envelope) ([]byte, error) {
	kemCT, err := base64.StdEncoding.DecodeString(env.Kem)
	if err != nil {
		return nil, errSeal
	}
	shared, err := k.dk.Decapsulate(kemCT)
	if err != nil {
		return nil, errSeal
	}
	block, err := aes.NewCipher(shared) // 32-byte shared secret -> AES-256
	if err != nil {
		return nil, errSeal
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errSeal
	}
	nonce, err := base64.StdEncoding.DecodeString(env.Nonce)
	if err != nil || len(nonce) != gcm.NonceSize() {
		return nil, errSeal
	}
	data, err := base64.StdEncoding.DecodeString(env.Ciphertext)
	if err != nil {
		return nil, errSeal
	}
	plain, err := gcm.Open(nil, nonce, data, nil)
	if err != nil {
		return nil, errSeal
	}
	return plain, nil
}
