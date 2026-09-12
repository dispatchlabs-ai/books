package sqlite_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

const bankFile = `<OFX><BANKMSGSRSV1><STMTTRNRS><TRNUID>fake</TRNUID><STATUS><CODE>0</CODE><SEVERITY>INFO</SEVERITY></STATUS><STMTRS><CURDEF>USD</CURDEF><BANKACCTFROM><BANKID>FAKE-BANK</BANKID><ACCTID>FAKE-ACCOUNT-1234</ACCTID><ACCTTYPE>CHECKING</ACCTTYPE></BANKACCTFROM><BANKTRANLIST><DTSTART>20260701</DTSTART><DTEND>20260731</DTEND><STMTTRN><TRNTYPE>CREDIT</TRNTYPE><DTPOSTED>20260715</DTPOSTED><TRNAMT>123.45</TRNAMT><FITID>fake-tx-1</FITID><NAME>Synthetic sale</NAME></STMTTRN></BANKTRANLIST><LEDGERBAL><BALAMT>123.45</BALAMT><DTASOF>20260731</DTASOF></LEDGERBAL></STMTRS></STMTTRNRS></BANKMSGSRSV1></OFX>`

func bankFixture(t *testing.T) fixture {
	t.Helper()
	f := newFixture(t)
	_, e := f.service.CreateStatementAccount(context.Background(), ledger.CreateStatementAccountInput{Code: "ACME-CASH", Entity: "ACME", Book: "ACME", GLAccount: "1000", Name: "Synthetic checking", Kind: "BANK", Currency: "USD", ReconciliationRequiredFrom: "2026-07-01"})
	if e != nil {
		t.Fatal(e)
	}
	return f
}
func uploadBank(t *testing.T, f fixture, key, file string) ledger.BankImportJob {
	t.Helper()
	ctx := context.Background()
	job, e := f.service.UploadBankImport(ctx, "ACME", key, "statement.ofx", []byte(file))
	if e != nil {
		t.Fatal(e)
	}
	job, e = f.service.ProcessBankImport(ctx, "ACME", job.ID)
	if e != nil || job.Status != "READY" {
		t.Fatalf("parse: %+v %v", job, e)
	}
	return job
}
func bankChoices(job ledger.BankImportJob) ledger.BankImportChoices {
	return ledger.BankImportChoices{Post: true, Mappings: []ledger.BankAccountMapping{{AccountKey: job.Document.Accounts[0].Key, StatementAccount: "ACME-CASH", Classifications: []ledger.BankClassification{{TransactionID: "fake-tx-1", ContraAccount: "4000"}}}}}
}
func assertBankError(t *testing.T, e error, code string) {
	t.Helper()
	a, ok := apperr.As(e)
	if !ok || a.Code != code {
		t.Fatalf("error=%v, want %s", e, code)
	}
}
func TestBankImportPreviewApplyReplayAndBackup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := bankFixture(t)
	job := uploadBank(t, f, "upload-1", bankFile)
	before, e := f.service.ListJournals(ctx, "ACME", "", "", "")
	if e != nil {
		t.Fatal(e)
	}
	plan, e := f.service.PreviewBankImport(ctx, "ACME", job.ID, "preview-1", bankChoices(job))
	if e != nil {
		t.Fatal(e)
	}
	if plan.Summary.NewJournals != 1 || plan.Summary.ImportedTransactions != 1 {
		t.Fatalf("preview=%+v", plan)
	}
	journals, e := f.service.ListJournals(ctx, "ACME", "", "", "")
	if e != nil || len(journals) != len(before) {
		t.Fatalf("preview wrote journals: %+v %v", journals, e)
	}
	sources, e := f.service.ListSourceRecords(ctx, ledger.SourceRecordFilter{})
	if e != nil || len(sources) != 0 {
		t.Fatalf("preview wrote source: %+v %v", sources, e)
	}
	receipt, e := f.service.ApplyBankImport(ctx, "ACME", plan.ID, plan.Digest)
	if e != nil {
		t.Fatal(e)
	}
	if len(receipt.JournalIDs) != 1 {
		t.Fatalf("receipt=%+v", receipt)
	}
	journal, e := f.service.GetJournal(ctx, receipt.JournalIDs[0])
	if e != nil || journal.Status != "POSTED" || journal.TotalDebitCents != 12345 || journal.TotalCreditCents != 12345 {
		t.Fatalf("journal=%+v %v", journal, e)
	}
	replay, e := f.service.ApplyBankImport(ctx, "ACME", plan.ID, plan.Digest)
	if e != nil {
		t.Fatal(e)
	}
	a, _ := json.Marshal(receipt)
	b, _ := json.Marshal(replay)
	if string(a) != string(b) {
		t.Fatal("receipt replay changed")
	}
	overlap := uploadBank(t, f, "overlap", strings.Replace(bankFile, "<DTEND>20260731", "<DTEND>20260730", 1))
	p2, e := f.service.PreviewBankImport(ctx, "ACME", overlap.ID, "p2", bankChoices(overlap))
	if e != nil {
		t.Fatal(e)
	}
	if p2.Summary.ImportedTransactions != 0 || p2.Summary.SkippedTransactions != 1 || p2.Summary.NewJournals != 0 || p2.Summary.ExistingJournals != 1 {
		t.Fatalf("overlap=%+v", p2.Summary)
	}
	if _, e = f.service.ApplyBankImport(ctx, "ACME", p2.ID, p2.Digest); e != nil {
		t.Fatal(e)
	}
	if d, e := f.store.Doctor(ctx); e != nil || !d.OK {
		t.Fatalf("doctor=%+v %v", d, e)
	}
	backupPath := filepath.Join(t.TempDir(), "backup.sqlite")
	if _, e = storesqlite.Backup(ctx, f.store, backupPath, "test"); e != nil {
		t.Fatal(e)
	}
	backup, e := storesqlite.Open(ctx, backupPath, storesqlite.ReadOnly)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = backup.Close() }()
	saved, e := ledger.NewService(backup, "test").GetBankImportJob(ctx, "ACME", job.ID)
	if e != nil || saved.Status != "APPLIED" || saved.Receipt.PlanID != plan.ID {
		t.Fatalf("backup lost job: %+v %v", saved, e)
	}
	raw, e := ledger.NewService(backup, "test").BankImportSource(ctx, "ACME", job.ID)
	if e != nil || string(raw) != bankFile {
		t.Fatalf("backup lost source: %v", e)
	}
}
func TestBankImportStalePlanAndCrossBookAccess(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := bankFixture(t)
	job := uploadBank(t, f, "upload", bankFile)
	plan, e := f.service.PreviewBankImport(ctx, "ACME", job.ID, "preview", bankChoices(job))
	if e != nil {
		t.Fatal(e)
	}
	if e = f.service.ConfigureBookAccount(ctx, "ACME", "4000", "2026-07-01", "", false); e != nil {
		t.Fatal(e)
	}
	_, e = f.service.ApplyBankImport(ctx, "ACME", plan.ID, plan.Digest)
	assertBankError(t, e, "IMPORT_PLAN_STALE")
	j, e := f.service.GetBankImportJob(ctx, "ACME", job.ID)
	if e != nil || j.Status != "READY" {
		t.Fatalf("failed apply changed job: %+v %v", j, e)
	}
	_, e = f.service.GetBankImportJob(ctx, "OTHER", job.ID)
	assertBankError(t, e, "IMPORT_JOB_NOT_FOUND")
	_, e = f.service.GetBankImportPlan(ctx, "OTHER", plan.ID)
	assertBankError(t, e, "IMPORT_PLAN_NOT_FOUND")
	_, e = f.service.BankImportSource(ctx, "OTHER", job.ID)
	assertBankError(t, e, "IMPORT_JOB_NOT_FOUND")
	_, e = f.service.PreviewBankImport(ctx, "ACME", job.ID, "replan", bankChoices(job))
	if e == nil {
		t.Fatal("disabled account accepted")
	}
	sources, e := f.service.ListSourceRecords(ctx, ledger.SourceRecordFilter{})
	if e != nil || len(sources) != 0 {
		t.Fatal("failed preview partially imported")
	}
}
func TestBankImportIdentityCollisionAndAtomicMultiAccountFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := bankFixture(t)
	job := uploadBank(t, f, "one", bankFile)
	choices := bankChoices(job)
	choices.Mappings[0].Classifications[0].ContraAccount = "1000"
	_, e := f.service.PreviewBankImport(ctx, "ACME", job.ID, "transfer", choices)
	assertBankError(t, e, "IMPORT_TRANSFER_UNSUPPORTED")
	choices = bankChoices(job)
	choices.Mappings = append(choices.Mappings, ledger.BankAccountMapping{AccountKey: "absent", StatementAccount: "ACME-CASH"})
	_, e = f.service.PreviewBankImport(ctx, "ACME", job.ID, "badmapping", choices)
	assertBankError(t, e, "IMPORT_MAPPING_INVALID")
	plan, e := f.service.PreviewBankImport(ctx, "ACME", job.ID, "good", bankChoices(job))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.service.ApplyBankImport(ctx, "ACME", plan.ID, plan.Digest); e != nil {
		t.Fatal(e)
	}
	changed := uploadBank(t, f, "changed", strings.Replace(bankFile, "123.45", "100.00", 1))
	_, e = f.service.PreviewBankImport(ctx, "ACME", changed.ID, "collision", bankChoices(changed))
	assertBankError(t, e, "SOURCE_MATERIALIZED")
	wrongBank := uploadBank(t, f, "wrongbank", strings.Replace(bankFile, "FAKE-BANK", "OTHER-FAKE-BANK", 1))
	_, e = f.service.PreviewBankImport(ctx, "ACME", wrongBank.ID, "wrong", bankChoices(wrongBank))
	assertBankError(t, e, "IMPORT_TARGET_ALREADY_MAPPED")
	_, e = f.service.UploadBankImport(ctx, "ACME", "one", "statement.ofx", []byte("changed"))
	assertBankError(t, e, "IDEMPOTENCY_CONFLICT")
}
func TestBankImportConcurrentApplyConverges(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := bankFixture(t)
	job := uploadBank(t, f, "upload", bankFile)
	plan, e := f.service.PreviewBankImport(ctx, "ACME", job.ID, "preview", bankChoices(job))
	if e != nil {
		t.Fatal(e)
	}
	second, e := storesqlite.Open(ctx, f.path, storesqlite.ReadWrite)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = second.Close() }()
	services := []*ledger.Service{f.service, ledger.NewService(second, "other-actor")}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, s := range services {
		wg.Add(1)
		go func(s *ledger.Service) {
			defer wg.Done()
			_, e := s.ApplyBankImport(ctx, "ACME", plan.ID, plan.Digest)
			errs <- e
		}(s)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	journals, e := f.service.ListJournals(ctx, "ACME", "", "", "")
	if e != nil || len(journals) != 1 {
		t.Fatalf("duplicate journals: %d %v", len(journals), e)
	}
}
func TestBankUploadSurvivesReopenAndParseFailureIsDurable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := bankFixture(t)
	job, e := f.service.UploadBankImport(ctx, "ACME", "bad", "bad.ofx", []byte("not OFX"))
	if e != nil {
		t.Fatal(e)
	}
	second, e := storesqlite.Open(ctx, f.path, storesqlite.ReadWrite)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = second.Close() }()
	s := ledger.NewService(second, "worker")
	pending, e := s.PendingBankImportIDs(ctx, "ACME", 10)
	if e != nil || len(pending) != 1 || pending[0] != job.ID {
		t.Fatalf("pending=%v %v", pending, e)
	}
	result, e := s.ProcessBankImport(ctx, "ACME", job.ID)
	if e != nil || result.Status != "FAILED" || result.Error == nil {
		t.Fatalf("failed parse=%+v %v", result, e)
	}
	again, e := s.ProcessBankImport(ctx, "ACME", job.ID)
	if e != nil || again.Status != "FAILED" {
		t.Fatalf("parse replay=%+v %v", again, e)
	}
}

func TestBankImportDistinctIDsAndMultiAccountAtomicity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := bankFixture(t)
	tr := bankFile[strings.Index(bankFile, "<STMTTRN>") : strings.Index(bankFile, "</STMTTRN>")+len("</STMTTRN>")]
	two := strings.Replace(bankFile, "</BANKTRANLIST>", strings.Replace(tr, "fake-tx-1", "fake-tx-2", 1)+"</BANKTRANLIST>", 1)
	job := uploadBank(t, f, "two", two)
	choices := bankChoices(job)
	choices.Mappings[0].Classifications = append(choices.Mappings[0].Classifications, ledger.BankClassification{TransactionID: "fake-tx-2", ContraAccount: "4000"})
	plan, e := f.service.PreviewBankImport(ctx, "ACME", job.ID, "two", choices)
	if e != nil {
		t.Fatal(e)
	}
	receipt, e := f.service.ApplyBankImport(ctx, "ACME", plan.ID, plan.Digest)
	if e != nil || len(receipt.JournalIDs) != 2 {
		t.Fatalf("distinct IDs collapsed: %+v %v", receipt, e)
	}
	response := bankFile[strings.Index(bankFile, "<STMTTRNRS>") : strings.Index(bankFile, "</STMTTRNRS>")+len("</STMTTRNRS>")]
	multi := strings.Replace(bankFile, "</BANKMSGSRSV1>", strings.Replace(response, "FAKE-ACCOUNT-1234", "FAKE-ACCOUNT-5678", 1)+"</BANKMSGSRSV1>", 1)
	job = uploadBank(t, f, "multi", multi)
	choices = bankChoices(job)
	choices.Mappings = append(choices.Mappings, ledger.BankAccountMapping{AccountKey: job.Document.Accounts[1].Key, StatementAccount: "MISSING", Classifications: []ledger.BankClassification{{TransactionID: "fake-tx-1", ContraAccount: "4000"}}})
	before, e := f.service.ListSourceRecords(ctx, ledger.SourceRecordFilter{})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.service.PreviewBankImport(ctx, "ACME", job.ID, "invalid-second", choices); e == nil {
		t.Fatal("invalid second mapping accepted")
	}
	after, e := f.service.ListSourceRecords(ctx, ledger.SourceRecordFilter{})
	if e != nil || len(after) != len(before) {
		t.Fatalf("partial multi-account writes: %v", e)
	}
}

func TestBankImportDoctorDetectsEvidenceTampering(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := bankFixture(t)
	job := uploadBank(t, f, "upload", bankFile)
	plan, e := f.service.PreviewBankImport(ctx, "ACME", job.ID, "preview", bankChoices(job))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.service.ApplyBankImport(ctx, "ACME", plan.ID, plan.Digest); e != nil {
		t.Fatal(e)
	}
	// Deliberately corrupt only this synthetic fixture and restore the exact
	// trigger: schema verification alone cannot detect forged evidence hashes.
	var trigger string
	if e = f.store.DB().QueryRowContext(ctx, `SELECT sql FROM sqlite_schema WHERE name='bank_import_job_lifecycle'`).Scan(&trigger); e != nil {
		t.Fatal(e)
	}
	if _, e = f.store.DB().ExecContext(ctx, `DROP TRIGGER bank_import_job_lifecycle`); e != nil {
		t.Fatal(e)
	}
	raw := []byte(strings.Replace(bankFile, "Synthetic sale", "Altered evidence", 1))
	hash := sha256.Sum256(raw)
	if _, e = f.store.DB().ExecContext(ctx, `UPDATE bank_import_jobs SET source_bytes=?,source_sha256=? WHERE id=?`, raw, hex.EncodeToString(hash[:]), job.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = f.store.DB().ExecContext(ctx, trigger); e != nil {
		t.Fatal(e)
	}
	doctor, e := f.store.Doctor(ctx)
	if e == nil || doctor.InvalidBankImports == 0 || doctor.OK {
		t.Fatalf("forged hash escaped audit binding: %+v %v", doctor, e)
	}
}
