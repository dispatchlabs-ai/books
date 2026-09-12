# Working on Books

## Find the relevant contract

Read [README.md](README.md) and the instructions nearest the files being changed.
Use [architecture](docs/architecture.md) for the ledger model,
[CLI documentation](docs/cli.md) for commands, [API documentation](docs/api.md)
for client integration, and [operations](docs/operations.md) for recovery.
Consult [currency rules](docs/currencies.md) and [statement formats](docs/statement-formats.md)
when those features are involved. Check the implementation and tests before
asserting that a feature or platform works.

## Protect financial data

Development must never access an existing Books database, even for inspection.
Create a new temporary directory for each test session. Explicitly point
`BOOKS_HOME` and `BOOKS_CONFIG` into it and set `BOOKS_ACTOR` before running Books.
Use invented financial records; remove only the temporary directory you created.
Keep private records, credentials, and deployment identifiers out of commits and
public reports. Contributor code must run without access to private credentials.

Accounting changes belong in the shared application and ledger services, not in
transport-specific code. Use those services for test setup instead of writing SQL.
Check exact integer amounts, balanced entries, immutable posted records, atomic
writes, and retry behavior. A schema migration, destructive repair, or breaking
public contract needs a maintainer decision before implementation.

## Verify changes

Development prerequisites and the canonical check are in
[CONTRIBUTING.md](CONTRIBUTING.md). Run `./scripts/check` before committing code,
build, workflow, dependency, or executable-example changes. This includes the
real CLI smoke test with disposable data. Keep Linux and macOS working; native
Windows support has not been implemented.

For a bug fix, test the reported failure and its relevant neighboring workflows.
Obtain an independent agent review for nontrivial code changes, address confirmed
findings, and repeat review after repairs. Documentation-only changes need diff
and link checks plus validation of any commands they change. Report missing or
failed checks accurately. Human accountability remains with the maintainer.

## Complete the work

Chris's implementation requests include checking, committing, and pushing to
`main`. Preserve other work and stage only the task's files. Verify the remote
commit after pushing. Use a pull request only when requested; external
contributions follow [CONTRIBUTING.md](CONTRIBUTING.md) and
[AI_POLICY.md](AI_POLICY.md). A review request does not authorize changes.
History rewrites and releases require specific user authorization.

Follow [SECURITY.md](SECURITY.md) for vulnerability reports and service boundaries.
Keep reports private, and do not publish a release, tag, or artifact as part of
an ordinary fix. [GOVERNANCE.md](GOVERNANCE.md) identifies decision ownership.
