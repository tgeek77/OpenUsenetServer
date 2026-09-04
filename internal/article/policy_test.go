package article

import (
	"testing"
	"time"
)

func TestTooOld(t *testing.T) {
	now, err := time.Parse(time.RFC1123Z, "Mon, 01 Jan 2024 12:00:00 +0000")
	if err != nil {
		t.Fatal(err)
	}
	old := "Mon, 01 Dec 2023 12:00:00 +0000"
	if !TooOld(old, 10, now) {
		t.Fatal("expected too old")
	}
	if TooOld(old, 0, now) {
		t.Fatal("cutoff 0 disables")
	}
	recent := "Mon, 01 Jan 2024 00:00:00 +0000"
	if TooOld(recent, 10, now) {
		t.Fatal("recent should pass")
	}
}

func TestCancelTarget(t *testing.T) {
	a, err := Parse([]byte("From: a@b\r\nNewsgroups: local.test\r\nSubject: x\r\nMessage-ID: <c@x>\r\nDate: Mon, 01 Jan 2024 12:00:00 +0000\r\nControl: cancel <old@x>\r\n\r\nbody\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := CancelTarget(a); got != "<old@x>" {
		t.Fatalf("got %q", got)
	}
	if SupersedesTarget(a) != "" {
		t.Fatal("no supersedes")
	}
}
