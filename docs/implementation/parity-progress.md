# Backend, API and MCP implementation progress

Updated September 13, 2026. **Existing-functionality parity is complete** within the
focused scope below. Canonical macOS/Linux checks and independent review passed.

## Delivered scope

The [catalog](../../internal/operations/catalog.json) contains 111 operations:
106 existing operations plus five artifact helpers. It maps 112 runnable CLI
paths (including aliases) and 129 MCP tool names to their HTTP bindings. `serve`
and `mcp` launch processes and are not nested server operations. The coverage test
rejects missing CLI paths, HTTP bindings, MCP names and nonempty gap records.
Binding counts alone do not prove argument or behavioral equivalence.

- Company bookkeeping, journal inspection/posting/corrections, accounts/defaults,
  reports, statement imports, reconciliation and close workflows share existing
  application services with the CLI.
- Whole-database operations expose entities, books, periods, ownership,
  consolidation, low-level journals, statements, evidence, reconciliation, audit,
  status and Doctor with explicit whole-database authority.
- Registry operations expose first-company setup, additional companies, default
  selection, listing and existing configuration preferences. Separate registry
  grants and explicit wildcard company grants support fresh-agent setup.
- Database administration exposes initialization, migration, backup and restore
  with explicit admin authority, UUID checks, native recovery protections and
  coordination with active Books connections.
- Bounded artifacts carry statements, QuickBooks bundles, lifecycle evidence,
  backup files and large JSON requests/results. Retained accounting evidence
  cannot be discarded. Client inputs never choose arbitrary server paths.
- Built-in MCP uses stdio only; HTTP remains an explicitly launched service.
  Typed adapters recheck grants in shared execution and preserve exact money.

## Scope decisions

Chris requested a focused completion of existing functionality, without a broader
rewrite. The CLI continues to call shared application/ledger services; duplicate
wrappers are not added solely to make its call stack match remote adapters.
Existing operation-specific retry contracts remain authoritative. There is no
universal receipt framework, arbitrary file/SQL/shell access, owner-profile setup
helper, new accounting feature, or claim of named graphical-agent compatibility.
The original [design](../mcp-design.md) retains those broader proposals as history;
[current MCP usage](../mcp.md) and [API usage](../api.md) describe implemented behavior.

Transport differences are explicit: remote calls use typed JSON and scoped
artifacts instead of CLI paths/output formatting, require stable transaction keys
and explicit account code/date inputs, and use operator-selected database handles.
Whole-database administration is not implied by company or registry authority.
Unsupported database reopen previews are rejected. Statement-account archive and
identity operations expose `dry_run:true`; omission/false selects committed mode.

## Validation

Earlier bookkeeping, database, MCP, artifact and evidence slices passed canonical
macOS/Linux checks and independent review before being committed and pushed:
`a54d54c`, `de5d1a6`, `53f9e5f`, and `e8a3858`. This final slice adds:

- `TestRegistryFreshAgentJourney`: start HTTP/MCP before the registry exists,
  create a company through MCP, configure it through HTTP, post through MCP,
  create another company through HTTP, immediately use it, exercise all registry
  operations and metadata, retry a large artifact-backed request across adapters,
  and confirm the amount through the CLI.
- `TestMaintenanceAdapters`: initialize a configured target, create neighboring
  database backups under the same key, compare MCP/HTTP retry receipts, restore
  through HTTP, read the restored ledger through the CLI, migrate and run Doctor.
- `TestMaintenanceRejectsLiveConnections` and
  `TestMaintenanceReadOnlyAndNewRestoreDirectory`: active connection/symlink
  protection, exclusive maintenance, read-only directory access and restoration
  into missing directory trees.
- `TestUnsupportedReopenPreviewDoesNotMutate` and `TestDatabaseAPI`: reject unsafe
  reopen previews and verify statement-account previews do not persist changes.
- `TestOperationInventoryCoversCLIAndOpenAPI`: enforce complete catalog bindings
  against the actual Cobra tree, generated OpenAPI and registered tool families.
  Existing domain, authorization, parser, money, artifact and actual stdio tests
  continue to cover their respective operations and failure paths.

All test data is fresh, synthetic and disposable. No existing Books database was
accessed. This is representative integration acceptance plus existing domain
coverage, not a claim that every input combination was exhaustively exercised
through every frontend or that the README prompt was certified in a GUI client.

Final `./scripts/check` passed on macOS and Linux with Go 1.26.6: unit and race
tests, vet, lint (zero issues), dependency integrity, vulnerability scan (none
found), privacy checks and the disposable CLI smoke workflow. Independent review
confirmed the maintenance, registry, artifact-input and preview repairs; the final
follow-up reported no actionable findings. Relative documentation links, generated
JSON and diff checks passed. The optional deployment-name denylist was not supplied.

Delivery commit: see this file's Git history.
