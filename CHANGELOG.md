# Changelog

All notable public changes to Books are documented here.

Books follows [Semantic Versioning](https://semver.org/). While the project is
in `0.x`, command, schema, and database compatibility may change between minor
versions. Breaking changes are called out in this file and in release notes.

## Unreleased

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
