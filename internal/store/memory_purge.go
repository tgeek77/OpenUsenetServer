package store

import (
	"context"
	"sort"
)

func (m *Memory) PurgeUnwantedArticles(_ context.Context) (PurgeResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureRetentionMaps()

	names := map[string]bool{}
	for _, a := range m.alerts {
		if a.Status != AlertBlocked {
			continue
		}
		names[a.GroupName] = true
		if g, ok := m.groups[a.GroupName]; ok {
			g.Status = "n"
			g.RetentionDays = nil
			g.RetentionMode = RetentionModeBlocked
		}
	}
	for name, g := range m.groups {
		if g.RetentionMode == RetentionModeBlocked {
			names[name] = true
		}
		for _, pattern := range m.bans {
			if groupMatches([]string{pattern}, name) {
				names[name] = true
				break
			}
		}
	}
	var res PurgeResult
	if len(names) == 0 {
		return res, nil
	}
	for name := range names {
		res.Groups = append(res.Groups, name)
	}
	sort.Strings(res.Groups)

	cross := map[string]bool{}
	var doomed []string
	for msgid, art := range m.arts {
		hit := false
		for g := range art.groups {
			if names[g] {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		doomed = append(doomed, msgid)
		for g := range art.groups {
			if !names[g] {
				cross[g] = true
			}
		}
	}
	sort.Strings(doomed)
	for g := range cross {
		res.CrosspostGroups = append(res.CrosspostGroups, g)
	}
	sort.Strings(res.CrosspostGroups)
	for _, msgid := range doomed {
		if m.deleteArticleLocked(msgid) {
			res.Articles++
			res.MessageIDs = append(res.MessageIDs, msgid)
		}
	}
	return res, nil
}
