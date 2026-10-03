package posting

import (
	"context"
	"errors"
	"log"
	"strings"
	"testing"

	"openusenet/internal/article"
	"openusenet/internal/config"
	"openusenet/internal/mail"
	"openusenet/internal/moderate"
	"openusenet/internal/store"
)

type offerLog struct{ n int }

func (o *offerLog) Offer(string, string, []string, []byte) { o.n++ }

func moderatedFixture(t *testing.T) (*store.Memory, config.Config) {
	t.Helper()
	st := store.NewMemory()
	cfg := config.Defaults()
	cfg.Server.Hostname = "news.test"
	cfg.Server.Pathhost = "news.test"
	cfg.Limits.ArtCutoffDays = 0
	ctx := context.Background()
	if err := st.EnsureGroup(ctx, "local.test", "", "y"); err != nil {
		t.Fatal(err)
	}
	if err := st.EnsureGroup(ctx, "misc.test.moderated", "", "m"); err != nil {
		t.Fatal(err)
	}
	return st, cfg
}

func TestAcceptMailsModeratedAndSkipsStore(t *testing.T) {
	st, cfg := moderatedFixture(t)
	ctx := context.Background()
	in := Input{From: "Poster <poster@example.org>", Newsgroups: "local.test,misc.test.moderated", Subject: "please", Body: "hello\n"}
	offers := &offerLog{}
	_, err := Accept(ctx, cfg, st, nil, offers, nil, in, 0)
	if err == nil || !strings.Contains(err.Error(), "mail server is not configured") {
		t.Fatalf("unconfigured %v", err)
	}
	if n, _ := st.CountArticles(ctx); n != 0 || offers.n != 0 {
		t.Fatalf("stored or offered before mail was configured: articles=%d offers=%d", n, offers.n)
	}

	if err := st.SaveMailSettings(ctx, mail.Settings{
		Host: "127.0.0.1", Port: 25, From: "news@news.test", Security: mail.SecurityPlain,
	}); err != nil {
		t.Fatal(err)
	}
	var addr string
	restore := moderate.SetDeliver(func(_ context.Context, s mail.Settings, to []string, raw []byte) error {
		if s.From != "news@news.test" || len(to) != 1 {
			return errors.New("bad envelope")
		}
		addr = to[0]
		if !strings.Contains(string(raw), "From: Poster <poster@example.org>") || strings.Contains(string(raw), "Auto-Submitted") {
			return errors.New("bad article")
		}
		return nil
	})
	t.Cleanup(restore)

	out, err := Accept(ctx, cfg, st, nil, offers, nil, in, 0)
	if err != nil {
		t.Fatal(err)
	}
	if out.Stored != nil || out.Mailed == nil {
		t.Fatalf("outcome %+v", out)
	}
	if out.Mailed.Group != "misc.test.moderated" || out.Mailed.Address != "misc-test-moderated@moderators.isc.org" || addr != out.Mailed.Address {
		t.Fatalf("mailed %+v to %q", out.Mailed, addr)
	}
	if out.Mailed.MessageID == "" {
		t.Fatal("missing message id")
	}
	if n, _ := st.CountArticles(ctx); n != 0 || offers.n != 0 {
		t.Fatalf("mailed post was stored or offered: articles=%d offers=%d", n, offers.n)
	}

	restoreFail := moderate.SetDeliver(func(context.Context, mail.Settings, []string, []byte) error {
		return errors.New("smtp down")
	})
	_, err = Accept(ctx, cfg, st, nil, offers, nil, in, 0)
	restoreFail()
	if err == nil || !strings.Contains(err.Error(), "smtp down") {
		t.Fatalf("mail failure %v", err)
	}
	if n, _ := st.CountArticles(ctx); n != 0 || offers.n != 0 {
		t.Fatal("failed mail was stored")
	}

	if err := st.AddModeratorRule(ctx, "local.*", "desk@example.org"); err != nil {
		t.Fatal(err)
	}
	if err := st.EnsureGroup(ctx, "local.announce", "", "m"); err != nil {
		t.Fatal(err)
	}
	restore = moderate.SetDeliver(func(_ context.Context, _ mail.Settings, to []string, _ []byte) error {
		addr = to[0]
		return nil
	})
	out, err = Accept(ctx, cfg, st, nil, offers, nil, Input{
		From: "poster@example.org", Newsgroups: "local.announce", Subject: "local", Body: "x\n",
	}, 0)
	restore()
	if err != nil {
		t.Fatal(err)
	}
	if out.Mailed == nil || out.Mailed.Address != "desk@example.org" || addr != "desk@example.org" {
		t.Fatalf("local rule %+v addr %q", out.Mailed, addr)
	}

	art, err := article.Parse([]byte("From: mod@example.org\r\nNewsgroups: misc.test.moderated\r\nSubject: approved\r\nApproved: mod@example.org\r\n\r\nkept\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	mailed, err := mailModerated(ctx, cfg, st, log.Default(), art)
	if err != nil || mailed != nil {
		t.Fatalf("approved should be stored by the caller, mailed=%v err=%v", mailed, err)
	}
}

func TestAcceptStoresUnmoderated(t *testing.T) {
	st, cfg := moderatedFixture(t)
	offers := &offerLog{}
	out, err := Accept(context.Background(), cfg, st, nil, offers, nil, Input{
		From: "poster@example.org", Newsgroups: "local.test", Subject: "plain", Body: "body\n",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if out.Mailed != nil || out.Stored == nil || offers.n != 1 {
		t.Fatalf("stored %+v offers %d", out, offers.n)
	}
	if out.Stored.Numbers["local.test"] == 0 {
		t.Fatalf("numbers %+v", out.Stored.Numbers)
	}
}
