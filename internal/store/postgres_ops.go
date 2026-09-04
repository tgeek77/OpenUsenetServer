package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func (p *Postgres) migrateFeedQueue(ctx context.Context) error {
	_, err := p.pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS feed_queue (
    id BIGSERIAL PRIMARY KEY,
    peer_id BIGINT NOT NULL REFERENCES peers(id) ON DELETE CASCADE,
    message_id TEXT NOT NULL,
    attempts INT NOT NULL DEFAULT 0,
    next_attempt TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (peer_id, message_id)
);
CREATE INDEX IF NOT EXISTS feed_queue_due_idx ON feed_queue(next_attempt);
`)
	if err != nil {
		return fmt.Errorf("feed_queue migrate: %w", err)
	}
	return nil
}

func (p *Postgres) RememberMessageID(ctx context.Context, msgid string) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO history (message_id) VALUES ($1)
		ON CONFLICT (message_id) DO NOTHING`, msgid)
	return err
}

func (p *Postgres) CancelMessageID(ctx context.Context, msgid string) (bool, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var artID int64
	err = tx.QueryRow(ctx, `SELECT id FROM articles WHERE message_id=$1`, msgid).Scan(&artID)
	if err == pgx.ErrNoRows {
		// Ensure history remembers the ID so it will not be reaccepted.
		if _, err := tx.Exec(ctx, `
			INSERT INTO history (message_id) VALUES ($1)
			ON CONFLICT (message_id) DO NOTHING`, msgid); err != nil {
			return false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return false, err
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}

	// Adjust group counts / high watermarks before deleting overview rows.
	rows, err := tx.Query(ctx, `
		SELECT group_id, article_num FROM overview WHERE article_id=$1`, artID)
	if err != nil {
		return false, err
	}
	type ov struct {
		gid int64
		num int64
	}
	var ovs []ov
	for rows.Next() {
		var o ov
		if err := rows.Scan(&o.gid, &o.num); err != nil {
			rows.Close()
			return false, err
		}
		ovs = append(ovs, o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return false, err
	}

	if _, err := tx.Exec(ctx, `DELETE FROM overview WHERE article_id=$1`, artID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM articles WHERE id=$1`, artID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO history (message_id) VALUES ($1)
		ON CONFLICT (message_id) DO NOTHING`, msgid); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM feed_queue WHERE message_id=$1`, msgid); err != nil {
		return false, err
	}

	for _, o := range ovs {
		if _, err := tx.Exec(ctx, `
			UPDATE newsgroups SET count = GREATEST(count - 1, 0)
			WHERE id=$1`, o.gid); err != nil {
			return false, err
		}
		// Recompute low from remaining overview (high stays for NNTP stability).
		if _, err := tx.Exec(ctx, `
			UPDATE newsgroups g SET low = COALESCE((
				SELECT MIN(o.article_num) FROM overview o WHERE o.group_id=g.id
			), g.high)
			WHERE g.id=$1`, o.gid); err != nil {
			return false, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (p *Postgres) DeleteGroup(ctx context.Context, name string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var gid int64
	err = tx.QueryRow(ctx, `SELECT id FROM newsgroups WHERE name=$1`, name).Scan(&gid)
	if err == pgx.ErrNoRows {
		return ErrNoGroup
	}
	if err != nil {
		return err
	}
	// Delete articles that only appear in this group.
	if _, err := tx.Exec(ctx, `
		DELETE FROM articles a
		WHERE a.id IN (SELECT article_id FROM overview WHERE group_id=$1)
		  AND NOT EXISTS (
			SELECT 1 FROM overview o WHERE o.article_id=a.id AND o.group_id<>$1
		  )`, gid); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM overview WHERE group_id=$1`, gid); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM subscriptions WHERE group_name=$1`, name); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM read_state WHERE group_name=$1`, name); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM newsgroups WHERE id=$1`, gid); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *Postgres) EnqueueFeed(ctx context.Context, peerID int64, msgid string) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO feed_queue (peer_id, message_id)
		VALUES ($1, $2)
		ON CONFLICT (peer_id, message_id) DO NOTHING`, peerID, msgid)
	return err
}

func (p *Postgres) ClaimFeedDue(ctx context.Context, limit int) ([]FeedQueueItem, error) {
	if limit <= 0 {
		limit = 32
	}
	rows, err := p.pool.Query(ctx, `
		UPDATE feed_queue SET attempts = attempts + 1, next_attempt = now() + interval '2 minutes'
		WHERE id IN (
			SELECT id FROM feed_queue
			WHERE next_attempt <= now()
			ORDER BY next_attempt, id
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, peer_id, message_id, attempts, next_attempt, last_error, created_at`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FeedQueueItem
	for rows.Next() {
		var it FeedQueueItem
		if err := rows.Scan(&it.ID, &it.PeerID, &it.MessageID, &it.Attempts, &it.NextAttempt, &it.LastError, &it.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (p *Postgres) CompleteFeed(ctx context.Context, id int64) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM feed_queue WHERE id=$1`, id)
	return err
}

func (p *Postgres) FailFeed(ctx context.Context, id int64, errMsg string, retryAfter time.Duration) error {
	if retryAfter <= 0 {
		retryAfter = 2 * time.Minute
	}
	_, err := p.pool.Exec(ctx, `
		UPDATE feed_queue SET last_error=$2, next_attempt=now() + $3::interval
		WHERE id=$1`, id, truncateErr(errMsg), fmt.Sprintf("%f seconds", retryAfter.Seconds()))
	return err
}

func (p *Postgres) FlushFeedQueue(ctx context.Context, peerID int64) (int, error) {
	var tag pgconn.CommandTag
	var err error
	if peerID > 0 {
		tag, err = p.pool.Exec(ctx, `DELETE FROM feed_queue WHERE peer_id=$1`, peerID)
	} else {
		tag, err = p.pool.Exec(ctx, `DELETE FROM feed_queue`)
	}
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func (p *Postgres) FeedQueueStats(ctx context.Context) (FeedQueueStats, error) {
	st := FeedQueueStats{ByPeer: map[int64]int{}}
	rows, err := p.pool.Query(ctx, `SELECT peer_id, COUNT(*) FROM feed_queue GROUP BY peer_id`)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	for rows.Next() {
		var pid int64
		var n int
		if err := rows.Scan(&pid, &n); err != nil {
			return st, err
		}
		st.ByPeer[pid] = n
		st.Depth += n
	}
	if err := rows.Err(); err != nil {
		return st, err
	}
	var oldest *time.Time
	err = p.pool.QueryRow(ctx, `SELECT MIN(created_at) FROM feed_queue`).Scan(&oldest)
	if err == nil && oldest != nil {
		st.OldestAge = time.Since(*oldest)
	}
	return st, nil
}

func truncateErr(s string) string {
	if len(s) > 500 {
		return s[:500]
	}
	return s
}
