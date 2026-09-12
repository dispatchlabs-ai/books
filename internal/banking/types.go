// Package banking parses untrusted statement files without making accounting decisions.
package banking

import "github.com/dispatchlabs-ai/books/internal/apperr"

const (
	MaxBytes         = 8 << 20
	MaxTransactions  = 10000
	MaxTransactionID = 512
	MaxAccounts      = 100
	MaxNodes         = 200000
	MaxDepth         = 32
	MaxText          = 16384
	ParserVersion    = "ofx-bank-card/v1"
)

// Money stays decimal text at the interchange boundary, including stored previews.
type Balance struct {
	Kind   string `json:"kind"`
	Amount string `json:"amount"`
	AsOf   string `json:"as_of"`
}
type Transaction struct {
	Identity    string            `json:"identity,omitempty"`
	Status      string            `json:"status,omitempty"`
	Details     []Detail          `json:"details,omitempty"`
	ID          string            `json:"id"`
	Type        string            `json:"type"`
	PostedDate  string            `json:"posted_date"`
	PostedAt    string            `json:"posted_at"`
	ValueAt     string            `json:"value_at,omitempty"`
	Amount      string            `json:"amount"`
	Description string            `json:"description"`
	Fields      map[string]string `json:"fields"`
}
type Account struct {
	Statements   []Statement   `json:"statements,omitempty"`
	Key          string        `json:"key"`
	Institution  string        `json:"institution"`
	AccountID    string        `json:"account_id"`
	BankID       string        `json:"bank_id,omitempty"`
	BranchID     string        `json:"branch_id,omitempty"`
	AccountType  string        `json:"account_type,omitempty"`
	Kind         string        `json:"kind"`
	Currency     string        `json:"currency"`
	From         string        `json:"from"`
	Through      string        `json:"through"`
	Balances     []Balance     `json:"balances"`
	Transactions []Transaction `json:"transactions"`
}
type Document struct {
	Fields      map[string]string `json:"fields,omitempty"`
	Diagnostics []Diagnostic      `json:"diagnostics,omitempty"`
	Format      string            `json:"format"`
	Version     string            `json:"version"`
	Parser      string            `json:"parser"`
	Accounts    []Account         `json:"accounts"`
}

func invalid(code, message string) error { return apperr.New(apperr.Input, code, message) }

// Details belong to one booked movement; they never become additional postings.
type Detail struct {
	ID          string            `json:"id,omitempty"`
	Amount      string            `json:"amount,omitempty"`
	Currency    string            `json:"currency,omitempty"`
	Description string            `json:"description,omitempty"`
	Fields      map[string]string `json:"fields,omitempty"`
}
type Statement struct {
	ID       string            `json:"id"`
	From     string            `json:"from,omitempty"`
	Through  string            `json:"through,omitempty"`
	Balances []Balance         `json:"balances,omitempty"`
	Fields   map[string]string `json:"fields,omitempty"`
}
type Diagnostic struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	AccountKey string `json:"account_key,omitempty"`
}
