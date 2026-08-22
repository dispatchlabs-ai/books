package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/dispatchlabs-ai/books/internal/apperr"
)

type DoctorResult struct {
	IntegrityCheck             string `json:"integrity_check"`
	ForeignKeyViolations       int    `json:"foreign_key_violations"`
	UnbalancedJournals         int    `json:"unbalanced_journals"`
	InvalidPostedLines         int    `json:"invalid_posted_lines"`
	ClosedDigestFailures       int    `json:"closed_digest_failures"`
	ClosedControlFailures      int    `json:"closed_control_failures"`
	InvalidReconciliations     int    `json:"invalid_reconciliations"`
	InvalidSourceRevisions     int    `json:"invalid_source_revisions"`
	InvalidSourceEvidence      int    `json:"invalid_source_evidence"`
	InvalidMaterializations    int    `json:"invalid_source_materializations"`
	InvalidSourceLinks         int    `json:"invalid_source_links"`
	InvalidStatementLifecycles int    `json:"invalid_statement_lifecycles"`
	AuditEvents                int64  `json:"audit_events"`
	OK                         bool   `json:"ok"`
}

type rowQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func ComputeBookDigest(ctx context.Context, q rowQueryer, bookID, endDate string) (string, error) {
	rows, err := q.QueryContext(ctx, `SELECT jl.account_id, SUM(jl.debit_cents), SUM(jl.credit_cents)
		FROM journal_entries je JOIN journal_lines jl ON jl.journal_entry_id = je.id
		WHERE je.book_id = ? AND je.status = 'POSTED' AND je.posting_date <= ?
		GROUP BY jl.account_id ORDER BY jl.account_id`, bookID, endDate)
	if err != nil {
		return "", MapError("compute period digest", err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(rows)
	hash := sha256.New()
	for rows.Next() {
		var accountID string
		var debits, credits int64
		if err := rows.Scan(&accountID, &debits, &credits); err != nil {
			return "", err
		}
		_, _ = fmt.Fprintf(hash, "%s\x1f%d\x1f%d\n", accountID, debits, credits)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (s *Store) Doctor(ctx context.Context) (DoctorResult, error) {
	result := DoctorResult{}
	if err := s.VerifySchema(ctx); err != nil {
		return result, err
	}
	rows, err := s.db.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return result, MapError("run integrity check", err)
	}
	var messages []string
	for rows.Next() {
		var message string
		if err := rows.Scan(&message); err != nil {
			_ = rows.Close()
			return result, err
		}
		messages = append(messages, message)
	}
	if err := rows.Close(); err != nil {
		return result, MapError("close integrity-check rows", err)
	}
	result.IntegrityCheck = strings.Join(messages, "; ")
	if result.IntegrityCheck != "ok" {
		return result, apperr.New(apperr.Integrity, "SQLITE_INTEGRITY_FAILED", "SQLite integrity check failed: "+result.IntegrityCheck)
	}
	rows, err = s.db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return result, MapError("run foreign key check", err)
	}
	for rows.Next() {
		result.ForeignKeyViolations++
	}
	if err := rows.Close(); err != nil {
		return result, MapError("close foreign-key-check rows", err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM (
        SELECT je.id FROM journal_entries je JOIN journal_lines jl ON jl.journal_entry_id = je.id
        WHERE je.status = 'POSTED' GROUP BY je.id
        HAVING COUNT(*) < 2 OR SUM(jl.debit_cents) = 0 OR SUM(jl.debit_cents) <> SUM(jl.credit_cents)
    )`).Scan(&result.UnbalancedJournals); err != nil {
		return result, MapError("check posted journal balances", err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM journal_entries je
        JOIN journal_lines jl ON jl.journal_entry_id = je.id
        LEFT JOIN fiscal_periods fp ON fp.id = je.period_id
        LEFT JOIN book_accounts ba ON ba.book_id = je.book_id AND ba.account_id = jl.account_id
        WHERE je.status = 'POSTED' AND (
          fp.id IS NULL OR je.posting_date NOT BETWEEN fp.start_date AND fp.end_date OR
          ba.account_id IS NULL OR je.posting_date < ba.active_from OR
          (ba.active_to IS NOT NULL AND je.posting_date > ba.active_to)
        )`).Scan(&result.InvalidPostedLines); err != nil {
		return result, MapError("check posted journal references", err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*)
        FROM reconciliations r
        JOIN reconciliation_status status ON status.reconciliation_id = r.id
        WHERE r.status = 'COMPLETED'
          AND (
            r.beginning_balance_cents + status.statement_activity_cents <> r.ending_balance_cents OR
            status.ledger_beginning_balance_cents <> r.beginning_balance_cents OR
            status.ledger_ending_balance_cents <> r.ending_balance_cents OR
            status.fully_allocated_statement_count <> status.statement_transaction_count OR
            status.fully_allocated_control_line_count <> status.control_line_count OR
			status.outstanding_mismatch_count <> 0 OR
            EXISTS (
              SELECT 1
              FROM reconciliations overlapping
              WHERE overlapping.id <> r.id
                AND overlapping.statement_account_id = r.statement_account_id
				AND overlapping.status <> 'ABANDONED'
                AND overlapping.end_date >= r.start_date
                AND r.end_date >= overlapping.start_date
            ) OR
            EXISTS (
              SELECT 1
              FROM reconciliations prior
              WHERE prior.statement_account_id = r.statement_account_id
				AND prior.status <> 'ABANDONED'
                AND prior.end_date < r.start_date
                AND prior.end_date = (
                  SELECT MAX(candidate.end_date)
                  FROM reconciliations candidate
                  WHERE candidate.statement_account_id = r.statement_account_id
					AND candidate.status <> 'ABANDONED'
                    AND candidate.end_date < r.start_date
                )
                AND (prior.status <> 'COMPLETED' OR
                     r.start_date <> date(prior.end_date, '+1 day') OR
                     r.beginning_balance_cents <> prior.ending_balance_cents)
            ) OR
            EXISTS (
              SELECT 1
              FROM reconciliation_allocations ri
              LEFT JOIN statement_transactions st ON st.id = ri.statement_transaction_id
              LEFT JOIN statement_accounts sa ON sa.id = r.statement_account_id
              LEFT JOIN journal_lines jl ON jl.id = ri.journal_line_id
              LEFT JOIN journal_entries je ON je.id = jl.journal_entry_id
              WHERE ri.reconciliation_id = r.id
                AND (st.id IS NULL OR st.statement_account_id <> r.statement_account_id OR
                     st.posted_date NOT BETWEEN r.start_date AND r.end_date OR
					 je.id IS NULL OR je.book_id <> sa.book_id OR je.status <> 'POSTED' OR
					 je.posting_date NOT BETWEEN MIN(sa.reconciliation_required_from, r.start_date) AND r.end_date OR
                     jl.account_id <> sa.gl_account_id OR
                     NOT ((ri.allocated_amount_cents > 0 AND st.amount_cents > 0 AND
                           (jl.debit_cents - jl.credit_cents) > 0) OR
                          (ri.allocated_amount_cents < 0 AND st.amount_cents < 0 AND
                           (jl.debit_cents - jl.credit_cents) < 0)))
            )
          )`).Scan(&result.InvalidReconciliations); err != nil {
		return result, MapError("check completed reconciliations", err)
	}
	if err := s.db.QueryRowContext(ctx, `WITH required AS (
		SELECT bp.book_id, bp.period_id, sa.id AS statement_account_id,
			MAX(sa.reconciliation_required_from, fp.start_date) AS required_start,
			MIN(COALESCE(sa.reconciliation_required_through, fp.end_date), fp.end_date) AS required_end
		FROM book_periods bp
		JOIN fiscal_periods fp ON fp.id = bp.period_id
		JOIN statement_accounts sa ON sa.book_id = bp.book_id
		WHERE bp.status = 'CLOSED' AND sa.required_for_close = 1
		  AND sa.reconciliation_required_from <= fp.end_date
		  AND COALESCE(sa.reconciliation_required_through, fp.end_date) >= fp.start_date
		  AND NOT EXISTS (
		      SELECT 1 FROM valid_statement_account_precoverage_closures valid
		      WHERE valid.statement_account_id = sa.id
		  )
	)
	SELECT COUNT(*) FROM required
	WHERE COALESCE((
		SELECT SUM(CAST(
			julianday(MIN(r.end_date, required.required_end)) -
			julianday(MAX(r.start_date, required.required_start)) + 1
			AS INTEGER))
		FROM reconciliations r
		JOIN reconciliation_status status ON status.reconciliation_id = r.id
		WHERE r.statement_account_id = required.statement_account_id
		  AND r.status = 'COMPLETED'
		  AND r.beginning_balance_cents + status.statement_activity_cents = r.ending_balance_cents
		  AND status.ledger_beginning_balance_cents = r.beginning_balance_cents
		  AND status.ledger_ending_balance_cents = r.ending_balance_cents
		  AND status.fully_allocated_statement_count = status.statement_transaction_count
		  AND status.fully_allocated_control_line_count = status.control_line_count
		  AND status.outstanding_mismatch_count = 0
		  AND r.end_date >= required.required_start
		  AND r.start_date <= required.required_end
	), 0) <> CAST(julianday(required.required_end) - julianday(required.required_start) + 1 AS INTEGER)`).Scan(&result.ClosedControlFailures); err != nil {
		return result, MapError("check closed-period statement controls", err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM (
		SELECT 'I:' || si.id AS invalid_revision
		FROM source_identities si
		LEFT JOIN source_records sr ON sr.source_identity_id = si.id
		GROUP BY si.id
		HAVING COUNT(sr.id) = 0
		   OR SUM(CASE WHEN sr.revision = 1 AND sr.supersedes_source_record_id IS NULL THEN 1 ELSE 0 END) <> 1
		   OR SUM(CASE WHEN NOT EXISTS (
		       SELECT 1 FROM source_records successor
		       WHERE successor.supersedes_source_record_id = sr.id
		   ) THEN 1 ELSE 0 END) <> 1
		   OR COUNT(sr.id) <> MAX(sr.revision)
		UNION ALL
		SELECT 'X:' || si.id
		FROM source_identities si
		JOIN books b ON b.id = si.book_id
		LEFT JOIN statement_accounts sa ON sa.id = si.statement_account_id
		WHERE b.entity_id IS NOT si.entity_id
		   OR (si.materialization_kind = 'STATEMENT' AND (
		       sa.id IS NULL OR sa.code <> si.source_account OR
		       sa.book_id <> si.book_id OR sa.entity_id <> si.entity_id
		   ))
		   OR (si.materialization_kind = 'JOURNAL' AND (
		       si.statement_account_id IS NOT NULL OR b.code <> si.source_account
		   ))
		UNION ALL
		SELECT 'R:' || sr.id
		FROM source_records sr
		LEFT JOIN source_records predecessor ON predecessor.id = sr.supersedes_source_record_id
		WHERE (sr.revision = 1 AND (sr.supersedes_source_record_id IS NOT NULL OR sr.observation_kind <> 'PROVIDER'))
		   OR (sr.revision > 1 AND (
		       predecessor.id IS NULL OR predecessor.source_identity_id <> sr.source_identity_id OR
		       predecessor.revision + 1 <> sr.revision
		   ))
		   OR EXISTS (
		       SELECT 1 FROM source_records successor
		       WHERE successor.supersedes_source_record_id = sr.id AND sr.disposition = 'POSTED'
		   )
		   OR (sr.revision > 1 AND sr.observation_kind = 'RESOLUTION' AND predecessor.disposition = sr.disposition)
		   OR (sr.revision > 1 AND sr.observation_kind = 'PROVIDER' AND NOT (
		       (predecessor.disposition = 'PENDING' AND sr.disposition IN ('PENDING', 'NEEDS_REVIEW', 'POSTED')) OR
		       (predecessor.disposition = 'NEEDS_REVIEW' AND sr.disposition = 'NEEDS_REVIEW') OR
		       (predecessor.disposition = 'SOURCE_ONLY' AND sr.disposition = 'SOURCE_ONLY')
		   ))
		)`).Scan(&result.InvalidSourceRevisions); err != nil {
		return result, MapError("check source revision chains", err)
	}
	sourceRows, err := s.db.QueryContext(ctx, `SELECT payload_sha256, raw_json,
		resolution_evidence_json, resolution_evidence_sha256
		FROM source_records ORDER BY id`)
	if err != nil {
		return result, MapError("read source evidence", err)
	}
	for sourceRows.Next() {
		var payloadSHA, rawJSON string
		var resolutionJSON, resolutionSHA sql.NullString
		if err := sourceRows.Scan(&payloadSHA, &rawJSON, &resolutionJSON, &resolutionSHA); err != nil {
			_ = sourceRows.Close()
			return result, MapError("read source evidence", err)
		}
		payloadDigest := sha256.Sum256([]byte(rawJSON))
		invalid := payloadSHA != hex.EncodeToString(payloadDigest[:])
		if resolutionJSON.Valid != resolutionSHA.Valid {
			invalid = true
		} else if resolutionJSON.Valid {
			resolutionDigest := sha256.Sum256([]byte(resolutionJSON.String))
			invalid = invalid || resolutionSHA.String != hex.EncodeToString(resolutionDigest[:])
		}
		if invalid {
			result.InvalidSourceEvidence++
		}
	}
	if err := sourceRows.Err(); err != nil {
		_ = sourceRows.Close()
		return result, MapError("read source evidence", err)
	}
	if err := sourceRows.Close(); err != nil {
		return result, MapError("close source evidence rows", err)
	}
	var invalidSourceProvenance int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*)
		FROM source_records source_row
		JOIN source_identities identity_row ON identity_row.id = source_row.source_identity_id
		LEFT JOIN source_record_operator_attestations attestation
		  ON attestation.source_record_id = source_row.id
		WHERE (identity_row.source_system = 'MANUAL_RECONCILIATION' AND (
		         attestation.source_record_id IS NULL OR
		         attestation.attested_at <> source_row.created_at OR
		         attestation.attested_by <> source_row.created_by
		      )) OR
		      (identity_row.source_system <> 'MANUAL_RECONCILIATION' AND
		       attestation.source_record_id IS NOT NULL)`).Scan(&invalidSourceProvenance); err != nil {
		return result, MapError("check source evidence provenance", err)
	}
	result.InvalidSourceEvidence += invalidSourceProvenance
	var invalidManualEvidence int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*)
		FROM source_records source_row
		JOIN source_identities identity_row ON identity_row.id = source_row.source_identity_id
		JOIN import_batches batch ON batch.id = source_row.import_batch_id
		LEFT JOIN statement_transactions statement_row
		  ON statement_row.source_record_id = source_row.id
		 AND statement_row.source_identity_id = identity_row.id
		WHERE identity_row.source_system = 'MANUAL_RECONCILIATION'
		  AND (
		    source_row.observation_kind <> 'PROVIDER' OR
		    source_row.disposition <> 'POSTED' OR
		    source_row.revision <> 1 OR source_row.supersedes_source_record_id IS NOT NULL OR
		    identity_row.materialization_kind <> 'STATEMENT' OR
		    batch.source_system <> 'MANUAL_RECONCILIATION' OR batch.status <> 'COMPLETED' OR
		    length(batch.file_sha256) <> 64 OR batch.file_sha256 <> lower(batch.file_sha256) OR
		    batch.file_sha256 GLOB '*[^0-9a-f]*' OR
		    statement_row.id IS NULL OR
		    (SELECT COUNT(*) FROM reconciliation_allocations allocation
		     WHERE allocation.statement_transaction_id = statement_row.id) <> 1 OR
		    NOT EXISTS (
		      SELECT 1
		      FROM reconciliation_allocations allocation
		      JOIN reconciliations reconciliation ON reconciliation.id = allocation.reconciliation_id
		      JOIN statement_accounts statement_account
		        ON statement_account.id = reconciliation.statement_account_id
		      JOIN journal_lines journal_line ON journal_line.id = allocation.journal_line_id
		      JOIN journal_entries journal ON journal.id = journal_line.journal_entry_id
		      WHERE allocation.statement_transaction_id = statement_row.id
		        AND reconciliation.statement_account_id = identity_row.statement_account_id
		        AND statement_row.statement_account_id = identity_row.statement_account_id
		        AND journal.book_id = identity_row.book_id
		        AND journal_line.account_id = statement_account.gl_account_id
		        AND allocation.allocated_amount_cents = source_row.amount_cents
		        AND (journal_line.debit_cents - journal_line.credit_cents) = source_row.amount_cents
		        AND identity_row.external_id = printf(
		          'reconcile:%s:%d:%d', lower(identity_row.source_account),
		          journal.entry_number, journal_line.line_number
		        )
		        AND json_type(source_row.raw_json) = 'object'
		        AND (SELECT COUNT(*) FROM json_each(source_row.raw_json)) = 6
		        AND json_type(source_row.raw_json, '$.plan_digest') = 'text'
		        AND json_extract(source_row.raw_json, '$.plan_digest') = batch.file_sha256
		        AND json_type(source_row.raw_json, '$.transaction_number') = 'integer'
		        AND json_extract(source_row.raw_json, '$.transaction_number') = journal.entry_number
		        AND json_type(source_row.raw_json, '$.line_number') = 'integer'
		        AND json_extract(source_row.raw_json, '$.line_number') = journal_line.line_number
		        AND json_type(source_row.raw_json, '$.ledger_date') = 'text'
		        AND json_extract(source_row.raw_json, '$.ledger_date') = journal.posting_date
		        AND json_type(source_row.raw_json, '$.statement_date') = 'text'
		        AND json_extract(source_row.raw_json, '$.statement_date') = statement_row.posted_date
		        AND json_type(source_row.raw_json, '$.provenance') = 'text'
		        AND json_extract(source_row.raw_json, '$.provenance') = 'OPERATOR_ATTESTATION'
		    )
		  )`).Scan(&invalidManualEvidence); err != nil {
		return result, MapError("check manual reconciliation evidence binding", err)
	}
	result.InvalidSourceEvidence += invalidManualEvidence
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*)
		FROM current_source_records current
		WHERE (current.materialization_kind = 'STATEMENT' AND (
		    (current.disposition = 'POSTED') <>
		    ((SELECT COUNT(*) FROM statement_transactions st WHERE st.source_identity_id = current.source_identity_id) = 1)
		)) OR (current.materialization_kind = 'JOURNAL' AND (
		    (current.disposition = 'POSTED') <>
		    ((SELECT COUNT(*) FROM source_record_journals srj
		      WHERE srj.source_record_id = current.id AND srj.link_role = 'PRIMARY') = 1)
		))`).Scan(&result.InvalidMaterializations); err != nil {
		return result, MapError("check current source materializations", err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM (
	        SELECT 'J:' || srj.source_record_id || ':' || srj.journal_entry_id AS invalid_link
	        FROM source_record_journals srj
	        JOIN source_records sr ON sr.id = srj.source_record_id
	        JOIN source_identities si ON si.id = sr.source_identity_id
	        JOIN journal_entries je ON je.id = srj.journal_entry_id
	        JOIN books b ON b.id = je.book_id
	        WHERE sr.disposition <> 'POSTED' OR
	              EXISTS (SELECT 1 FROM source_records successor WHERE successor.supersedes_source_record_id = sr.id) OR
	              (si.entity_id IS NOT NULL AND b.entity_id IS NOT NULL AND si.entity_id <> b.entity_id
	               AND b.kind <> 'ELIMINATION' AND srj.link_role NOT IN ('MIRROR', 'ELIMINATION')) OR
	              (srj.link_role = 'EVIDENCE' AND je.book_id <> si.book_id) OR
	              (srj.link_role = 'PRIMARY' AND (
	               si.materialization_kind <> 'JOURNAL' OR je.book_id <> si.book_id OR
	               je.source_system <> si.source_system OR je.source_key <> si.external_id))
	        UNION ALL
	        SELECT 'T:' || st.id
	        FROM statement_transactions st
	        JOIN source_records sr ON sr.id = st.source_record_id
	        JOIN source_identities si ON si.id = sr.source_identity_id
	        JOIN statement_accounts sa ON sa.id = st.statement_account_id
	        WHERE sr.disposition <> 'POSTED' OR si.id <> st.source_identity_id OR
	              si.materialization_kind <> 'STATEMENT' OR si.statement_account_id <> sa.id OR
	              si.source_account <> sa.code OR
	              si.entity_id <> sa.entity_id OR si.book_id <> sa.book_id OR
	              sr.transaction_date <> st.posted_date OR sr.description <> st.description OR
	              sr.amount_cents <> st.amount_cents OR
	              EXISTS (SELECT 1 FROM source_records successor WHERE successor.supersedes_source_record_id = sr.id)
		    )`).Scan(&result.InvalidSourceLinks); err != nil {
		return result, MapError("check source materialization links", err)
	}
	lifecycleRows, err := s.db.QueryContext(ctx, `SELECT closure.id, closure.statement_account_id,
		COALESCE(binding.active_identity_count, 0), COALESCE(binding.active_identity_digest, ''),
		valid.id IS NOT NULL
		FROM statement_account_precoverage_closures closure
		LEFT JOIN statement_account_precoverage_identity_bindings binding ON binding.closure_id = closure.id
		LEFT JOIN valid_statement_account_precoverage_closures valid ON valid.id = closure.id
		ORDER BY closure.id`)
	if err != nil {
		return result, MapError("read statement-account lifecycle evidence", err)
	}
	type lifecycleBinding struct {
		closureID, statementAccountID, digest string
		count                                 int
		structurallyValid                     bool
	}
	var lifecycleBindings []lifecycleBinding
	for lifecycleRows.Next() {
		var binding lifecycleBinding
		if err := lifecycleRows.Scan(&binding.closureID, &binding.statementAccountID, &binding.count, &binding.digest, &binding.structurallyValid); err != nil {
			_ = lifecycleRows.Close()
			return result, MapError("scan statement-account lifecycle evidence", err)
		}
		lifecycleBindings = append(lifecycleBindings, binding)
	}
	if err := lifecycleRows.Err(); err != nil {
		_ = lifecycleRows.Close()
		return result, MapError("read statement-account lifecycle evidence", err)
	}
	if err := lifecycleRows.Close(); err != nil {
		return result, MapError("close statement-account lifecycle evidence", err)
	}
	invalidLifecycles := make(map[string]struct{})
	for _, binding := range lifecycleBindings {
		if !binding.structurallyValid {
			invalidLifecycles[binding.closureID] = struct{}{}
		}
		activeCount, activeDigest, err := ActiveStatementAccountIdentityDigest(ctx, s.db, binding.statementAccountID)
		if err != nil {
			return result, err
		}
		if activeCount != binding.count || activeDigest != binding.digest {
			invalidLifecycles[binding.closureID] = struct{}{}
		}
	}
	result.InvalidStatementLifecycles = len(invalidLifecycles)
	closedRows, err := s.db.QueryContext(ctx, `SELECT bp.book_id, fp.end_date, bp.close_digest
        FROM book_periods bp JOIN fiscal_periods fp ON fp.id = bp.period_id WHERE bp.status = 'CLOSED'`)
	if err != nil {
		return result, MapError("read closed periods", err)
	}
	type closedPeriod struct{ bookID, endDate, digest string }
	var closed []closedPeriod
	for closedRows.Next() {
		var period closedPeriod
		if err := closedRows.Scan(&period.bookID, &period.endDate, &period.digest); err != nil {
			_ = closedRows.Close()
			return result, err
		}
		closed = append(closed, period)
	}
	if err := closedRows.Close(); err != nil {
		return result, MapError("close closed-period rows", err)
	}
	for _, period := range closed {
		digest, err := ComputeBookDigest(ctx, s.db, period.bookID, period.endDate)
		if err != nil {
			return result, err
		}
		if digest != period.digest {
			result.ClosedDigestFailures++
		}
	}
	result.AuditEvents, err = VerifyAudit(ctx, s.db)
	if err != nil {
		return result, err
	}
	result.OK = result.ForeignKeyViolations == 0 && result.UnbalancedJournals == 0 && result.InvalidPostedLines == 0 && result.ClosedDigestFailures == 0 && result.ClosedControlFailures == 0 && result.InvalidReconciliations == 0 && result.InvalidSourceRevisions == 0 && result.InvalidSourceEvidence == 0 && result.InvalidMaterializations == 0 && result.InvalidSourceLinks == 0 && result.InvalidStatementLifecycles == 0
	if !result.OK {
		return result, apperr.New(apperr.Integrity, "DATABASE_INVARIANT_FAILED", "one or more accounting database invariants failed")
	}
	return result, nil
}
