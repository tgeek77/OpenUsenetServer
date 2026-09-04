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
