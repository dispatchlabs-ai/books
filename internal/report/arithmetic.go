package report

import (
	"math"

	"github.com/dispatchlabs-ai/books/internal/apperr"
)

func amountOverflow() error {
	return apperr.New(apperr.Integrity, "REPORT_AMOUNT_OVERFLOW", "report amount exceeds signed 64-bit cents")
}

func checkedAdd(left, right int64) (int64, error) {
	if (right > 0 && left > math.MaxInt64-right) || (right < 0 && left < math.MinInt64-right) {
		return 0, amountOverflow()
	}
	return left + right, nil
}

func checkedNegate(value int64) (int64, error) {
	if value == math.MinInt64 {
		return 0, amountOverflow()
	}
	return -value, nil
}
