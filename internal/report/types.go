// Package report derives exact-cent financial reports from posted journal lines.
package report

// Scope identifies exactly one legal entity or consolidation group by code.
type Scope struct {
	EntityCode string `json:"entity,omitempty"`
	GroupCode  string `json:"group,omitempty"`
}

// ResolvedScope is the immutable set of books used for a report. Range reports
// include every ownership-derived perimeter whose dates overlap the range.
type ResolvedScope struct {
	Kind     string       `json:"kind"`
	ID       string       `json:"id"`
	Code     string       `json:"code"`
	Name     string       `json:"name"`
	Currency string       `json:"currency"`
	Basis    string       `json:"basis"`
	AsOfDate string       `json:"as_of_date"`
	Books    []ScopedBook `json:"books"`
}

// ConsolidationInterval limits an owned entity's contribution to a group range
// report. Empty EffectiveTo means that the ownership path remains open.
type ConsolidationInterval struct {
	EffectiveFrom string `json:"effective_from"`
	EffectiveTo   string `json:"effective_to,omitempty"`
}

// ScopedBook describes an actual or elimination book included in a report scope.
type ScopedBook struct {
	BookID                 string                  `json:"book_id"`
	BookCode               string                  `json:"book_code"`
	BookName               string                  `json:"book_name"`
	Kind                   string                  `json:"kind"`
	Basis                  string                  `json:"basis"`
	EntityID               string                  `json:"entity_id,omitempty"`
	EntityCode             string                  `json:"entity_code,omitempty"`
	EntityName             string                  `json:"entity_name,omitempty"`
	ConsolidationIntervals []ConsolidationInterval `json:"consolidation_intervals,omitempty"`
}

// EntityAmount is one legal entity's contribution to an amount.
type EntityAmount struct {
	EntityID   string `json:"entity_id"`
	EntityCode string `json:"entity_code"`
	EntityName string `json:"entity_name"`
	Cents      int64  `json:"cents"`
}

// Breakdown keeps legal-entity, elimination, and consolidated values visible.
// For general-ledger and trial-balance balances, positive cents are debits and
// negative cents are credits. Financial-statement rows use their natural display
// sign, documented by the containing report type.
type Breakdown struct {
	ByEntity          []EntityAmount `json:"by_entity"`
	EliminationsCents int64          `json:"eliminations_cents"`
	ConsolidatedCents int64          `json:"consolidated_cents"`
}

// Account identifies the immutable reporting classification of a ledger account.
type Account struct {
	ID               string `json:"id"`
	Code             string `json:"code"`
	Name             string `json:"name"`
	Type             string `json:"type"`
	Subtype          string `json:"subtype,omitempty"`
	NormalBalance    string `json:"normal_balance"`
	StatementSection string `json:"statement_section,omitempty"`
}

type GeneralLedgerInput struct {
	Scope       Scope
	FromDate    string
	ToDate      string
	AccountCode string
	IncludeZero bool
}

type GeneralLedgerReport struct {
	Scope    ResolvedScope          `json:"scope"`
	FromDate string                 `json:"from_date"`
	ToDate   string                 `json:"to_date"`
	Accounts []GeneralLedgerAccount `json:"accounts"`
}

type GeneralLedgerAccount struct {
	Account        Account             `json:"account"`
	OpeningBalance Breakdown           `json:"opening_balance"`
	Lines          []GeneralLedgerLine `json:"lines"`
	ClosingBalance Breakdown           `json:"closing_balance"`
}

type GeneralLedgerLine struct {
	JournalID           string `json:"journal_id"`
	EntryNumber         int64  `json:"entry_number"`
	PostingDate         string `json:"posting_date"`
	BookCode            string `json:"book_code"`
	BookKind            string `json:"book_kind"`
	EntityCode          string `json:"entity_code,omitempty"`
	Reference           string `json:"reference,omitempty"`
	JournalDescription  string `json:"journal_description"`
	LineNumber          int    `json:"line_number"`
	LineDescription     string `json:"line_description,omitempty"`
	DebitCents          int64  `json:"debit_cents"`
	CreditCents         int64  `json:"credit_cents"`
	ChangeCents         int64  `json:"change_cents"`
	RunningBalanceCents int64  `json:"running_balance_cents"`
	Synthetic           bool   `json:"synthetic,omitempty"`
	SyntheticKind       string `json:"synthetic_kind,omitempty"`
}

type TrialBalanceInput struct {
	Scope       Scope
	AsOfDate    string
	IncludeZero bool
}

type TrialBalanceReport struct {
	Scope            ResolvedScope     `json:"scope"`
	AsOfDate         string            `json:"as_of_date"`
	Rows             []TrialBalanceRow `json:"rows"`
	TotalDebitCents  int64             `json:"total_debit_cents"`
	TotalCreditCents int64             `json:"total_credit_cents"`
}

type TrialBalanceRow struct {
	Account     Account   `json:"account"`
	Balance     Breakdown `json:"balance"`
	DebitCents  int64     `json:"debit_cents"`
	CreditCents int64     `json:"credit_cents"`
}

type ProfitLossInput struct {
	Scope       Scope
	FromDate    string
	ToDate      string
	IncludeZero bool
}

// ProfitLossReport presents revenue and expense in their natural signs:
// credits increase revenue, debits increase expense, and net income is revenue
// less expense.
type ProfitLossReport struct {
	Scope         ResolvedScope  `json:"scope"`
	FromDate      string         `json:"from_date"`
	ToDate        string         `json:"to_date"`
	Revenue       []StatementRow `json:"revenue"`
	Expenses      []StatementRow `json:"expenses"`
	TotalRevenue  Breakdown      `json:"total_revenue"`
	TotalExpenses Breakdown      `json:"total_expenses"`
	NetIncome     Breakdown      `json:"net_income"`
}

type StatementRow struct {
	Account Account   `json:"account"`
	Amount  Breakdown `json:"amount"`
}

type BalanceSheetInput struct {
	Scope       Scope
	AsOfDate    string
	IncludeZero bool
}

// BalanceSheetReport presents assets, liabilities, and equity in their natural
// signs. Current earnings captures unclosed cumulative revenue less expense.
type BalanceSheetReport struct {
	Scope            ResolvedScope  `json:"scope"`
	AsOfDate         string         `json:"as_of_date"`
	Assets           []StatementRow `json:"assets"`
	Liabilities      []StatementRow `json:"liabilities"`
	Equity           []StatementRow `json:"equity"`
	TotalAssets      Breakdown      `json:"total_assets"`
	TotalLiabilities Breakdown      `json:"total_liabilities"`
	PostedEquity     Breakdown      `json:"posted_equity"`
	CurrentEarnings  Breakdown      `json:"current_earnings"`
	TotalEquity      Breakdown      `json:"total_equity"`
}
