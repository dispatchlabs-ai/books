# Client API and statement imports

Books can run as a headless backend for a web, Electron, or mobile client. The
CLI remains available without a server. This is an experimental v1 integration
surface, not a hosted multi-tenant service. See [the design](backend-design.md),
[OpenAPI](schemas/books-api-v7.openapi.json), and the small
[TypeScript transport example](examples/books-client.ts).

The new OpenAPI artifact is an additive contract snapshot (1.6.0); routes and
response envelopes remain v1. The [1.5.0 snapshot](schemas/books-api-v6.openapi.json), [1.4.0 snapshot](schemas/books-api-v5.openapi.json), [1.3.0 snapshot](schemas/books-api-v4.openapi.json), [1.2.0 snapshot](schemas/books-api-v3.openapi.json), [1.1.0 snapshot](schemas/books-api-v2.openapi.json) and
[initial snapshot](schemas/books-api-v1.openapi.json) remain unchanged.

## Run locally

Initialize the company and chart using the existing CLI. For a disposable demo:

```sh
export BOOKS_HOME="$(mktemp -d)"
export BOOKS_CONFIG="$BOOKS_HOME/books.toml"
export BOOKS_ACTOR=demo
books init --name 'Example Company' --company example --start 2026-01-01
books account add bank Checking
```

Create a high-entropy bearer token and a private server config. This example
keeps the credential in a separate file and prints no secrets:

```sh
python3 - <<'PY'
import hashlib, json, os, secrets
from pathlib import Path
home = Path(os.environ['BOOKS_HOME'])
token = secrets.token_urlsafe(32)
for name, data in {
    'client-token': token,
    'server.json': json.dumps({
        'schema': 'books.server/v2', 'listen': '127.0.0.1:8484',
        'allowed_origins': ['http://localhost:3000'],
        'principals': [{
            'id': 'demo-client',
            'token_sha256': hashlib.sha256(token.encode()).hexdigest(),
            'companies': {'example': ['read', 'import', 'post', 'manage']}
        }]
    }, indent=2)
}.items():
    path = home / name
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'w') as f:
        f.write(data)
PY
books serve --server-config "$BOOKS_HOME/server.json"
```

The server config must be a private regular file (no group/other permissions).
Use one random token per principal. The server stores SHA-256 digests, not
password verifiers: short or human-chosen passwords are unsuitable. All
requests, including health, need `Authorization: Bearer <token>`.

Non-loopback IP listeners require `tls_certificate` and `tls_key`. Browser
origins must be exact entries; HTTPS is required except local development.
No origin is allowed by default. The service does not set authentication
cookies. Provision and rotate credentials outside the API; restart the server
to load changed grants, tokens, company bindings, certificates, or origins.
Credentials confer all listed permissions until that restart. Keep client
secrets in native secure storage or a trusted web backend; do not embed a
shared posting token in distributed JavaScript or an Electron renderer bundle.

## Permissions and scopes

Server configuration `books.server/v2` supports these company grants:

| Grant | Operations |
| --- | --- |
| `read` | Queries, reports, and reconciliation/close plan generation |
| `import` | Statement upload, preview, and source-only import apply |
| `post` | Routine posting, journal status/correction/reversal, reconciliation apply; posting imported journals also requires `import` |
| `manage` | Chart accounts, defaults, fiscal periods, reopen and period-close apply |

Every grant set includes `read`. Applying a year-end close or changing a closing
journal requires both `manage` and `post`. In v2, `post` can be granted without
`import`. Existing `books.server/v1` configurations keep their original rule
that `post` requires `import`, and do not accept `manage`.

Principals supply the audit actor; JSON cannot specify an actor or database path.
`GET /v1/companies` lists only granted registry keys. All company routes use
`/v1/companies/{company}`. An unavailable company or foreign journal,
reconciliation, job, source, or plan returns 404. Scope remains enforced when
companies share one SQLite file. Company registry entries bind the database UUID,
active actual book, and entity; startup fails if a binding is invalid.

## Shared accounting workflows

The CLI calls `internal/application` in-process. HTTP calls the same company-bound
workflows. HTTP uses explicit ISO dates; CLI conveniences such as `today` and
transaction-number ranges are parsed by the CLI. Requests use JSON content type
and a 2 MiB limit. Unknown/duplicate fields are rejected. Decimal amounts are
strings, for example `"12.34"`; every int64 value, including transaction numbers
and plan minor-unit amounts, is a canonical decimal string.

| Route suffix | Method and body |
| --- | --- |
| `transactions/spend`, `receive`, `transfer` | POST amount, date, account/from/to selectors; optional description, reference, draft, dry_run |
| `journals` | POST journal with posting_date, description and decimal debit/credit lines; optional draft, dry_run |
| `transactions/{number}` | GET journal |
| `transactions/{number}/post`, `/abandon` | POST optional dry_run |
| `transactions/{number}/reverse`, `/undo` | POST date; optional description, draft, dry_run |
| `transactions/{number}/correct` | POST replacement journal and reason; optional draft, dry_run |
| `accounts` | POST kind, name, explicit code and active_from; optional statement-control/default settings |
| `defaults` | GET, or POST key (payment-account/deposit-account/retained-earnings) and account |
| `periods` | GET, or POST fiscal_year and optional dry_run |
| `periods/{period}/reopen` | POST reason and optional dry_run |
| `dashboard` | GET company counts and open work |
| `reconciliations` | GET; optional account/status/from/to filters |
| `reconciliations/{id}` | GET |
| `reconciliations/{id}/reopen` | POST reason |
| `reconciliations/plan` | POST statement_account, through, ending; optional start, beginning, clear_all or cleared number strings, target_reconciliation_id |
| `reconciliations/apply` | POST plan and optional dry_run |
| `close/plan` | POST period |
| `close/apply` | POST plan and optional dry_run |
| `year-close/plan` | POST fiscal_year; optional retained_earnings selector |
| `year-close/apply` | POST plan and optional dry_run |

New routine transactions and journals require `Idempotency-Key`. Repeating the
same key and content returns the existing journal; changed content conflicts.
The journal route assigns manual source identity and accepts standard journals.
Correction identifies a retry by original transaction, replacement and reason;
its reversal and replacement commit together. Reversals identify the original
and require matching date/period/description on retry. Status changes converge
on the requested status. Account codes identify creations; an existing code
returns `ACCOUNT_EXISTS` and can be inspected before retrying another operation.
Defaults are assignments and fiscal-year setup reuses existing definitions.

Display the complete plan before apply and send it back unchanged, including
null arrays and decimal-string integers. Plans retain their digest and bind
company, currency and relevant control state. Apply rejects stale evidence
before committing. Identical successful reconciliation and close plans can be
replayed. Reconciliation plan requests may return a blocked plan with HTTP 200: inspect `ready`
and `blockers` before offering apply. Dry runs do not reserve idempotency keys.

Global registry creation, database migration/backup/restore, QuickBooks imports
from local paths, and precoverage evidence files are shared local administration
services. They are callable in-process by local clients and the CLI. They are
not company HTTP routes: database-wide administration and arbitrary filesystem
paths need a separate administrative trust boundary.

## Upload, inspect, preview, apply

1. `POST /v1/companies/example/imports?name=statement.ofx` with raw bytes,
   `Content-Type: application/octet-stream`, and a stable `Idempotency-Key`.
   For profiles requiring options, send `multipart/form-data` with one `file`
   part and an optional `options` JSON text part. The filename supplies `name`
   unless the query overrides it. HTTP 202 means the source is durably stored,
   not accepted as accounting.
2. Poll `GET /v1/companies/example/imports/{job}`. The worker moves UPLOADED to
   READY or FAILED, retaining a stable error diagnostic on failure. Disconnects
   do not cancel the job; restarting resumes queued uploads. The CLI also has
   `bank-import process JOB` for explicit recovery.
3. Read the document's account keys and transactions. Map every account to a
   statement account in the selected company or exclude it with a reason.
   At least one account must be mapped. Submit choices to
   `POST /v1/companies/example/imports/{job}/previews` with JSON content type
   and a stable preview `Idempotency-Key`:

```json
{
  "post": true,
  "mappings": [{
    "account_key": "<account key from job.document.accounts>",
    "statement_account": "EXAMPLE-1000",
    "classifications": [{
      "transaction_id": "<exact transaction ID from the parsed document>",
      "contra_account": "4000"
    }]
  }],
  "exclusions": []
}
```

4. Inspect the returned plan, choices, summary, and digest. Preview executes
   actual ledger validation under a rolled-back savepoint; it stores the plan
   and audit event, but no source records, identities, transactions, or journals.
5. `POST /v1/companies/example/import-plans/{plan}/apply` with no body and
   `If-Match: "<digest>"` applies exactly that preview. It atomically commits
   source evidence, identity mappings, statement transactions, optional journals,
   audit events, and the durable receipt. A disconnect after commit can be
   recovered by repeating the same request.

A source-only plan uses `post: false` without classifications. Posting requires
one explicit classification per nonzero booked transaction that is not a duplicate. Zero-amount observations
reject classifications and create no journals. Revenue, expense, and equity
contra accounts are supported; transfers, loan principal, receivables/payables,
and investment posting require other workflows. Source transaction types are not
automatic accounting classifications. Imported balances remain source evidence;
they do not create opening-balance plugs or complete reconciliations.

An upload key identifies the same filename, bytes, and normalized options; changing any returns
`IDEMPOTENCY_CONFLICT`. A preview key identifies the same job and choices. A
ledger change after preview returns `IMPORT_PLAN_STALE` before any accounting
write; create a new preview with a new key and inspect it again. This revision
is conservative across books sharing one SQLite file. Once applied, the same
plan returns its recorded receipt even after later ledger changes. A different
plan cannot reapply that job.

FITIDs are scoped to full source account identity, never globally or by account
suffix. Repeated overlapping transactions retain one materialization; separate
FITIDs with identical date, amount, and description remain separate. A changed
materialized FITID fails closed. Explicit corrections are a later adapter
capability. A statement account cannot silently switch bank identities.

## Supported input and limits

Supported profiles cover OFX/QFX/QBO bank/card, QIF non-investment registers,
CSV/TSV/XLSX with explicit profiles, CAMT 052/053/054, MT940/942, BAI2/BTRS,
CODA, CFONB120, and Norma 43. See [statement formats](statement-formats.md) for
versions, required options, field semantics, controls, and unsupported features.

Uploads are at most 8 MiB, options 32 KiB, and choices 2 MiB. Parsing is bounded
to 100 accounts and 10,000 movements with additional text/XML/ZIP complexity
limits. New adapters preserve foreign-currency source amounts for inspection;
posting requires the target entity's chosen currency. PENDING/REVIEW movements retain source evidence and
reject classifications. `retained_observations` counts these and duplicate
observations; they do not create statement transactions or journals.

Native IDs keep source-account scope. For new IDs from another format or
file-scoped records without native IDs, existing movements with the same date
and amount require an explicit `identity_decisions` entry in the mapping.
`POST /imports/{job}/matches` accepts the same choices with JSON content type,
needs only read permission, and performs no writes. It returns candidate source
IDs, dispositions, and the current ledger revision. Use `action: "duplicate"`
with `existing_source_id` and a reason to retain duplicate evidence, or
`action: "new"` with a reason for a separate movement. Preview and apply check
these decisions again. See [identity review](statement-formats.md#identity-and-posting).

## Read contracts

All ordinary responses use `{"schema":"books.api/v1","ok":true,"data":...}`;
failures use `ok:false` and `error:{code,message}`. Source download is raw bytes.
HTTP statuses: 400 invalid input, 401 unauthenticated, 403 insufficient grant or
origin, 404 unavailable resource, 409 stale/conflicting request, 422 accounting
validation, 500 integrity/internal failure, 503 storage/worker unavailable.
Unknown JSON members, duplicate keys, and trailing JSON values are rejected.

- `GET /v1/capabilities`, `/v1/health`, `/v1/companies`.
- Company `GET /accounts`, `/statement-accounts`.
- Company `GET /transactions?after=0&limit=100` returns journal summaries in entry
  order, `items` and `next_cursor`; limit is 1–200. This is not a synchronization
  feed or an immutable paginated snapshot.
- Company `GET /reports/trial-balance?as_of=2026-07-31`,
  `/reports/balance-sheet?as_of=2026-07-31`, and
  `/reports/profit-loss?from=2026-01-01&to=2026-07-31` read posted accounting.
- Company `GET /imports/{job}`, `/imports/{job}/source`, `/import-plans/{plan}`.

API fields named `*_cents` are **integer minor-unit strings** (e.g. `"12345"`),
and int64 entry numbers are also strings. Source amounts are decimal strings
(e.g. `"123.45"`). Do not convert either to JavaScript floating-point accounting
values; use `BigInt` for minor units or an exact decimal library. Interpret
minor units using the owning currency, not a fixed divisor of 100. See
[currency rules](currencies.md). The CLI retains its
existing `books.cli/v1` decimal-money serialization; the envelopes differ.

## Matching CLI workflow

```sh
books --company example --json bank-import upload --input statement.ofx --key upload-1
books --company example --json bank-import show JOB
books --company example --json bank-import matches JOB --input choices.json
books --company example --json bank-import preview JOB --input choices.json --key preview-1
books --company example --json bank-import plan PLAN
books --company example --json bank-import apply PLAN --digest DIGEST --commit
books --company example doctor
```

Upload and preview persist evidence, so `--dry-run` is unsupported for them;
preview itself never commits accounting. `apply` requires `--commit` and the
exact digest. A CLI upload can return a durable FAILED job with its diagnostic;
check job status as well as successful transport/CLI execution.

## Operation and next milestones

Database schema v2 includes raw files, immutable source options, parsed documents,
immutable plans, and receipts in
the same verified database backup. `doctor` checks their hashes, payload
bindings, and corresponding audit evidence. See [migration](migration.md) and
[operations](operations.md). No API can delete import evidence. There is no
storage quota or retention worker; operators must monitor disk usage. Limits
bound individual requests, not total authenticated upload volume.

The current worker parses queued uploads in process and records failures.
It is not a distributed job scheduler. There is no job-list endpoint, cancellation,
progress percentage, event stream, offline posting, or multi-writer replication.
Clients must retain returned job IDs. Cash planning, further bank dialects, richer
client SDKs, OIDC/user provisioning, and packaging are subsequent milestones.

## General ledger

`GET /v1/companies/{company}/reports/general-ledger` requires company `read`
permission. Required query parameters are `from` and `to` (inclusive ISO dates).
Optional `account` accepts a company account code or unambiguous name;
`include_zero` accepts exactly `true` or `false` and defaults to false. Duplicate
and unknown parameters, including `entity` or `group` overrides, are rejected.

The response uses `books.api/v1` and the existing exact minor-unit string
encoding. It contains resolved company scope, date range and account sections
with opening balance, ordered posted journal lines, running balances and closing
balance. It uses the same company application operation as the CLI's `gl` path.
Consolidated general-ledger API access remains planned and is tracked separately
in [implementation progress](implementation/parity-progress.md).

## Company report options

Trial balance, balance sheet and profit-loss endpoints also accept `include_zero`.
Omitting it or passing `false` preserves the existing behavior. Passing `true`
includes zero-balance accounts, matching the CLI's `--include-zero`. Invalid or
repeated `include_zero` values return `REPORT_QUERY_INVALID` (HTTP 400).
These reports use typed company application requests, also used by their normal
company CLI paths. Raw-database and consolidated CLI reports remain supported;
consolidated HTTP reporting is still a separate tracked gap. Existing API money
fields retain exact minor-unit strings, including values beyond JavaScript's
safe integer range; do not convert them to JavaScript numbers.


Company report execution now passes through backend operation descriptors with a
company `read` grant check. HTTP supplies authenticated principal access, and the
local CLI explicitly supplies local owner access. This shared policy currently
covers the four company reports and journal inspection/validation; other
workflows retain their existing checks while the backend rollout continues.

## Journal inspection and validation

`GET /v1/companies/{company}/journals/{journal}` reads a journal by its opaque ID,
including lines and exact minor-unit totals. `GET` on the same path followed by
`/validation` returns `valid`, `errors`, debit/credit totals and currency without
posting or editing the journal. An invalid draft returns HTTP 200 with
`valid: false`; callers must inspect that field. The CLI retains its nonzero exit
status for an invalid `journal validate` result.

Both routes require company `read` access and reject query parameters. Missing
IDs and IDs belonging to another book return `JOURNAL_NOT_FOUND` (404). The
scope check and result use one database snapshot. The existing transaction-number
route remains supported. Low-level CLI journal reads retain their trusted local
database scope while extraction into the typed operation layer continues.
