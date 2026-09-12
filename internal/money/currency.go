package money

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const DefaultCurrency = "USD"

// Currency carries a validated ISO monetary scale. Its zero value is USD for
// the original decimal-money contract; there is no process-wide currency state.
type Currency struct {
	code  string
	scale int
}

func NormalizeCurrency(value string) string { return strings.ToUpper(strings.TrimSpace(value)) }
func Lookup(value string) (Currency, error) {
	code := NormalizeCurrency(value)
	info, ok := currencies[code]
	if !ok {
		return Currency{}, fmt.Errorf("unsupported monetary currency %q", code)
	}
	return Currency{code: code, scale: info.Scale}, nil
}
func (c Currency) Code() string {
	if c.code == "" {
		return DefaultCurrency
	}
	return c.code
}
func (c Currency) Scale() int {
	if c.code == "" {
		return 2
	}
	return c.scale
}
func IsSupportedCurrency(value string) bool { _, err := Lookup(value); return err == nil }
func SupportedCurrencies() []string {
	result := make([]string, 0, len(currencies))
	for code := range currencies {
		result = append(result, code)
	}
	sort.Strings(result)
	return result
}
func CurrencyFromNumeric(value string) (string, bool) {
	for code, info := range currencies {
		if info.Numeric == value {
			return code, true
		}
	}
	return "", false
}
func ParseCurrency(value, code string) (int64, error) {
	currency, err := Lookup(code)
	if err != nil {
		return 0, err
	}
	return currency.Parse(value)
}

func (c Currency) String() string               { return c.Code() }
func (c Currency) MarshalJSON() ([]byte, error) { return json.Marshal(c.Code()) }
func (c *Currency) UnmarshalJSON(data []byte) error {
	var code string
	if err := json.Unmarshal(data, &code); err != nil {
		return err
	}
	return c.Scan(code)
}
func (c *Currency) Scan(value any) error {
	code, ok := value.(string)
	if !ok {
		return fmt.Errorf("currency must be text")
	}
	unit, err := Lookup(code)
	if err != nil {
		return err
	}
	*c = unit
	return nil
}
