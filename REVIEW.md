# Code review

OpenUsenetServer has about 21,000 non-blank lines of Go (about 4,400 of them tests) in
`cmd/openusenet` and 23 packages under `internal/`, plus the admin portal page
(`internal/admin/index.html`, about 1,750 lines), the scripts in `scripts/`, the Dockerfile
and the Compose file. This file tracks how much of it has been reviewed since 0.8.0
(`main` at `abe8543`). The findings themselves are in [ISSUES.md](ISSUES.md); security
findings are reported privately (see [SECURITY.md](SECURITY.md)).

So far the review has covered only the places the first blind test runs pointed at: the
AUTHINFO and CAPABILITIES handlers, the article head handling, and the transfer replies. No
area has had a full read yet.

## How it works

- Pick an area from the table, set it to "in progress" and say so in the issue tracker (our
  agents also use their agent-chatroom board, `taking: ...`), so two people don't review the
  same code.
- Each finding gets an ID in ISSUES.md (O-nn). Each fix gets its own branch and one pull
  request, with a test where possible (a unit test, or a check in `e2e/`). Don't collect
  unrelated fixes on one branch.
- When an area is done, set it to "reviewed" with the date. A later pass makes it
  "reviewed 2×".

Status values: not started, partial (what was looked at), in progress, reviewed (date),
reviewed 2× (date).

## Areas

| # | Area | Files | Lines | What to look at | Status |
|---|---|---|---|---|---|
| 0 | Docs and samples | `README.md`, `docs/`, `config.example.yml`, `docker-compose.yml`, `docker/` | | Config keys against `internal/config`, Compose and env vars, stale instructions | not started (O-08 noted) |
| 1 | NNTP protocol | `internal/nntp` | ~2,230 | Command loop, reply codes (RFC 3977, 4643, 4644), dot-stuffing, line and article size limits, idle timeouts, AUTHINFO, IHAVE/CHECK/TAKETHIS, POST | partial: AUTHINFO and CAPABILITIES (O-01 to O-04), transfer replies (O-06); conformance checks pass otherwise |
| 2 | Articles | `internal/article` | ~990 | Parsing, header handling, injection (Path, Injection-Info, Message-ID), charset and RFC 2047, overview lines, validation | partial: `Parse`/`Wire` (O-05), injection (O-12), Xref (O-10) |
| 3 | Store | `internal/store` | ~5,160 | PostgreSQL schema and migrations, SQL building, transactions and races, article numbering, history, retention, purge, the memory store used by tests | partial: `TEXT` columns for articles (O-13) |
| 4 | Server and listeners | `internal/server`, `cmd/openusenet` | ~1,150 | Listeners and TLS, connection limits, schedules, startup and migration, command line | not started |
| 5 | Outgoing feeds | `internal/feed`, `internal/inn` | ~1,710 | Peer queue, streaming and IHAVE fallback, newsfeeds patterns and flags, retries, INN config parsing | not started |
| 6 | Inbound and peer auth | `internal/inbound`, `internal/peerauth` | ~320 | Who may IHAVE, allow lists, peer passwords | not started |
| 7 | Control messages and moderation | `internal/control`, `internal/moderate`, `internal/posting` | ~1,120 | newgroup/rmgroup/checkgroups/cancel handling and who may send them, moderated groups, mail to moderators | not started |
| 8 | Admin portal and auth | `internal/admin`, `internal/auth`, `internal/admin/index.html` | ~3,050 + page | Sessions, login, CSRF, access checks per handler, reader and search, archive import from the browser | not started |
| 9 | Spam and binaries | `internal/cleanfeed`, `internal/binary`, `scripts/cleanfeed-filter.pl`, `scripts/install-cleanfeed.sh` | ~440 + scripts | Calling the external filter, binary detection, flood handling | not started |
| 10 | Archive, mail, inpaths, ops | `internal/archive`, `internal/mail`, `internal/inpaths`, `internal/ops`, `internal/retention`, `internal/isc`, `internal/wildmat` | ~2,900 | mbox import/export and file paths, SMTP, TOP1000 reports, watchdog, ISC list fetch, wildmat | not started |
| 11 | Build and packaging | `go.mod`, `Makefile`, `Dockerfile`, `docker-compose.yml` | | Go version, distribution builds, Docker and Podman builds, `go vet` | partial: O-07 reported, O-09 (fixed on the fork), O-14 |
| 12 | Tests and CI | `*_test.go`, `e2e/` | ~4,400 | Coverage of the areas above; the e2e conformance test; no CI workflow yet | partial: `e2e/conformance_test.go` on the fork's `dev-gw` (own module, nntp-testkit v0.11.2, `make e2e`) |

Line counts are approximate (non-blank lines, tests included), from `main` at `abe8543`
(2026-11-09).

## Findings

Fixed so far (the open ones are in [ISSUES.md](ISSUES.md)).

| Finding | Area | Found by | Fixed in |
|---|---|---|---|
| none yet | | | |

## Test coverage by area

| Area | Tests |
|---|---|
| 1 | `internal/nntp/session_test.go`; `e2e/conformance_test.go` on the fork (41 nntp-testkit checks, RFC 3977/4643/4644) |
| 2 | `internal/article/*_test.go` |
| 3 | `internal/store/*_test.go` (mostly the memory store) |
| 4 | `internal/server/server_test.go` (POST feeds IHAVE between two servers) |
| 5 | `internal/feed/feed_test.go`, `internal/inn/*_test.go` |
