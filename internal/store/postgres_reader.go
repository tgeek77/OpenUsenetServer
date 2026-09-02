package store

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (p *Postgres) ListSubscriptions(ctx context.Context, userID int64) ([]Subscription, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT s.group_name, s.subscribed_at,
		       COALESCE(g.description, ''), COALESCE(g.status, 'y'),
		       COALESCE(g.low, 0), COALESCE(g.high, 0), COALESCE(g.count, 0),
		       COALESCE(r.last_read_num, 0)
		FROM subscriptions s
		LEFT JOIN newsgroups g ON g.name = s.group_name
		LEFT JOIN read_state r ON r.user_id = s.user_id AND r.group_name = s.group_name
		WHERE s.user_id = $1
		ORDER BY s.group_name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Subscription
	for rows.Next() {
		var s Subscription
		if err := rows.Scan(&s.GroupName, &s.SubscribedAt, &s.Description, &s.Status,
			&s.Low, &s.High, &s.Count, &s.LastReadNum); err != nil {
			return nil, err
		}
		if s.High > s.LastReadNum {
			s.Unread = s.High - s.LastReadNum
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (p *Postgres) Subscribe(ctx context.Context, userID int64, group string) error {
	group = strings.TrimSpace(group)
	if group == "" {
		return errors.New("group required")
	}
	g, err := p.GetGroup(ctx, group)
	if err != nil {
		return err
	}
	if g == nil {
		return ErrNoGroup
	}
	_, err = p.pool.Exec(ctx, `
		INSERT INTO subscriptions (user_id, group_name) VALUES ($1, $2)
		ON CONFLICT DO NOTHING`, userID, group)
	return err
}

func (p *Postgres) Unsubscribe(ctx context.Context, userID int64, group string) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM subscriptions WHERE user_id=$1 AND group_name=$2`,
		userID, strings.TrimSpace(group))
	return err
}

func (p *Postgres) GetReadState(ctx context.Context, userID int64, group string) (int64, error) {
	var n int64
	err := p.pool.QueryRow(ctx, `
		SELECT last_read_num FROM read_state WHERE user_id=$1 AND group_name=$2`,
		userID, strings.TrimSpace(group)).Scan(&n)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return n, err
}

func (p *Postgres) SetReadState(ctx context.Context, userID int64, group string, lastReadNum int64) error {
	if lastReadNum < 0 {
		lastReadNum = 0
	}
	_, err := p.pool.Exec(ctx, `
		INSERT INTO read_state (user_id, group_name, last_read_num)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, group_name) DO UPDATE SET
			last_read_num = GREATEST(read_state.last_read_num, EXCLUDED.last_read_num)`,
		userID, strings.TrimSpace(group), lastReadNum)
	return err
}
