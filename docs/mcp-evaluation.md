# Evaluating the MCP interface

The current refinement preserves operation names, schemas, authorization and
accounting behavior. It adds purpose-specific descriptions, initialization
instructions, and a smaller artifact target for reads. It does not establish
optimal tool grouping or improved model success without comparative agent runs.

## Controlled comparisons

Use these three arms with identical synthetic data, authorization, prompts,
model/version, reasoning settings and task limits:

1. Baseline: catalog and instructions from revision
   `bdc042b6498667127be0ae046c475c043c10312e`.
2. Guided: current descriptions and initialization instructions, all authorized
   definitions available immediately.
3. Discovered: the same guided server, with the host's actual deferred discovery
   enabled. Record the host/version and search behavior. Mark unsupported hosts
   as unsupported; do not substitute a keyword filter and call it native discovery.

Run presentation as a separate comparison (512 KiB versus 32 KiB read target)
when attributing context savings. Use the same target across catalog arms to
avoid confounding guidance with result-size changes. Keep these choices in the
external test harness; do not add production switches for obsolete descriptions.

Run each task at least five times from a fresh disposable Books home, alternating
arm order. Pin source commits and hash the served catalogs and task fixtures.
Record all model-visible messages and definitions, selected tools and arguments,
results, discovery queries/misses, retries and final answers. Preserve total,
input/output and cached token counts separately, along with elapsed time. Unknown
host token accounting is unknown, not zero; serialized schema bytes are a
separate measurement.

## Synthetic task cases and accounting oracles

| Task | Required outcome and failure probes |
| --- | --- |
| Record income and spending | Record 1,000.00 revenue and 50.00 expense in the selected entity. Posted profit is 950.00; drafts must not count. |
| Continuous statement import | Upload, parse, resolve explicit mappings/choices, preview and apply a supported synthetic statement. Independently check source identities, exact journal lines and receipt; ingestion is not posting. |
| Pending then settled | Retain pending evidence without posting. Feed a supported settled observation and verify one supported posting; report unsupported lifecycle treatment as blocked rather than inventing a merge. |
| Overlapping observations | Import overlapping fixtures with exact duplicate evidence and a separate legitimate same-amount transaction. Prevent duplicate posting without discarding the legitimate transaction. |
| Current reports | Produce dated profit/loss and balance sheet with source cutoff, retained pending and gaps. Do not close a period to obtain reports. |
| Reconcile | Plan/apply an exact statement interval and balance; verify allocations and zero difference. Change ledger state after planning and require stale-plan recovery. |
| Correct a posting | Create the linked reversal/replacement and preserve the original posted journal. Verify amounts and links, not just a balanced total. |
| Uncertain response | Drop an acknowledged mutation response, reconnect, inspect/retry according to its contract; verify exactly one financial effect. Repeat with an unsupported retry shape and require state inspection. |
| Large read | Return a report above the read target. Recover the complete artifact in the original scope and verify its hash, exact amounts and validation; reject a foreign scope. |
| Explicit period close | Only when specifically requested, plan/apply a period lock and verify its state. Keep this separate from continuous bookkeeping. |
| Scope confusion | Provide two entities in one database plus a third in another. Verify the intended entity alone changes and denied handles remain denied. |

Grade final ledger state with a separate read-only oracle, plus source/receipt and
prior-journal comparisons. Count wrong-scope attempts even if authorization
rejects them. Count duplicate mutations, incorrect amounts/classifications,
unresolved tasks, tool-selection errors and unsupported completion claims.
Inspect unsuccessful discovery as well as successful retrieval. Treat every
incorrect financial mutation as a failed run; do not trade it for lower latency.
Report per-task results and uncertainty rather than a universal tool-count target.

## Deterministic checks shipped with this change

`./scripts/check` runs catalog guidance coverage, actual stdio initialization,
permission-filtered discovery, exact-money and cross-interface replay tests.
`TestMCPContinuousWorkflow` covers receipt/reconnect/replay, draft isolation,
posting, a linked posted reversal, foreign-company rejection and final trial
balance with CLI, Doctor and audit checks. Result tests cover complete read
artifact recovery, cross-scope rejection, inline chunks, disabled storage and
committed-mutation fallback. Existing adapter tests transfer and recover an
8 MiB source through real MCP calls.

These checks validate implementation and accounting behavior. They are scripted
calls, not model tool-selection measurements. No named host's discovery behavior,
model accuracy improvement, token savings or optimal grouping is claimed by
these tests. Comparative agent runs must publish their own dated receipts before
making those claims.
