# Client backend and statement adapters

Approved direction: Books is a headless financial backend for CLI, web, desktop,
and mobile clients. Chris Reynolds is the accountable maintainer. This design
and implementation are AI-assisted; independent agent review is advisory.

The backend provides a shared statement import path: durable upload,
bounded parsing, account discovery, explicit entity/account mapping, preview,
atomic apply with optional explicit journal classifications, and scoped queries
and reports. Both CLI and HTTP call the same services. Existing CLI contracts
remain supported. No production database is a development fixture.

## Ownership

- `internal/banking`: format detection and bounded, deterministic parsing into
  typed statements, balances, and transactions. No database or posting policy.
- `internal/ledger`: durable import jobs, immutable previews, source/account
  mappings, posting and replay within the existing transaction boundary.
- `internal/application`: company binding and reusable client workflows.
- `internal/httpapi`: authentication, company permissions, request limits,
  versioned HTTP serialization and worker lifetime.
- `internal/cli`: CLI input/output and server lifecycle commands.
- `internal/store/sqlite`: additive schema-v2 migration, integrity and backup.

Uploaded bytes, parsed evidence, previews, and apply receipts are included in
the same database backup as the ledger. Parsing happens outside write
transactions. Applying a preview rechecks its ledger revision inside an
immediate transaction. Source imports and explicitly requested journals commit
together; failure cannot leave half of an import posted. Exact replay returns
the recorded receipt even after subsequent ledger activity.

The initial HTTP service uses operator-provisioned bearer credentials with
separate read, import, and posting grants per configured company. An
authenticated principal supplies the audit identity; clients cannot impersonate
another actor or select a server filesystem path. Local operation binds to
loopback. Remote operation requires TLS at an explicitly configured boundary.
The service does not use cookie authentication or ambient browser credentials.

## Incremental delivery

The original OFX bank/card flow now has adapters for QFX/QBO, QIF, tabular
profiles, CAMT, MT, BAI2/BTRS, and regional statements. Each adapter has a
[declared profile](statement-formats.md) and durable unsupported-input diagnostics.
Parser output retains exact source currency, status, nested details, and
statement controls; ledger posting requires the entity's chosen currency. Source options are bound
to immutable upload audit evidence without another SQLite schema migration.

Weak source identities use file hash and ordinal. Ledger-owned candidate review
compares mapped-account date and amount across exports and requires explicit
new/duplicate decisions; format adapters do not decide accounting identity.
Pending/review movements and duplicate observations remain source evidence.
Parent details never create additional postings. Saved plans bind interpretation
and identity choices and revalidate against current ledger state during apply.

Subsequent milestones include cash planning as a separate domain; richer user
provisioning and client SDKs; and offline draft synchronization. Actual, pending,
scheduled, and projected money remain distinct. Desktop Windows packaging,
multi-writer replication, hosted identity providers, and release artifacts need
their own tested contracts.

The API and client example are the initial integration reference. The database
schema is private implementation detail; frontends must use supported services.
Direct CLI operation remains available without a server process.

## Adapter validation

The adapter expansion adds no runtime dependencies or SQLite schema migration.
Validation includes the full `./scripts/check` gate (tests, race detector, vet,
lint, vulnerability scan, and disposable binary/Doctor/audit smoke flow), plus
built-binary upload/matches/preview/apply/replay journeys for all 15 format
labels. CAMT has six separate journeys covering each family and namespace
version. Each journey checks exact trial-balance totals and Doctor/audit results;
MT942's default review status creates no journal. Real HTTP multipart CSV and
legacy octet-stream OFX journeys also verify raw download and exact report
amounts. Actual job, match, choice, plan, and receipt responses validate against
the new OpenAPI snapshot; the TypeScript example passes strict compilation.
Synthetic regression tests cover malformed/control failures, pending settlement,
cross-export duplicates and legitimate repeats, durable options after worker
reopen/backup, changed-interpretation replay, and legacy OFX identities. Independent
advisory review found no remaining correctness findings. This is evidence for
the documented profiles, not bank-by-bank certification.

## Single-currency validation

The USD restriction came from transport/parser assumptions, not a missing
ledger currency model. The shared money package now owns validated monetary
scales; transports resolve the actual target currency before parsing or
formatting. Ledger identity and matching rules continue to reject currency
mismatches. FX conversion and multiple currencies within one entity are outside
this change. No dependency or database schema migration was needed.

`./scripts/check` passes, including race tests, vet, lint, vulnerability checks,
and its disposable binary flow. Additional compiled-binary journeys initialize,
post, report in human/JSON modes, reconcile, back up, restore, and verify
Doctor/audit for EUR, JPY, KWD, and CLF. Automated CLI/HTTP tests verify exact
decimal versus integer-minor-unit transport amounts, profiled CSV posting,
fractional-unit rejection, saved-plan currency mismatch rejection, legacy USD
plan isolation, registry currency tampering, KWD fiscal-year closing/replay,
and same-currency versus mixed-currency consolidation. OFX tests cover all four
monetary scales. Independent KWD QuickBooks object JSON, general-ledger JSON,
and journal XLSX probes preserve exact minor units. The OpenAPI 1.2.0 artifact
validates and the TypeScript example passes strict compilation. Independent
advisory review found no remaining code findings.
