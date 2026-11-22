# Merge plan for the fork's fixes

Every fix is its own branch on [go-while/OpenUsenetServer](https://github.com/go-while/OpenUsenetServer),
based on upstream `main` at `abe8543` (except `fix/over-msgid-bytes-on-kab`, which sits on
`fix/keep-article-bytes` on purpose, see below), with its own tests. This file is the planned order for
upstream pull requests and what was checked. Findings: [ISSUES.md](ISSUES.md).

Only three pull requests are open upstream (#2, #3, #4); the rest are filed one at a time as
earlier ones land.

## How it was checked

- **Pairwise:** all pairs of the branches were merged test-wise (`git merge-tree`).
  Conflicts that were only tests appended to the same file were removed by moving each
  branch's tests into its own file. Two conflicts are left, both with keep-article-bytes and each a few lines (see below). Clashes git cannot see (two test helpers with one name; a changed function signature
  that another branch's test calls) showed up when building the integration and were fixed on
  the branches.
- **Integration:** branch `merge-train` is `main` plus all 19 branches merged in the order below,
  without a conflict. On it: `go build`, `go vet ./...` (clean, O-14 included), `go test ./...`
  and `go test -race ./...` pass.
- **e2e:** `merge-train` plus the fork's `e2e/` suite (nntp-testkit, 36 tests), with
  `OUS_E2E_KNOWN_ISSUES=1 OUS_E2E_SLOW=1`: everything passes, including all regression tests and
  the 41 conformance checks, except O-17 (a design question, not in this plan). O-13 needs
  PostgreSQL and was verified separately on PostgreSQL 16.
- **Review:** each branch was written by one agent and reviewed by another (ous-sol, GPT-6.1
  Sol); review findings were fixed on the branch and re-checked.

`merge-train.sh` on the fork's `dev-gw` branch rebuilds `merge-train` from the branch list (run it by path from a `merge-train` worktree; usage in the script).

## Order

| # | Branch | Head | Finding | Size | Why here |
|---|---|---|---|---|---|
| — | `security-policy` (PR #2), `issues-review` (PR #3) | | docs | | independent, any time |
| 1 | `fix/authinfo-codes` (PR #4) | `ac01a95` | O-01..O-04 AUTHINFO codes | 3 files, +148/-9 | open already |
| 2 | `fix/go-vet-self-assign` | `6f7bd46` | O-14 `go vet` | 1 line | makes `go vet ./...` clean for every later PR |
| 3 | `fix/server-test-watchdog` | `7dd085c` | test: load watchdog off in TestPOSTFeedsIHAVE | +6 | stops the 441 flake on busy CI |
| 4 | `fix/post-auth-reply` | `32fca07` | O-19 refused POST gets two replies | +51/-2 | small, session sync bug |
| 5 | `fix/takethis-reply-text` | `fbd1458` | O-06 239 text repeats the id | +29/-4 | small |
| 6 | `fix/hdr-no-current-article` | `8bdee4d` | O-23 HDR/OVER 420 vs 423 | +87 | small |
| 7 | `fix/posting-account` | `87e09c3` | O-12 posting-account | 7 files, +137/-15 | small, touches posting and the web reader |
| 8 | `fix/listener-race` | `d95499d` | O-22 listener data race | +8/-4 | small |
| 9 | `fix/import-job-race` | `6e79e9d` | O-26 archive job data race | 2 files, +61/-2 | small |
| 10 | `fix/inbound-peer-by-hostname` | `0956037` | O-24 hostname peers bypass patterns | +67/-7 | small, Medium |
| 10a | `fix/example-pathhost` | `0ab01b1` | O-08 placeholder path identity; `--hostname` moves a defaulted pathhost | 10 files, +393/-8 | small; refuses to start with peers while the name is a placeholder |
| 11 | `fix/mode-stream-fallback` | `0527278` | O-18 MODE STREAM fallback | +206/-2 | feeder, independent of 12 |
| 12 | `fix/feed-single-drain` | `8699c7b` | O-20, O-21 parallel drains, resends | 3 files, +605/-23 | feeder; keeps `deliverArticle`'s signature so 11 and 12 merge in either order |
| 12a | `fix/feed-persistent-conn` | `e9c3ce1` | O-17 one persistent, pipelined connection per peer | 5 files, +1606/-160 on top of 11 and 12 | **after 11 and 12** (it is built on both) |
| 13 | `fix/peer-password-random` | `b85a95e` | O-25 computable peer password | 5 files, +209/-8 | one open question for the maintainer (ISSUES.md O-25) |
| 14 | `fix/keep-article-bytes` | `2ad717c` | O-05, O-15, O-16 article bytes kept | 16 files, +894/-157 | large; the core change |
| 15 | `fix/over-msgid-bytes-on-kab` | `1ff8529` | O-11 OVER by message-id | +67/-11 on top of 14 | **after 14**; resolves the one real conflict (see below) |
| 15a | `fix/xref-served` | `877662d` | O-10 Xref in served articles; byte counts include it | 10 files, +907/-5 on top of 14 | **after 14**; independent of 15 and 16 (merge-tree clean); one-time PG migration of the bytes column, upgrade all servers on one database together |
| 16 | `fix/store-articles-bytea` | `f42e4fe` | O-13 8-bit articles in PostgreSQL | 9 files, +490/-17 | large, schema migration; last |

Steps 2 to 13 are independent of each other: they merge onto `main` and onto each other in
any order without a conflict (checked pairwise), and all of them together build and pass. The order above is only small and safe
first. 14 to 16 are the large changes; 15 depends on 14.

### O-17 and keep-article-bytes

keep-article-bytes changes one line in the feeder: stored articles are parsed with
`article.ParseLenient` (heads stored by older versions may not pass the strict `Parse`). The
O-17 branch moved that code from `deliver` into the new `prepare`. Whichever lands second takes
over the line: `parsed, _ := article.ParseLenient(wire)` in `prepare`. `merge-train.sh` replays
this resolution with git rerere.

### O-11 and keep-article-bytes

Both change the message-id branch of `cmdOver`. `fix/over-msgid-bytes-on-kab` is
`fix/over-msgid-bytes` (`37b3206`) cherry-picked onto `fix/keep-article-bytes`, keeping O-11's
side (the stored overview row through `overviewLine`; the article rebuilt there by
keep-article-bytes is dropped). If O-11 is wanted before 14, use `fix/over-msgid-bytes` instead
and resolve 14 the same way. After 15, `article.OverviewLine` has no caller outside tests.

## Not in this plan

- **O-07**: not a bug (Debian presets `GOTOOLCHAIN=local`), see ISSUES.md.
- Admin portal hardening that was discussed but not decided: a one-time token or loopback-only
  first-admin setup, and a rate limit on `/api/login`.

## After each merge

- Remove the `knownIssue(...)` call of the merged finding in the fork's `e2e/` tests, so the
  test runs by default.
- Update ISSUES.md: status **fixed (#PR)**.
- Rebuild `merge-train` on the new upstream `main` (in a `merge-train` worktree: `git fetch https://github.com/tgeek77/OpenUsenetServer main && sh <dev-gw checkout>/merge-train.sh FETCH_HEAD`; merged branches are skipped) and rerun the checks above before filing the next PR. If Jason squashes or rebases a PR, the branches after it may need a rebase; the script stops at the first conflict.
