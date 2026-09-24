package binary_test

import (
	"os"
	"strings"
	"testing"

	"openusenet/internal/archive"
	"openusenet/internal/article"
	"openusenet/internal/binary"
)

// Optional local regression against the debian.bugs.rc sample the operator provided.
func TestDebianBugsRCMostlyText(t *testing.T) {
	path := "/home/jsevans/linux.debian.bugs.rc.mbox.gz"
	if _, err := os.Stat(path); err != nil {
		t.Skip("sample mbox not present")
	}
	r, err := archive.OpenMBox(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	total, binN := 0, 0
	err = archive.ReadMBoxMessages(r, func(raw []byte) error {
		total++
		text := string(raw)
		if !strings.Contains(text, "\r\n") {
			text = strings.ReplaceAll(text, "\n", "\r\n")
		}
		art, err := article.Parse([]byte(text))
		if err != nil {
			return nil
		}
		if binary.LooksBinary(art.RawHeaders, art.Body) {
			binN++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ratio := float64(binN) / float64(total)
	t.Logf("total=%d binary=%d ratio=%.3f", total, binN, ratio)
	// Before fix this was ~0.31; allow a little room for any real binaries.
	if ratio > 0.05 {
		t.Fatalf("binary ratio too high: %.3f (%d/%d)", ratio, binN, total)
	}
}
