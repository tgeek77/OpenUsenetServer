package isc

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"openusenet/internal/article"
	"openusenet/internal/store"
)

const (
	DefaultURL = "https://ftp.isc.org/usenet/CONFIG"
	UserAgent  = "OpenUsenetServer/0.1.0 (Usenet config; +https://openusenet)"
)

// Fetch downloads ISC active + newsgroups (gzip preferred) and merges them.
func Fetch(ctx context.Context, baseURL string) ([]store.Group, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = DefaultURL
	}
	client := &http.Client{Timeout: 60 * time.Second}
	activeRaw, err := getFile(ctx, client, baseURL, "active")
	if err != nil {
		return nil, err
	}
	statuses, err := ParseActive(bytes.NewReader(activeRaw))
	if err != nil {
		return nil, err
	}
	descRaw, err := getFile(ctx, client, baseURL, "newsgroups")
	if err != nil {
		return nil, fmt.Errorf("newsgroups: %w", err)
	}
	descs, err := ParseNewsgroups(bytes.NewReader(descRaw))
	if err != nil {
		return nil, err
	}
	out := make([]store.Group, 0, len(statuses))
	for name, status := range statuses {
		if !article.ValidGroupName(name) {
			continue
		}
		out = append(out, store.Group{
			Name:        name,
			Status:      status,
			Description: descs[name],
		})
	}
	return out, nil
}

func getFile(ctx context.Context, client *http.Client, base, name string) ([]byte, error) {
	gzURL := base + "/" + name + ".gz"
	body, err := get(ctx, client, gzURL)
	if err == nil {
		return gunzipIfNeeded(body)
	}
	plain, err2 := get(ctx, client, base+"/"+name)
	if err2 != nil {
		return nil, fmt.Errorf("%s: %v (plain: %v)", gzURL, err, err2)
	}
	return gunzipIfNeeded(plain)
}

func get(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "application/gzip, application/octet-stream, */*")
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, res.Body)
		return nil, fmt.Errorf("%s: HTTP %s", url, res.Status)
	}
	return io.ReadAll(io.LimitReader(res.Body, 16<<20))
}

func gunzipIfNeeded(b []byte) ([]byte, error) {
	if len(b) >= 2 && b[0] == 0x1f && b[1] == 0x8b {
		r, err := gzip.NewReader(bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		defer r.Close()
		return io.ReadAll(r)
	}
	return b, nil
}

// ParseActive reads INN active format: name high low status.
func ParseActive(r io.Reader) (map[string]string, error) {
	out := map[string]string{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		name, status := fields[0], fields[3]
		if status == "" || status[0] == '=' {
			continue
		}
		if len(status) > 1 {
			status = status[:1]
		}
		switch status {
		case "y", "n", "m", "j", "x":
			out[name] = status
		}
	}
	return out, sc.Err()
}

// ParseNewsgroups reads name, tab(s), description.
func ParseNewsgroups(r io.Reader) (map[string]string, error) {
	out := map[string]string{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, desc, ok := strings.Cut(line, "\t")
		if !ok {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			name, desc = fields[0], strings.Join(fields[1:], " ")
		} else {
			desc = strings.TrimSpace(desc)
		}
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		out[name] = sanitizeUTF8(desc)
	}
	return out, sc.Err()
}

// sanitizeUTF8 makes ISC descriptions safe for PostgreSQL UTF-8.
// Invalid sequences are treated as ISO-8859-1.
func sanitizeUTF8(s string) string {
	if utf8.ValidString(s) {
		return strings.ToValidUTF8(s, "")
	}
	b := []byte(s)
	var out strings.Builder
	out.Grow(len(b))
	for _, c := range b {
		if c < 0x20 && c != '\t' {
			continue
		}
		out.WriteRune(rune(c))
	}
	return out.String()
}
