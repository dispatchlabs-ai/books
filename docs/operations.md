# Operations

## Environment

`BOOKS_DB` selects the database. `BOOKS_ACTOR` records the human or process responsible for each mutation. Both can be overridden by global flags.

```sh
export BOOKS_DB=/absolute/path/to/books.sqlite
export BOOKS_ACTOR="operator name"
```

Use an absolute database path for production work. Live databases, exports, statements, and credentials do not belong in the Git repository.

Dry-run validation is implemented for database migration and restore, journal posting and batch posting, period close and year close, and reconciliation completion or abandonment. Statement-account archive, identity-add, and precoverage-closure commands validate in preview mode until `--commit` is supplied. Other mutating commands reject `--dry-run` with `DRY_RUN_UNSUPPORTED`; they never echo unvalidated input as a successful preview.

## Routine workflow

1. Run `books db doctor` and `books audit verify`.
2. Import or create drafts.
3. Use the command's `--dry-run` mode where supported.
4. Validate drafts, then post them.
5. Review the general ledger, trial balance, financial statements, source-only items, and unallocated statement transactions.
6. Reconcile required control accounts.
7. Create a verified backup.
8. Close periods in chronological order.

Useful review commands:

```sh
books journal list --book ENTITY --from 2026-01-01 --to 2026-01-31
books source list --disposition NEEDS_REVIEW
books source list --disposition PENDING
books source list --history
books transaction list --unallocated
books statement-account list --entity ENTITY
books statement-account identity list --entity ENTITY
books statement-account lifecycle list --entity ENTITY
books report trial-balance --entity ENTITY --as-of 2026-01-31
```

Create statement controls with the first date that formal reconciliation is required for period close. Archive them with the last required date; these bounds are reconciliation policy, not claims about the provider account's legal lifetime:

```sh
books statement-account create \
  --code ENTITY-CASH --entity ENTITY --book ENTITY --account 1000 \
  --name "Operating account" --kind BANK --currency USD \
  --reconcile-from 2026-01-01

books statement-account archive --code ENTITY-CASH \
  --reconcile-through 2026-12-31 --reason "Control replaced" --commit
```

Record each provider or registry alias separately. `identity add` validates the statement-account target, uniqueness, and evidence without writing until `--commit` is supplied. A precoverage certificate freezes the complete alias set: retrying an existing identical mapping remains idempotent, but any new identity is rejected even if inactive:

```sh
books statement-account identity add \
  --statement-account ENTITY-CASH --source-system FINANCES --source-realm GLOBAL \
  --external-id source-account-id --account-number 0110 \
  --name "Operating account" --source-kind SQLITE_ACCOUNT_REGISTRY \
	  --source-path /absolute/path/to/account-registry.sqlite \
  --source-sha256 64_CHARACTER_SHA256 --locator 'accounts#source-account-id'

books statement-account identity add \
  --statement-account ENTITY-CASH --source-system FINANCES --source-realm GLOBAL \
  --external-id source-account-id --account-number 0110 \
  --name "Operating account" --source-kind SQLITE_ACCOUNT_REGISTRY \
	  --source-path /absolute/path/to/account-registry.sqlite \
  --source-sha256 64_CHARACTER_SHA256 --locator 'accounts#source-account-id' \
  --commit
```

An account legally closed before `reconcile-from` has no later monthly statement. Use the lifecycle command only when the retained input identifies one active exact statement-account alias, every other active alias has been reviewed, the official closure letter predates required coverage, an official provider snapshot reports the same account as archived or closed with exact-zero current and available balances, and the Books control is zero with no later activity. Active aliases in the selected provider realm must identify the same provider account. Every current statement-source observation must already be `POSTED` with its exact materialization or explicitly resolved `SOURCE_ONLY`; `PENDING` and `NEEDS_REVIEW` are certification blockers. The CLI hashes both source files, locates exactly one provider snapshot object by the mapped external identity, checks its payload hash, holder, suffix, status, and balances, computes the complete active identity count and digest, then repeats every accounting guard inside the transaction.

Retain one absolute-path JSON input per account:

```json
{
  "statement_account": "ENTITY-LEGACY-CASH",
  "identity": {
    "source_system": "PROVIDER_API",
    "source_realm": "GLOBAL",
    "external_id": "provider-account-id"
  },
  "closed_on": "2025-09-19",
  "closure_evidence": {
    "source_kind": "PROVIDER_CLOSURE_LETTER",
    "source_path": "/absolute/path/to/closure-letter.pdf",
    "source_sha256": "64_CHARACTER_SHA256",
    "locator": "page 1; account suffix and effective closure date"
  },
  "zero_evidence": {
    "source_kind": "PROVIDER_ACCOUNT_SNAPSHOT",
    "source_path": "/absolute/path/to/provider-accounts.json",
    "source_sha256": "64_CHARACTER_SHA256",
    "locator": "exact provider account object",
    "payload_sha256": "64_CHARACTER_SHA256",
    "observed_on": "2026-04-28",
    "provider_status": "ARCHIVED",
    "current_balance": "0.00",
    "available_balance": "0.00"
  },
  "account_holder": "Entity, Inc.",
  "account_suffix": "1234",
  "reason": "Provider closed the exact zero-balance account before required reconciliation coverage"
}
```

Preview, commit, inspect, and prove the overlapping rerun is unchanged:

```sh
books statement-account lifecycle close-before-coverage \
  --input /absolute/path/to/close-legacy-cash.json
books statement-account lifecycle close-before-coverage \
  --input /absolute/path/to/close-legacy-cash.json --commit
books statement-account lifecycle list \
  --statement-account ENTITY-LEGACY-CASH
books statement-account lifecycle close-before-coverage \
  --input /absolute/path/to/close-legacy-cash.json --commit
```

The final command must return the same lifecycle ID with `changed: false` and append no audit event. A nonzero Books control, a missing or mismatched identity, changed evidence bytes, a closure date within required coverage, unresolved current source evidence, any later control or statement activity, or any open/unmaterialized work is a blocker. Commit archives the statement account, freezes its complete identity set, blocks later source observations and revisions, and makes its completed reconciliations ineligible for reopen. Doctor and supported period close require the immutable certificate, identity-set binding, exactly one consistent audit event for each, and a valid complete audit hash chain. This lifecycle does not post a journal, create a reconciliation, or clear a residual.

Statement imports preserve a revision whenever the same provider identity changes. A provider's pending-to-posted update needs no manual resolution fields. Decisions such as treating a removed pending authorization as source-only or posting an item held for review require explicit evidence in the import row:

```json
{
  "statement_account": "ENTITY-CASH",
  "source_system": "FINANCES",
  "source_name": "review-resolution.json",
  "transactions": [{
    "external_id": "provider-transaction-id",
    "posted_date": "2026-07-31",
    "description": "Customer receipt",
    "amount": "1250.00",
    "disposition": "POSTED",
    "resolution_reason": "Matched to the July provider statement",
    "resolution_evidence": {
      "statement_page": 3,
      "provider_trace": "trace-42"
    },
    "raw_json": {
      "id": "provider-transaction-id",
      "status": "posted"
    }
  }]
}
```

Import it through `books statement import --input review-resolution.json`. `books source list` shows current observations; add `--history` to inspect every superseded revision. `books source show --id SOURCE_RECORD_ID` reports revision and evidence hashes without exposing raw JSON.

If open reconciliation work was started with the wrong dates or balances, abandon it with retained audit evidence and start a corrected reconciliation:

```sh
books reconcile abandon --id RECONCILIATION_ID \
  --reason "Incorrect opening balance" --commit
```

If a completed reconciliation was explicitly reopened for a late posting, revise that same interval through the short plan/apply workflow instead of abandoning it or starting an overlapping interval:

```sh
books reconcile replan RECONCILIATION_ID \
  --ending 12420.23 --cleared all \
  --out rereconcile-2026-08.json
books reconcile apply --plan rereconcile-2026-08.json
```

The replan preserves previously attested cleared allocations, requires every current residual line to be reviewed as cleared or outstanding, and binds the target's prior balances, reopen event, and evidence population against staleness. For an older correction, reopen later periods and reconciliations from newest to oldest; after correcting the oldest affected interval, replan and apply each interval from oldest to newest so corrected statement endings become the successors' audited opening balances.

## Backup and restore

Backup requires a writable source because the backup path and digest are recorded in the ledger's audit trail. The destination must not already exist.

```sh
books db backup --out /absolute/path/to/books-2026-01-31.backup
```

Restore validates and stages the exact source bytes first, creates a pre-restore backup when a target exists, and requires the exact absolute target as confirmation:

```sh
books db restore \
  --from /absolute/path/to/books-2026-01-31.backup \
  --confirm /absolute/path/to/books.sqlite
```

The advanced direct-database command requires the source database UUID to match an existing target's UUID; at a new path it explicitly adopts the source lineage. The company-oriented `books restore` is stricter: it binds the staged source to the UUID persisted in `books.toml`, so it remains safe when the registered target is missing or has been replaced. Restore results and audit evidence include the source SHA-256, source UUID, prior target UUID when present, and source entity/book identities.

## Closing and correction

Close ordinary periods in date order. For a fiscal year, close the earlier periods, preview the system-derived closing entry, post it, then close the marked year-end period.

```sh
books period year-close --book ENTITY --year 2026 \
  --retained-earnings 39000 --dry-run
books period year-close --book ENTITY --year 2026 \
  --retained-earnings 39000
books period close --book ENTITY --period 2026-12 --dry-run
books period close --book ENTITY --period 2026-12
```

To correct a closed fiscal year, reopen periods in reverse chronological order, reverse the exact closing journal, post the correction, and run the year-close workflow again. Every action remains in the audit log.

Run close and reopen transitions through the CLI. SQLite enforces the accounting preconditions as defense in depth, but a direct SQL update cannot produce the service-owned balance digest and audit-chain event and is therefore unsupported.

## Failure handling

- `DATABASE_SCHEMA_DRIFT` means a table, index, view, or trigger differs from the embedded application schema. Stop writes and restore a verified database; do not repair the schema by hand.
- `DATABASE_TARGET_CHANGED` means the path, filesystem identity, or Books database UUID changed between read-only migration inspection and writable open. No migration DDL was attempted; stop and verify the intended file and symlink target.
- `MIGRATION_REQUIRED` means a supported future public migration must be applied before use.
- `DATABASE_TOO_NEW` means the file's schema version is newer than this CLI supports. Stop and install a compatible newer Books release; never relabel migration metadata by hand.
- `JOURNAL_INVALID` or `PERIOD_CLOSE_BLOCKED` includes the accounting condition that must be resolved.
- `RECONCILIATION_NOT_BALANCED` means balances or signed allocations are incomplete.
- A nonzero suspense balance intentionally blocks close.

Stable error categories map to nonzero exit codes, making JSON output suitable for shell automation.
