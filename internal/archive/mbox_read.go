package archive

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"strings"
)

// ReadMBoxMessages walks an mbox or mboxrd file and yields each message body
// (headers+body, no Unix From_ envelope). mboxrd ">From " escaping is undone.
func ReadMBoxMessages(r io.Reader, fn func(raw []byte) error) error {
	br := bufio.NewReaderSize(r, 256*1024)
	var cur bytes.Buffer
	have := false
	flush := func() error {
		if !have {
			return nil
		}
		raw := bytes.TrimRight(cur.Bytes(), "\n")
		cur.Reset()
		have = false
		if len(bytes.TrimSpace(raw)) == 0 {
			return nil
		}
		return fn(append([]byte(nil), raw...))
	}
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			// Strip CR from CRLF.
			if n := len(line); n > 0 && line[n-1] == '\n' {
				if n > 1 && line[n-2] == '\r' {
					line = append(line[:n-2], '\n')
				}
			}
			if isFromSeparator(line) {
				if err := flush(); err != nil {
					return err
				}
				have = true
				continue
			}
			if !have {
				// Leading junk before first From_ — ignore.
				continue
			}
			if bytes.HasPrefix(line, []byte(">From ")) {
				line = line[1:] // mboxrd unescape
			}
			cur.Write(line)
		}
		if err == io.EOF {
			return flush()
		}
		if err != nil {
			return err
		}
	}
}

func isFromSeparator(line []byte) bool {
	// "From " at start of line (optional trailing CR already stripped to LF-only).
	s := string(bytes.TrimRight(line, "\n"))
	return strings.HasPrefix(s, "From ")
}

// OpenMBox opens path for reading. Paths ending in .gz are gunzipped.
func OpenMBox(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(strings.ToLower(path), ".gz") {
		zr, err := gzip.NewReader(f)
		if err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("gzip: %w", err)
		}
		return &gzipReadCloser{zr: zr, f: f}, nil
	}
	return f, nil
}

type gzipReadCloser struct {
	zr *gzip.Reader
	f  *os.File
}

func (g *gzipReadCloser) Read(p []byte) (int, error) { return g.zr.Read(p) }

func (g *gzipReadCloser) Close() error {
	err1 := g.zr.Close()
	err2 := g.f.Close()
	if err1 != nil {
		return err1
	}
	return err2
}

// GroupFromMBoxPath derives a newsgroup name from a typical export filename
// such as "alt.fan.usenet.mbox" or "news.groups.mbox.gz".
func GroupFromMBoxPath(path string) string {
	base := path
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	base = strings.TrimSuffix(base, ".gz")
	base = strings.TrimSuffix(base, ".mbox")
	base = strings.TrimSpace(base)
	return base
}
