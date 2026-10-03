package store

import (
	"context"
)

func (p *Postgres) PurgeUnwantedArticles(ctx context.Context) (PurgeResult, error) {
	var res PurgeResult
	if _, err := p.pool.Exec(ctx, `
		UPDATE newsgroups SET status='n', retention_days=NULL, retention_mode=$1
		WHERE name IN (SELECT group_name FROM group_alerts WHERE status=$2)`,
		RetentionModeBlocked, AlertBlocked); err != nil {
		return res, err
	}
	patterns, err := p.ListGroupBans(ctx)
	if err != nil {
		return res, err
	}
	groups, err := p.ListGroups(ctx, "")
	if err != nil {
		return res, err
	}
	var names []string
	for _, g := range groups {
		if g.RetentionMode == RetentionModeBlocked || groupMatches(patterns, g.Name) {
			names = append(names, g.Name)
		}
	}
	if len(names) == 0 {
		return res, nil
	}
	res.Groups = names

	rows, err := p.pool.Query(ctx, `
		SELECT DISTINCT a.id, a.message_id
		FROM articles a
		JOIN overview o ON o.article_id = a.id
		JOIN newsgroups g ON g.id = o.group_id
		WHERE g.name = ANY($1)`, names)
	if err != nil {
		return res, err
	}
	defer rows.Close()
	var artIDs []int64
	for rows.Next() {
		var id int64
		var msgid string
		if err := rows.Scan(&id, &msgid); err != nil {
			return res, err
		}
		artIDs = append(artIDs, id)
		res.MessageIDs = append(res.MessageIDs, msgid)
	}
	if err := rows.Err(); err != nil {
		return res, err
	}
	if len(artIDs) == 0 {
		return res, nil
	}

	sibs, err := p.pool.Query(ctx, `
		SELECT DISTINCT g.name
		FROM overview o
		JOIN newsgroups g ON g.id = o.group_id
		WHERE o.article_id = ANY($1)
		  AND NOT (g.name = ANY($2))`, artIDs, names)
	if err != nil {
		return res, err
	}
	defer sibs.Close()
	affected := append([]string{}, names...)
	for sibs.Next() {
		var name string
		if err := sibs.Scan(&name); err != nil {
			return res, err
		}
		res.CrosspostGroups = append(res.CrosspostGroups, name)
		affected = append(affected, name)
	}
	if err := sibs.Err(); err != nil {
		return res, err
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return res, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `DELETE FROM feed_queue WHERE message_id = ANY($1)`, res.MessageIDs); err != nil {
		return res, err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM articles WHERE id = ANY($1)`, artIDs)
	if err != nil {
		return res, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE newsgroups g SET
			count = (SELECT COUNT(*) FROM overview o WHERE o.group_id = g.id),
			low = COALESCE((SELECT MIN(o.article_num) FROM overview o WHERE o.group_id = g.id), g.high)
		WHERE g.name = ANY($1)`, affected); err != nil {
		return res, err
	}
	if err := tx.Commit(ctx); err != nil {
		return res, err
	}
	res.Articles = int(tag.RowsAffected())
	return res, nil
}
