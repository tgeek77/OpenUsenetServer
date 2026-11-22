package article

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Article is a Netnews article on the wire (headers + body, CRLF).
type Article struct {
	RawHeaders string
	Body       string
	Headers    map[string][]string // canonical name (original case of first seen)
}

// Parse parses a wire-format CRLF Netnews article without altering header order,
// folding, or bare CR/LF bytes. Header field names are validated per RFC 5322.
func Parse(raw []byte) (*Article, error) {
	return parse(raw, true)
}

// ParseLenient parses a stored or archived article, tolerating legacy header lines
// that would fail strict field-name validation.
func ParseLenient(raw []byte) (*Article, error) {
	return parse(raw, false)
}

func parse(raw []byte, strict bool) (*Article, error) {
	text := string(raw)
	head, body, ok := strings.Cut(text, "\r\n\r\n")
	if !ok {
		// empty body is allowed if trailing CRLF pair present
		if strings.HasSuffix(text, "\r\n") {
			head = strings.TrimSuffix(text, "\r\n")
			body = ""
		} else {
			return nil, fmt.Errorf("article has no header/body separator")
		}
	}
	a := &Article{
		RawHeaders: head,
		Body:       body,
		Headers:    map[string][]string{},
	}
	if err := a.parseHeaders(head, strict); err != nil {
		return nil, err
	}
	return a, nil
}

func validHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		// RFC 5322 section 3.6.8 ftext: printable US-ASCII 33..126 except ':' (58)
		if c < 0x21 || c > 0x7e || c == ':' {
			return false
		}
	}
	return true
}

func (a *Article) parseHeaders(head string, strict bool) error {
	var curName string
	var curVal strings.Builder
	flush := func() {
		if curName == "" {
			return
		}
		a.Headers[curName] = append(a.Headers[curName], strings.TrimSpace(curVal.String()))
		curName, curVal = "", strings.Builder{}
	}
	for _, line := range strings.Split(head, "\r\n") {
		if line == "" {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			if curName == "" {
				if strict {
					return fmt.Errorf("folded header with no name")
				}
				continue
			}
			curVal.WriteByte(' ')
			curVal.WriteString(strings.TrimSpace(line))
			continue
		}
		flush()
		name, val, ok := strings.Cut(line, ":")
		if !ok || !validHeaderName(name) {
			if strict {
				return fmt.Errorf("malformed header line")
			}
			continue
		}
		curName = name
		curVal.WriteString(strings.TrimPrefix(val, " "))
	}
	flush()
	return nil
}

// splitHeaderBlocks splits RawHeaders into logical header blocks (each block is
// a header line plus any following folded continuation lines).
func splitHeaderBlocks(rawHeaders string) []string {
	if rawHeaders == "" {
		return nil
	}
	lines := strings.Split(rawHeaders, "\r\n")
	var blocks []string
	for _, line := range lines {
		if line == "" {
			continue
		}
		if (line[0] == ' ' || line[0] == '\t') && len(blocks) > 0 {
			blocks[len(blocks)-1] += "\r\n" + line
		} else {
			blocks = append(blocks, line)
		}
	}
	return blocks
}

func headerBlockName(block string) string {
	first, _, _ := strings.Cut(block, "\r\n")
	name, _, ok := strings.Cut(first, ":")
	if !ok {
		return ""
	}
	return name
}

func setRawHeader(rawHeaders, name, value string, prependIfNew bool) string {
	blocks := splitHeaderBlocks(rawHeaders)
	replacement := name + ": " + value
	out := make([]string, 0, len(blocks)+1)
	replaced := false
	for _, blk := range blocks {
		if strings.EqualFold(headerBlockName(blk), name) {
			if !replaced {
				out = append(out, replacement)
				replaced = true
			}
			continue
		}
		out = append(out, blk)
	}
	if !replaced {
		if prependIfNew {
			out = append([]string{replacement}, out...)
		} else {
			out = append(out, replacement)
		}
	}
	return strings.Join(out, "\r\n")
}

func deleteRawHeader(rawHeaders, name string) string {
	blocks := splitHeaderBlocks(rawHeaders)
	out := make([]string, 0, len(blocks))
	for _, blk := range blocks {
		if strings.EqualFold(headerBlockName(blk), name) {
			continue
		}
		out = append(out, blk)
	}
	return strings.Join(out, "\r\n")
}

// WithXref returns rawHeaders with its Xref header replaced or appended.
// If xref is empty, rawHeaders is returned unchanged.
func WithXref(rawHeaders, xref string) string {
	xref = strings.TrimSpace(xref)
	if xref == "" {
		return rawHeaders
	}
	return setRawHeader(rawHeaders, "Xref", xref, false)
}

func (a *Article) Get(name string) string {
	for k, vs := range a.Headers {
		if strings.EqualFold(k, name) && len(vs) > 0 {
			return vs[0]
		}
	}
	return ""
}

func (a *Article) Set(name, value string) {
	for k := range a.Headers {
		if strings.EqualFold(k, name) {
			delete(a.Headers, k)
		}
	}
	a.Headers[name] = []string{value}
	if a.RawHeaders != "" {
		a.RawHeaders = setRawHeader(a.RawHeaders, name, value, strings.EqualFold(name, "Path"))
	}
}

func (a *Article) Delete(name string) {
	for k := range a.Headers {
		if strings.EqualFold(k, name) {
			delete(a.Headers, k)
		}
	}
	if a.RawHeaders != "" {
		a.RawHeaders = deleteRawHeader(a.RawHeaders, name)
	}
}

func (a *Article) Has(name string) bool {
	return a.Get(name) != ""
}

func (a *Article) Newsgroups() []string {
	raw := a.Get("Newsgroups")
	if raw == "" {
		return nil
	}
	var out []string
	for _, g := range strings.Split(raw, ",") {
		g = strings.TrimSpace(g)
		if g != "" {
			out = append(out, g)
		}
	}
	return out
}

func (a *Article) Wire() []byte {
	var b strings.Builder
	if a.RawHeaders != "" {
		b.WriteString(a.RawHeaders)
		b.WriteString("\r\n\r\n")
	} else {
		// Fallback for programmatically constructed Article structs without RawHeaders.
		order := []string{
			"Path", "From", "Newsgroups", "Subject", "Date", "Message-ID",
			"Organization", "Injection-Date", "Injection-Info", "References",
			"Followup-To", "Reply-To", "Sender", "Expires", "Distribution",
			"Keywords", "Summary", "Approved", "Control", "MIME-Version",
			"Content-Type", "Content-Transfer-Encoding", "User-Agent", "Xref",
		}
		seen := map[string]bool{}
		write := func(name, val string) {
			b.WriteString(name)
			b.WriteString(": ")
			b.WriteString(val)
			b.WriteString("\r\n")
		}
		for _, name := range order {
			for k, vs := range a.Headers {
				if strings.EqualFold(k, name) {
					seen[k] = true
					for _, v := range vs {
						write(k, v)
					}
				}
			}
		}
		for k, vs := range a.Headers {
			if seen[k] {
				continue
			}
			for _, v := range vs {
				write(k, v)
			}
		}
		b.WriteString("\r\n")
	}
	b.WriteString(a.Body)
	if a.Body != "" && !strings.HasSuffix(a.Body, "\r\n") {
		b.WriteString("\r\n")
	}
	return []byte(b.String())
}

func (a *Article) Bytes() int { return len(a.Wire()) }

func (a *Article) Lines() int {
	if a.Body == "" {
		return 0
	}
	n := strings.Count(a.Body, "\r\n")
	if !strings.HasSuffix(a.Body, "\r\n") {
		n++
	}
	return n
}

type InjectOpts struct {
	Pathhost       string
	Hostname       string
	Organization   string
	PostingAccount string
	Now            time.Time
}

// InjectForPost validates and fills RFC 5536 injection headers.
func InjectForPost(a *Article, opt InjectOpts) error {
	if opt.Now.IsZero() {
		opt.Now = time.Now().UTC()
	}
	if strings.TrimSpace(a.Get("From")) == "" {
		return fmt.Errorf("missing From")
	}
	if len(a.Newsgroups()) == 0 {
		return fmt.Errorf("missing Newsgroups")
	}
	if strings.TrimSpace(a.Get("Subject")) == "" {
		return fmt.Errorf("missing Subject")
	}
	for _, g := range a.Newsgroups() {
		if !ValidGroupName(g) {
			return fmt.Errorf("bad newsgroup name %q", g)
		}
	}
	if msgid := strings.TrimSpace(a.Get("Message-ID")); msgid != "" {
		if !ValidMessageID(msgid) {
			return fmt.Errorf("bad Message-ID")
		}
	} else {
		a.Set("Message-ID", GenerateMessageID(opt.Hostname))
	}
	if !a.Has("Date") {
		a.Set("Date", opt.Now.Format(time.RFC1123Z))
	}
	if !a.Has("Path") {
		a.Set("Path", opt.Pathhost+"!not-for-mail")
	} else if !strings.HasPrefix(a.Get("Path"), opt.Pathhost+"!") {
		a.Set("Path", opt.Pathhost+"!"+a.Get("Path"))
	}
	if opt.Organization != "" && !a.Has("Organization") {
		a.Set("Organization", opt.Organization)
	}
	if !a.Has("Injection-Date") {
		// INN parses this with parsedate_rfc5322_lax and rejects the
		// compact YYYYMMDDHHMMSSZ form, then remembers the message-id.
		a.Set("Injection-Date", opt.Now.UTC().Format(time.RFC1123Z))
	}
	if !a.Has("Injection-Info") {
		if acct := strings.TrimSpace(opt.PostingAccount); acct != "" {
			a.Set("Injection-Info", fmt.Sprintf("%s; posting-account=%q", opt.Hostname, acct))
		} else {
			a.Set("Injection-Info", opt.Hostname)
		}
	}
	stripXref(a)
	return nil
}

// InjectForIHave validates a transferred article and prepends Path.
func InjectForIHave(a *Article, opt InjectOpts) error {
	if strings.TrimSpace(a.Get("From")) == "" {
		return fmt.Errorf("missing From")
	}
	if len(a.Newsgroups()) == 0 {
		return fmt.Errorf("missing Newsgroups")
	}
	if strings.TrimSpace(a.Get("Subject")) == "" {
		return fmt.Errorf("missing Subject")
	}
	for _, g := range a.Newsgroups() {
		if !ValidGroupName(g) {
			return fmt.Errorf("bad newsgroup name %q", g)
		}
	}
	msgid := strings.TrimSpace(a.Get("Message-ID"))
	if msgid == "" {
		return fmt.Errorf("missing Message-ID")
	}
	if !ValidMessageID(msgid) {
		return fmt.Errorf("bad Message-ID")
	}
	if strings.TrimSpace(a.Get("Date")) == "" {
		return fmt.Errorf("missing Date")
	}
	if PathContains(a.Get("Path"), opt.Pathhost) {
		return fmt.Errorf("path loop")
	}
	if !a.Has("Path") {
		a.Set("Path", opt.Pathhost+"!not-for-mail")
	} else {
		a.Set("Path", opt.Pathhost+"!"+a.Get("Path"))
	}
	stripXref(a)
	return nil
}

func stripXref(a *Article) {
	a.Delete("Xref")
}

// PathContains reports whether token appears as a bang-separated Path hop.
func PathContains(path, token string) bool {
	token = strings.TrimSpace(token)
	if token == "" || path == "" {
		return false
	}
	for _, hop := range strings.Split(path, "!") {
		if strings.EqualFold(hop, token) {
			return true
		}
	}
	return false
}

func ValidGroupName(g string) bool {
	if g == "" || len(g) > 255 {
		return false
	}
	for _, r := range g {
		if r > 127 {
			return false
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '+' || r == '-' || r == '_' {
			continue
		}
		return false
	}
	if strings.HasPrefix(g, ".") || strings.HasSuffix(g, ".") || strings.Contains(g, "..") {
		return false
	}
	return true
}

func ValidMessageID(id string) bool {
	if len(id) < 3 || len(id) > MaxMessageID {
		return false
	}
	if id[0] != '<' || id[len(id)-1] != '>' {
		return false
	}
	inner := id[1 : len(id)-1]
	if strings.ContainsAny(inner, "<> \t\r\n") {
		return false
	}
	for i := 0; i < len(inner); i++ {
		if inner[i] < 0x21 || inner[i] > 0x7e {
			return false
		}
	}
	return strings.Contains(inner, "@")
}

const MaxMessageID = 250

func GenerateMessageID(host string) string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	if host == "" {
		host = "localhost"
	}
	return fmt.Sprintf("<%d.%s@%s>", time.Now().UnixNano(), hex.EncodeToString(b[:]), host)
}

func OverviewLine(num int64, a *Article, xref string) string {
	fields := []string{
		strconv.FormatInt(num, 10),
		sanitizeOverview(a.Get("Subject")),
		sanitizeOverview(a.Get("From")),
		sanitizeOverview(a.Get("Date")),
		sanitizeOverview(a.Get("Message-ID")),
		sanitizeOverview(a.Get("References")),
		strconv.Itoa(a.Bytes()),
		strconv.Itoa(a.Lines()),
	}
	if xref != "" {
		// OVERVIEW.FMT lists this column as "Xref:full". RFC 2980 says the
		// :full suffix means the overview value itself is "Xref: <value>",
		// not the literal format token "Xref:full".
		fields = append(fields, "Xref: "+xref)
	}
	return strings.Join(fields, "\t")
}

func sanitizeOverview(s string) string {
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}
