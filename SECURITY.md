# Security Policy

## Supported Versions

OpenUsenetServer is developed on the `main` branch of
[tgeek77/OpenUsenetServer](https://github.com/tgeek77/OpenUsenetServer). Security fixes go
into `main` and the next release (the version `openusenet version` prints, currently 0.8.0,
and the matching `jsevans/openusenet` Docker image).

| Version | Security support |
| ------- | ---------------- |
| `main` and the latest release / Docker image | :white_check_mark: |
| Older releases and images | :x: (upgrade to the latest first) |
| Forks (including `go-while/OpenUsenetServer`) | :x: (report against upstream `main`) |

The project is young and, in its author's words, not production ready. If you run an older
build, please check whether the problem is still present in `main` before reporting it; it may
already have been fixed.

## Reporting a Vulnerability

Please do **not** report security vulnerabilities through GitHub issues, pull requests,
discussions, Usenet newsgroups (such as `news.software.nntp` or `news.admin.peering`) or
mailing lists: they are all **public**.

Report them **privately** through
[GitHub Security Advisories](https://github.com/tgeek77/OpenUsenetServer/security/advisories/new)
for this repository. This opens a private report that only you and the maintainers (and the
collaborators they add) can see until it is resolved and disclosed.

Please include as much of the following as you can:

- what the vulnerability is and its impact (for instance remote code execution, SQL injection,
  an authentication or access bypass, a session or CSRF problem in the admin portal, a forged
  or unverified control message, command injection into the cleanfeed or mail helpers, path
  traversal in archive import or export, article loss or database corruption, denial of
  service);
- the affected part (the NNTP reader/feeder in `internal/nntp`, outgoing feeds in
  `internal/feed`, the admin web portal in `internal/admin`, the store in `internal/store`,
  control messages, archive import/export, cleanfeed, mail, inpaths, the `openusenet` command
  line, the Dockerfile or Compose setup) and the release, image or commit you tested, ideally
  the commit that introduced the problem;
- the configuration it needs (`config.yml`, environment variables, admin portal settings,
  peer and inbound settings);
- step-by-step instructions to reproduce it, or a minimal proof of concept;
- a patch, if you have one;
- your assessment of the severity and of what an attacker needs, for instance:
  - any host that can connect to the NNTP or HTTP port, before any authentication;
  - an authenticated reader (a user account);
  - an admin portal account;
  - an inbound peer (by default IHAVE and streaming are open to any remote, see
    `inbound` in `config.example.yml`);
  - an article that reaches the server through any feed, such as a control message, which
    can come from anywhere on Usenet;
  - local access to the host, the container, the PostgreSQL database or the spool.

Please do not disclose the issue publicly before a fix is available. If you have several
unrelated issues, please submit one report for each.

### Reporting from a script

When private vulnerability reporting is enabled for the repository, a report can also be
submitted through the GitHub API instead of the web form. Anyone with a GitHub account can
submit one; collaborator access is not required. `gh` must be logged in:

```sh
gh api -X POST repos/tgeek77/OpenUsenetServer/security-advisories/reports \
    -f summary='nntp: one-line title' \
    -f description="$(cat report.md)"
```

### Collaborators and their agents

Collaborators, and the AI agents working for them, submit verified findings through the web
form or the API above, one report per issue, and keep them out of shared chat rooms, public
issues and public files (such as [ISSUES.md](ISSUES.md)) until the advisory is published.
Maintainers triage submissions and decide how to proceed.

## What to Expect

- The maintainers will acknowledge your report within a few days.
- We will investigate, keep you informed, and work on a fix in the private advisory (with a
  private fork where needed).
- Once a fix is ready, we will agree with you on a date to merge it and publish the advisory,
  and request a CVE where appropriate.
- You will be credited in the advisory and the release notes, unless you prefer to remain
  anonymous.

## Scope

This policy covers the source code in this repository: the `openusenet` binary and its
packages under `internal/`, the admin portal, the scripts in `scripts/`, the sample
configuration, and the Dockerfile and Compose files.

Problems in third-party software that OpenUsenetServer uses (PostgreSQL, the Go standard
library and modules, cleanfeed-ng, Perl, Caddy, the base images, the operating system) should
be reported to those projects. If OpenUsenetServer uses such software in an unsafe way, that
is in scope here.
