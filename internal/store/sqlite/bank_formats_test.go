package sqlite_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dispatchlabs-ai/books/internal/banking"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

func tableOptions() banking.Options {
	return banking.Options{Format: "CSV", Institution: "FAKE-BANK", AccountID: "FAKE-ACCOUNT", Currency: "USD", DateLayout: "2006-01-02", Tabular: &banking.TabularProfile{HeaderRow: 1, DateColumn: "Date", AmountColumn: "Amount", DescriptionColumns: []string{"Description"}, DecimalSeparator: "."}}
}

const tableFile = "Date,Amount,Description\n2026-07-15,123.45,Synthetic sale\n"

func uploadFormat(t *testing.T, f fixture, key, raw string, o banking.Options) ledger.BankImportJob {
	t.Helper()
	ctx := context.Background()
	job, e := f.service.UploadBankImportWithOptions(ctx, "ACME", key, "synthetic.dat", []byte(raw), o)
	if e != nil {
		t.Fatal(e)
	}
	job, e = f.service.ProcessBankImport(ctx, "ACME", job.ID)
	if e != nil || job.Status != "READY" {
		t.Fatalf("parse=%+v error=%v", job, e)
	}
	return job
}
func formatChoices(job ledger.BankImportJob, post bool) ledger.BankImportChoices {
	m := ledger.BankAccountMapping{AccountKey: job.Document.Accounts[0].Key, StatementAccount: "ACME-CASH"}
	if post {
		for _, tr := range job.Document.Accounts[0].Transactions {
			if tr.Status == "POSTED" && tr.Amount != "0.00" {
				m.Classifications = append(m.Classifications, ledger.BankClassification{TransactionID: tr.ID, ContraAccount: "4000"})
			}
		}
	}
	return ledger.BankImportChoices{Mappings: []ledger.BankAccountMapping{m}, Post: post}
}
func applyFormat(t *testing.T, f fixture, job ledger.BankImportJob, key string, choices ledger.BankImportChoices) ledger.BankImportReceipt {
	t.Helper()
	ctx := context.Background()
	plan, e := f.service.PreviewBankImport(ctx, "ACME", job.ID, key, choices)
	if e != nil {
		t.Fatal(e)
	}
	receipt, e := f.service.ApplyBankImport(ctx, "ACME", plan.ID, plan.Digest)
	if e != nil {
		t.Fatal(e)
	}
	return receipt
}
func TestBankFormatOptionsSurviveWorkerReopenAndBackup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := bankFixture(t)
	options := tableOptions()
	job, e := f.service.UploadBankImportWithOptions(ctx, "ACME", "format-upload", "synthetic.csv", []byte(tableFile), options)
	if e != nil {
		t.Fatal(e)
	}
	second, e := storesqlite.Open(ctx, f.path, storesqlite.ReadWrite)
	if e != nil {
		t.Fatal(e)
	}
	worker := ledger.NewService(second, "synthetic-worker")
	saved, e := worker.ProcessBankImport(ctx, "ACME", job.ID)
	if e != nil || saved.Status != "READY" || saved.Options.Tabular == nil || saved.Document.Format != "CSV" {
		_ = second.Close()
		t.Fatalf("reopen=%+v %v", saved, e)
	}
	if e = second.Close(); e != nil {
		t.Fatal(e)
	}
	a, _ := json.Marshal(saved.Options)
	b, _ := json.Marshal(options)
	if string(a) != string(b) {
		t.Fatalf("profile changed: %s / %s", a, b)
	}
	receipt := applyFormat(t, f, saved, "format-plan", formatChoices(saved, true))
	if receipt.Summary.NewJournals != 1 || receipt.Summary.ImportedTransactions != 1 {
		t.Fatalf("receipt=%+v", receipt)
	}
	changed := options
	changed.DateLayout = "01/02/2006"
	_, e = f.service.UploadBankImportWithOptions(ctx, "ACME", "format-upload", "synthetic.csv", []byte(tableFile), changed)
	assertBankError(t, e, "IDEMPOTENCY_CONFLICT")
	backupPath := filepath.Join(t.TempDir(), "synthetic-backup.sqlite")
	if _, e = storesqlite.Backup(ctx, f.store, backupPath, "test"); e != nil {
		t.Fatal(e)
	}
	backup, e := storesqlite.Open(ctx, backupPath, storesqlite.ReadOnly)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = backup.Close() }()
	restored, e := ledger.NewService(backup, "test").GetBankImportJob(ctx, "ACME", job.ID)
	if e != nil {
		t.Fatal(e)
	}
	c, _ := json.Marshal(restored.Options)
	if string(c) != string(b) || restored.Receipt == nil {
		t.Fatal("backup lost options or receipt")
	}
	if d, e := backup.Doctor(ctx); e != nil || !d.OK {
		t.Fatalf("backup doctor=%+v %v", d, e)
	}
}
func TestBankImportRetainsPendingAndReviewWithoutPosting(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := bankFixture(t)
	o := tableOptions()
	o.Tabular.IDColumn = "ID"
	o.Tabular.StatusColumn = "State"
	o.Tabular.StatusValues = map[string]string{"booked": "POSTED", "pending": "PENDING", "information": "REVIEW"}
	raw := "Date,Amount,Description,ID,State\n2026-07-15,123.45,Synthetic sale,FAKE-1,booked\n2026-07-15,20.00,Synthetic pending,FAKE-2,pending\n2026-07-15,30.00,Synthetic information,FAKE-3,information\n"
	job := uploadFormat(t, f, "mixed", raw, o)
	choices := formatChoices(job, true)
	invalid := formatChoices(job, true)
	invalid.Mappings[0].Classifications = append(invalid.Mappings[0].Classifications, ledger.BankClassification{TransactionID: "FAKE-2", ContraAccount: "4000"})
	_, e := f.service.PreviewBankImport(ctx, "ACME", job.ID, "bad-classification", invalid)
	assertBankError(t, e, "IMPORT_CLASSIFICATION_INVALID")
	receipt := applyFormat(t, f, job, "mixed-plan", choices)
	if receipt.Summary.ImportedTransactions != 1 || receipt.Summary.RetainedObservations != 2 || receipt.Summary.NewJournals != 1 {
		t.Fatalf("mixed receipt=%+v", receipt)
	}
	sources, e := f.service.ListSourceRecords(ctx, ledger.SourceRecordFilter{})
	if e != nil || len(sources) != 3 {
		t.Fatalf("source count=%d %v", len(sources), e)
	}
	seen := map[string]bool{}
	for _, s := range sources {
		seen[s.Disposition] = true
	}
	if !seen[ledger.SourceDispositionPending] || !seen[ledger.SourceDispositionNeedsReview] || !seen[ledger.SourceDispositionPosted] {
		t.Fatalf("source states=%+v", sources)
	}
	later := "Date,Amount,Description,ID,State\n2026-07-15,20.00,Synthetic pending,FAKE-2,booked\n"
	next := uploadFormat(t, f, "settlement", later, o)
	receipt = applyFormat(t, f, next, "settlement-plan", formatChoices(next, true))
	if receipt.Summary.NewJournals != 1 || receipt.Summary.ImportedTransactions != 1 {
		t.Fatalf("settlement=%+v", receipt)
	}
	if d, e := f.store.Doctor(ctx); e != nil || !d.OK {
		t.Fatalf("doctor=%+v %v", d, e)
	}
}
func TestCrossFormatIdentityReviewRetainsDuplicatesAndLegitimateRepeats(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := bankFixture(t)
	ofx := uploadBank(t, f, "ofx", bankFile)
	applyFormat(t, f, ofx, "ofx-plan", bankChoices(ofx))
	csv := uploadFormat(t, f, "csv", tableFile, tableOptions())
	choices := formatChoices(csv, true)
	_, e := f.service.PreviewBankImport(ctx, "ACME", csv.ID, "no-review", choices)
	assertBankError(t, e, "IMPORT_IDENTITY_REVIEW_REQUIRED")
	matches, e := f.service.MatchBankImport(ctx, "ACME", csv.ID, choices)
	if e != nil || len(matches.Matches) != 1 || len(matches.Matches[0].Candidates) != 1 {
		t.Fatalf("matches=%+v %v", matches, e)
	}
	existing := matches.Matches[0].Candidates[0].SourceRecordID
	transaction := csv.Document.Accounts[0].Transactions[0].ID
	choices.Mappings[0].Classifications = nil
	choices.Mappings[0].IdentityDecisions = []ledger.BankIdentityDecision{{TransactionID: transaction, Action: "duplicate", ExistingSourceID: "outside-this-account", Reason: "Synthetic review"}}
	_, e = f.service.PreviewBankImport(ctx, "ACME", csv.ID, "wrong-source", choices)
	assertBankError(t, e, "IMPORT_IDENTITY_INVALID")
	choices.Mappings[0].IdentityDecisions[0].ExistingSourceID = existing
	receipt := applyFormat(t, f, csv, "duplicate-plan", choices)
	if receipt.Summary.NewJournals != 0 || receipt.Summary.ImportedTransactions != 0 || receipt.Summary.RetainedObservations != 1 {
		t.Fatalf("duplicate receipt=%+v", receipt)
	}
	replay := uploadFormat(t, f, "csv-replay", tableFile, tableOptions())
	receipt = applyFormat(t, f, replay, "duplicate-replay", formatChoices(replay, false))
	if receipt.Summary.SkippedTransactions != 1 || receipt.Summary.NewJournals != 0 {
		t.Fatalf("duplicate replay=%+v", receipt)
	}
	other := uploadFormat(t, f, "legitimate-repeat", strings.Replace(tableFile, "Synthetic sale", "Separate synthetic sale", 1), tableOptions())
	choices = formatChoices(other, true)
	choices.Mappings[0].IdentityDecisions = []ledger.BankIdentityDecision{{TransactionID: other.Document.Accounts[0].Transactions[0].ID, Action: "new", Reason: "Two separate sales with the same amount and date"}}
	receipt = applyFormat(t, f, other, "new-plan", choices)
	if receipt.Summary.ImportedTransactions != 1 || receipt.Summary.NewJournals != 1 {
		t.Fatalf("legitimate repeat=%+v", receipt)
	}
	journals, e := f.service.ListJournals(ctx, "ACME", "", "", "")
	if e != nil || len(journals) != 2 {
		t.Fatalf("journal count=%d %v", len(journals), e)
	}
	sources, e := f.service.ListSourceRecords(ctx, ledger.SourceRecordFilter{})
	if e != nil || len(sources) != 3 {
		t.Fatalf("source count=%d %v", len(sources), e)
	}
	if d, e := f.store.Doctor(ctx); e != nil || !d.OK {
		t.Fatalf("doctor=%+v %v", d, e)
	}
}
func TestBankImportFileReplayCannotChangeLocaleInterpretation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := bankFixture(t)
	raw := strings.Replace(tableFile, "123.45", "1.000", 1)
	o := tableOptions()
	first := uploadFormat(t, f, "original", raw, o)
	applyFormat(t, f, first, "original-plan", formatChoices(first, true))
	again := uploadFormat(t, f, "again", raw, o)
	receipt := applyFormat(t, f, again, "again-plan", formatChoices(again, true))
	if receipt.Summary.SkippedTransactions != 1 || receipt.Summary.ExistingJournals != 1 || receipt.Summary.NewJournals != 0 {
		t.Fatalf("same profile replay=%+v", receipt)
	}
	o.Tabular.DecimalSeparator = ","
	o.Tabular.ThousandsSeparator = "."
	changed := uploadFormat(t, f, "new-profile", raw, o)
	if changed.Document.Accounts[0].Transactions[0].Amount != "1000.00" {
		t.Fatal("fixture should produce a different amount")
	}
	_, e := f.service.PreviewBankImport(ctx, "ACME", changed.ID, "changed-plan", formatChoices(changed, true))
	assertBankError(t, e, "IMPORT_REPLAY_CONFLICT")
}
func TestWeakIDsPreserveTwoIdenticalRowsAndForeignEvidence(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := bankFixture(t)
	raw := tableFile + "2026-07-15,123.45,Synthetic sale\n"
	job := uploadFormat(t, f, "identical-rows", raw, tableOptions())
	receipt := applyFormat(t, f, job, "two-rows", formatChoices(job, true))
	if receipt.Summary.NewJournals != 2 || receipt.Summary.ImportedTransactions != 2 {
		t.Fatalf("distinct rows collapsed=%+v", receipt)
	}
	o := tableOptions()
	o.Currency = "EUR"
	foreign := uploadFormat(t, f, "foreign", tableFile, o)
	if foreign.Document.Accounts[0].Currency != "EUR" || len(foreign.Document.Diagnostics) == 0 {
		t.Fatal("foreign source was not retained")
	}
	_, e := f.service.PreviewBankImport(ctx, "ACME", foreign.ID, "foreign-plan", formatChoices(foreign, true))
	assertBankError(t, e, "IMPORT_ACCOUNT_MISMATCH")
	_, e = f.service.MatchBankImport(ctx, "OTHER", job.ID, formatChoices(job, false))
	assertBankError(t, e, "IMPORT_JOB_NOT_FOUND")
}

func TestBookedImportCannotDuplicateUnpostedEvidence(t *testing.T) {
	for _, state := range []string{"PENDING", "REVIEW"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			f := bankFixture(t)
			o := tableOptions()
			o.Tabular.StatusColumn = "State"
			o.Tabular.StatusValues = map[string]string{"unposted": state}
			raw := "Date,Amount,Description,State\n2026-07-15,123.45,Synthetic sale,unposted\n"
			pending := uploadFormat(t, f, "unposted", raw, o)
			applyFormat(t, f, pending, "unposted-plan", formatChoices(pending, false))
			booked := uploadBank(t, f, "booked", bankFile)
			choices := bankChoices(booked)
			choices.Mappings[0].Classifications = nil
			matches, e := f.service.MatchBankImport(ctx, "ACME", booked.ID, choices)
			if e != nil || len(matches.Matches) != 1 {
				t.Fatalf("matches=%+v %v", matches, e)
			}
			choices.Mappings[0].IdentityDecisions = []ledger.BankIdentityDecision{{TransactionID: "fake-tx-1", Action: "duplicate", ExistingSourceID: matches.Matches[0].Candidates[0].SourceRecordID, Reason: "Synthetic review"}}
			_, e = f.service.PreviewBankImport(ctx, "ACME", booked.ID, "blocked-duplicate", choices)
			assertBankError(t, e, "IMPORT_DUPLICATE_NOT_POSTED")
			journals, e := f.service.ListJournals(ctx, "ACME", "", "", "")
			if e != nil || len(journals) != 0 {
				t.Fatal("blocked preview mutated accounting")
			}
		})
	}
}
func TestLongOFXIdentifierCanResolveCrossFormatCandidate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := bankFixture(t)
	csv := uploadFormat(t, f, "csv", tableFile, tableOptions())
	applyFormat(t, f, csv, "csv-plan", formatChoices(csv, false))
	id := strings.Repeat("x", 512)
	ofx := uploadBank(t, f, "ofx", strings.Replace(bankFile, "fake-tx-1", id, 1))
	choices := bankChoices(ofx)
	choices.Mappings[0].Classifications[0].TransactionID = id
	matches, e := f.service.MatchBankImport(ctx, "ACME", ofx.ID, choices)
	if e != nil || len(matches.Matches) != 1 {
		t.Fatalf("matches=%+v %v", matches, e)
	}
	choices.Mappings[0].IdentityDecisions = []ledger.BankIdentityDecision{{TransactionID: id, Action: "new", Reason: "A separate synthetic payment"}}
	receipt := applyFormat(t, f, ofx, "long-id-plan", choices)
	if receipt.Summary.NewJournals != 1 {
		t.Fatalf("long FITID=%+v", receipt)
	}
}
