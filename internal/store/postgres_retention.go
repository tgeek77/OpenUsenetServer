package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	wmat "openusenet/internal/wildmat"
)

func (p *Postgres) migrateRetention(ctx context.Context) error {
	alters := []string{
		`ALTER TABLE newsgroups ADD COLUMN IF NOT EXISTS retention_days INT`,
		`ALTER TABLE newsgroups ADD COLUMN IF NOT EXISTS retention_mode TEXT NOT NULL DEFAULT ''`,
		`CREATE TABLE IF NOT EXISTS group_accept_events (
			id BIGSERIAL PRIMARY KEY,
			group_id BIGINT NOT NULL REFERENCES newsgroups(id) ON DELETE CASCADE,
			is_binary BOOLEAN NOT NULL,
			seen_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS group_accept_events_group_seen ON group_accept_events(group_id, seen_at)`,
		`CREATE TABLE IF NOT EXISTS group_alerts (
			id BIGSERIAL PRIMARY KEY,
			group_name TEXT NOT NULL,
			kind TEXT NOT NULL,
			detail TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'open',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS group_alerts_status ON group_alerts(status)`,
		`CREATE TABLE IF NOT EXISTS user_binary_posts (
			user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			day DATE NOT NULL,
			count INT NOT NULL DEFAULT 0,
			PRIMARY KEY (user_id, day)
		)`,
	}
	for _, q := range alters {
		if _, err := p.pool.Exec(ctx, q); err != nil {
			return fmt.Errorf("retention migrate: %w", err)
		}
	}
	return nil
}

const groupSelectCols = `name, description, status, low, high, count, created_at, retention_days, retention_mode`

func (p *Postgres) NoteAccept(ctx context.Context, groups []string, binary bool, flood FloodParams) ([]string, error) {
	var flooded []string
	if flood.Window <= 0 {
		flood.Window = 24 * time.Hour
	}
	if flood.FloodDays <= 0 {
		flood.FloodDays = 7
	}
	for _, name := range groups {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		var id int64
		var mode string
		var retDays *int
		err := p.pool.QueryRow(ctx, `SELECT id, retention_mode, retention_days FROM newsgroups WHERE name=$1`, name).
			Scan(&id, &mode, &retDays)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return flooded, err
		}
		if _, err := p.pool.Exec(ctx, `INSERT INTO group_accept_events (group_id, is_binary) VALUES ($1,$2)`, id, binary); err != nil {
			return flooded, err
		}
		if mode == RetentionModeWhitelist {
			continue
		}
		// Seed known binary hierarchies with short retention without waiting for flood.
		if retDays == nil && flood.SeedWildmat != "" && wmat.Match(flood.SeedWildmat, name) {
			days := flood.FloodDays
			if _, err := p.pool.Exec(ctx, `
				UPDATE newsgroups SET retention_days=$1, retention_mode=$2
				WHERE id=$3 AND retention_mode <> $4`,
				days, RetentionModeAuto, id, RetentionModeWhitelist); err != nil {
				return flooded, err
			}
			retDays = &days
			if err := p.ensureOpenAlert(ctx, name, fmt.Sprintf("seed wildmat match (%s)", flood.SeedWildmat)); err != nil {
				return flooded, err
			}
			flooded = append(flooded, name)
		}
		if !binary {
			continue
		}
		since := time.Now().UTC().Add(-flood.Window)
		var total, bins int
		err = p.pool.QueryRow(ctx, `
			SELECT COUNT(*), COALESCE(SUM(CASE WHEN is_binary THEN 1 ELSE 0 END),0)
			FROM group_accept_events WHERE group_id=$1 AND seen_at >= $2`, id, since).Scan(&total, &bins)
		if err != nil {
			return flooded, err
		}
		if total == 0 || bins < flood.MinBinary {
			continue
		}
		ratio := float64(bins) / float64(total)
		if ratio < flood.MinRatio {
			continue
		}
		days := flood.FloodDays
		tag, err := p.pool.Exec(ctx, `
			UPDATE newsgroups SET retention_days=$1, retention_mode=$2
			WHERE id=$3 AND retention_mode <> $4`,
			days, RetentionModeAuto, id, RetentionModeWhitelist)
		if err != nil {
			return flooded, err
		}
		detail := fmt.Sprintf("%d binary / %d accepts in %s (ratio %.2f)", bins, total, flood.Window, ratio)
		if err := p.ensureOpenAlert(ctx, name, detail); err != nil {
			return flooded, err
		}
		if tag.RowsAffected() > 0 {
			flooded = append(flooded, name)
		}
	}
	return flooded, nil
}

func (p *Postgres) ensureOpenAlert(ctx context.Context, group, detail string) error {
	var n int
	err := p.pool.QueryRow(ctx, `
		SELECT 1 FROM group_alerts WHERE group_name=$1 AND kind=$2 AND status=$3 LIMIT 1`,
		group, AlertKindBinaryFlood, AlertOpen).Scan(&n)
	if err == nil {
		_, err = p.pool.Exec(ctx, `
			UPDATE group_alerts SET detail=$1 WHERE group_name=$2 AND kind=$3 AND status=$4`,
			detail, group, AlertKindBinaryFlood, AlertOpen)
		return err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	_, err = p.pool.Exec(ctx, `
		INSERT INTO group_alerts (group_name, kind, detail, status) VALUES ($1,$2,$3,$4)`,
		group, AlertKindBinaryFlood, detail, AlertOpen)
	return err
}

func (p *Postgres) ConsumeBinaryPostQuota(ctx context.Context, userID int64, limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}
	day := time.Now().UTC().Format("2006-01-02")
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var count int
	err = tx.QueryRow(ctx, `
		SELECT count FROM user_binary_posts WHERE user_id=$1 AND day=$2::date FOR UPDATE`,
		userID, day).Scan(&count)
	if errors.Is(err, pgx.ErrNoRows) {
		count = 0
		err = nil
	}
	if err != nil {
		return 0, err
	}
	if count >= limit {
		return count, ErrQuotaExceeded
	}
	count++
	_, err = tx.Exec(ctx, `
		INSERT INTO user_binary_posts (user_id, day, count) VALUES ($1,$2::date,$3)
		ON CONFLICT (user_id, day) DO UPDATE SET count = EXCLUDED.count`,
		userID, day, count)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return count, nil
}

func (p *Postgres) BinaryPostQuotaUsed(ctx context.Context, userID int64) (int, error) {
	day := time.Now().UTC().Format("2006-01-02")
	var count int
	err := p.pool.QueryRow(ctx, `
		SELECT count FROM user_binary_posts WHERE user_id=$1 AND day=$2::date`, userID, day).Scan(&count)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return count, err
}

func (p *Postgres) ListGroupAlerts(ctx context.Context, status string) ([]GroupAlert, error) {
	var rows pgx.Rows
	var err error
	if status == "" || status == "all" {
		rows, err = p.pool.Query(ctx, `
			SELECT id, group_name, kind, detail, status, created_at FROM group_alerts
			ORDER BY created_at DESC LIMIT 200`)
	} else {
		rows, err = p.pool.Query(ctx, `
			SELECT id, group_name, kind, detail, status, created_at FROM group_alerts
			WHERE status=$1 ORDER BY created_at DESC LIMIT 200`, status)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GroupAlert
	for rows.Next() {
		var a GroupAlert
		if err := rows.Scan(&a.ID, &a.GroupName, &a.Kind, &a.Detail, &a.Status, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (p *Postgres) ResolveGroupAlert(ctx context.Context, id int64, action string) error {
	var group, status string
	err := p.pool.QueryRow(ctx, `SELECT group_name, status FROM group_alerts WHERE id=$1`, id).Scan(&group, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("alert not found")
	}
	if err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "block":
		if _, err := p.pool.Exec(ctx, `UPDATE newsgroups SET status='n' WHERE name=$1`, group); err != nil {
			return err
		}
		_, err = p.pool.Exec(ctx, `UPDATE group_alerts SET status=$1 WHERE id=$2`, AlertBlocked, id)
	case "whitelist":
		if _, err := p.pool.Exec(ctx, `
			UPDATE newsgroups SET retention_days=NULL, retention_mode=$1 WHERE name=$2`,
			RetentionModeWhitelist, group); err != nil {
			return err
		}
		_, err = p.pool.Exec(ctx, `UPDATE group_alerts SET status=$1 WHERE id=$2`, AlertWhitelisted, id)
	case "dismiss":
		_, err = p.pool.Exec(ctx, `UPDATE group_alerts SET status=$1 WHERE id=$2`, AlertDismissed, id)
	case "reset":
		if _, err := p.pool.Exec(ctx, `
			UPDATE newsgroups SET retention_days=NULL, retention_mode='' WHERE name=$1`, group); err != nil {
			return err
		}
		_, err = p.pool.Exec(ctx, `UPDATE group_alerts SET status=$1 WHERE id=$2`, AlertDismissed, id)
	default:
		return fmt.Errorf("unknown action %q", action)
	}
	return err
}

func (p *Postgres) SetGroupRetention(ctx context.Context, name string, days *int, mode string) error {
	tag, err := p.pool.Exec(ctx, `
		UPDATE newsgroups SET retention_days=$1, retention_mode=$2 WHERE name=$3`, days, mode, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("group not found")
	}
	return nil
}

func (p *Postgres) Expire(ctx context.Context, historyDays int) (ExpireResult, error) {
	var res ExpireResult
	// Remove overview rows for articles past per-group retention.
	tag, err := p.pool.Exec(ctx, `
		DELETE FROM overview o
		USING newsgroups g, articles a
		WHERE o.group_id = g.id AND o.article_id = a.id
		  AND g.retention_days IS NOT NULL AND g.retention_days > 0
		  AND a.stored_at < now() - (g.retention_days || ' days')::interval`)
	if err != nil {
		return res, err
	}
	res.OverviewRemoved = int(tag.RowsAffected())

	tag, err = p.pool.Exec(ctx, `
		DELETE FROM articles a
		WHERE NOT EXISTS (SELECT 1 FROM overview o WHERE o.article_id = a.id)`)
	if err != nil {
		return res, err
	}
	res.ArticlesRemoved = int(tag.RowsAffected())

	if historyDays > 0 {
		tag, err = p.pool.Exec(ctx, `
			DELETE FROM history WHERE arrived_at < now() - ($1 || ' days')::interval
			  AND message_id NOT IN (SELECT message_id FROM articles)`, fmt.Sprintf("%d", historyDays))
		if err != nil {
			return res, err
		}
		res.HistoryRemoved = int(tag.RowsAffected())
	}
	// Prune old accept events (keep 7 days beyond flood window).
	_, _ = p.pool.Exec(ctx, `DELETE FROM group_accept_events WHERE seen_at < now() - interval '8 days'`)
	return res, nil
}
