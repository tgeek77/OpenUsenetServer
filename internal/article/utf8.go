package article

import (
	"strings"
	"unicode/utf8"
)

// SanitizeUTF8 makes text safe for PostgreSQL UTF-8 storage.
// Valid UTF-8 is left alone; invalid bytes are treated as ISO-8859-1.
func SanitizeUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	b := []byte(s)
	var out strings.Builder
	out.Grow(len(b))
	for i := 0; i < len(b); {
		c := b[i]
		if c < 0x20 && c != '\t' && c != '\n' && c != '\r' {
			i++
			continue
		}
		r, size := utf8.DecodeRune(b[i:])
		if r != utf8.RuneError || size > 1 {
			out.WriteRune(r)
			i += size
			continue
		}
		out.WriteRune(rune(c))
		i++
	}
	return out.String()
}
