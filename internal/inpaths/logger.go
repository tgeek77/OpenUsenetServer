package inpaths

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const dumpPrefix = "inpaths."

// Logger records Path headers and writes ninpaths dump files.
type Logger struct {
	dir string
	mu  sync.Mutex
}

func NewLogger(dir string) (*Logger, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, fmt.Errorf("inpaths dir required")
	}
	pathDir := filepath.Join(dir, "path")
	if err := os.MkdirAll(pathDir, 0o755); err != nil {
		return nil, err
	}
	return &Logger{dir: dir}, nil
}

func (l *Logger) PathDir() string {
	return filepath.Join(l.dir, "path")
}

func (l *Logger) pendingFile() string {
	return filepath.Join(l.PathDir(), "pending")
}

// Record stores one Path header value.
func (l *Logger) Record(path string) {
	if l == nil {
		return
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.OpenFile(l.pendingFile(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintln(f, path)
	_ = f.Close()
}

// PendingArticles returns paths not yet flushed.
func (l *Logger) PendingArticles() int {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	f, err := os.Open(l.pendingFile())
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) != "" {
			n++
		}
	}
	return n
}

func (l *Logger) loadPendingLocked() *Stats {
	st := NewStats()
	f, err := os.Open(l.pendingFile())
	if err != nil {
		return st
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			st.AddPath(line)
		}
	}
	return st
}

// Snapshot returns pending (unflushed) stats.
func (l *Logger) Snapshot() *Stats {
	if l == nil {
		return NewStats()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.loadPendingLocked()
}

// Flush writes inpaths.%d and clears the pending file.
func (l *Logger) Flush() (string, error) {
	if l == nil {
		return "", fmt.Errorf("inpaths logger disabled")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	st := l.loadPendingLocked()
	if st.articles == 0 {
		return "", fmt.Errorf("no paths to flush")
	}
	name := fmt.Sprintf("%s%d", dumpPrefix, time.Now().Unix())
	path := filepath.Join(l.PathDir(), name)
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	if err := st.WriteDump(f); err != nil {
		f.Close()
		os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	_ = os.Remove(l.pendingFile())
	return path, nil
}

// ListDumps returns inpaths.* files in path/, newest first.
func ListDumps(dir string) ([]string, error) {
	pathDir := filepath.Join(dir, "path")
	ents, err := os.ReadDir(pathDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var files []string
	for _, e := range ents {
		if e.IsDir() || !strings.HasPrefix(e.Name(), dumpPrefix) {
			continue
		}
		files = append(files, filepath.Join(pathDir, e.Name()))
	}
	sort.Slice(files, func(i, j int) bool { return files[i] > files[j] })
	return files, nil
}

// LoadDumps merges dump files not older than maxAge (zero = all).
func LoadDumps(dir string, maxAge time.Duration) (*Stats, error) {
	files, err := ListDumps(dir)
	if err != nil {
		return nil, err
	}
	cutoff := time.Now().Add(-maxAge)
	out := NewStats()
	out.articles = 0
	for _, path := range files {
		if maxAge > 0 {
			info, err := os.Stat(path)
			if err != nil {
				continue
			}
			if info.ModTime().Before(cutoff) {
				continue
			}
		}
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		st, err := ReadDump(f)
		f.Close()
		if err != nil {
			continue
		}
		out.Merge(st)
	}
	return out, nil
}

// PruneDumps removes dump files older than keep.
func PruneDumps(dir string, keep time.Duration) (int, error) {
	if keep <= 0 {
		return 0, nil
	}
	files, err := ListDumps(dir)
	if err != nil {
		return 0, err
	}
	cutoff := time.Now().Add(-keep)
	n := 0
	for _, path := range files {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			if err := os.Remove(path); err == nil {
				n++
			}
		}
	}
	return n, nil
}
