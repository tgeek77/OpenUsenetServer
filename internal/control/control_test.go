package control_test

import (
	"context"
	"log"
	"strings"
	"testing"

	"openusenet/internal/article"
	"openusenet/internal/control"
	"openusenet/internal/store"
)

func TestParseNewgroup(t *testing.T) {
	raw := "" +
		"From: group-admin@isc.org\r\n" +
		"Newsgroups: control.newgroup\r\n" +
		"Subject: cmsg newgroup example.test\r\n" +
		"Control: newgroup example.test moderated\r\n" +
		"Message-ID: <ng1@isc.org>\r\n" +
		"Date: Mon, 01 Jan 2024 00:00:00 +0000\r\n" +
		"\r\n" +
		"For your newsgroups file:\r\n" +
		"example.test\tExample test group (Moderated)\r\n"
	art, err := article.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	msg, ok := control.Parse(art)
	if !ok || msg.Kind != control.KindNewgroup || msg.Group != "example.test" || !msg.Moderated {
		t.Fatalf("%#v", msg)
	}
	if !strings.Contains(msg.Description, "Example test") {
		t.Fatalf("desc %q", msg.Description)
	}
}

func TestParseRmgroupAndCancelIgnored(t *testing.T) {
	art, _ := article.Parse([]byte("Control: rmgroup example.test\r\nFrom: a@b\r\nNewsgroups: control.rmgroup\r\nSubject: x\r\nMessage-ID: <r@x>\r\nDate: Mon, 01 Jan 2024 00:00:00 +0000\r\n\r\n"))
	msg, ok := control.Parse(art)
	if !ok || msg.Kind != control.KindRmgroup {
		t.Fatal(msg)
	}
	art2, _ := article.Parse([]byte("Control: cancel <old@x>\r\nFrom: a@b\r\nNewsgroups: control.cancel\r\nSubject: x\r\nMessage-ID: <c@x>\r\nDate: Mon, 01 Jan 2024 00:00:00 +0000\r\n\r\n"))
	if _, ok := control.Parse(art2); ok {
		t.Fatal("cancel should not parse as hierarchy control")
	}
}

func TestApplyNewgroupOverridesISC(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	if err := st.EnsureGroups(ctx, []store.Group{{Name: "example.test", Description: "isc", Status: "y"}}); err != nil {
		t.Fatal(err)
	}
	g, _ := st.GetGroup(ctx, "example.test")
	if g.Origin != store.OriginISC {
		t.Fatalf("origin %q", g.Origin)
	}
	msg := control.Message{Kind: control.KindNewgroup, Group: "example.test", Moderated: true, Description: "from control"}
	res, err := control.Apply(ctx, st, msg, log.Default())
	if err != nil || !res.Applied {
		t.Fatalf("%v %#v", err, res)
	}
	g, _ = st.GetGroup(ctx, "example.test")
	if g.Status != "m" || g.Origin != store.OriginControl || g.Description != "from control" {
		t.Fatalf("%#v", g)
	}
	// ISC refresh must not clobber.
	if err := st.EnsureGroups(ctx, []store.Group{{Name: "example.test", Description: "isc again", Status: "y"}}); err != nil {
		t.Fatal(err)
	}
	g, _ = st.GetGroup(ctx, "example.test")
	if g.Status != "m" || g.Origin != store.OriginControl || g.Description != "from control" {
		t.Fatalf("ISC clobbered control group: %#v", g)
	}
}

func TestApplyRmgroupAndCheckgroups(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	_ = st.EnsureGroups(ctx, []store.Group{
		{Name: "foo.one", Status: "y"},
		{Name: "foo.two", Status: "y"},
		{Name: "bar.one", Status: "y"},
	})
	_, err := control.Apply(ctx, st, control.Message{Kind: control.KindRmgroup, Group: "foo.one"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	g, _ := st.GetGroup(ctx, "foo.one")
	if g.Status != "n" || g.Origin != store.OriginControl {
		t.Fatalf("%#v", g)
	}

	body := "foo.two\tTwo\nfoo.three\tThree (Moderated)\n"
	art, _ := article.Parse([]byte(
		"Control: checkgroups foo.*\r\nFrom: admin@foo\r\nNewsgroups: control.checkgroups\r\n" +
			"Subject: cmsg checkgroups\r\nMessage-ID: <cg@x>\r\nDate: Mon, 01 Jan 2024 00:00:00 +0000\r\n\r\n" + body))
	msg, ok := control.Parse(art)
	if !ok {
		t.Fatal("parse")
	}
	res, err := control.Apply(ctx, st, msg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Created < 1 {
		t.Fatalf("expected create %#v", res)
	}
	three, _ := st.GetGroup(ctx, "foo.three")
	if three == nil || three.Status != "m" {
		t.Fatalf("%#v", three)
	}
	// foo.one already n; foo.two updated; nothing else in foo.* should stay y if missing — foo.one stays n
	two, _ := st.GetGroup(ctx, "foo.two")
	if two.Origin != store.OriginControl {
		t.Fatalf("%#v", two)
	}
	bar, _ := st.GetGroup(ctx, "bar.one")
	if bar.Status != "y" {
		t.Fatal("bar should be untouched")
	}
}

func TestSeedBuiltinGroups(t *testing.T) {
	st := store.NewMemory()
	if err := control.SeedBuiltinGroups(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"control", "control.newgroup", "control.rmgroup", "control.checkgroups", "control.cancel"} {
		g, err := st.GetGroup(context.Background(), name)
		if err != nil || g == nil {
			t.Fatalf("missing %s", name)
		}
	}
}
