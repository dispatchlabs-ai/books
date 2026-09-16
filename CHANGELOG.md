# Changelog

All notable public changes to Books are documented here.

Books follows [Semantic Versioning](https://semver.org/). While the project is
in `0.x`, command, schema, and database compatibility may change between minor
versions. Breaking changes are called out in this file and in release notes.

## Unreleased

- Remember the selected accessible browser entity across reloads and edit monthly
  targets directly in the Cash table, preserving drafts on period changes and
  failed saves.

- Simplified the browser to Cash: a compact responsive account table, shared
  shadcn controls and monthly budget editing. Removed the experimental Accounts,
  Outlook, Ask Books and decision screens; backend operations are unchanged.

- Added a Cash home screen with week, month, and year bank-account forecasts,
  first negative dates, exact projected lows, and explicit incomplete coverage.

- Fixed hosted form login and logout rejecting browser submissions by preserving
  same-origin referrer information while keeping strict Origin validation.

- Added optional hosted HTML sign-in with expiring, revocable sessions and
  protected feedback notes with stale-save rejection.

- Added operation-specific MCP descriptions and initialization workflow guidance
  for company/database scope, continuous bookkeeping, planning and retry recovery.
  Large reads prefer complete scoped artifacts above 32 KiB; artifact chunks and
  mutation receipts preserve existing compatibility and delivery guarantees.
  Published a controlled agent-evaluation protocol; model performance is unmeasured.

- Added an experimental shadcn web interface for personal and business overview,
  account activity, and scoped AI-adapter conversations, with an isolated demo.
  Read-only API credentials stay server-side; cash forecasts and planning are
  explicitly demo-only until connected services exist.

- Added built-in stdio MCP with typed, permission-filtered tools over the shared
  backend; no network listener or separate HTTP server is required.
- Completed API/MCP coverage for existing company and whole-database workflows,
  including topology, consolidation, detailed journals, evidence, audit and health.
- Added server configurations v3/v4 with whole-database handles, separate registry
  and maintenance permissions, and explicit current/future company grants.
  Earlier configurations retain their existing authority.
- Exposed company/configuration management and database initialization, migration,
  backup and restore. Active Books connections block exclusive maintenance;
  adapters open per operation, and backup retry keys are scoped by database UUID.
- Added bounded resumable artifacts for statements, QuickBooks bundles, lifecycle
  evidence, backups and large JSON requests/results, with scope binding and
  retention for committed evidence.
- Published additive OpenAPI snapshots through 1.11.0. Existing v1 response
  envelopes and database schema are unchanged by the parity work. Remote account
  creation requires explicit code/date fields; routine transaction writes require
  stable keys. Existing workflow retry contracts remain authoritative.
- Added statement-account archive/identity previews and rejected unsupported
  database reopen dry runs before mutation. Preserved read-only directory access
  and restoration into missing directory trees.
- Added an agent-first README demo with separate operational guidance and a
  manual installation path.

- Added company API workflows for routine transactions, journal corrections and
  reversals, reconciliation, period/year close, chart defaults and fiscal periods.
  CLI and API share application services; direct CLI use requires no server.
- Added server configuration v2 with independent posting and management grants,
  an additive OpenAPI 1.3.0 snapshot and typed workflow client examples.
- Corrections commit their reversal and replacement together. Year-close apply
  rejects changed balances inside the write transaction, and fiscal-year setup
  opens periods only in the selected book.
- QuickBooks apply failures identify completed durable setup/import steps and
  the batch recovery target; account-default failures identify the created account.

- Added a selectable single currency per company or person, including exact
  zero-, two-, three-, and four-decimal monetary units. USD remains the default.
  Accounts, imports, reports, reconciliation, and close use the book currency;
  mixed-currency posting and consolidation are rejected without conversion.
- Added OpenAPI snapshot 1.2.0 and currency-bound non-USD reconciliation v4 and
  year-close v2 plans. Existing USD plan formats and database schema stay valid.

- Added QFX/QBO, QIF, profiled CSV/TSV/XLSX, CAMT 052/053/054, MT940/942,
  BAI2/BTRS, CODA, CFONB120, and Norma 43 statement adapters with explicit
  version/profile limits and source control validation.
- Added durable import options, multipart API uploads, format discovery, and
  read-only match inspection with explicit cross-export identity decisions.
  Pending/review and duplicate observations retain evidence without posting;
  source currency must match the target book before posting.
- Added an OpenAPI 1.1.0 contract snapshot and expanded TypeScript example;
  existing v1 routes, envelopes, and database schema v2 remain supported.

- Added a company-scoped headless API with hashed bearer credentials, separate
  read/import/post grants, exact JSON amounts, bounded requests, and a durable
  import worker.
- Added OFX bank/card upload, discovery, explicit mapping/classification, saved
  previews, atomic apply, and stable replay through the API and CLI.
- Added forward schema v2 for raw import evidence, previews, and receipts,
  including audit-bound Doctor verification and backup coverage.

- Added a noninteractive human- and agent-oriented accounting CLI with an
  isolated company registry under `~/.books`.
- Added manual chart setup, routine and split transaction entry, immutable
  reversals and corrections, statement imports, bank reconciliation, general
  ledger, trial balance, profit and loss, balance sheet, period close, fiscal
  close, audit verification, and verified backup and restore.
- Added an optional, generic initial import from QuickBooks exports.
- Supports macOS and Linux hosts.
- Added bounded XLSX processing and a Go 1.26.6 minimum toolchain.
- Defined the complete database format as native schema v1. New databases start
  directly at that schema and record one checksum-bound migration.
- Made reopened historical reconciliations retain their original accounting
  boundary even when it predates the statement account's current formal
  reconciliation coverage, and require every such control line to be allocated
  or explicitly reviewed as outstanding before completion.
- Bound manual reconciliation observations to the exact journal entry and line
  named by their immutable plan evidence, with matching Doctor checks.
- Required `EVIDENCE` source links to remain in the source identity's exact
  book; cross-book and cross-entity propagation must use explicit mirror or
  elimination roles.
