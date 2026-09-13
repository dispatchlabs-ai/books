# Proposed MCP capability coverage

Design companion to [MCP server design](mcp-design.md). **Not implemented.**
Baseline: Books `2c492eef297d778e064531eaa4448b5494890999`, September 13, 2026.

Inventory was checked against CLI command constructors and recursive public help
using an isolated configuration. It includes the runnable `periods` parent as
well as leaf commands; generated help/completion are interface conveniences.
Aliases share tools only when parameters preserve their complete semantics.
The table covers command families, not yet every flag: implementation acceptance
requires per-argument and output coverage tests, including existing dry runs.

| CLI operation | Proposed MCP tool | Mapping note |
| --- | --- | --- |
| `account add` | `books_account_add` | — |
| `account configure` | `books_account_configure` | — |
| `account create` | `books_account_create` | — |
| `account identity add` | `books_account_identity_add` | — |
| `account identity list` | `books_account_identity_list` | — |
| `account list` | `books_account_list` | — |
| `accounts` | `books_account_list` | — |
| `audit list` | `books_audit_list` | — |
| `audit verify` | `books_audit_verify` | — |
| `backup` | `books_db_backup` | company target; low-level db form uses database target and database-wide authorization |
| `bank-import apply` | `books_bank_import_apply` | — |
| `bank-import formats` | `books_bank_import_formats` | — |
| `bank-import matches` | `books_bank_import_matches` | — |
| `bank-import plan` | `books_bank_import_plan` | — |
| `bank-import preview` | `books_bank_import_preview` | — |
| `bank-import process` | `books_bank_import_process` | — |
| `bank-import show` | `books_bank_import_show` | — |
| `bank-import upload` | `books_bank_import_upload` | — |
| `book list` | `books_book_list` | — |
| `bs` | `books_report_balance_sheet` | — |
| `close apply` | `books_close_apply` | — |
| `close plan` | `books_close_plan` | — |
| `companies` | `books_company_list` | — |
| `company add` | `books_company_add` | — |
| `company default` | `books_company_default` | — |
| `company list` | `books_company_list` | — |
| `config get` | `books_config_get` | — |
| `config path` | `books_config_path` | — |
| `config set` | `books_config_set` | — |
| `correct` | `books_correct` | — |
| `db backup` | `books_db_backup` | — |
| `db doctor` | `books_db_doctor` | — |
| `db init` | `books_db_init` | — |
| `db migrate` | `books_db_migrate` | — |
| `db restore` | `books_db_restore` | — |
| `db status` | `books_db_status` | — |
| `doctor` | `books_db_doctor` | company target; low-level db form uses database target and database-wide authorization |
| `entity create` | `books_entity_create` | — |
| `entity list` | `books_entity_list` | — |
| `gl` | `books_report_general_ledger` | — |
| `group create` | `books_group_create` | — |
| `group list` | `books_group_list` | — |
| `import quickbooks apply` | `books_import_quickbooks_apply` | — |
| `import quickbooks inspect` | `books_import_quickbooks_inspect` | — |
| `import quickbooks plan` | `books_import_quickbooks_plan` | — |
| `import-batch list` | `books_import_batch_list` | — |
| `import-batch show` | `books_import_batch_show` | — |
| `init` | `books_company_add` | initialize=true preserves initial-registry semantics |
| `journal abandon` | `books_journal_abandon` | — |
| `journal add` | `books_journal_add` | — |
| `journal create` | `books_journal_create` | — |
| `journal edit` | `books_journal_edit` | — |
| `journal import` | `books_journal_import` | — |
| `journal list` | `books_journal_list` | — |
| `journal post` | `books_journal_post` | — |
| `journal post-batch` | `books_journal_post_batch` | — |
| `journal reverse` | `books_journal_reverse` | Creates a draft reversal; distinct from posted transaction reverse |
| `journal show` | `books_journal_show` | — |
| `journal validate` | `books_journal_validate` | — |
| `ownership list` | `books_ownership_list` | — |
| `ownership set` | `books_ownership_set` | — |
| `period close` | `books_period_close` | — |
| `period create` | `books_period_create` | — |
| `period list` | `books_period_list` | — |
| `period reopen` | `books_period_reopen` | — |
| `period year-close` | `books_period_year_close` | — |
| `periods` | `books_period_list` | — |
| `periods add` | `books_periods_add` | — |
| `pl` | `books_report_profit_loss` | — |
| `receive` | `books_receive` | — |
| `reconcile abandon` | `books_reconcile_abandon` | — |
| `reconcile allocate` | `books_reconcile_allocate` | — |
| `reconcile allocations` | `books_reconcile_allocations` | — |
| `reconcile apply` | `books_reconcile_apply` | — |
| `reconcile complete` | `books_reconcile_complete` | — |
| `reconcile list` | `books_reconcile_list` | — |
| `reconcile plan` | `books_reconcile_plan` | — |
| `reconcile reopen` | `books_reconcile_reopen` | — |
| `reconcile replan` | `books_reconcile_replan` | — |
| `reconcile start` | `books_reconcile_start` | — |
| `reconcile status` | `books_reconcile_status` | — |
| `reconcile unallocate` | `books_reconcile_unallocate` | — |
| `reopen` | `books_period_reopen` | — |
| `report balance-sheet` | `books_report_balance_sheet` | — |
| `report general-ledger` | `books_report_general_ledger` | — |
| `report profit-loss` | `books_report_profit_loss` | — |
| `report trial-balance` | `books_report_trial_balance` | — |
| `restore` | `books_db_restore` | company target; low-level db form uses database target and database-wide authorization |
| `reverse` | `books_reverse` | — |
| `serve` | `process launch` | books serve remains an operator launch command; MCP serves equivalent data operations without launching a nested listener |
| `source link-journal` | `books_source_link_journal` | — |
| `source links` | `books_source_links` | — |
| `source list` | `books_source_list` | — |
| `source show` | `books_source_show` | — |
| `spend` | `books_spend` | — |
| `statement import` | `books_statement_import` | — |
| `statement-account archive` | `books_statement_account_archive` | — |
| `statement-account create` | `books_statement_account_create` | — |
| `statement-account identity add` | `books_statement_account_identity_add` | — |
| `statement-account identity list` | `books_statement_account_identity_list` | — |
| `statement-account lifecycle close-before-coverage` | `books_statement_account_lifecycle_close_before_coverage` | — |
| `statement-account lifecycle list` | `books_statement_account_lifecycle_list` | — |
| `statement-account list` | `books_statement_account_list` | — |
| `tb` | `books_report_trial_balance` | — |
| `transaction list` | `books_transaction_list` | — |
| `transfer` | `books_transfer` | — |
| `tx abandon` | `books_tx_abandon` | — |
| `tx list` | `books_tx_list` | — |
| `tx post` | `books_tx_post` | — |
| `tx show` | `books_tx_show` | — |
| `undo` | `books_undo` | — |
| `year-close apply` | `books_year_close_apply` | — |
| `year-close plan` | `books_year_close_plan` | — |

## Additional HTTP and MCP surfaces

The HTTP API also supplies health, authorized company listing, account defaults,
fiscal-period setup, dashboard summaries, pending import work, and complete
uploaded source downloads. Map these to `books_health`, `books_company_list`,
`books_account_defaults_get`, `books_account_defaults_set`, `books_periods_add`,
`books_dashboard`, `books_import_pending`, and `books_import_source_read`.
Match suggestions remain `books_bank_import_matches`. Transaction, correction,
reporting and close endpoints map to the corresponding tools above; preserve
permission checks for closing journals and all request fields.

MCP-specific supporting tools: `books_capabilities`, `books_operation_status`,
`books_artifact_read`, `books_artifact_export`, `books_file_stage`,
`books_file_stage_chunk`, `books_file_stage_finish`, `books_plan_get`,
`books_restore_plan`, and `books_migration_plan`. These support discovery,
bounded transfer, retry recovery and administrative previews; they do not grant
capabilities beyond the launch policy.

The full-owner catalog must expose every accounting and administrative operation
above. Restricted catalogs intentionally expose only authorized tools. Startup,
shutdown, shell completion, help formatting and client connection setup are
process/interface lifecycle, not remotely callable accounting tools. Version and
resolved configuration information are returned through `books_capabilities`
and `books_config_path` under their access rules.
