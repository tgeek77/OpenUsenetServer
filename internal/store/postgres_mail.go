package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"openusenet/internal/mail"
)

func (p *Postgres) migrateMail(ctx context.Context) error {
	_, err := p.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS mail_settings (
			id INT PRIMARY KEY CHECK (id = 1),
			host TEXT NOT NULL DEFAULT '',
			port INT NOT NULL DEFAULT 25,
			username TEXT NOT NULL DEFAULT '',
			password TEXT NOT NULL DEFAULT '',
			from_addr TEXT NOT NULL DEFAULT '',
			security TEXT NOT NULL DEFAULT 'auto',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`)
	if err != nil {
		return errors.New("mail migrate: " + err.Error())
	}
	return nil
}

func (p *Postgres) GetMailSettings(ctx context.Context) (mail.Settings, bool, error) {
	var s mail.Settings
	err := p.pool.QueryRow(ctx, `
		SELECT host, port, username, password, from_addr, security
		FROM mail_settings WHERE id=1`).Scan(
		&s.Host, &s.Port, &s.Username, &s.Password, &s.From, &s.Security)
	if errors.Is(err, pgx.ErrNoRows) {
		return mail.Settings{}, false, nil
	}
	if err != nil {
		return mail.Settings{}, false, err
	}
	return s, true, nil
}

func (p *Postgres) SaveMailSettings(ctx context.Context, s mail.Settings) error {
	s = s.Normalize()
	if err := s.Validate(); err != nil {
		return err
	}
	_, err := p.pool.Exec(ctx, `
		INSERT INTO mail_settings (id, host, port, username, password, from_addr, security, updated_at)
		VALUES (1, $1, $2, $3, $4, $5, $6, now())
		ON CONFLICT (id) DO UPDATE SET
			host = EXCLUDED.host,
			port = EXCLUDED.port,
			username = EXCLUDED.username,
			password = EXCLUDED.password,
			from_addr = EXCLUDED.from_addr,
			security = EXCLUDED.security,
			updated_at = now()`,
		s.Host, s.Port, s.Username, s.Password, s.From, s.Security)
	return err
}
