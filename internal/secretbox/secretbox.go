// Package secretbox seals short secrets for at-rest storage with
// AES-256-GCM (One-Click Connect D6). The key is derived from an
// operator-supplied passphrase; sealed values are self-describing
// strings safe for TEXT columns.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
)

// prefix versions the sealed format so a future scheme can coexist.
const prefix = "v1:"

// Box seals and opens secrets. Safe for concurrent use.
type Box struct {
	aead cipher.AEAD
}

// New derives the AES-256 key from the passphrase via SHA-256 — the
// passphrase is an operator secret of arbitrary shape (GOCOV_SECRET_KEY),
// not a low-entropy password, so a KDF with a work factor buys nothing.
func New(passphrase string) (*Box, error) {
	if passphrase == "" {
		return nil, fmt.Errorf("secretbox: empty key")
	}
	sum := sha256.Sum256([]byte(passphrase))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Seal encrypts plain into a "v1:<base64(nonce||ciphertext)>" string.
func (b *Box) Seal(plain string) (string, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := b.aead.Seal(nonce, nonce, []byte(plain), nil)
	return prefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// Open decrypts a value produced by Seal. Fails on tampering, a wrong
// key, or an unrecognized format.
func (b *Box) Open(sealed string) (string, error) {
	raw, ok := strings.CutPrefix(sealed, prefix)
	if !ok {
		return "", fmt.Errorf("secretbox: unrecognized sealed format")
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return "", fmt.Errorf("secretbox: %w", err)
	}
	if len(data) < b.aead.NonceSize() {
		return "", fmt.Errorf("secretbox: sealed value too short")
	}
	plain, err := b.aead.Open(nil, data[:b.aead.NonceSize()], data[b.aead.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("secretbox: %w", err)
	}
	return string(plain), nil
}
