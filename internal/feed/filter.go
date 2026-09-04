package feed

import (
	"strings"

	"openusenet/internal/store"
	"openusenet/internal/wildmat"
)

// GroupsWanted reports whether an article's newsgroups match the peer's
// subscription patterns (INN newsfeeds-style wildmat, including @ poison).
// Empty patterns default to "*".
func GroupsWanted(patterns string, groups []string) bool {
	pat := strings.TrimSpace(patterns)
	if pat == "" {
		pat = "*"
	}
	return wildmat.MatchAny(pat, groups)
}

// PeerWants is GroupsWanted for a Peer.
func PeerWants(p store.Peer, groups []string) bool {
	return GroupsWanted(p.Patterns, groups)
}
