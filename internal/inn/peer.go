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
// kind is incoming, innfeed, or newsfeeds. Any other kind (including auto)
// scans the whole paste for every fragment it can find.
func ParseFile(text, kind string) (*Spec, []string) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch kind {
	case "incoming", "incoming.conf":
		return parseIncomingFile(unfoldContinued(text))
	case "innfeed", "innfeed.conf":
		return parseInnfeedFile(unfoldContinued(text))
	case "newsfeeds", "newsfeed":
		return parseNewsfeedsFile(unfoldContinued(text))
	default:
		spec, warns, _ := ParsePaste(text)
		return spec, warns
	}
}

// ParsePaste pulls peer fields out of a mixed paste: INN fragments, backslash
// continuations, and informal peering mail (hostname, IPV4, IPV6, Pattern).
// present lists JSON field names that the paste actually contained, so callers
// can fill those without wiping fields the paste never mentioned.
func ParsePaste(text string) (spec *Spec, warns []string, present []string) {
	text = unfoldContinued(text)
	var s Spec
	found := false
	mark := func(field string) {
		found = true
		for _, p := range present {
			if p == field {
				return
			}
		}
		present = append(present, field)
	}

	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	inPeer := ""
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		if m := rePeerBlock.FindStringSubmatch(line); m != nil {
			inPeer = strings.Trim(m[1], `"`)
			if s.Name == "" {
				s.Name = inPeer
				mark("name")
			}
			continue
		}
		if inPeer != "" {
			if strings.HasPrefix(line, "}") {
				inPeer = ""
				continue
			}
			if m := reIPName.FindStringSubmatch(line); m != nil {
				s.OutgoingHost = strings.Trim(m[1], `";`)
				mark("host")
				continue
			}
			if m := reHostname.FindStringSubmatch(line); m != nil {
				s.IncomingHost = strings.Trim(m[1], `";`)
				mark("incoming_host")
				continue
			}
			if m := rePortNumber.FindStringSubmatch(line); m != nil {
				s.Port, _ = strconv.Atoi(m[1])
				mark("port")
				continue
			}
			if m := rePassword.FindStringSubmatch(line); m != nil {
				s.Password = strings.Trim(m[1], `";`)
				mark("incoming_password")
				continue
			}
			continue
		}
		if m := rePatternLine.FindStringSubmatch(line); m != nil {
			applyPatternField(&s, m[1])
			mark("patterns")
			if s.Distributions != "" {
				mark("distributions")
			}
			warns = appendUnique(warns, "patterns/flags applied on offer")
			continue
		}
		if m := reIPv4Line.FindStringSubmatch(line); m != nil {
			if s.OutgoingHost == "" && s.IncomingHost == "" {
				s.OutgoingHost = m[1]
				mark("host")
			}
			warns = appendUnique(warns, "IPV4 "+m[1])
			continue
		}
		if m := reIPv6Line.FindStringSubmatch(line); m != nil {
			warns = appendUnique(warns, "IPV6 "+m[1])
			continue
		}
		beforeName, beforePath := s.Name, s.PathToken
		if applyNewsfeedsLine(&s, line) {
			if s.Name != "" && s.Name != beforeName {
				mark("name")
			}
			if s.PathToken != "" && s.PathToken != beforePath {
				mark("path_token")
			}
			mark("patterns")
			if s.Distributions != "" {
				mark("distributions")
			}
			if s.Flags != "" {
				mark("flags")
			}
			warns = appendUnique(warns, "patterns/flags applied on offer")
			continue
		}
		if reBareHost.MatchString(line) {
			if s.Name == "" {
				s.Name = line
				mark("name")
			}
			if s.IncomingHost == "" || isIPLiteral(s.IncomingHost) {
				s.IncomingHost = line
				mark("incoming_host")
			}
			if s.OutgoingHost == "" || isIPLiteral(s.OutgoingHost) {
				s.OutgoingHost = line
				mark("host")
			}
		}
	}
	if !found {
		return nil, []string{"no peer found in paste"}, nil
	}
	s.Warnings = warns
	s.Defaults()
	return &s, warns, present
}

var (
	rePatternLine = regexp.MustCompile(`(?i)^pattern\s*:\s*(.+)$`)
	reIPv4Line    = regexp.MustCompile(`(?i)^ipv4\s*:\s*(\d{1,3}(?:\.\d{1,3}){3})`)
	reIPv6Line    = regexp.MustCompile(`(?i)^ipv6\s*:\s*([0-9A-Fa-f:]+)`)
	reBareHost    = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,62}[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]{0,62}[A-Za-z0-9])?)+$`)
)

func unfoldContinued(text string) string {
	var b strings.Builder
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	pending := ""
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), " \t")
		if pending != "" {
			line = pending + strings.TrimSpace(line)
			pending = ""
		}
		if strings.HasSuffix(line, `\`) {
			pending = strings.TrimRight(strings.TrimSuffix(line, `\`), " \t")
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if pending != "" {
		b.WriteString(pending)
		b.WriteByte('\n')
	}
	return b.String()
}

// splitNewsfeedsFields parses the four colon-separated fields INN requires.
// Flags and param may be empty, as in "site:patterns::".
func splitNewsfeedsFields(line string) (site, patterns, flags, param string, ok bool) {
	parts := strings.SplitN(line, ":", 4)
	if len(parts) < 4 || strings.TrimSpace(parts[0]) == "" {
		return "", "", "", "", false
	}
	return strings.TrimSpace(parts[0]), parts[1], parts[2], parts[3], true
}

func applyNewsfeedsLine(s *Spec, line string) bool {
	site, patField, flags, param, ok := splitNewsfeedsFields(line)
	if !ok {
		return false
	}
	name := site
	pathToken := ""
	if i := strings.Index(site, "/"); i >= 0 {
		name = site[:i]
		pathToken = site[i+1:]
	}
	// ME is this server's subscription. The slash list is path exclusions
	// (INN site/exclude), not a sitename. Program feeds such as ninpaths!
	// are local and are not peers.
	if strings.EqualFold(name, "ME") {
		applyPatternField(s, patField)
		if strings.TrimSpace(flags) != "" {
			s.Flags = strings.TrimSpace(flags)
		}
		if pathToken != "" {
			s.PathToken = pathToken
		}
		return true
	}
	// name! entries such as ninpaths! run a local program. They are not peers.
	if strings.HasSuffix(name, "!") && !strings.Contains(strings.ToLower(param), "innfeed") {
		return false
	}
	if !strings.Contains(param, "!") && !strings.Contains(strings.ToLower(param), "innfeed") {
		return false
	}
	if name != "" {
		s.Name = name
	}
	if pathToken != "" {
		s.PathToken = pathToken
	}
	applyPatternField(s, patField)
	if strings.TrimSpace(flags) != "" {
		s.Flags = strings.TrimSpace(flags)
	}
	return true
}

func isIPLiteral(host string) bool {
	if strings.Contains(host, ":") {
		return true
	}
	if host == "" || strings.ContainsAny(host, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ") {
		return false
	}
	dots := strings.Count(host, ".")
	return dots == 3
}

func applyPatternField(s *Spec, patField string) {
	patField = strings.TrimSpace(patField)
	patField = strings.TrimPrefix(patField, ":")
	patField = strings.TrimRight(patField, `\`)
	patterns, distribs := patField, ""
	if i := strings.Index(patField, "/"); i >= 0 {
		patterns = patField[:i]
		distribs = patField[i+1:]
	}
	if patterns != "" {
		s.Patterns = patterns
	}
	if distribs != "" {
		s.Distributions = distribs
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
			// streaming:/patterns: are handled by our server defaults; no import needed.
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
	var s Spec
	found := false
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if applyNewsfeedsLine(&s, line) {
			found = true
			warns = appendUnique(warns, "patterns/flags applied on offer")
		}
	}
	if !found {
		return nil, []string{"newsfeeds: no feed line found"}
	}
	s.Warnings = warns
	s.Defaults()
	return &s, warns
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
