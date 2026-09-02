package inpaths

import (
	"strings"
	"testing"
)

func TestExampleDump(t *testing.T) {
	s := NewStats()
	s.AddPath("news.trigofacile.com!news.ecp.fr!usenet.stanford.edu!not-for-mail")
	s.AddPath("news.trigofacile.com!news.ecp.fr!newsgate.cistron.nl!bleachbot!not-for-mail")

	if s.articles != 2 {
		t.Fatalf("articles %d", s.articles)
	}
	if s.siteCount["news.trigofacile.com"] != 2 {
		t.Fatalf("trigofacile count %d", s.siteCount["news.trigofacile.com"])
	}
	wantRels := map[string]int{
		"usenet.stanford.edu!news.ecp.fr":      1,
		"news.ecp.fr!news.trigofacile.com":     2,
		"bleachbot!newsgate.cistron.nl":        1,
		"newsgate.cistron.nl!news.ecp.fr":      1,
	}
	for k, n := range wantRels {
		if s.rels[k] != n {
			t.Fatalf("rel %q got %d want %d", k, s.rels[k], n)
		}
	}

	var buf strings.Builder
	if err := s.WriteDump(&buf); err != nil {
		t.Fatal(err)
	}
	back, err := ReadDump(strings.NewReader(buf.String()))
	if err != nil {
		t.Fatal(err)
	}
	if back.articles != 2 || back.siteCount["bleachbot"] != 1 {
		t.Fatalf("round trip %+v", back.siteCount)
	}
}

func TestHopOK(t *testing.T) {
	if HopOK("192.168.1.1") {
		t.Fatal("ip only")
	}
	if !HopOK("news.example.com") {
		t.Fatal("hostname")
	}
	if HopOK("not-for-mail") {
		t.Fatal("not-for-mail")
	}
}

func TestLoggerFlush(t *testing.T) {
	dir := t.TempDir()
	lg, err := NewLogger(dir)
	if err != nil {
		t.Fatal(err)
	}
	lg.Record("a!b!not-for-mail")
	if lg.PendingArticles() != 1 {
		t.Fatal("pending")
	}
	path, err := lg.Flush()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(path, "inpaths.") {
		t.Fatal(path)
	}
	if lg.PendingArticles() != 0 {
		t.Fatal("should reset")
	}
	st, err := LoadDumps(dir, 0)
	if err != nil || st.Articles() != 1 {
		t.Fatalf("load %v %d", err, st.Articles())
	}
}
