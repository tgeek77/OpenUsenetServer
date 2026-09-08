# OpenUsenetServer

A Usenet / NNTP server for people who do not want to configure [INN](https://www.isc.org/othersoftware/). One Go binary, PostgreSQL, per-newsgroup mbox archives, Docker Compose.

You do **not** need `inn.conf`, `incoming.conf`, `newsfeeds`, or `ctlinnd`.

## Status

Phase 1+: RFC 3977 **reader** on port 119 (POST, OVER, groups, live mbox spool) plus RFC 3977 **IHAVE** to DB-backed peers, **AUTHINFO USER/PASS**, an **admin portal** (Reader / Stats / Ops / Admin), and a web **newsreader**. Every logged-in user gets **Reader** and content **Stats** (top groups, posters, Path providers); Ops and Admin stay admin-only. Peer wildmat filtering, durable outbound feed queue, cancels, **hierarchy control messages** (`newgroup` / `rmgroup` / `checkgroups` into `control.*`, overriding ISC), art-cutoff, history remember-on-reject, and innwatch-style pause/throttle are included. Optional TLS for NNTP/HTTP. Text articles are kept indefinitely; groups flooded with binary traffic get a short retention quota and an admin review alert. Authenticated users are limited to 25 binary POSTs/day. MFA comes later.

Live text articles are kept indefinitely. Use [archive exports](docs/backup-and-archive.md) (`*.mbox.gz`) for Internet Archive / offsite snapshots, or `openusenet archive import` to load historical mbox dumps.

## Quick start (Docker Compose)

```bash
cp .env.example .env
# set OPENUSENET_HOSTNAME, ADMIN_DOMAIN, ACME_EMAIL for a public host
# optional first admin: OPENUSENET_BOOTSTRAP_ADMIN=admin:changeme
docker compose up -d
printf 'CAPABILITIES\r\nQUIT\r\n' | nc -q 2 127.0.0.1 119
# admin (local):  http://127.0.0.1:8080/
# admin (public): https://$ADMIN_DOMAIN/   # Caddy + Let's Encrypt; see docs/tls.md
```

Default seed group: `local.test`, plus the canonical ISC `active` / `newsgroups` files from https://ftp.isc.org/usenet/CONFIG/ (pulled on `openusenet migrate`).

Caddy terminates HTTPS for the admin portal on ports 80/443. Port 8080 is bound to localhost only. NNTPS (563) is not enabled in Compose yet; cleartext NNTP on 119 is what peers use today.

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

Implements the RFC 3977 READER, POST, LIST, OVER, HDR, and NEWNEWS bundles, plus `XOVER`/`XHDR` aliases. `MODE READER` is accepted as a no-op. `IHAVE` and RFC 4644 streaming (`STREAMING`, `CHECK`, `TAKETHIS`, `MODE STREAM`) are advertised. `AUTHINFO USER` is advertised. The web newsreader includes PostgreSQL full-text search over subject/from/body.

Inbound IHAVE/CHECK/TAKETHIS can be limited with `inbound.allow` (hostnames / IPs / CIDRs). Empty allow list = all remotes. Outbound feeds prefer CHECK/TAKETHIS when the peer advertises `STREAMING`, otherwise IHAVE. Peer `newsfeeds` patterns, distributions, and flags (`Ap`, `<size`, `C`/`G`/`U`/`H`, etc.) are enforced when offering articles.

## Layout

| Path | Role |
|------|------|
| `cmd/openusenet` | CLI |
| `internal/nntp` | RFC 3977 session |
| `internal/store` | PostgreSQL (and in-memory tests) |
| `internal/admin` | Admin portal |
| `internal/archive` | Live mbox spool + mbox.gz export/import |
| `docs/backup-and-archive.md` | Backup / IA notes |
| `docs/tls.md` | Caddy / Let's Encrypt admin HTTPS; NNTPS later |
| `config.example.yml` | Local config |
| `docker/Caddyfile` | Reverse proxy for admin HTTPS |

License: GPL-3.0-or-later.
