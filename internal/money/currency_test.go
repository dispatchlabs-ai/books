package money

import (
	"math"
	"testing"
)

func TestCurrencyScalesAndExactBoundaries(t *testing.T) {
	for _, tc := range []struct {
		code, input, want string
		units             int64
	}{
		{"USD", "123.45", "123.45", 12345}, {"EUR", "123.45", "123.45", 12345},
		{"JPY", "123.00", "123", 123}, {"KWD", "123.456", "123.456", 123456},
		{"CLF", "123.4567", "123.4567", 1234567},
	} {
		t.Run(tc.code, func(t *testing.T) {
			c, e := Lookup(tc.code)
			if e != nil {
				t.Fatal(e)
			}
			amount, e := c.Parse(tc.input)
			if e != nil || amount != tc.units || c.Format(amount) != tc.want {
				t.Fatalf("%d %v %s", amount, e, c.Format(amount))
			}
			for _, sign := range []int64{1, -1} {
				value := int64(math.MaxInt64) * sign
				parsed, e := c.Parse(c.Format(value))
				if e != nil || parsed != value {
					t.Fatalf("boundary %d %v", parsed, e)
				}
			}
			if _, e = c.Parse(tc.want + "1"); e == nil && c.Scale() > 0 {
				t.Fatal("fractional minor unit accepted")
			}
		})
	}
	jpy, _ := Lookup("JPY")
	if _, e := jpy.Parse("123.1"); e == nil {
		t.Fatal("fractional yen accepted")
	}
	usd, _ := Lookup("USD")
	if _, e := usd.Parse("92233720368547758.08"); e == nil {
		t.Fatal("overflow accepted")
	}
}

func TestIndependentCurrencyInstances(t *testing.T) {
	for _, code := range []string{"JPY", "USD", "KWD", "CLF"} {
		t.Run(code, func(t *testing.T) {
			t.Parallel()
			c, e := Lookup(code)
			if e != nil {
				t.Fatal(e)
			}
			for range 100 {
				v, e := c.Parse(c.Format(12345))
				if e != nil || v != 12345 {
					t.Fatal(v, e)
				}
			}
		})
	}
}
