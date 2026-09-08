package feed

import (
	"strings"

	"openusenet/internal/store"
	"openusenet/internal/wildmat"
)

// GroupsWanted reports whether an article's newsgroups match the peer's
// subscription patterns (INN newsfeeds-style wildmat, including @ poison).
// Empty patterns default to "*". Poison (@) on any group rejects the whole article.
func GroupsWanted(patterns string, groups []string) bool {
	pat := strings.TrimSpace(patterns)
	if pat == "" {
		pat = "*"
	}
	for _, g := range groups {
		if wildmat.Poisoned(pat, g) {
			return false
		}
	}
	return wildmat.MatchAny(pat, groups)
}

// PeerWants is GroupsWanted for a Peer (patterns only).
func PeerWants(p store.Peer, groups []string) bool {
	return GroupsWanted(p.Patterns, groups)
}

// PeerWantsArticle applies patterns, distributions, and newsfeeds flags.
func PeerWantsArticle(p store.Peer, v ArticleView) bool {
	if !GroupsWanted(p.Patterns, v.Groups) {
		return false
	}
	if !DistributionWanted(p.Distributions, v.Distribution) {
		return false
	}
	flags := ParseFlags(p.Flags)
	return flags.Allows(v)
}
