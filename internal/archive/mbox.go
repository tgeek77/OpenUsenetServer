package archive

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"openusenet/internal/article"
)

// MBox writes mboxrd files, one per newsgroup.
type MBox struct {
	root string
	mu   sync.Mutex
}

func New(root string) *MBox {
	return &MBox{root: root}
}

func (m *MBox) pathFor(group string) string {
	safe := strings.ReplaceAll(group, "/", ".")
	parts := strings.Split(safe, ".")
	dir := filepath.Join(append([]string{m.root}, parts[:len(parts)-1]...)...)
	name := parts[len(parts)-1] + ".mbox"
	if len(parts) == 1 {
		dir = m.root
		name = safe + ".mbox"
	}
	return filepath.Join(dir, name)
}

// Append writes the article in mboxrd form. Returns offset and length.
func (m *MBox) Append(group string, a *article.Article) (offset, length int64, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	path := m.pathFor(group)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return 0, 0, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return 0, 0, err
	}
	offset = st.Size()
	from := envelope(a)
	var b strings.Builder
	b.WriteString(from)
	b.WriteString("\n")
	wire := string(a.Wire())
	wire = strings.ReplaceAll(wire, "\r\n", "\n")
	for _, line := range strings.Split(wire, "\n") {
		if strings.HasPrefix(line, "From ") {
			b.WriteByte('>')
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if !strings.HasSuffix(b.String(), "\n\n") {
		b.WriteByte('\n')
	}
	n, err := f.WriteString(b.String())
	if err != nil {
		return 0, 0, err
	}
	return offset, int64(n), nil
}

// Scrub deletes mbox files for dropped groups and rewrites other group files
// so a removed article's body is not left on disk.
func (m *MBox) Scrub(dropGroups, rewriteGroups, messageIDs []string) error {
	if m == nil || m.root == "" {
		return nil
	}
	if len(dropGroups) == 0 && len(messageIDs) == 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, g := range dropGroups {
		if err := os.Remove(m.pathFor(g)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if len(messageIDs) == 0 {
		return nil
	}
	drop := make(map[string]struct{}, len(messageIDs))
	for _, id := range messageIDs {
		drop[id] = struct{}{}
	}
	for _, g := range rewriteGroups {
		if err := rewriteMbox(m.pathFor(g), drop); err != nil {
			return err
		}
	}
	return nil
}

func rewriteMbox(path string, drop map[string]struct{}) error {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var cur strings.Builder
	var kept []string
	flush := func() {
		msg := cur.String()
		cur.Reset()
		if msg == "" || messageDropped(msg, drop) {
			return
		}
		kept = append(kept, msg)
	}
	for _, line := range strings.SplitAfter(string(b), "\n") {
		if strings.HasPrefix(line, "From ") && cur.Len() > 0 {
			flush()
		}
		cur.WriteString(line)
	}
	flush()
	var out strings.Builder
	for _, msg := range kept {
		out.WriteString(msg)
		if !strings.HasSuffix(msg, "\n") {
			out.WriteByte('\n')
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(out.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func messageDropped(msg string, drop map[string]struct{}) bool {
	for _, line := range strings.Split(msg, "\n") {
		lower := strings.ToLower(line)
		if !strings.HasPrefix(lower, "message-id:") {
			continue
		}
		id := strings.TrimSpace(line[len("message-id:"):])
		if _, ok := drop[id]; ok {
			return true
		}
	}
	return false
}

func envelope(a *article.Article) string {
	from := a.Get("From")
	addr := "MAILER-DAEMON"
	if i := strings.Index(from, "<"); i >= 0 {
		if j := strings.Index(from[i:], ">"); j > 0 {
			addr = from[i+1 : i+j]
		}
	} else if from != "" {
		addr = strings.Fields(from)[0]
	}
	addr = strings.ReplaceAll(addr, " ", "")
	if addr == "" {
		addr = "MAILER-DAEMON"
	}
	return fmt.Sprintf("From %s %s", addr, time.Now().UTC().Format(time.ANSIC))
}
