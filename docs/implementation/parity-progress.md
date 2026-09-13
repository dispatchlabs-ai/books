# Backend, API and MCP implementation progress

Updated September 13, 2026. Overall status: **in progress; full parity is not
implemented and no MCP server ships yet.** This is the durable tracker for the
[interface parity contract](../interface-parity.md) and [MCP design](../mcp-design.md).

## Current milestone: inventory and first API gap

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
it is **not yet typed dispatch, shared authorization, or evidence of complete
argument/scope parity**. Existing company-scoped report routes do not cover
consolidated reporting. Every MCP binding is still missing. `gap` fields keep
those limits explicit; no operation is marked fully complete merely because its
name appears in the inventory.

## Remaining milestones

| Work | State | Completion evidence required |
| --- | --- | --- |
| Typed backend operation descriptors and shared authorization | Not started | Typed schemas, scope/grants, effects and adapter bindings; no policy bypasses |
| Remaining workflow extraction from CLI | Not started | Application-owned orchestration with preserved CLI behavior |
| Detailed journals, account/evidence/lifecycle and reconciliation API gaps | Not started | Shared service routes, OpenAPI and cross-interface tests |
| Database topology, ownership and consolidation API | Not started | Whole-scope authorization and complete report equivalence |
| Registry, migration, backup/restore administration API | Not started | Explicit admin grants, lineage, maintenance locks and crash recovery |
| Remote file bundles, artifact transfer and durable operation receipts | Not started | Bounded transfer, authorization, replay/conflict and interruption tests |
| Built-in stdio MCP | Not started | Policy, SDK integration, typed tools/resources; no network listener |
| Complete CLI/API/MCP parity | Not started | Every catalog operation and argument covered; no unexplained gaps |
| Fresh-agent and independent frontend acceptance | Not started | Named client/platform journeys and independent security review |

Next implementation slice: expand the inventory into typed operation and scope
contracts, then complete the remaining read/report and evidence operations before
introducing database administration grants. Preserve all interfaces during the
extraction. MCP must use those same contracts rather than gaining a separate
implementation of accounting behavior.

## Validation ledger

The new targeted tests are `TestGeneralLedgerAPI` and
`TestOperationInventoryCoversCLIAndOpenAPI`. All fixtures are synthetic and use
disposable data. No existing company data is a fixture.

September 13 validation: `./scripts/check` passed on macOS, including unit/race
checks, vet, lint, vulnerability scan and synthetic CLI smoke. The full CLI test
package plus application/HTTP/catalog packages passed on Linux with Go 1.26.6.
The independent reviewer reported no actionable correctness or security findings
and independently reran the two targeted tests. Documentation links, JSON parsing
and diff checks passed. The optional deployment-name denylist was not supplied.

Review limits: no full three-interface conformance yet; MCP is absent. The new
endpoint-specific tests cover USD and ordinary int64 amounts, not an exhaustive
currency/large-int64 matrix. Broader money/report tests remain in the existing
suite. Additional scope/argument and runtime binding coverage belongs to the next
milestones, not a completed parity claim.

The catalog audit currently compares implemented CLI names and documented HTTP
routes. It does not prove router registration or full parameter coverage for every
historical endpoint. Runtime route/schema conformance and the final zero-gap gate
remain explicit work, alongside typed backend dispatch.

## How to maintain this tracker

Update this file in each implementation commit: mark only demonstrated outcomes,
record meaningful test/review results and blockers, and identify the next slice.
Update catalog bindings and gaps in the same commit as an interface change. Keep
capability documentation honest during rollout. Progress tracking does not create
a background scheduler or authorize deployment of a network service.
