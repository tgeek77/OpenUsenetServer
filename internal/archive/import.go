package archive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"openusenet/internal/article"
	"openusenet/internal/binary"
	"openusenet/internal/store"
)

// ImportResult summarizes an mbox import pass.
type ImportResult struct {
	Scanned    int            `json:"scanned"`
	Imported   int            `json:"imported"`
	Duplicates int            `json:"duplicates"`
	Skipped    int            `json:"skipped"`
	Groups     map[string]int `json:"groups"`
}

// ImportOpts controls archive.Import.
type ImportOpts struct {
	// Group, if set, is ensured and used when the article has no Newsgroups
	// header (or to restrict storage to this group only when RestrictGroup).
	Group string
	// RestrictGroup stores only into Group even if Newsgroups lists others.
	RestrictGroup bool
	// CreateGroups ensures missing newsgroups (default true when unset via CLI).
	CreateGroups bool
	// Spool appends imported articles to the live mbox spool when non-nil.
	Spool *MBox
	// Hostname is used for Xref prefix (optional).
	Hostname string
}

// Import reads an mbox stream and stores articles into st.
func Import(ctx context.Context, st store.Store, r io.Reader, opt ImportOpts) (ImportResult, error) {
	res := ImportResult{Groups: map[string]int{}}
	fallback := strings.TrimSpace(opt.Group)

	err := ReadMBoxMessages(r, func(raw []byte) error {
		res.Scanned++
		if err := ctx.Err(); err != nil {
			return err
		}
		art, err := article.Parse(normalizeImportRaw(raw))
		if err != nil {
			res.Skipped++
			return nil
		}
		msgid := strings.TrimSpace(art.Get("Message-ID"))
		if msgid == "" {
			msgid = strings.TrimSpace(art.Get("Message-Id"))
		}
		if msgid == "" || !article.ValidMessageID(msgid) {
			res.Skipped++
			return nil
		}
		groups := art.Newsgroups()
		if opt.RestrictGroup && fallback != "" {
			groups = []string{fallback}
		} else if len(groups) == 0 && fallback != "" {
			groups = []string{fallback}
			art.Set("Newsgroups", fallback)
		}
		if len(groups) == 0 {
			res.Skipped++
			return nil
		}
		if opt.CreateGroups {
			for _, g := range groups {
				if err := st.EnsureGroup(ctx, g, "", "y"); err != nil {
					return fmt.Errorf("ensure group %s: %w", g, err)
				}
			}
		}
		dup, err := st.HasMessageID(ctx, msgid)
		if err != nil {
			return err
		}
		if dup {
			res.Duplicates++
			return nil
		}
		hdr := art.RawHeaders
		body := art.Body
		// Prefer wire split so stored headers match article.Parse conventions.
		if w := art.Wire(); len(w) > 0 {
			if h, b, ok := strings.Cut(string(w), "\r\n\r\n"); ok {
				hdr, body = h, b
			}
		}
		isBin := binary.LooksBinary(art.RawHeaders, art.Body)
		_, err = st.Post(ctx, hdr, body, msgid,
			art.Get("Subject"), art.Get("From"), art.Get("Date"), art.Get("References"),
			opt.Hostname, art.Bytes(), art.Lines(), groups, isBin)
		if errors.Is(err, store.ErrDuplicate) {
			res.Duplicates++
			return nil
		}
		if errors.Is(err, store.ErrNoGroup) {
			res.Skipped++
			return nil
		}
		if err != nil {
			return fmt.Errorf("store %s: %w", msgid, err)
		}
		if err := st.RecordContentStats(ctx, store.ContentStatsEvent{
			Groups: groups, From: art.Get("From"), Path: art.Get("Path"),
			Binary: isBin, ExcludeSite: []string{opt.Hostname},
		}); err != nil {
			return fmt.Errorf("content stats %s: %w", msgid, err)
		}
		res.Imported++
		for _, g := range groups {
			res.Groups[g]++
		}
		if opt.Spool != nil {
			for _, g := range groups {
				if _, _, err := opt.Spool.Append(g, art); err != nil {
					return fmt.Errorf("spool %s: %w", g, err)
				}
			}
		}
		return nil
	})
	return res, err
}

// ImportFile opens path (plain or .gz) and imports it.
func ImportFile(ctx context.Context, st store.Store, path string, opt ImportOpts) (ImportResult, error) {
	if strings.TrimSpace(opt.Group) == "" {
		opt.Group = GroupFromMBoxPath(path)
	}
	r, err := OpenMBox(path)
	if err != nil {
		return ImportResult{}, err
	}
	defer func() { _ = r.Close() }()
	return Import(ctx, st, r, opt)
}

func normalizeImportRaw(raw []byte) []byte {
	// article.Parse expects a header/body separator; tolerate LF-only mbox bodies.
	text := string(raw)
	if !strings.Contains(text, "\r\n") {
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}
	// Historical mboxes often contain Latin-1 / Windows-1252 bytes.
	return []byte(article.SanitizeUTF8(text))
}
