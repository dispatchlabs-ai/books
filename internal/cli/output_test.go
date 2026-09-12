package cli

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestJSONMoneyUsesDecimalStrings(t *testing.T) {
	t.Parallel()
	value := struct {
		AmountCents int64 `json:"amount_cents"`
		EntryNumber int64 `json:"entry_number"`
	}{AmountCents: -12345, EntryNumber: 7}
	var output bytes.Buffer
	if err := writeJSON(&output, value); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	data := decoded["data"].(map[string]any)
	if data["amount_cents"] != "-123.45" {
		t.Fatalf("amount_cents = %#v", data["amount_cents"])
	}
	if data["entry_number"] != float64(7) {
		t.Fatalf("entry_number = %#v", data["entry_number"])
	}
}
