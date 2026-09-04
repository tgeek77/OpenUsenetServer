package archive

import (
	"compress/gzip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"openusenet/internal/store"
	"openusenet/internal/wildmat"
)

// ExportResult is the outcome of one snapshot export.
type ExportResult struct {
	Dir      string
	Files    []string
	Groups   int
	Articles int
}

// Export writes per-group mboxrd files compressed with gzip under destRoot/timestamp/.
// selector is "all" or a wildmat. Live articles are never deleted.
func Export(ctx context.Context, st store.Store, destRoot, selector string) (*ExportResult, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		selector = "all"
	}
	groups, err := st.ListGroups(ctx, "")
	if err != nil {
		return nil, err
	}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	dir := filepath.Join(destRoot, stamp)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	res := &ExportResult{Dir: dir}
	for _, g := range groups {
		if selector != "all" && !wildmat.Match(selector, g.Name) {
			continue
		}
		arts, err := st.ArticlesForGroup(ctx, g.Name)
		if err != nil {
			return nil, err
		}
		path, n, err := writeGroupGzip(dir, g.Name, arts)
		if err != nil {
			return nil, err
		}
		res.Files = append(res.Files, path)
		res.Groups++
		res.Articles += n
	}
	return res, nil
}

func writeGroupGzip(dir, group string, arts []store.StoredArticle) (string, int, error) {
	name := strings.ReplaceAll(group, "/", ".") + ".mbox.gz"
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	zw := gzip.NewWriter(f)
	defer zw.Close()
	for _, a := range arts {
		from := "From MAILER-DAEMON " + a.StoredAt.UTC().Format(time.ANSIC) + "\n"
		if i := strings.Index(a.From, "<"); i >= 0 {
			if j := strings.Index(a.From[i:], ">"); j >= 0 {
				from = "From " + strings.TrimSpace(a.From[i+1:i+j]) + " " + a.StoredAt.UTC().Format(time.ANSIC) + "\n"
			}
		}
		if _, err := zw.Write([]byte(from)); err != nil {
			return "", 0, err
		}
		hdr := strings.ReplaceAll(a.Headers, "\r\n", "\n")
		body := strings.ReplaceAll(a.Body, "\r\n", "\n")
		wire := hdr + "\n\n" + body
		if !strings.HasSuffix(wire, "\n") {
			wire += "\n"
		}
		for _, line := range strings.Split(wire, "\n") {
			if strings.HasPrefix(line, "From ") {
				line = ">" + line
			}
			if _, err := zw.Write([]byte(line + "\n")); err != nil {
				return "", 0, err
			}
		}
		if _, err := zw.Write([]byte("\n")); err != nil {
			return "", 0, err
		}
	}
	if err := zw.Close(); err != nil {
		return "", 0, err
	}
	return path, len(arts), nil
}

// PruneOldExports keeps the newest retain generation directories under destRoot.
func PruneOldExports(destRoot string, retain int) error {
	if retain <= 0 {
		return nil
	}
	entries, err := os.ReadDir(destRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	if len(dirs) <= retain {
		return nil
	}
	// names are UTC stamps; lexical sort works
	for i := 0; i < len(dirs); i++ {
		for j := i + 1; j < len(dirs); j++ {
			if dirs[j] < dirs[i] {
				dirs[i], dirs[j] = dirs[j], dirs[i]
			}
		}
	}
	drop := dirs[:len(dirs)-retain]
	for _, d := range drop {
		if err := os.RemoveAll(filepath.Join(destRoot, d)); err != nil {
			return fmt.Errorf("prune %s: %w", d, err)
		}
	}
	return nil
}
