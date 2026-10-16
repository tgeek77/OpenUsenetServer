package article

import (
	"mime"
	"strings"
)

// wordDecoder decodes RFC 2047 encoded-words (=?charset?Q?...?= and B form).
// CharsetReader uses IANA MIME names so any declared charset converts to UTF-8.
var wordDecoder = mime.WordDecoder{
	CharsetReader: charsetReader,
}

// DecodeWords decodes RFC 2047 encoded-words in a header value.
// Text that is not encoded is returned unchanged. On a charset error the
// original string is returned so a bad word does not erase the header.
func DecodeWords(s string) string {
	if !strings.Contains(s, "=?") {
		return s
	}
	out, err := wordDecoder.DecodeHeader(s)
	if err != nil {
		return s
	}
	return out
}

// DecodeHeaderBlock decodes encoded-words in each header value of a stored
// header block. Header names and the following Date line stay in place.
func DecodeHeaderBlock(head string) string {
	if !strings.Contains(head, "=?") {
		return head
	}
	text := strings.ReplaceAll(head, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	var logical []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		logical = append(logical, cur.String())
		cur.Reset()
	}
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			continue
		}
		if (line[0] == ' ' || line[0] == '\t') && cur.Len() > 0 {
			cur.WriteByte(' ')
			cur.WriteString(strings.TrimSpace(line))
			continue
		}
		flush()
		cur.WriteString(line)
	}
	flush()
	var b strings.Builder
	for i, line := range logical {
		name, val, ok := strings.Cut(line, ":")
		if !ok {
			b.WriteString(line)
		} else {
			b.WriteString(name)
			b.WriteByte(':')
			b.WriteString(DecodeWords(val))
		}
		if i < len(logical)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
