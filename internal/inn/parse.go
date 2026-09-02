package inn

import (
	"bufio"
	"net"
	"regexp"
	"strconv"
	"strings"
)

// Draft is a parsed peer candidate before Apply.
type Draft struct {
	Host     string   `json:"host"`
	Port     int      `json:"port"`
	Notes    string   `json:"notes"`
	Warnings []string `json:"warnings,omitempty"`
}

var (
	reHostname   = regexp.MustCompile(`(?i)^\s*hostname\s*:\s*(\S+)`)
	rePeerBlock  = regexp.MustCompile(`(?i)^\s*peer\s+(\S+)`)
	reNewsfeeds  = regexp.MustCompile(`^([^:]+):([^:]*):([^:]*):(.+)$`)
	reHostPort   = regexp.MustCompile(`^([A-Za-z0-9._-]+)(?::(\d+))?$`)
)

// Parse extracts outbound peer drafts from INN newsfeeds / incoming.conf snippets
// or simple host[:port] lines. Unsupported knobs become Warnings.
func Parse(text string) []Draft {
	byHost := map[string]*Draft{}
	order := []string{}
	add := func(host string, port int, notes string, warn ...string) {
		host = strings.TrimSpace(host)
		if host == "" || strings.EqualFold(host, "ME") {
			return
		}
		if port <= 0 {
			port = 119
		}
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		key := strings.ToLower(host) + "|" + strconv.Itoa(port)
		d, ok := byHost[key]
		if !ok {
			d = &Draft{Host: host, Port: port}
			byHost[key] = d
			order = append(order, key)
		}
		if notes != "" && d.Notes == "" {
			d.Notes = notes
		}
		d.Warnings = appendUnique(d.Warnings, warn...)
	}

	sc := bufio.NewScanner(strings.NewReader(text))
	var inPeer string
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		if m := rePeerBlock.FindStringSubmatch(line); m != nil {
			inPeer = strings.Trim(m[1], `"`)
			continue
		}
		if inPeer != "" {
			if strings.HasPrefix(line, "}") {
				inPeer = ""
				continue
			}
			if m := reHostname.FindStringSubmatch(line); m != nil {
				host := strings.Trim(m[1], `";`)
				add(host, 119, "incoming.conf peer "+inPeer,
					"INN streaming/filters not imported; outbound IHAVE host/port only")
				continue
			}
			if strings.Contains(strings.ToLower(line), "streaming:") ||
				strings.Contains(strings.ToLower(line), "max-size:") ||
				strings.Contains(strings.ToLower(line), "patterns:") {
				add(inPeer, 119, "incoming.conf peer "+inPeer,
					"ignored INN field: "+line)
			}
			continue
		}
		if m := reNewsfeeds.FindStringSubmatch(line); m != nil {
			name, patterns, flags, rest := m[1], m[2], m[3], m[4]
			host := strings.TrimSpace(rest)
			if i := strings.IndexAny(host, " \t"); i >= 0 {
				host = host[:i]
			}
			warns := []string{"newsfeeds flags/patterns not applied (" + flags + "; " + patterns + ")"}
			if strings.Contains(flags, "S") || strings.Contains(flags, "Nm") {
				warns = append(warns, "streaming CHECK/TAKETHIS not supported yet")
			}
			add(host, 119, "newsfeeds:"+name, warns...)
			continue
		}
		if m := reHostPort.FindStringSubmatch(line); m != nil {
			port := 119
			if m[2] != "" {
				port, _ = strconv.Atoi(m[2])
			}
			add(m[1], port, "plain host line")
		}
	}
	out := make([]Draft, 0, len(order))
	for _, k := range order {
		out = append(out, *byHost[k])
	}
	return out
}

func appendUnique(dst []string, add ...string) []string {
	seen := map[string]bool{}
	for _, s := range dst {
		seen[s] = true
	}
	for _, s := range add {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		dst = append(dst, s)
	}
	return dst
}
