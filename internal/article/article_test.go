package article

import (
	"testing"
	"time"
)

func TestPathContains(t *testing.T) {
	path := "news-b!news-a!not-for-mail"
	if !PathContains(path, "news-a") || !PathContains(path, "news-b") {
		t.Fatal("expected hops present")
	}
	if PathContains(path, "news-c") {
		t.Fatal("news-c should not match")
	}
	if PathContains("", "news-a") || PathContains(path, "") {
		t.Fatal("empty should not match")
	}
}

func TestInjectForIHave(t *testing.T) {
	a, err := Parse([]byte(
		"Path: news-a!not-for-mail\r\n" +
			"From: a@b.c\r\n" +
			"Newsgroups: local.test\r\n" +
			"Subject: hi\r\n" +
			"Date: Mon, 01 Jan 2024 00:00:00 +0000\r\n" +
			"Message-ID: <1@news-a>\r\n" +
			"Xref: news-a local.test:1\r\n" +
			"\r\nbody\r\n",
	))
	if err != nil {
		t.Fatal(err)
	}
	if err := InjectForIHave(a, InjectOpts{Pathhost: "news-b", Hostname: "news-b"}); err != nil {
		t.Fatal(err)
	}
	if a.Get("Path") != "news-b!news-a!not-for-mail" {
		t.Fatalf("path %q", a.Get("Path"))
	}
	if a.Get("Message-ID") != "<1@news-a>" {
		t.Fatalf("msgid rewritten: %q", a.Get("Message-ID"))
	}
	if a.Has("Xref") {
		t.Fatal("xref should be stripped")
	}
	if err := InjectForIHave(a, InjectOpts{Pathhost: "news-a", Hostname: "news-a"}); err == nil {
		t.Fatal("expected path loop")
	}
}

func TestInjectForPostInjectionDate(t *testing.T) {
	a, err := Parse([]byte("From: a@b.c\r\nNewsgroups: misc.test\r\nSubject: hi\r\n\r\nbody\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 3, 14, 36, 19, 0, time.UTC)
	if err := InjectForPost(a, InjectOpts{Hostname: "news.example", Pathhost: "news.example", Now: now}); err != nil {
		t.Fatal(err)
	}
	got := a.Get("Injection-Date")
	want := "Sat, 03 Oct 2026 14:36:19 +0000"
	if got != want {
		t.Fatalf("Injection-Date %q, want %q", got, want)
	}
}

func TestInjectForIHaveRequiresMessageID(t *testing.T) {
	a, err := Parse([]byte("From: a@b.c\r\nNewsgroups: local.test\r\nSubject: x\r\nDate: Mon, 01 Jan 2024 00:00:00 +0000\r\n\r\nb\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := InjectForIHave(a, InjectOpts{Pathhost: "h", Hostname: "h"}); err == nil {
		t.Fatal("expected missing Message-ID")
	}
}
