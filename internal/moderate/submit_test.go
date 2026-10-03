package moderate

import (
	"context"
	"errors"
	"strings"
	"testing"

	"openusenet/internal/mail"
)

func TestAddressPublicDefault(t *testing.T) {
	addr, err := Address("misc.test.moderated", nil)
	if err != nil {
		t.Fatal(err)
	}
	if addr != "misc-test-moderated@moderators.isc.org" {
		t.Fatalf("addr %q", addr)
	}
	addr, err = Address("alt.test-moderated", nil)
	if err != nil {
		t.Fatal(err)
	}
	if addr != "alt-test-moderated@moderators.isc.org" {
		t.Fatalf("existing hyphen %q", addr)
	}
}

func TestAddressFirstLocalRule(t *testing.T) {
	rules := []Rule{
		{Pattern: "local.*", Address: "desk@example.org"},
		{Pattern: "*", Address: "other@example.org"},
	}
	addr, err := Address("local.announce", rules)
	if err != nil {
		t.Fatal(err)
	}
	if addr != "desk@example.org" {
		t.Fatalf("local %q", addr)
	}
	addr, err = Address("misc.test.moderated", rules)
	if err != nil {
		t.Fatal(err)
	}
	if addr != "other@example.org" {
		t.Fatalf("star should win before the public default, got %q", addr)
	}
	addr, err = Address("misc.test.moderated", []Rule{{Pattern: "local.*", Address: "desk@example.org"}})
	if err != nil {
		t.Fatal(err)
	}
	if addr != "misc-test-moderated@moderators.isc.org" {
		t.Fatalf("default %q", addr)
	}
}

func TestApplyTemplate(t *testing.T) {
	addr, err := ApplyTemplate("news.groups", "mod-%s@example.org")
	if err != nil {
		t.Fatal(err)
	}
	if addr != "mod-news-groups@example.org" {
		t.Fatalf("%q", addr)
	}
	addr, err = ApplyTemplate("a.b", "100%%@example.org")
	if err != nil {
		t.Fatal(err)
	}
	if addr != "100%@example.org" {
		t.Fatalf("percent %q", addr)
	}
	addr, err = ApplyTemplate("local.test", "moderator@localhost")
	if err != nil || addr != "moderator@localhost" {
		t.Fatalf("localhost %q %v", addr, err)
	}
	if _, err := ApplyTemplate("a.b", "%s-%s@example.org"); err == nil {
		t.Fatal("two substitutions accepted")
	}
	if _, err := ApplyTemplate("a.b", "%d@example.org"); err == nil {
		t.Fatal("unknown conversion accepted")
	}
	if _, _, err := NormalizeRule("a,b.*", "mod@example.org"); err == nil {
		t.Fatal("comma pattern accepted")
	}
	if _, _, err := NormalizeRule("!local.*", "mod@example.org"); err == nil {
		t.Fatal("negated pattern accepted")
	}
}

func TestTargetFirstModerated(t *testing.T) {
	lookup := func(name string) (string, bool, error) {
		switch name {
		case "alt.test":
			return "y", true, nil
		case "misc.test.moderated":
			return "m", true, nil
		case "missing":
			return "", false, nil
		default:
			return "", false, errors.New("lookup " + name)
		}
	}
	got, err := Target([]string{"alt.test", "misc.test.moderated"}, false, lookup)
	if err != nil || got != "misc.test.moderated" {
		t.Fatalf("got %q %v", got, err)
	}
	got, err = Target([]string{"misc.test.moderated"}, true, lookup)
	if err != nil || got != "" {
		t.Fatalf("approved %q %v", got, err)
	}
	got, err = Target([]string{"missing", "alt.test"}, false, lookup)
	if err != nil || got != "" {
		t.Fatalf("unmoderated %q %v", got, err)
	}
	if _, err := Target([]string{"broken"}, false, lookup); err == nil {
		t.Fatal("lookup error swallowed")
	}
}

func TestSubmitMailsArticle(t *testing.T) {
	var gotTo []string
	var gotRaw string
	var gotFrom string
	restore := SetDeliver(func(_ context.Context, s mail.Settings, to []string, raw []byte) error {
		gotTo = to
		gotRaw = string(raw)
		gotFrom = s.From
		return nil
	})
	t.Cleanup(restore)
	wire := []byte("From: poster@example.org\r\nSubject: hello\r\n\r\nbody\r\n")
	addr, err := Submit(context.Background(), mail.Settings{
		Host: "127.0.0.1", Port: 25, From: "news@news.example.org", Security: mail.SecurityPlain,
	}, "misc.test.moderated", nil, wire)
	if err != nil {
		t.Fatal(err)
	}
	if addr != "misc-test-moderated@moderators.isc.org" || len(gotTo) != 1 || gotTo[0] != addr {
		t.Fatalf("addr %q to %v", addr, gotTo)
	}
	if gotFrom != "news@news.example.org" {
		t.Fatalf("envelope from %q", gotFrom)
	}
	if !strings.HasPrefix(gotRaw, "To: "+addr+"\r\n") || !strings.Contains(gotRaw, "From: poster@example.org") || !strings.Contains(gotRaw, "Subject: hello") {
		t.Fatalf("raw %q", gotRaw)
	}
	if strings.Contains(gotRaw, "Auto-Submitted") || strings.Count(gotRaw, "Subject:") != 1 {
		t.Fatalf("wrapped raw %q", gotRaw)
	}
}

func TestSubmitMailFailure(t *testing.T) {
	restore := SetDeliver(func(context.Context, mail.Settings, []string, []byte) error {
		return errors.New("smtp down")
	})
	t.Cleanup(restore)
	_, err := Submit(context.Background(), mail.Settings{
		Host: "127.0.0.1", From: "news@news.example.org",
	}, "misc.test.moderated", nil, []byte("From: a@b.c\r\n\r\n\r\n"))
	if err == nil || !strings.Contains(err.Error(), "smtp down") {
		t.Fatalf("got %v", err)
	}
}

func TestSubmitNeedsMailServer(t *testing.T) {
	_, err := Submit(context.Background(), mail.Settings{}, "misc.test.moderated", nil, []byte("From: a@b.c\r\n\r\n\r\n"))
	if err == nil || !strings.Contains(err.Error(), "mail server is not configured") {
		t.Fatalf("got %v", err)
	}
	_, err = Submit(context.Background(), mail.Settings{Host: "127.0.0.1"}, "misc.test.moderated", nil, []byte("x"))
	if err == nil || !strings.Contains(err.Error(), "from address is required") {
		t.Fatalf("got %v", err)
	}
}
