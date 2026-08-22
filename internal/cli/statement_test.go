package cli

import "testing"

func TestParseStatementImportPreservesDisposition(t *testing.T) {
	input, err := parseStatementImport([]byte(`{
        "statement_account":"ACME-CASH",
        "source_system":"BANK",
        "source_name":"july.json",
        "transactions":[{
          "external_id":"pending-1",
          "posted_date":"2026-07-31",
          "description":"Pending evidence",
	          "amount":"-12.34",
	          "disposition":"PENDING",
	          "exclusion_reason":"provider pending"
	        },{
	          "external_id":"reviewed-1",
	          "posted_date":"2026-07-31",
	          "description":"Reviewed evidence",
	          "amount":"12.34",
	          "resolution_reason":"provider statement resolved the row",
	          "resolution_evidence":{"statement_page":2}
        }]
    }`))
	if err != nil {
		t.Fatal(err)
	}
	if len(input.Transactions) != 2 || input.Transactions[0].Disposition != "PENDING" ||
		input.Transactions[0].ExclusionReason != "provider pending" || input.Transactions[0].AmountCents != -1234 ||
		input.Transactions[1].ResolutionReason != "provider statement resolved the row" ||
		string(input.Transactions[1].ResolutionEvidence) != `{"statement_page":2}` {
		t.Fatalf("unexpected parsed statement source: %+v", input.Transactions)
	}
}
