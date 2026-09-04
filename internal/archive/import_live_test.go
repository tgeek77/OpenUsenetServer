package archive_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"openusenet/internal/archive"
	"openusenet/internal/store"
)

// Exercises real mbox dumps when present (local laptop samples).
func TestImportLiveTempMBox(t *testing.T) {
	dir := filepath.Join(os.Getenv("HOME"), "temp")
	files := []string{
		"news.groups.mbox",
		"alt.fan.usenet.mbox",
		"alt.usenet.mbox",
		"news.software.misc.mbox",
	}
	st := store.NewMemory()
	ctx := context.Background()
	start := time.Now()
	var scanned, imported, skipped int
	any := false
	for _, name := range files {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		any = true
		res, err := archive.ImportFile(ctx, st, path, archive.ImportOpts{
			CreateGroups: true,
			Hostname:     "import.test",
		})
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		t.Logf("%s: scanned=%d imported=%d duplicates=%d skipped=%d groups=%v",
			name, res.Scanned, res.Imported, res.Duplicates, res.Skipped, res.Groups)
		scanned += res.Scanned
		imported += res.Imported
		skipped += res.Skipped
	}
	if !any {
		t.Skip("~/temp/*.mbox not present")
	}
	if scanned == 0 {
		t.Fatal("expected to scan some messages from non-empty mboxes")
	}
	if imported == 0 {
		t.Fatalf("imported=0 after scanning %d (skipped=%d)", scanned, skipped)
	}
	t.Logf("total scanned=%d imported=%d skipped=%d elapsed=%s",
		scanned, imported, skipped, time.Since(start).Round(time.Millisecond))
}
