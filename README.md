# OpenUsenetServer

A Usenet / NNTP server for people who do not want to configure [INN](https://www.isc.org/othersoftware/). One Go binary, PostgreSQL, per-newsgroup mbox archives, Docker Compose.

You do **not** need `inn.conf`, `incoming.conf`, `newsfeeds`, or `ctlinnd`.

## Status

Phase 1+: RFC 3977 **reader** on port 119 (POST, OVER, groups, live mbox spool) plus RFC 3977 **IHAVE** to DB-backed peers, **AUTHINFO USER/PASS**, an **admin portal**, and a web **newsreader** (subscriptions, threaded overview, compose/reply). Optional TLS for NNTP/HTTP. MFA and streaming feeds come later.

Live articles are kept indefinitely. Use [archive exports](docs/backup-and-archive.md) (`*.mbox.gz`) for Internet Archive / offsite snapshots.

## Quick start (Docker Compose)

```bash
cp .env.example .env
# set OPENUSENET_HOSTNAME to your FQDN when you have one
# optional first admin: OPENUSENET_BOOTSTRAP_ADMIN=admin:changeme
docker compose up --build -d
printf 'CAPABILITIES\r\nQUIT\r\n' | nc -q 2 127.0.0.1 119
# admin portal: http://127.0.0.1:8080/
```

Default seed group: `local.test`, plus the canonical ISC `active` / `newsgroups` files from https://ftp.isc.org/usenet/CONFIG/ (pulled on `openusenet migrate`).

## Auth

- Anonymous NNTP **read** is allowed.
- Once any user exists, **POST** requires `AUTHINFO USER` / `AUTHINFO PASS` with `can_post` or `admin`.
- Admin portal requires login. First admin: setup page, `openusenet user add --admin ...`, or `OPENUSENET_BOOTSTRAP_ADMIN=user:pass`.

## Quick start (local, no root)

Postgres must be reachable. Then:

```bash
cp config.example.yml config.yml
go run ./cmd/openusenet migrate --config config.yml
go run ./cmd/openusenet user add --admin --username admin --password secret --config config.yml
go run ./cmd/openusenet serve --config config.yml
```

## CLI

```text
openusenet serve|migrate|user|archive|healthcheck|version
```

Environment overrides include `OPENUSENET_HOSTNAME`, `OPENUSENET_LISTEN`, `OPENUSENET_HTTP`, `OPENUSENET_HTTP_TLS`, `OPENUSENET_NNTP_TLS`, `OPENUSENET_TLS_CERT`, `OPENUSENET_TLS_KEY`, `OPENUSENET_POSTGRES`, `OPENUSENET_MBOX_DIR`, `OPENUSENET_EXPORT_DIR`, `OPENUSENET_INBOUND_ALLOW`, `OPENUSENET_BOOTSTRAP_ADMIN`.

## Protocol (this release)

Implements the RFC 3977 READER, POST, LIST, OVER, HDR, and NEWNEWS bundles, plus `XOVER`/`XHDR` aliases. `MODE READER` is accepted as a no-op. `IHAVE` is advertised. `AUTHINFO USER` is advertised. Streaming `CHECK`/`TAKETHIS` is not advertised yet.

Inbound IHAVE can be limited with `inbound.allow` (hostnames / IPs / CIDRs). Empty allow list = all remotes.

## Layout

| Path | Role |
|------|------|
| `cmd/openusenet` | CLI |
| `internal/nntp` | RFC 3977 session |
| `internal/store` | PostgreSQL (and in-memory tests) |
| `internal/admin` | Admin portal |
| `internal/archive` | Live mbox spool + mbox.gz exports |
| `docs/backup-and-archive.md` | Backup / IA notes |
| `config.example.yml` | Local config |

License: GPL-3.0-or-later.
