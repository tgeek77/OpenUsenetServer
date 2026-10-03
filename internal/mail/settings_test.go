package mail

import (
	"context"
	"strings"
	"testing"
)

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
	if got := (Settings{Security: "ssl/tls"}).Normalize().Security; got != SecurityTLS {
		t.Fatalf("ssl/tls %s", got)
	}
}

func TestImplicitTLSOnPort465(t *testing.T) {
	if !implicitTLS(Settings{Port: 465, Security: SecurityAuto}) || !implicitTLS(Settings{Port: 465, Security: SecuritySTARTTLS}) {
		t.Fatal("port 465 should speak SSL/TLS from the first byte")
	}
	if implicitTLS(Settings{Port: 587, Security: SecurityAuto}) || implicitTLS(Settings{Port: 25, Security: SecurityPlain}) {
		t.Fatal("587 and plain port 25 stay in the clear until STARTTLS")
	}
	err := Send(context.Background(), Settings{
		Host: "127.0.0.1", Port: 465, Security: SecurityPlain, From: "news@example.org",
	}, Message{To: []string{"a@example.org"}, Subject: "x", Body: "y"})
	if err == nil || !strings.Contains(err.Error(), "SSL/TLS") {
		t.Fatalf("plain 465 %v", err)
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
