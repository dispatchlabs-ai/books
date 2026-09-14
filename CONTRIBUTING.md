# Contributing to Books

Books is an experimental accounting system. Small, well-understood changes with
clear evidence are welcome. Large speculative rewrites are not.

## Before starting

1. Become a user of the command or workflow you want to change.
2. Search existing issues.
3. Record substantial behavior, schema, dependency, or architecture decisions.
   Maintainer-directed work does not require an issue or pull request.
4. Never share real financial data. Reproduce with a new disposable
   `BOOKS_HOME` and synthetic companies.

## Development

Books requires Go 1.26.6 or newer, CGO, a C compiler, a POSIX shell, and
[ripgrep](https://github.com/BurntSushi/ripgrep#installation) (`rg`) on `PATH`. The web checks also require Node.js 26.5+ and npm; browser checks
require an installed Playwright Chromium (see [web checks](web/README.md#checks)).
Maintainer-directed
work defaults to committing and pushing directly to `main`; agents open pull
requests only when explicitly asked. External contributors use a branch and
pull request. Run the checks locally:

```sh
./scripts/check
```

That command is the canonical validation entrypoint. It uses a disposable Books
home and runs formatting, module integrity, tests, race tests, vet, lint, and
known-vulnerability checks. Maintainers run this gate on their own equipment;
GitHub Actions is disabled for this repository. Check relevant changes on both
Linux and macOS, and include results with the reviewed commit. Do not add hosted
CI unless the maintainer requests it.

A maintainer-hosted Linux runner is enrolled and automatically checks new `main`
heads from disposable clones, publishing a status on the exact tested commit.
It polls rather than queues every push, so intermediate heads may be skipped;
macOS checks are run separately. The prepared `.woodpecker.yml` runs the same gate
in a pinned container, but Woodpecker enrollment/cutover has not been completed.
Neither build requires repository secrets or private data. Pull requests do not
automatically run on the maintainer's machines; run the local gate and include
its result.

The gate validates the privacy scanner with synthetic fixtures. To additionally
scan for deployment-specific names, set `BOOKS_PRIVATE_DENYLIST` to an absolute
path to a private file outside the checkout, with one literal term per line.
Never commit that file or its terms. Without this variable, the gate explicitly
reports that the optional deployment-name scan was skipped. With it, matches,
unreadable/empty files, and scanner errors fail the gate. This current-tree scan
is not a secret scanner or a public-history audit; review both separately before
release. Do not rewrite published history without a maintainer-approved plan.

Follow `AGENTS.md` whether you work manually or with an agent.

## Pull requests

A pull request must include:

- the problem and user impact;
- scope and explicit non-goals;
- accounting, migration, security, privacy, and compatibility implications;
- deterministic tests;
- documentation and changelog updates where behavior changes;
- the exact `./scripts/check` result; and
- AI-assistance disclosure under `AI_POLICY.md`.

The submitting human must understand every changed line and accept responsibility
for its correctness and maintenance. Generated code that the submitter cannot
explain will not be accepted.

Changes to accounting invariants, SQLite migrations, import parsing, release
automation, or GitHub workflows require explicit maintainer review. New
dependencies require a license, security, maintenance, and necessity rationale.

Unless explicitly stated otherwise, contributions intentionally submitted for
inclusion are licensed under MIT as described in `LICENSE`.
