package wire

import (
	"encoding/json"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	"reflect"
	"testing"
)

func TestRawEvidenceExactness(t *testing.T) {
	var input ledger.JournalImportInput
	if err := Decode([]byte(`{"records":[{"raw_json":{"n":9007199254740993,"s":"evidence"}}]}`), &input); err != nil {
		t.Fatal(err)
	}
	if string(input.Records[0].RawJSON) != `{"n":9007199254740993,"s":"evidence"}` {
		t.Fatal(string(input.Records[0].RawJSON))
	}
	var bad ledger.JournalImportInput
	if err := Decode([]byte(`{"records":[{"raw_json":{"n":1,"n":2}}]}`), &bad); err == nil {
		t.Fatal("duplicate raw evidence accepted")
	}
	var amount struct {
		Value int64 `json:"value"`
	}
	for _, raw := range []string{`{"value":9007199254740993}`, `{"value":"01"}`, `{"value":"9223372036854775808"}`} {
		if err := Decode([]byte(raw), &amount); err == nil {
			t.Fatal(raw)
		}
	}
	if err := Decode([]byte(`{"value":"9007199254740993"}`), &amount); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(EncodeValue(reflect.ValueOf(amount), true))
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"value":"9007199254740993"}` {
		t.Fatal(string(encoded))
	}
}
