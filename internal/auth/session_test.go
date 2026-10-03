package auth

import (
	"testing"
	"time"
)

func TestSessionSealOpen(t *testing.T) {
	s := NewSigner([]byte("test-key"))
	exp := time.Now().Add(time.Hour).Truncate(time.Second)
	raw, err := s.Seal(7, 2, exp, true)
	if err != nil {
		t.Fatal(err)
	}
	tok, ok := s.Open(raw)
	if !ok {
		t.Fatal("open")
	}
	if tok.UserID != 7 || tok.Gen != 2 || !tok.Remember || !tok.Expiry.Equal(exp) {
		t.Fatalf("%#v", tok)
	}
	if _, ok := s.Open(raw + "x"); ok {
		t.Fatal("tampered token accepted")
	}
	other := NewSigner([]byte("other-key"))
	if _, ok := other.Open(raw); ok {
		t.Fatal("wrong key accepted")
	}
}

func TestSessionExpired(t *testing.T) {
	s := NewSigner([]byte("test-key"))
	raw, err := s.Seal(1, 0, time.Now().Add(-time.Second), false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Open(raw); ok {
		t.Fatal("expired token accepted")
	}
}
