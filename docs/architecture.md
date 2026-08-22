# Architecture

## Ledger model

One database can contain many legal entities. Each entity has one active actual book and one functional currency. A consolidation group identifies a parent entity; its perimeter is the parent plus every recursively owned descendant whose effective-dated ownership path is active for the report date or range. Books currently permits only 100% ownership and USD consolidation.

Every group has one elimination book. Entity books remain separate, and consolidated reports sum the ownership-derived perimeter plus posted elimination entries. The ownership graph is the only source of consolidation membership.

The chart of accounts is global so consolidated rows have stable identities. `book_accounts` controls where and when each account may receive postings. External account identities are immutable, entity-scoped mappings with source hashes and locators.

## Journals and periods

Journals begin as drafts. Posting validates:

- at least two lines and nonzero, equal debit and credit totals;
- an open period containing the posting date;
- account activation in the selected book on that date;
- exact reversal and fiscal-closing rules when applicable; and
- intercompany counterparty/key pairs when supplied.

Posted journals and lines cannot be changed or deleted. A correction is an exact linked reversal followed by a new journal. Imported or reversal-derived drafts are also immutable and must be abandoned and recreated from corrected evidence.

Periods close in chronological order. A year-end period with nonzero operating balances requires one exact closing journal: every revenue and expense account is reduced to zero and the offset goes to one equity account. Reopening proceeds in reverse order. A closed fiscal year can be corrected by reopening its year-end period, exactly reversing the close, posting the correction, closing again, and producing a new exact close.

## Source evidence and reconciliation

Every import batch is idempotent by source system and content hash. A `source_identity` is the immutable logical provider key: source system, source account, and provider external ID, bound to one entity, book, and statement or journal materialization kind. Each provider observation is a new immutable `source_record` revision. Revisions point to the observation they supersede; `current_source_records` exposes exactly one chain head per identity while the base table retains full history.

Every observation has one disposition:

- `POSTED`: may materialize as a statement transaction and link to journals;
- `PENDING`: retained but not posted;
- `NEEDS_REVIEW`: retained but not posted; or
- `SOURCE_ONLY`: evidence outside the active reconciliation boundary.

Provider observations may refresh a pending item, move it to review, or finalize it as posted. Moving a pending item to source-only (including a provider row removed before posting), or moving a review/source-only item to another disposition, is a resolution and requires a nonempty reason plus immutable JSON evidence. A posted observation is terminal. A current posted statement identity has exactly one statement transaction tied to the exact observation; a current posted journal identity has exactly one primary journal link. Non-posted observations have no materialization.

The many-to-many source/journal link table records `PRIMARY`, `EVIDENCE`, `MIRROR`, and `ELIMINATION` roles. An `EVIDENCE` link must remain in the source identity's exact book; cross-book propagation uses an explicit mirror or elimination role. Cross-entity links likewise require an explicit mirror or elimination role.

A statement account maps one bank, card, loan, or investment account to one GL control account. Its immutable identity rows form a many-to-one alias set: registries, import files, providers, and brokerage identifiers can all point to the same statement account. External IDs are realm-local, so uniqueness is enforced on source system, source realm, and external ID together; the same provider ID may exist in different company realms without collision. Registries and providers with globally scoped IDs use the explicit `GLOBAL` realm. Every alias retains the observed number, name, active flag, source hash, locator, optional payload hash, timestamp, and actor; source identity fields never live on the statement account itself.

A completed reconciliation requires exact statement beginning and ending balances, exact statement activity, and exact adjusted GL balances after immutable reviewed outstanding items. Every statement transaction and cleared control-account line is fully signed-allocated; every residual eligible control-line amount is explicitly reviewed as outstanding and carried into the next interval until it clears. Completed periods cannot overlap; a later interval carries forward the prior statement ending balance and separately adjusts its raw book opening for the prior outstanding evidence. Manual reconciliation mirrors retain immutable operator-attestation provenance and are exposed as `OPERATOR_ATTESTATION`, not provider observations. Their external ID and six-field raw payload bind the exact plan digest, journal entry and line numbers, ledger and statement dates, and provenance to the one allocated control line, so a merely equal amount/date/account on another journal cannot substitute. `reconciliation_required_from` and `reconciliation_required_through` bound formal period-close coverage; they do not claim when the underlying legal account opened or closed. Bank and investment statement accounts require asset controls; card and loan statement accounts require liability controls.

If an exact provider identity was legally closed before `reconciliation_required_from`, no statement can exist for the later required period. Books can make that formal coverage interval empty only with one immutable precoverage-closure certificate. The certificate retains the reviewed input hash, official closure-letter path and hash, exact provider-account snapshot path, file and payload hashes, archived/closed status, exact-zero current and available balances, holder and account suffix, dates, actor, and reason. A separate immutable binding records the count and canonical SHA-256 digest of every active identity row, including its complete mapping and evidence fields; its own audit event binds that set to the certificate.

Certification requires an active selected identity, rejects active aliases in the same provider realm that identify different account numbers, and binds all other reviewed aliases. Every current statement-source observation must already be terminal: `POSTED` with its exact materialization, or explicitly resolved `SOURCE_ONLY`. `PENDING` and `NEEDS_REVIEW` are blockers regardless of date because archiving would make their later supported resolution impossible. The service also requires real canonical calendar dates, absolute retained evidence paths, a zero control at closure and currently, no later posted control line, and no incompatible statement or reconciliation evidence. It then archives the statement account at the required-from boundary. New aliases, source observations or revisions, and reconciliation reopens are prohibited afterward; exact identity retries remain idempotent. Doctor and supported period close rederive the complete accounting lifecycle and require exactly one payload-consistent closure audit event and one payload-consistent identity-binding audit event on a valid hash chain. The certificate—not a fabricated zero statement, reconciliation, journal, or plug—is what proves the provider account could not exist during formal coverage.

Mistaken open reconciliation work is terminally abandoned with an audit reason. An abandoned reconciliation retains its evidence, cannot be changed or deleted, and is excluded from overlap, continuity, archive-boundary, and period-close coverage calculations.

## Reports

Reports read posted accrual-basis journals only and explicitly label the resolved scope and each scoped book `ACCRUAL`; unsupported bases are rejected. Trial balance and general ledger amounts use debit-minus-credit signs; financial statements display natural signs. Profit and loss excludes closing and closing-reversal entries. Balance sheets include unclosed current earnings and must satisfy assets = liabilities + equity exactly. Range-based group reports apply ownership dates to each posting and synthesize perimeter entry/exit balances where needed so the general ledger closes to the trial balance.

## SQLite integrity

The database uses foreign keys, recursive triggers, immediate write transactions, `DELETE` journaling, and `EXTRA` synchronous durability. New databases are created directly from the complete native schema v1. The application validates:

- a fixed SQLite application ID and schema version;
- a complete, checksum-verified migration ledger;
- every live table, index, view, and trigger against a pristine embedded schema for the database's recorded version;
- SQLite integrity and foreign keys;
- balanced and structurally valid posted journals;
- closed-period balance digests;
- completed reconciliation and source-link invariants;
- every source payload and resolution-evidence hash against its stored JSON; and
- the complete hash-chained audit log.

Database creation is exclusive. Backup and restore publish files atomically and roll back failed filesystem swaps.
Future schema migrations must validate the exact recorded source-version manifest before destructive DDL. Each migration's SQL, data transformation, ledger row, `user_version`, and exact target-version verification share one transaction, so a target failure rolls the version back.
