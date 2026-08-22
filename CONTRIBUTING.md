# Contributing to Books

Books is an experimental accounting system. Small, well-understood changes with
clear evidence are welcome. Large speculative rewrites are not.

## Before starting

1. Become a user of the command or workflow you want to change.
2. Search existing issues.
3. Open a scoped issue before substantial behavior, schema, dependency, or
   architecture work.
4. Never share real financial data. Reproduce with a new disposable
   `BOOKS_HOME` and synthetic companies.

## Development

Books requires Go 1.26.6 or newer, CGO, and a C compiler. Clone the repository,
create a branch, and run:

```sh
./scripts/check
```

That command is the canonical validation entrypoint. It uses a disposable Books
home and runs formatting, module integrity, tests, race tests, vet, lint, and
known-vulnerability checks.

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
