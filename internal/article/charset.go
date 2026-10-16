package article

import (
	"fmt"
	"io"
	"strings"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/encoding/ianaindex"
	"golang.org/x/text/transform"
)

// encodingForCharset maps a MIME/HTML charset name to a decoder.
// Names are matched through the IANA MIME index, then IANA, then HTML,
// so Usenet labels (iso-8859-1, koi8-r, gb2312, shift_jis, windows-1256, …)
// work regardless of language.
func encodingForCharset(name string) (encoding.Encoding, error) {
	name = strings.Trim(strings.TrimSpace(name), `"'`)
	if name == "" {
		return encoding.Nop, nil
	}
	lower := strings.ToLower(name)
	if lower == "utf-8" || lower == "utf8" || lower == "us-ascii" || lower == "ascii" {
		return encoding.Nop, nil
	}
	candidates := []string{name, lower}
	if strings.HasPrefix(lower, "x-") {
		candidates = append(candidates, strings.TrimPrefix(lower, "x-"))
	}
	for _, try := range candidates {
		if enc, err := ianaindex.MIME.Encoding(try); err == nil && enc != nil {
			return enc, nil
		}
		if enc, err := ianaindex.IANA.Encoding(try); err == nil && enc != nil {
			return enc, nil
		}
		if enc, err := htmlindex.Get(try); err == nil && enc != nil {
			return enc, nil
		}
	}
	return nil, fmt.Errorf("unsupported charset %q", name)
}

func charsetReader(charset string, input io.Reader) (io.Reader, error) {
	enc, err := encodingForCharset(charset)
	if err != nil {
		return nil, err
	}
	if enc == nil || enc == encoding.Nop {
		return input, nil
	}
	return transform.NewReader(input, enc.NewDecoder()), nil
}

func decodeCharset(s, charset string) string {
	enc, err := encodingForCharset(charset)
	if err != nil || enc == nil || enc == encoding.Nop {
		return SanitizeUTF8(s)
	}
	out, _, err := transform.String(enc.NewDecoder(), s)
	if err != nil {
		return SanitizeUTF8(s)
	}
	return SanitizeUTF8(out)
}
