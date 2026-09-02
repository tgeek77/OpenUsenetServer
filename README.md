# OpenUsenetServer

A Usenet / NNTP server for people who do not want to configure [INN](https://www.isc.org/othersoftware/). One Go binary, PostgreSQL, per-newsgroup mbox archives, Docker Compose.

You do **not** need `inn.conf`, `incoming.conf`, `newsfeeds`, or `ctlinnd`.

## Status

Phase 1: RFC 3977 **reader** on port 119 (POST, OVER, groups, mbox archive) plus RFC 3977 **IHAVE** transfer to YAML `peers`, and an unauthenticated **admin portal** on port 8080. TLS, AUTH, INN peer-snippet import/export, streaming feeds, plugins, and Tor come later.

## Quick start (Docker Compose)

```bash
cp .env.example .env
# set OPENUSENET_HOSTNAME to your FQDN when you have one
docker compose up --build -d
printf 'CAPABILITIES\r\nQUIT\r\n' | nc -q 2 127.0.0.1 119
# admin portal: http://127.0.0.1:8080/  (no authentication)
```

Default seed group: `local.test`, plus the canonical ISC `active` / `newsgroups` files from https://ftp.isc.org/usenet/CONFIG/ (pulled on `openusenet migrate`).

## Quick start (local, no root)

Postgres must be reachable. Then:

```bash
cp config.example.yml config.yml
go run ./cmd/openusenet migrate --config config.yml
go run ./cmd/openusenet serve --config config.yml
# listens on :1119 by default in the example config
```

```bash
printf 'CAPABILITIES\r\nQUIT\r\n' | nc 127.0.0.1 1119
```

## CLI

```text
openusenet --help
openusenet serve --help
openusenet migrate --help
openusenet healthcheck --help
```

Examples:

```bash
openusenet serve --config config.yml
openusenet serve --listen :1119 --postgres postgres://openusenet:openusenet@127.0.0.1:5432/openusenet?sslmode=disable
openusenet migrate --config config.yml
openusenet healthcheck --addr 127.0.0.1:119
```

Environment variables override the config file: `OPENUSENET_HOSTNAME`, `OPENUSENET_ORGANIZATION`, `OPENUSENET_LISTEN`, `OPENUSENET_HTTP`, `OPENUSENET_POSTGRES`, `OPENUSENET_MBOX_DIR`. The admin portal is unauthenticated; bind it to localhost or disable with `OPENUSENET_HTTP=-` if the host is reachable from untrusted networks.

## Protocol (this release)

Implements the RFC 3977 READER, POST, LIST, OVER, HDR, and NEWNEWS bundles, plus `XOVER`/`XHDR` aliases. `MODE READER` is accepted as a no-op. `IHAVE` is advertised and used to push newly accepted articles to `peers:` in the YAML config. Streaming `CHECK`/`TAKETHIS` is not advertised yet.

## Layout

| Path | Role |
|------|------|
| `cmd/openusenet` | CLI |
| `internal/nntp` | RFC 3977 session |
| `internal/store` | PostgreSQL (and in-memory tests) |
| `internal/article` | RFC 5536 parse / POST injection |
| `internal/archive` | mboxrd per newsgroup |
| `config.example.yml` | Local config |

License: GPL-3.0-or-later.
