# TLS (admin HTTPS now, NNTPS later)

## Admin portal (Caddy + Let's Encrypt)

Caddy sits in front of the admin UI and obtains certificates automatically.

1. Point your public DNS `A`/`AAAA` at the VM.
2. Copy `.env.example` → `.env` and set at least:

   ```bash
   OPENUSENET_HOSTNAME=news.example.org
   ADMIN_DOMAIN=news.example.org   # usually the same FQDN
   ACME_EMAIL=you@example.org
   POSTGRES_PASSWORD=...           # and matching OPENUSENET_POSTGRES
   ```

3. Open firewall / security-group ports **80** (ACME HTTP-01), **443** (HTTPS), and **119** (NNTP).
4. Start:

   ```bash
   docker compose up -d
   ```

5. Open `https://news.example.org/` — Caddy renews certificates on its own.

Admin HTTP remains on `http://127.0.0.1:8080/` for local/debug access only.

### Without a public domain yet

Leave `ADMIN_DOMAIN=localhost` (Compose default). Caddy serves HTTPS with an internal CA (browsers warn). Use `http://127.0.0.1:8080/` for admin on a laptop. Do not expect Let's Encrypt until DNS points at the host.

## NNTPS (port 563) — not wired yet

OpenUsenetServer already supports `listen.nntp_tls` + `tls.cert_file` / `tls.key_file` (see `config.example.yml` and `OPENUSENET_NNTP_TLS` / `OPENUSENET_TLS_*`). What is left for a live NNTPS cutover:

1. Put PEM files on a shared volume. Caddy stores ACME material under `caddy_data` in its own format — do **not** point the NNTP listener at that volume as-is.
2. When you are ready, either:
   - Run certbot/lego to write `fullchain.pem` + `privkey.pem`, mount them into `openusenet`, and optionally teach Caddy to use the same files with `tls /certs/...`; or
   - Export PEMs and renew with a small sidecar.
3. Publish host port **563**, set `OPENUSENET_NNTP_TLS=:563`, and keep cleartext **119** for classic INN peers unless they ask for TLS.

Until then, peers use port **119** as usual.
