# CLI workflows

This document describes the short company-oriented interface. Existing lower-level commands remain available for multi-entity books, consolidation, exact source lifecycle work, and forensic inspection.

## Noninteractive behavior

Every command is complete from arguments, flags, environment, configuration, and explicitly named input files. Books never asks a question after launch. It never reads stdin unless `--input -` is present.

Use `--dry-run` for supported previews and `--json` for the stable machine envelope. A missing required value, an unknown command or flag, an ambiguous account name, a stale plan, or a failed accounting invariant exits nonzero. Machine mode emits exactly one failure envelope, including for parser errors and blocked plans.

## Storage layout

The default layout is:

```text
~/.books/
├── books.toml
└── companies/
    └── acme/
        ├── ledger.sqlite
        ├── attachments/
        ├── backups/
        ├── imports/
        └── plans/
```

The registry contains paths and the immutable database UUID used for lineage checks rather than embedding company data. Relative paths are resolved from the directory containing `books.toml`. Existing registries that predate the UUID field are bound automatically on the next verified open of their live database. Config and newly created databases use owner-only file permissions; company directories use owner-only directory permissions.

`--db` and `BOOKS_DB` are reserved for advanced low-level commands that operate entirely from an explicit database. Company-oriented commands reject them so registry metadata and a different database can never be mixed. Configuration mutations use an interprocess lock; concurrent company creation and default updates are serialized without losing either successful change.

## Company setup

```sh
books init --name NAME [--company KEY] [--currency USD] [--basis accrual] \
  [--start YYYY-MM-01] [--fiscal-year-end MONTH] [--chart starter|empty]

books company add --name NAME [--company KEY] [same setup flags] [--default]
books company list
books company default KEY
books companies
books config path
books config get output
books config set output table|json|jsonl|csv
books config get defaults --company KEY
books config set defaults.payment-account ACCOUNT --company KEY
books config set defaults.deposit-account ACCOUNT --company KEY
books config set defaults.retained-earnings ACCOUNT --company KEY
```

`--start` must be the first day of the month following the fiscal-year-end month. When omitted, Books selects the current fiscal year. `--chart starter` creates accounts 1100, 2000, 3000, 3100, 4000, and 5000. `--chart empty` is useful before a QuickBooks import. Only accrual accounting is currently implemented; `--basis cash` fails with `BASIS_NOT_SUPPORTED` before creating configuration or company data.

## Account kinds

```sh
books account add KIND NAME... [--code CODE] [--active-from DATE]
```

| Kind | Ledger type | Automatic range | Reconciliation account |
|---|---|---:|---|
| `bank` | Asset | 1000–1090 | Bank |
| `ar` | Asset | 1100–1190 | No |
| `asset` | Asset | 1200–1490 | No |
| `fixed-asset` | Asset | 1500–1590 | No |
| `investment` | Asset | 1600–1690 | Investment |
| `ap` | Liability | 2000–2090 | No |
| `credit-card` | Liability | 2100–2190 | Credit card |
| `loan` | Liability | 2200–2290 | Loan |
| `liability` | Liability | 2300–2990 | No |
| `equity` | Equity | 3000–3990 | No |
| `income` | Revenue | 4000–4990 | No |
| `expense` | Expense | 5000–9990 | No |

Automatic codes advance by ten. Pass `--no-reconcile` to suppress statement-account creation, or `--reconcile-from DATE` to set its coverage boundary. `--default-payment`, `--default-deposit`, and `--retained-earnings` update the selected company's defaults.

## Routine entries

```sh
books spend AMOUNT ACCOUNT [DESCRIPTION...] [--from ACCOUNT] [--date DATE]
books receive AMOUNT ACCOUNT [DESCRIPTION...] [--to ACCOUNT] [--date DATE]
books transfer AMOUNT FROM TO [DESCRIPTION...] [--date DATE]
```

All three support `--memo`, `--reference`, `--key`, `--draft`, and `--dry-run`. Amounts are unformatted decimal values such as `42.50`; commas and currency symbols are intentionally rejected. Dates are ISO `YYYY-MM-DD`, `today`, or `yesterday`.

Without `--draft`, creation and posting are one database transaction. If a closed period, completed reconciliation, fiscal-year close, or another posting guard rejects the entry, no draft or journal lines remain from the failed command.

Account selectors try exact code, exact case-insensitive name, and then one unambiguous normalized name match. Ambiguity is an error; Books does not guess.

The default payment/deposit account resolves in this order:

1. explicit `--from` or `--to`;
2. the company default;
3. the only eligible active bank/card account;
4. an error requiring an explicit choice.

## Complex journals

```sh
books journal add --input PATH [--draft]
books journal add --input - [--draft]
```

Human journal JSON uses decimal debit and credit strings. `book` and `period` are inferred. Account fields accept codes or unambiguous names.

```json
{
  "posting_date": "2026-08-18",
  "description": "Owner contribution",
  "reference": "CAPITAL-001",
  "lines": [
    {"account": "Checking", "debit": "5000.00"},
    {"account": "Owner Equity", "credit": "5000.00"}
  ]
}
```

The lower-level `journal create`, `edit`, `validate`, `post`, `reverse`, `abandon`, `import`, and `post-batch` commands remain available and use internal IDs where exact forensic control requires them.

## Transaction lifecycle

```sh
books tx list [--from DATE] [--to DATE] [--status STATUS]
books tx show NUMBER
books tx post NUMBER
books tx abandon NUMBER
books reverse NUMBER [--date DATE] [--memo TEXT] [--draft]
books undo NUMBER [--date DATE] [--reason TEXT]
books correct NUMBER --reason TEXT --input JOURNAL.json [--draft]
```

`undo` abandons a draft or reverses a posted entry. `correct` first creates and validates both the linked reversal and replacement. Without `--draft`, it posts the reversal and then the replacement. If an unexpected storage failure occurs between those commits, the command returns `CORRECTION_PARTIAL` and identifies both human transaction numbers for recovery.

## Manual reconciliation

```sh
books reconcile plan ACCOUNT --through DATE --ending AMOUNT \
  [--start DATE] [--beginning AMOUNT] [--cleared all|none|NUMBER-LIST] [--out PATH]

books reconcile apply --plan PATH

books reconcile replan RECONCILIATION_ID --ending AMOUNT \
  [--cleared all|none|NUMBER-LIST] [--out PATH]
```

Start and beginning default from the previous completed reconciliation, or from the statement account's coverage date and ledger opening balance for the first reconciliation. Selected transaction numbers are attested as cleared; every omitted eligible control line is retained as a reviewed outstanding item. Plans show opening and ending outstanding totals plus adjusted beginning and ending balances. Apply requires the adjusted statement and book balances to agree, carries outstanding items forward, and allows a prior outstanding item to be selected when it clears on a later statement.

Plans bind exact internal line identities, both raw ledger boundary balances, the current statement population, the prior reconciliation identity and status, blockers, and a plan digest. Apply rechecks that complete snapshot and performs operator-attestation import, reconciliation creation or revision, allocation, outstanding-evidence recording, and completion in one immediate transaction. Failures roll back completely; exact completed retries are idempotent. After an audited `reconcile reopen`, `reconcile replan` targets that exact interval, preserves previously attested cleared activity, incorporates late postings, and produces the reviewed plan needed to reclose the period. Historical corrections use newest-to-oldest reopening followed by oldest-to-newest replanning; successor replans can revise their opening balance to the corrected prior statement ending under the same reopen-event and plan-digest guards.

The lower-level reconciliation interface remains available for imported bank evidence and many-to-many allocations.

## Reports

```sh
books gl [--from DATE] [--to DATE] [--account ACCOUNT]
books tb [--as-of DATE]
books pl [--from DATE] [--to DATE]
books bs [--as-of DATE]
```

Without an explicit entity or group, the selected company's entity is used. As-of reports default to today within the configured periods. Profit and loss defaults to the current fiscal year; general ledger defaults to the current period. Every report scope and scoped book explicitly declares `basis: "ACCRUAL"`; unsupported non-accrual books fail closed.

`gl --account` accepts either an exact account code or the same unambiguous account-name selector used by transaction commands.

## Close workflows

```sh
books close plan PERIOD [--out PATH]
books close apply --plan PATH
books year-close plan YEAR [--retained-earnings ACCOUNT] [--out PATH]
books year-close apply --plan PATH
books reopen PERIOD --reason TEXT
```

The plan/apply boundary is mandatory for the short close interface. Planning runs every current close preflight—including all earlier fiscal-year periods being closed before a year-close plan can become ready—and captures the exact ledger or derived-journal digest. Applying re-runs the preflight and rejects stale state. The derived closing journal is created and posted atomically, so a failed apply leaves no closing draft. Exact apply retries are idempotent: stored close evidence or the source-bound closing journal must match the plan.

## QuickBooks initial import

```sh
books import quickbooks inspect --from PATH [--accounts Account.json] \
  [--mode auto|general-ledger|objects|journal] [--start DATE] [--through DATE]

books import quickbooks plan [same source flags] [--out PATH]
books import quickbooks apply --plan PATH [--draft]
```

`auto` chooses `GeneralLedger.json` when it exists in a directory, otherwise a QBO object directory; file extensions select GeneralLedger JSON or journal XLSX. JSON date bounds are inferred. XLSX imports require explicit bounds unless another source declares them.

Unsupported or unresolved QuickBooks content is a blocking diagnostic, not a partial guess. The supplied account catalog is setup evidence, not merely a journal lookup: every active account is planned and imported even when it has no activity in the selected interval. Apply creates required statement controls for active imported bank, credit-card, investment, and loan accounts from the reviewed import start date, so unreconciled imported controls block close. Apply also re-runs the importer from the retained paths and compares a deterministic digest before writing. That deterministic import digest identifies the batch, so regenerating an unchanged plan can post a previously imported draft instead of creating an empty second batch.

## Backup, restore, and verification

```sh
books doctor
books audit list [--limit N]
books audit verify
books backup [--out PATH]
books restore --from PATH --dry-run
books restore --from PATH --confirm COMPANY
```

The default backup name is timestamped beneath the company backup directory. A destination is never overwritten. Restore verifies the staged backup's database UUID and configured entity/book identity against `books.toml`, including when the target database is missing, requires the exact selected company key, and creates a pre-restore backup when a target exists. Its result and immutable audit event identify the installed source by SHA-256, database UUID, and legal entity/book metadata.
