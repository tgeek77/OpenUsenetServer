package article_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"openusenet/internal/article"
)

func TestSanitizeUTF8Latin1(t *testing.T) {
	raw := "smart" + string([]byte{0x91}) + "quote"
	out := article.SanitizeUTF8(raw)
	if !utf8.ValidString(out) {
		t.Fatalf("not utf8: %q", out)
	}
	if !strings.Contains(out, "smart") || !strings.Contains(out, "quote") {
		t.Fatalf("%q", out)
	}
}

func TestDecodeWordsSubject(t *testing.T) {
	in := "=?UTF-8?Q?Russians_are_advised_to_pray_to_Jesus_before_the_start_of_the?= =?UTF-8?Q?_work_week_=E2=80=93_this_will_help_to_better_cope_with_tasks?= =?UTF-8?Q?_and_negativity_from_superiors_and_colleagues.?="
	want := "Russians are advised to pray to Jesus before the start of the work week – this will help to better cope with tasks and negativity from superiors and colleagues."
	if got := article.DecodeWords(in); got != want {
		t.Fatalf("got %q", got)
	}
	plain := "Hello world"
	if got := article.DecodeWords(plain); got != plain {
		t.Fatalf("plain %q", got)
	}
}

func TestDecodeHeaderBlockKeepsDate(t *testing.T) {
	head := "Subject: =?UTF-8?Q?Hello_there.?=\r\n" +
		"From: =?UTF-8?Q?Ada_Lovelace?= <ada@example.com>\r\n" +
		"Date: Tue, 13 Oct 2026 12:00:00 +0000\r\n"
	got := article.DecodeHeaderBlock(head)
	if !strings.Contains(got, "Subject: Hello there.") {
		t.Fatalf("subject: %q", got)
	}
	if !strings.Contains(got, "From: Ada Lovelace <ada@example.com>") {
		t.Fatalf("from: %q", got)
	}
	if !strings.Contains(got, "Date: Tue, 13 Oct 2026 12:00:00 +0000") {
		t.Fatalf("date: %q", got)
	}
}

func TestSanitizeUTF8StripsNUL(t *testing.T) {
	in := "ok\x00still"
	out := article.SanitizeUTF8(in)
	if strings.ContainsRune(out, 0) || out != "okstill" {
		t.Fatalf("%q", out)
	}
}
