// Package secretbox seals short secrets for at-rest storage with
// AES-256-GCM (One-Click Connect D6). The key is the operator's
// GOCOV_SECRET_KEY, hex-decoded; sealed values are self-describing
// strings safe for TEXT columns.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

// prefix versions the sealed format so a future scheme can coexist.
const prefix = "v1:"

// keyLen is the AES-256 key length in bytes; the operator supplies it
// as hex, so a well-formed key is exactly twice this many characters.
const keyLen = 32

// Box seals and opens secrets. Safe for concurrent use.
type Box struct {
	aead cipher.AEAD
}

// New builds a Box from a 64-hex-character key (`openssl rand -hex 32`).
// The hex decodes straight into the AES-256 key: the value is uniform
// key material, not a memorable passphrase, so there is no low-entropy
// input for a KDF's work factor to stretch. Anything of another shape
// is refused here rather than silently sealing under a weak key.
func New(hexKey string) (*Box, error) {
	// The key itself never reaches the error string — hex.DecodeString's
	// own message quotes the offending byte.
	key, err := hex.DecodeString(hexKey)
	if err != nil || len(key) != keyLen {
		return nil, fmt.Errorf("secretbox: key must be %d hex characters", 2*keyLen)
	}
	block, err := aes.NewCipher(key)
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
