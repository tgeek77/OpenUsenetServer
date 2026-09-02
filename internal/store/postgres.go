package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	wmat "github.com/openusenet/openusenet/internal/wildmat"
)

const schema = `
CREATE TABLE IF NOT EXISTS newsgroups (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    status CHAR(1) NOT NULL DEFAULT 'y',
    low BIGINT NOT NULL DEFAULT 0,
    high BIGINT NOT NULL DEFAULT 0,
    count BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS articles (
    id BIGSERIAL PRIMARY KEY,
    message_id TEXT NOT NULL UNIQUE,
    subject TEXT NOT NULL DEFAULT '',
    from_hdr TEXT NOT NULL DEFAULT '',
    date_hdr TEXT NOT NULL DEFAULT '',
    references_hdr TEXT NOT NULL DEFAULT '',
    bytes INT NOT NULL,
    lines INT NOT NULL,
    headers TEXT NOT NULL,
    body TEXT NOT NULL,
    stored_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    xref TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS overview (
    group_id BIGINT NOT NULL REFERENCES newsgroups(id) ON DELETE CASCADE,
    article_num BIGINT NOT NULL,
    article_id BIGINT NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    mbox_offset BIGINT,
    mbox_len BIGINT,
    PRIMARY KEY (group_id, article_num)
);

CREATE INDEX IF NOT EXISTS overview_article_idx ON overview(article_id);
CREATE INDEX IF NOT EXISTS articles_stored_idx ON articles(stored_at);

CREATE TABLE IF NOT EXISTS history (
    message_id TEXT PRIMARY KEY,
    arrived_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS users (
    id BIGSERIAL PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'user',
    can_post BOOLEAN NOT NULL DEFAULT true,
    disabled BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS peers (
    id BIGSERIAL PRIMARY KEY,
    host TEXT NOT NULL,
    port INT NOT NULL DEFAULT 119,
    enabled BOOLEAN NOT NULL DEFAULT true,
    notes TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (host, port)
);

CREATE TABLE IF NOT EXISTS subscriptions (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    group_name TEXT NOT NULL,
    subscribed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, group_name)
);

CREATE TABLE IF NOT EXISTS read_state (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    group_name TEXT NOT NULL,
    last_read_num BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, group_name)
);
`

type Postgres struct {
	pool *pgxpool.Pool
}

func OpenPostgres(ctx context.Context, url string) (*Postgres, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres ping: %w", err)
	}
	p := &Postgres{pool: pool}
	if _, err := pool.Exec(ctx, schema); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres schema: %w", err)
	}
	return p, nil
}

func (p *Postgres) Close() error {
	p.pool.Close()
	return nil
}

func (p *Postgres) EnsureGroup(ctx context.Context, name, desc, status string) error {
	if status == "" {
		status = "y"
	}
	_, err := p.pool.Exec(ctx, `
		INSERT INTO newsgroups (name, description, status)
		VALUES ($1, $2, $3)
		ON CONFLICT (name) DO UPDATE SET
			description = CASE WHEN EXCLUDED.description <> '' THEN EXCLUDED.description ELSE newsgroups.description END,
			status = EXCLUDED.status`, name, desc, status)
	return err
}

func (p *Postgres) EnsureGroups(ctx context.Context, groups []Group) error {
	const batch = 200
	for i := 0; i < len(groups); i += batch {
		end := i + batch
		if end > len(groups) {
			end = len(groups)
		}
		if err := p.ensureGroupBatch(ctx, groups[i:end]); err != nil {
			return err
		}
	}
	return nil
}

func (p *Postgres) ensureGroupBatch(ctx context.Context, groups []Group) error {
	if len(groups) == 0 {
		return nil
	}
	var b strings.Builder
	args := make([]any, 0, len(groups)*3)
	b.WriteString(`INSERT INTO newsgroups (name, description, status) VALUES `)
	for i, g := range groups {
		if i > 0 {
			b.WriteByte(',')
		}
		n := i * 3
		fmt.Fprintf(&b, "($%d,$%d,$%d)", n+1, n+2, n+3)
		status := g.Status
		if status == "" {
			status = "y"
		}
		args = append(args, g.Name, g.Description, status)
	}
	b.WriteString(` ON CONFLICT (name) DO UPDATE SET
		description = CASE WHEN EXCLUDED.description <> '' THEN EXCLUDED.description ELSE newsgroups.description END,
		status = EXCLUDED.status`)
	_, err := p.pool.Exec(ctx, b.String(), args...)
	return err
}

func (p *Postgres) CountGroups(ctx context.Context) (int, error) {
	var n int
	err := p.pool.QueryRow(ctx, `SELECT COUNT(*) FROM newsgroups`).Scan(&n)
	return n, err
}

func (p *Postgres) CountArticles(ctx context.Context) (int, error) {
	var n int
	err := p.pool.QueryRow(ctx, `SELECT COUNT(*) FROM articles`).Scan(&n)
	return n, err
}

func (p *Postgres) RecentArticles(ctx context.Context, limit int) ([]StoredArticle, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	rows, err := p.pool.Query(ctx, `
		SELECT message_id, subject, from_hdr, date_hdr, references_hdr, bytes, lines, stored_at, xref
		FROM articles ORDER BY stored_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StoredArticle
	for rows.Next() {
		var a StoredArticle
		if err := rows.Scan(&a.MessageID, &a.Subject, &a.From, &a.Date, &a.Refs, &a.Bytes, &a.Lines, &a.StoredAt, &a.Xref); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (p *Postgres) SearchGroups(ctx context.Context, query string, busyOnly bool, limit int) ([]Group, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	q := `SELECT name, description, status, low, high, count, created_at FROM newsgroups WHERE 1=1`
	args := []any{}
	if busyOnly {
		q += ` AND count > 0`
	}
	query = strings.TrimSpace(query)
	if query != "" {
		args = append(args, "%"+query+"%")
		q += fmt.Sprintf(` AND name ILIKE $%d`, len(args))
	}
	q += ` ORDER BY count DESC, name LIMIT ` + fmt.Sprintf("%d", limit)
	rows, err := p.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Group
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.Name, &g.Description, &g.Status, &g.Low, &g.High, &g.Count, &g.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (p *Postgres) ListGroups(ctx context.Context, wildmat string) ([]Group, error) {
	rows, err := p.pool.Query(ctx, `SELECT name, description, status, low, high, count, created_at FROM newsgroups ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Group
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.Name, &g.Description, &g.Status, &g.Low, &g.High, &g.Count, &g.CreatedAt); err != nil {
			return nil, err
		}
		if wildmat != "" && !wmat.Match(wildmat, g.Name) {
			continue
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (p *Postgres) GetGroup(ctx context.Context, name string) (*Group, error) {
	var g Group
	err := p.pool.QueryRow(ctx, `SELECT name, description, status, low, high, count, created_at FROM newsgroups WHERE name=$1`, name).
		Scan(&g.Name, &g.Description, &g.Status, &g.Low, &g.High, &g.Count, &g.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
}

func (p *Postgres) ArticleNumbers(ctx context.Context, group string, lo, hi int64) ([]int64, error) {
	q := `SELECT o.article_num FROM overview o JOIN newsgroups g ON g.id=o.group_id WHERE g.name=$1`
	args := []any{group}
	if lo > 0 {
		q += fmt.Sprintf(" AND o.article_num >= $%d", len(args)+1)
		args = append(args, lo)
	}
	if hi > 0 {
		q += fmt.Sprintf(" AND o.article_num <= $%d", len(args)+1)
		args = append(args, hi)
	}
	q += " ORDER BY o.article_num"
	rows, err := p.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var n int64
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (p *Postgres) GetByNumber(ctx context.Context, group string, num int64) (*StoredArticle, error) {
	a := &StoredArticle{}
	err := p.pool.QueryRow(ctx, `
		SELECT o.article_num, a.message_id, a.headers, a.body, a.bytes, a.lines, a.stored_at, a.xref,
		       a.subject, a.from_hdr, a.date_hdr, a.references_hdr
		FROM overview o
		JOIN newsgroups g ON g.id=o.group_id
		JOIN articles a ON a.id=o.article_id
		WHERE g.name=$1 AND o.article_num=$2`, group, num).
		Scan(&a.Num, &a.MessageID, &a.Headers, &a.Body, &a.Bytes, &a.Lines, &a.StoredAt, &a.Xref,
			&a.Subject, &a.From, &a.Date, &a.Refs)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (p *Postgres) GetByMsgID(ctx context.Context, msgid string) (*StoredArticle, error) {
	a := &StoredArticle{Num: 0}
	err := p.pool.QueryRow(ctx, `
		SELECT 0, a.message_id, a.headers, a.body, a.bytes, a.lines, a.stored_at, a.xref,
		       a.subject, a.from_hdr, a.date_hdr, a.references_hdr
		FROM articles a WHERE a.message_id=$1`, msgid).
		Scan(&a.Num, &a.MessageID, &a.Headers, &a.Body, &a.Bytes, &a.Lines, &a.StoredAt, &a.Xref,
			&a.Subject, &a.From, &a.Date, &a.Refs)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (p *Postgres) Overview(ctx context.Context, group string, lo, hi int64) ([]OverviewRow, error) {
	q := `
		SELECT o.article_num, a.subject, a.from_hdr, a.date_hdr, a.message_id, a.references_hdr, a.bytes, a.lines, a.xref
		FROM overview o
		JOIN newsgroups g ON g.id=o.group_id
		JOIN articles a ON a.id=o.article_id
		WHERE g.name=$1`
	args := []any{group}
	if lo > 0 {
		q += fmt.Sprintf(" AND o.article_num >= $%d", len(args)+1)
		args = append(args, lo)
	}
	if hi > 0 {
		q += fmt.Sprintf(" AND o.article_num <= $%d", len(args)+1)
		args = append(args, hi)
	}
	q += " ORDER BY o.article_num"
	rows, err := p.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OverviewRow
	for rows.Next() {
		var r OverviewRow
		if err := rows.Scan(&r.Num, &r.Subject, &r.From, &r.Date, &r.MsgID, &r.Refs, &r.Bytes, &r.Lines, &r.Xref); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (p *Postgres) Header(ctx context.Context, group string, lo, hi int64, header string) ([]OverviewRow, error) {
	rows, err := p.Overview(ctx, group, lo, hi)
	if err != nil {
		return nil, err
	}
	// Fetch headers for non-overview fields.
	for i := range rows {
		a, err := p.GetByNumber(ctx, group, rows[i].Num)
		if err != nil || a == nil {
			continue
		}
		rows[i].Header = pickHeader(header, a)
	}
	return rows, nil
}

func pickHeader(header string, a *StoredArticle) string {
	switch strings.ToLower(header) {
	case "subject":
		return a.Subject
	case "from":
		return a.From
	case "date":
		return a.Date
	case "message-id":
		return a.MessageID
	case "references":
		return a.Refs
	case ":bytes":
		return fmt.Sprintf("%d", a.Bytes)
	case ":lines":
		return fmt.Sprintf("%d", a.Lines)
	case "xref":
		return a.Xref
	default:
		for _, line := range strings.Split(a.Headers, "\r\n") {
			k, v, ok := strings.Cut(line, ":")
			if ok && strings.EqualFold(k, header) {
				return strings.TrimSpace(v)
			}
		}
		return ""
	}
}

func (p *Postgres) NewNews(ctx context.Context, wildmat string, since time.Time) ([]string, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT DISTINCT a.message_id
		FROM articles a
		JOIN overview o ON o.article_id=a.id
		JOIN newsgroups g ON g.id=o.group_id
		WHERE a.stored_at >= $1
		ORDER BY a.message_id`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	if wildmat == "" || wildmat == "*" {
		return out, rows.Err()
	}
	// Filter by group membership.
	var filtered []string
	for _, id := range out {
		var names []string
		r2, err := p.pool.Query(ctx, `
			SELECT g.name FROM overview o JOIN newsgroups g ON g.id=o.group_id
			JOIN articles a ON a.id=o.article_id WHERE a.message_id=$1`, id)
		if err != nil {
			return nil, err
		}
		for r2.Next() {
			var n string
			_ = r2.Scan(&n)
			names = append(names, n)
		}
		r2.Close()
		for _, n := range names {
			if wmat.Match(wildmat, n) {
				filtered = append(filtered, id)
				break
			}
		}
	}
	return filtered, nil
}

func (p *Postgres) NewGroups(ctx context.Context, since time.Time) ([]Group, error) {
	rows, err := p.pool.Query(ctx, `SELECT name, description, status, low, high, count, created_at FROM newsgroups WHERE created_at >= $1 ORDER BY name`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Group
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.Name, &g.Description, &g.Status, &g.Low, &g.High, &g.Count, &g.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (p *Postgres) HasMessageID(ctx context.Context, msgid string) (bool, error) {
	var n int
	err := p.pool.QueryRow(ctx, `SELECT 1 FROM history WHERE message_id=$1`, msgid).Scan(&n)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (p *Postgres) Post(ctx context.Context, headers, body, msgid, subject, from, date, refs, xrefHost string, bytes, lines int, groups []string) (*PostResult, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var dummy int
	err = tx.QueryRow(ctx, `SELECT 1 FROM history WHERE message_id=$1`, msgid).Scan(&dummy)
	if err == nil {
		return nil, ErrDuplicate
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	type ginfo struct {
		id  int64
		num int64
		name string
	}
	var used []ginfo
	for _, name := range groups {
		var id, high int64
		err := tx.QueryRow(ctx, `SELECT id, high FROM newsgroups WHERE name=$1 FOR UPDATE`, name).Scan(&id, &high)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		high++
		if _, err := tx.Exec(ctx, `UPDATE newsgroups SET high=$1, low=CASE WHEN low=0 THEN $1 ELSE low END, count=count+1 WHERE id=$2`, high, id); err != nil {
			return nil, err
		}
		used = append(used, ginfo{id: id, num: high, name: name})
	}
	if len(used) == 0 {
		return nil, ErrNoGroup
	}

	var xrefParts []string
	nums := map[string]int64{}
	for _, u := range used {
		xrefParts = append(xrefParts, fmt.Sprintf("%s:%d", u.name, u.num))
		nums[u.name] = u.num
	}
	xref := xrefHost + " " + strings.Join(xrefParts, " ")

	var artID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO articles (message_id, subject, from_hdr, date_hdr, references_hdr, bytes, lines, headers, body, xref)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`,
		msgid, subject, from, date, refs, bytes, lines, headers, body, xref).Scan(&artID)
	if err != nil {
		return nil, err
	}
	for _, u := range used {
		if _, err := tx.Exec(ctx, `INSERT INTO overview (group_id, article_num, article_id) VALUES ($1,$2,$3)`, u.id, u.num, artID); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO history (message_id) VALUES ($1)`, msgid); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &PostResult{MessageID: msgid, Xref: xref, Numbers: nums}, nil
}

func (p *Postgres) Next(ctx context.Context, group string, cur int64) (*StoredArticle, error) {
	a := &StoredArticle{}
	err := p.pool.QueryRow(ctx, `
		SELECT o.article_num, a.message_id, a.headers, a.body, a.bytes, a.lines, a.stored_at, a.xref,
		       a.subject, a.from_hdr, a.date_hdr, a.references_hdr
		FROM overview o JOIN newsgroups g ON g.id=o.group_id JOIN articles a ON a.id=o.article_id
		WHERE g.name=$1 AND o.article_num > $2
		ORDER BY o.article_num LIMIT 1`, group, cur).
		Scan(&a.Num, &a.MessageID, &a.Headers, &a.Body, &a.Bytes, &a.Lines, &a.StoredAt, &a.Xref,
			&a.Subject, &a.From, &a.Date, &a.Refs)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return a, err
}

func (p *Postgres) Prev(ctx context.Context, group string, cur int64) (*StoredArticle, error) {
	a := &StoredArticle{}
	err := p.pool.QueryRow(ctx, `
		SELECT o.article_num, a.message_id, a.headers, a.body, a.bytes, a.lines, a.stored_at, a.xref,
		       a.subject, a.from_hdr, a.date_hdr, a.references_hdr
		FROM overview o JOIN newsgroups g ON g.id=o.group_id JOIN articles a ON a.id=o.article_id
		WHERE g.name=$1 AND o.article_num < $2
		ORDER BY o.article_num DESC LIMIT 1`, group, cur).
		Scan(&a.Num, &a.MessageID, &a.Headers, &a.Body, &a.Bytes, &a.Lines, &a.StoredAt, &a.Xref,
			&a.Subject, &a.From, &a.Date, &a.Refs)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return a, err
}
