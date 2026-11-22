# Known Issues

Findings about OpenUsenetServer 0.8.0 since 2026-11-09. They come from the first blind test
runs against an unmodified server (`config.example.yml`, PostgreSQL 16, test VM), by other
projects' tools, and from reading the code those runs pointed at.
[REVIEW.md](REVIEW.md) tracks how much of the code has been reviewed. Both files are kept up to
date on branch `dev-gw` of the test fork
[go-while/OpenUsenetServer](https://github.com/go-while/OpenUsenetServer/tree/dev-gw), where the
tests below live.

Security problems do not go here: report them privately as described in
[SECURITY.md](SECURITY.md).

Line numbers refer to `main` at `abe8543` unless stated otherwise. "Verified" means checked
against the code by reading it; "reported" means a test or a user reported it and it has not
been reproduced or checked yet.

The planned merge order of the fix branches is in [MERGE-PLAN.md](MERGE-PLAN.md).

Status values: **open**, **PR** (fix waiting for review, with its number), **fixed** (with
the PR that fixed it), **decision** (needs the maintainer to decide), **won't fix**.

## Test runs so far

| Run | Tool | Result |
|---|---|---|
| Conformance, in-process | `e2e/conformance_test.go` (`make e2e`; on the fork's `dev-gw`, not in `main` yet), nntp-testkit v0.11.2, memory store | 30 pass, 4 fail, 7 skip (empty group); with a seeded group 37 pass, 4 fail (the AUTHINFO checks) |
| Conformance, live | `nntp-conformance` v0.11.2 against `openusenet serve` with PostgreSQL | 1st pass 30/4/7, 2nd pass 37 pass, 4 fail, 0 skip |
| pynntp integration tests | pynntp `main` + portable tests (greenbender/pynntp#144), anonymous | 4 pass, 22 fail, 8 skip; one server-side failure (O-01), the rest pynntp or test assumptions |
| Diablo feed interop | Diablo `dnewslink`, 5 articles per mode, then resent | streaming and IHAVE work; duplicates get 438/435; one finding (O-05) |
| Build report | OCTADE, `news.software.nntp` 815 (2026-10-06) | does not build on Debian; Docker and Podman builds fail on package conflicts (O-07) |
| NewsCache interop | NewsCache 1.3.23 with OUS as upstream, 203 commands direct vs through the cache, twice; 3 posts | reads, error codes and COMPRESS fallback match; posted bodies byte-exact; three findings (O-10 to O-12) |
| usenet_archiver real-server test | 16 tricky articles each by POST and IHAVE, pulled back 4 times with the archiver (run r20261109T150600-188e36) | 30 of 32 accepted; archiver side exact; two new findings (O-15, O-16), O-13 on POST, O-05 and O-12 seen again |
| INN interop | INN 2.8.0 prerelease (upstream `main` 4c5218607): innfeed -> OUS, OUS -> innd (streaming), 5 posts each way compared article by article; nntp-testkit against INN (`innd` and `nnrpd`) | both directions work, nothing loops; INN keeps heads byte-exact and writes Xref, OUS does not (O-05, O-10); no new findings |
| Diablo interop, OUS feeding | Diablo `main` 5f3a3cc: OUS peer on Diablo's streaming port and on an IHAVE-only proxy, 3 posts each, read back from Diablo's reader | all 6 arrive with body and head identical to what OUS serves; findings O-17, O-18 |
| e2e suite (fork `dev-gw`) | `e2e/` with nntp-testkit v0.11.2: 36 tests (reader, POST, transit, moderation, feeder against fake peers, one regression test per finding), in-process server, memory store; O-13 on PostgreSQL 16 | all pass on `main` with known issues skipped; each regression test fails on `main` and passes with its fix branch; conformance 37 pass, 0 fail with a seeded group (the four AUTHINFO checks skipped as known); findings O-20 to O-22 |
| Conformance preview | nntp-testkit `main` 4c1bc74 (7 more reader checks, before v0.12.0) against `dev-gw`, seeded group | 43 pass, 1 fail: O-23 |

## Conformance side by side

nntp-testkit v0.11.2, all 41 checks, run blind (no skips) on 2026-11-09. "n/a" means the check does
not apply to that port (a reader port without IHAVE/streaming, or a transit-only port that
answers reader commands with 480).

| Server | Reader checks (25) | AUTHINFO (6) | IHAVE + streaming (10) |
|---|---|---|---|
| OUS 0.8.0 (one port) | 25 pass | 2 pass, 4 fail (O-01 to O-04) | 10 pass |
| INN 2.8.0 pre, `nnrpd` (reader port, login) | 25 pass | 6 pass | 1 pass, 9 n/a (not advertised) |
| INN 2.8.0 pre, `innd` (transit port) | 8 general pass; 17 n/a (480 for reader commands) | n/a (no login given) | 10 pass |
| Diablo `main` (reported by diablo-cc-1, not run here) | pass except `mode-reader` (480 before login, by design) | 6 pass (D-46, diablo-nntp #67) | pass (D-48, #67) |

## Overview

| ID | Severity | Summary | Status |
|---|---|---|---|
| [O-01](#o-01-wrong-password-gets-480-not-481) | Medium | A wrong AUTHINFO password gets 480 instead of 481 | PR (#4) |
| [O-02](#o-02-authinfo-pass-before-user-gets-501-not-482) | Low | AUTHINFO PASS before USER gets 501 instead of 482 | PR (#4) |
| [O-03](#o-03-authinfo-after-a-login-is-accepted-and-drops-the-login) | Low | AUTHINFO after a successful login is accepted (381) instead of 502, and logs the session out | PR (#4) |
| [O-04](#o-04-capabilities-still-lists-authinfo-after-a-login) | Low | CAPABILITIES still lists `AUTHINFO USER` after a login | PR (#4) |
| [O-05](#o-05-the-article-head-is-rebuilt-not-kept) | Medium | The article head is rebuilt, not kept: header order changes, unknown headers come out in random order, folding is lost | open (fix: fork branch `fix/keep-article-bytes`) |
| [O-06](#o-06-235-and-239-reply-text-repeats-the-message-id) | Low | 239 (and 235) reply text repeats the message-id | open (fix: fork branch `fix/takethis-reply-text`) |
| [O-07](#o-07-build-fails-on-debian-and-with-docker-or-podman) | Low | Build fails on Debian, Docker and Podman builds fail on package conflicts | not a bug (Debian's Go defaults to `GOTOOLCHAIN=local`; see details) |
| [O-08](#o-08-the-example-path-host-reaches-peers) | Low | Running with the example config prepends `news.localhost!` to Path | open (fix: fork branch `fix/example-pathhost`: placeholder in the examples, refuses to peer with a placeholder name) |
| [O-09](#o-09-test-dependency-raises-the-go-version) | Low | The e2e test dependency raises `go.mod` to Go 1.26 | fixed on the fork (`e2e/` is its own module) |
| [O-10](#o-10-stored-articles-have-no-xref-header) | Medium | Stored articles have no Xref header, but OVER's `Xref:full` field has one | open (fix: fork branch `fix/xref-served`, on keep-article-bytes: Xref set when ARTICLE/HEAD serve, stored articles and the feed unchanged) |
| [O-11](#o-11-over-by-message-id-reports-the-wrong-bytes) | Low | OVER by message-id reports the wrong `:bytes` | open (fix: fork branch `fix/over-msgid-bytes`) |
| [O-12](#o-12-injection-info-always-says-posting-accountopenusenet) | Low | Injection-Info always says `posting-account="openusenet"`, not the logged-in user | open (fix: fork branch `fix/posting-account`) |
| [O-13](#o-13-8-bit-articles-cannot-be-stored-and-get-436-forever) | High | Articles with non-UTF-8 bytes or NUL (Latin-1 text, yEnc) cannot be stored; IHAVE answers 436, so peers retry forever | open (fix: fork branch `fix/store-articles-bytea`; verified on PostgreSQL 16 incl. migration of a live test DB) |
| [O-14](#o-14-go-vet-fails-on-main) | Low | `go vet ./...` fails on `main`: self-assignment in `internal/mail/settings.go` | open (fix: fork branch `fix/go-vet-self-assign`) |
| [O-15](#o-15-a-bare-cr-or-lf-becomes-a-line-break) | Medium | A bare CR (or LF) inside an article becomes a line break: `col1\rcol2` is stored as two lines | open (fix: fork branch `fix/keep-article-bytes`) |
| [O-16](#o-16-a-header-line-without-a-colon-is-mangled-not-refused) | Low | A header line without a colon is split at its first colon and stored with a space added, instead of refused | open (fix: fork branch `fix/keep-article-bytes`) |
| [O-17](#o-17-the-feeder-opens-one-connection-per-article) | Medium | The outgoing feeder opens one connection per article, and sends CHECK/TAKETHIS one at a time | open (fix: fork branch `fix/feed-persistent-conn`) |
| [O-18](#o-18-no-streaming-to-a-peer-without-capabilities) | Low | A peer that does not answer CAPABILITIES (Diablo) gets IHAVE; MODE STREAM is never tried | open (fix: fork branch `fix/mode-stream-fallback`) |
| [O-19](#o-19-a-refused-post-gets-two-replies) | Medium | A refused POST gets 480 or 440 and then also 340; the server reads the next commands as article text | open (fix: fork branch `fix/post-auth-reply`) |
| [O-20](#o-20-parallel-feed-connections-without-a-limit) | Medium | Every accepted article starts its own feed run, so connections to one peer run in parallel without a limit | open (fix: fork branch `fix/feed-single-drain`) |
| [O-21](#o-21-a-slow-peer-gets-articles-twice) | Low | A batch slower than the 2-minute claim is claimed again, and the peer is offered the same articles twice | open (fix: fork branch `fix/feed-single-drain`) |
| [O-22](#o-22-the-listener-is-published-without-synchronisation) | Low | `ListenAndServe` stores its listener without synchronisation and `Addr()` reads it: a data race (`go test -race`) | open (fix: fork branch `fix/listener-race`) |
| [O-23](#o-23-hdr-and-over-without-a-range-answer-423-not-420) | Low | HDR and OVER without a range answer 423 instead of 420 when there is no valid current article | open (fix: fork branch `fix/hdr-no-current-article`) |
| [O-24](#o-24-a-peer-configured-by-hostname-can-feed-any-group) | Medium | A peer configured by hostname is admitted, but its group patterns are not applied, so it can feed any group | open (fix: fork branch `fix/inbound-peer-by-hostname`) |
| [O-25](#o-25-the-suggested-peer-password-can-be-computed-by-anyone) | Low | The admin portal suggests a peer password derived from the two path hostnames, which anyone can compute | open (fix: fork branch `fix/peer-password-random`); decision on the snippet for peers without an outgoing password |
| [O-26](#o-26-a-new-archive-job-is-encoded-while-it-is-being-updated) | Low | A new archive import or export job is returned and encoded as JSON while its goroutine updates it: a data race (`go test -race`) | open (fix: fork branch `fix/import-job-race`) |

## Details

### O-01 Wrong password gets 480, not 481
RFC 4643 section 2.3.2: a rejected password gets 481 ("Authentication failed/rejected"); 480
means "authentication required" and RFC 4643 section 2.3.1 forbids it as a reply to AUTHINFO.
`cmdAuthinfo` answers `480 Authentication failed` (`internal/nntp/session.go:647`,
`FailAuthNeeded`). Clients that treat 480 as "log in first" may retry the same credentials in
a loop. Verified. Found by nntp-testkit (`authinfo-wrong-pass`) and pynntp's tests.
Fix: a 481 constant in `internal/nntp/codes.go` and use it here.

### O-02 AUTHINFO PASS before USER gets 501, not 482
RFC 4643 section 2.3.2: AUTHINFO PASS without a preceding USER is a sequence error, 482.
The server answers `501 AUTHINFO USER required first` (`internal/nntp/session.go:626`).
Verified. Found by nntp-testkit (`authinfo-pass-without-user`).

### O-03 AUTHINFO after a login is accepted and drops the login
RFC 4643 section 2.2: once a session is authenticated, every further AUTHINFO gets 502. The
server answers a second `AUTHINFO USER` with 381 (`internal/nntp/session.go:612-620`), and
that branch also clears `authOK`, `authUser` and `authUserID`, so a logged-in session that
sends AUTHINFO USER again is logged out even if it never sends PASS. The same applies after a
peer login through `tryFeedAuth` (`feedAuthOK` is never checked here). Verified. Found by
nntp-testkit (`authinfo-after-auth`).

### O-04 CAPABILITIES still lists AUTHINFO after a login
RFC 4643 section 2.2: after a successful login the server must not advertise AUTHINFO any
more. `cmdCapabilities` returns a fixed list that always contains `AUTHINFO USER`
(`internal/nntp/session.go:150-170`). Verified. Found by nntp-testkit
(`authinfo-capabilities`). Diablo fixed O-01 to O-04 together (its D-46, diablo-nntp #67) and
pins them with the six RFC 4643 checks of nntp-testkit; the fork's `e2e/conformance_test.go` can do the
same once they are fixed.

### O-05 The article head is rebuilt, not kept
RFC 5537 section 3.2 (relaying agents) and 3.3 (serving agents): an agent "MUST NOT alter,
delete, or rearrange any part of an article" except Path and Xref. `article.Parse` splits the
head into `map[string][]string` (`internal/article/article.go:15-84`), and `Wire()`
(`article.go:122-164`) writes it back in its own order: a fixed list of known headers first,
then all other headers in Go map iteration order, which is random. Effects:
- the received order is lost (Diablo sent `Subject, Message-ID, Date`; HEAD returned
  `Subject, Date, Message-ID`);
- headers outside the list (`X-*`, `Cancel-Lock`, `NNTP-Posting-Host`, `Lines`, ...) can come
  out in a different order every time `Wire()` runs;
- folded header lines are joined into one line and their whitespace trimmed
  (`parseHeaders`, `article.go:51-84`), so long `References` or `Path` lines can exceed the
  998-character line limit of RFC 5536 section 3.2.4 (via RFC 5322);
- two spellings of one header name (`Message-ID`, `Message-Id`) are separate map keys.

`Wire()` is what IHAVE/TAKETHIS (`internal/nntp/session.go:1139`, `:1175`), POST
(`internal/posting/posting.go:139`), moderation, the mbox archive and `Bytes()` use, so the
rebuilt head is what gets stored, served and fed to peers. Signed headers (for example the
PGP signature of control messages, which covers listed header values) can break when
whitespace changes. Verified. Found by the Diablo feed interop run.
Fix direction: keep `RawHeaders` (already parsed, `article.go:16`) as the source of truth and
change only Path (prepend) and Xref (replace) in place.

### O-06 235 and 239 reply text repeats the message-id
For TAKETHIS the reply is `239 <id> article transferred <id>`: `replyTransfer` puts the id
first as RFC 4644 requires, and the text passed in by `acceptPeerArticle` adds it again
(`internal/nntp/session.go:1206`). Legal, only redundant; for IHAVE (`235 article
transferred <id>`) the id in the text is fine. Verified. Found by the Diablo feed interop run.

### O-07 Build fails on Debian and with Docker or Podman
Reported in `news.software.nntp` (article 815, 2026-10-06): "it does not want to compile on
Debian (package conflicts). Also trying docker and podman exposes package conflicts." Checked,
not a bug in OpenUsenetServer:
- `go.mod` asks for Go 1.25. Since Go 1.21 the `go` command fetches the toolchain a module
  needs by itself (`GOTOOLCHAIN=auto`), but Debian's `golang-go` package (1.24 in Debian 13)
  presets `GOTOOLCHAIN=local`, so it stops with "go.mod requires go >= 1.25 (running go 1.24;
  GOTOOLCHAIN=local)". Building with `GOTOOLCHAIN=auto go build ./cmd/openusenet`, or with a Go
  from go.dev, works.
- The Docker image builds with `golang:1.25-bookworm` and runs on `ubuntu:24.04` (cleanfeed-ng
  needs Perl 5.38 or later), so it does not depend on the host's packages; the reported
  Docker/Podman conflicts were not reproduced and came without an error message.
A line about `GOTOOLCHAIN=auto` in the README's build section would help Debian users.

### O-08 The example path host reaches peers
With `config.example.yml`, `server.pathhost` is `news.localhost`, and every article the
server relays gets `news.localhost!` prepended to Path (seen in the Diablo interop run). A
server started from the example and peered as-is sends that name to its peers. Behaviour as
configured; a startup warning when `pathhost` (or `hostname`) is still the example value would
help. Decision for the maintainer.

### O-09 Test dependency raises the Go version
The e2e conformance test (`e2e/` on the fork's branch `dev-gw`) imports nntp-testkit, which needs Go 1.26, so
`go mod tidy` raises the `go` line of the main module from 1.25 to 1.26 and the Dockerfile
builder image has to follow. That makes O-07 worse for distribution builds. Alternative: give
`e2e/` its own `go.mod` (with a `replace openusenet => ..`), so only the tests need Go 1.26.
Done on `dev-gw`: `e2e/go.mod` is module `openusenet/e2e` (Go 1.26, nntp-testkit v0.11.2,
`replace openusenet => ../`); the server's `go.mod` and the Dockerfile stay at Go 1.25.
`go test ./...` in the root does not enter `e2e/`; `make e2e` runs it.

### O-10 Stored articles have no Xref header
RFC 3977 section 8.4.1: the `Xref:full` overview field is the article's Xref header, so the
two should agree (INN writes Xref into the stored article). `InjectForIHave` and
`InjectForPost` strip any incoming Xref (`internal/article/article.go:231`, `stripXref`), and
after `Post` the new Xref is set on the article only for the mbox copy
(`internal/nntp/session.go:1234`, `internal/posting/posting.go:157`, both inside
`if mbox != nil`); the head stored in the database has none. OVER still prints
`Xref: news.localhost local.test:1` from the stored `xref` column. Effect seen with
NewsCache: it rebuilds its overview from the article once it caches it, so through the cache
the field goes empty. Verified. Found by the NewsCache interop run.

Fix (fork branch `fix/xref-served`): ARTICLE and HEAD set `Xref:` from the stored xref value
when they serve the article (replacing any Xref line in the stored head, which archive imports
can carry); the stored article and the feed stay as they are. OVER and HDR `:bytes` count the
served size: new articles store it, and a one-time PostgreSQL migration (under an advisory lock,
with a marker in a new `store_migrations` table) recounts only the `bytes` column of older rows.
Upgrade all servers that share one database together: a server still running older code stores
new rows without the Xref line in the count.


### O-11 OVER by message-id reports the wrong bytes
`OVER <message-id>` builds a stand-in `Article` with only Subject, From, Date, Message-ID and
References plus the body (`internal/nntp/session.go:498-504`) and lets `OverviewLine` count
`:bytes` from that (`Bytes()` is `len(Wire())`, `internal/article/article.go:165`), so every
other header is missing from the count. The same article gave `:bytes` 365 with OVER by number
and 300 by message-id. The stored `bytes` column (`a.Bytes`, already loaded) is the right
value. Verified. Found by the NewsCache interop run.

### O-12 Injection-Info always says posting-account="openusenet"
`InjectForPost` writes `Injection-Info: <host>; posting-account="openusenet"` for every post
(`internal/article/article.go:229`); `InjectOpts` has no field for the user, although the
session knows it (`s.authUser`). A post by `nc-interop` carried `openusenet`. RFC 5536
section 3.2.8 (Injection-Info) describes `posting-account` as identifying the poster's
account for abuse handling; a fixed value makes it useless (a hashed or plain user name, as
INN and others do, would be the fix). Verified. Found by the NewsCache interop run.

### O-13 8-bit articles cannot be stored and get 436 forever
The `articles` table keeps `headers` and `body` as PostgreSQL `TEXT`
(`internal/store/postgres.go:37-38`), and the transfer and posting paths pass the raw bytes
without conversion (`SanitizeUTF8` in `internal/article/utf8.go` is only used for display, the
ISC list and archive import). PostgreSQL rejects any byte sequence that is not valid UTF-8, and
any NUL. Reproduced on the test server (IHAVE, `local.test`):
- body `caf\xe9` (Latin-1): `335`, then `436 try again later`; log: `peer transfer store
  <...>: ERROR: invalid byte sequence for encoding "UTF8": 0xe9 0x20 0x6e (SQLSTATE 22021)`;
- body with a NUL byte: the same, `0x00`;
- the same text in UTF-8: `235`, stored.

436 tells the peer to offer the article again later, so every such article is retried
indefinitely and never stored. Usenet carries a lot of 8-bit text that is not UTF-8 (Latin-1,
KOI8-R, Shift-JIS, ...) and yEnc binaries are 8-bit by design, so this probably affects a large
part of a real feed, binary groups included. Converting with `SanitizeUTF8` would store the
article but change its bytes (and RFC 5537 forbids that); storing head and body as `bytea`
keeps them byte-exact. Until then, a permanent reject (437/439) is better than 436.
Verified (reproduced). Found while answering usenet_archiver's questions. The usenet_archiver
test then showed the POST side: a Latin-1 body gets `403 internal error` (any error a command
handler returns ends in that generic reply, `internal/nntp/session.go:89-90`); RFC 3977 wants
441 for a refused POST.

### O-14 go vet fails on main
`internal/mail/settings.go:113` is `s.Password = s.Password` (next to the `TrimSpace` lines for
the other fields), and `go vet ./...` stops there with "self-assignment of s.Password". If the
point is that passwords are not trimmed, a comment instead of the assignment says so and keeps
`go vet` clean, so it can run in CI. Verified.

### O-15 A bare CR or LF becomes a line break
`article.Parse` starts with `normalizeNewlines` (`internal/article/article.go:44-49`), which
turns every CRLF into LF, then every remaining CR into LF, then every LF into CRLF. A CR or LF
that is part of a line's data is therefore turned into a line break: a body line
`col1\rcol2 on one line` is stored and served as two lines, `col1` and `col2 on one line`
(POST and IHAVE alike). RFC 3977 section 3.1.1 allows a server to refuse such articles, but
not to change them (RFC 5537). Options: keep the bytes (best for archives and binaries), or
refuse with 441 (POST) and 437/439 (IHAVE/TAKETHIS). Verified (reading the code; reproduced by
the usenet_archiver test, `cr.post` and `cr.ihave`).

### O-16 A header line without a colon is mangled, not refused
`parseHeaders` cuts each header line at its first colon (`internal/article/article.go:74-79`)
and only refuses a line with no colon at all. A line such as
`From nobody Thu Dec  8 12:55:04 2016` inside the head becomes a field named
`From nobody Thu Dec  8 12` with the value `55:04 2016`, and `Wire()` writes it back as
`From nobody Thu Dec  8 12: 55:04 2016`. A field name may not contain spaces (RFC 5536 section
2.2 via RFC 5322 section 3.6.8), so the head is invalid and should be refused (go-pugleaf
answers `invalid header block`). Verified (reading the code; reproduced by the usenet_archiver
test, `fromhdr2.post` and `fromhdr2.ihave`).

### O-17 The feeder opens one connection per article
`drainOnce` (`internal/feed/feed.go:128-150`) delivers each queued article with its own
`deliverArticle` call, and `deliverArticle` (`internal/feed/stream.go:16-51`) dials the peer,
reads the greeting, logs in if configured, sends CAPABILITIES, transfers that one article and
sends QUIT. Seen by inn-1 in INN's logs: every connection from OUS accepted exactly one article
and closed, up to three at once. With streaming, CHECK and TAKETHIS go one at a time and wait
for each reply (`streamingTransfer`, `stream.go:115-153`), so the point of RFC 4644 (pipelined
CHECKs on a long-lived connection) is lost. It works for a small feed, but a real peer expects a
persistent connection per peer that carries a queue of articles; with a full feed the
connection rate alone can trip peers' rate limits. Verified (reading the code; seen in INN's
logs and in Diablo's: `ihave=1 ... acc=1` and a disconnect for every connection).

### O-18 No streaming to a peer without CAPABILITIES
`peerSupportsStreaming` (`internal/feed/stream.go:85-114`) sends CAPABILITIES and, when the
reply is not 101, treats the peer as IHAVE-only. Diablo's feeder answers CAPABILITIES with
`500 Syntax error or bad command` (a gap on Diablo's side, RFC 3977 makes CAPABILITIES
mandatory) but accepts `MODE STREAM` with 203, so OUS feeds Diablo by IHAVE although it could
stream. Trying `MODE STREAM` when CAPABILITIES fails would cover such
peers. The articles still arrive, so this is only about efficiency. Verified (reading the code;
confirmed in Diablo's feeder log: every OUS session on the streaming port was
`ihave=1 chk=0 takethis=0`).

### O-19 A refused POST gets two replies
`requirePostAuth` (`internal/nntp/session.go:653-672`) sends `480 Authentication required for
POST` (not logged in) or `440 posting not permitted` (the user may not post) and returns the
error of that write, which is nil, so `cmdPost` (`session.go:902-916`) carries on and also
sends `340 send article to be posted`. The client got its 480 and goes on with, say,
`AUTHINFO USER`, but the server now reads that as article text until a lone dot, so the session
hangs or goes out of sync. The IHAVE path already avoids this with `replied()`. Verified (found
by the agent writing the O-01 to O-04 fix, whose test hung on it; reproduced with a unit test
that times out without the fix).

### O-20 Parallel feed connections without a limit
`Offer` (`internal/feed/feed.go:93`) starts `go f.drainOnce(...)` for every accepted article, in
addition to the worker that drains every 5 seconds (`RunWorker`, `feed.go:112`). Each drain
claims what is due and delivers it with one connection per article (O-17), so a burst of
articles opens as many connections to the same peer at once, with no per-peer limit. Peers cap
connections per peer (INN's `max-connections`, Diablo's `maxconnect`) and refuse the rest,
which then go back into the queue. Verified with a fake peer: 20 articles gave 20 connections,
6 at once; with every connection held open, 16 of 16 were open at the same time
(e2e `TestFeedParallelBound`, a known issue). Belongs with O-17: a persistent feeder per peer
fixes both.

### O-21 A slow peer gets articles twice
`ClaimFeedDue` marks claimed items due again after 2 minutes
(`internal/store/postgres_ops.go:176`, memory store `memory.go:1118`), but `drainOnce`
(`feed.go:128`) delivers its up to 32 items one after the other, each with a 15-second timeout
per step (`feed.go:51`). Against a slow or unreachable peer a batch takes longer than 2 minutes,
and the next drain (the 5-second worker, or any new article) claims the rest again, so the same
articles are offered over a second connection at the same time. Nothing is lost (the peer
refuses duplicates), but it doubles the traffic exactly when the peer is struggling. Verified
with a fake peer that greets after 5 seconds: 30 queued items gave 36 connections, 6 articles
offered twice (e2e `TestFeedLeaseNoDuplicates`, a known issue, needs `OUS_E2E_SLOW=1`).

### O-22 The listener is published without synchronisation
`ListenAndServe` stores the listener in `s.ln` (`internal/server/server.go:93`) while
`Addr()` (`server.go:360`) reads it from other goroutines, with no lock or atomic. Tests that
start a server and poll `Addr()` (upstream's `internal/server/server_test.go`, and the fork's
e2e harness until it stopped doing so) are data races; `go test -race` reports them. In
production nothing calls `Addr()` concurrently, so this is a test-visible bug. Fix: an
`atomic.Pointer` or a mutex around `s.ln`. Verified (`go test -race`, found by ous-sol).

### O-23 HDR and OVER without a range answer 423, not 420
Without a range, HDR (RFC 3977 section 8.5.2) and OVER (8.3.2) are about the current article,
and an invalid current article gets 420. `cmdHdr` (`internal/nntp/session.go:554`) uses the
current number as the range, so in a group without a current article (number 0) it answers
`423 no articles in that range`. Both `cmdHdr` and `cmdOver` (`session.go:507`) also answer 423
when the current article was cancelled or expired after GROUP selected it. Verified; found by
nntp-testkit's new no-current-article check (kit `main`, before v0.12.0), the expired case by
ous-sol's review.

### O-24 A peer configured by hostname can feed any group
Admission (`inbound.Allowed`, `internal/nntp/session.go:1110`) matches the connecting address
against peer hosts by IP, CIDR or a forward DNS lookup of a hostname (`internal/inbound/allow.go`).
The peer whose patterns then filter the article is picked by `matchInboundPeer`
(`session.go:856`), which only compares the connecting IP as text with `incoming_host`, `host`
and `name`. For a peer configured by hostname (the usual way) that never matches, so
`acceptPeerArticle` finds no peer and applies no patterns (`session.go:1156`): the peer can feed
any group, whatever its subscription says. Peers configured by IP, and peers that logged in with
the feed password, are matched correctly. OUS never uses reverse (PTR) lookups. Verified with a
peer `localhost` (patterns `local.*`, inbound closed): an article for `alt.test` got 235
(`TestHostnamePeerPatternsApply` on the fix branch).

### O-25 The suggested peer password can be computed by anyone
A peer's page in the admin portal shows INN snippets for the other side, and the
`incoming.conf` snippet contains `password:` from `peerauth.PairPassword`
(`internal/admin/admin.go:844`): the first 16 hex digits of SHA-256 over the two path hostnames
and a fixed string. Both hostnames are in every Path header, so anyone can compute the password
of any pair of peers (shown on a test server: the portal's value and one computed from the two
names alone were the same). Versions from `7724153` to `6c348b5` also stored this value in
peers' password fields. The impact is low, because peers are matched by address first, but a
password that is not secret is no password. OUS itself never sends the derived value unless it is
stored as the peer's outgoing password. Fix: the snippet shows the peer's outgoing password, or
a random secret created once per peer and stored; `PairPassword` stays, and the portal warns when
a peer still uses a derived password. Stored passwords are not changed, so existing feeds keep
working. Verified.

Open question for the maintainer (found by ous-sol's review): for a peer without an outgoing
password, OUS sends no AUTHINFO, but the snippet still asks the remote side to require a
password (the derived one before, a random one with the fix), so a remote admin who pastes it
as is will refuse our feed. Options: (1) show the line commented out (`# password: ...`) with a
note to set the same value as the peer's outgoing password first; (2) leave the line out until
an outgoing password is set; (3) prefill the outgoing password in the editor so saving stores it
(OUS then sends AUTHINFO, which breaks the feed until the remote side has it). Left to the
maintainer.

### O-26 A new archive job is encoded while it is being updated
`startImportJob` and `startArchiveJob` (`internal/admin/admin.go:1130`, `:1159`) store a new job,
start the goroutine that fills in its result under `jobsMu`, and return the same pointer; the
handler encodes it as JSON without the lock (`admin.go:1096`). `go test -race` reports 9 data
races in `TestArchiveImportAPI` on `main`, and a status reply can be torn when a small import
finishes at once. The job list (`archiveJobs`) already copies under the lock. Fix: return a copy
taken under the lock. Verified (`go test -race`, found while running the race detector on the
integration of all fix branches).

## Checked, not a bug

From the pynntp and conformance runs; recorded so they are not reported again.

- `DATE` answers `111 20261109141713 server date and time`: text after the argument is allowed
  (RFC 3977 section 3.2); pynntp's parser rejects it and is being fixed there.
- `GROUP` of an empty group answers `211 0 0 0 <group>`: one of the three empty-group forms
  RFC 3977 section 6.1.1 allows (INN answers `211 0 1 0`).
- POST without a login gets 480 once at least one user exists (`requirePostAuth`,
  `internal/nntp/session.go:653`): by design; with no users posting is open.
- Reading without a login is allowed (nntp-testkit `authinfo-480`: "not restricted"): by
  design.
- Replies to commands by message-id carry article number 0 (`223 0 <id>`): correct per
  RFC 3977 section 6.2.
- O-07: building on Debian needs `GOTOOLCHAIN=auto` (Debian presets `local`) or a Go from
  go.dev; not a code problem.
