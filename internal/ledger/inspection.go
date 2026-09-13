package ledger

import (
	"context"
	"github.com/dispatchlabs-ai/books/internal/money"
	"strings"
)

type ConsolidationGroup struct {
	ID                string `json:"id"`
	Code              string `json:"code"`
	Name              string `json:"name"`
	Parent            string `json:"parent"`
	Currency          string `json:"currency"`
	EliminationBookID string `json:"elimination_book_id"`
	EliminationBook   string `json:"elimination_book"`
}

func (s *Service) ListGroups(ctx context.Context) ([]ConsolidationGroup, error) {
	rowsDB, err := s.store.DB().QueryContext(ctx, `SELECT g.id, g.code, g.name, e.code, g.currency,
                COALESCE(b.id, ''), COALESCE(b.code, '') FROM consolidation_groups g
                JOIN entities e ON e.id = g.parent_entity_id
                LEFT JOIN books b ON b.group_id = g.id AND b.kind = 'ELIMINATION' AND b.status = 'ACTIVE'
                ORDER BY g.code`)
	if err != nil {
		return nil, err
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(rowsDB)

	var data []ConsolidationGroup
	for rowsDB.Next() {
		var value ConsolidationGroup
		if err := rowsDB.Scan(&value.ID, &value.Code, &value.Name, &value.Parent, &value.Currency, &value.EliminationBookID, &value.EliminationBook); err != nil {
			return nil, err
		}
		data = append(data, value)

	}
	return data, rowsDB.Err()
}

type OwnershipInterest struct {
	ID, Parent, Child, From, To string
	OwnershipBPS                int `json:"ownership_bps"`
}

func (s *Service) ListOwnership(ctx context.Context) ([]OwnershipInterest, error) {
	rowsDB, err := s.store.DB().QueryContext(ctx, `SELECT oi.id, p.code, c.code, oi.ownership_bps,
                oi.effective_from, COALESCE(oi.effective_to, '') FROM ownership_interests oi
                JOIN entities p ON p.id = oi.parent_entity_id JOIN entities c ON c.id = oi.child_entity_id
                ORDER BY oi.effective_from, p.code, c.code`)
	if err != nil {
		return nil, err
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(rowsDB)

	var data []OwnershipInterest
	for rowsDB.Next() {
		var r OwnershipInterest
		if err := rowsDB.Scan(&r.ID, &r.Parent, &r.Child, &r.OwnershipBPS, &r.From, &r.To); err != nil {
			return nil, err
		}
		data = append(data, r)

	}
	return data, rowsDB.Err()
}

type StatementTransactionSummary struct {
	Currency         money.Currency `json:"currency"`
	ID               string         `json:"id"`
	StatementAccount string         `json:"statement_account"`
	ExternalID       string         `json:"external_id"`
	PostedDate       string         `json:"posted_date"`
	Description      string         `json:"description"`
	AmountCents      int64          `json:"amount_cents"`
	Disposition      string         `json:"disposition"`
	ExclusionReason  string         `json:"exclusion_reason,omitempty"`
	AllocatedCents   int64          `json:"allocated_cents"`
	RemainingCents   int64          `json:"remaining_cents"`
	AllocationCount  int            `json:"allocation_count"`
}

func (s *Service) ListStatementTransactions(ctx context.Context, account, from, to string, unallocated bool) ([]StatementTransactionSummary, error) {
	query := `SELECT st.id, sa.code, sa.currency, si.external_id, st.posted_date, st.description, st.amount_cents,
	                COALESCE(sr.disposition, ''), COALESCE(sr.exclusion_reason, ''),
	                COALESCE(SUM(ri.allocated_amount_cents), 0), COUNT(ri.id) FROM statement_transactions st
	                JOIN statement_accounts sa ON sa.id = st.statement_account_id
	                JOIN source_identities si ON si.id = st.source_identity_id
	                LEFT JOIN source_records sr ON sr.id = st.source_record_id
                LEFT JOIN reconciliation_allocations ri ON ri.statement_transaction_id = st.id WHERE 1=1`
	var queryArgs []any
	if account != "" {
		query += " AND sa.code = ?"
		queryArgs = append(queryArgs, strings.ToUpper(account))
	}
	if from != "" {
		query += " AND st.posted_date >= ?"
		queryArgs = append(queryArgs, from)
	}
	if to != "" {
		query += " AND st.posted_date <= ?"
		queryArgs = append(queryArgs, to)
	}
	query += ` GROUP BY st.id, sa.code, si.external_id, st.posted_date, st.description, st.amount_cents, sr.disposition, sr.exclusion_reason`
	if unallocated {
		query += " HAVING COALESCE(SUM(ri.allocated_amount_cents), 0) <> st.amount_cents"
	}
	query += " ORDER BY st.posted_date, sa.code, si.external_id"
	rowsDB, err := s.store.DB().QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, err
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(rowsDB)

	var data []StatementTransactionSummary
	for rowsDB.Next() {
		var value StatementTransactionSummary
		if err := rowsDB.Scan(&value.ID, &value.StatementAccount, &value.Currency, &value.ExternalID, &value.PostedDate, &value.Description, &value.AmountCents, &value.Disposition, &value.ExclusionReason, &value.AllocatedCents, &value.AllocationCount); err != nil {
			return nil, err
		}
		value.RemainingCents = value.AmountCents - value.AllocatedCents
		data = append(data, value)

	}
	return data, rowsDB.Err()
}
