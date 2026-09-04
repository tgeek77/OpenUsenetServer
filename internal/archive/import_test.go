package archive_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openusenet/internal/archive"
	"openusenet/internal/store"
)

func TestReadMBoxMessages(t *testing.T) {
	const sample = `From a@b Mon Jan 1 00:00:00 2020
From: a@b
Newsgroups: local.test
Subject: one
Message-ID: <one@x>

hello

From c@d Mon Jan 1 00:00:01 2020
From: c@d
Newsgroups: local.test
Subject: two
Message-ID: <two@x>

>From someone earlier
world
`
	var n int
	err := archive.ReadMBoxMessages(strings.NewReader(sample), func(raw []byte) error {
		n++
		s := string(raw)
		if n == 2 && !strings.Contains(s, "From someone earlier") {
			t.Fatalf("mboxrd unescape failed: %q", s)
		}
		if strings.HasPrefix(s, "From ") {
			t.Fatalf("envelope left in body: %q", s[:40])
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("got %d messages", n)
	}
}

func TestImportMBoxMemory(t *testing.T) {
	st := store.NewMemory()
	ctx := context.Background()
	const sample = `From a@b Mon Jan 1 00:00:00 2020
From: Alice <a@b>
Newsgroups: local.test
Subject: hello
Message-ID: <import-1@test>
Date: Mon, 1 Jan 2020 00:00:00 +0000

body one

From a@b Mon Jan 1 00:00:01 2020
From: Alice <a@b>
Newsgroups: local.test
Subject: hello again
Message-ID: <import-1@test>
Date: Mon, 1 Jan 2020 00:00:01 +0000

duplicate id
`
	res, err := archive.Import(ctx, st, strings.NewReader(sample), archive.ImportOpts{
		CreateGroups: true,
		Hostname:     "news.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Scanned != 2 || res.Imported != 1 || res.Duplicates != 1 {
		t.Fatalf("res=%+v", res)
	}
	art, err := st.GetByMsgID(ctx, "<import-1@test>")
	if err != nil || art == nil {
		t.Fatalf("get: %v %+v", err, art)
	}
	if !strings.Contains(art.Body, "body one") {
		t.Fatalf("body=%q", art.Body)
	}
}

func TestGroupFromMBoxPath(t *testing.T) {
	if g := archive.GroupFromMBoxPath("/tmp/alt.fan.usenet.mbox"); g != "alt.fan.usenet" {
		t.Fatal(g)
	}
	if g := archive.GroupFromMBoxPath("news.groups.mbox.gz"); g != "news.groups" {
		t.Fatal(g)
	}
}

func TestImportFileFallbackGroup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "local.test.mbox")
	body := `From a@b Mon Jan 1 00:00:00 2020
From: a@b
Subject: no groups header
Message-ID: <nogroups@test>
Date: Mon, 1 Jan 2020 00:00:00 +0000

x
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	st := store.NewMemory()
	res, err := archive.ImportFile(context.Background(), st, path, archive.ImportOpts{CreateGroups: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 1 {
		t.Fatalf("%+v", res)
	}
	if res.Groups["local.test"] != 1 {
		t.Fatalf("%+v", res.Groups)
	}
}
