package article

import (
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"strings"
	"unicode"
)

// DecodeBodyForDisplay decodes Content-Transfer-Encoding and the declared
// charset for the web newsreader, independent of language. Stored articles
// and the NNTP wire format are not changed. Multipart prefers text/plain.
func DecodeBodyForDisplay(headers, body string) string {
	art, err := Parse([]byte(strings.TrimRight(headers, "\r\n") + "\r\n\r\n" + body))
	if err != nil {
		return SanitizeUTF8(body)
	}
	return decodeDisplayBody(art.Get("Content-Type"), art.Get("Content-Transfer-Encoding"), art.Body)
}

func decodeDisplayBody(contentType, cte, body string) string {
	media, params := parseContentType(contentType)
	raw, err := decodeTransfer(body, cte)
	if err != nil {
		raw = body
	}
	if strings.HasPrefix(media, "multipart/") {
		if part, ok := decodeMultipartPart(raw, params["boundary"]); ok {
			return part
		}
	}
	if strings.HasPrefix(media, "text/") || media == "" || media == "application/octet-stream" {
		return decodeCharset(raw, params["charset"])
	}
	return SanitizeUTF8(raw)
}

func parseContentType(ct string) (string, map[string]string) {
	ct = strings.TrimSpace(ct)
	if ct == "" {
		return "text/plain", map[string]string{}
	}
	media, params, err := mime.ParseMediaType(ct)
	if err != nil {
		return strings.ToLower(strings.TrimSpace(strings.Split(ct, ";")[0])), map[string]string{}
	}
	return strings.ToLower(media), params
}

func decodeTransfer(body, cte string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(cte)) {
	case "", "7bit", "8bit", "binary":
		return body, nil
	case "quoted-printable":
		b, err := io.ReadAll(quotedprintable.NewReader(strings.NewReader(body)))
		if err != nil {
			return body, err
		}
		return string(b), nil
	case "base64":
		clean := strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return -1
			}
			return r
		}, body)
		b, err := base64.StdEncoding.DecodeString(clean)
		if err != nil {
			b, err = base64.RawStdEncoding.DecodeString(clean)
			if err != nil {
				return body, err
			}
		}
		return string(b), nil
	default:
		return body, nil
	}
}

func decodeMultipartPart(body, boundary string) (string, bool) {
	if boundary == "" {
		return "", false
	}
	mr := multipart.NewReader(strings.NewReader(body), boundary)
	var html string
	for {
		p, err := mr.NextPart()
		if err != nil {
			break
		}
		media, params := parseContentType(p.Header.Get("Content-Type"))
		slurp, err := io.ReadAll(p)
		_ = p.Close()
		if err != nil {
			continue
		}
		decoded, decErr := decodeTransfer(string(slurp), p.Header.Get("Content-Transfer-Encoding"))
		if decErr != nil {
			decoded = string(slurp)
		}
		if strings.HasPrefix(media, "multipart/") {
			if inner, ok := decodeMultipartPart(decoded, params["boundary"]); ok {
				return inner, true
			}
			continue
		}
		text := decodeCharset(decoded, params["charset"])
		if media == "text/plain" || media == "" {
			return text, true
		}
		if html == "" && strings.HasPrefix(media, "text/") {
			html = text
		}
	}
	if html != "" {
		return html, true
	}
	return "", false
}
