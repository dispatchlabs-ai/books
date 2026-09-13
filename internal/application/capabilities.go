package application

import (
	"github.com/dispatchlabs-ai/books/internal/banking"
	"github.com/dispatchlabs-ai/books/internal/money"
)

// Capabilities reports implementation support, never principal permissions.
func Capabilities() map[string]any {
	names := []string{}
	for _, f := range banking.Capabilities() {
		names = append(names, f.Format)
	}
	return map[string]any{"api": "books.api/v1", "formats": names, "format_profiles": banking.Capabilities(), "parser": banking.StatementParserVersion, "supported_parsers": []string{banking.ParserVersion, banking.StatementParserVersion}, "sgml_versions": []string{"102", "103", "160"}, "xml": "OFX 2 bank/card subset", "currency": money.SupportedCurrencies(), "single_currency_per_entity": true, "currency_conversion": false, "max_upload_bytes": banking.MaxBytes, "durable_imports": true, "atomic_apply": true, "workflows": []string{"transactions", "corrections", "reconciliation", "period-close", "year-close", "accounts", "periods", "defaults"}, "local_administration": []string{}, "administration": []string{"company-registry", "backup-restore", "quickbooks", "retained-lifecycle-evidence", "consolidation"}, "offline_posting": false, "change_feed": false, "posting_contra_types": []string{"REVENUE", "EXPENSE", "EQUITY"}}
}
