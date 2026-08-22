package ledger

type Entity struct {
	ID                 string `json:"id"`
	Code               string `json:"code"`
	LegalName          string `json:"legal_name"`
	FunctionalCurrency string `json:"functional_currency"`
	Status             string `json:"status"`
	BookID             string `json:"book_id,omitempty"`
	BookCode           string `json:"book_code,omitempty"`
}

// Book is an actual or elimination ledger inside the database.
type Book struct {
	ID              string `json:"id"`
	Code            string `json:"code"`
	Name            string `json:"name"`
	Kind            string `json:"kind"`
	EntityCode      string `json:"entity,omitempty"`
	GroupCode       string `json:"group,omitempty"`
	AccountingBasis string `json:"accounting_basis"`
	Currency        string `json:"currency"`
	Status          string `json:"status"`
	CreatedAt       string `json:"created_at"`
}

type CreateEntityInput struct {
	Code      string
	LegalName string
	Currency  string
	BookCode  string
	BookName  string
	Basis     string
}

type Group struct {
	ID                  string `json:"id"`
	Code                string `json:"code"`
	Name                string `json:"name"`
	ParentEntityID      string `json:"parent_entity_id"`
	Currency            string `json:"currency"`
	EliminationBookID   string `json:"elimination_book_id,omitempty"`
	EliminationBookCode string `json:"elimination_book_code,omitempty"`
}

type CreateGroupInput struct {
	Code                string
	Name                string
	ParentEntity        string
	EliminationBookCode string
	EliminationBookName string
}

type Account struct {
	ID               string `json:"id"`
	Code             string `json:"code"`
	Name             string `json:"name"`
	Type             string `json:"type"`
	Subtype          string `json:"subtype"`
	NormalBalance    string `json:"normal_balance"`
	StatementSection string `json:"statement_section"`
	BookCode         string `json:"book,omitempty"`
	PostingEnabled   *bool  `json:"posting_enabled,omitempty"`
	ActiveFrom       string `json:"active_from,omitempty"`
	ActiveTo         string `json:"active_to,omitempty"`
}

type CreateAccountInput struct {
	Code             string
	Name             string
	Type             string
	Subtype          string
	NormalBalance    string
	StatementSection string
	BookCodes        []string
	ActiveFrom       string
}

type AccountIdentityEvidence struct {
	SourceKind    string `json:"source_kind"`
	SourcePath    string `json:"source_path"`
	SourceSHA256  string `json:"source_sha256"`
	Locator       string `json:"locator"`
	PayloadSHA256 string `json:"payload_sha256,omitempty"`
}

type AddAccountIdentityInput struct {
	Entity        string                  `json:"entity"`
	Account       string                  `json:"account"`
	SourceSystem  string                  `json:"source_system"`
	ExternalID    string                  `json:"external_id"`
	AccountNumber string                  `json:"account_number,omitempty"`
	Name          string                  `json:"name"`
	Active        bool                    `json:"active"`
	Evidence      AccountIdentityEvidence `json:"evidence"`
}

type AccountIdentity struct {
	ID            string                  `json:"id"`
	EntityID      string                  `json:"entity_id"`
	EntityCode    string                  `json:"entity_code"`
	AccountID     string                  `json:"account_id"`
	AccountCode   string                  `json:"account_code"`
	SourceSystem  string                  `json:"source_system"`
	ExternalID    string                  `json:"external_id"`
	AccountNumber string                  `json:"account_number,omitempty"`
	Name          string                  `json:"name"`
	Active        bool                    `json:"active"`
	Evidence      AccountIdentityEvidence `json:"evidence"`
	CreatedAt     string                  `json:"created_at"`
}

type AccountIdentityFilter struct {
	Entity       string
	Account      string
	SourceSystem string
}

type Period struct {
	ID           string `json:"id"`
	Code         string `json:"code"`
	StartDate    string `json:"start_date"`
	EndDate      string `json:"end_date"`
	FiscalYear   int    `json:"fiscal_year"`
	PeriodNumber int    `json:"period_number"`
	YearEnd      bool   `json:"year_end"`
	BookCode     string `json:"book,omitempty"`
	BookStatus   string `json:"book_status,omitempty"`
	CloseDigest  string `json:"close_digest,omitempty"`
}

type CreatePeriodInput struct {
	Code         string
	StartDate    string
	EndDate      string
	FiscalYear   int
	PeriodNumber int
	YearEnd      bool
}

type JournalLineInput struct {
	Account            string `json:"account"`
	Description        string `json:"description,omitempty"`
	DebitCents         int64  `json:"debit_cents"`
	CreditCents        int64  `json:"credit_cents"`
	CounterpartyEntity string `json:"counterparty_entity,omitempty"`
	IntercompanyKey    string `json:"intercompany_key,omitempty"`
}

type CreateJournalInput struct {
	Book                string             `json:"book"`
	Kind                string             `json:"kind,omitempty"`
	PostingDate         string             `json:"posting_date"`
	Period              string             `json:"period"`
	Description         string             `json:"description"`
	Reference           string             `json:"reference,omitempty"`
	SourceSystem        string             `json:"source_system,omitempty"`
	SourceKey           string             `json:"source_key,omitempty"`
	TaxType             string             `json:"tax_type,omitempty"`
	TaxAccountingPeriod string             `json:"tax_accounting_period,omitempty"`
	ReversalOfID        string             `json:"reversal_of_id,omitempty"`
	Lines               []JournalLineInput `json:"lines"`
}

type Journal struct {
	ID                  string        `json:"id"`
	BookID              string        `json:"book_id"`
	BookCode            string        `json:"book_code"`
	Kind                string        `json:"kind"`
	EntryNumber         int64         `json:"entry_number"`
	PostingDate         string        `json:"posting_date"`
	PeriodCode          string        `json:"period"`
	Status              string        `json:"status"`
	Description         string        `json:"description"`
	Reference           string        `json:"reference,omitempty"`
	SourceSystem        string        `json:"source_system,omitempty"`
	SourceKey           string        `json:"source_key,omitempty"`
	TaxType             string        `json:"tax_type,omitempty"`
	TaxAccountingPeriod string        `json:"tax_accounting_period,omitempty"`
	ReversalOfID        string        `json:"reversal_of_id,omitempty"`
	CreatedAt           string        `json:"created_at"`
	PostedAt            string        `json:"posted_at,omitempty"`
	Lines               []JournalLine `json:"lines"`
	TotalDebitCents     int64         `json:"total_debit_cents"`
	TotalCreditCents    int64         `json:"total_credit_cents"`
}

type JournalLine struct {
	ID                 string `json:"id"`
	LineNumber         int    `json:"line_number"`
	AccountID          string `json:"account_id"`
	AccountCode        string `json:"account_code"`
	AccountName        string `json:"account_name"`
	Description        string `json:"description"`
	DebitCents         int64  `json:"debit_cents"`
	CreditCents        int64  `json:"credit_cents"`
	CounterpartyEntity string `json:"counterparty_entity,omitempty"`
	IntercompanyKey    string `json:"intercompany_key,omitempty"`
}

type JournalValidation struct {
	JournalID   string   `json:"journal_id"`
	Valid       bool     `json:"valid"`
	Errors      []string `json:"errors"`
	DebitCents  int64    `json:"debit_cents"`
	CreditCents int64    `json:"credit_cents"`
}
