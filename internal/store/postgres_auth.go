package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

const userCols = `id, username, password_hash, role, can_post, disabled, display_name, email, must_change_password, session_gen, created_at`

func scanUser(sc interface{ Scan(dest ...any) error }) (User, error) {
	var u User
	err := sc.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.CanPost, &u.Disabled,
		&u.DisplayName, &u.Email, &u.MustChangePassword, &u.SessionGen, &u.CreatedAt)
	return u, err
}

func (p *Postgres) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := p.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (p *Postgres) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := p.pool.Query(ctx, `SELECT `+userCols+` FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (p *Postgres) GetUser(ctx context.Context, username string) (*User, error) {
	u, err := scanUser(p.pool.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE username = $1`, strings.TrimSpace(username)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (p *Postgres) GetUserByID(ctx context.Context, id int64) (*User, error) {
	u, err := scanUser(p.pool.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE id = $1`, id))
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
		INSERT INTO users (username, password_hash, role, can_post, disabled, display_name, email, must_change_password)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, session_gen, created_at`,
		u.Username, u.PasswordHash, u.Role, u.CanPost, u.Disabled,
		strings.TrimSpace(u.DisplayName), strings.TrimSpace(u.Email), u.MustChangePassword).
		Scan(&u.ID, &u.SessionGen, &u.CreatedAt)
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
	wasDisabled := u.Disabled
	if disabled != nil {
		u.Disabled = *disabled
	}
	if passwordHash != "" {
		u.PasswordHash = passwordHash
		u.MustChangePassword = true
	}
	bump := 0
	if passwordHash != "" {
		bump++
	}
	if disabled != nil && *disabled && !wasDisabled {
		bump++
	}
	_, err = p.pool.Exec(ctx, `
		UPDATE users SET role=$2, can_post=$3, disabled=$4, password_hash=$5,
			must_change_password=$6, session_gen=session_gen+$7
		WHERE username=$1`,
		u.Username, u.Role, u.CanPost, u.Disabled, u.PasswordHash, u.MustChangePassword, bump)
	return err
}

func (p *Postgres) UpdateProfile(ctx context.Context, username, displayName, email string) error {
	tag, err := p.pool.Exec(ctx, `
		UPDATE users SET display_name=$2, email=$3 WHERE username=$1`,
		strings.TrimSpace(username), strings.TrimSpace(displayName), strings.TrimSpace(email))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (p *Postgres) ChangePassword(ctx context.Context, username, passwordHash string) (*User, error) {
	if passwordHash == "" {
		return nil, errors.New("password required")
	}
	u, err := scanUser(p.pool.QueryRow(ctx, `
		UPDATE users SET password_hash=$2, must_change_password=false, session_gen=session_gen+1
		WHERE username=$1
		RETURNING `+userCols,
		strings.TrimSpace(username), passwordHash))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (p *Postgres) GetOrCreateSecret(ctx context.Context, name string) (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	var out string
	err := p.pool.QueryRow(ctx, `
		INSERT INTO server_secrets (name, value) VALUES ($1, $2)
		ON CONFLICT (name) DO UPDATE SET value = server_secrets.value
		RETURNING value`, strings.TrimSpace(name), hex.EncodeToString(b[:])).Scan(&out)
	return out, err
}

func (p *Postgres) ListGroupBans(ctx context.Context) ([]string, error) {
	rows, err := p.pool.Query(ctx, `SELECT pattern FROM group_bans ORDER BY pattern`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var ptn string
		if err := rows.Scan(&ptn); err != nil {
			return nil, err
		}
		out = append(out, ptn)
	}
	return out, rows.Err()
}

func (p *Postgres) AddGroupBan(ctx context.Context, pattern string) error {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" || strings.ContainsAny(pattern, " \t\r\n") {
		return errors.New("invalid pattern")
	}
	_, err := p.pool.Exec(ctx, `INSERT INTO group_bans (pattern) VALUES ($1) ON CONFLICT DO NOTHING`, pattern)
	return err
}

func (p *Postgres) DeleteGroupBan(ctx context.Context, pattern string) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM group_bans WHERE pattern=$1`, strings.TrimSpace(pattern))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("ban not found")
	}
	return nil
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
		SELECT id, name, path_token, incoming_host, host, port, patterns, distributions, flags,
		       enabled, incoming_password, outgoing_password, notes, created_at
		FROM peers ORDER BY host, port`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPeers(rows)
}

func (p *Postgres) ListEnabledPeers(ctx context.Context) ([]Peer, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, name, path_token, incoming_host, host, port, patterns, distributions, flags,
		       enabled, incoming_password, outgoing_password, notes, created_at
		FROM peers WHERE enabled ORDER BY host, port`)
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
		if err := rows.Scan(&peer.ID, &peer.Name, &peer.PathToken, &peer.IncomingHost, &peer.Host, &peer.Port,
			&peer.Patterns, &peer.Distributions, &peer.Flags, &peer.Enabled,
			&peer.IncomingPassword, &peer.OutgoingPassword, &peer.Notes, &peer.Created); err != nil {
			return nil, err
		}
		out = append(out, peer)
	}
	return out, rows.Err()
}

func (p *Postgres) GetPeer(ctx context.Context, id int64) (*Peer, error) {
	var peer Peer
	err := p.pool.QueryRow(ctx, `
		SELECT id, name, path_token, incoming_host, host, port, patterns, distributions, flags,
		       enabled, incoming_password, outgoing_password, notes, created_at
		FROM peers WHERE id=$1`, id).
		Scan(&peer.ID, &peer.Name, &peer.PathToken, &peer.IncomingHost, &peer.Host, &peer.Port,
			&peer.Patterns, &peer.Distributions, &peer.Flags, &peer.Enabled,
			&peer.IncomingPassword, &peer.OutgoingPassword, &peer.Notes, &peer.Created)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &peer, nil
}

func (p *Postgres) CreatePeer(ctx context.Context, peer Peer) (*Peer, error) {
	peer = peer.Normalize()
	if peer.Host == "" {
		return nil, errors.New("host required")
	}
	err := p.pool.QueryRow(ctx, `
		INSERT INTO peers (name, path_token, incoming_host, host, port, patterns, distributions, flags,
			enabled, incoming_password, outgoing_password, notes)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		RETURNING id, created_at`,
		peer.Name, peer.PathToken, peer.IncomingHost, peer.Host, peer.Port,
		peer.Patterns, peer.Distributions, peer.Flags, peer.Enabled,
		peer.IncomingPassword, peer.OutgoingPassword, peer.Notes).
		Scan(&peer.ID, &peer.Created)
	if err != nil {
		return nil, err
	}
	return &peer, nil
}

func (p *Postgres) UpdatePeer(ctx context.Context, peer Peer) (*Peer, error) {
	if peer.ID <= 0 {
		return nil, errors.New("peer id required")
	}
	peer = peer.Normalize()
	if peer.Host == "" {
		return nil, errors.New("host required")
	}
	tag, err := p.pool.Exec(ctx, `
		UPDATE peers SET name=$2, path_token=$3, incoming_host=$4, host=$5, port=$6,
			patterns=$7, distributions=$8, flags=$9, enabled=$10,
			incoming_password=$11, outgoing_password=$12, notes=$13
		WHERE id=$1`,
		peer.ID, peer.Name, peer.PathToken, peer.IncomingHost, peer.Host, peer.Port,
		peer.Patterns, peer.Distributions, peer.Flags, peer.Enabled,
		peer.IncomingPassword, peer.OutgoingPassword, peer.Notes)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrPeerNotFound
	}
	return &peer, nil
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
