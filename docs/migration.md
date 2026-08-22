# Database lifecycle controls

Books currently has one public database format: the complete native schema in
`0001_initial.sql`. A new database is created directly at schema version 1 and
records that one migration, its checksum, the application version, and the
SQLite version in `schema_migrations`.

The migration engine remains the foundation for future public schema changes.
`books db migrate --dry-run` opens a candidate read-only, validates its
application ID, metadata, gapless migration ledger, embedded checksums, and
exact recorded-version schema, and rehearses any pending work on an in-memory
SQLite copy. Commit mode pins the inspected file identity and database UUID,
repeats validation on the writable connection, and applies all pending SQL,
data work, ledger rows, `user_version` changes, and final schema verification in
one transaction.

Released migration files are immutable. A future schema change adds the next
sequential migration and tests both fresh creation and the supported upgrade.
Never edit a recorded migration checksum or relabel a database by hand.

## Historical-data import sequence

1. Preserve the source exports and copy them into a read-only review area.
2. Record SHA-256 digests for every source file.
3. Declare an exact, gapless date interval for every historical source.
4. Build an account plan across all entities before importing journals.
   External IDs are realm-local; record the realm explicitly and never merge by
   display name alone.
5. Review every diagnostic. Blocking diagnostics stop the conversion; void or
   zero-value source objects remain explicit informational diagnostics.
6. Create a fresh schema-v1 company through Books, then reproduce the
   entities, ownership graph, books, periods, chart, book activations, statement
   controls, and immutable external identities through reviewed import commands.
   Formal reconciliation bounds remain independent of an account's legal
   lifetime.
7. Import historical journals as idempotent batches and post each batch
   atomically. Apply only evidence-backed corrections at their actual accounting
   dates; preserve tax type and accounting period independently of settlement
   date.
8. Recompute and post exact annual closing journals; never import a
   retained-earnings plug.
9. Import every statement-source row. Pending, review, and source-only rows stay
   in the evidence ledger without materializing as accounting transactions.
   Preserve provider state changes as revisions of the same logical identity.
10. Rebuild reconciliations from retained evidence. Every residual control line
    is either allocated or explicitly reviewed as outstanding. Converted
    reconciliations whose historical start predates the current formal coverage
    date retain that start as their accounting boundary.
11. Post mirror and elimination entries explicitly, with counterparty and
    intercompany keys.
12. Verify annual income, control balances, source counts, subledgers, report
    equations, SQLite integrity, closed digests, source links, every
    reconciliation, Doctor, and the audit chain.
13. Re-run the import plan against unchanged sources and prove that the result
    is deterministic or that every permitted nondeterministic field is explained
    in the import receipt.
14. Create and verify a Books backup before the first applied batch and again
    after the complete import passes every control.

## Cutover evidence

Source exports, manifests, statements, mapping files, validation reports,
receipts, and verified backups stay outside Git. The validation report should
contain source hashes, row counts, exact control results, completed
reconciliations, integrity results, and certification limits.

Failed imports must never be described as current. Preserve their diagnostics,
restore the verified pre-import backup when rollback is required, correct the
plan or source evidence, and apply a newly reviewed idempotent batch.

## No-plug rule

Differences without primary evidence remain visible. Bank/card opening
differences require the relevant statement or transaction detail. Payroll
clearing requires payroll reports. Unapplied receipts require invoice evidence.
Subsidiary-equity elimination requires a supportable investment account. None
may be forced to zero merely to make a control agree.
