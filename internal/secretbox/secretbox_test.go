package secretbox

import (
	"strings"
	"testing"
)

// Two well-formed keys, as `openssl rand -hex 32` would produce them.
const (
	keyOne = "4b1d0f8a2c6e59d3a7f014b8e2c95d36a8b7c40e1f2a3b4c5d6e7f8091a2b3c4"
	keyTwo = "9f3e2d1c0b9a8776655443322110ffeeddccbbaa99887766554433221100aabb"
)

func TestRoundTrip(t *testing.T) {
	b, err := New(keyOne)
	if err != nil {
		t.Fatal(err)
	}
	for _, plain := range []string{"", "refresh-token-value", strings.Repeat("x", 4096)} {
		sealed, err := b.Seal(plain)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(sealed, "v1:") {
			t.Errorf("sealed = %q, want v1: prefix", sealed[:8])
		}
		if strings.Contains(sealed, plain) && plain != "" {
			t.Error("sealed value contains the plaintext")
		}
		got, err := b.Open(sealed)
		if err != nil {
			t.Fatal(err)
		}
		if got != plain {
			t.Errorf("roundtrip = %q, want %q", got, plain)
		}
	}
}

func TestSealRandomized(t *testing.T) {
	b, _ := New(keyOne)
	s1, _ := b.Seal("same")
	s2, _ := b.Seal("same")
	if s1 == s2 {
		t.Error("two seals of the same value are identical (nonce reuse?)")
	}
}

func TestOpenRejects(t *testing.T) {
	b, _ := New(keyOne)
	sealed, _ := b.Seal("secret")

	other, _ := New(keyTwo)
	if _, err := other.Open(sealed); err == nil {
		t.Error("wrong key accepted")
	}
	tampered := sealed[:len(sealed)-2] + "AA"
	if _, err := b.Open(tampered); err == nil {
		t.Error("tampered value accepted")
	}
	if _, err := b.Open("plaintext-in-db"); err == nil {
		t.Error("unsealed value accepted")
	}
	if _, err := b.Open("v1:!!!"); err == nil {
		t.Error("bad base64 accepted")
	}
}

// A key of any other shape is refused outright: New must not stretch a
// memorable passphrase into a cipher key.
func TestNewRejectsMalformedKey(t *testing.T) {
	for name, key := range map[string]string{
		"empty":       "",
		"passphrase":  "some operator key",
		"too short":   keyOne[:62],
		"too long":    keyOne + "ab",
		"odd length":  keyOne[:63],
		"not hex":     strings.Repeat("z", 64),
		"binary key":  strings.Repeat("\x00", 32),
		"leading tab": "\t" + keyOne[:62],
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := New(key); err == nil {
				t.Errorf("New(%q) = nil error, want rejection", key)
			}
		})
	}
}

// The rejection message says only what shape is wanted: a key that
// reaches a log line is a key that has to be rotated.
func TestNewErrorHidesKey(t *testing.T) {
	_, err := New("zz3e2d1c0b9a8776655443322110ffeeddccbbaa99887766554433221100aabb")
	if err == nil {
		t.Fatal("malformed key accepted")
	}
	if got, want := err.Error(), "secretbox: key must be 64 hex characters"; got != want {
		t.Errorf("New error = %q, want %q", got, want)
	}
}
