package mail

import "testing"

func TestNormalizeDefaultPorts(t *testing.T) {
	if got := (Settings{Security: "plain"}).Normalize().Port; got != 25 {
		t.Fatalf("plain port %d", got)
	}
	if got := (Settings{Security: "starttls"}).Normalize().Port; got != 587 {
		t.Fatalf("starttls port %d", got)
	}
	if got := (Settings{Security: "tls"}).Normalize().Port; got != 465 {
		t.Fatalf("tls port %d", got)
	}
	if got := (Settings{Security: "ssl"}).Normalize().Security; got != SecurityTLS {
		t.Fatalf("security %s", got)
	}
}

func TestValidateHost(t *testing.T) {
	if err := (Settings{Host: "smtp://mail.example", From: "a@b.c"}).Validate(); err == nil {
		t.Fatal("expected scheme rejection")
	}
	if err := (Settings{Host: "mail.example", Port: 25, From: "News <news@example.org>", Security: "plain"}).Validate(); err != nil {
		t.Fatal(err)
	}
	addr, err := Envelope("News <news@example.org>")
	if err != nil || addr != "news@example.org" {
		t.Fatalf("%q %v", addr, err)
	}
}

func TestForSendKeepsBaseFrom(t *testing.T) {
	base := Settings{Host: "127.0.0.1", Port: 25, From: "news@example.org", Security: SecurityPlain}
	over := Settings{Host: "smtp.example", Port: 587}
	got := ForSend(base, over)
	if got.Host != "smtp.example" || got.From != "news@example.org" || got.Security != SecurityAuto {
		t.Fatalf("%+v", got)
	}
	if got := ForSend(base, Settings{}); got.Host != "127.0.0.1" {
		t.Fatalf("%+v", got)
	}
}
