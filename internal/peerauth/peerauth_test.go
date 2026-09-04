package peerauth

import (
	"testing"

	"openusenet/internal/store"
)

func TestPairPasswordSymmetric(t *testing.T) {
	a := PairPassword("news-a", "news-b")
	b := PairPassword("news-b", "news-a")
	if a == "" || a != b {
		t.Fatalf("a=%q b=%q", a, b)
	}
}

func TestFillPasswords(t *testing.T) {
	p := store.Peer{Host: "news-b", IncomingHost: "news-b"}
	FillPasswords("news-a", &p)
	if p.IncomingPassword == "" || p.OutgoingPassword == "" {
		t.Fatal("expected passwords filled")
	}
	if p.IncomingPassword != p.OutgoingPassword {
		t.Fatal("pair password should match both directions")
	}
}

func TestVerifyFeedAuth(t *testing.T) {
	p := &store.Peer{IncomingPassword: "secret"}
	if !VerifyFeedAuth(p, "secret", true) {
		t.Fatal("expected ok")
	}
	if VerifyFeedAuth(p, "wrong", true) {
		t.Fatal("expected fail")
	}
}
