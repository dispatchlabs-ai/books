package money

import "strings"

const DefaultCurrency = "USD"

// NormalizeCurrency returns the canonical representation used by Books.
func NormalizeCurrency(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

// IsSupportedCurrency reports whether the public release can represent the
// currency's minor-unit exponent without ambiguity. The initial release is
// intentionally USD-only.
func IsSupportedCurrency(value string) bool {
	return NormalizeCurrency(value) == DefaultCurrency
}
