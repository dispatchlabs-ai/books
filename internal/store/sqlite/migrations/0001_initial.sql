PRAGMA application_id = 1112493899;
PRAGMA user_version = 1;

-- Books native schema v1. New databases are created directly at this schema.

CREATE TABLE database_metadata (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    database_uuid TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL,
    base_currency TEXT NOT NULL CHECK (length(base_currency) = 3 AND base_currency = upper(base_currency))
) STRICT;

CREATE TABLE entities (
    id TEXT PRIMARY KEY,
    code TEXT NOT NULL UNIQUE CHECK (length(code) BETWEEN 2 AND 32 AND code = upper(code)),
    legal_name TEXT NOT NULL CHECK (length(trim(legal_name)) > 0),
    functional_currency TEXT NOT NULL CHECK (length(functional_currency) = 3 AND functional_currency = upper(functional_currency)),
    status TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'ARCHIVED')),
    created_at TEXT NOT NULL,
    archived_at TEXT
) STRICT;

CREATE TABLE consolidation_groups (
    id TEXT PRIMARY KEY,
    code TEXT NOT NULL UNIQUE CHECK (length(code) BETWEEN 2 AND 32 AND code = upper(code)),
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    parent_entity_id TEXT NOT NULL REFERENCES entities(id) ON DELETE RESTRICT,
    currency TEXT NOT NULL CHECK (length(currency) = 3 AND currency = upper(currency)),
    status TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'ARCHIVED')),
    created_at TEXT NOT NULL
) STRICT;

CREATE TABLE ownership_interests (
    id TEXT PRIMARY KEY,
    parent_entity_id TEXT NOT NULL REFERENCES entities(id) ON DELETE RESTRICT,
    child_entity_id TEXT NOT NULL REFERENCES entities(id) ON DELETE RESTRICT,
    ownership_bps INTEGER NOT NULL CHECK (ownership_bps = 10000),
    effective_from TEXT NOT NULL CHECK (effective_from GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    effective_to TEXT CHECK (effective_to IS NULL OR effective_to >= effective_from),
    created_at TEXT NOT NULL,
    CHECK (parent_entity_id <> child_entity_id)
) STRICT;

CREATE UNIQUE INDEX ownership_interests_open_child_unique
    ON ownership_interests(child_entity_id)
    WHERE effective_to IS NULL;

CREATE INDEX ownership_interests_parent_effective
    ON ownership_interests(parent_entity_id, effective_from, effective_to);

CREATE INDEX ownership_interests_child_effective
    ON ownership_interests(child_entity_id, effective_from, effective_to);

CREATE TABLE books (
    id TEXT PRIMARY KEY,
    code TEXT NOT NULL UNIQUE CHECK (length(code) BETWEEN 2 AND 32 AND code = upper(code)),
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    kind TEXT NOT NULL CHECK (kind IN ('ACTUAL', 'ELIMINATION')),
    entity_id TEXT REFERENCES entities(id) ON DELETE RESTRICT,
    group_id TEXT REFERENCES consolidation_groups(id) ON DELETE RESTRICT,
    accounting_basis TEXT NOT NULL DEFAULT 'ACCRUAL' CHECK (accounting_basis IN ('ACCRUAL', 'CASH')),
    currency TEXT NOT NULL CHECK (length(currency) = 3 AND currency = upper(currency)),
    status TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'ARCHIVED')),
    created_at TEXT NOT NULL,
    CHECK (
        (kind = 'ACTUAL' AND entity_id IS NOT NULL AND group_id IS NULL) OR
        (kind = 'ELIMINATION' AND entity_id IS NULL AND group_id IS NOT NULL)
    )
) STRICT;

CREATE UNIQUE INDEX books_one_actual_per_entity
    ON books(entity_id) WHERE kind = 'ACTUAL' AND status = 'ACTIVE';

CREATE UNIQUE INDEX books_one_elimination_per_group
    ON books(group_id) WHERE kind = 'ELIMINATION' AND status = 'ACTIVE';

CREATE TABLE accounts (
    id TEXT PRIMARY KEY,
    code TEXT NOT NULL UNIQUE CHECK (length(code) BETWEEN 1 AND 64 AND code = upper(code)),
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    account_type TEXT NOT NULL CHECK (account_type IN ('ASSET', 'LIABILITY', 'EQUITY', 'REVENUE', 'EXPENSE')),
    subtype TEXT NOT NULL DEFAULT '',
    normal_balance TEXT NOT NULL CHECK (normal_balance IN ('DEBIT', 'CREDIT')),
    statement_section TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
) STRICT;

CREATE TABLE book_accounts (
    book_id TEXT NOT NULL REFERENCES books(id) ON DELETE RESTRICT,
    account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    posting_enabled INTEGER NOT NULL DEFAULT 1 CHECK (posting_enabled IN (0, 1)),
    active_from TEXT NOT NULL CHECK (active_from GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    active_to TEXT CHECK (active_to IS NULL OR active_to >= active_from),
    created_at TEXT NOT NULL,
    PRIMARY KEY (book_id, account_id)
) WITHOUT ROWID, STRICT;

CREATE TABLE account_identities (
    id TEXT PRIMARY KEY,
    entity_id TEXT NOT NULL REFERENCES entities(id) ON DELETE RESTRICT,
    account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    source_system TEXT NOT NULL CHECK (
        length(source_system) BETWEEN 1 AND 64
        AND source_system = trim(source_system)
        AND source_system = upper(source_system)
    ),
    external_id TEXT NOT NULL CHECK (
        length(external_id) BETWEEN 1 AND 512
        AND external_id = trim(external_id)
    ),
    account_number TEXT NOT NULL DEFAULT '' CHECK (
        length(account_number) <= 128
        AND account_number = trim(account_number)
    ),
    account_name TEXT NOT NULL CHECK (
        length(account_name) BETWEEN 1 AND 512
        AND account_name = trim(account_name)
    ),
    source_active INTEGER NOT NULL CHECK (source_active IN (0, 1)),
    evidence_source_kind TEXT NOT NULL CHECK (
        length(evidence_source_kind) BETWEEN 1 AND 64
        AND evidence_source_kind = trim(evidence_source_kind)
        AND evidence_source_kind = upper(evidence_source_kind)
    ),
    evidence_source_path TEXT NOT NULL CHECK (
        length(evidence_source_path) > 0
        AND evidence_source_path = trim(evidence_source_path)
    ),
    evidence_source_sha256 TEXT NOT NULL CHECK (
        length(evidence_source_sha256) = 64
        AND evidence_source_sha256 = lower(evidence_source_sha256)
        AND evidence_source_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    evidence_locator TEXT NOT NULL CHECK (
        length(evidence_locator) > 0
        AND evidence_locator = trim(evidence_locator)
    ),
    evidence_payload_sha256 TEXT CHECK (
        evidence_payload_sha256 IS NULL OR (
            length(evidence_payload_sha256) = 64
            AND evidence_payload_sha256 = lower(evidence_payload_sha256)
            AND evidence_payload_sha256 NOT GLOB '*[^0-9a-f]*'
        )
    ),
    created_at TEXT NOT NULL,
    UNIQUE (entity_id, source_system, external_id)
) STRICT;

CREATE INDEX account_identities_account_idx
    ON account_identities(account_id, entity_id);

CREATE TABLE fiscal_periods (
    id TEXT PRIMARY KEY,
    code TEXT NOT NULL UNIQUE CHECK (length(code) BETWEEN 4 AND 16),
    start_date TEXT NOT NULL CHECK (start_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    end_date TEXT NOT NULL CHECK (end_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND end_date >= start_date),
    fiscal_year INTEGER NOT NULL CHECK (fiscal_year BETWEEN 1900 AND 9999),
    period_number INTEGER NOT NULL CHECK (period_number BETWEEN 1 AND 53),
    is_year_end INTEGER NOT NULL DEFAULT 0 CHECK (is_year_end IN (0, 1)),
    created_at TEXT NOT NULL,
    UNIQUE (fiscal_year, period_number),
    UNIQUE (start_date, end_date)
) STRICT;

CREATE UNIQUE INDEX fiscal_periods_one_year_end
    ON fiscal_periods(fiscal_year) WHERE is_year_end = 1;

CREATE TABLE book_periods (
    book_id TEXT NOT NULL REFERENCES books(id) ON DELETE RESTRICT,
    period_id TEXT NOT NULL REFERENCES fiscal_periods(id) ON DELETE RESTRICT,
    status TEXT NOT NULL DEFAULT 'OPEN' CHECK (status IN ('OPEN', 'CLOSED')),
    closed_at TEXT,
    closed_by TEXT,
    close_digest TEXT,
    reopened_at TEXT,
    reopened_by TEXT,
    reopen_reason TEXT,
    PRIMARY KEY (book_id, period_id),
    CHECK (
		(closed_at IS NULL AND closed_by IS NULL AND close_digest IS NULL) OR
		(closed_at IS NOT NULL AND length(trim(closed_at)) > 0
		 AND closed_by IS NOT NULL AND length(trim(closed_by)) > 0
		 AND close_digest IS NOT NULL AND length(close_digest) = 64
		 AND close_digest = lower(close_digest) AND close_digest NOT GLOB '*[^0-9a-f]*')
	),
	CHECK (
		(reopened_at IS NULL AND reopened_by IS NULL AND reopen_reason IS NULL) OR
		(reopened_at IS NOT NULL AND length(trim(reopened_at)) > 0
		 AND reopened_by IS NOT NULL AND length(trim(reopened_by)) > 0
		 AND reopen_reason IS NOT NULL AND length(trim(reopen_reason)) > 0)
	),
	CHECK (
		(status = 'CLOSED' AND closed_at IS NOT NULL) OR
		(status = 'OPEN' AND (
			(closed_at IS NULL AND reopened_at IS NULL) OR
			(closed_at IS NOT NULL AND reopened_at IS NOT NULL)
		))
    )
) WITHOUT ROWID, STRICT;

CREATE TABLE journal_entries (
    id TEXT PRIMARY KEY,
    book_id TEXT NOT NULL REFERENCES books(id) ON DELETE RESTRICT,
    entry_number INTEGER NOT NULL CHECK (entry_number > 0),
    kind TEXT NOT NULL DEFAULT 'STANDARD' CHECK (kind IN ('STANDARD', 'CLOSING', 'CLOSING_REVERSAL')),
    posting_date TEXT NOT NULL CHECK (posting_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    period_id TEXT NOT NULL REFERENCES fiscal_periods(id) ON DELETE RESTRICT,
    status TEXT NOT NULL DEFAULT 'DRAFT' CHECK (status IN ('DRAFT', 'POSTED', 'ABANDONED')),
    description TEXT NOT NULL CHECK (length(trim(description)) > 0),
    reference TEXT,
    source_system TEXT,
    source_key TEXT,
    source_payload_sha256 TEXT,
    tax_type TEXT,
    tax_accounting_period TEXT,
    reversal_of_id TEXT REFERENCES journal_entries(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL,
    created_by TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    posted_at TEXT,
    posted_by TEXT,
    UNIQUE (book_id, entry_number),
    CHECK ((source_system IS NULL) = (source_key IS NULL)),
    CHECK ((source_system IS NULL) = (source_payload_sha256 IS NULL)),
    CHECK (source_payload_sha256 IS NULL OR length(source_payload_sha256) = 64),
    CHECK (tax_type IS NULL OR length(trim(tax_type)) > 0),
    CHECK (tax_accounting_period IS NULL OR length(trim(tax_accounting_period)) > 0),
    CHECK ((status = 'POSTED') = (posted_at IS NOT NULL AND posted_by IS NOT NULL))
) STRICT;

CREATE UNIQUE INDEX journal_entries_source_unique
    ON journal_entries(book_id, source_system, source_key)
    WHERE source_system IS NOT NULL;

CREATE UNIQUE INDEX journal_entries_one_active_reversal
    ON journal_entries(reversal_of_id)
    WHERE reversal_of_id IS NOT NULL AND status <> 'ABANDONED';

CREATE TABLE journal_lines (
    id TEXT PRIMARY KEY,
    journal_entry_id TEXT NOT NULL REFERENCES journal_entries(id) ON DELETE RESTRICT,
    line_number INTEGER NOT NULL CHECK (line_number > 0),
    account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    description TEXT NOT NULL DEFAULT '',
    debit_cents INTEGER NOT NULL DEFAULT 0 CHECK (debit_cents >= 0),
    credit_cents INTEGER NOT NULL DEFAULT 0 CHECK (credit_cents >= 0),
    counterparty_entity_id TEXT REFERENCES entities(id) ON DELETE RESTRICT,
    intercompany_key TEXT,
    created_at TEXT NOT NULL,
    UNIQUE (journal_entry_id, line_number),
    CHECK ((debit_cents > 0 AND credit_cents = 0) OR (credit_cents > 0 AND debit_cents = 0))
) STRICT;

CREATE INDEX journal_entries_book_date_idx
    ON journal_entries(book_id, posting_date, status);

CREATE INDEX journal_lines_account_idx
    ON journal_lines(account_id, journal_entry_id);

CREATE INDEX journal_lines_counterparty_idx
    ON journal_lines(counterparty_entity_id, intercompany_key)
    WHERE counterparty_entity_id IS NOT NULL;

CREATE TABLE import_batches (
    id TEXT PRIMARY KEY,
    source_system TEXT NOT NULL CHECK (length(trim(source_system)) > 0),
    entity_id TEXT REFERENCES entities(id) ON DELETE RESTRICT,
    source_name TEXT NOT NULL CHECK (length(trim(source_name)) > 0),
    file_sha256 TEXT NOT NULL CHECK (length(file_sha256) = 64),
    status TEXT NOT NULL DEFAULT 'STAGED' CHECK (status IN ('STAGED', 'COMPLETED', 'FAILED')),
    record_count INTEGER NOT NULL DEFAULT 0 CHECK (record_count >= 0),
    created_at TEXT NOT NULL,
    completed_at TEXT,
    UNIQUE (source_system, file_sha256),
    CHECK ((status = 'COMPLETED') = (completed_at IS NOT NULL))
) STRICT;

CREATE TABLE source_identities (
    id TEXT PRIMARY KEY,
    entity_id TEXT REFERENCES entities(id) ON DELETE RESTRICT,
    book_id TEXT NOT NULL REFERENCES books(id) ON DELETE RESTRICT,
    materialization_kind TEXT NOT NULL CHECK (materialization_kind IN ('STATEMENT', 'JOURNAL')),
    statement_account_id TEXT REFERENCES statement_accounts(id) ON DELETE RESTRICT,
    source_system TEXT NOT NULL CHECK (
        length(source_system) BETWEEN 1 AND 64
        AND source_system = trim(source_system)
        AND source_system = upper(source_system)
    ),
    source_account TEXT NOT NULL CHECK (
        length(source_account) BETWEEN 1 AND 64
        AND source_account = trim(source_account)
        AND source_account = upper(source_account)
    ),
    external_id TEXT NOT NULL CHECK (
        length(external_id) BETWEEN 1 AND 512
        AND external_id = trim(external_id)
    ),
    created_at TEXT NOT NULL,
    created_by TEXT NOT NULL CHECK (length(trim(created_by)) > 0),
    UNIQUE (source_system, source_account, external_id),
    CHECK (
        (materialization_kind = 'STATEMENT' AND statement_account_id IS NOT NULL) OR
        (materialization_kind = 'JOURNAL' AND statement_account_id IS NULL)
    )
) STRICT;

CREATE INDEX source_identities_book_idx
    ON source_identities(book_id, materialization_kind, source_system, source_account);

CREATE INDEX source_identities_statement_account_idx
    ON source_identities(statement_account_id, source_system, external_id)
    WHERE materialization_kind = 'STATEMENT';

CREATE TABLE source_records (
    id TEXT PRIMARY KEY,
    source_identity_id TEXT NOT NULL REFERENCES source_identities(id) ON DELETE RESTRICT,
    import_batch_id TEXT NOT NULL REFERENCES import_batches(id) ON DELETE RESTRICT,
    revision INTEGER NOT NULL CHECK (revision > 0),
    supersedes_source_record_id TEXT UNIQUE REFERENCES source_records(id) ON DELETE RESTRICT,
    observation_kind TEXT NOT NULL DEFAULT 'PROVIDER'
        CHECK (observation_kind IN ('PROVIDER', 'RESOLUTION')),
    transaction_date TEXT NOT NULL CHECK (transaction_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    description TEXT NOT NULL CHECK (length(trim(description)) > 0),
    amount_cents INTEGER NOT NULL,
    tax_type TEXT,
    tax_accounting_period TEXT,
    disposition TEXT NOT NULL DEFAULT 'POSTED'
        CHECK (disposition IN ('POSTED', 'PENDING', 'NEEDS_REVIEW', 'SOURCE_ONLY')),
    exclusion_reason TEXT,
    payload_sha256 TEXT NOT NULL CHECK (
        length(payload_sha256) = 64
        AND payload_sha256 = lower(payload_sha256)
        AND payload_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    raw_json TEXT NOT NULL CHECK (json_valid(raw_json)),
    resolution_reason TEXT,
    resolution_evidence_json TEXT,
    resolution_evidence_sha256 TEXT,
    created_at TEXT NOT NULL,
    created_by TEXT NOT NULL CHECK (length(trim(created_by)) > 0),
    UNIQUE (source_identity_id, revision),
    CHECK (
        (revision = 1 AND supersedes_source_record_id IS NULL) OR
        (revision > 1 AND supersedes_source_record_id IS NOT NULL)
    ),
    CHECK (tax_type IS NULL OR length(trim(tax_type)) > 0),
    CHECK (tax_accounting_period IS NULL OR length(trim(tax_accounting_period)) > 0),
    CHECK (
        (disposition = 'POSTED' AND exclusion_reason IS NULL) OR
        (disposition <> 'POSTED' AND length(trim(COALESCE(exclusion_reason, ''))) > 0
         AND exclusion_reason = trim(exclusion_reason))
    ),
    CHECK (
        (observation_kind = 'PROVIDER'
         AND resolution_reason IS NULL
         AND resolution_evidence_json IS NULL
         AND resolution_evidence_sha256 IS NULL) OR
        (observation_kind = 'RESOLUTION'
         AND length(trim(COALESCE(resolution_reason, ''))) > 0
         AND resolution_reason = trim(resolution_reason)
         AND json_valid(resolution_evidence_json)
         AND length(resolution_evidence_sha256) = 64
         AND resolution_evidence_sha256 = lower(resolution_evidence_sha256)
         AND resolution_evidence_sha256 NOT GLOB '*[^0-9a-f]*')
    )
) STRICT;

CREATE INDEX source_records_identity_revision_idx
    ON source_records(source_identity_id, revision DESC);

CREATE INDEX source_records_date_idx ON source_records(transaction_date);

CREATE INDEX source_records_disposition_idx ON source_records(disposition, transaction_date);

CREATE VIEW current_source_records AS
SELECT sr.id, sr.source_identity_id, sr.import_batch_id, sr.revision,
       sr.supersedes_source_record_id, sr.observation_kind,
       si.entity_id, si.book_id, si.materialization_kind, si.statement_account_id,
       si.source_system, si.source_account, si.external_id,
       sr.transaction_date, sr.description, sr.amount_cents,
       sr.tax_type, sr.tax_accounting_period, sr.disposition, sr.exclusion_reason,
       sr.payload_sha256, sr.raw_json, sr.resolution_reason,
       sr.resolution_evidence_json, sr.resolution_evidence_sha256,
       sr.created_at, sr.created_by
FROM source_records sr
JOIN source_identities si ON si.id = sr.source_identity_id
WHERE NOT EXISTS (
    SELECT 1 FROM source_records successor
    WHERE successor.supersedes_source_record_id = sr.id
);

CREATE TABLE source_record_journals (
    source_record_id TEXT NOT NULL REFERENCES source_records(id) ON DELETE RESTRICT,
    journal_entry_id TEXT NOT NULL REFERENCES journal_entries(id) ON DELETE RESTRICT,
    link_role TEXT NOT NULL CHECK (link_role IN ('PRIMARY', 'EVIDENCE', 'MIRROR', 'ELIMINATION')),
    created_at TEXT NOT NULL,
    created_by TEXT NOT NULL CHECK (length(trim(created_by)) > 0),
    PRIMARY KEY (source_record_id, journal_entry_id)
) WITHOUT ROWID, STRICT;

CREATE INDEX source_record_journals_journal_idx
    ON source_record_journals(journal_entry_id, source_record_id);

CREATE UNIQUE INDEX source_record_primary_journal_unique
    ON source_record_journals(source_record_id)
    WHERE link_role = 'PRIMARY';

CREATE TABLE statement_accounts (
    id TEXT PRIMARY KEY,
    code TEXT NOT NULL UNIQUE CHECK (length(code) BETWEEN 2 AND 64 AND code = upper(code)),
    entity_id TEXT NOT NULL REFERENCES entities(id) ON DELETE RESTRICT,
    book_id TEXT NOT NULL REFERENCES books(id) ON DELETE RESTRICT,
    gl_account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    account_kind TEXT NOT NULL CHECK (account_kind IN ('BANK', 'CREDIT_CARD', 'LOAN', 'INVESTMENT')),
    currency TEXT NOT NULL CHECK (length(currency) = 3 AND currency = upper(currency)),
    required_for_close INTEGER NOT NULL DEFAULT 1 CHECK (required_for_close IN (0, 1)),
    reconciliation_required_from TEXT NOT NULL CHECK (reconciliation_required_from GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    reconciliation_required_through TEXT CHECK (
        reconciliation_required_through IS NULL OR
        (reconciliation_required_through GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND reconciliation_required_through >= reconciliation_required_from)
    ),
    status TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'ARCHIVED')),
    archived_at TEXT NOT NULL DEFAULT '',
    archived_by TEXT NOT NULL DEFAULT '',
    archive_reason TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    CHECK (
        (status = 'ACTIVE' AND reconciliation_required_through IS NULL
         AND archived_at = '' AND archived_by = '' AND archive_reason = '') OR
        (status = 'ARCHIVED' AND length(trim(archived_at)) > 0
         AND length(trim(archived_by)) > 0 AND length(trim(archive_reason)) > 0
         AND reconciliation_required_through IS NOT NULL)
    ),
    UNIQUE (book_id, gl_account_id)
) STRICT;

CREATE TABLE statement_account_identities (
    id TEXT PRIMARY KEY,
    statement_account_id TEXT NOT NULL REFERENCES statement_accounts(id) ON DELETE RESTRICT,
    source_system TEXT NOT NULL CHECK (
        length(source_system) BETWEEN 1 AND 64
        AND source_system = trim(source_system)
        AND source_system = upper(source_system)
    ),
    source_realm TEXT NOT NULL CHECK (
        length(source_realm) BETWEEN 1 AND 128
        AND source_realm = trim(source_realm)
        AND source_realm = upper(source_realm)
    ),
    external_id TEXT NOT NULL CHECK (
        length(external_id) BETWEEN 1 AND 512
        AND external_id = trim(external_id)
    ),
    account_number TEXT NOT NULL DEFAULT '' CHECK (
        length(account_number) <= 128
        AND account_number = trim(account_number)
    ),
    account_name TEXT NOT NULL CHECK (
        length(account_name) BETWEEN 1 AND 512
        AND account_name = trim(account_name)
    ),
    source_active INTEGER NOT NULL CHECK (source_active IN (0, 1)),
    evidence_source_kind TEXT NOT NULL CHECK (
        length(evidence_source_kind) BETWEEN 1 AND 64
        AND evidence_source_kind = trim(evidence_source_kind)
        AND evidence_source_kind = upper(evidence_source_kind)
    ),
    evidence_source_path TEXT NOT NULL CHECK (
        length(evidence_source_path) > 0
        AND evidence_source_path = trim(evidence_source_path)
    ),
    evidence_source_sha256 TEXT NOT NULL CHECK (
        length(evidence_source_sha256) = 64
        AND evidence_source_sha256 = lower(evidence_source_sha256)
        AND evidence_source_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    evidence_locator TEXT NOT NULL CHECK (
        length(evidence_locator) > 0
        AND evidence_locator = trim(evidence_locator)
    ),
    evidence_payload_sha256 TEXT CHECK (
        evidence_payload_sha256 IS NULL OR (
            length(evidence_payload_sha256) = 64
            AND evidence_payload_sha256 = lower(evidence_payload_sha256)
            AND evidence_payload_sha256 NOT GLOB '*[^0-9a-f]*'
        )
    ),
    created_at TEXT NOT NULL,
    created_by TEXT NOT NULL CHECK (length(trim(created_by)) > 0),
    UNIQUE (source_system, source_realm, external_id)
) STRICT;

CREATE INDEX statement_account_identities_account_idx
    ON statement_account_identities(statement_account_id, source_system, source_realm, external_id);

CREATE TABLE statement_transactions (
    id TEXT PRIMARY KEY,
    statement_account_id TEXT NOT NULL REFERENCES statement_accounts(id) ON DELETE RESTRICT,
    source_identity_id TEXT NOT NULL UNIQUE REFERENCES source_identities(id) ON DELETE RESTRICT,
    source_record_id TEXT NOT NULL UNIQUE REFERENCES source_records(id) ON DELETE RESTRICT,
    posted_date TEXT NOT NULL CHECK (posted_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    description TEXT NOT NULL CHECK (length(trim(description)) > 0),
    amount_cents INTEGER NOT NULL,
    created_at TEXT NOT NULL
) STRICT;

CREATE INDEX statement_transactions_date_idx
    ON statement_transactions(statement_account_id, posted_date);

CREATE TABLE reconciliations (
    id TEXT PRIMARY KEY,
    statement_account_id TEXT NOT NULL REFERENCES statement_accounts(id) ON DELETE RESTRICT,
    start_date TEXT NOT NULL CHECK (start_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    end_date TEXT NOT NULL CHECK (end_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND end_date >= start_date),
    beginning_balance_cents INTEGER NOT NULL,
    ending_balance_cents INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'OPEN' CHECK (status IN ('OPEN', 'COMPLETED', 'ABANDONED')),
    created_at TEXT NOT NULL,
    created_by TEXT NOT NULL,
    completed_at TEXT,
    completed_by TEXT,
    reopened_at TEXT,
    reopened_by TEXT,
    reopen_reason TEXT,
    abandoned_at TEXT,
    abandoned_by TEXT,
    abandon_reason TEXT,
    CHECK (
        (status = 'COMPLETED'
         AND completed_at IS NOT NULL AND length(trim(completed_at)) > 0
         AND completed_by IS NOT NULL AND length(trim(completed_by)) > 0)
        OR
        (status <> 'COMPLETED' AND completed_at IS NULL AND completed_by IS NULL)
    ),
    CHECK (
        (status = 'ABANDONED'
         AND abandoned_at IS NOT NULL AND length(trim(abandoned_at)) > 0
         AND abandoned_by IS NOT NULL AND length(trim(abandoned_by)) > 0
         AND abandon_reason IS NOT NULL AND length(trim(abandon_reason)) > 0)
        OR
        (status <> 'ABANDONED'
         AND abandoned_at IS NULL AND abandoned_by IS NULL AND abandon_reason IS NULL)
    )
) STRICT;

CREATE UNIQUE INDEX reconciliations_current_end_unique
    ON reconciliations(statement_account_id, end_date)
    WHERE status <> 'ABANDONED';

CREATE TABLE reconciliation_allocations (
    id TEXT PRIMARY KEY,
    reconciliation_id TEXT NOT NULL REFERENCES reconciliations(id) ON DELETE RESTRICT,
    statement_transaction_id TEXT NOT NULL REFERENCES statement_transactions(id) ON DELETE RESTRICT,
    journal_line_id TEXT NOT NULL REFERENCES journal_lines(id) ON DELETE RESTRICT,
    allocated_amount_cents INTEGER NOT NULL CHECK (allocated_amount_cents <> 0),
    created_at TEXT NOT NULL,
    created_by TEXT NOT NULL,
    UNIQUE (reconciliation_id, statement_transaction_id, journal_line_id)
) STRICT;

CREATE INDEX reconciliation_allocations_statement_idx
    ON reconciliation_allocations(statement_transaction_id);

CREATE INDEX reconciliation_allocations_line_idx
    ON reconciliation_allocations(journal_line_id);

CREATE TABLE audit_events (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id TEXT NOT NULL UNIQUE,
    occurred_at TEXT NOT NULL,
    actor TEXT NOT NULL,
    app_version TEXT NOT NULL,
    command TEXT NOT NULL,
    aggregate_type TEXT NOT NULL,
    aggregate_id TEXT NOT NULL,
    payload_json TEXT NOT NULL CHECK (json_valid(payload_json)),
    previous_hash TEXT NOT NULL,
    event_hash TEXT NOT NULL UNIQUE CHECK (length(event_hash) = 64)
) STRICT;

CREATE TABLE backup_records (
    id TEXT PRIMARY KEY,
    created_at TEXT NOT NULL,
    created_by TEXT NOT NULL,
    destination TEXT NOT NULL,
    file_sha256 TEXT NOT NULL CHECK (length(file_sha256) = 64),
    integrity_status TEXT NOT NULL CHECK (integrity_status = 'OK')
) STRICT;

CREATE TRIGGER entities_no_delete
BEFORE DELETE ON entities BEGIN
    SELECT RAISE(ABORT, 'entities cannot be deleted; archive them');
END;
CREATE TRIGGER ownership_no_overlap_insert
BEFORE INSERT ON ownership_interests BEGIN
    SELECT CASE WHEN EXISTS (
        SELECT 1 FROM ownership_interests oi
        WHERE oi.child_entity_id = NEW.child_entity_id
          AND COALESCE(oi.effective_to, '9999-12-31') >= NEW.effective_from
          AND COALESCE(NEW.effective_to, '9999-12-31') >= oi.effective_from
    ) THEN RAISE(ABORT, 'child ownership periods overlap') END;
    SELECT CASE WHEN EXISTS (
        WITH RECURSIVE descendants(id) AS (
            SELECT child_entity_id FROM ownership_interests WHERE parent_entity_id = NEW.child_entity_id
            UNION
            SELECT oi.child_entity_id FROM ownership_interests oi JOIN descendants d ON oi.parent_entity_id = d.id
        )
        SELECT 1 FROM descendants WHERE id = NEW.parent_entity_id
    ) THEN RAISE(ABORT, 'ownership relationship would create a cycle') END;
END;

CREATE TRIGGER ownership_no_overlap_update
BEFORE UPDATE ON ownership_interests BEGIN
    SELECT CASE WHEN EXISTS (
        SELECT 1 FROM ownership_interests oi
        WHERE oi.id <> OLD.id
          AND oi.child_entity_id = NEW.child_entity_id
          AND COALESCE(oi.effective_to, '9999-12-31') >= NEW.effective_from
          AND COALESCE(NEW.effective_to, '9999-12-31') >= oi.effective_from
    ) THEN RAISE(ABORT, 'child ownership periods overlap') END;
    SELECT CASE WHEN EXISTS (
        WITH RECURSIVE descendants(id) AS (
            SELECT child_entity_id FROM ownership_interests
            WHERE parent_entity_id = NEW.child_entity_id AND id <> OLD.id
            UNION
            SELECT oi.child_entity_id FROM ownership_interests oi
            JOIN descendants d ON oi.parent_entity_id = d.id WHERE oi.id <> OLD.id
        )
        SELECT 1 FROM descendants WHERE id = NEW.parent_entity_id
    ) THEN RAISE(ABORT, 'ownership relationship would create a cycle') END;
END;

CREATE TRIGGER books_validate_insert
BEFORE INSERT ON books BEGIN
    SELECT CASE
        WHEN NEW.kind = 'ACTUAL' AND NEW.currency <> (SELECT functional_currency FROM entities WHERE id = NEW.entity_id)
            THEN RAISE(ABORT, 'actual book currency differs from entity currency')
        WHEN NEW.kind = 'ELIMINATION' AND NEW.currency <> (SELECT currency FROM consolidation_groups WHERE id = NEW.group_id)
            THEN RAISE(ABORT, 'elimination book currency differs from group currency')
    END;
END;

CREATE TRIGGER books_protect_structure
BEFORE UPDATE OF kind, entity_id, group_id, currency ON books
WHEN EXISTS (SELECT 1 FROM journal_entries WHERE book_id = OLD.id)
BEGIN
    SELECT RAISE(ABORT, 'book structure cannot change after journals exist');
END;

CREATE TRIGGER accounts_protect_reporting_classification
BEFORE UPDATE OF account_type, subtype, normal_balance, statement_section ON accounts
WHEN EXISTS (
    SELECT 1 FROM journal_lines jl
    JOIN journal_entries je ON je.id = jl.journal_entry_id
    WHERE jl.account_id = OLD.id AND je.status = 'POSTED'
)
OR EXISTS (SELECT 1 FROM statement_accounts sa WHERE sa.gl_account_id = OLD.id)
BEGIN
    SELECT RAISE(ABORT, 'account classification cannot change after posting or statement-account assignment');
END;

CREATE TRIGGER book_accounts_preserve_posted_coverage
BEFORE UPDATE OF active_from, active_to ON book_accounts
WHEN EXISTS (
    SELECT 1
    FROM journal_entries je
    JOIN journal_lines jl ON jl.journal_entry_id = je.id
    WHERE je.book_id = OLD.book_id
      AND jl.account_id = OLD.account_id
      AND je.status = 'POSTED'
      AND (
          je.posting_date < NEW.active_from OR
          (NEW.active_to IS NOT NULL AND je.posting_date > NEW.active_to)
      )
)
BEGIN
    SELECT RAISE(ABORT, 'account activation cannot exclude existing posted lines');
END;

CREATE TRIGGER book_accounts_no_delete
BEFORE DELETE ON book_accounts BEGIN
    SELECT RAISE(ABORT, 'book account activations cannot be deleted');
END;

CREATE TRIGGER account_identities_validate_insert
BEFORE INSERT ON account_identities BEGIN
    SELECT CASE WHEN NOT EXISTS (
        SELECT 1
        FROM books b
        JOIN book_accounts ba ON ba.book_id = b.id AND ba.account_id = NEW.account_id
        WHERE b.entity_id = NEW.entity_id
          AND b.kind = 'ACTUAL'
          AND b.status = 'ACTIVE'
    ) THEN RAISE(ABORT, 'external account identity must map to an account enabled in the entity actual book') END;
END;

CREATE TRIGGER account_identities_immutable_update
BEFORE UPDATE ON account_identities BEGIN
    SELECT RAISE(ABORT, 'external account identities are immutable');
END;

CREATE TRIGGER account_identities_immutable_delete
BEFORE DELETE ON account_identities BEGIN
    SELECT RAISE(ABORT, 'external account identities are immutable');
END;

CREATE TRIGGER fiscal_periods_immutable_update
BEFORE UPDATE ON fiscal_periods
BEGIN
    SELECT RAISE(ABORT, 'fiscal periods are immutable');
END;

CREATE TRIGGER fiscal_periods_no_overlap_insert
BEFORE INSERT ON fiscal_periods BEGIN
    SELECT CASE WHEN EXISTS (
        SELECT 1 FROM fiscal_periods fp
        WHERE fp.end_date >= NEW.start_date AND NEW.end_date >= fp.start_date
    ) THEN RAISE(ABORT, 'fiscal periods overlap') END;
END;

CREATE TRIGGER fiscal_periods_no_overlap_update
BEFORE UPDATE ON fiscal_periods BEGIN
    SELECT CASE WHEN EXISTS (
        SELECT 1 FROM fiscal_periods fp
        WHERE fp.id <> OLD.id
          AND fp.end_date >= NEW.start_date AND NEW.end_date >= fp.start_date
    ) THEN RAISE(ABORT, 'fiscal periods overlap') END;
END;

CREATE TRIGGER fiscal_periods_no_delete
BEFORE DELETE ON fiscal_periods BEGIN
    SELECT RAISE(ABORT, 'fiscal periods cannot be deleted');
END;

CREATE TRIGGER journal_entries_insert_draft_only
BEFORE INSERT ON journal_entries
WHEN NEW.status <> 'DRAFT' OR NEW.posted_at IS NOT NULL OR NEW.posted_by IS NOT NULL
BEGIN
    SELECT RAISE(ABORT, 'journal entries must be inserted as drafts');
END;

CREATE TRIGGER journal_entries_guard_posted_update
BEFORE UPDATE ON journal_entries
WHEN OLD.status = 'POSTED'
BEGIN
    SELECT RAISE(ABORT, 'posted journal entries are immutable');
END;

CREATE TRIGGER journal_entries_guard_abandoned_update
BEFORE UPDATE ON journal_entries
WHEN OLD.status = 'ABANDONED'
BEGIN
    SELECT RAISE(ABORT, 'abandoned journal entries are immutable');
END;

CREATE TRIGGER journal_entries_guard_transition
BEFORE UPDATE OF status ON journal_entries
WHEN OLD.status = 'DRAFT' AND NEW.status NOT IN ('DRAFT', 'POSTED', 'ABANDONED')
BEGIN
    SELECT RAISE(ABORT, 'invalid journal status transition');
END;

CREATE TRIGGER journal_entries_guard_derived_draft_update
BEFORE UPDATE OF book_id, kind, posting_date, period_id, description, reference,
                 source_system, source_key, source_payload_sha256, tax_type,
                 tax_accounting_period, reversal_of_id
ON journal_entries
WHEN OLD.status = 'DRAFT'
 AND (OLD.source_system IS NOT NULL OR OLD.reversal_of_id IS NOT NULL OR
      EXISTS (SELECT 1 FROM source_record_journals srj WHERE srj.journal_entry_id = OLD.id))
BEGIN
    SELECT RAISE(ABORT, 'source-derived and reversal drafts are immutable; abandon and recreate them');
END;

CREATE TRIGGER journal_entries_validate_post
BEFORE UPDATE OF status ON journal_entries
WHEN OLD.status = 'DRAFT' AND NEW.status = 'POSTED'
BEGIN
    SELECT CASE WHEN NEW.posted_at IS NULL OR NEW.posted_by IS NULL
        THEN RAISE(ABORT, 'posted timestamp and actor are required') END;
    SELECT CASE WHEN (SELECT status FROM books WHERE id = NEW.book_id) <> 'ACTIVE'
        THEN RAISE(ABORT, 'journal book is not active') END;
    SELECT CASE WHEN NOT EXISTS (
        SELECT 1 FROM fiscal_periods fp
        JOIN book_periods bp ON bp.period_id = fp.id AND bp.book_id = NEW.book_id
        WHERE fp.id = NEW.period_id
          AND NEW.posting_date BETWEEN fp.start_date AND fp.end_date
          AND bp.status = 'OPEN'
    ) THEN RAISE(ABORT, 'posting date is outside an open book period') END;
    SELECT CASE WHEN EXISTS (
        SELECT 1 FROM book_periods later
        JOIN fiscal_periods later_period ON later_period.id = later.period_id
        WHERE later.book_id = NEW.book_id AND later.status = 'CLOSED'
          AND later_period.id <> NEW.period_id AND NEW.posting_date <= later_period.end_date
    ) THEN RAISE(ABORT, 'a later closed period prevents this posting') END;
    SELECT CASE WHEN EXISTS (
        SELECT 1
        FROM journal_lines jl
        JOIN statement_accounts sa
          ON sa.book_id = NEW.book_id
         AND sa.gl_account_id = jl.account_id
        JOIN reconciliations r
          ON r.statement_account_id = sa.id
         AND r.status = 'COMPLETED'
         AND NEW.posting_date <= r.end_date
        WHERE jl.journal_entry_id = NEW.id
    ) THEN RAISE(ABORT, 'control-account posting would invalidate a completed reconciliation; reopen it first') END;
    SELECT CASE WHEN (SELECT COUNT(*) FROM journal_lines WHERE journal_entry_id = NEW.id) < 2
        THEN RAISE(ABORT, 'a posted journal requires at least two lines') END;
    SELECT CASE WHEN (SELECT COALESCE(SUM(debit_cents), 0) FROM journal_lines WHERE journal_entry_id = NEW.id) = 0
        THEN RAISE(ABORT, 'a posted journal must have a nonzero amount') END;
    SELECT CASE WHEN
        (SELECT COALESCE(SUM(debit_cents), 0) FROM journal_lines WHERE journal_entry_id = NEW.id) <>
        (SELECT COALESCE(SUM(credit_cents), 0) FROM journal_lines WHERE journal_entry_id = NEW.id)
        THEN RAISE(ABORT, 'journal debits and credits do not balance') END;
    SELECT CASE WHEN EXISTS (
        SELECT 1 FROM journal_lines jl
        LEFT JOIN book_accounts ba ON ba.book_id = NEW.book_id AND ba.account_id = jl.account_id
        WHERE jl.journal_entry_id = NEW.id
          AND (
              ba.account_id IS NULL OR ba.posting_enabled <> 1 OR
              NEW.posting_date < ba.active_from OR
              (ba.active_to IS NOT NULL AND NEW.posting_date > ba.active_to)
          )
    ) THEN RAISE(ABORT, 'journal contains an inactive or foreign book account') END;
    SELECT CASE WHEN NEW.kind = 'CLOSING' AND NOT EXISTS (
        SELECT 1 FROM fiscal_periods fp
        WHERE fp.id = NEW.period_id
          AND fp.end_date = NEW.posting_date
          AND fp.is_year_end = 1
    ) THEN RAISE(ABORT, 'closing journal must post on the fiscal year final date') END;
    SELECT CASE WHEN NEW.kind = 'CLOSING' AND EXISTS (
        SELECT 1
        FROM fiscal_periods current_period
        JOIN fiscal_periods earlier_period
          ON earlier_period.fiscal_year = current_period.fiscal_year
         AND earlier_period.end_date < current_period.start_date
        JOIN book_periods earlier_book_period
          ON earlier_book_period.period_id = earlier_period.id
         AND earlier_book_period.book_id = NEW.book_id
        WHERE current_period.id = NEW.period_id
          AND earlier_book_period.status <> 'CLOSED'
    ) THEN RAISE(ABORT, 'all earlier fiscal-year periods must be closed before posting a closing journal') END;
    SELECT CASE WHEN NEW.kind = 'CLOSING' AND EXISTS (
        SELECT 1
        FROM fiscal_periods current_period
        JOIN fiscal_periods prior_close_period
          ON prior_close_period.fiscal_year = current_period.fiscal_year
        JOIN journal_entries prior_close
          ON prior_close.period_id = prior_close_period.id
         AND prior_close.book_id = NEW.book_id
         AND prior_close.kind = 'CLOSING'
         AND prior_close.status = 'POSTED'
        WHERE current_period.id = NEW.period_id
          AND NOT EXISTS (
              SELECT 1 FROM journal_entries close_reversal
              WHERE close_reversal.reversal_of_id = prior_close.id
                AND close_reversal.kind = 'CLOSING_REVERSAL'
                AND close_reversal.status = 'POSTED'
          )
    ) THEN RAISE(ABORT, 'fiscal year already has an active closing journal') END;
    SELECT CASE WHEN NEW.kind = 'CLOSING' AND (
        (SELECT COUNT(*) FROM journal_lines jl JOIN accounts a ON a.id = jl.account_id
         WHERE jl.journal_entry_id = NEW.id AND a.account_type IN ('REVENUE', 'EXPENSE')) = 0 OR
        (SELECT COUNT(*) FROM journal_lines jl JOIN accounts a ON a.id = jl.account_id
         WHERE jl.journal_entry_id = NEW.id AND a.account_type = 'EQUITY') <>
            CASE WHEN (
                SELECT COALESCE(SUM(jl.debit_cents - jl.credit_cents), 0)
                FROM journal_lines jl JOIN accounts a ON a.id = jl.account_id
                WHERE jl.journal_entry_id = NEW.id AND a.account_type IN ('REVENUE', 'EXPENSE')
            ) = 0 THEN 0 ELSE 1 END OR
        EXISTS (
            SELECT 1 FROM journal_lines jl JOIN accounts a ON a.id = jl.account_id
            WHERE jl.journal_entry_id = NEW.id
              AND a.account_type NOT IN ('REVENUE', 'EXPENSE', 'EQUITY')
        )
    ) THEN RAISE(ABORT, 'closing journal has invalid accounts or equity-line count') END;
    SELECT CASE WHEN NEW.kind = 'CLOSING' AND EXISTS (
        WITH year_bounds AS (
            SELECT MIN(year_period.start_date) AS start_date,
                   MAX(year_period.end_date) AS end_date
            FROM fiscal_periods current_period
            JOIN fiscal_periods year_period
              ON year_period.fiscal_year = current_period.fiscal_year
            WHERE current_period.id = NEW.period_id
        ), operating AS (
            SELECT jl.account_id,
                   SUM(jl.debit_cents - jl.credit_cents) AS balance_cents
            FROM journal_entries je
            JOIN journal_lines jl ON jl.journal_entry_id = je.id
            JOIN year_bounds bounds
            WHERE je.book_id = NEW.book_id
              AND je.status = 'POSTED'
              AND je.kind = 'STANDARD'
              AND je.posting_date BETWEEN bounds.start_date AND bounds.end_date
            GROUP BY jl.account_id
        ), closing_lines AS (
            SELECT account_id, COUNT(*) AS line_count,
                   SUM(debit_cents - credit_cents) AS change_cents
            FROM journal_lines
            WHERE journal_entry_id = NEW.id
            GROUP BY account_id
        )
        SELECT 1
        FROM accounts a
        JOIN book_accounts ba
          ON ba.account_id = a.id AND ba.book_id = NEW.book_id
        LEFT JOIN operating o ON o.account_id = a.id
        LEFT JOIN closing_lines c ON c.account_id = a.id
        WHERE a.account_type IN ('REVENUE', 'EXPENSE')
          AND (
              COALESCE(o.balance_cents, 0) + COALESCE(c.change_cents, 0) <> 0 OR
              (COALESCE(o.balance_cents, 0) <> 0 AND COALESCE(c.line_count, 0) <> 1) OR
              (COALESCE(o.balance_cents, 0) = 0 AND COALESCE(c.line_count, 0) <> 0)
          )
    ) THEN RAISE(ABORT, 'closing journal must exactly zero every fiscal-year profit-and-loss balance') END;
    SELECT CASE WHEN NEW.kind = 'STANDARD' AND EXISTS (
        SELECT 1
        FROM fiscal_periods current_period
        JOIN fiscal_periods close_period
          ON close_period.fiscal_year = current_period.fiscal_year
        JOIN journal_entries close_entry
          ON close_entry.period_id = close_period.id
         AND close_entry.book_id = NEW.book_id
         AND close_entry.kind = 'CLOSING'
         AND close_entry.status = 'POSTED'
        WHERE current_period.id = NEW.period_id
          AND NOT EXISTS (
              SELECT 1 FROM journal_entries close_reversal
              WHERE close_reversal.reversal_of_id = close_entry.id
                AND close_reversal.kind = 'CLOSING_REVERSAL'
                AND close_reversal.status = 'POSTED'
          )
    ) THEN RAISE(ABORT, 'fiscal year is closed; its closing journal must be reopened before standard posting') END;
    SELECT CASE WHEN NEW.reversal_of_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM journal_entries original
        WHERE original.id = NEW.reversal_of_id
          AND original.book_id = NEW.book_id
          AND original.status = 'POSTED'
          AND NEW.posting_date >= original.posting_date
          AND NEW.tax_type IS original.tax_type
          AND NEW.tax_accounting_period IS original.tax_accounting_period
          AND (
              (NEW.kind = 'STANDARD' AND original.kind = 'STANDARD') OR
              (NEW.kind = 'CLOSING_REVERSAL' AND original.kind = 'CLOSING'
               AND NEW.period_id = original.period_id
               AND NEW.posting_date = original.posting_date)
          )
    ) THEN RAISE(ABORT, 'reversal must reference a posted journal in the same book without predating it and with matching tax metadata') END;
    SELECT CASE WHEN NEW.kind = 'CLOSING_REVERSAL' AND NEW.reversal_of_id IS NULL
        THEN RAISE(ABORT, 'closing reversal must reference a closing journal') END;
    SELECT CASE WHEN NEW.reversal_of_id IS NOT NULL AND (
        (SELECT COUNT(*) FROM journal_lines WHERE journal_entry_id = NEW.id) <>
        (SELECT COUNT(*) FROM journal_lines WHERE journal_entry_id = NEW.reversal_of_id) OR
        EXISTS (
            SELECT 1
            FROM journal_lines reversal_line
            LEFT JOIN journal_lines original_line
              ON original_line.journal_entry_id = NEW.reversal_of_id
             AND original_line.line_number = reversal_line.line_number
             AND original_line.account_id = reversal_line.account_id
             AND original_line.debit_cents = reversal_line.credit_cents
             AND original_line.credit_cents = reversal_line.debit_cents
             AND original_line.description = reversal_line.description
             AND original_line.counterparty_entity_id IS reversal_line.counterparty_entity_id
             AND original_line.intercompany_key IS reversal_line.intercompany_key
            WHERE reversal_line.journal_entry_id = NEW.id
              AND original_line.id IS NULL
        )
    ) THEN RAISE(ABORT, 'reversal lines must exactly negate the original journal') END;
END;

CREATE TRIGGER journal_entries_no_delete
BEFORE DELETE ON journal_entries BEGIN
    SELECT RAISE(ABORT, 'journal entries cannot be deleted; abandon drafts or reverse postings');
END;

CREATE TRIGGER journal_lines_validate_insert
BEFORE INSERT ON journal_lines BEGIN
    SELECT CASE WHEN (SELECT status FROM journal_entries WHERE id = NEW.journal_entry_id) <> 'DRAFT'
        THEN RAISE(ABORT, 'lines may only be added to draft journals') END;
    SELECT CASE WHEN NOT EXISTS (
        SELECT 1
        FROM journal_entries je
        JOIN book_accounts ba ON ba.book_id = je.book_id AND ba.account_id = NEW.account_id
        WHERE je.id = NEW.journal_entry_id
    ) THEN RAISE(ABORT, 'line account is not enabled for the journal book') END;
    SELECT CASE WHEN EXISTS (
        SELECT 1 FROM journal_entries je
        WHERE je.id = NEW.journal_entry_id
          AND (je.source_system IS NOT NULL OR je.reversal_of_id IS NOT NULL OR
               EXISTS (SELECT 1 FROM source_record_journals linked WHERE linked.journal_entry_id = je.id))
          AND (
              EXISTS (SELECT 1 FROM source_record_journals srj WHERE srj.journal_entry_id = je.id) OR
              EXISTS (SELECT 1 FROM audit_events ae WHERE ae.aggregate_type = 'journal' AND ae.aggregate_id = je.id)
          )
    ) THEN RAISE(ABORT, 'source-derived and reversal draft lines are immutable') END;
END;

CREATE TRIGGER journal_lines_validate_update
BEFORE UPDATE ON journal_lines BEGIN
    SELECT CASE WHEN (SELECT status FROM journal_entries WHERE id = OLD.journal_entry_id) <> 'DRAFT'
        THEN RAISE(ABORT, 'posted journal lines are immutable') END;
    SELECT CASE WHEN (SELECT status FROM journal_entries WHERE id = NEW.journal_entry_id) <> 'DRAFT'
        THEN RAISE(ABORT, 'lines may only belong to draft journals') END;
    SELECT CASE WHEN NOT EXISTS (
        SELECT 1
        FROM journal_entries je
        JOIN book_accounts ba ON ba.book_id = je.book_id AND ba.account_id = NEW.account_id
        WHERE je.id = NEW.journal_entry_id
    ) THEN RAISE(ABORT, 'line account is not enabled for the journal book') END;
    SELECT CASE WHEN EXISTS (
        SELECT 1 FROM journal_entries je
        WHERE je.id IN (OLD.journal_entry_id, NEW.journal_entry_id)
          AND (je.source_system IS NOT NULL OR je.reversal_of_id IS NOT NULL OR
               EXISTS (SELECT 1 FROM source_record_journals linked WHERE linked.journal_entry_id = je.id))
          AND (
              EXISTS (SELECT 1 FROM source_record_journals srj WHERE srj.journal_entry_id = je.id) OR
              EXISTS (SELECT 1 FROM audit_events ae WHERE ae.aggregate_type = 'journal' AND ae.aggregate_id = je.id)
          )
    ) THEN RAISE(ABORT, 'source-derived and reversal draft lines are immutable') END;
END;

CREATE TRIGGER journal_lines_validate_delete
BEFORE DELETE ON journal_lines
BEGIN
    SELECT CASE WHEN (SELECT status FROM journal_entries WHERE id = OLD.journal_entry_id) <> 'DRAFT'
        THEN RAISE(ABORT, 'posted journal lines are immutable') END;
    SELECT CASE WHEN EXISTS (
        SELECT 1 FROM journal_entries je
        WHERE je.id = OLD.journal_entry_id
          AND (je.source_system IS NOT NULL OR je.reversal_of_id IS NOT NULL OR
               EXISTS (SELECT 1 FROM source_record_journals linked WHERE linked.journal_entry_id = je.id))
          AND (
              EXISTS (SELECT 1 FROM source_record_journals srj WHERE srj.journal_entry_id = je.id) OR
              EXISTS (SELECT 1 FROM audit_events ae WHERE ae.aggregate_type = 'journal' AND ae.aggregate_id = je.id)
          )
    ) THEN RAISE(ABORT, 'source-derived and reversal draft lines are immutable') END;
END;

CREATE TRIGGER source_identities_validate_insert
BEFORE INSERT ON source_identities BEGIN
    SELECT CASE WHEN NOT EXISTS (
        SELECT 1
        FROM books b
        WHERE b.id = NEW.book_id
          AND (b.entity_id = NEW.entity_id OR (b.entity_id IS NULL AND NEW.entity_id IS NULL))
          AND (
              (NEW.materialization_kind = 'JOURNAL' AND b.code = NEW.source_account) OR
              (NEW.materialization_kind = 'STATEMENT' AND EXISTS (
                  SELECT 1 FROM statement_accounts sa
                  WHERE sa.id = NEW.statement_account_id
                    AND sa.code = NEW.source_account
                    AND sa.book_id = NEW.book_id
                    AND sa.entity_id = NEW.entity_id
              ))
          )
    ) THEN RAISE(ABORT, 'source identity must match its book and materialization account') END;
END;

CREATE TRIGGER source_identities_immutable_update
BEFORE UPDATE ON source_identities BEGIN
    SELECT RAISE(ABORT, 'source identities are immutable');
END;

CREATE TRIGGER source_identities_immutable_delete
BEFORE DELETE ON source_identities BEGIN
    SELECT RAISE(ABORT, 'source identities are immutable');
END;

CREATE TRIGGER source_records_validate_insert
BEFORE INSERT ON source_records BEGIN
    SELECT CASE WHEN NOT EXISTS (
        SELECT 1
        FROM source_identities si
        JOIN import_batches ib ON ib.id = NEW.import_batch_id
        WHERE si.id = NEW.source_identity_id
          AND ib.source_system = si.source_system
          AND (ib.entity_id IS NULL OR ib.entity_id = si.entity_id)
    ) THEN RAISE(ABORT, 'source observation must match its identity and import batch') END;
    SELECT CASE WHEN NEW.revision = 1 AND NEW.observation_kind <> 'PROVIDER'
        THEN RAISE(ABORT, 'initial source observation must come from the provider') END;
    SELECT CASE WHEN NEW.revision = 1 AND EXISTS (
        SELECT 1 FROM source_records existing
        WHERE existing.source_identity_id = NEW.source_identity_id
    ) THEN RAISE(ABORT, 'source identity already has an initial observation') END;
    SELECT CASE WHEN NEW.revision > 1 AND NOT EXISTS (
        SELECT 1
        FROM source_records predecessor
        WHERE predecessor.id = NEW.supersedes_source_record_id
          AND predecessor.source_identity_id = NEW.source_identity_id
          AND predecessor.revision + 1 = NEW.revision
          AND NOT EXISTS (
              SELECT 1 FROM source_records successor
              WHERE successor.supersedes_source_record_id = predecessor.id
          )
    ) THEN RAISE(ABORT, 'source observation must supersede the current prior revision') END;
    SELECT CASE WHEN NEW.revision > 1 AND (
        SELECT disposition FROM source_records
        WHERE id = NEW.supersedes_source_record_id
    ) = 'POSTED' THEN RAISE(ABORT, 'posted source observation is terminal') END;
    SELECT CASE WHEN NEW.revision > 1 AND NEW.observation_kind = 'RESOLUTION' AND NEW.disposition = (
        SELECT disposition FROM source_records
        WHERE id = NEW.supersedes_source_record_id
    ) THEN RAISE(ABORT, 'source resolution must change disposition') END;
    SELECT CASE WHEN NEW.revision > 1 AND NEW.observation_kind = 'PROVIDER' AND NOT (
        ((SELECT disposition FROM source_records WHERE id = NEW.supersedes_source_record_id) = 'PENDING'
         AND NEW.disposition IN ('PENDING', 'NEEDS_REVIEW', 'POSTED')) OR
        ((SELECT disposition FROM source_records WHERE id = NEW.supersedes_source_record_id) = 'NEEDS_REVIEW'
         AND NEW.disposition = 'NEEDS_REVIEW') OR
        ((SELECT disposition FROM source_records WHERE id = NEW.supersedes_source_record_id) = 'SOURCE_ONLY'
         AND NEW.disposition = 'SOURCE_ONLY')
    ) THEN RAISE(ABORT, 'source disposition transition requires explicit resolution evidence') END;
    SELECT CASE WHEN NEW.disposition = 'POSTED' AND EXISTS (
        SELECT 1
        FROM source_identities si
        JOIN statement_accounts sa ON sa.id = si.statement_account_id
        WHERE si.id = NEW.source_identity_id
          AND si.materialization_kind = 'STATEMENT'
          AND sa.status <> 'ACTIVE'
    ) THEN RAISE(ABORT, 'posted source observation requires an active statement account') END;
END;

CREATE TRIGGER source_records_immutable_update
BEFORE UPDATE ON source_records BEGIN
    SELECT RAISE(ABORT, 'source records are immutable');
END;

CREATE TRIGGER source_records_immutable_delete
BEFORE DELETE ON source_records BEGIN
    SELECT RAISE(ABORT, 'source records are immutable');
END;

CREATE TRIGGER source_record_journals_validate_insert
BEFORE INSERT ON source_record_journals BEGIN
    SELECT CASE WHEN NOT EXISTS (
        SELECT 1
        FROM source_records sr
        JOIN source_identities si ON si.id = sr.source_identity_id
        JOIN journal_entries je ON je.id = NEW.journal_entry_id
        JOIN books b ON b.id = je.book_id
        WHERE sr.id = NEW.source_record_id
          AND sr.disposition = 'POSTED'
          AND NOT EXISTS (
              SELECT 1 FROM source_records successor
              WHERE successor.supersedes_source_record_id = sr.id
          )
          AND (
              si.entity_id IS NULL OR b.entity_id IS NULL OR si.entity_id = b.entity_id OR
              b.kind = 'ELIMINATION' OR NEW.link_role IN ('MIRROR', 'ELIMINATION')
          )
          AND (
              NEW.link_role <> 'PRIMARY' OR
              (si.materialization_kind = 'JOURNAL'
               AND je.book_id = si.book_id
               AND je.source_system = si.source_system
               AND je.source_key = si.external_id)
          )
          AND (NEW.link_role <> 'EVIDENCE' OR je.book_id = si.book_id)
    ) THEN RAISE(ABORT, 'source-journal link violates current disposition, identity, book, or entity-role constraints') END;
END;

CREATE TRIGGER source_record_journals_immutable_update
BEFORE UPDATE ON source_record_journals BEGIN
    SELECT RAISE(ABORT, 'source-journal links are immutable');
END;

CREATE TRIGGER source_record_journals_immutable_delete
BEFORE DELETE ON source_record_journals BEGIN
    SELECT RAISE(ABORT, 'source-journal links are immutable');
END;

CREATE TRIGGER import_batches_completed_immutable
BEFORE UPDATE ON import_batches
WHEN OLD.status = 'COMPLETED'
BEGIN
    SELECT RAISE(ABORT, 'completed import batches are immutable');
END;

CREATE TRIGGER import_batches_no_delete
BEFORE DELETE ON import_batches BEGIN
    SELECT RAISE(ABORT, 'import batches cannot be deleted');
END;

CREATE TRIGGER statement_accounts_validate_insert
BEFORE INSERT ON statement_accounts BEGIN
    SELECT CASE WHEN NEW.status <> 'ACTIVE'
        THEN RAISE(ABORT, 'new statement accounts must be active') END;
    SELECT CASE WHEN NOT EXISTS (
        SELECT 1 FROM books b
        JOIN entities e ON e.id = b.entity_id
        JOIN book_accounts ba ON ba.book_id = b.id AND ba.account_id = NEW.gl_account_id
        JOIN accounts a ON a.id = ba.account_id
        WHERE b.id = NEW.book_id
          AND b.entity_id = NEW.entity_id
          AND b.kind = 'ACTUAL'
          AND NEW.currency = b.currency
          AND NEW.currency = e.functional_currency
          AND ((NEW.account_kind IN ('BANK', 'INVESTMENT') AND a.account_type = 'ASSET') OR
               (NEW.account_kind IN ('CREDIT_CARD', 'LOAN') AND a.account_type = 'LIABILITY'))
    ) THEN RAISE(ABORT, 'statement account must match actual-book currency and control-account type') END;
END;

CREATE TRIGGER statement_accounts_validate_update
BEFORE UPDATE OF entity_id, book_id, gl_account_id, account_kind, currency ON statement_accounts BEGIN
    SELECT CASE WHEN NOT EXISTS (
        SELECT 1 FROM books b
        JOIN entities e ON e.id = b.entity_id
        JOIN book_accounts ba ON ba.book_id = b.id AND ba.account_id = NEW.gl_account_id
        JOIN accounts a ON a.id = ba.account_id
        WHERE b.id = NEW.book_id
          AND b.entity_id = NEW.entity_id
          AND b.kind = 'ACTUAL'
          AND NEW.currency = b.currency
          AND NEW.currency = e.functional_currency
          AND ((NEW.account_kind IN ('BANK', 'INVESTMENT') AND a.account_type = 'ASSET') OR
               (NEW.account_kind IN ('CREDIT_CARD', 'LOAN') AND a.account_type = 'LIABILITY'))
    ) THEN RAISE(ABORT, 'statement account must match actual-book currency and control-account type') END;
END;

CREATE TRIGGER statement_accounts_protect_identity
BEFORE UPDATE OF code, entity_id, book_id, gl_account_id, name, account_kind, currency,
    required_for_close, reconciliation_required_from, created_at ON statement_accounts
BEGIN
    SELECT RAISE(ABORT, 'statement account identity and close policy are immutable');
END;

CREATE TRIGGER statement_accounts_no_delete
BEFORE DELETE ON statement_accounts BEGIN
    SELECT RAISE(ABORT, 'statement accounts cannot be deleted; archive them');
END;

CREATE TRIGGER statement_accounts_validate_archive
BEFORE UPDATE OF status, reconciliation_required_through, archived_at, archived_by, archive_reason ON statement_accounts
WHEN OLD.status = 'ARCHIVED'
BEGIN
    SELECT RAISE(ABORT, 'archived statement accounts are immutable');
END;

CREATE TRIGGER statement_accounts_validate_archive_transition
BEFORE UPDATE OF status, reconciliation_required_through, archived_at, archived_by, archive_reason ON statement_accounts
WHEN OLD.status = 'ACTIVE' AND NEW.status = 'ARCHIVED'
BEGIN
    SELECT CASE WHEN EXISTS (
        SELECT 1 FROM current_source_records source
        WHERE source.materialization_kind = 'STATEMENT'
          AND source.statement_account_id = OLD.id
          AND source.disposition = 'POSTED'
          AND (
              SELECT COUNT(*) FROM statement_transactions materialized
              WHERE materialized.source_identity_id = source.source_identity_id
                AND materialized.source_record_id = source.id
          ) <> 1
    ) THEN RAISE(ABORT, 'current posted statement source must be materialized before archive') END;
    SELECT CASE WHEN EXISTS (
        SELECT 1 FROM reconciliations
        WHERE statement_account_id = OLD.id AND status = 'OPEN'
    ) THEN RAISE(ABORT, 'open reconciliation must be completed or abandoned before statement account archive') END;
    SELECT CASE WHEN EXISTS (
        SELECT 1 FROM statement_transactions
        WHERE statement_account_id = OLD.id
          AND posted_date > NEW.reconciliation_required_through
    ) OR EXISTS (
        SELECT 1 FROM reconciliations
        WHERE statement_account_id = OLD.id
          AND status <> 'ABANDONED'
          AND end_date > NEW.reconciliation_required_through
    ) THEN RAISE(ABORT, 'statement account archive precedes later activity') END;
END;

CREATE TRIGGER statement_account_identities_immutable_update
BEFORE UPDATE ON statement_account_identities BEGIN
    SELECT RAISE(ABORT, 'statement account identities are immutable');
END;

CREATE TRIGGER statement_account_identities_immutable_delete
BEFORE DELETE ON statement_account_identities BEGIN
    SELECT RAISE(ABORT, 'statement account identities are immutable');
END;

CREATE TRIGGER statement_transactions_immutable_update
BEFORE UPDATE ON statement_transactions BEGIN
    SELECT RAISE(ABORT, 'statement transactions are immutable');
END;

CREATE TRIGGER statement_transactions_immutable_delete
BEFORE DELETE ON statement_transactions BEGIN
    SELECT RAISE(ABORT, 'statement transactions are immutable');
END;

CREATE TRIGGER statement_transactions_require_active_account
BEFORE INSERT ON statement_transactions
WHEN COALESCE((
	SELECT status FROM statement_accounts WHERE id = NEW.statement_account_id
), '') <> 'ACTIVE'
BEGIN
	SELECT RAISE(ABORT, 'new statement transactions require an active statement account');
END;

CREATE TRIGGER statement_transactions_validate_source_insert
BEFORE INSERT ON statement_transactions
BEGIN
    SELECT CASE WHEN NOT EXISTS (
        SELECT 1
        FROM source_records sr
        JOIN source_identities si ON si.id = sr.source_identity_id
        JOIN statement_accounts sa ON sa.id = NEW.statement_account_id
        WHERE sr.id = NEW.source_record_id
          AND si.id = NEW.source_identity_id
          AND si.materialization_kind = 'STATEMENT'
          AND si.statement_account_id = sa.id
          AND si.source_account = sa.code
          AND si.entity_id = sa.entity_id
          AND si.book_id = sa.book_id
          AND sr.disposition = 'POSTED'
          AND sr.transaction_date = NEW.posted_date
          AND sr.description = NEW.description
          AND sr.amount_cents = NEW.amount_cents
          AND NOT EXISTS (
              SELECT 1 FROM source_records successor
              WHERE successor.supersedes_source_record_id = sr.id
          )
    ) THEN RAISE(ABORT, 'statement transaction must exactly materialize one current POSTED source observation') END;
END;

CREATE TRIGGER statement_transactions_guard_completed_insert
BEFORE INSERT ON statement_transactions
WHEN EXISTS (
    SELECT 1
    FROM reconciliations r
    WHERE r.statement_account_id = NEW.statement_account_id
      AND r.status = 'COMPLETED'
      AND NEW.posted_date <= r.end_date
)
BEGIN
    SELECT RAISE(ABORT, 'statement transaction would invalidate a completed reconciliation; reopen it first');
END;

CREATE TRIGGER reconciliations_validate_insert
BEFORE INSERT ON reconciliations BEGIN
	SELECT CASE WHEN NEW.status <> 'OPEN'
		THEN RAISE(ABORT, 'new reconciliations must be open') END;
	SELECT CASE WHEN COALESCE((
		SELECT status FROM statement_accounts WHERE id = NEW.statement_account_id
	), '') <> 'ACTIVE' THEN RAISE(ABORT, 'new reconciliations require an active statement account') END;
    SELECT CASE WHEN NOT EXISTS (
        SELECT 1 FROM reconciliations r
        WHERE r.statement_account_id = NEW.statement_account_id
		  AND r.status <> 'ABANDONED'
    ) AND EXISTS (
        SELECT 1 FROM statement_transactions st
        WHERE st.statement_account_id = NEW.statement_account_id
          AND st.posted_date < NEW.start_date
    ) THEN RAISE(ABORT, 'first reconciliation must include the earliest imported statement activity') END;
    SELECT CASE WHEN EXISTS (
        SELECT 1 FROM reconciliations r
        WHERE r.statement_account_id = NEW.statement_account_id
		  AND r.status <> 'ABANDONED'
          AND r.end_date >= NEW.start_date AND NEW.end_date >= r.start_date
    ) THEN RAISE(ABORT, 'reconciliation periods overlap') END;
    SELECT CASE WHEN EXISTS (
        SELECT 1
        FROM reconciliations prior
        WHERE prior.statement_account_id = NEW.statement_account_id
		  AND prior.status <> 'ABANDONED'
          AND prior.end_date = (
              SELECT MAX(latest.end_date)
              FROM reconciliations latest
              WHERE latest.statement_account_id = NEW.statement_account_id
				AND latest.status <> 'ABANDONED'
          )
          AND prior.status <> 'COMPLETED'
    ) THEN RAISE(ABORT, 'the prior reconciliation must be completed first') END;
    SELECT CASE WHEN EXISTS (
        SELECT 1
        FROM reconciliations prior
        WHERE prior.statement_account_id = NEW.statement_account_id
		  AND prior.status <> 'ABANDONED'
          AND prior.end_date = (
              SELECT MAX(latest.end_date)
              FROM reconciliations latest
              WHERE latest.statement_account_id = NEW.statement_account_id
				AND latest.status <> 'ABANDONED'
          )
          AND (NEW.start_date <> date(prior.end_date, '+1 day')
               OR NEW.beginning_balance_cents <> prior.ending_balance_cents)
    ) THEN RAISE(ABORT, 'reconciliation must adjoin the prior period and carry forward its ending balance') END;
END;

CREATE TRIGGER reconciliations_guard_open_evidence
BEFORE UPDATE OF reopened_at, reopened_by, reopen_reason ON reconciliations
WHEN OLD.status = 'OPEN' AND NEW.status = 'OPEN'
BEGIN
    SELECT RAISE(ABORT, 'reconciliation reopen evidence changes only during reopen');
END;

CREATE TRIGGER reconciliation_allocations_validate_update
BEFORE UPDATE ON reconciliation_allocations BEGIN
    SELECT RAISE(ABORT, 'reconciliation allocations are immutable; remove then reallocate');
END;

CREATE TRIGGER reconciliation_allocations_validate_delete
BEFORE DELETE ON reconciliation_allocations
WHEN (SELECT status FROM reconciliations WHERE id = OLD.reconciliation_id) <> 'OPEN'
BEGIN
    SELECT RAISE(ABORT, 'non-open reconciliation allocations are immutable');
END;

CREATE TRIGGER reconciliations_validate_abandon
BEFORE UPDATE OF status ON reconciliations
WHEN OLD.status = 'OPEN' AND NEW.status = 'ABANDONED'
BEGIN
	SELECT CASE WHEN NEW.abandoned_at IS NULL OR length(trim(NEW.abandoned_at)) = 0
		OR NEW.abandoned_by IS NULL OR length(trim(NEW.abandoned_by)) = 0
		OR NEW.abandon_reason IS NULL OR length(trim(NEW.abandon_reason)) = 0
		THEN RAISE(ABORT, 'abandonment timestamp, actor, and reason are required') END;
	SELECT CASE WHEN NEW.reopened_at IS NOT OLD.reopened_at
		OR NEW.reopened_by IS NOT OLD.reopened_by
		OR NEW.reopen_reason IS NOT OLD.reopen_reason
		THEN RAISE(ABORT, 'reconciliation reopen evidence is immutable during abandonment') END;
	SELECT CASE WHEN EXISTS (
		SELECT 1 FROM reconciliations later
		WHERE later.statement_account_id = OLD.statement_account_id
		  AND later.id <> OLD.id AND later.status <> 'ABANDONED'
		  AND later.end_date > OLD.end_date
	) THEN RAISE(ABORT, 'later reconciliation work must be abandoned first') END;
END;

CREATE TRIGGER reconciliations_guard_completed
BEFORE UPDATE ON reconciliations
WHEN OLD.status = 'COMPLETED' AND NOT (
    NEW.status = 'OPEN' AND NEW.reopened_at IS NOT NULL AND NEW.reopened_by IS NOT NULL
    AND length(trim(COALESCE(NEW.reopen_reason, ''))) > 0
)
BEGIN
    SELECT RAISE(ABORT, 'completed reconciliations are immutable unless explicitly reopened');
END;

CREATE TRIGGER reconciliations_guard_abandoned
BEFORE UPDATE ON reconciliations
WHEN OLD.status = 'ABANDONED'
BEGIN
	SELECT RAISE(ABORT, 'abandoned reconciliations are immutable');
END;

CREATE TRIGGER reconciliations_validate_reopen
BEFORE UPDATE OF status ON reconciliations
WHEN OLD.status = 'COMPLETED' AND NEW.status = 'OPEN'
BEGIN
    SELECT CASE WHEN NEW.reopened_at IS NULL OR length(trim(NEW.reopened_at)) = 0
        OR NEW.reopened_by IS NULL OR length(trim(NEW.reopened_by)) = 0
        OR NEW.reopen_reason IS NULL OR length(trim(NEW.reopen_reason)) = 0
        OR NEW.reopened_at IS OLD.reopened_at
        THEN RAISE(ABORT, 'reconciliation reopen requires fresh timestamp, actor, and reason evidence') END;
    SELECT CASE WHEN EXISTS (
        SELECT 1
        FROM reconciliations later
        WHERE later.statement_account_id = OLD.statement_account_id
          AND later.start_date > OLD.end_date
          AND later.status = 'COMPLETED'
    ) THEN RAISE(ABORT, 'later completed reconciliations must be reopened first') END;
    SELECT CASE WHEN EXISTS (
        SELECT 1
        FROM statement_accounts sa
        JOIN book_periods bp ON bp.book_id = sa.book_id AND bp.status = 'CLOSED'
        JOIN fiscal_periods fp ON fp.id = bp.period_id
        WHERE sa.id = OLD.statement_account_id
          AND fp.end_date >= OLD.start_date
          AND OLD.end_date >= fp.start_date
    ) THEN RAISE(ABORT, 'overlapping closed book periods must be reopened first') END;
END;

CREATE TRIGGER book_periods_structure_immutable
BEFORE UPDATE OF book_id, period_id ON book_periods
BEGIN
	SELECT RAISE(ABORT, 'book-period identity is immutable');
END;

CREATE TRIGGER book_periods_guard_closed_update
BEFORE UPDATE ON book_periods
WHEN OLD.status = 'CLOSED' AND NEW.status = 'CLOSED'
BEGIN
	SELECT RAISE(ABORT, 'closed book periods are immutable unless explicitly reopened');
END;

CREATE TRIGGER book_periods_guard_open_metadata
BEFORE UPDATE OF closed_at, closed_by, close_digest, reopened_at, reopened_by, reopen_reason ON book_periods
WHEN OLD.status = 'OPEN' AND NEW.status = 'OPEN'
BEGIN
	SELECT RAISE(ABORT, 'book-period lifecycle evidence changes only during close or reopen');
END;

CREATE TRIGGER book_periods_validate_reopen
BEFORE UPDATE OF status ON book_periods
WHEN OLD.status = 'CLOSED' AND NEW.status = 'OPEN'
BEGIN
	SELECT CASE WHEN NEW.closed_at IS NOT OLD.closed_at OR NEW.closed_by IS NOT OLD.closed_by
		OR NEW.close_digest IS NOT OLD.close_digest
		THEN RAISE(ABORT, 'period close evidence is immutable during reopen') END;
	SELECT CASE WHEN NEW.reopened_at IS NULL OR length(trim(NEW.reopened_at)) = 0
		OR NEW.reopened_by IS NULL OR length(trim(NEW.reopened_by)) = 0
		OR NEW.reopen_reason IS NULL OR length(trim(NEW.reopen_reason)) = 0
		THEN RAISE(ABORT, 'reopen timestamp, actor, and reason are required') END;
	SELECT CASE WHEN NEW.reopened_at IS OLD.reopened_at
		THEN RAISE(ABORT, 'period reopen requires fresh lifecycle evidence') END;
	SELECT CASE WHEN EXISTS (
		SELECT 1 FROM fiscal_periods current_period
		JOIN fiscal_periods later_period ON later_period.start_date > current_period.end_date
		JOIN book_periods later ON later.period_id = later_period.id AND later.book_id = OLD.book_id
		WHERE current_period.id = OLD.period_id AND later.status = 'CLOSED'
	) THEN RAISE(ABORT, 'later closed periods must be reopened first') END;
END;

CREATE TRIGGER book_periods_no_delete
BEFORE DELETE ON book_periods
BEGIN
	SELECT RAISE(ABORT, 'book periods cannot be deleted');
END;

CREATE TRIGGER reconciliations_no_delete
BEFORE DELETE ON reconciliations BEGIN
    SELECT RAISE(ABORT, 'reconciliations cannot be deleted');
END;

CREATE TRIGGER audit_events_no_update
BEFORE UPDATE ON audit_events BEGIN
    SELECT RAISE(ABORT, 'audit events are append-only');
END;

CREATE TRIGGER audit_events_no_delete
BEFORE DELETE ON audit_events BEGIN
    SELECT RAISE(ABORT, 'audit events are append-only');
END;

CREATE TABLE statement_account_precoverage_closures (
    id TEXT PRIMARY KEY,
    statement_account_id TEXT NOT NULL UNIQUE
        REFERENCES statement_accounts(id) ON DELETE RESTRICT,
    statement_account_identity_id TEXT NOT NULL UNIQUE
        REFERENCES statement_account_identities(id) ON DELETE RESTRICT,
    closed_on TEXT NOT NULL CHECK (
        closed_on GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'
    ),
    closure_evidence_source_kind TEXT NOT NULL CHECK (
        closure_evidence_source_kind = 'PROVIDER_CLOSURE_LETTER'
    ),
    closure_evidence_source_path TEXT NOT NULL CHECK (
        length(trim(closure_evidence_source_path)) > 0
        AND closure_evidence_source_path = trim(closure_evidence_source_path)
    ),
    closure_evidence_source_sha256 TEXT NOT NULL CHECK (
        length(closure_evidence_source_sha256) = 64
        AND closure_evidence_source_sha256 = lower(closure_evidence_source_sha256)
        AND closure_evidence_source_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    closure_evidence_locator TEXT NOT NULL CHECK (
        length(trim(closure_evidence_locator)) > 0
        AND closure_evidence_locator = trim(closure_evidence_locator)
    ),
    zero_evidence_source_kind TEXT NOT NULL CHECK (
        zero_evidence_source_kind = 'PROVIDER_ACCOUNT_SNAPSHOT'
    ),
    zero_evidence_source_path TEXT NOT NULL CHECK (
        length(trim(zero_evidence_source_path)) > 0
        AND zero_evidence_source_path = trim(zero_evidence_source_path)
    ),
    zero_evidence_source_sha256 TEXT NOT NULL CHECK (
        length(zero_evidence_source_sha256) = 64
        AND zero_evidence_source_sha256 = lower(zero_evidence_source_sha256)
        AND zero_evidence_source_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    zero_evidence_locator TEXT NOT NULL CHECK (
        length(trim(zero_evidence_locator)) > 0
        AND zero_evidence_locator = trim(zero_evidence_locator)
    ),
    zero_evidence_payload_sha256 TEXT NOT NULL CHECK (
        length(zero_evidence_payload_sha256) = 64
        AND zero_evidence_payload_sha256 = lower(zero_evidence_payload_sha256)
        AND zero_evidence_payload_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    zero_observed_on TEXT NOT NULL CHECK (
        zero_observed_on GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'
        AND zero_observed_on >= closed_on
    ),
    provider_status TEXT NOT NULL CHECK (provider_status IN ('ARCHIVED', 'CLOSED')),
    current_balance_cents INTEGER NOT NULL CHECK (current_balance_cents = 0),
    available_balance_cents INTEGER NOT NULL CHECK (available_balance_cents = 0),
    account_holder TEXT NOT NULL CHECK (
        length(trim(account_holder)) BETWEEN 1 AND 512
        AND account_holder = trim(account_holder)
    ),
    account_suffix TEXT NOT NULL CHECK (
        length(trim(account_suffix)) BETWEEN 1 AND 32
        AND account_suffix = trim(account_suffix)
    ),
    reason TEXT NOT NULL CHECK (
        length(trim(reason)) BETWEEN 1 AND 1024
        AND reason = trim(reason)
    ),
    input_source_path TEXT NOT NULL CHECK (
        length(trim(input_source_path)) > 0
        AND input_source_path = trim(input_source_path)
    ),
    input_source_sha256 TEXT NOT NULL CHECK (
        length(input_source_sha256) = 64
        AND input_source_sha256 = lower(input_source_sha256)
        AND input_source_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    created_at TEXT NOT NULL,
    created_by TEXT NOT NULL CHECK (length(trim(created_by)) > 0)
) STRICT;

CREATE TRIGGER statement_account_precoverage_closures_validate_insert
BEFORE INSERT ON statement_account_precoverage_closures
BEGIN
    SELECT CASE WHEN NOT EXISTS (
        SELECT 1
        FROM statement_accounts sa
        JOIN statement_account_identities sai
          ON sai.id = NEW.statement_account_identity_id
         AND sai.statement_account_id = sa.id
        WHERE sa.id = NEW.statement_account_id
          AND sa.status = 'ACTIVE'
          AND NEW.closed_on < sa.reconciliation_required_from
          AND length(sai.account_number) >= length(NEW.account_suffix)
          AND substr(sai.account_number, -length(NEW.account_suffix)) = NEW.account_suffix
    ) THEN RAISE(ABORT, 'precoverage closure requires an active exact statement-account identity and a closure date before required coverage') END;
    SELECT CASE WHEN EXISTS (
        SELECT 1 FROM current_source_records source
        WHERE source.materialization_kind = 'STATEMENT'
          AND source.statement_account_id = NEW.statement_account_id
          AND source.disposition = 'POSTED'
          AND NOT EXISTS (
              SELECT 1 FROM statement_transactions materialized
              WHERE materialized.source_identity_id = source.source_identity_id
                AND materialized.source_record_id = source.id
          )
    ) THEN RAISE(ABORT, 'current posted statement source must be materialized before precoverage closure') END;
    SELECT CASE WHEN EXISTS (
        SELECT 1 FROM reconciliations
        WHERE statement_account_id = NEW.statement_account_id AND status = 'OPEN'
    ) THEN RAISE(ABORT, 'open reconciliation must be completed or abandoned before precoverage closure') END;
    SELECT CASE WHEN EXISTS (
        SELECT 1 FROM statement_transactions
        WHERE statement_account_id = NEW.statement_account_id AND posted_date > NEW.closed_on
    ) OR EXISTS (
        SELECT 1 FROM reconciliations
        WHERE statement_account_id = NEW.statement_account_id
          AND status <> 'ABANDONED' AND end_date > NEW.closed_on
    ) THEN RAISE(ABORT, 'precoverage closure precedes statement or reconciliation activity') END;
    SELECT CASE WHEN EXISTS (
        SELECT 1
        FROM statement_accounts sa
        JOIN journal_entries je ON je.book_id = sa.book_id
        JOIN journal_lines jl
          ON jl.journal_entry_id = je.id AND jl.account_id = sa.gl_account_id
        WHERE sa.id = NEW.statement_account_id
          AND je.status = 'POSTED' AND je.posting_date > NEW.closed_on
    ) THEN RAISE(ABORT, 'precoverage closure precedes posted control-account activity') END;
    SELECT CASE WHEN EXISTS (
        SELECT 1
        FROM statement_accounts sa
        JOIN journal_entries je ON je.book_id = sa.book_id
        JOIN journal_lines jl
          ON jl.journal_entry_id = je.id AND jl.account_id = sa.gl_account_id
        WHERE sa.id = NEW.statement_account_id AND je.status = 'DRAFT'
    ) THEN RAISE(ABORT, 'draft control-account activity must be resolved before precoverage closure') END;
    SELECT CASE WHEN COALESCE((
        SELECT SUM(jl.debit_cents - jl.credit_cents)
        FROM statement_accounts sa
        JOIN journal_entries je ON je.book_id = sa.book_id
        JOIN journal_lines jl
          ON jl.journal_entry_id = je.id AND jl.account_id = sa.gl_account_id
        WHERE sa.id = NEW.statement_account_id
          AND je.status = 'POSTED' AND je.posting_date <= NEW.closed_on
    ), 0) <> 0 THEN RAISE(ABORT, 'precoverage closure requires an exact-zero control balance') END;
END;

CREATE TRIGGER journal_lines_block_precoverage_closure_insert
BEFORE INSERT ON journal_lines
WHEN EXISTS (
    SELECT 1
    FROM journal_entries je
    JOIN statement_accounts sa
      ON sa.book_id = je.book_id AND sa.gl_account_id = NEW.account_id
    JOIN statement_account_precoverage_closures closure
      ON closure.statement_account_id = sa.id
    WHERE je.id = NEW.journal_entry_id
)
BEGIN
    SELECT RAISE(ABORT, 'precoverage-closed statement controls cannot receive journal lines');
END;

CREATE TRIGGER journal_lines_block_precoverage_closure_update
BEFORE UPDATE ON journal_lines
WHEN EXISTS (
    SELECT 1
    FROM journal_entries je
    JOIN statement_accounts sa
      ON sa.book_id = je.book_id AND sa.gl_account_id = NEW.account_id
    JOIN statement_account_precoverage_closures closure
      ON closure.statement_account_id = sa.id
    WHERE je.id = NEW.journal_entry_id
)
BEGIN
    SELECT RAISE(ABORT, 'precoverage-closed statement controls cannot receive journal lines');
END;

CREATE TRIGGER journal_entries_block_precoverage_closure_post
BEFORE UPDATE OF status ON journal_entries
WHEN OLD.status = 'DRAFT' AND NEW.status = 'POSTED'
 AND EXISTS (
     SELECT 1
     FROM journal_lines jl
     JOIN statement_accounts sa
       ON sa.book_id = NEW.book_id AND sa.gl_account_id = jl.account_id
     JOIN statement_account_precoverage_closures closure
       ON closure.statement_account_id = sa.id
     WHERE jl.journal_entry_id = NEW.id
 )
BEGIN
    SELECT RAISE(ABORT, 'precoverage-closed statement controls cannot receive posted journal activity');
END;

CREATE TRIGGER statement_account_precoverage_closures_immutable_update
BEFORE UPDATE ON statement_account_precoverage_closures
BEGIN
    SELECT RAISE(ABORT, 'statement-account precoverage closure evidence is immutable');
END;

CREATE TRIGGER statement_account_precoverage_closures_immutable_delete
BEFORE DELETE ON statement_account_precoverage_closures
BEGIN
    SELECT RAISE(ABORT, 'statement-account precoverage closure evidence is immutable');
END;

CREATE TRIGGER statement_accounts_validate_precoverage_archive_transition
BEFORE UPDATE OF status, reconciliation_required_through, archived_at, archived_by, archive_reason
ON statement_accounts
WHEN OLD.status = 'ACTIVE' AND NEW.status = 'ARCHIVED'
 AND EXISTS (
     SELECT 1 FROM statement_account_precoverage_closures closure
     WHERE closure.statement_account_id = OLD.id
 )
BEGIN
    SELECT CASE WHEN NEW.reconciliation_required_through <> OLD.reconciliation_required_from
        THEN RAISE(ABORT, 'precoverage closure archive must use the required-from boundary') END;
END;

CREATE TABLE statement_account_precoverage_identity_bindings (
    closure_id TEXT PRIMARY KEY
        REFERENCES statement_account_precoverage_closures(id) ON DELETE RESTRICT,
    active_identity_count INTEGER NOT NULL CHECK (active_identity_count > 0),
    active_identity_digest TEXT NOT NULL CHECK (
        length(active_identity_digest) = 64
        AND active_identity_digest = lower(active_identity_digest)
        AND active_identity_digest NOT GLOB '*[^0-9a-f]*'
    ),
    created_at TEXT NOT NULL,
    created_by TEXT NOT NULL CHECK (length(trim(created_by)) > 0)
) STRICT;

CREATE TRIGGER statement_account_precoverage_identity_bindings_immutable_update
BEFORE UPDATE ON statement_account_precoverage_identity_bindings
BEGIN
    SELECT RAISE(ABORT, 'precoverage identity-set bindings are immutable');
END;

CREATE TRIGGER statement_account_precoverage_identity_bindings_immutable_delete
BEFORE DELETE ON statement_account_precoverage_identity_bindings
BEGIN
    SELECT RAISE(ABORT, 'precoverage identity-set bindings are immutable');
END;

CREATE TRIGGER statement_account_precoverage_closures_require_terminal_sources
BEFORE INSERT ON statement_account_precoverage_closures
BEGIN
    SELECT CASE WHEN date(NEW.closed_on) IS NULL OR date(NEW.closed_on) <> NEW.closed_on
        OR date(NEW.zero_observed_on) IS NULL OR date(NEW.zero_observed_on) <> NEW.zero_observed_on
        THEN RAISE(ABORT, 'precoverage closure dates must be real canonical calendar dates') END;
    SELECT CASE WHEN substr(NEW.closure_evidence_source_path, 1, 1) <> '/'
        OR substr(NEW.zero_evidence_source_path, 1, 1) <> '/'
        OR substr(NEW.input_source_path, 1, 1) <> '/'
        THEN RAISE(ABORT, 'precoverage closure evidence paths must be absolute') END;
    SELECT CASE WHEN COALESCE((
        SELECT source_active FROM statement_account_identities
        WHERE id = NEW.statement_account_identity_id
    ), 0) <> 1 THEN RAISE(ABORT, 'precoverage closure requires an active exact provider identity') END;
    SELECT CASE WHEN EXISTS (
        SELECT 1
        FROM statement_account_identities selected
        JOIN statement_account_identities alias
          ON alias.statement_account_id = selected.statement_account_id
         AND alias.source_active = 1
         AND alias.source_system = selected.source_system
         AND alias.source_realm = selected.source_realm
         AND alias.id <> selected.id
        WHERE selected.id = NEW.statement_account_identity_id
          AND alias.account_number <> selected.account_number
    ) THEN RAISE(ABORT, 'active aliases in the certified provider realm must identify the same provider account') END;
    SELECT CASE WHEN EXISTS (
        SELECT 1 FROM current_source_records source
        WHERE source.materialization_kind = 'STATEMENT'
          AND source.statement_account_id = NEW.statement_account_id
          AND source.disposition IN ('PENDING', 'NEEDS_REVIEW')
    ) THEN RAISE(ABORT, 'pending and review statement source must be resolved to POSTED or SOURCE_ONLY before precoverage closure') END;
END;

CREATE TRIGGER statement_account_identities_block_precoverage_closure_insert
BEFORE INSERT ON statement_account_identities
WHEN EXISTS (
    SELECT 1 FROM statement_account_precoverage_closures closure
    WHERE closure.statement_account_id = NEW.statement_account_id
)
BEGIN
    SELECT RAISE(ABORT, 'precoverage closure freezes the complete statement-account identity set');
END;

CREATE TRIGGER source_records_block_precoverage_closure_insert
BEFORE INSERT ON source_records
WHEN EXISTS (
    SELECT 1
    FROM source_identities identity
    JOIN statement_account_precoverage_closures closure
      ON closure.statement_account_id = identity.statement_account_id
    WHERE identity.id = NEW.source_identity_id
      AND identity.materialization_kind = 'STATEMENT'
)
BEGIN
    SELECT RAISE(ABORT, 'precoverage-closed statement accounts cannot receive source observations or revisions');
END;

CREATE TRIGGER reconciliations_block_precoverage_closure_reopen
BEFORE UPDATE OF status ON reconciliations
WHEN OLD.status = 'COMPLETED' AND NEW.status = 'OPEN'
 AND EXISTS (
     SELECT 1 FROM statement_account_precoverage_closures closure
     WHERE closure.statement_account_id = OLD.statement_account_id
 )
BEGIN
    SELECT RAISE(ABORT, 'precoverage closure is terminal; its reconciliations cannot be reopened');
END;

CREATE TRIGGER reconciliations_block_precoverage_closure_insert
BEFORE INSERT ON reconciliations
WHEN EXISTS (
    SELECT 1 FROM statement_account_precoverage_closures closure
    WHERE closure.statement_account_id = NEW.statement_account_id
)
BEGIN
    SELECT RAISE(ABORT, 'precoverage closure is terminal; no later reconciliation work is allowed');
END;

CREATE VIEW valid_statement_account_precoverage_closures AS
SELECT closure.id, closure.statement_account_id
FROM statement_account_precoverage_closures closure
JOIN statement_accounts sa ON sa.id = closure.statement_account_id
JOIN statement_account_identities selected
  ON selected.id = closure.statement_account_identity_id
 AND selected.statement_account_id = sa.id
JOIN statement_account_precoverage_identity_bindings binding
  ON binding.closure_id = closure.id
WHERE selected.source_active = 1
  AND date(closure.closed_on) = closure.closed_on
  AND date(closure.zero_observed_on) = closure.zero_observed_on
  AND substr(closure.closure_evidence_source_path, 1, 1) = '/'
  AND substr(closure.zero_evidence_source_path, 1, 1) = '/'
  AND substr(closure.input_source_path, 1, 1) = '/'
  AND length(selected.account_number) >= length(closure.account_suffix)
  AND substr(selected.account_number, -length(closure.account_suffix)) = closure.account_suffix
  AND binding.active_identity_count = (
      SELECT COUNT(*) FROM statement_account_identities identity
      WHERE identity.statement_account_id = sa.id AND identity.source_active = 1
  )
  AND NOT EXISTS (
      SELECT 1 FROM statement_account_identities identity
      WHERE identity.statement_account_id = sa.id
        AND identity.created_at > closure.created_at
  )
  AND NOT EXISTS (
      SELECT 1 FROM statement_account_identities alias
      WHERE alias.statement_account_id = sa.id
        AND alias.source_active = 1
        AND alias.source_system = selected.source_system
        AND alias.source_realm = selected.source_realm
        AND alias.id <> selected.id
        AND alias.account_number <> selected.account_number
  )
  AND closure.closed_on < sa.reconciliation_required_from
  AND closure.zero_observed_on >= closure.closed_on
  AND sa.status = 'ARCHIVED'
  AND sa.reconciliation_required_through = sa.reconciliation_required_from
  AND sa.archive_reason = closure.reason
  AND sa.archived_at <> '' AND sa.archived_by = closure.created_by
  AND NOT EXISTS (
      SELECT 1 FROM current_source_records source
      WHERE source.materialization_kind = 'STATEMENT'
        AND source.statement_account_id = sa.id
        AND source.disposition IN ('PENDING', 'NEEDS_REVIEW')
  )
  AND NOT EXISTS (
      SELECT 1 FROM source_records source_record
      JOIN source_identities source_identity ON source_identity.id = source_record.source_identity_id
      WHERE source_identity.materialization_kind = 'STATEMENT'
        AND source_identity.statement_account_id = sa.id
        AND source_record.created_at > closure.created_at
  )
  AND NOT EXISTS (
      SELECT 1 FROM current_source_records source
      WHERE source.materialization_kind = 'STATEMENT'
        AND source.statement_account_id = sa.id
        AND source.disposition = 'POSTED'
        AND NOT EXISTS (
            SELECT 1 FROM statement_transactions materialized
            WHERE materialized.source_identity_id = source.source_identity_id
              AND materialized.source_record_id = source.id
        )
  )
  AND NOT EXISTS (
      SELECT 1 FROM reconciliations
      WHERE statement_account_id = sa.id AND status = 'OPEN'
  )
  AND NOT EXISTS (
      SELECT 1 FROM reconciliations
      WHERE statement_account_id = sa.id
        AND (created_at > closure.created_at OR reopened_at > closure.created_at)
  )
  AND NOT EXISTS (
      SELECT 1 FROM statement_transactions
      WHERE statement_account_id = sa.id AND posted_date > closure.closed_on
  )
  AND NOT EXISTS (
      SELECT 1 FROM reconciliations
      WHERE statement_account_id = sa.id
        AND status <> 'ABANDONED' AND end_date > closure.closed_on
  )
  AND NOT EXISTS (
      SELECT 1 FROM journal_entries je
      JOIN journal_lines jl ON jl.journal_entry_id = je.id
      WHERE je.book_id = sa.book_id AND jl.account_id = sa.gl_account_id
        AND je.status = 'POSTED' AND je.posting_date > closure.closed_on
  )
  AND NOT EXISTS (
      SELECT 1 FROM journal_entries je
      JOIN journal_lines jl ON jl.journal_entry_id = je.id
      WHERE je.book_id = sa.book_id AND jl.account_id = sa.gl_account_id
        AND je.status = 'DRAFT'
  )
  AND COALESCE((
      SELECT SUM(jl.debit_cents - jl.credit_cents)
      FROM journal_entries je
      JOIN journal_lines jl ON jl.journal_entry_id = je.id
      WHERE je.book_id = sa.book_id AND jl.account_id = sa.gl_account_id
        AND je.status = 'POSTED' AND je.posting_date <= closure.closed_on
  ), 0) = 0
  AND COALESCE((
      SELECT SUM(jl.debit_cents - jl.credit_cents)
      FROM journal_entries je
      JOIN journal_lines jl ON jl.journal_entry_id = je.id
      WHERE je.book_id = sa.book_id AND jl.account_id = sa.gl_account_id
        AND je.status = 'POSTED'
  ), 0) = 0
  AND (
      SELECT COUNT(*) FROM audit_events audit
      WHERE audit.command = 'statement-account lifecycle close-before-coverage'
        AND audit.aggregate_type = 'statement_account_precoverage_closure'
        AND audit.aggregate_id = closure.id
  ) = 1
  AND (
      SELECT COUNT(*) FROM audit_events audit
      WHERE audit.command = 'statement-account lifecycle close-before-coverage'
        AND audit.aggregate_type = 'statement_account_precoverage_closure'
        AND audit.aggregate_id = closure.id
        AND audit.actor = closure.created_by
        AND json_extract(audit.payload_json, '$.id') = closure.id
        AND json_type(audit.payload_json, '$.active_identity_count') IS NULL
        AND json_type(audit.payload_json, '$.active_identity_digest') IS NULL
        AND json_extract(audit.payload_json, '$.statement_account_id') = closure.statement_account_id
        AND json_extract(audit.payload_json, '$.statement_account') = sa.code
        AND json_extract(audit.payload_json, '$.statement_account_identity_id') = closure.statement_account_identity_id
        AND json_extract(audit.payload_json, '$.entity') = (
            SELECT code FROM entities WHERE id = sa.entity_id
        )
        AND json_extract(audit.payload_json, '$.book') = (
            SELECT code FROM books WHERE id = sa.book_id
        )
        AND json_extract(audit.payload_json, '$.gl_account') = (
            SELECT code FROM accounts WHERE id = sa.gl_account_id
        )
        AND json_extract(audit.payload_json, '$.reconciliation_required_from') = sa.reconciliation_required_from
        AND json_extract(audit.payload_json, '$.reconciliation_required_through') = sa.reconciliation_required_through
        AND json_extract(audit.payload_json, '$.coverage_disposition') = 'CLOSED_BEFORE_REQUIRED_COVERAGE'
        AND json_extract(audit.payload_json, '$.closed_on') = closure.closed_on
        AND json_extract(audit.payload_json, '$.closure_evidence.source_kind') = closure.closure_evidence_source_kind
        AND json_extract(audit.payload_json, '$.closure_evidence.source_path') = closure.closure_evidence_source_path
        AND json_extract(audit.payload_json, '$.closure_evidence.source_sha256') = closure.closure_evidence_source_sha256
        AND json_extract(audit.payload_json, '$.closure_evidence.locator') = closure.closure_evidence_locator
        AND json_extract(audit.payload_json, '$.zero_evidence.source_kind') = closure.zero_evidence_source_kind
        AND json_extract(audit.payload_json, '$.zero_evidence.source_path') = closure.zero_evidence_source_path
        AND json_extract(audit.payload_json, '$.zero_evidence.source_sha256') = closure.zero_evidence_source_sha256
        AND json_extract(audit.payload_json, '$.zero_evidence.locator') = closure.zero_evidence_locator
        AND json_extract(audit.payload_json, '$.zero_evidence.payload_sha256') = closure.zero_evidence_payload_sha256
        AND json_extract(audit.payload_json, '$.zero_evidence.observed_on') = closure.zero_observed_on
        AND json_extract(audit.payload_json, '$.zero_evidence.provider_status') = closure.provider_status
        AND json_extract(audit.payload_json, '$.zero_evidence.current_balance_cents') = closure.current_balance_cents
        AND json_extract(audit.payload_json, '$.zero_evidence.available_balance_cents') = closure.available_balance_cents
        AND json_extract(audit.payload_json, '$.account_holder') = closure.account_holder
        AND json_extract(audit.payload_json, '$.account_suffix') = closure.account_suffix
        AND json_extract(audit.payload_json, '$.reason') = closure.reason
        AND json_extract(audit.payload_json, '$.input_source_path') = closure.input_source_path
        AND json_extract(audit.payload_json, '$.input_source_sha256') = closure.input_source_sha256
        AND json_extract(audit.payload_json, '$.control_balance_at_closure_cents') = 0
        AND json_extract(audit.payload_json, '$.current_control_balance_cents') = 0
        AND json_extract(audit.payload_json, '$.post_closure_control_line_count') = 0
        AND json_extract(audit.payload_json, '$.draft_control_line_count') = 0
        AND json_extract(audit.payload_json, '$.status') = sa.status
        AND json_extract(audit.payload_json, '$.archived_at') = sa.archived_at
        AND json_extract(audit.payload_json, '$.archived_by') = sa.archived_by
        AND json_extract(audit.payload_json, '$.created_at') = closure.created_at
        AND json_extract(audit.payload_json, '$.created_by') = closure.created_by
        AND json_extract(audit.payload_json, '$.changed') = 1
  ) = 1
  AND (
      SELECT COUNT(*) FROM audit_events audit
      WHERE audit.command = 'statement-account precoverage bind-identities'
        AND audit.aggregate_type = 'statement_account_precoverage_identity_binding'
        AND audit.aggregate_id = binding.closure_id
  ) = 1
  AND (
      SELECT COUNT(*) FROM audit_events audit
      WHERE audit.command = 'statement-account precoverage bind-identities'
        AND audit.aggregate_type = 'statement_account_precoverage_identity_binding'
        AND audit.aggregate_id = binding.closure_id
        AND audit.actor = binding.created_by
        AND json_extract(audit.payload_json, '$.closure_id') = binding.closure_id
        AND json_extract(audit.payload_json, '$.statement_account_id') = closure.statement_account_id
        AND json_extract(audit.payload_json, '$.active_identity_count') = binding.active_identity_count
        AND json_extract(audit.payload_json, '$.active_identity_digest') = binding.active_identity_digest
        AND json_extract(audit.payload_json, '$.created_at') = binding.created_at
        AND json_extract(audit.payload_json, '$.created_by') = binding.created_by
  ) = 1;

CREATE TRIGGER book_periods_validate_close
BEFORE UPDATE OF status ON book_periods
WHEN OLD.status = 'OPEN' AND NEW.status = 'CLOSED'
BEGIN
	SELECT CASE WHEN NEW.closed_at IS NULL OR length(trim(NEW.closed_at)) = 0
		OR NEW.closed_by IS NULL OR length(trim(NEW.closed_by)) = 0
		OR NEW.close_digest IS NULL OR length(NEW.close_digest) <> 64
		OR NEW.close_digest <> lower(NEW.close_digest)
		OR NEW.close_digest GLOB '*[^0-9a-f]*'
		THEN RAISE(ABORT, 'period close timestamp, actor, and SHA-256 digest are required') END;
	SELECT CASE WHEN NEW.closed_at IS OLD.closed_at
		THEN RAISE(ABORT, 'period close requires fresh lifecycle evidence') END;
	SELECT CASE WHEN NEW.reopened_at IS NOT OLD.reopened_at
		OR NEW.reopened_by IS NOT OLD.reopened_by
		OR NEW.reopen_reason IS NOT OLD.reopen_reason
		THEN RAISE(ABORT, 'period reopen evidence is immutable outside a reopen transition') END;
	SELECT CASE WHEN COALESCE((SELECT status FROM books WHERE id = OLD.book_id), '') <> 'ACTIVE'
		THEN RAISE(ABORT, 'period book is not active') END;
	SELECT CASE WHEN EXISTS (
		SELECT 1 FROM fiscal_periods current_period
		JOIN fiscal_periods earlier_period ON earlier_period.end_date < current_period.start_date
		JOIN book_periods earlier ON earlier.period_id = earlier_period.id AND earlier.book_id = OLD.book_id
		WHERE current_period.id = OLD.period_id AND earlier.status = 'OPEN'
	) THEN RAISE(ABORT, 'earlier fiscal periods must be closed first') END;
	SELECT CASE WHEN EXISTS (
		SELECT 1 FROM fiscal_periods current_period
		JOIN fiscal_periods later_period ON later_period.start_date > current_period.end_date
		JOIN book_periods later ON later.period_id = later_period.id AND later.book_id = OLD.book_id
		WHERE current_period.id = OLD.period_id AND later.status = 'CLOSED'
	) THEN RAISE(ABORT, 'a later fiscal period is already closed') END;
	SELECT CASE WHEN EXISTS (
		SELECT 1 FROM journal_entries je
		JOIN fiscal_periods fp ON fp.id = OLD.period_id
		WHERE je.book_id = OLD.book_id AND je.status = 'DRAFT'
		  AND je.posting_date BETWEEN fp.start_date AND fp.end_date
	) THEN RAISE(ABORT, 'unresolved draft journals remain in the period') END;
	SELECT CASE WHEN (
		SELECT COALESCE(SUM(jl.debit_cents), 0)
		FROM journal_entries je JOIN journal_lines jl ON jl.journal_entry_id = je.id
		WHERE je.book_id = OLD.book_id AND je.status = 'POSTED'
		  AND je.posting_date <= (SELECT end_date FROM fiscal_periods WHERE id = OLD.period_id)
	) <> (
		SELECT COALESCE(SUM(jl.credit_cents), 0)
		FROM journal_entries je JOIN journal_lines jl ON jl.journal_entry_id = je.id
		WHERE je.book_id = OLD.book_id AND je.status = 'POSTED'
		  AND je.posting_date <= (SELECT end_date FROM fiscal_periods WHERE id = OLD.period_id)
	) THEN RAISE(ABORT, 'trial balance must balance before period close') END;
	SELECT CASE WHEN EXISTS (
		SELECT 1
		FROM fiscal_periods current_period
		JOIN fiscal_periods year_period ON year_period.fiscal_year = current_period.fiscal_year
		JOIN journal_entries je ON je.period_id = year_period.id
		JOIN journal_lines jl ON jl.journal_entry_id = je.id
		JOIN accounts a ON a.id = jl.account_id
		WHERE current_period.id = OLD.period_id AND current_period.is_year_end = 1
		  AND je.book_id = OLD.book_id AND je.status = 'POSTED'
		  AND a.account_type IN ('REVENUE', 'EXPENSE')
		GROUP BY a.id HAVING SUM(jl.debit_cents - jl.credit_cents) <> 0
	) THEN RAISE(ABORT, 'fiscal-year profit-and-loss balances require an active closing journal') END;
	SELECT CASE WHEN EXISTS (
		SELECT 1
		FROM statement_account_precoverage_closures closure
		JOIN statement_accounts sa ON sa.id = closure.statement_account_id
		LEFT JOIN valid_statement_account_precoverage_closures valid ON valid.id = closure.id
		WHERE sa.book_id = OLD.book_id AND valid.id IS NULL
	) THEN RAISE(ABORT, 'invalid precoverage statement-account lifecycle blocks period close') END;
	SELECT CASE WHEN EXISTS (
		WITH current_period AS (
			SELECT start_date, end_date FROM fiscal_periods WHERE id = OLD.period_id
		), required AS (
			SELECT sa.id,
				MAX(sa.reconciliation_required_from, current_period.start_date) AS required_start,
				MIN(COALESCE(sa.reconciliation_required_through, current_period.end_date), current_period.end_date) AS required_end
			FROM statement_accounts sa JOIN current_period
			WHERE sa.book_id = OLD.book_id AND sa.required_for_close = 1
			  AND sa.reconciliation_required_from <= current_period.end_date
			  AND COALESCE(sa.reconciliation_required_through, current_period.end_date) >= current_period.start_date
			  AND NOT EXISTS (
			      SELECT 1 FROM valid_statement_account_precoverage_closures valid
			      WHERE valid.statement_account_id = sa.id
			  )
		)
		SELECT 1 FROM required
		WHERE COALESCE((
			SELECT SUM(CAST(
				julianday(MIN(r.end_date, required.required_end)) -
				julianday(MAX(r.start_date, required.required_start)) + 1 AS INTEGER))
			FROM reconciliations r
			JOIN reconciliation_status status ON status.reconciliation_id = r.id
			WHERE r.statement_account_id = required.id AND r.status = 'COMPLETED'
			  AND r.beginning_balance_cents + status.statement_activity_cents = r.ending_balance_cents
			  AND status.ledger_beginning_balance_cents = r.beginning_balance_cents
			  AND status.ledger_ending_balance_cents = r.ending_balance_cents
			  AND status.fully_allocated_statement_count = status.statement_transaction_count
			  AND status.fully_allocated_control_line_count = status.control_line_count
			  AND r.end_date >= required.required_start AND r.start_date <= required.required_end
		), 0) <> CAST(julianday(required.required_end) - julianday(required.required_start) + 1 AS INTEGER)
	) THEN RAISE(ABORT, 'required statement accounts must be reconciled through period end') END;
	SELECT CASE WHEN EXISTS (
		SELECT 1
		FROM accounts a
		JOIN book_accounts ba ON ba.account_id = a.id AND ba.book_id = OLD.book_id
		JOIN journal_lines jl ON jl.account_id = a.id
		JOIN journal_entries je ON je.id = jl.journal_entry_id
		WHERE a.subtype = 'SUSPENSE' AND je.book_id = OLD.book_id AND je.status = 'POSTED'
		  AND je.posting_date <= (SELECT end_date FROM fiscal_periods WHERE id = OLD.period_id)
		GROUP BY a.id HAVING SUM(jl.debit_cents - jl.credit_cents) <> 0
	) THEN RAISE(ABORT, 'suspense accounts must have zero balances before period close') END;
END;

CREATE TABLE reconciliation_outstanding_items (
    id TEXT PRIMARY KEY,
    reconciliation_id TEXT NOT NULL REFERENCES reconciliations(id) ON DELETE RESTRICT,
    journal_line_id TEXT NOT NULL REFERENCES journal_lines(id) ON DELETE RESTRICT,
    outstanding_amount_cents INTEGER NOT NULL CHECK (outstanding_amount_cents <> 0),
    created_at TEXT NOT NULL,
    created_by TEXT NOT NULL,
    UNIQUE (reconciliation_id, journal_line_id)
) STRICT;

CREATE INDEX reconciliation_outstanding_items_line_idx
    ON reconciliation_outstanding_items(journal_line_id);

CREATE TABLE source_record_operator_attestations (
    source_record_id TEXT PRIMARY KEY REFERENCES source_records(id) ON DELETE RESTRICT,
    attested_at TEXT NOT NULL,
    attested_by TEXT NOT NULL CHECK (length(trim(attested_by)) > 0)
) STRICT;

CREATE TRIGGER source_record_operator_attestations_validate_insert
BEFORE INSERT ON source_record_operator_attestations BEGIN
    SELECT CASE WHEN COALESCE((
        SELECT identity_row.source_system
        FROM source_records source_row
        JOIN source_identities identity_row ON identity_row.id = source_row.source_identity_id
        WHERE source_row.id = NEW.source_record_id
    ), '') <> 'MANUAL_RECONCILIATION'
        THEN RAISE(ABORT, 'operator attestation requires manual-reconciliation source evidence') END;
END;

CREATE TRIGGER source_record_operator_attestations_immutable_update
BEFORE UPDATE ON source_record_operator_attestations BEGIN
    SELECT RAISE(ABORT, 'source-record operator attestations are immutable');
END;

CREATE TRIGGER source_record_operator_attestations_immutable_delete
BEFORE DELETE ON source_record_operator_attestations BEGIN
    SELECT RAISE(ABORT, 'source-record operator attestations are immutable');
END;

CREATE TRIGGER reconciliation_allocations_validate_insert
BEFORE INSERT ON reconciliation_allocations BEGIN
    SELECT CASE WHEN (SELECT status FROM reconciliations WHERE id = NEW.reconciliation_id) <> 'OPEN'
        THEN RAISE(ABORT, 'only open reconciliations may receive allocations') END;
    SELECT CASE WHEN NOT EXISTS (
        SELECT 1
        FROM reconciliations r
        JOIN statement_accounts sa ON sa.id = r.statement_account_id
        JOIN statement_transactions st ON st.id = NEW.statement_transaction_id
        JOIN source_records sr ON sr.id = st.source_record_id
        JOIN source_identities si ON si.id = sr.source_identity_id
        JOIN import_batches ib ON ib.id = sr.import_batch_id
        JOIN journal_lines jl ON jl.id = NEW.journal_line_id
        JOIN journal_entries je ON je.id = jl.journal_entry_id
        WHERE r.id = NEW.reconciliation_id
          AND st.statement_account_id = r.statement_account_id
          AND st.posted_date BETWEEN r.start_date AND r.end_date
          AND je.book_id = sa.book_id
          AND je.status = 'POSTED'
          AND je.posting_date BETWEEN MIN(sa.reconciliation_required_from, r.start_date) AND r.end_date
          AND jl.account_id = sa.gl_account_id
          AND ((NEW.allocated_amount_cents > 0 AND st.amount_cents > 0
                AND (jl.debit_cents - jl.credit_cents) > 0)
            OR (NEW.allocated_amount_cents < 0 AND st.amount_cents < 0
                AND (jl.debit_cents - jl.credit_cents) < 0))
          AND (
              si.source_system <> 'MANUAL_RECONCILIATION' OR (
                  sr.observation_kind = 'PROVIDER'
                  AND sr.disposition = 'POSTED'
                  AND sr.revision = 1
                  AND sr.supersedes_source_record_id IS NULL
                  AND ib.source_system = 'MANUAL_RECONCILIATION'
                  AND ib.status = 'COMPLETED'
                  AND length(ib.file_sha256) = 64
                  AND ib.file_sha256 = lower(ib.file_sha256)
                  AND ib.file_sha256 NOT GLOB '*[^0-9a-f]*'
                  AND EXISTS (
                      SELECT 1 FROM source_record_operator_attestations attestation
                      WHERE attestation.source_record_id = sr.id
                        AND attestation.attested_at = sr.created_at
                        AND attestation.attested_by = sr.created_by
                  )
                  AND si.external_id = printf(
                      'reconcile:%s:%d:%d', lower(si.source_account),
                      je.entry_number, jl.line_number
                  )
                  AND json_type(sr.raw_json) = 'object'
                  AND (SELECT COUNT(*) FROM json_each(sr.raw_json)) = 6
                  AND json_type(sr.raw_json, '$.plan_digest') = 'text'
                  AND json_extract(sr.raw_json, '$.plan_digest') = ib.file_sha256
                  AND json_type(sr.raw_json, '$.transaction_number') = 'integer'
                  AND json_extract(sr.raw_json, '$.transaction_number') = je.entry_number
                  AND json_type(sr.raw_json, '$.line_number') = 'integer'
                  AND json_extract(sr.raw_json, '$.line_number') = jl.line_number
                  AND json_type(sr.raw_json, '$.ledger_date') = 'text'
                  AND json_extract(sr.raw_json, '$.ledger_date') = je.posting_date
                  AND json_type(sr.raw_json, '$.statement_date') = 'text'
                  AND json_extract(sr.raw_json, '$.statement_date') = st.posted_date
                  AND json_type(sr.raw_json, '$.provenance') = 'text'
                  AND json_extract(sr.raw_json, '$.provenance') = 'OPERATOR_ATTESTATION'
              )
          )
    ) THEN RAISE(ABORT, 'allocation must link same-sign statement and eligible posted control-account activity') END;
    SELECT CASE WHEN (
        SELECT COALESCE(SUM(ri.allocated_amount_cents), 0) + NEW.allocated_amount_cents
        FROM reconciliation_allocations ri
        WHERE ri.reconciliation_id = NEW.reconciliation_id
          AND ri.statement_transaction_id = NEW.statement_transaction_id
    ) NOT BETWEEN MIN(0, (SELECT amount_cents FROM statement_transactions WHERE id = NEW.statement_transaction_id))
              AND MAX(0, (SELECT amount_cents FROM statement_transactions WHERE id = NEW.statement_transaction_id))
        THEN RAISE(ABORT, 'allocation exceeds the statement transaction amount') END;
    SELECT CASE WHEN (
        SELECT COALESCE(SUM(ri.allocated_amount_cents), 0) + NEW.allocated_amount_cents
        FROM reconciliation_allocations ri
        JOIN reconciliations allocated ON allocated.id = ri.reconciliation_id
        WHERE ri.journal_line_id = NEW.journal_line_id
          AND allocated.status <> 'ABANDONED'
    ) NOT BETWEEN MIN(0, (
                  SELECT jl.debit_cents - jl.credit_cents FROM journal_lines jl WHERE jl.id = NEW.journal_line_id
              ))
              AND MAX(0, (
                  SELECT jl.debit_cents - jl.credit_cents FROM journal_lines jl WHERE jl.id = NEW.journal_line_id
              ))
        THEN RAISE(ABORT, 'allocation exceeds the remaining control-account journal line amount') END;
END;

CREATE TRIGGER reconciliation_outstanding_items_validate_insert
BEFORE INSERT ON reconciliation_outstanding_items BEGIN
    SELECT CASE WHEN (SELECT status FROM reconciliations WHERE id = NEW.reconciliation_id) <> 'OPEN'
        THEN RAISE(ABORT, 'only open reconciliations may record outstanding items') END;
    SELECT CASE WHEN NOT EXISTS (
        SELECT 1
        FROM reconciliations r
        JOIN statement_accounts sa ON sa.id = r.statement_account_id
        JOIN journal_lines jl ON jl.id = NEW.journal_line_id AND jl.account_id = sa.gl_account_id
        JOIN journal_entries je ON je.id = jl.journal_entry_id
        WHERE r.id = NEW.reconciliation_id
          AND je.book_id = sa.book_id AND je.status = 'POSTED'
          AND je.posting_date BETWEEN MIN(sa.reconciliation_required_from, r.start_date) AND r.end_date
          AND NEW.outstanding_amount_cents =
              (jl.debit_cents - jl.credit_cents) - COALESCE((
                  SELECT SUM(ri.allocated_amount_cents)
                  FROM reconciliation_allocations ri
                  JOIN reconciliations allocated ON allocated.id = ri.reconciliation_id
                  WHERE ri.journal_line_id = jl.id
                    AND allocated.statement_account_id = r.statement_account_id
                    AND allocated.status <> 'ABANDONED'
                    AND allocated.end_date <= r.end_date
              ), 0)
    ) THEN RAISE(ABORT, 'outstanding item must exactly record the unallocated control-line remainder') END;
END;

CREATE TRIGGER reconciliation_outstanding_items_validate_update
BEFORE UPDATE ON reconciliation_outstanding_items BEGIN
    SELECT RAISE(ABORT, 'reconciliation outstanding items are immutable; remove then re-record');
END;

CREATE TRIGGER reconciliation_outstanding_items_validate_delete
BEFORE DELETE ON reconciliation_outstanding_items
WHEN (SELECT status FROM reconciliations WHERE id = OLD.reconciliation_id) <> 'OPEN'
BEGIN
    SELECT RAISE(ABORT, 'non-open reconciliation outstanding items are immutable');
END;

CREATE VIEW reconciliation_status AS
SELECT
    r.id AS reconciliation_id,
    COALESCE((
        SELECT COUNT(*) FROM statement_transactions st
        WHERE st.statement_account_id = r.statement_account_id
          AND st.posted_date BETWEEN r.start_date AND r.end_date
    ), 0) AS statement_transaction_count,
    COALESCE((
        SELECT SUM(st.amount_cents) FROM statement_transactions st
        WHERE st.statement_account_id = r.statement_account_id
          AND st.posted_date BETWEEN r.start_date AND r.end_date
    ), 0) AS statement_activity_cents,
    COALESCE((
        SELECT COUNT(*) FROM statement_transactions st
        WHERE st.statement_account_id = r.statement_account_id
          AND st.posted_date BETWEEN r.start_date AND r.end_date
          AND COALESCE((
              SELECT SUM(ri.allocated_amount_cents)
              FROM reconciliation_allocations ri
              WHERE ri.reconciliation_id = r.id AND ri.statement_transaction_id = st.id
          ), 0) = st.amount_cents
    ), 0) AS fully_allocated_statement_count,
    COALESCE((
        SELECT COUNT(DISTINCT ri.journal_line_id)
        FROM reconciliation_allocations ri WHERE ri.reconciliation_id = r.id
    ), 0) AS control_line_count,
    COALESCE((
        SELECT COUNT(*) FROM (
            SELECT current_allocation.journal_line_id
            FROM reconciliation_allocations current_allocation
            JOIN journal_lines jl ON jl.id = current_allocation.journal_line_id
            WHERE current_allocation.reconciliation_id = r.id
            GROUP BY current_allocation.journal_line_id
            HAVING COALESCE((
                SELECT SUM(allocation.allocated_amount_cents)
                FROM reconciliation_allocations allocation
                JOIN reconciliations allocated ON allocated.id = allocation.reconciliation_id
                WHERE allocation.journal_line_id = current_allocation.journal_line_id
                  AND allocated.status <> 'ABANDONED' AND allocated.end_date <= r.end_date
            ), 0) = MAX(jl.debit_cents - jl.credit_cents)
        ) fully_allocated
    ), 0) AS fully_allocated_control_line_count,
    COALESCE((
        SELECT COUNT(*) FROM reconciliation_allocations ri WHERE ri.reconciliation_id = r.id
    ), 0) AS allocation_count,
    COALESCE((
        SELECT SUM(jl.debit_cents - jl.credit_cents)
        FROM statement_accounts sa
        JOIN journal_entries je ON je.book_id = sa.book_id AND je.status = 'POSTED' AND je.posting_date < r.start_date
        JOIN journal_lines jl ON jl.journal_entry_id = je.id AND jl.account_id = sa.gl_account_id
        WHERE sa.id = r.statement_account_id
    ), 0) AS book_beginning_balance_cents,
    COALESCE((
        SELECT SUM(jl.debit_cents - jl.credit_cents)
        FROM statement_accounts sa
        JOIN journal_entries je ON je.book_id = sa.book_id AND je.status = 'POSTED' AND je.posting_date <= r.end_date
        JOIN journal_lines jl ON jl.journal_entry_id = je.id AND jl.account_id = sa.gl_account_id
        WHERE sa.id = r.statement_account_id
    ), 0) AS book_ending_balance_cents,
    COALESCE((
        SELECT SUM(prior_item.outstanding_amount_cents)
        FROM reconciliations prior
        JOIN reconciliation_outstanding_items prior_item ON prior_item.reconciliation_id = prior.id
        WHERE prior.statement_account_id = r.statement_account_id
          AND prior.status = 'COMPLETED' AND prior.end_date < r.start_date
          AND prior.end_date = (
              SELECT MAX(candidate.end_date) FROM reconciliations candidate
              WHERE candidate.statement_account_id = r.statement_account_id
                AND candidate.status = 'COMPLETED' AND candidate.end_date < r.start_date
          )
    ), 0) AS opening_outstanding_cents,
    COALESCE((
        SELECT SUM(item.outstanding_amount_cents)
        FROM reconciliation_outstanding_items item WHERE item.reconciliation_id = r.id
    ), 0) AS ending_outstanding_cents,
    COALESCE((
        SELECT COUNT(*) FROM reconciliation_outstanding_items item WHERE item.reconciliation_id = r.id
    ), 0) AS outstanding_line_count,
    COALESCE((
        SELECT COUNT(*)
        FROM statement_accounts sa
        JOIN journal_entries je ON je.book_id = sa.book_id AND je.status = 'POSTED'
        JOIN journal_lines jl ON jl.journal_entry_id = je.id AND jl.account_id = sa.gl_account_id
        LEFT JOIN reconciliation_outstanding_items item
          ON item.reconciliation_id = r.id AND item.journal_line_id = jl.id
        WHERE sa.id = r.statement_account_id
          AND je.posting_date BETWEEN MIN(sa.reconciliation_required_from, r.start_date) AND r.end_date
          AND (
            ((jl.debit_cents - jl.credit_cents) - COALESCE((
                SELECT SUM(ri.allocated_amount_cents)
                FROM reconciliation_allocations ri
                JOIN reconciliations allocated ON allocated.id = ri.reconciliation_id
                WHERE ri.journal_line_id = jl.id
                  AND allocated.statement_account_id = r.statement_account_id
                  AND allocated.status <> 'ABANDONED' AND allocated.end_date <= r.end_date
            ), 0) = 0 AND item.id IS NOT NULL)
            OR
            ((jl.debit_cents - jl.credit_cents) - COALESCE((
                SELECT SUM(ri.allocated_amount_cents)
                FROM reconciliation_allocations ri
                JOIN reconciliations allocated ON allocated.id = ri.reconciliation_id
                WHERE ri.journal_line_id = jl.id
                  AND allocated.statement_account_id = r.statement_account_id
                  AND allocated.status <> 'ABANDONED' AND allocated.end_date <= r.end_date
            ), 0) <> 0 AND (item.id IS NULL OR item.outstanding_amount_cents <>
                (jl.debit_cents - jl.credit_cents) - COALESCE((
                    SELECT SUM(ri.allocated_amount_cents)
                    FROM reconciliation_allocations ri
                    JOIN reconciliations allocated ON allocated.id = ri.reconciliation_id
                    WHERE ri.journal_line_id = jl.id
                      AND allocated.statement_account_id = r.statement_account_id
                      AND allocated.status <> 'ABANDONED' AND allocated.end_date <= r.end_date
                ), 0)))
          )
    ), 0) AS outstanding_mismatch_count,
    COALESCE((
        SELECT SUM(jl.debit_cents - jl.credit_cents)
        FROM statement_accounts sa
        JOIN journal_entries je ON je.book_id = sa.book_id AND je.status = 'POSTED' AND je.posting_date < r.start_date
        JOIN journal_lines jl ON jl.journal_entry_id = je.id AND jl.account_id = sa.gl_account_id
        WHERE sa.id = r.statement_account_id
    ), 0) - COALESCE((
        SELECT SUM(prior_item.outstanding_amount_cents)
        FROM reconciliations prior
        JOIN reconciliation_outstanding_items prior_item ON prior_item.reconciliation_id = prior.id
        WHERE prior.statement_account_id = r.statement_account_id
          AND prior.status = 'COMPLETED' AND prior.end_date < r.start_date
          AND prior.end_date = (
              SELECT MAX(candidate.end_date) FROM reconciliations candidate
              WHERE candidate.statement_account_id = r.statement_account_id
                AND candidate.status = 'COMPLETED' AND candidate.end_date < r.start_date
          )
    ), 0) AS ledger_beginning_balance_cents,
    COALESCE((
        SELECT SUM(jl.debit_cents - jl.credit_cents)
        FROM statement_accounts sa
        JOIN journal_entries je ON je.book_id = sa.book_id AND je.status = 'POSTED' AND je.posting_date <= r.end_date
        JOIN journal_lines jl ON jl.journal_entry_id = je.id AND jl.account_id = sa.gl_account_id
        WHERE sa.id = r.statement_account_id
    ), 0) - COALESCE((
        SELECT SUM(item.outstanding_amount_cents)
        FROM reconciliation_outstanding_items item WHERE item.reconciliation_id = r.id
    ), 0) AS ledger_ending_balance_cents
FROM reconciliations r;

CREATE TRIGGER reconciliations_structure_immutable
BEFORE UPDATE OF statement_account_id, start_date, end_date,
    created_at, created_by
ON reconciliations BEGIN
    SELECT RAISE(ABORT, 'reconciliation identity, dates, and creation evidence are immutable');
END;

CREATE TRIGGER reconciliations_beginning_balance_guard
BEFORE UPDATE OF beginning_balance_cents ON reconciliations
WHEN NOT (
    OLD.status = 'OPEN' AND NEW.status = 'OPEN'
    AND OLD.reopened_at IS NOT NULL AND NEW.reopened_at IS OLD.reopened_at
    AND NEW.reopened_by IS OLD.reopened_by AND NEW.reopen_reason IS OLD.reopen_reason
)
BEGIN
    SELECT RAISE(ABORT, 'beginning balance changes only while revising an explicitly reopened reconciliation');
END;

CREATE TRIGGER reconciliations_ending_balance_guard
BEFORE UPDATE OF ending_balance_cents ON reconciliations
WHEN NOT (
    OLD.status = 'OPEN' AND NEW.status = 'OPEN'
    AND OLD.reopened_at IS NOT NULL AND NEW.reopened_at IS OLD.reopened_at
    AND NEW.reopened_by IS OLD.reopened_by AND NEW.reopen_reason IS OLD.reopen_reason
)
BEGIN
    SELECT RAISE(ABORT, 'ending balance changes only while revising an explicitly reopened reconciliation');
END;

CREATE TRIGGER reconciliations_validate_complete
BEFORE UPDATE OF status ON reconciliations
WHEN OLD.status = 'OPEN' AND NEW.status = 'COMPLETED'
BEGIN
    SELECT CASE WHEN NEW.completed_at IS NULL OR length(trim(NEW.completed_at)) = 0
        OR NEW.completed_by IS NULL OR length(trim(NEW.completed_by)) = 0
        THEN RAISE(ABORT, 'completion timestamp and actor are required') END;
    SELECT CASE WHEN NEW.reopened_at IS NOT OLD.reopened_at
        OR NEW.reopened_by IS NOT OLD.reopened_by OR NEW.reopen_reason IS NOT OLD.reopen_reason
        THEN RAISE(ABORT, 'reconciliation reopen evidence is immutable during completion') END;
    SELECT CASE WHEN EXISTS (
        SELECT 1 FROM reconciliations prior
        WHERE prior.statement_account_id = NEW.statement_account_id AND prior.status <> 'ABANDONED'
          AND prior.end_date < NEW.start_date
          AND prior.end_date = (
              SELECT MAX(candidate.end_date) FROM reconciliations candidate
              WHERE candidate.statement_account_id = NEW.statement_account_id
                AND candidate.status <> 'ABANDONED' AND candidate.end_date < NEW.start_date
          ) AND prior.status <> 'COMPLETED'
    ) THEN RAISE(ABORT, 'the prior reconciliation must be completed first') END;
    SELECT CASE WHEN NEW.beginning_balance_cents + (
        SELECT statement_activity_cents FROM reconciliation_status WHERE reconciliation_id = NEW.id
    ) <> NEW.ending_balance_cents
        THEN RAISE(ABORT, 'statement activity does not reach the ending balance') END;
    SELECT CASE WHEN (
        SELECT ledger_beginning_balance_cents FROM reconciliation_status WHERE reconciliation_id = NEW.id
    ) <> NEW.beginning_balance_cents
        THEN RAISE(ABORT, 'adjusted book beginning balance does not equal statement beginning balance') END;
    SELECT CASE WHEN (
        SELECT ledger_ending_balance_cents FROM reconciliation_status WHERE reconciliation_id = NEW.id
    ) <> NEW.ending_balance_cents
        THEN RAISE(ABORT, 'adjusted book ending balance does not equal statement ending balance') END;
    SELECT CASE WHEN EXISTS (
        SELECT 1 FROM reconciliation_status status WHERE status.reconciliation_id = NEW.id
          AND status.fully_allocated_statement_count <> status.statement_transaction_count
    ) THEN RAISE(ABORT, 'every statement transaction must be fully allocated') END;
    SELECT CASE WHEN EXISTS (
        SELECT 1 FROM reconciliation_status status WHERE status.reconciliation_id = NEW.id
          AND status.fully_allocated_control_line_count <> status.control_line_count
    ) THEN RAISE(ABORT, 'every cleared control-account line must be fully allocated') END;
    SELECT CASE WHEN EXISTS (
        SELECT 1 FROM reconciliation_status status WHERE status.reconciliation_id = NEW.id
          AND status.outstanding_mismatch_count <> 0
    ) THEN RAISE(ABORT, 'every unallocated control-account remainder must be explicitly reviewed as outstanding') END;
END;
