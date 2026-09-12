// Package money parses and formats exact integer minor-unit amounts.
package money

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

var ErrInvalid = errors.New("invalid money amount")

// Parse preserves the original USD decimal contract.
func Parse(value string) (int64, error) { return (Currency{}).Parse(value) }

// Parse converts exact decimal text to this currency's integer minor units.
func (currency Currency) Parse(value string) (int64, error) {
	scale := currency.Scale()
	factor := uint64(1)
	for range scale {
		factor *= 10
	}
	s := strings.TrimSpace(value)
	if s == "" {
		return 0, fmt.Errorf("%w: empty value", ErrInvalid)
	}

	negative := false
	switch s[0] {
	case '-':
		negative = true
		s = s[1:]
	case '+':
		s = s[1:]
	}
	if s == "" {
		return 0, fmt.Errorf("%w: %q", ErrInvalid, value)
	}
	if strings.ContainsAny(s, ",$_ ") {
		return 0, fmt.Errorf("%w: use an unformatted decimal value", ErrInvalid)
	}

	parts := strings.Split(s, ".")
	if len(parts) > 2 || parts[0] == "" {
		return 0, fmt.Errorf("%w: %q", ErrInvalid, value)
	}
	whole, err := strconv.ParseUint(parts[0], 10, 63)
	if err != nil {
		return 0, fmt.Errorf("%w: %q", ErrInvalid, value)
	}
	fraction := uint64(0)
	if len(parts) == 2 {
		if len(parts[1]) == 0 {
			return 0, fmt.Errorf("%w: fractional digits are required after a decimal point", ErrInvalid)
		}
		if len(parts[1]) > scale {
			if strings.Trim(parts[1][scale:], "0") != "" {
				return 0, fmt.Errorf("%w: %s permits %d decimal places", ErrInvalid, currency.Code(), scale)
			}
			parts[1] = parts[1][:scale]
		}
		fractionText := parts[1]
		fractionText += strings.Repeat("0", scale-len(fractionText))
		if fractionText == "" {
			fractionText = "0"
		}
		fraction, err = strconv.ParseUint(fractionText, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("%w: %q", ErrInvalid, value)
		}
	}
	limit := uint64(math.MaxInt64)
	if whole > limit/factor || whole*factor > limit-fraction {
		return 0, fmt.Errorf("%w: amount exceeds int64 minor units", ErrInvalid)
	}
	cents := int64(whole*factor + fraction)
	if negative {
		cents = -cents
	}
	return cents, nil
}

// Format preserves the original USD decimal contract.
func Format(cents int64) string { return (Currency{}).Format(cents) }

// Format renders minor units at the currency's monetary scale.
func (currency Currency) Format(cents int64) string {
	scale := currency.Scale()
	factor := uint64(1)
	for range scale {
		factor *= 10
	}
	negative := cents < 0
	var magnitude uint64
	if negative {
		magnitude = uint64(-(cents + 1)) + 1
	} else {
		magnitude = uint64(cents)
	}
	formatted := strconv.FormatUint(magnitude, 10)
	if scale > 0 {
		formatted = fmt.Sprintf("%d.%0*d", magnitude/factor, scale, magnitude%factor)
	}
	if negative {
		return "-" + formatted
	}
	return formatted
}
