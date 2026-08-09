package secretbox

import (
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	b, err := New("some operator key")
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
	b, _ := New("k")
	s1, _ := b.Seal("same")
	s2, _ := b.Seal("same")
	if s1 == s2 {
		t.Error("two seals of the same value are identical (nonce reuse?)")
	}
}

func TestOpenRejects(t *testing.T) {
	b, _ := New("key-one")
	sealed, _ := b.Seal("secret")

	other, _ := New("key-two")
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

func TestNewRejectsEmptyKey(t *testing.T) {
	if _, err := New(""); err == nil {
		t.Error("empty passphrase accepted")
	}
}
