# Monthly spending budgets

Cash compares each bank bucket's monthly spending target with the previous two
complete calendar months of posted expenses. September uses July and August:
`(July + August) / 2`, rounded half away from zero in the entity currency's minor
unit. The Week/Month/Year forecast selector does not change this monthly comparison.

## Attribution

Only posted EXPENSE lines count. Pending sources, bank funding, card payments,
opening balances, and fiscal closing/reopening entries do not count as spending.
Refund credits reduce expense totals. Purchases use their journal posting date,
not the later date of card funding or payment.

A purchase-level assignment takes precedence over an expense-category assignment.
Linked reversals inherit the original purchase's assignment. Otherwise a unique
bank counterparty can identify a direct bank expense. Ambiguous purchases remain
Unassigned; Books never infers a bucket from transfer notes or follows payment
chains. An average is recorded spending, not a certification that source history
is complete. Unassigned spending is shown separately and can change the bucket
averages when classified. Category edits also affect the displayed historical
comparison; they do not alter immutable ledger entries.

In **Edit budgets**, change monthly targets directly in the Cash table and use
**Save budgets** or **Cancel** above it. Open **Spending assignments** below the
table to assign expense categories and review purchase exceptions. An empty
target means Not set; zero is a deliberate target. Period changes keep the draft;
entity switching and view refresh become available again after saving or cancelling.
Each save requires the revision read by the editor. A concurrent change is rejected
so the operator can reload instead of overwriting another edit.

Monthly targets are managed in Books. Dated forecast events remain separate and
are not silently rewritten when a target changes: an annual bill, spending
allowance, and savings contribution are different planning facts. The forecast
continues to identify its opening snapshot and coverage. Budget targets do not
execute payments or move money.

## Interfaces and access

- CLI: `books --company example --json budget show --as-of 2026-09-15`
- CLI: `books --company example --json budget save --input budget.json`
- HTTP: `POST /v1/companies/{company}/operations/budget_get` and `budget_save`
- MCP: `books_company_budget_get` and `books_company_budget_save`

Read input is `{ "as_of": "2026-09-15" }`. Save input adds `plan`, containing
`revision`, `buckets`, and `assignments`, returned by the read. A bucket contains
`account`, nullable `monthly` (integer minor-unit string), and `expense_accounts`.
A purchase assignment contains `journal`, integer `line`, and bucket `account`.
Accounts and posted purchase lines must belong to the selected book.

Remote reads require `read`; saves require the additional **budget** company grant.
This grant confers no journal/import/close/payment authority. Hosted configuration
must explicitly grant it before the editor can save. The CLI rejects `--dry-run`
for saves rather than silently writing. No ledger schema migration is required.

## Storage and recovery

Books stores company-bound planning revisions in `<database-path>.budgets/`,
bound to the database UUID, entity and book. Files have private modes; every saved
revision retains its actor and timestamp. A process lock, revision comparison,
file synchronization and atomic replacement protect concurrent updates.

**Native SQLite backups contain the ledger only. Back up the `.budgets` directory
alongside the database.** Restore it separately to preserve budget history. Missing
planning state produces unset budgets, never fabricated values. Restoring an older
ledger can invalidate assignments to journals absent from that ledger; review
assignments before saving. Do not use real planning files as development fixtures.
