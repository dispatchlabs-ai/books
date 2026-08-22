package money

import (
	"math"
	"testing"
)

func TestParseAndFormat(t *testing.T) {
	t.Parallel()
	tests := map[string]int64{
		"0":       0,
		"0.00":    0,
		"1":       100,
		"1.2":     120,
		"-12.34":  -1234,
		"+999.99": 99999,
	}
	for input, want := range tests {
		input, want := input, want
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			got, err := Parse(input)
			if err != nil {
				t.Fatalf("Parse(%q): %v", input, err)
			}
			if got != want {
				t.Fatalf("Parse(%q) = %d, want %d", input, got, want)
			}
			if reparsed, err := Parse(Format(got)); err != nil || reparsed != want {
				t.Fatalf("round trip %q = %d, %v", Format(got), reparsed, err)
			}
		})
	}
}

func TestParseRejectsInvalidValues(t *testing.T) {
	t.Parallel()
	for _, input := range []string{"", ".1", "1.", "1.001", "$1.00", "1,000", "NaN", "--1"} {
		if _, err := Parse(input); err == nil {
			t.Errorf("Parse(%q) succeeded", input)
		}
	}
}

func TestFormatMinInt64(t *testing.T) {
	t.Parallel()
	if got := Format(math.MinInt64); got != "-92233720368547758.08" {
		t.Fatalf("Format(min int64) = %q", got)
	}
}

func TestSupportedCurrencyIsExplicitlyUSDOnly(t *testing.T) {
	t.Parallel()
	if !IsSupportedCurrency(" usd ") {
		t.Fatal("USD should be supported")
	}
	for _, currency := range []string{"EUR", "JPY", "KWD", ""} {
		if IsSupportedCurrency(currency) {
			t.Fatalf("%s unexpectedly supported", currency)
		}
	}
}
