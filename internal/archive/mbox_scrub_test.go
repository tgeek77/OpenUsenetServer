package archive

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openusenet/internal/article"
)

func TestScrubDropsGroupFileAndCrosspostCopy(t *testing.T) {
	dir := t.TempDir()
	mb := New(dir)
	art, err := article.Parse([]byte("From: a@b\r\nNewsgroups: alt.binaries.foo,misc.test\r\nSubject: x\r\nMessage-ID: <gone@news>\r\n\r\npayload\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	keep, err := article.Parse([]byte("From: a@b\r\nNewsgroups: misc.test\r\nSubject: y\r\nMessage-ID: <stay@news>\r\n\r\nhello\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := mb.Append("alt.binaries.foo", art); err != nil {
		t.Fatal(err)
	}
	if _, _, err := mb.Append("misc.test", art); err != nil {
		t.Fatal(err)
	}
	if _, _, err := mb.Append("misc.test", keep); err != nil {
		t.Fatal(err)
	}
	if err := mb.Scrub([]string{"alt.binaries.foo"}, []string{"misc.test"}, []string{"<gone@news>"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "alt", "binaries", "foo.mbox")); !os.IsNotExist(err) {
		t.Fatalf("banned mbox still present: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "misc", "test.mbox"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if strings.Contains(text, "payload") || strings.Contains(text, "<gone@news>") {
		t.Fatalf("crosspost body still in mbox:\n%s", text)
	}
	if !strings.Contains(text, "<stay@news>") || !strings.Contains(text, "hello") {
		t.Fatalf("kept article missing:\n%s", text)
	}
}
