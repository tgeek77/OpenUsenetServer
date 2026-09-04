package archive_test

import (
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openusenet/internal/archive"
	"openusenet/internal/store"
)

func TestExportGzip(t *testing.T) {
	st := store.NewMemory()
	ctx := context.Background()
	if err := st.EnsureGroup(ctx, "local.test", "t", "y"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Post(ctx, "Subject: hi\r\nFrom: a@b.c\r\nNewsgroups: local.test\r\nMessage-ID: <e@x>\r\n",
		"body\r\n", "<e@x>", "hi", "a@b.c", time.Now().Format(time.RFC1123Z), "", "news", 10, 1, []string{"local.test"}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	res, err := archive.Export(ctx, st, dir, "local.test")
	if err != nil {
		t.Fatal(err)
	}
	if res.Groups != 1 || res.Articles != 1 || len(res.Files) != 1 {
		t.Fatalf("%+v", res)
	}
	f, err := os.Open(res.Files[0])
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	b, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "Subject: hi") || !strings.Contains(string(b), "body") {
		t.Fatalf("%s", b)
	}
	if filepath.Ext(res.Files[0]) != ".gz" {
		t.Fatal(res.Files[0])
	}
}
