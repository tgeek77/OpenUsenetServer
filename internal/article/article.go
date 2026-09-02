package article

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/mail"
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

func Parse(raw []byte) (*Article, error) {
	raw = bytes.ReplaceAll(raw, []byte("\n"), []byte("\n"))
	// Normalise lone LF to CRLF for split, keep content.
	text := string(normalizeNewlines(raw))
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
	if err := a.parseHeaders(head); err != nil {
		return nil, err
	}
	return a, nil
}

func normalizeNewlines(b []byte) []byte {
	b = bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
	b = bytes.ReplaceAll(b, []byte("\r"), []byte("\n"))
	b = bytes.ReplaceAll(b, []byte("\n"), []byte("\r\n"))
	return b
}

func (a *Article) parseHeaders(head string) error {
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
				return fmt.Errorf("folded header with no name")
			}
			curVal.WriteByte(' ')
			curVal.WriteString(strings.TrimSpace(line))
			continue
		}
		flush()
		name, val, ok := strings.Cut(line, ":")
		if !ok {
			return fmt.Errorf("malformed header line")
		}
		curName = name
		if strings.HasPrefix(val, " ") {
			val = val[1:]
		}
		curVal.WriteString(val)
	}
	flush()
	return nil
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
	// Stable-ish order: well-known headers first, then the rest.
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
	n := strings.Count(a.Body, "\n")
	if !strings.HasSuffix(a.Body, "\n") {
		n++
	}
	return n
}

type InjectOpts struct {
	Pathhost     string
	Hostname     string
	Organization string
	Now          time.Time
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
		a.Set("Message-ID", msgid)
	} else {
		a.Set("Message-ID", GenerateMessageID(opt.Hostname))
	}
	if !a.Has("Date") {
		a.Set("Date", opt.Now.Format(time.RFC1123Z))
	} else if _, err := mail.ParseDate(a.Get("Date")); err != nil {
		// keep original if weird; injection still adds Injection-Date
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
		a.Set("Injection-Date", opt.Now.UTC().Format("20060102150405")+"Z")
	}
	if !a.Has("Injection-Info") {
		a.Set("Injection-Info", fmt.Sprintf("%s; posting-account=\"openusenet\"", opt.Hostname))
	}
	// POST must not carry a client-supplied Xref.
	for k := range a.Headers {
		if strings.EqualFold(k, "Xref") {
			delete(a.Headers, k)
		}
	}
	return nil
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
		fields = append(fields, "Xref:full "+xref)
	}
	return strings.Join(fields, "\t")
}

func sanitizeOverview(s string) string {
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}
