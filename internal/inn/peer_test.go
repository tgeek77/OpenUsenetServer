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
