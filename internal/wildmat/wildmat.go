package wildmat

import (
	"strings"
	"unicode/utf8"
)

// Match implements RFC 3977 section 4, plus INN newsfeeds `@` poison
// patterns (treated like `!` for match purposes).
func Match(wildmat, s string) bool {
	if wildmat == "" {
		return false
	}
	matched := false
	found := false
	for _, p := range strings.Split(wildmat, ",") {
		neg := false
		if strings.HasPrefix(p, "!") || strings.HasPrefix(p, "@") {
			neg = true
			p = p[1:]
		}
		if p == "" {
			continue
		}
		if matchPattern(p, s) {
			found = true
			matched = !neg
		}
	}
	return found && matched
}

// MatchAny reports whether any of the strings match the wildmat.
func MatchAny(wildmat string, ss []string) bool {
	for _, s := range ss {
		if Match(wildmat, s) {
			return true
		}
	}
	return false
}

// Poisoned reports whether s matches an `@` poison pattern in wildmat
// (INN: exclude and remember Message-ID).
func Poisoned(wildmat, s string) bool {
	if wildmat == "" {
		return false
	}
	for _, p := range strings.Split(wildmat, ",") {
		if !strings.HasPrefix(p, "@") {
			continue
		}
		p = p[1:]
		if p != "" && matchPattern(p, s) {
			return true
		}
	}
	return false
}

func matchPattern(pat, s string) bool {
	return matchRunes([]rune(pat), []rune(s))
}

func matchRunes(pat, s []rune) bool {
	for {
		if len(pat) == 0 {
			return len(s) == 0
		}
		switch pat[0] {
		case '*':
			for i := 0; i <= len(s); i++ {
				if matchRunes(pat[1:], s[i:]) {
					return true
				}
			}
			return false
		case '?':
			if len(s) == 0 {
				return false
			}
			pat, s = pat[1:], s[1:]
		default:
			if len(s) == 0 || pat[0] != s[0] {
				return false
			}
			pat, s = pat[1:], s[1:]
		}
	}
}

func Valid(w string) bool {
	if w == "" {
		return false
	}
	if !utf8.ValidString(w) {
		return false
	}
	for _, p := range strings.Split(w, ",") {
		p = strings.TrimPrefix(p, "!")
		p = strings.TrimPrefix(p, "@")
		if p == "" {
			return false
		}
		for _, r := range p {
			switch r {
			case ',', '\\', '[', ']':
				return false
			}
		}
	}
	return true
}
