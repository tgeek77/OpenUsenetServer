# Backup and Internet Archive exports

OpenUsenet keeps articles in PostgreSQL indefinitely by default. Use **archive exports** when you want a separate, portable snapshot for Internet Archive or offsite storage.

## What gets exported

- One gzip-compressed mboxrd file per newsgroup: `misc.test.moderated.mbox.gz`
- Written under `archive.export_dir/<UTC-timestamp>/`
- Source of truth is PostgreSQL (not the live append-only `mbox_dir` spool)

Exports never delete or expire live articles.

## Manual export

```bash
openusenet archive export --groups all --config config.yml
openusenet archive export --groups 'misc.test*' --dir ./exports --config config.yml
```

Or use **Archive export** in the admin portal (requires admin login).

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
