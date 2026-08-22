# Books

Books is a local-first, command-line general ledger for humans and software agents. It keeps exact double-entry books in SQLite, makes posted accounting immutable, and provides a short noninteractive workflow for manual bookkeeping alongside the lower-level multi-entity and evidence-import interfaces.

Books is experimental source software. It includes company setup, a manual chart of accounts, manual transaction entry, bank/card/loan/investment reconciliation, general ledger, trial balance, profit and loss, balance sheet, period and fiscal-year close, optional QuickBooks initial import, consolidation, audit verification, and verified backup/restore.

Books currently supports USD functional currency on macOS and Linux. It is not accounting, tax, or legal advice. Keep independent backups and have a qualified professional review accounting outputs before relying on them.

## Build

Building requires Go 1.26.6 or newer, CGO, and a working C compiler for the SQLite driver.

Install the current development source. There is no tagged release yet; pin a
commit instead of `main` when you need a reproducible installation:

```sh
go install github.com/dispatchlabs-ai/books/cmd/books@main
```

Or build a local checkout:

```sh
go build -o dist/books ./cmd/books
```

The examples below assume `dist/books` is available as `books` on `PATH`.

## Start a company manually

One command creates the default config, an isolated company database, an actual book, twelve monthly periods, and a small starter chart:

```sh
books init \
  --name "Acme Services, Inc." \
  --company acme \
  --currency USD \
  --basis accrual \
  --start 2026-01-01 \
  --fiscal-year-end december
```

By default, Books writes the registry to `~/.books/books.toml` and this company's data to `~/.books/companies/acme/`. Nothing needs to be created in the current directory.

Books currently implements accrual accounting only. `--basis cash` is rejected rather than producing statements whose accounting basis would be ambiguous or misleading.

Books currently supports USD only. Any other functional currency is rejected rather than being represented with an incorrect two-decimal minor-unit assumption.

The starter chart contains accounts receivable, accounts payable, owner equity, retained earnings, revenue, and general expense. Add the accounts that are specific to the company:

```sh
books account add bank Checking \
  --default-payment \
  --default-deposit

books account add credit-card "Business Visa"
books account add bank Savings
books account add income "Consulting Revenue"
books account add expense Software
books accounts
```

Account codes are assigned from conventional ranges when `--code` is omitted. Bank, credit-card, loan, and investment accounts automatically receive a linked reconciliation account unless `--no-reconcile` is supplied.

For an empty chart instead, initialize with `--chart empty`. Additional fiscal years are one command:

```sh
books periods add 2027
```

## Enter transactions

Routine two-sided transactions post immediately. Dates default to today, descriptions have useful defaults, and a configured or sole bank/card account can be inferred:

```sh
books spend 42.50 Software "Monthly hosting"
books receive 2500 "Consulting Revenue" "Invoice 1042"
books transfer 1000 Checking Savings
```

Use flags only when the default is not right:

```sh
books spend 86.14 Meals \
  --from "Business Visa" \
  --date 2026-08-17 \
  --reference receipt-9821

books receive 800 Revenue \
  --to Checking \
  --key stripe-payout-2026-08-18
```

`--key` is an optional idempotency key for human use. Agents and retrying automation must always supply a stable `--key` for mutating transaction commands. `--draft` saves without posting, and `--dry-run` validates and previews without writing.

Inspect and manage entries by their short, per-book transaction number:

```sh
books tx list
books tx show 12
books tx post 13
books tx abandon 14
books reverse 12 --date today --memo "Duplicate charge"
books undo 13 --reason "Entered in the wrong company"
```

Posted entries and their lines never change. `reverse`, `undo`, and `correct` preserve that rule by creating linked reversals. A correction takes the replacement as explicit JSON:

```sh
books correct 12 \
  --reason "Wrong expense account" \
  --input corrected-journal.json
```

For a split or otherwise complex entry, use `journal add`. The selected company supplies the book, and the date supplies the period, so those fields may be omitted:

```json
{
  "posting_date": "2026-08-18",
  "description": "Allocate annual insurance",
  "lines": [
    {"account": "Insurance", "debit": "100.00"},
    {"account": "Prepaid Insurance", "credit": "100.00"}
  ]
}
```

```sh
books journal add --input journal.json
printf '%s\n' "$JOURNAL_JSON" | books journal add --input -
```

Books never reads stdin unless the command explicitly receives `--input -`.

## Reconcile an account manually

Reconciliation uses a reviewable plan so an incorrect balance or transaction selection cannot commit accidentally:

```sh
books reconcile plan Checking \
  --through 2026-08-31 \
  --ending 12450.23 \
  --cleared all
```

The default plan path is printed and stored beneath the selected company's `plans/` directory. Review it, then apply exactly that file:

```sh
books reconcile apply \
  --plan ~/.books/companies/acme/plans/reconcile-1000-2026-08-31.json
```

The plan records the exact transaction and control-line identities, ledger beginning and ending balances, statement population, and prior reconciliation boundary. It is SHA-256 protected and becomes stale if any bound state changes. Apply rechecks that complete state under the database write lock, then creates immutable manual statement evidence whose external ID and raw payload name the exact journal entry and line, allocates it, and completes the reconciliation in one transaction. Doctor rechecks that identity binding, so an unrelated journal with the same account, date, and amount cannot substitute. A stale, mismatched, or unbalanced apply leaves no partial evidence, open reconciliation, or allocation behind.

`--cleared` accepts `all`, `none`, comma-separated transaction numbers, and inclusive ranges such as `4,7-10`. Any eligible control-account line that is not selected is retained as a reviewed outstanding deposit or payment. The plan shows the opening and ending outstanding totals and requires the adjusted statement and book balances to agree exactly. Outstanding items carry into later plans and can be selected when they clear on a later statement.

Credit-card and loan statement balances may be entered as the positive amount owed; Books converts them to the ledger's signed liability convention.

If a completed reconciliation is reopened to admit a late posting, rebuild that exact interval instead of starting a new one:

```sh
books reconcile reopen \
  --id RECONCILIATION_ID \
  --reason "Late bank fee"

books reconcile replan RECONCILIATION_ID \
  --ending 12420.23 \
  --cleared all

books reconcile apply \
  --plan ~/.books/companies/acme/plans/rereconcile-1000-2026-08-31.json
```

For a historical correction, reopen later closed periods and reconciliations from newest to oldest, then replan/apply them from oldest to newest. Each successor replan carries forward the corrected prior statement ending as its revised opening balance while preserving already attested cleared allocations.

## Reports

The short report commands default to the selected company's entity and sensible dates:

```sh
books gl
books tb
books pl
books bs
```

Every scope and date can still be explicit:

```sh
books gl --from 2026-08-01 --to 2026-08-31 --account 1000
books tb --as-of 2026-08-31
books pl --from 2026-01-01 --to 2026-08-31
books bs --as-of 2026-08-31
```

The full forms remain under `books report`, including entity and consolidation-group scopes. Every report declares `basis: "ACCRUAL"` in its resolved scope and on each scoped book.

## Close the books

Period close and fiscal-year close also separate planning from commitment:

```sh
books close plan 2026-08
books close apply \
  --plan ~/.books/companies/acme/plans/close-2026-08.json

books year-close plan 2026
books year-close apply \
  --plan ~/.books/companies/acme/plans/year-close-2026.json
```

A close plan binds the selected company, period, and current ledger digest. Apply rejects a changed plan or a changed ledger. Reopening requires an explicit audit reason:

```sh
books reopen 2026-08 --reason "Late bank correction"
```

Applying the same completed period-close or fiscal-year-close plan again is safe. Books returns the already committed result when its stored evidence exactly matches the reviewed plan, and rejects conflicting evidence.

## Initial import from QuickBooks

QuickBooks is optional. For a new import, an empty initial chart avoids accidental code conflicts:

```sh
books init \
  --name "Acme Services, Inc." \
  --company acme \
  --start 2023-01-01 \
  --chart empty
```

Books accepts a QuickBooks `GeneralLedger` JSON report, a directory of QBO object JSON exports containing `Account.json`, or a reviewed journal XLSX with an accompanying account catalog:

```sh
books import quickbooks inspect \
  --from /exports/quickbooks/GeneralLedger.json \
  --accounts /exports/quickbooks/Account.json

books import quickbooks plan \
  --from /exports/quickbooks/GeneralLedger.json \
  --accounts /exports/quickbooks/Account.json

books import quickbooks apply \
  --plan ~/.books/companies/acme/plans/quickbooks-2023-01-01-2023-12-31.json
```

Inspect and plan do not change the ledger. Apply rebuilds the source plan and rejects changed exports, creates missing monthly periods and every active account in the supplied catalog (including zero-activity accounts), records immutable QBO account identities and evidence hashes, imports the journals idempotently, and posts the whole batch atomically. Imported bank, credit-card, investment, and loan accounts receive required statement controls beginning at the reviewed import start date, so period close remains blocked until their reconciliation coverage is complete. The batch identity comes from deterministic imported content, so a newly generated plan for unchanged source can safely finish a prior draft apply. Use `--draft` when a reviewed plan should still land as drafts.

## Companies and configuration

List and select registered companies without changing directories:

```sh
books companies
books company add --name "Second Company LLC" --company second --start 2026-01-01
books company default second
books --company acme tb
```

Configuration resolution order is:

1. `--config PATH`
2. `BOOKS_CONFIG`
3. `$BOOKS_HOME/books.toml`
4. `~/.books/books.toml`

Agents should normally pass `--company` explicitly. Humans can rely on `default_company`. `BOOKS_DB` and `--db` remain available only for advanced low-level commands that derive their context directly from one database. Registered-company and human workflow commands reject a database override instead of combining registry metadata with another SQLite file.

Repository tests and agent work must set `BOOKS_HOME` to a new disposable directory. They must never probe `~/.books` or an existing SQLite file.

## Automation contract

The CLI has no prompts, menus, editor launches, pagers, TUI, or implicit stdin reads. Missing or ambiguous data fails with a nonzero status. Behavior is the same under a terminal, a pipe, and CI.

Table output is the human default. JSON, JSONL, and CSV are explicit:

```sh
books --company acme tx list --json
books --company acme tb --format csv
```

JSON uses the `books.cli/v1` envelope. Money fields are exact decimal strings, errors have stable codes, and human commands identify entries by transaction number rather than exposing SQLite UUIDs. The contract also covers command-discovery, flag, and argument errors. A failed command emits one failure envelope; a blocked plan is retained at its reported path without first emitting a contradictory success result.

Set a persistent output default when every invocation should be machine-oriented; an explicit `--format` or `--json` still wins:

```sh
books config set output json
```

## Operations

```sh
books doctor
books audit verify
books backup
books backup --out /secure/acme-2026-08-31.sqlite
books restore --from /secure/acme-2026-08-31.sqlite --dry-run
books restore --from /secure/acme-2026-08-31.sqlite --confirm acme
```

Restore validates the exact source bytes first and requires their immutable database UUID plus entity/book identity to match the selected company's registry entry. The UUID remains available when the live database is missing or has been replaced, so disaster recovery cannot silently adopt another same-code company's books. Restore requires the exact selected company key as noninteractive confirmation, creates a recoverable pre-restore backup when a target exists, and records the source SHA-256 and both database identities in the audit log.

## Accounting guarantees

- USD money is stored only as signed 64-bit integer minor units (cents); accounting never uses floating point.
- A journal cannot post unless it balances, uses an open period, and uses accounts enabled for that book and date.
- Posted journals, imported evidence, reconciliations, close evidence, and audit events are immutable. Corrections use linked reversals.
- Imported identities are realm-aware, evidence-backed, revision-safe, and idempotent.
- Every completed reconciliation exactly allocates its statement activity to posted control-account lines.
- Consolidation membership comes from effective-dated 100% ownership, with explicit elimination books and entries.
- Closed periods retain a balance digest, and the audit log is hash chained. The in-database chain detects uncoordinated changes but does not resist a privileged party that can rewrite and replace the entire SQLite file.
- Every database open verifies the application ID, migration ledger, migration checksums, and live SQLite schema.

The CLI is the supported write interface. Direct SQLite writes are unsupported.

## Product boundary

Books does not implement invoicing, A/R or A/P workflow, bill pay, payroll processing, inventory, tax filing, automatic bank feeds, non-USD functional currencies, currency translation, partial ownership/noncontrolling interests, or a web interface. Those balances and activities can still be represented in the general ledger where they fit the supported USD accrual model.

Books databases, attachments, plans, and backups are local files and are not encrypted by Books. Protect them with operating-system permissions, full-disk encryption, and an appropriate backup policy. Never attach real financial data to a public issue.

## Contributing and security

Read [CONTRIBUTING.md](CONTRIBUTING.md) before proposing a change and [AGENTS.md](AGENTS.md) before using a coding agent. Report vulnerabilities through the private process in [SECURITY.md](SECURITY.md), not a public issue.

Books is licensed under the [MIT License](LICENSE).

See [CLI workflows](docs/cli.md), [architecture](docs/architecture.md), [banking data standard](docs/banking-data.md), [operations](docs/operations.md), and [migration controls](docs/migration.md) for more detail.
