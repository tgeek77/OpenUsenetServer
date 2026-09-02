package store

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (p *Postgres) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := p.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (p *Postgres) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, username, password_hash, role, can_post, disabled, created_at
		FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.CanPost, &u.Disabled, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (p *Postgres) GetUser(ctx context.Context, username string) (*User, error) {
	var u User
	err := p.pool.QueryRow(ctx, `
		SELECT id, username, password_hash, role, can_post, disabled, created_at
		FROM users WHERE username = $1`, strings.TrimSpace(username)).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.CanPost, &u.Disabled, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (p *Postgres) CreateUser(ctx context.Context, u User) (*User, error) {
	u.Username = strings.TrimSpace(u.Username)
	if u.Username == "" || u.PasswordHash == "" {
		return nil, errors.New("username and password required")
	}
	if u.Role == "" {
		u.Role = RoleUser
	}
	err := p.pool.QueryRow(ctx, `
		INSERT INTO users (username, password_hash, role, can_post, disabled)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at`,
		u.Username, u.PasswordHash, u.Role, u.CanPost, u.Disabled).
		Scan(&u.ID, &u.CreatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate") {
			return nil, ErrUserExists
		}
		return nil, err
	}
	return &u, nil
}

func (p *Postgres) UpdateUser(ctx context.Context, username string, role string, canPost, disabled *bool, passwordHash string) error {
	u, err := p.GetUser(ctx, username)
	if err != nil {
		return err
	}
	if u == nil {
		return ErrUserNotFound
	}
	if role != "" {
		u.Role = role
	}
	if canPost != nil {
		u.CanPost = *canPost
	}
	if disabled != nil {
		u.Disabled = *disabled
	}
	if passwordHash != "" {
		u.PasswordHash = passwordHash
	}
	_, err = p.pool.Exec(ctx, `
		UPDATE users SET role=$2, can_post=$3, disabled=$4, password_hash=$5 WHERE username=$1`,
		u.Username, u.Role, u.CanPost, u.Disabled, u.PasswordHash)
	return err
}

func (p *Postgres) DeleteUser(ctx context.Context, username string) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM users WHERE username=$1`, strings.TrimSpace(username))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (p *Postgres) CountPeers(ctx context.Context) (int, error) {
	var n int
	err := p.pool.QueryRow(ctx, `SELECT COUNT(*) FROM peers`).Scan(&n)
	return n, err
}

func (p *Postgres) ListPeers(ctx context.Context) ([]Peer, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, host, port, enabled, notes, created_at FROM peers ORDER BY host, port`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPeers(rows)
}

func (p *Postgres) ListEnabledPeers(ctx context.Context) ([]Peer, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, host, port, enabled, notes, created_at FROM peers WHERE enabled ORDER BY host, port`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPeers(rows)
}

func scanPeers(rows pgx.Rows) ([]Peer, error) {
	var out []Peer
	for rows.Next() {
		var peer Peer
		if err := rows.Scan(&peer.ID, &peer.Host, &peer.Port, &peer.Enabled, &peer.Notes, &peer.Created); err != nil {
			return nil, err
		}
		out = append(out, peer)
	}
	return out, rows.Err()
}

func (p *Postgres) GetPeer(ctx context.Context, id int64) (*Peer, error) {
	var peer Peer
	err := p.pool.QueryRow(ctx, `
		SELECT id, host, port, enabled, notes, created_at FROM peers WHERE id=$1`, id).
		Scan(&peer.ID, &peer.Host, &peer.Port, &peer.Enabled, &peer.Notes, &peer.Created)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &peer, nil
}

func (p *Postgres) CreatePeer(ctx context.Context, peer Peer) (*Peer, error) {
	peer.Host = strings.TrimSpace(peer.Host)
	if peer.Host == "" {
		return nil, errors.New("host required")
	}
	if peer.Port <= 0 {
		peer.Port = 119
	}
	err := p.pool.QueryRow(ctx, `
		INSERT INTO peers (host, port, enabled, notes)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at`,
		peer.Host, peer.Port, peer.Enabled, peer.Notes).
		Scan(&peer.ID, &peer.Created)
	if err != nil {
		return nil, err
	}
	return &peer, nil
}

func (p *Postgres) UpdatePeer(ctx context.Context, id int64, host string, port int, enabled *bool, notes *string) (*Peer, error) {
	peer, err := p.GetPeer(ctx, id)
	if err != nil {
		return nil, err
	}
	if peer == nil {
		return nil, ErrPeerNotFound
	}
	if host = strings.TrimSpace(host); host != "" {
		peer.Host = host
	}
	if port > 0 {
		peer.Port = port
	}
	if enabled != nil {
		peer.Enabled = *enabled
	}
	if notes != nil {
		peer.Notes = *notes
	}
	_, err = p.pool.Exec(ctx, `
		UPDATE peers SET host=$2, port=$3, enabled=$4, notes=$5 WHERE id=$1`,
		id, peer.Host, peer.Port, peer.Enabled, peer.Notes)
	if err != nil {
		return nil, err
	}
	return peer, nil
}

func (p *Postgres) DeletePeer(ctx context.Context, id int64) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM peers WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrPeerNotFound
	}
	return nil
}

func (p *Postgres) ArticlesForGroup(ctx context.Context, group string) ([]StoredArticle, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT a.message_id, a.subject, a.from_hdr, a.date_hdr, a.references_hdr,
		       a.bytes, a.lines, a.headers, a.body, a.stored_at, a.xref, o.article_num
		FROM overview o
		JOIN newsgroups g ON g.id = o.group_id
		JOIN articles a ON a.id = o.article_id
		WHERE g.name = $1
		ORDER BY o.article_num`, group)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StoredArticle
	for rows.Next() {
		var a StoredArticle
		if err := rows.Scan(&a.MessageID, &a.Subject, &a.From, &a.Date, &a.Refs,
			&a.Bytes, &a.Lines, &a.Headers, &a.Body, &a.StoredAt, &a.Xref, &a.Num); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
