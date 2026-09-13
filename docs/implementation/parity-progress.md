# Backend, API and MCP implementation progress

Updated September 13, 2026. Overall status: **in progress; full parity is not
implemented; an initial stdio MCP adapter is now present.** This is the durable tracker for the
[interface parity contract](../interface-parity.md) and [MCP design](../mcp-design.md).

## Milestone 1: inventory and first API gap — complete

- [x] Record the canonical operation inventory with CLI aliases, existing HTTP
  bindings and explicit gaps in [catalog.json](../../internal/operations/catalog.json).
- [x] Add a catalog audit against the actual runnable Cobra command tree and
  OpenAPI routes. New or stale bindings fail the audit.
- [x] Add a company-bound general-ledger application operation; route ordinary
  company CLI reports and the new HTTP endpoint through it.
- [x] Expose `GET /v1/companies/{company}/reports/general-ledger` with read access,
  exact money, explicit dates, account selection and zero-account inclusion.
- [x] Test CLI/API amounts, opening/closing balances, missing authentication,
  unauthorized company selection, scope overrides and shared-database isolation.
- [x] Publish additive OpenAPI 1.4.0 while preserving earlier contract snapshots.
- [x] Complete canonical checks, independent review and supported-platform validation.
- [x] Commit and push the verified milestone (see this file’s Git history).

The inventory currently contains 106 operation records. It inventories bindings;
the inventory itself is **not executable dispatch, authorization, or evidence of
complete argument/scope parity**. Existing company-scoped report routes do not cover
consolidated reporting. MCP bindings now cover an implemented subset. `gap` fields keep
those limits explicit; no operation is marked fully complete merely because its
name appears in the inventory.

## Milestone 2: typed company reports — complete

- [x] Add shared typed date/range and zero-account options for trial balance,
  balance sheet and profit/loss; preserve the existing application entry points.
- [x] Route ordinary company CLI reports through those application operations.
- [x] Add HTTP `include_zero` with explicit boolean validation; retain default
  behavior, existing paths and exact minor-unit strings.
- [x] Add USD/JPY/KWD tests beyond JavaScript's safe integer range, zero-account
  inclusion, CLI/API amount agreement and direct-database CLI fallback checks.
- [x] Publish additive OpenAPI 1.5.0; preserve earlier snapshots.
- [x] Complete independent review, macOS gate and Linux validation.
- [x] Commit and push this verified slice (see this file’s Git history).

Historical milestone note: consolidated API and MCP coverage were still missing at this point.

## Milestone 3: executable company report contracts — complete

- [x] Bind the four company report operation IDs to typed input/output contracts,
  version, company scope, read grant and read effect.
- [x] Couple shared grant validation to application execution; zero access denies,
  authenticated grants are copied, and local owner access is explicit.
- [x] Route company CLI and HTTP reports through the executable contracts.
- [x] Test backend denials without an HTTP handler, local/scoped equivalence,
  grant snapshot isolation, cancellation and inventory consistency.
- [x] Complete canonical macOS checks, Linux validation and independent review.
- [x] Commit and push the verified milestone (see this file’s Git history).

This first executable subset lives in `internal/operations/reports.go`. The JSON
inventory tracks bindings and gaps; it does not grant authority. Non-report
operations, consolidated/direct-database CLI paths and legacy application methods
have not yet moved behind shared policy. JSON Schema generation and runtime
binding/parameter audits remain pending. Trusted constructors are adapter APIs,
not client input; HTTP constructs company access only after authentication.

## Milestone 4: journal inspection and validation API — complete

- [x] Add typed company journal read/validation operations and HTTP bindings.
- [x] Hold one snapshot for journal membership and result reads; reject foreign
  reversal targets before inspecting their validation details.
- [x] Reject aggregate journal amount overflow with rollback.
- [x] Test shared-database isolation, authorization, exact amounts, invalid drafts,
  no posting, CLI amount equivalence, foreign reversal and overflow retry behavior.
- [x] Add OpenAPI 1.6.0 snapshot and update inventory/docs.
- [x] Complete canonical checks, Linux validation and independent follow-up review.
- [x] Commit and push the verified milestone (see this file’s Git history).

The low-level CLI still reads its trusted database directly through the ledger
reader/validator; its typed company/database scope extraction remains pending.
Subsequent milestones add database journal operations and MCP bindings.

## Milestone 5: whole-database runtime and API — complete

- [x] Add explicit v3 database handles, UUID binding and whole-database read/manage
  grants; preserve company and v1/v2 boundaries.
- [x] Bind 54 typed ledger/report operations to shared runtime execution.
- [x] Expose topology, consolidation, low-level journals, accounts, source evidence,
  statement accounts and direct reconciliation through operation-specific routes.
- [x] Extract shared lossless JSON codec; generate OpenAPI from typed descriptors.
- [x] Test every database operation's wrong-handle denial and every write's
  read-only denial; test company escalation, UUID mismatch and evidence imports.
- [x] Complete canonical checks, Linux validation and follow-up review.
- [x] Commit and push the verified milestone, then continue toward full parity.

Database manage is deliberately whole-database bookkeeping/topology authority.
File-based lifecycle operations, maintenance and registry are still pending.
Direct low-level CLI calls still require extraction into the descriptor adapters.

## Milestone 6: stdio MCP and company operation bindings — complete

- [x] Add the official pinned SDK, private explicit policy, named typed tools,
  copied grants and direct backend execution without an HTTP listener.
- [x] Add 43 company operation contracts and HTTP bindings alongside 54 database
  bindings: 97 tool names currently cover 84 inventory operations.
- [x] Test actual compiled-binary stdio discovery/posting/reporting, exact money,
  oversized frames, ambient-config independence and HTTP/MCP retry agreement.
- [x] Preserve company closing/post/import grant boundaries in shared execution.
- [x] Finish independent follow-up review and supported-platform canonical checks.
- [x] Commit and push this verified slice; continue registry, maintenance, files and final conformance.

## Milestone 7: bounded files and remaining inspection bindings — complete

- [x] Add database audit/status/Doctor and company format/replan contracts.
- [x] Add bounded resumable artifact transfer, SHA-256 checks, scope/identity binding,
  context-aware locks, quota reservations and discard tombstones.
- [x] Preserve the full 8 MiB statement upload capacity through HTTP/MCP chunks.
- [x] Add large MCP result references and successful-mutation delivery fallback.
- [x] Test interrupted chunks, ownership/identity changes, limits, symlinks,
  cancellation, maximum-size cross-adapter upload and result download.
- [x] Complete follow-up review, supported-platform gates and commit/push.

## Remaining milestones

| Work | State | Completion evidence required |
| --- | --- | --- |
| Typed backend operation descriptors and shared authorization | 43 company and 54 database bindings implemented; administration pending | Typed schemas, scope/grants, effects and adapter bindings; no policy bypasses |
| Remaining workflow extraction from CLI | Not started | Application-owned orchestration with preserved CLI behavior |
| Detailed journals, account/evidence/lifecycle and reconciliation API gaps | Journal inspection/validation added; remaining gaps pending | Shared service routes, OpenAPI and cross-interface tests |
| Database topology, ownership and consolidation API | Implemented through whole-database operations; acceptance pending | Whole-scope authorization and complete report equivalence |
| Registry, migration, backup/restore administration API | Not started | Explicit admin grants, lineage, maintenance locks and crash recovery |
| Remote file bundles, artifact transfer and durable operation receipts | Not started | Bounded transfer, authorization, replay/conflict and interruption tests |
| Built-in stdio MCP | Initial 97-tool subset verified | Policy, SDK integration, typed tools/resources; no network listener |
| Complete CLI/API/MCP parity | Not started | Every catalog operation and argument covered; no unexplained gaps |
| Fresh-agent and independent frontend acceptance | Not started | Named client/platform journeys and independent security review |

Next implementation slice: extend executable operation and scope contracts to
remaining read and evidence operations, then complete their API gaps before
introducing database administration grants. Preserve all interfaces during the
extraction. MCP must use those same contracts rather than gaining a separate
implementation of accounting behavior.

## Validation ledger

Milestone 1 targeted tests are `TestGeneralLedgerAPI` and
`TestOperationInventoryCoversCLIAndOpenAPI`. All fixtures are synthetic and use
disposable data. No existing company data is a fixture.

Milestone 1 validation (September 13): `./scripts/check` passed on macOS, including unit/race
checks, vet, lint, vulnerability scan and synthetic CLI smoke. The full CLI test
package plus application/HTTP/catalog packages passed on Linux with Go 1.26.6.
The independent reviewer reported no actionable correctness or security findings
and independently reran the two targeted tests. Documentation links, JSON parsing
and diff checks passed. The optional deployment-name denylist was not supplied.

Milestone 2 validation (September 13): `./scripts/check` passed on macOS,
including unit/race checks, vet, lint, vulnerability scan and synthetic CLI
smoke. CLI, application, HTTP and catalog packages passed on Linux with Go
1.26.6. Independent review found no actionable issues and reran targeted report
and inventory tests. `TestCompanyReportOptionsAndExactAmounts` covers USD, JPY
and KWD at 9007199254740993 minor units, zero-account inclusion, invalid/repeated
boolean parameters and both direct-database CLI selection paths. JSON, local
documentation links and diff checks passed. The optional deployment-name
denylist was not supplied.

Review limits: no full three-interface conformance yet; MCP is absent. Explicit
`include_zero=false` equivalence and profit/loss/balance-sheet consolidated
fallback are not directly covered by the new test. Broader report tests remain
in the existing suite. Additional scope/argument and runtime binding coverage
belongs to the remaining milestones, not a completed parity claim.

The catalog audit currently compares implemented CLI names and documented HTTP
routes. It does not prove router registration or full parameter coverage for every
historical endpoint. Runtime route/schema conformance and the final zero-gap gate
remain explicit work, alongside typed backend dispatch.

Milestone 3 validation (September 13): canonical macOS checks passed, including
unit/race tests, vet, lint, vulnerability scanning and synthetic CLI smoke. Linux
CLI/application/HTTP/operations tests passed with Go 1.26.6. Independent review
identified a blank CLI actor compatibility edge; the CLI now supplies its default
identity for those read-only calls, and all four reports have empty/whitespace
actor equivalence regressions. Follow-up review found no further issues.
`TestReportBackendAuthorization` tests denials directly against shared execution,
and `TestTypedReportCatalog` checks typed metadata against inventory bindings.
Local documentation links, JSON and diff checks passed. Optional deployment-name
denylist was unset. Existing application methods remain callable internally;
this slice does not claim a universal backend policy boundary.

Milestone 4 validation (September 13): canonical macOS checks and Linux
CLI/application/HTTP/operations/ledger package tests passed with Go 1.26.6.
Independent review found unchecked journal aggregate overflow and cross-book
reversal validation assumptions in the existing ledger reader; both were repaired
with synthetic regressions and follow-up review found no remaining issues.
`TestJournalReadsAPI` covers exact large totals, foreign IDs in one database,
read grants, invalid drafts, no posting, CLI amount agreement and overflow
rollback/retry. OpenAPI additive comparison, documentation links and diff checks
passed. Optional deployment-name denylist was unset. Concurrency is protected by
a database transaction; this slice does not include a forced concurrent-edit test
or complete CLI/API/MCP conformance.

Milestone 5 validation: full `scripts/check` passed on macOS and Linux with
Go 1.26.6. Independent review repairs added transactional journal reads,
snake_case request fields and exact raw-evidence JSON decoding. Follow-up review
and synthetic authorization/evidence import tests passed. OpenAPI is generated
from 54 explicit typed bindings. Optional deployment denylist was unset. Full
positive conformance of every operation across all three adapters remains pending.

## How to maintain this tracker

Update this file in each implementation commit: mark only demonstrated outcomes,
record meaningful test/review results and blockers, and identify the next slice.
Update catalog bindings and gaps in the same commit as an interface change. Keep
capability documentation honest during rollout. Progress tracking does not create
a background scheduler or authorize deployment of a network service.

Milestone 6 validation (September 13): full `scripts/check` passed on macOS and Linux with Go 1.26.6, including race checks, lint, vulnerability scan and synthetic CLI smoke. Independent follow-up confirmed bounded frames, ambient-configuration isolation, remote retry keys and explicit account creation fields. The official SDK client passed actual stdio and in-memory HTTP/MCP retry tests. GUI agent clients and complete parity are not yet validated.

Milestone 7 validation (September 13): full canonical gates passed on macOS and Linux, including synthetic CLI smoke, race, lint and vulnerability checks. Independent follow-up confirmed identity-bound artifact access, cancelable lock waits, interrupted chunk recovery, artifact error preservation, successful-mutation delivery fallback and read-only owner cleanup. Exact 8 MiB HTTP upload/MCP consumption and chunked large-result recovery pass. Full parity and durable operation receipts remain pending.
