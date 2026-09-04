package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func (p *Postgres) migrateSearch(ctx context.Context) error {
	// Generated column keeps FTS in sync on insert/update (POST, IHAVE, import).
	_, err := p.pool.Exec(ctx, `
ALTER TABLE articles ADD COLUMN IF NOT EXISTS search_tsv tsvector
  GENERATED ALWAYS AS (
    setweight(to_tsvector('english', coalesce(subject, '')), 'A') ||
    setweight(to_tsvector('english', coalesce(from_hdr, '')), 'B') ||
    setweight(to_tsvector('english', coalesce(body, '')), 'C')
  ) STORED`)
	if err != nil {
		return fmt.Errorf("search_tsv column: %w", err)
	}

	// CONCURRENTLY cannot run inside a transaction; pool.Exec uses autocommit.
	_, err = p.pool.Exec(ctx, `
CREATE INDEX CONCURRENTLY IF NOT EXISTS articles_search_tsv_idx
  ON articles USING GIN (search_tsv)`)
	if err != nil {
		// Some environments reject CONCURRENTLY; fall back to blocking create.
		_, err = p.pool.Exec(ctx, `
CREATE INDEX IF NOT EXISTS articles_search_tsv_idx
  ON articles USING GIN (search_tsv)`)
		if err != nil {
			return fmt.Errorf("search_tsv index: %w", err)
		}
	}
	return nil
}

// ArticleSearchHit is one full-text search result.
type ArticleSearchHit struct {
	MessageID string   `json:"message_id"`
	Subject   string   `json:"subject"`
	From      string   `json:"from"`
	Date      string   `json:"date"`
	StoredAt  string   `json:"stored_at"`
	Xref      string   `json:"xref"`
	Groups    []string `json:"groups,omitempty"`
	Group     string   `json:"group,omitempty"` // primary group for opening
	Num       int64    `json:"num,omitempty"`
	Rank      float64  `json:"rank"`
	Snippet   string   `json:"snippet,omitempty"`
}

func (p *Postgres) SearchArticles(ctx context.Context, query, group string, limit, offset int) ([]ArticleSearchHit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	group = strings.TrimSpace(group)

	rows, err := p.pool.Query(ctx, `
		SELECT a.message_id, a.subject, a.from_hdr, a.date_hdr, a.stored_at, a.xref,
		       ts_rank_cd(a.search_tsv, q) AS rank,
		       ts_headline('english', left(a.body, 8000), q, 'MaxWords=35, MinWords=15') AS snippet,
		       COALESCE((
		         SELECT g.name FROM overview o
		         JOIN newsgroups g ON g.id = o.group_id
		         WHERE o.article_id = a.id
		         ORDER BY CASE WHEN $2 <> '' AND g.name = $2 THEN 0 ELSE 1 END, g.name
		         LIMIT 1
		       ), ''),
		       COALESCE((
		         SELECT o.article_num FROM overview o
		         JOIN newsgroups g ON g.id = o.group_id
		         WHERE o.article_id = a.id
		         ORDER BY CASE WHEN $2 <> '' AND g.name = $2 THEN 0 ELSE 1 END, g.name
		         LIMIT 1
		       ), 0),
		       COALESCE((
		         SELECT array_agg(g.name ORDER BY g.name)
		         FROM overview o JOIN newsgroups g ON g.id = o.group_id
		         WHERE o.article_id = a.id
		       ), ARRAY[]::text[])
		FROM articles a, websearch_to_tsquery('english', $1) q
		WHERE a.search_tsv @@ q
		  AND ($2 = '' OR EXISTS (
		    SELECT 1 FROM overview o JOIN newsgroups g ON g.id = o.group_id
		    WHERE o.article_id = a.id AND g.name = $2
		  ))
		ORDER BY rank DESC, a.stored_at DESC
		LIMIT $3 OFFSET $4`, query, group, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ArticleSearchHit
	for rows.Next() {
		var h ArticleSearchHit
		var stored time.Time
		if err := rows.Scan(&h.MessageID, &h.Subject, &h.From, &h.Date, &stored, &h.Xref,
			&h.Rank, &h.Snippet, &h.Group, &h.Num, &h.Groups); err != nil {
			return nil, err
		}
		h.StoredAt = stored.UTC().Format(time.RFC3339)
		if h.Groups == nil {
			h.Groups = []string{}
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
