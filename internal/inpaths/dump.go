package inpaths

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// WriteDump writes a ninpaths 3.1.x dump file.
func (s *Stats) WriteDump(w io.Writer) error {
	if s.articles == 0 {
		return fmt.Errorf("no paths recorded")
	}
	start := s.start.Unix()
	end := s.end.Unix()
	avg := (start + end) / 2
	nbSites := len(s.sites)
	nbRels := len(s.rels)
	if _, err := fmt.Fprintf(w, "!!NINP %s %d %d %d %d %d\n", Version, start, end, nbSites, s.articles, avg); err != nil {
		return err
	}
	for i, name := range s.sites {
		if i > 0 {
			if _, err := fmt.Fprint(w, " "); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "%s %d", name, s.siteCount[name]); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprint(w, "\n!!NLREC\n"); err != nil {
		return err
	}
	first := true
	for _, key := range sortedRelKeys(s.rels) {
		from, to, ok := splitRelKey(key)
		if !ok {
			continue
		}
		fi, ok1 := s.siteIdx[from]
		ti, ok2 := s.siteIdx[to]
		if !ok1 || !ok2 {
			continue
		}
		n := s.rels[key]
		if !first {
			if _, err := fmt.Fprint(w, ":"); err != nil {
				return err
			}
		}
		first = false
		if n == 1 {
			if _, err := fmt.Fprintf(w, ":%d!%d", fi, ti); err != nil {
				return err
			}
		} else {
			if _, err := fmt.Fprintf(w, ":%d!%d,%d", fi, ti, n); err != nil {
				return err
			}
		}
	}
	if first {
		if _, err := fmt.Fprint(w, ":"); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "\n!!NEND %d\n", nbRels); err != nil {
		return err
	}
	return nil
}

func splitRelKey(key string) (from, to string, ok bool) {
	i := strings.Index(key, "!")
	if i <= 0 {
		return "", "", false
	}
	return key[:i], key[i+1:], true
}

func sortedRelKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// stable-ish order for tests
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

// ReadDump parses a ninpaths dump file.
func ReadDump(r io.Reader) (*Stats, error) {
	sc := bufio.NewScanner(r)
	s := NewStats()
	s.articles = 0
	line := nextNonEmpty(sc)
	if line == "" {
		return nil, fmt.Errorf("empty dump")
	}
	if !strings.HasPrefix(line, "!!NINP ") {
		return nil, fmt.Errorf("bad dump header")
	}
	fields := strings.Fields(line)
	if len(fields) < 7 {
		return nil, fmt.Errorf("short dump header")
	}
	start, _ := strconv.ParseInt(fields[2], 10, 64)
	end, _ := strconv.ParseInt(fields[3], 10, 64)
	s.start = time.Unix(start, 0)
	s.end = time.Unix(end, 0)
	s.articles, _ = strconv.Atoi(fields[5])

	for sc.Scan() {
		line = strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "!!NLREC") {
			break
		}
		parts := strings.Fields(line)
		for i := 0; i+1 < len(parts); i += 2 {
			name := parts[i]
			n, _ := strconv.Atoi(parts[i+1])
			s.siteIndex(name)
			s.siteCount[name] = n
		}
	}
	var relLine strings.Builder
	for sc.Scan() {
		t := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(t, "!!NEND") {
			break
		}
		relLine.WriteString(t)
	}
	parseRelations(s, relLine.String())
	return s, sc.Err()
}

func nextNonEmpty(sc *bufio.Scanner) string {
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" {
			return line
		}
	}
	return ""
}

func parseRelations(s *Stats, blob string) {
	blob = strings.TrimSpace(blob)
	if blob == "" || blob == ":" {
		return
	}
	if !strings.HasPrefix(blob, ":") {
		blob = ":" + blob
	}
	for _, part := range strings.Split(blob, ":") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		count := 1
		if j := strings.LastIndex(part, ","); j > 0 {
			if n, err := strconv.Atoi(part[j+1:]); err == nil {
				count = n
				part = part[:j]
			}
		}
		idx := strings.Split(part, "!")
		if len(idx) < 2 {
			continue
		}
		a, err1 := strconv.Atoi(idx[0])
		b, err2 := strconv.Atoi(idx[1])
		if err1 != nil || err2 != nil || a < 0 || b < 0 || a >= len(s.sites) || b >= len(s.sites) {
			continue
		}
		key := s.sites[a] + "!" + s.sites[b]
		s.rels[key] += count
	}
}

// Report returns the ninpaths report body for pathhost (merged dump format).
func (s *Stats) Report(pathhost string) (string, error) {
	if s.articles == 0 {
		return "", fmt.Errorf("no paths recorded")
	}
	var b strings.Builder
	if pathhost != "" {
		fmt.Fprintf(&b, "Path statistics from %s\n\n", pathhost)
	}
	if err := s.WriteDump(&b); err != nil {
		return "", err
	}
	return b.String(), nil
}
