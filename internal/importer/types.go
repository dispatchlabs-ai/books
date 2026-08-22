// Package importer converts reviewed historical accounting exports into
// deterministic account and draft-journal plans. It never writes a ledger.
package importer

import "github.com/dispatchlabs-ai/books/internal/ledger"

// SourceKind identifies an immutable source format.
type SourceKind string

const (
	SourceJournalXLSX   SourceKind = "JOURNAL_XLSX"
	SourceGeneralLedger SourceKind = "QBO_GENERAL_LEDGER_JSON"
	SourceQBOObjectDir  SourceKind = "QBO_OBJECT_DIRECTORY"
)

// Source limits one source to an optional inclusive date interval. The entity
// interval and source interval are both enforced.
type Source struct {
	Kind      SourceKind
	Path      string
	StartDate string
	EndDate   string
}

// EntityRequest describes one legal entity's immutable source set.
type EntityRequest struct {
	EntityCode         string
	BookCode           string
	Currency           string
	StartDate          string
	CutoffDate         string
	AccountCatalogPath string
	Sources            []Source
}

// Request is intentionally multi-entity. Account codes are global in books,
// so all realms must be reconciled before any journal input is materialized.
type Request struct {
	Entities []EntityRequest
}

// Severity controls whether a diagnostic blocks the affected source object.
type Severity string

const (
	SeverityInfo    Severity = "INFO"
	SeverityWarning Severity = "WARNING"
	SeverityError   Severity = "ERROR"
)

// Diagnostic records a source-local validation decision. Content errors are
// returned here rather than hidden behind a partial inference.
type Diagnostic struct {
	Severity   Severity `json:"severity"`
	Code       string   `json:"code"`
	EntityCode string   `json:"entity_code,omitempty"`
	SourcePath string   `json:"source_path,omitempty"`
	Locator    string   `json:"locator,omitempty"`
	SourceKey  string   `json:"source_key,omitempty"`
	Message    string   `json:"message"`
}

// Evidence pins a planned value to an immutable file and location.
type Evidence struct {
	SourceKind    SourceKind `json:"source_kind"`
	SourcePath    string     `json:"source_path"`
	SourceSHA256  string     `json:"source_sha256"`
	Locator       string     `json:"locator"`
	PayloadSHA256 string     `json:"payload_sha256,omitempty"`
}

// ExternalIdentity preserves the QBO realm-local account identity. QBO IDs
// are never treated as globally unique.
type ExternalIdentity struct {
	EntityCode   string   `json:"entity_code"`
	SourceSystem string   `json:"source_system"`
	ExternalID   string   `json:"external_id"`
	AccountNum   string   `json:"account_number,omitempty"`
	Name         string   `json:"name"`
	Active       bool     `json:"active"`
	Evidence     Evidence `json:"evidence"`
}

// BookActivation is derived from actual posting evidence. For an account that
// is inactive in the source catalog, ActiveTo is its latest imported posting,
// not an invented deactivation date.
type BookActivation struct {
	BookCode       string `json:"book_code"`
	ActiveFrom     string `json:"active_from"`
	ActiveTo       string `json:"active_to,omitempty"`
	PostingEnabled bool   `json:"posting_enabled"`
}

// MasterAccount is a global chart-of-accounts candidate. Identities are merged
// only when account number and semantic signature agree.
type MasterAccount struct {
	Code             string             `json:"code"`
	Name             string             `json:"name"`
	Type             string             `json:"type"`
	Subtype          string             `json:"subtype"`
	NormalBalance    string             `json:"normal_balance"`
	StatementSection string             `json:"statement_section"`
	Identities       []ExternalIdentity `json:"external_identities"`
	Activations      []BookActivation   `json:"book_activations"`
}

// PlannedJournal is a balanced draft input plus the evidence used to derive it.
type PlannedJournal struct {
	EntityCode string                    `json:"entity_code"`
	Input      ledger.CreateJournalInput `json:"input"`
	Evidence   Evidence                  `json:"evidence"`
}

// EntityPlan contains the source-normalized journals for one entity.
type EntityPlan struct {
	EntityCode string           `json:"entity_code"`
	BookCode   string           `json:"book_code"`
	Journals   []PlannedJournal `json:"journals"`
}

// Plan is safe to inspect or serialize. It performs no database writes.
type Plan struct {
	Accounts    []MasterAccount `json:"accounts"`
	Entities    []EntityPlan    `json:"entities"`
	Diagnostics []Diagnostic    `json:"diagnostics"`
}

// HasErrors reports whether any source object was withheld for an unresolved
// or invalid accounting condition.
func (plan Plan) HasErrors() bool {
	for _, diagnostic := range plan.Diagnostics {
		if diagnostic.Severity == SeverityError {
			return true
		}
	}
	return false
}
