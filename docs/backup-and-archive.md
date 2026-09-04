# Backup and Internet Archive exports

OpenUsenet keeps articles in PostgreSQL indefinitely by default. Use **archive exports** when you want a separate, portable snapshot for Internet Archive or offsite storage.

## What gets exported

- One gzip-compressed mboxrd file per newsgroup: `misc.test.moderated.mbox.gz`
- Written under `archive.export_dir/<UTC-timestamp>/`
- Source of truth is PostgreSQL (not the live append-only `mbox_dir` spool)

Exports never delete or expire live articles. Separately, **retention** may expire
bodies in groups that hit a binary-flood quota (see below); the export feature is
orthogonal.

## Retention (text forever, binary flood quotas)

- Default: articles are kept **forever** (`default_live_days: 0`).
- When a group receives a flood of binary-looking posts (yEnc / uuencode / bulk Base64 / binary MIME), it is auto-switched to a short live quota (`flood_live_days`, default 7) and an admin-portal **alert** asks you to block, whitelist, or dismiss.
- Message-ID **history** is pruned after `history_days` so expired binaries are not re-accepted forever.
- Authenticated users may POST at most `user_binary_posts_per_day` (default 25) binary articles per UTC day. Text posts are unlimited. This is volume control for filesharing uploads, not content policing.

## Full-text search

The web newsreader can search article **subject**, **from**, and **body** via PostgreSQL
full-text search (`websearch_to_tsquery`). Queries support phrases (`"exact words"`)
and exclusions (`-spam`).

On first migrate / startup after upgrade, Postgres adds a generated `search_tsv`
column and a GIN index. That index grows with the corpus and can take noticeable
disk and time after large archive imports. Selective group import keeps this manageable.

## Manual export

```bash
openusenet archive export --groups all --config config.yml
openusenet archive export --groups 'misc.test*' --dir ./exports --config config.yml
```

Or use **Archive export** in the admin portal (requires admin login).

## Import (historical mbox)

Load old articles from plain `.mbox` / `.mbox.gz` (mbox or mboxrd) into PostgreSQL.
Duplicate Message-IDs are skipped. Peers are not offered imported articles.

**Admin portal:** Archive import (file upload, admins only).

```bash
openusenet archive import ~/temp/news.groups.mbox --config config.yml
openusenet archive import ~/temp/*.mbox --config config.yml
openusenet archive import dump.mbox.gz --group alt.fan.usenet --restrict-group
```

Newsgroups come from each article’s `Newsgroups` header. If missing, the filename
(`alt.fan.usenet.mbox` → `alt.fan.usenet`) or `--group` is used. Pass
`--spool` to also append to the live `mbox_dir` spool.

## Automatic export

In `config.yml`:

```yaml
archive:
  export_dir: ./exports
  schedule: weekly   # or daily
  groups: all
  retain_generations: 4
```

Old export **directories** beyond `retain_generations` are pruned; the database is untouched.

## Full server backup

1. `pg_dump` the OpenUsenet database
2. Copy `archive.export_dir` (and optionally the live `mbox_dir` spool)
3. Keep `config.yml` (hostname, TLS paths, inbound allow list)

## Giving a dump to Internet Archive

1. Run `openusenet archive export --groups all`
2. Upload the dated directory of `*.mbox.gz` files
3. Document the server hostname and export timestamp in the item description
