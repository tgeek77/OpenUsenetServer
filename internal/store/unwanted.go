package store

import (
	"context"
	"strings"

	wmat "openusenet/internal/wildmat"
)

// GroupsUnwanted reports whether any named group is ignored or blocked.
// One match makes the whole article unwanted, including crossposts.
func GroupsUnwanted(ctx context.Context, st Store, groups []string) (bool, error) {
	if st == nil || len(groups) == 0 {
		return false, nil
	}
	patterns, err := st.ListGroupBans(ctx)
	if err != nil {
		return false, err
	}
	if groupMatches(patterns, groups...) {
		return true, nil
	}
	for _, name := range groups {
		g, err := st.GetGroup(ctx, name)
		if err != nil {
			return false, err
		}
		if g != nil && g.RetentionMode == RetentionModeBlocked {
			return true, nil
		}
	}
	return false, nil
}

func groupMatches(patterns []string, groups ...string) bool {
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		for _, name := range groups {
			if wmat.Match(pattern, name) {
				return true
			}
		}
	}
	return false
}
