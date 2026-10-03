package store

import (
	"context"
	"errors"

	"openusenet/internal/moderate"
)

func (p *Postgres) migrateModerators(ctx context.Context) error {
	_, err := p.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS moderator_rules (
			id BIGSERIAL PRIMARY KEY,
			pattern TEXT NOT NULL UNIQUE,
			address TEXT NOT NULL
		)`)
	if err != nil {
		return errors.New("moderator migrate: " + err.Error())
	}
	return nil
}

func (p *Postgres) ListModeratorRules(ctx context.Context) ([]ModeratorRule, error) {
	rows, err := p.pool.Query(ctx, `SELECT pattern, address FROM moderator_rules ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ModeratorRule
	for rows.Next() {
		var r ModeratorRule
		if err := rows.Scan(&r.Pattern, &r.Address); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (p *Postgres) AddModeratorRule(ctx context.Context, pattern, address string) error {
	pattern, address, err := moderate.NormalizeRule(pattern, address)
	if err != nil {
		return err
	}
	_, err = p.pool.Exec(ctx, `
		INSERT INTO moderator_rules (pattern, address) VALUES ($1, $2)
		ON CONFLICT (pattern) DO UPDATE SET address = EXCLUDED.address`, pattern, address)
	return err
}

func (p *Postgres) DeleteModeratorRule(ctx context.Context, pattern string) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM moderator_rules WHERE pattern=$1`, pattern)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("moderator rule not found")
	}
	return nil
}
