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

// Parse converts a decimal currency string to cents without floating point.
func Parse(value string) (int64, error) {
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
		if len(parts[1]) == 0 || len(parts[1]) > 2 {
			return 0, fmt.Errorf("%w: exactly one or two decimal places are allowed", ErrInvalid)
		}
		fractionText := parts[1]
		if len(fractionText) == 1 {
			fractionText += "0"
		}
		fraction, err = strconv.ParseUint(fractionText, 10, 7)
		if err != nil {
			return 0, fmt.Errorf("%w: %q", ErrInvalid, value)
		}
	}
	if whole > uint64(math.MaxInt64)/100 || whole*100 > uint64(math.MaxInt64)-fraction {
		return 0, fmt.Errorf("%w: amount exceeds int64 cents", ErrInvalid)
	}
	cents := int64(whole*100 + fraction)
	if negative {
		cents = -cents
	}
	return cents, nil
}

// Format converts cents to a decimal currency string.
func Format(cents int64) string {
	negative := cents < 0
	var magnitude uint64
	if negative {
		magnitude = uint64(-(cents + 1)) + 1
	} else {
		magnitude = uint64(cents)
	}
	formatted := fmt.Sprintf("%d.%02d", magnitude/100, magnitude%100)
	if negative {
		return "-" + formatted
	}
	return formatted
}
