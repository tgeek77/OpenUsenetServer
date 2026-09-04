package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	wmat "openusenet/internal/wildmat"
)

type memAccept struct {
	binary bool
	at     time.Time
}

type memAlert struct {
	GroupAlert
}

func (m *Memory) ensureRetentionMaps() {
	if m.accepts == nil {
		m.accepts = map[string][]memAccept{}
	}
	if m.alerts == nil {
		m.alerts = []memAlert{}
	}
	if m.binQuota == nil {
		m.binQuota = map[string]int{} // userID|day -> count
	}
	if m.nextAID == 0 {
		m.nextAID = 1
	}
}

func (m *Memory) NoteAccept(_ context.Context, groups []string, binary bool, flood FloodParams) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureRetentionMaps()
	if flood.Window <= 0 {
		flood.Window = 24 * time.Hour
	}
	if flood.FloodDays <= 0 {
		flood.FloodDays = 7
	}
	now := time.Now().UTC()
	var flooded []string
	for _, name := range groups {
		name = strings.TrimSpace(name)
		g, ok := m.groups[name]
		if !ok {
			continue
		}
		m.accepts[name] = append(m.accepts[name], memAccept{binary: binary, at: now})
		if g.RetentionMode == RetentionModeWhitelist {
			continue
		}
		if g.RetentionDays == nil && flood.SeedWildmat != "" && wmat.Match(flood.SeedWildmat, name) {
			days := flood.FloodDays
			g.RetentionDays = &days
			g.RetentionMode = RetentionModeAuto
			m.ensureOpenAlertLocked(name, fmt.Sprintf("seed wildmat match (%s)", flood.SeedWildmat))
			flooded = append(flooded, name)
		}
		if !binary {
			continue
		}
		since := now.Add(-flood.Window)
		total, bins := 0, 0
		for _, e := range m.accepts[name] {
			if e.at.Before(since) {
				continue
			}
			total++
			if e.binary {
				bins++
			}
		}
		if total == 0 || bins < flood.MinBinary {
			continue
		}
		ratio := float64(bins) / float64(total)
		if ratio < flood.MinRatio {
			continue
		}
		days := flood.FloodDays
		if g.RetentionMode != RetentionModeWhitelist {
			g.RetentionDays = &days
			g.RetentionMode = RetentionModeAuto
		}
		detail := fmt.Sprintf("%d binary / %d accepts in %s (ratio %.2f)", bins, total, flood.Window, ratio)
		m.ensureOpenAlertLocked(name, detail)
		flooded = append(flooded, name)
	}
	return flooded, nil
}

func (m *Memory) ensureOpenAlertLocked(group, detail string) {
	for i := range m.alerts {
		if m.alerts[i].GroupName == group && m.alerts[i].Kind == AlertKindBinaryFlood && m.alerts[i].Status == AlertOpen {
			m.alerts[i].Detail = detail
			return
		}
	}
	m.alerts = append(m.alerts, memAlert{GroupAlert{
		ID: m.nextAID, GroupName: group, Kind: AlertKindBinaryFlood,
		Detail: detail, Status: AlertOpen, CreatedAt: time.Now().UTC(),
	}})
	m.nextAID++
}

func (m *Memory) ConsumeBinaryPostQuota(_ context.Context, userID int64, limit int) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureRetentionMaps()
	if limit <= 0 {
		return 0, nil
	}
	key := fmt.Sprintf("%d|%s", userID, time.Now().UTC().Format("2006-01-02"))
	count := m.binQuota[key]
	if count >= limit {
		return count, ErrQuotaExceeded
	}
	count++
	m.binQuota[key] = count
	return count, nil
}

func (m *Memory) BinaryPostQuotaUsed(_ context.Context, userID int64) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureRetentionMaps()
	key := fmt.Sprintf("%d|%s", userID, time.Now().UTC().Format("2006-01-02"))
	return m.binQuota[key], nil
}

func (m *Memory) ListGroupAlerts(_ context.Context, status string) ([]GroupAlert, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureRetentionMaps()
	var out []GroupAlert
	for _, a := range m.alerts {
		if status != "" && status != "all" && a.Status != status {
			continue
		}
		out = append(out, a.GroupAlert)
	}
	return out, nil
}

func (m *Memory) ResolveGroupAlert(_ context.Context, id int64, action string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureRetentionMaps()
	for i := range m.alerts {
		if m.alerts[i].ID != id {
			continue
		}
		group := m.alerts[i].GroupName
		switch strings.ToLower(strings.TrimSpace(action)) {
		case "block":
			if g, ok := m.groups[group]; ok {
				g.Status = "n"
			}
			m.alerts[i].Status = AlertBlocked
		case "whitelist":
			if g, ok := m.groups[group]; ok {
				g.RetentionDays = nil
				g.RetentionMode = RetentionModeWhitelist
			}
			m.alerts[i].Status = AlertWhitelisted
		case "dismiss":
			m.alerts[i].Status = AlertDismissed
		case "reset":
			if g, ok := m.groups[group]; ok {
				g.RetentionDays = nil
				g.RetentionMode = ""
			}
			m.alerts[i].Status = AlertDismissed
		default:
			return fmt.Errorf("unknown action %q", action)
		}
		return nil
	}
	return fmt.Errorf("alert not found")
}

func (m *Memory) SetGroupRetention(_ context.Context, name string, days *int, mode string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.groups[name]
	if !ok {
		return fmt.Errorf("group not found")
	}
	g.RetentionDays = days
	g.RetentionMode = mode
	return nil
}

func (m *Memory) Expire(_ context.Context, historyDays int) (ExpireResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var res ExpireResult
	now := time.Now().UTC()
	for gname, g := range m.groups {
		if g.RetentionDays == nil || *g.RetentionDays <= 0 {
			continue
		}
		cutoff := now.Add(-time.Duration(*g.RetentionDays) * 24 * time.Hour)
		var keep []int64
		for _, n := range g.nums {
			a := m.byNum[gname][n]
			if a == nil {
				continue
			}
			if a.StoredAt.Before(cutoff) {
				delete(a.groups, gname)
				delete(m.byNum[gname], n)
				g.Count--
				res.OverviewRemoved++
				continue
			}
			keep = append(keep, n)
		}
		g.nums = keep
		if len(keep) == 0 {
			g.Low, g.High, g.Count = 0, 0, 0
		} else {
			g.Low, g.High = keep[0], keep[len(keep)-1]
			g.Count = int64(len(keep))
		}
	}
	for msgid, a := range m.arts {
		if len(a.groups) == 0 {
			delete(m.arts, msgid)
			res.ArticlesRemoved++
		}
	}
	if historyDays > 0 {
		cutoff := now.Add(-time.Duration(historyDays) * 24 * time.Hour)
		for id, at := range m.history {
			if at.Before(cutoff) {
				if _, ok := m.arts[id]; !ok {
					delete(m.history, id)
					res.HistoryRemoved++
				}
			}
		}
	}
	return res, nil
}
