# Books desktop and mobile screen designs

Version 1 · September 13, 2026 · API source commit `5c62bca`

These are proposed screen designs, not a working frontend. The set contains 37 paired boards: 35 supported-workflow page designs, one shared state sheet, and one explicitly planned cash-projection page. Every board includes desktop and mobile treatments. All 111 operation IDs in the [operation catalog](../../../internal/operations/catalog.json) are assigned below; aliases and transport routes are preserved in the [coverage CSV](action-coverage.csv).

Open [the visual gallery](index.html) in a browser, or select a full-resolution image below. Generated with the built-in imagegen tool. The [manifest](manifest.json) includes every original prompt, page requirement, operation assignment and image path; individual prompts are in [prompts](prompts/). All records, names and amounts are synthetic. Examples illustrate individual screens rather than one internally continuous financial dataset.

## Design direction

One entity-aware workspace serves personal and business finances. Personal overview emphasizes household income and expenses; business overview emphasizes revenue, expenses and profit. The actual ledger has the same accrual rules. Personal/Business grouping is a proposed client presentation preference, not an existing entity-type API field. The public illustrations use Maple Household and Example Studio.

Desktop uses persistent navigation and adjacent evidence/detail panels. Mobile reflows tables into cards, opens detail panels as full pages or sheets, and keeps the primary action within reach. Complex accounting and administration stay available on mobile behind More; there is no reduced mobile permission model. Forms require at least 44-pixel interactive targets when implemented, scalable text, visible focus, semantic labels and non-color-only statuses. Generated boards show hierarchy, not final measured CSS.

## Screen inventory

| # | Page / workflow | API operations |
| --- | --- | --- |
| 01 | [Personal overview](images/01-personal-overview-v2.png) | `dashboard`, `report_balance_sheet`, `report_profit_loss` |
| 02 | [Business overview](images/02-business-overview-v2.png) | `dashboard` |
| 03 | [Choose or create an entity](images/03-entities.png) | `company_list`, `company_default`, `company_add` |
| 04 | [Accounts](images/04-accounts.png) | `account_list`, `statement_account_list` |
| 05 | [Account detail](images/05-account-detail-v2.png) | `account_identity_list`, `statement_account_identity_list`, `account_defaults_get` |
| 06 | [Create and configure an account](images/06-account-settings-v3.png) | `account_add`, `account_create`, `account_configure`, `account_defaults_set`, `statement_account_create`, `account_identity_add`, `statement_account_identity_add` |
| 07 | [Account lifecycle](images/07-account-lifecycle-v2.png) | `statement_account_lifecycle_list`, `statement_account_archive`, `statement_account_lifecycle_close_before_coverage` |
| 08 | [Imported activity](images/08-activity.png) | `transaction_list`, `source_list` |
| 09 | [Posted entry detail](images/09-entry-detail-v3.png) | `tx_show`, `journal_show`, `tx_post`, `tx_abandon`, `journal_abandon`, `journal_post` |
| 10 | [Record spending](images/10-spend.png) | `spend` |
| 11 | [Record income](images/11-receive.png) | `receive` |
| 12 | [Record a transfer](images/12-transfer.png) | `transfer` |
| 13 | [Journal editor](images/13-journal-editor-v2.png) | `journal_add`, `journal_create`, `journal_edit`, `journal_validate` |
| 14 | [Journals and batch posting](images/14-journals-v2.png) | `tx_list`, `journal_list`, `journal_import`, `journal_post_batch` |
| 15 | [Correct or reverse an entry](images/15-correction.png) | `correct`, `reverse`, `undo`, `journal_reverse` |
| 16 | [Import files](images/16-imports-v2.png) | `bank_import_formats`, `bank_import_upload`, `bank_import_show`, `bank_import_process` |
| 17 | [Map imported accounts](images/17-import-mapping-v2.png) | `bank_import_show` |
| 18 | [Resolve source identity matches](images/18-import-matches-v2.png) | `bank_import_matches` |
| 19 | [Review and apply an import](images/19-import-preview-v2.png) | `bank_import_preview`, `bank_import_plan`, `bank_import_apply` |
| 20 | [QuickBooks migration](images/20-quickbooks-v2.png) | `import_quickbooks_inspect`, `import_quickbooks_plan`, `import_quickbooks_apply` |
| 21 | [Source evidence and import batches](images/21-evidence-v2.png) | `source_list`, `source_show`, `source_links`, `source_link_journal`, `import_batch_list`, `import_batch_show`, `import_source_read`, `statement_import` |
| 22 | [Evidence file transfer](images/22-files-v2.png) | `artifact_begin`, `artifact_write`, `artifact_finish`, `artifact_read`, `artifact_discard` |
| 23 | [Reconciliations and statement setup](images/23-reconciliations-v2.png) | `reconcile_list`, `reconcile_start` |
| 24 | [Reconciliation workspace](images/24-reconcile-workspace.png) | `reconcile_plan`, `reconcile_allocate`, `reconcile_allocations`, `reconcile_unallocate` |
| 25 | [Complete or reopen reconciliation](images/25-reconcile-complete-v2.png) | `reconcile_apply`, `reconcile_complete`, `reconcile_status`, `reconcile_reopen`, `reconcile_replan`, `reconcile_abandon` |
| 26 | [Income and expenses / profit and loss](images/26-profit-loss-v2.png) | `report_profit_loss` |
| 27 | [Balance sheet](images/27-balance-sheet-v2.png) | `report_balance_sheet` |
| 28 | [Trial balance](images/28-trial-balance-v2.png) | `report_trial_balance` |
| 29 | [General ledger](images/29-general-ledger-v2.png) | `report_general_ledger` |
| 30 | [Periods and optional period close](images/30-periods.png) | `period_list`, `period_create`, `periods_add`, `close_plan`, `close_apply`, `period_close`, `period_reopen` |
| 31 | [Fiscal year close](images/31-year-close-v2.png) | `year_close_plan`, `year_close_apply`, `period_year_close` |
| 32 | [Entities, books and consolidation](images/32-groups-v2.png) | `entity_list`, `entity_create`, `book_list`, `group_list`, `group_create`, `ownership_list`, `ownership_set` |
| 33 | [Audit and integrity](images/33-audit.png) | `audit_list`, `audit_verify` |
| 34 | [Connection and preferences](images/34-settings.png) | `capabilities`, `health`, `config_get`, `config_path`, `config_set` |
| 35 | [Database maintenance and recovery](images/35-maintenance-v2.png) | `db_status`, `db_doctor`, `db_init`, `db_migrate`, `db_backup`, `db_restore` |
| 36 | [Empty, blocked and recovery states](images/36-states.png) | Shared states across operations |
| 37 | [Cash projection — planned](images/37-cash-projection-v2.png) | Planned; no current API |

## Interaction and implementation contract

The page specifications below are authoritative where generated image copy or illustrative values differ. These images do not add API functionality. Read [API](../../api.md), [database operations](../../database-operations.md), [artifacts](../../artifacts.md), [architecture](../../architecture.md), and [workflow guidance](../../agent-workflows.md) when implementing. The latest additive API sections and operation catalog supersede older gap descriptions remaining earlier in API documentation.

- Keep the selected entity visible on all reads and writes. Ordinary pages use company-scoped operations. Database-only functionality must require explicit whole-database authority; never silently broaden access because a company uses the same database. Registry and database administration have independent grants. Personal and business are separate scopes, not a combined dashboard total.
- One functional currency per entity, exact decimal/minor-unit handling, and accrual basis. Display currency and report scope. Never calculate money with floating point. Same-currency, 100%-ownership groups are the supported consolidation perimeter; show actual versus elimination books, and expose intercompany counterparty/key fields in the advanced journal editor where required.
- Source provider status and accounting posting status are separate axes. A provider-booked movement may have no journal; pending/review observations create no journal. Never display an imported balance as a ledger opening balance or automatic reconciliation. Overview totals come from posted reports and must expose incomplete source/posting coverage.
- Supported import classification assigns an explicit revenue, expense or equity contra account to each eligible nonzero booked movement. Source-only imports remain useful without classification. Transfers, loans, receivables/payables and investment accounting use explicit journals or existing transfer workflows. No automatic classifier or mandatory human classification queue is implied.
- Normal drafts may be edited; imported and reversal-derived drafts are immutable. Posted entries are immutable: correct/reverse/undo creates linked records. New accounting writes keep a stable idempotency key through transport retries. A dry run does not reserve the key. Validation success requires checking `valid`, not merely an HTTP success.
- Import preview must show mappings, exclusions with reasons, identity decisions, classifications, totals and resulting journals. Apply the exact retained plan with its digest. A stale plan requires another preview. Successful apply receipts may be replayed; a job cannot be reapplied via a different plan. Never infer duplicate identity solely from equal date and amount.
- Bank import job history is a client-retained list of known job IDs, not a server job-list endpoint. Show parsing state without invented percentages; retain IDs across navigation, support reopening a known job, and process recovery where authorized. There is no job cancellation endpoint. Artifact byte transfer can show byte-count progress, independently from import parsing.
- Table search, filters, comparison percentages and report drilldowns may require client composition of supported reads. Scope such controls to loaded results unless a complete query has been fetched. Transaction pagination is not a synchronization feed or immutable snapshot. Client printing/download of rendered reports is optional presentation work, not a report-export API.
- Reconciliation distinguishes imported statement evidence, posted control lines, signed allocations and reviewed outstanding items. A zero difference alone is insufficient; check all readiness blockers. Manual plan/apply and direct allocation/complete are alternate supported paths. Reopen requires an audit reason; rebuild the exact interval, preserve outstanding evidence and handle successors in order.
- Period locks and year close are optional administrative workflows for continuous bookkeeping. The system can require controls when a user elects to close. Display complete plans and blockers before apply, preserve exact plans, and require reasons to reopen. Closing/reopening order must follow backend rules.
- Artifact storage uses scoped opaque references, hashes, byte limits and resumable offsets. It does not expose arbitrary server paths. Temporary unretained files may be discarded; retained evidence cannot be deleted. Preserve retained external artifacts with database backups. Upload limits remain 8 MiB for statements, even through the 256 MiB artifact transport.
- Restore previews lineage, identity and source verification before confirmation of the configured database handle. Initialization is only for a missing authorized target. Migration and restore need exclusive maintenance access. After an uncertain response, inspect status/identity/audit before retrying; unlike ordinary entry/import writes, they have no universal exactly-once receipt.
- Audit verification and Doctor report integrity checks, not complete or professionally certified accounts. Connection setup uses operator-provisioned credentials. Capability availability is not proof of authorization; enforce grants server-side and handle 403 responses. No user signup/invites, bank OAuth, live bank feed, payment execution, invoicing, payroll, tax filing or offline writes are part of this set.
- Cash projection is explicitly planned. It needs obligations, scheduling assumptions, account floors and a forecast engine that are not in the current operation catalog. Actuals and forecasts stay distinct. Proposed transfers have send/arrival assumptions and do not execute a bank transfer.

## Page requirements

### 01 — Personal overview

Maple Household entity selected, Personal subtitle. Overview with posted cash $8,400, posted liabilities $1,200, net assets $7,200. Source coverage panel: 24 imported movements, 20 posted, 4 source-only; latest imported date Sep 12. Show Income $5,000, Spending $3,200 for September as posted income/expenses, not cash movement. Main recent activity and reconciliation status. Import activity primary action, Record menu. No future prediction.

[Desktop and mobile design](images/01-personal-overview-v2.png) · [Generation prompt](prompts/01-personal-overview.txt)

### 02 — Business overview

Example Studio entity selected, Business subtitle. Overview with September revenue $12,000, expenses $8,000, profit $4,000, basis Accrual. Recent posted activity and open work: 3 draft entries and 1 reconciliation in progress. Postings and source coverage explicit. Button Record transaction, secondary Import. No mandatory monthly-close task, no invoicing/bill-pay controls.

[Desktop and mobile design](images/02-business-overview-v2.png) · [Generation prompt](prompts/02-business-overview.txt)

### 03 — Choose or create an entity

Entity chooser with Personal and Business sections: Maple Household and Example Studio. Default badge and Set as default menu. Active right-hand create entity form, name, unique code, currency USD, start date, fiscal year end, starter chart or empty chart; basis Accrual fixed. Create entity CTA. Personal/Business grouping is presentation metadata proposed in client, not an existing API entity-type field. Mobile full-page create form with back to chooser.

[Desktop and mobile design](images/03-entities.png) · [Generation prompt](prompts/03-entities.txt)

### 04 — Accounts

Chart of accounts screen with tabs All, Assets, Liabilities, Equity, Income, Expenses; codes names types, posted balance and statement control status. Checking 1000 $8,400; Card 2000 $1,200 owed. Add account button. Distinguish linked reconciliation account from GL. Mobile readable account cards and filter sheet button, no squeezed table.

[Desktop and mobile design](images/04-accounts.png) · [Generation prompt](prompts/04-accounts.txt)

### 05 — Account detail

Checking account detail with posted balance $8,400 and separately last imported balance $8,600 as of Sep 12, never silently combined. Activity, Source identities, Settings tabs. Reconciliation coverage through Aug31. Source identity institution + stable ID masked, evidence link. Default payment and deposit badges. Mobile stacked summary and activity drilldown.

[Desktop and mobile design](images/05-account-detail-v2.png) · [Generation prompt](prompts/05-account-detail.txt)

### 06 — Create and configure an account

Account settings form: code 1000, name Checking, type Bank, currency inherited USD, active from, linked statement control and coverage start. Separate default payment/deposit toggles. Source identity subsection bank identifier, account identifier, evidence file with Add identity. Save settings button, Preview changes. Administrator advanced book/date eligibility behind disclosure. No delete posted account.

[Desktop and mobile design](images/06-account-settings-v3.png) · [Generation prompt](prompts/06-account-settings.txt)

### 07 — Account lifecycle

Account lifecycle page: Old Savings, evidence timeline, active from and archived dates, reconciliation coverage. Open Archive account panel asking effective date, reason, supporting evidence, Preview archive. Separate exceptional Closed before coverage action with evidence upload and preview. Show history retained and new activity disabled after archive; no destructive delete. Mobile archive form with evidence attachment.

[Desktop and mobile design](images/07-account-lifecycle-v2.png) · [Generation prompt](prompts/07-account-lifecycle.txt)

### 08 — Imported activity

Imported activity list with columns date, description, account, amount, source status and linked entry. Tabs Booked, Pending, All observations. Pending $42.00 badge Source only; posted source movement $75.00 linked to entry42. Header Source activity, separate Posted entries navigation. Filters date/account/status are client-side where backend lacks filtering; sample rows all synthetic. No approve-every-item queue.

[Desktop and mobile design](images/08-activity.png) · [Generation prompt](prompts/08-activity.txt)

### 09 — Posted entry detail

Entry42 detail, posted Sep12, Office supplies $75.00, immutable posted badge. Balanced lines Supplies debit75 Checking credit75. Evidence link and audit trail. Overflow Correct, Reverse, Undo; no Edit for posted state. Small clearly separate Draft variant card offering Validate, Post entry, Abandon draft. Mobile same detail with action sheet.

[Desktop and mobile design](images/09-entry-detail-v3.png) · [Generation prompt](prompts/09-entry-detail.txt)

### 10 — Record spending

Record spending page in Maple Household. Amount $75.00 USD, From Checking, Category Household supplies, date Sep12, description and reference fields. Debit Supplies75 Credit Checking75 preview. Primary Post spending, secondary Save draft, Preview action. Short note Records accounting; does not send a payment. Mobile single-column form with sticky footer.

[Desktop and mobile design](images/10-spend.png) · [Generation prompt](prompts/10-spend.txt)

### 11 — Record income

Record income page for Example Studio. Amount $2,500.00 USD, Deposit to Checking, Income account Services, date Sep12, description Design work, reference optional. Balanced preview debitChecking2500 creditServices2500. Primary Post income, secondary Save draft, Preview. Records accounting; does not collect money. Mobile stacked form.

[Desktop and mobile design](images/11-receive.png) · [Generation prompt](prompts/11-receive.txt)

### 12 — Record a transfer

Record transfer page Maple Household: From Checking, To Savings, amount $1,000.00 USD, date Sep12, memo Reserve funding. Diagram Checking to Savings. DebitSavings1000 CreditChecking1000. Explicit text Records a transfer already made; does not move money. Post transfer primary, Save draft secondary. Both accounts same entity and currency. Mobile stacked selectors and fixed action.

[Desktop and mobile design](images/12-transfer.png) · [Generation prompt](prompts/12-transfer.txt)

### 13 — Journal editor

Draft journal editor for Example Studio, posting date Sep12, description Insurance allocation, line editor Prepaid insurance credit100 and Insurance expense debit100. Add line. Footer Debits $100 Credits $100 Difference $0 Balanced. Validate, Save draft, Post entry. Book selector advanced authorized database mode only. Mobile line cards with edit line sheet, large totals and sticky save.

[Desktop and mobile design](images/13-journal-editor-v2.png) · [Generation prompt](prompts/13-journal-editor.txt)

### 14 — Journals and batch posting

Journal list with All, Draft, Posted tabs. Draft tab active with 3 selected drafts, balanced badges, source type and dates. Batch toolbar Validate selected and Post 3 entries. Import journal JSON button with reviewed import drawer showing file, 3 balanced entries, Save drafts. Draft batch commits together. Mobile list selection mode and bottom action bar.

[Desktop and mobile design](images/14-journals-v2.png) · [Generation prompt](prompts/14-journals.txt)

### 15 — Correct or reverse an entry

Correct entry42 screen with original immutable entry in left card, replacement Supplies60 and Postage15 against Checking75, reason field Split original category. Preview: linked reversal plus replacement, net cash unchanged. Primary Post correction. Tabs Correct, Reverse, Undo expose alternate workflows with date and reason, never edit original. Mobile original summary then replacement and sticky preview.

[Desktop and mobile design](images/15-correction.png) · [Generation prompt](prompts/15-correction.txt)

### 16 — Import files

Import activity hub with upload dropzone and supported formats selector: CSV/TSV/XLSX, OFX/QFX/QBO, QIF, CAMT, MT940/942, BAI2/BTRS, CODA, CFONB120, Norma43. File limit8MiB. CSV profile date format, decimal separator, signed amount column and status mapping. Recent uploads section explicitly This device, status Ready/Parsing/Failed; resume known job field. Retry processing for failed job, no cancel or fake percentage. Mobile file picker and profile sheet.

[Desktop and mobile design](images/16-imports-v2.png) · [Generation prompt](prompts/16-imports.txt)

### 17 — Map imported accounts

Import wizard step2 Map accounts. Synthetic statement.csv with Checking and Savings source accounts, target selectors existing statement controls or Create account. Explicit Exclude with required reason. Parsed date/description/amount/status sample table. CurrencyUSD check. Mode Source only versus Classify and post. Continue to matching. Mobile one account per card with parsed rows expandable.

[Desktop and mobile design](images/17-import-mapping-v2.png) · [Generation prompt](prompts/17-import-mapping.txt)

### 18 — Resolve source identity matches

Import wizard match inspection: two same-date same-amount records are candidates, not proven duplicates. Source purchase75 versus existing75 side by side with stable IDs and evidence. Radios Same movement with existing source selection, Separate movement, required Reason. Footer 1 decision remaining, Preview import disabled until complete. Mobile vertically stacked comparison cards and action sheet. This is explicit identity resolution, not blanket human classification approval.

[Desktop and mobile design](images/18-import-matches-v2.png) · [Generation prompt](prompts/18-import-matches.txt)

### 19 — Review and apply an import

Import preview page with Source only mode, 24 booked movements, 4 pending observations, zero journals to create. Mapped Checking, evidence retained, exact preview timestamp. Apply import primary. Optional Classify and post section showing explicit category assignments when chosen, not automatic classification. Receipt variant bottom shows Applied,24 source transactions,0 journals and View activity. Stale variant subtle amber banner Changes detected; Create new preview. Mobile summary cards, expanded changes and sticky Apply.

[Desktop and mobile design](images/19-import-preview-v2.png) · [Generation prompt](prompts/19-import-preview.txt)

### 20 — QuickBooks migration

QuickBooks migration wizard: authorized uploaded GeneralLedger JSON and Account JSON, or reviewed journal XLSX and account catalog, not live OAuth sync. Inspect result 8 accounts,12 balanced entries. Plan preview source range and target Example Studio USD, required statement coverage. Save as drafts option, Apply migration CTA, dry-run preview. Receipt link on success. Mobile file cards and step progress.

[Desktop and mobile design](images/20-quickbooks-v2.png) · [Generation prompt](prompts/20-quickbooks.txt)

### 21 — Source evidence and import batches

Evidence library with Sources and Import batches tabs. Selected statement source detail shows original document Download, integrity hash abbreviated, imported account, retained observations, linked entry42. Link to journal action, reason/context and journal picker. Advanced Import normalized statement evidence action. Batch detail table rows source, entry, result. Mobile source list to full detail. Evidence is immutable; no Delete evidence control.

[Desktop and mobile design](images/21-evidence-v2.png) · [Generation prompt](prompts/21-evidence.txt)

### 22 — Evidence file transfer

Evidence files transfer page with authorized scope Example Studio, upload file row, byte-count progress permitted for artifact transfer, Verify and finish button and SHA-256 verification badge. Uploaded backup256KiB ready to download. Discard unfinished upload action separate; not deleting committed accounting evidence. Maximum256MiB. Mobile file picker, upload bytes, verification then Download. No arbitrary server filesystem path browser.

[Desktop and mobile design](images/22-files-v2.png) · [Generation prompt](prompts/22-files.txt)

### 23 — Reconciliations and statement setup

Reconciliations page with accounts Checking/Card and completed Aug31 versus in-progress Sep. Start reconciliation form account, interval Sep1-Sep30, beginning $8,000, statement ending $8,400 and statement evidence selector. Card balances accept positive amount owed. Start button. Mobile list and full-screen setup. No forced period close.

[Desktop and mobile design](images/23-reconciliations-v2.png) · [Generation prompt](prompts/23-reconciliations.txt)

### 24 — Reconciliation workspace

Checking reconciliation matching workspace. Statement activity left, posted control lines right, selected $400 pair, Allocate button. Allocated items and Unallocate action. Cleared and Outstanding tabs with carry-forward items; summary statement adjusted8400, book8400, difference0. Create completion plan CTA; expose manual plan through date/ending/cleared selections. Mobile one comparison card at time, selected pair and sticky totals.

[Desktop and mobile design](images/24-reconcile-workspace.png) · [Generation prompt](prompts/24-reconcile-workspace.txt)

### 25 — Complete or reopen reconciliation

Reconciliation completion review, Checking Sep30, ending8400, difference0,2 outstanding payments retained, review interval and allocation summary. Complete reconciliation CTA. Below clear alternate Completed state receipt with Reopen requiring reason, and rebuild this interval action after reopening. Abandon only open session. Stale plan needs regenerate. Mobile complete summary plus reopen sheet. No claim source import alone reconciles.

[Desktop and mobile design](images/25-reconcile-complete-v2.png) · [Generation prompt](prompts/25-reconcile-complete.txt)

### 26 — Income and expenses / profit and loss

Profit and loss report Example Studio, entity/group scope selector, Sep1-Sep30, Accrual USD, posted entries only. Revenue Services12000; Expenses Contractors5000 Software1000 Rent2000 total8000; Profit4000. Clean report hierarchy and right aligned currency, Include zero accounts toggle, account drilldown. Mobile section cards preserve totals. Personal equivalent label Income and expenses specified small subtitle, not a second competing report.

[Desktop and mobile design](images/26-profit-loss-v2.png) · [Generation prompt](prompts/26-profit-loss.txt)

### 27 — Balance sheet

Balance sheet report Maple Household as of Sep30, Accrual USD, posted entries only. Assets Checking8400 Savings3600 Total12000. Liabilities Card1200 Total1200. Equity10800. Assets12000=Liabilities1200+Equity10800. Date picker, scope selector, includezero. Mobile collapsible sections with all subtotal rows visible, never call ledger assets complete net worth when coverage missing.

[Desktop and mobile design](images/27-balance-sheet-v2.png) · [Generation prompt](prompts/27-balance-sheet.txt)

### 28 — Trial balance

Trial balance Example Studio Sep30 USD Accrual. Debit Checking10000 Expenses8000; Credit Revenue12000 Equity6000; totals18000 each difference0. Account codes labels debit credit columns, includezero, posted only. Mobile card per account and sticky balanced totals. Empty account zero option available.

[Desktop and mobile design](images/28-trial-balance-v2.png) · [Generation prompt](prompts/28-trial-balance.txt)

### 29 — General ledger

General ledger Checking Sep1-Sep30 USD Accrual posted only. Opening8000; Sep10 Client receipt+1000 running9000; Sep12 Supplies-75 running8925; Sep15 Rent-525 running8400; Closing8400. Account/date/scope controls and includezero. Rows link entry IDs. Mobile chronological ledger cards with amount and running balance, no tiny compressed table.

[Desktop and mobile design](images/29-general-ledger-v2.png) · [Generation prompt](prompts/29-general-ledger.txt)

### 30 — Periods and optional period close

Periods settings Example Studio fiscal2026 list months Open or Locked. Header Optional period locks, regular bookkeeping continues in open periods. Add fiscal year button. Close review drawer for August with reconciliation coverage and blockers, Review plan then Apply lock; disabled when blocker. Reopen locked period requires reason. Advanced create explicit period disclosed. Mobile monthcards and close review sheet.

[Desktop and mobile design](images/30-periods.png) · [Generation prompt](prompts/30-periods.txt)

### 31 — Fiscal year close

Fiscal year close2026 review Example Studio, retained earnings account3000 selector, temporary account balances income12000 expenses8000, net4000 transfer to retained earnings. Preview balanced closing journal and prerequisites, Apply year close CTA. Idempotent completed receipt alternate below. Mobile summary and expandable lines, blocked state if periods/control requirements unmet. Administrative optional action, not home dashboard nag.

[Desktop and mobile design](images/31-year-close-v2.png) · [Generation prompt](prompts/31-year-close.txt)

### 32 — Entities, books and consolidation

Advanced workspace topology settings. Example Group with Example Studio and Example Services100% ownership, effective dates and USD. Entity/book table with Actual and Elimination book roles, no unsupported Create book button. Add entity, Create group, Set ownership actions. Ownership form parent child100% effective date evidence, samecurrency only. Open consolidated reports. Mobile hierarchy list and ownership edit sheet. Separate database grant context shown unobtrusively.

[Desktop and mobile design](images/32-groups-v2.png) · [Generation prompt](prompts/32-groups.txt)

### 33 — Audit and integrity

Audit history page event table time actor action scope outcome, filter local results, selected import-applied event with evidence hashes and linked receipt. Verify audit chain button. Status Integrity check passed with explanatory text Checks recorded history integrity; does not certify complete accounting. Mobile timeline detail and Verify. No editable audit events.

[Desktop and mobile design](images/33-audit.png) · [Generation prompt](prompts/33-audit.txt)

### 34 — Connection and preferences

Settings with Connection tab server address https://books.example.test, masked bearer token input never actualsecret, Connected status and available capabilities. Access badges Read Import Post Manage according to provisioned grants; request access from operator explanatory copy no fake user administration. Preferences default entity and output preference advanced operator registry section. Config location read-only and redact paths. Mobile section list and connect form; no bank OAuth buttons.

[Desktop and mobile design](images/34-settings.png) · [Generation prompt](prompts/34-settings.txt)

### 35 — Database maintenance and recovery

Administrator maintenance page selected database Example Studio, schema current, healthy. Run diagnostics, Create verified backup, Download backup. Restore panel choose verified backup artifact, Preview restore verifies same lineage, confirmation type example-studio then Restore. Migration preview and initialize empty configured target in advanced section. DATABASE_BUSY banner requires ending active connections, no force option. Mobile same operations as stacked sections and restore confirmation sheet. No arbitrary database path entry.

[Desktop and mobile design](images/35-maintenance-v2.png) · [Generation prompt](prompts/35-maintenance.txt)

### 36 — Empty, blocked and recovery states

Four representative states in one design board, each shown desktop and mobile as compact paired panels: empty ledger with Import activity and Record entry; permission denied with read-only navigation and contact operator; stale preview with Create new preview preserving choices; disconnected after write with Checking result status and retry same request, never automatic duplicate. Keep all text readable, distinct state cards, design states rather than new pages. No real data.

[Desktop and mobile design](images/36-states.png) · [Generation prompt](prompts/36-states.txt)

### 37 — Cash projection — planned

Maple Household cash projection, visible Planned feature badge. Per-account navigator Spending, Reserves, Cards. Checking projected daily stair-step balance opening1200, bill900 lowers300, incoming6000 raises6300, bill5300 lowers1000. Minimum buffer500 amber warning at300. Daily cash ledger linked to selected incoming event, actual solid versus forecast dashed clearly labeled, date range and Day Week Month. Mobile account picker, chart, then daily eventcards and transfer detail sheet. Proposed transfer details Send Sep15 Arrive Sep16, no live move-money button. Forecast engine and obligations backend not yet implemented. Use synthetic accounts only.

[Desktop and mobile design](images/37-cash-projection-v2.png) · [Generation prompt](prompts/37-cash-projection.txt)


## Review and validation

All 37 selected boards were visually inspected, with targeted imagegen refinements
for accounting semantics, source identity, report controls and mobile actions.
Generated text, icons, dates and spacing remain illustrative; the written behavior
contract governs implementation. This set does not claim a pixel-perfect production
UI or an exhaustive image of every possible validation error.

The operation mapping, PNG integrity, manifest hashes, and local document/gallery
links were checked directly. The in-app browser blocked local file-URL preview,
so browser rendering of the static gallery was not verified. No accounting code
or live financial database was changed. The full backend test suite was not
required for this design/documentation-only change.
