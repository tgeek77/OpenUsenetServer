package isc

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseActiveAndNewsgroups(t *testing.T) {
	active := "comp.lang.go 0000000010 0000000001 y\n" +
		"news.announce.newgroups 0000000000 0000000001 m\n" +
		"junk.alias 0000000000 0000000001 =comp.lang.go\n" +
		"# comment\n"
	st, err := ParseActive(strings.NewReader(active))
	if err != nil {
		t.Fatal(err)
	}
	if st["comp.lang.go"] != "y" || st["news.announce.newgroups"] != "m" {
		t.Fatalf("%v", st)
	}
	if _, ok := st["junk.alias"]; ok {
		t.Fatal("aliases should be skipped")
	}

	ng := "comp.lang.go\tThe Go programming language.\n" +
		"news.announce.newgroups\t\tAnnouncements (Moderated)\n"
	ds, err := ParseNewsgroups(strings.NewReader(ng))
	if err != nil {
		t.Fatal(err)
	}
	if ds["comp.lang.go"] != "The Go programming language." {
		t.Fatalf("desc %q", ds["comp.lang.go"])
	}
}

func TestSanitizeLatin1(t *testing.T) {
	// 0xf2 is ô in latin1 and invalid as UTF-8 lead in this sequence.
	raw := "foo.bar\tCaf" + string([]byte{0xe9}) + " discussion\n"
	ds, err := ParseNewsgroups(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ds["foo.bar"], "Caf") {
		t.Fatalf("desc %q", ds["foo.bar"])
	}
}

func TestFetchGzip(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/active.gz", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("missing User-Agent")
		}
		_, _ = w.Write(gz(t, "local.test 0000000000 0000000001 y\ncomp.lang.go 0000000000 0000000001 y\n"))
	})
	mux.HandleFunc("/newsgroups.gz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(gz(t, "local.test\tLocal test group\ncomp.lang.go\tGo language\n"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	groups, err := Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("got %d groups", len(groups))
	}
	byName := map[string]string{}
	for _, g := range groups {
		byName[g.Name] = g.Description
	}
	if byName["comp.lang.go"] != "Go language" {
		t.Fatalf("%v", byName)
	}
}

func gz(t *testing.T, s string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := io.WriteString(w, s); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
