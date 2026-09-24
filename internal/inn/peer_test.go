package inn

import (
	"strings"
	"testing"
)

func TestParseEternalSeptember(t *testing.T) {
	incoming := `peer eternal-september {
    hostname:       feeder.eternal-september.org
}`
	s, warns := ParseFile(incoming, "incoming")
	if s == nil || s.IncomingHost != "feeder.eternal-september.org" {
		t.Fatalf("incoming: %#v warns=%v", s, warns)
	}
	if s.Name != "eternal-september" {
		t.Fatalf("name %q", s.Name)
	}

	innfeed := `peer eternal-september {
    ip-name:        feeder.eternal-september.org
    port-number:    433
}`
	s, _ = ParseFile(innfeed, "innfeed")
	if s == nil || s.OutgoingHost != "feeder.eternal-september.org" || s.Port != 433 {
		t.Fatalf("innfeed: %#v", s)
	}

	newsfeeds := `eternal-september/eternal-september.org:*,@*.bina*,@*.bain*,@*.dateien*,@*.pictures*,!local*,!junk/!local:Ap,Tm:innfeed!`
	s, _ = ParseFile(newsfeeds, "newsfeeds")
	if s == nil || s.PathToken != "eternal-september.org" || s.Flags != "Ap,Tm" || s.Distributions != "!local" {
		t.Fatalf("newsfeeds: %#v", s)
	}
}

func TestParsePasteEmails(t *testing.T) {
	neodome := `
## innfeed.conf
peer neodome.net {
    ip-name: news-in.neodome.net
    port-number: 119
}

## newsfeeds
news.neodome.net/news.neodome.net\
  :*,!local*\
  :Ap,Tm,<65536:innfeed!

## incoming.conf
peer neodome.net {
  hostname: news-out.neodome.net
}
`
	s, warns, present := ParsePaste(neodome)
	if s == nil {
		t.Fatal(warns)
	}
	if s.OutgoingHost != "news-in.neodome.net" || s.IncomingHost != "news-out.neodome.net" {
		t.Fatalf("hosts out=%q in=%q", s.OutgoingHost, s.IncomingHost)
	}
	if s.Name != "news.neodome.net" || s.PathToken != "news.neodome.net" {
		t.Fatalf("name=%q path=%q", s.Name, s.PathToken)
	}
	if s.Patterns != "*,!local*" || s.Flags != "Ap,Tm,<65536" || s.Port != 119 {
		t.Fatalf("pat=%q flags=%q port=%d", s.Patterns, s.Flags, s.Port)
	}
	if len(present) == 0 {
		t.Fatal("expected present fields")
	}

	aulich := `
I use INN with CF-NG and PF-NG

Details:

news.aulich.net

IPV4: 85.31.187.13

IPV6: 2a02:180:2:83::7

Pattern: :*,!unidata.*,!control.*,!junk/!local\
`
	s, _, present = ParsePaste(aulich)
	if s == nil || s.Name != "news.aulich.net" || s.OutgoingHost != "news.aulich.net" {
		t.Fatalf("aulich %#v", s)
	}
	if s.Patterns != "*,!unidata.*,!control.*,!junk" || s.Distributions != "!local" {
		t.Fatalf("pat=%q dist=%q", s.Patterns, s.Distributions)
	}
	joined := strings.Join(s.Warnings, " ")
	if !strings.Contains(joined, "85.31.187.13") || !strings.Contains(joined, "2a02:180:2:83::7") {
		t.Fatal(joined)
	}
	_ = present
}

func TestParseIncomingPassword(t *testing.T) {
	incoming := `peer news-b {
    hostname:       news-b.example
    password:       s3cret
}`
	s, _ := ParseFile(incoming, "incoming")
	if s == nil || s.Password != "s3cret" {
		t.Fatalf("password: %#v", s)
	}
	out := FormatIncoming(*s)
	if !strings.Contains(out, "password:") || !strings.Contains(out, "s3cret") {
		t.Fatal(out)
	}
}

func TestFormatRoundTrip(t *testing.T) {
	s := Spec{
		Name: "eternal-september", PathToken: "eternal-september.org",
		IncomingHost: "feeder.eternal-september.org", OutgoingHost: "feeder.eternal-september.org",
		Port: 433, Patterns: "*,@*.bina*", Distributions: "!local", Flags: "Ap,Tm",
	}
	snippets := Snippets(s)
	if !strings.Contains(snippets["incoming"], "feeder.eternal-september.org") {
		t.Fatal(snippets["incoming"])
	}
	if !strings.Contains(snippets["innfeed"], "433") {
		t.Fatal(snippets["innfeed"])
	}
	if !strings.Contains(snippets["newsfeeds"], "eternal-september/eternal-september.org") {
		t.Fatal(snippets["newsfeeds"])
	}
}
