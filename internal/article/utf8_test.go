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
