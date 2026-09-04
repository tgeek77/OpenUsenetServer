package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func (p *Postgres) migrateContentStats(ctx context.Context) error {
	stmts := []string{
		`ALTER TABLE articles ADD COLUMN IF NOT EXISTS is_binary BOOLEAN NOT NULL DEFAULT false`,
		`CREATE TABLE IF NOT EXISTS stats_group_day (
			day DATE NOT NULL,
			group_id BIGINT NOT NULL REFERENCES newsgroups(id) ON DELETE CASCADE,
			text_n INT NOT NULL DEFAULT 0,
			binary_n INT NOT NULL DEFAULT 0,
			PRIMARY KEY (day, group_id)
		)`,
		`CREATE TABLE IF NOT EXISTS stats_group_total (
			group_id BIGINT PRIMARY KEY REFERENCES newsgroups(id) ON DELETE CASCADE,
			text_n INT NOT NULL DEFAULT 0,
			binary_n INT NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS stats_from_day (
			day DATE NOT NULL,
			from_key TEXT NOT NULL,
			n INT NOT NULL DEFAULT 0,
			PRIMARY KEY (day, from_key)
		)`,
		`CREATE TABLE IF NOT EXISTS stats_path_day (
			day DATE NOT NULL,
			site TEXT NOT NULL,
			n INT NOT NULL DEFAULT 0,
			PRIMARY KEY (day, site)
		)`,
		`CREATE INDEX IF NOT EXISTS stats_from_day_n ON stats_from_day(day, n DESC)`,
		`CREATE INDEX IF NOT EXISTS stats_path_day_n ON stats_path_day(day, n DESC)`,
	}
	for _, q := range stmts {
		if _, err := p.pool.Exec(ctx, q); err != nil {
			return fmt.Errorf("content stats migrate: %w", err)
		}
	}
	return nil
}

// ContentStatsEvent is one accepted article for popularity rollups.
type ContentStatsEvent struct {
	Groups      []string
	From        string
	Path        string
	Binary      bool
	ExcludeSite []string // pathhost, hostname
	Day         time.Time
}

// NameCount is a ranked name + count pair.
type NameCount struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

// FromCount is a ranked From + count pair.
type FromCount struct {
	From  string `json:"from"`
	Count int64  `json:"count"`
}

// SiteCount is a ranked Path site + count pair.
type SiteCount struct {
	Site  string `json:"site"`
	Count int64  `json:"count"`
}

// ContentStats is the reader Stats dashboard payload.
type ContentStats struct {
	PopulatedGroups   int64                  `json:"populated_groups"`
	TopGroupsToday    map[string][]NameCount `json:"top_groups_today"`
	TopGroupsTotal    map[string][]NameCount `json:"top_groups_total"`
	TopPostersToday   []FromCount            `json:"top_posters_today"`
	TopPostersTotal   []FromCount            `json:"top_posters_total"`
	TopProvidersToday []SiteCount            `json:"top_providers_today"`
	TopProvidersTotal []SiteCount            `json:"top_providers_total"`
}

func (p *Postgres) RecordContentStats(ctx context.Context, ev ContentStatsEvent) error {
	day := ev.Day.UTC()
	if day.IsZero() {
		day = time.Now().UTC()
	}
	day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	textInc, binInc := 0, 0
	if ev.Binary {
		binInc = 1
	} else {
		textInc = 1
	}

	for _, name := range ev.Groups {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		var gid int64
		err := tx.QueryRow(ctx, `SELECT id FROM newsgroups WHERE name=$1`, name).Scan(&gid)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO stats_group_day (day, group_id, text_n, binary_n)
			VALUES ($1,$2,$3,$4)
			ON CONFLICT (day, group_id) DO UPDATE SET
				text_n = stats_group_day.text_n + EXCLUDED.text_n,
				binary_n = stats_group_day.binary_n + EXCLUDED.binary_n`,
			day, gid, textInc, binInc); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO stats_group_total (group_id, text_n, binary_n)
			VALUES ($1,$2,$3)
			ON CONFLICT (group_id) DO UPDATE SET
				text_n = stats_group_total.text_n + EXCLUDED.text_n,
				binary_n = stats_group_total.binary_n + EXCLUDED.binary_n`,
			gid, textInc, binInc); err != nil {
			return err
		}
	}

	fromKey := NormalizeFromKey(ev.From)
	if fromKey != "" {
		if _, err := tx.Exec(ctx, `
			INSERT INTO stats_from_day (day, from_key, n) VALUES ($1,$2,1)
			ON CONFLICT (day, from_key) DO UPDATE SET n = stats_from_day.n + 1`,
			day, fromKey); err != nil {
			return err
		}
	}

	for _, site := range PathStatSites(ev.Path, ev.ExcludeSite...) {
		if _, err := tx.Exec(ctx, `
			INSERT INTO stats_path_day (day, site, n) VALUES ($1,$2,1)
			ON CONFLICT (day, site) DO UPDATE SET n = stats_path_day.n + 1`,
			day, site); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func NormalizeFromKey(from string) string {
	return strings.ToLower(strings.TrimSpace(from))
}

// PathStatSites returns Path bang-hops excluding self and not-for-mail.
func PathStatSites(path string, exclude ...string) []string {
	ex := map[string]struct{}{}
	for _, e := range exclude {
		e = strings.ToLower(strings.TrimSpace(e))
		if e != "" {
			ex[e] = struct{}{}
		}
	}
	ex["not-for-mail"] = struct{}{}
	var out []string
	seen := map[string]struct{}{}
	for _, hop := range strings.Split(path, "!") {
		hop = strings.TrimSpace(hop)
		if hop == "" {
			continue
		}
		key := strings.ToLower(hop)
		if _, skip := ex[key]; skip {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, hop)
	}
	return out
}

func (p *Postgres) ContentStats(ctx context.Context) (ContentStats, error) {
	var out ContentStats
	out.TopGroupsToday = map[string][]NameCount{}
	out.TopGroupsTotal = map[string][]NameCount{}

	err := p.pool.QueryRow(ctx, `SELECT COUNT(*) FROM newsgroups WHERE count > 1`).Scan(&out.PopulatedGroups)
	if err != nil {
		return out, err
	}

	today := time.Now().UTC()
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)

	todayGroups, err := p.topGroupsDay(ctx, today, 100)
	if err != nil {
		return out, err
	}
	totalGroups, err := p.topGroupsTotal(ctx, 100)
	if err != nil {
		return out, err
	}
	out.TopGroupsToday = sliceTops(todayGroups)
	out.TopGroupsTotal = sliceTops(totalGroups)

	out.TopPostersToday, err = p.topFromDay(ctx, today, 10)
	if err != nil {
		return out, err
	}
	out.TopPostersTotal, err = p.topFromAll(ctx, 10)
	if err != nil {
		return out, err
	}
	out.TopProvidersToday, err = p.topPathDay(ctx, today, 10)
	if err != nil {
		return out, err
	}
	out.TopProvidersTotal, err = p.topPathAll(ctx, 10)
	if err != nil {
		return out, err
	}
	return out, nil
}

func sliceTops(all []NameCount) map[string][]NameCount {
	m := map[string][]NameCount{"10": {}, "25": {}, "100": {}}
	for _, n := range []int{10, 25, 100} {
		key := fmt.Sprintf("%d", n)
		if len(all) < n {
			m[key] = append([]NameCount{}, all...)
		} else {
			m[key] = append([]NameCount{}, all[:n]...)
		}
	}
	return m
}

func (p *Postgres) topGroupsDay(ctx context.Context, day time.Time, limit int) ([]NameCount, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT g.name, s.text_n
		FROM stats_group_day s
		JOIN newsgroups g ON g.id = s.group_id
		WHERE s.day = $1 AND s.text_n > 0
		ORDER BY s.text_n DESC, g.name
		LIMIT $2`, day, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNameCounts(rows)
}

func (p *Postgres) topGroupsTotal(ctx context.Context, limit int) ([]NameCount, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT g.name, s.text_n
		FROM stats_group_total s
		JOIN newsgroups g ON g.id = s.group_id
		WHERE s.text_n > 0
		ORDER BY s.text_n DESC, g.name
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNameCounts(rows)
}

func (p *Postgres) topFromDay(ctx context.Context, day time.Time, limit int) ([]FromCount, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT from_key, n FROM stats_from_day
		WHERE day = $1 ORDER BY n DESC, from_key LIMIT $2`, day, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FromCount
	for rows.Next() {
		var f FromCount
		if err := rows.Scan(&f.From, &f.Count); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (p *Postgres) topFromAll(ctx context.Context, limit int) ([]FromCount, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT from_key, SUM(n)::bigint AS n
		FROM stats_from_day
		GROUP BY from_key
		ORDER BY n DESC, from_key
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FromCount
	for rows.Next() {
		var f FromCount
		if err := rows.Scan(&f.From, &f.Count); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (p *Postgres) topPathDay(ctx context.Context, day time.Time, limit int) ([]SiteCount, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT site, n FROM stats_path_day
		WHERE day = $1 ORDER BY n DESC, site LIMIT $2`, day, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SiteCount
	for rows.Next() {
		var s SiteCount
		if err := rows.Scan(&s.Site, &s.Count); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (p *Postgres) topPathAll(ctx context.Context, limit int) ([]SiteCount, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT site, SUM(n)::bigint AS n
		FROM stats_path_day
		GROUP BY site
		ORDER BY n DESC, site
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SiteCount
	for rows.Next() {
		var s SiteCount
		if err := rows.Scan(&s.Site, &s.Count); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

type nameCountRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanNameCounts(rows nameCountRows) ([]NameCount, error) {
	var out []NameCount
	for rows.Next() {
		var n NameCount
		if err := rows.Scan(&n.Name, &n.Count); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
