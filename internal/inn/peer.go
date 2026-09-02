package inn

import (
	"bufio"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Spec is the unified peer object mapped to INN's three config fragments.
type Spec struct {
	Name          string   `json:"name"`
	PathToken     string   `json:"path_token"`
	IncomingHost  string   `json:"incoming_host"`
	OutgoingHost  string   `json:"outgoing_host"`
	Port          int      `json:"port"`
	Patterns      string   `json:"patterns"`
	Distributions string   `json:"distributions"`
	Flags         string   `json:"flags"`
	Password      string   `json:"password,omitempty"`
	Warnings      []string `json:"warnings,omitempty"`
}

var (
	reIPName     = regexp.MustCompile(`(?i)^\s*ip-name\s*:\s*(\S+)`)
	rePortNumber = regexp.MustCompile(`(?i)^\s*port-number\s*:\s*(\d+)`)
	rePassword   = regexp.MustCompile(`(?i)^\s*password\s*:\s*(\S+)`)
)

// Defaults fills empty fields with sensible INN-compatible values.
func (s *Spec) Defaults() {
	s.Name = strings.TrimSpace(s.Name)
	s.PathToken = strings.TrimSpace(s.PathToken)
	s.IncomingHost = strings.TrimSpace(s.IncomingHost)
	s.OutgoingHost = strings.TrimSpace(s.OutgoingHost)
	if s.OutgoingHost == "" {
		s.OutgoingHost = s.IncomingHost
	}
	if s.IncomingHost == "" {
		s.IncomingHost = s.OutgoingHost
	}
	if s.Port <= 0 {
		s.Port = 119
	}
	if s.Name == "" {
		s.Name = slugName(s.OutgoingHost)
	}
	if s.Patterns == "" {
		s.Patterns = "*"
	}
	if s.Flags == "" {
		s.Flags = "Tm"
	}
}

func slugName(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return "peer"
	}
	if i := strings.Index(host, "."); i > 0 {
		return host[:i]
	}
	return host
}

// ParseFile extracts peer fields from one INN file snippet.
// kind is incoming, innfeed, or newsfeeds.
func ParseFile(text, kind string) (*Spec, []string) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch kind {
	case "incoming", "incoming.conf":
		return parseIncomingFile(text)
	case "innfeed", "innfeed.conf":
		return parseInnfeedFile(text)
	case "newsfeeds", "newsfeed":
		return parseNewsfeedsFile(text)
	default:
		drafts := Parse(text)
		if len(drafts) == 0 {
			return nil, []string{"no peer found in paste"}
		}
		d := drafts[0]
		return &Spec{
			Name:         slugName(d.Host),
			IncomingHost: d.Host,
			OutgoingHost: d.Host,
			Port:         d.Port,
			Patterns:     "*",
			Flags:        "Tm",
			Warnings:     d.Warnings,
		}, d.Warnings
	}
}

func parseIncomingFile(text string) (*Spec, []string) {
	var name, host, password string
	var warns []string
	inPeer := ""
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if m := rePeerBlock.FindStringSubmatch(line); m != nil {
			inPeer = strings.Trim(m[1], `"`)
			continue
		}
		if inPeer != "" {
			if strings.HasPrefix(line, "}") {
				break
			}
			if m := reHostname.FindStringSubmatch(line); m != nil {
				host = strings.Trim(m[1], `";`)
				name = inPeer
				continue
			}
			if m := rePassword.FindStringSubmatch(line); m != nil {
				password = strings.Trim(m[1], `";`)
				continue
			}
			if strings.Contains(strings.ToLower(line), "streaming:") ||
				strings.Contains(strings.ToLower(line), "patterns:") {
				warns = appendUnique(warns, "ignored INN field: "+line)
			}
		}
	}
	if host == "" {
		return nil, []string{"incoming.conf: no hostname found"}
	}
	s := &Spec{Name: name, IncomingHost: host, OutgoingHost: host, Port: 119, Patterns: "*", Flags: "Tm", Password: password, Warnings: warns}
	s.Defaults()
	return s, warns
}

func parseInnfeedFile(text string) (*Spec, []string) {
	var name, host string
	var port int
	var warns []string
	inPeer := ""
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if m := rePeerBlock.FindStringSubmatch(line); m != nil {
			inPeer = strings.Trim(m[1], `"`)
			continue
		}
		if inPeer != "" {
			if strings.HasPrefix(line, "}") {
				break
			}
			if m := reIPName.FindStringSubmatch(line); m != nil {
				host = strings.Trim(m[1], `";`)
				name = inPeer
				continue
			}
			if m := rePortNumber.FindStringSubmatch(line); m != nil {
				port, _ = strconv.Atoi(m[1])
				continue
			}
		}
	}
	if host == "" {
		return nil, []string{"innfeed.conf: no ip-name found"}
	}
	s := &Spec{Name: name, IncomingHost: host, OutgoingHost: host, Port: port, Patterns: "*", Flags: "Tm", Warnings: warns}
	s.Defaults()
	return s, warns
}

func parseNewsfeedsFile(text string) (*Spec, []string) {
	var warns []string
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(strings.ToUpper(line), "ME:") {
			continue
		}
		if m := reNewsfeeds.FindStringSubmatch(line); m != nil {
			sitename, patField, flags := m[1], m[2], m[3]
			name := sitename
			pathToken := ""
			if i := strings.Index(sitename, "/"); i >= 0 {
				name = sitename[:i]
				pathToken = sitename[i+1:]
			}
			patterns, distribs := patField, ""
			if i := strings.LastIndex(patField, "/"); i >= 0 {
				patterns = patField[:i]
				distribs = patField[i+1:]
			}
			warns = appendUnique(warns, "newsfeeds stored for INN export; outbound IHAVE uses host/port only")
			if strings.Contains(flags, "S") {
				warns = appendUnique(warns, "streaming CHECK/TAKETHIS not supported yet")
			}
			s := &Spec{
				Name: name, PathToken: pathToken, Patterns: patterns, Distributions: distribs,
				Flags: flags, Port: 119, Warnings: warns,
			}
			s.Defaults()
			return s, warns
		}
	}
	return nil, []string{"newsfeeds: no feed line found"}
}

// FormatIncoming renders an incoming.conf peer block for our server.
func FormatIncoming(s Spec) string {
	s.Defaults()
	out := fmt.Sprintf("peer %s {\n    hostname:       %s\n", s.Name, s.IncomingHost)
	if strings.TrimSpace(s.Password) != "" {
		out += fmt.Sprintf("    password:       %s\n", s.Password)
	}
	out += "}\n"
	return out
}

// FormatInnfeed renders an innfeed.conf peer block for our server.
func FormatInnfeed(s Spec) string {
	s.Defaults()
	return fmt.Sprintf("peer %s {\n    ip-name:        %s\n    port-number:    %d\n}\n", s.Name, s.OutgoingHost, s.Port)
}

// FormatNewsfeeds renders a newsfeeds line for our server.
func FormatNewsfeeds(s Spec) string {
	s.Defaults()
	site := s.Name
	if s.PathToken != "" {
		site = s.Name + "/" + s.PathToken
	}
	dist := ""
	if s.Distributions != "" {
		dist = "/" + s.Distributions
	}
	return fmt.Sprintf("%s:%s%s:%s:innfeed!\n", site, s.Patterns, dist, s.Flags)
}

// Snippets returns all three INN fragments for configuring our server for this peer.
func Snippets(s Spec) map[string]string {
	s.Defaults()
	return map[string]string{
		"incoming":  FormatIncoming(s),
		"innfeed":   FormatInnfeed(s),
		"newsfeeds": FormatNewsfeeds(s),
	}
}

// ExportOpts customizes the three INN snippets we send to a remote peer.
type ExportOpts struct {
	Patterns       string `json:"patterns"`
	Distributions  string `json:"distributions"`
	Flags          string `json:"flags"`
	Port           int    `json:"port"`
	RemotePathhost string `json:"remote_pathhost"`
	Password       string `json:"password"`
}

// OurSide formats the three snippets we send to a remote peer so they can add us.
func OurSide(hostname, pathhost string, nntpPort int, opts ExportOpts) map[string]string {
	if pathhost == "" {
		pathhost = hostname
	}
	if nntpPort <= 0 {
		nntpPort = 119
	}
	if opts.Port > 0 {
		nntpPort = opts.Port
	}
	name := pathhost
	if i := strings.Index(name, "."); i > 0 {
		name = name[:i]
	}
	patterns := strings.TrimSpace(opts.Patterns)
	if patterns == "" {
		patterns = "*"
	}
	flags := strings.TrimSpace(opts.Flags)
	if flags == "" {
		flags = "Ap,Tm"
	}
	s := Spec{
		Name: name, PathToken: hostname, IncomingHost: hostname,
		OutgoingHost: hostname, Port: nntpPort,
		Patterns: patterns, Distributions: strings.TrimSpace(opts.Distributions), Flags: flags,
		Password: strings.TrimSpace(opts.Password),
	}
	return Snippets(s)
}

// MergeSpec overlays non-empty fields from src onto dst.
func MergeSpec(dst *Spec, src Spec) {
	if src.Name != "" {
		dst.Name = src.Name
	}
	if src.PathToken != "" {
		dst.PathToken = src.PathToken
	}
	if src.IncomingHost != "" {
		dst.IncomingHost = src.IncomingHost
	}
	if src.OutgoingHost != "" {
		dst.OutgoingHost = src.OutgoingHost
	}
	if src.Port > 0 {
		dst.Port = src.Port
	}
	if src.Patterns != "" {
		dst.Patterns = src.Patterns
	}
	if src.Distributions != "" {
		dst.Distributions = src.Distributions
	}
	if src.Flags != "" {
		dst.Flags = src.Flags
	}
	if src.Password != "" {
		dst.Password = src.Password
	}
	dst.Warnings = appendUnique(dst.Warnings, src.Warnings...)
}
