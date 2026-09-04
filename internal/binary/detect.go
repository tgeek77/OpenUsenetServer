// Package binary classifies Usenet articles as binary vs text (yEnc, uuencode, MIME, Base64).
package binary

import (
	"mime"
	"strings"
	"unicode"
)

// LooksBinary reports whether headers+body look like a binary/filesharing post.
func LooksBinary(headers, body string) bool {
	if yEnc(body) || uuencode(body) {
		return true
	}
	if mimeBinary(headers) && len(body) > 512 {
		return true
	}
	if bulkBase64(body) {
		return true
	}
	return false
}

func yEnc(body string) bool {
	lower := strings.ToLower(body)
	return strings.Contains(lower, "=ybegin") ||
		strings.Contains(lower, "=ypart") ||
		strings.Contains(lower, "=yend")
}

func uuencode(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "begin ") {
			rest := strings.TrimPrefix(line, "begin ")
			parts := strings.Fields(rest)
			if len(parts) >= 2 && isOctalMode(parts[0]) {
				return true
			}
		}
	}
	return false
}

func isOctalMode(s string) bool {
	if len(s) < 3 || len(s) > 4 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '7' {
			return false
		}
	}
	return true
}

func mimeBinary(headers string) bool {
	ct := headerValue(headers, "Content-Type")
	if ct == "" {
		return false
	}
	media, _, err := mime.ParseMediaType(ct)
	if err != nil {
		media = strings.ToLower(strings.TrimSpace(strings.Split(ct, ";")[0]))
	} else {
		media = strings.ToLower(media)
	}
	switch {
	case strings.HasPrefix(media, "application/"):
		if media == "application/pgp-signature" || media == "application/pkcs7-signature" {
			return false
		}
		return true
	case strings.HasPrefix(media, "image/"),
		strings.HasPrefix(media, "audio/"),
		strings.HasPrefix(media, "video/"),
		media == "message/partial":
		return true
	default:
		return false
	}
}

func headerValue(headers, name string) string {
	for _, line := range strings.Split(headers, "\n") {
		line = strings.TrimRight(line, "\r")
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(k), name) {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// bulkBase64 detects long runs of Base64-looking lines.
func bulkBase64(body string) bool {
	lines := strings.Split(body, "\n")
	run := 0
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if isBase64Line(line) {
			run++
			if run >= 8 {
				return true
			}
			continue
		}
		run = 0
	}
	return false
}

func isBase64Line(line string) bool {
	line = strings.TrimSpace(line)
	if len(line) < 60 {
		return false
	}
	pad := 0
	for _, r := range line {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '+', r == '/':
			continue
		case r == '=':
			pad++
			if pad > 2 {
				return false
			}
		default:
			if unicode.IsSpace(r) {
				continue
			}
			return false
		}
	}
	return true
}
