# Changelog

All notable public changes to Books are documented here.

Books follows [Semantic Versioning](https://semver.org/). While the project is
in `0.x`, command, schema, and database compatibility may change between minor
versions. Breaking changes are called out in this file and in release notes.

## Unreleased

- Added a noninteractive human- and agent-oriented accounting CLI with an
  isolated company registry under `~/.books`.
- Added manual chart setup, routine and split transaction entry, immutable
  reversals and corrections, statement imports, bank reconciliation, general
  ledger, trial balance, profit and loss, balance sheet, period close, fiscal
  close, audit verification, and verified backup and restore.
- Added an optional, generic initial import from QuickBooks exports.
- Limited the current implementation to USD functional currency and macOS or
  Linux hosts.
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
