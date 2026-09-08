package feed

import (
	"strconv"
	"strings"

	"openusenet/internal/article"
)

// Flags is a parsed INN newsfeeds(5) flag field for outbound article selection.
// Feed type (Tm/Tf/…) and write items (W…) are ignored for transfer filtering.
type Flags struct {
	Raw string

	MaxBytes int // <N
	MinBytes int // >N

	// A-checks (last of c/C wins).
	ExcludeControl bool // Ac
	OnlyControl    bool // AC
	NeedDistrib    bool // Ad
	AllGroupsExist bool // Ae / Af historically often paired; Af in INN is "filter" — see below
	NoFiltered     bool // Af — don't send filter-rejected (we never queue those)
	JunkByGroups   bool // Aj — pattern against Newsgroups (always our behavior)
	NeedOverview   bool // Ao — require overview; we always store overview
	PathOnlyExclude bool // Ap — Path exclusions only (not sitename)

	MaxCrossCost   int // Ccount: |groups| + |followups|^2
	MaxGroups      int // Gcount
	MaxPathHops    int // Hcount (0 = unset; bare H means 1)
	MaxFollowups   int // Ucount
	ModeratedOnly  bool
	UnmoderatedOnly bool
}

// ParseFlags parses an INN newsfeeds flags string (comma-separated).
func ParseFlags(raw string) Flags {
	f := Flags{Raw: strings.TrimSpace(raw)}
	if f.Raw == "" {
		return f
	}
	for _, part := range strings.Split(f.Raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		switch {
		case strings.HasPrefix(part, "<"):
			if n, err := strconv.Atoi(part[1:]); err == nil && n > 0 {
				f.MaxBytes = n
			}
		case strings.HasPrefix(part, ">"):
			if n, err := strconv.Atoi(part[1:]); err == nil && n > 0 {
				f.MinBytes = n
			}
		case part[0] == 'A' || part[0] == 'a':
			parseAChecks(&f, part[1:])
		case part[0] == 'C' || part[0] == 'c':
			if n, err := strconv.Atoi(part[1:]); err == nil {
				f.MaxCrossCost = n
			}
		case part[0] == 'G' || part[0] == 'g':
			if n, err := strconv.Atoi(part[1:]); err == nil {
				f.MaxGroups = n
			}
		case part[0] == 'H' || part[0] == 'h':
			if part == "H" || part == "h" {
				f.MaxPathHops = 1
			} else if n, err := strconv.Atoi(part[1:]); err == nil {
				f.MaxPathHops = n
			}
		case part[0] == 'U' || part[0] == 'u':
			if n, err := strconv.Atoi(part[1:]); err == nil {
				f.MaxFollowups = n
			}
		case part[0] == 'N' || part[0] == 'n':
			switch strings.ToLower(part[1:]) {
			case "m":
				f.ModeratedOnly = true
			case "u":
				f.UnmoderatedOnly = true
			}
		}
	}
	return f
}

func parseAChecks(f *Flags, checks string) {
	for _, r := range checks {
		switch r {
		case 'c':
			f.ExcludeControl = true
			f.OnlyControl = false
		case 'C':
			f.OnlyControl = true
			f.ExcludeControl = false
		case 'd':
			f.NeedDistrib = true
		case 'e':
			f.AllGroupsExist = true
		case 'f':
			f.NoFiltered = true
		case 'j':
			f.JunkByGroups = true
		case 'o':
			f.NeedOverview = true
		case 'p', 'P':
			f.PathOnlyExclude = true
		}
	}
}

// ArticleView is the article metadata needed for newsfeeds flag checks.
type ArticleView struct {
	Groups       []string
	Path         string
	Distribution string
	Control      string
	FollowupTo   string
	Bytes        int
	Filtered     bool
	// GroupStatus maps newsgroup → active status ("y","n","m",…). Missing = absent.
	GroupStatus map[string]string
}

// ViewFromArticle builds an ArticleView from a parsed article and wire size.
func ViewFromArticle(art *article.Article, wireLen int) ArticleView {
	if art == nil {
		return ArticleView{Bytes: wireLen}
	}
	return ArticleView{
		Groups:       art.Newsgroups(),
		Path:         art.Get("Path"),
		Distribution: art.Get("Distribution"),
		Control:      art.Get("Control"),
		FollowupTo:   art.Get("Followup-To"),
		Bytes:        wireLen,
	}
}

// Allows reports whether the article may be sent given these flags.
// groupOK is consulted when Ae or Nm/Nu need the active file; nil skips those checks.
func (f Flags) Allows(v ArticleView) bool {
	if f.MaxBytes > 0 && v.Bytes >= f.MaxBytes {
		return false
	}
	if f.MinBytes > 0 && v.Bytes <= f.MinBytes {
		return false
	}
	isControl := strings.TrimSpace(v.Control) != ""
	if f.ExcludeControl && isControl {
		return false
	}
	if f.OnlyControl && !isControl {
		return false
	}
	if f.NeedDistrib && strings.TrimSpace(v.Distribution) == "" {
		return false
	}
	if f.NoFiltered && v.Filtered {
		return false
	}
	if f.MaxGroups > 0 && len(v.Groups) > f.MaxGroups {
		return false
	}
	followN := followupGroupCount(v)
	if f.MaxFollowups > 0 && followN > f.MaxFollowups {
		return false
	}
	if f.MaxCrossCost > 0 {
		cost := len(v.Groups) + followN*followN
		if cost > f.MaxCrossCost {
			return false
		}
	}
	if f.MaxPathHops > 0 && pathHopCount(v.Path) > f.MaxPathHops {
		return false
	}
	if f.AllGroupsExist {
		if v.GroupStatus == nil {
			return false
		}
		for _, g := range v.Groups {
			if _, ok := v.GroupStatus[g]; !ok {
				return false
			}
		}
	}
	if f.ModeratedOnly || f.UnmoderatedOnly {
		if v.GroupStatus == nil {
			return false
		}
		for _, g := range v.Groups {
			st, ok := v.GroupStatus[g]
			if !ok {
				return false
			}
			mod := strings.EqualFold(st, "m")
			if f.ModeratedOnly && !mod {
				return false
			}
			if f.UnmoderatedOnly && mod {
				return false
			}
		}
	}
	return true
}

func followupGroupCount(v ArticleView) int {
	ft := strings.TrimSpace(v.FollowupTo)
	if ft == "" {
		return len(v.Groups)
	}
	if strings.EqualFold(ft, "poster") {
		return 0
	}
	n := 0
	for _, g := range strings.Split(ft, ",") {
		if strings.TrimSpace(g) != "" {
			n++
		}
	}
	return n
}

func pathHopCount(path string) int {
	path = strings.TrimSpace(path)
	if path == "" {
		return 0
	}
	n := 0
	for _, hop := range strings.Split(path, "!") {
		if strings.TrimSpace(hop) != "" {
			n++
		}
	}
	return n
}

// DistributionWanted implements INN newsfeeds distribution sub-field rules.
// Empty distribs means all articles are eligible (distribution ignored).
func DistributionWanted(distribs string, distributionHeader string) bool {
	distribs = strings.TrimSpace(distribs)
	if distribs == "" {
		return true
	}
	header := strings.TrimSpace(distributionHeader)
	if header == "" {
		// No Distribution header: only restricted if ME-style inbound; for site feeds, send.
		return true
	}
	var tokens []string
	for _, t := range strings.Split(distribs, ",") {
		t = strings.TrimSpace(t)
		if t != "" {
			tokens = append(tokens, t)
		}
	}
	if len(tokens) == 0 {
		return true
	}
	hasNeg := false
	for _, t := range tokens {
		if strings.HasPrefix(t, "!") {
			hasNeg = true
			break
		}
	}
	// Multiple distributions in the header are OR'd.
	for _, d := range strings.Split(header, ",") {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		if distributionTokenAllows(tokens, d, hasNeg) {
			return true
		}
	}
	return false
}

func distributionTokenAllows(tokens []string, dist string, hasNeg bool) bool {
	matchedPos := false
	for _, t := range tokens {
		neg := strings.HasPrefix(t, "!")
		pat := strings.TrimPrefix(t, "!")
		if !strings.EqualFold(pat, dist) {
			continue
		}
		if neg {
			return false
		}
		matchedPos = true
	}
	if matchedPos {
		return true
	}
	// No match: if only negations were listed, send; if any positive listed, don't.
	return hasNeg && !hasPositiveDistrib(tokens)
}

func hasPositiveDistrib(tokens []string) bool {
	for _, t := range tokens {
		if !strings.HasPrefix(t, "!") {
			return true
		}
	}
	return false
}
