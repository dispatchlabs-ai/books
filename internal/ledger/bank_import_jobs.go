package ledger

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/banking"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

type BankImportError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type BankImportJob struct {
	ID           string             `json:"id"`
	Options      banking.Options    `json:"options,omitempty"`
	Book         string             `json:"book"`
	SourceName   string             `json:"source_name"`
	SourceSHA256 string             `json:"source_sha256"`
	Status       string             `json:"status"`
	Parser       string             `json:"parser,omitempty"`
	Document     *banking.Document  `json:"document,omitempty"`
	Error        *BankImportError   `json:"error,omitempty"`
	Receipt      *BankImportReceipt `json:"receipt,omitempty"`
	CreatedAt    string             `json:"created_at"`
	CreatedBy    string             `json:"created_by"`
	CompletedAt  string             `json:"completed_at,omitempty"`
}

func bankHash(b []byte) string       { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func bankJSON(v any) ([]byte, error) { return json.Marshal(v) }
func validateOperationKey(k string) error {
	if len(k) < 1 || len(k) > 128 || strings.TrimSpace(k) != k {
		return apperr.New(apperr.Invalid, "IDEMPOTENCY_KEY_INVALID", "provide a stable idempotency key of 1 to 128 characters")
	}
	for _, c := range k {
		if c < 33 || c > 126 {
			return apperr.New(apperr.Invalid, "IDEMPOTENCY_KEY_INVALID", "idempotency key must contain printable ASCII without spaces")
		}
	}
	return nil
}
func bankBookID(ctx context.Context, q queryer, book string) (string, error) {
	var id string
	e := q.QueryRowContext(ctx, `SELECT b.id FROM books b JOIN entities e ON e.id=b.entity_id WHERE b.code=? AND b.kind='ACTUAL' AND b.status='ACTIVE' AND e.status='ACTIVE' AND b.currency=e.functional_currency AND b.accounting_basis='ACCRUAL'`, normalizeCode(book)).Scan(&id)
	if e == sql.ErrNoRows {
		return "", apperr.New(apperr.NotFound, "BOOK_NOT_AVAILABLE", "an active actual book is required")
	}
	return id, e
}

// UploadBankImport durably accepts bounded source bytes before any parser runs.
func (s *Service) UploadBankImport(ctx context.Context, book, key, name string, data []byte) (BankImportJob, error) {
	return s.UploadBankImportWithOptions(ctx, book, key, name, data, banking.Options{})
}

func (s *Service) UploadBankImportWithOptions(ctx context.Context, book, key, name string, data []byte, options banking.Options) (BankImportJob, error) {
	options, e := banking.NormalizeOptions(options)
	if e != nil {
		return BankImportJob{}, e
	}
	if e := s.requireActor(); e != nil {
		return BankImportJob{}, e
	}
	if e := validateOperationKey(key); e != nil {
		return BankImportJob{}, e
	}
	if len(data) < 1 || len(data) > banking.MaxBytes {
		return BankImportJob{}, apperr.New(apperr.Input, "IMPORT_SIZE_INVALID", "statement must contain between 1 and 8388608 bytes")
	}
	if len(name) < 1 || len(name) > 200 || strings.ContainsAny(name, "/\\\r\n\x00") || strings.TrimSpace(name) != name {
		return BankImportJob{}, apperr.New(apperr.Input, "IMPORT_NAME_INVALID", "provide a short filename without a path")
	}
	sourceHash := bankHash(data)
	request := banking.UploadRequest(name, sourceHash, options)
	requestHash := bankHash(request)
	tx, e := s.store.Begin(ctx)
	if e != nil {
		return BankImportJob{}, e
	}
	defer func() { _ = tx.Rollback() }()
	bookID, e := bankBookID(ctx, tx, book)
	if e != nil {
		return BankImportJob{}, e
	}
	var id, oldHash string
	e = tx.QueryRowContext(ctx, `SELECT id,request_sha256 FROM bank_import_jobs WHERE book_id=? AND upload_key=?`, bookID, key).Scan(&id, &oldHash)
	if e == nil {
		if oldHash != requestHash {
			return BankImportJob{}, apperr.New(apperr.Conflict, "IDEMPOTENCY_CONFLICT", "upload key was already used with different content")
		}
		return readBankImportJob(ctx, tx, book, id)
	}
	if e != sql.ErrNoRows {
		return BankImportJob{}, e
	}
	id, e = storesqlite.NewID()
	if e != nil {
		return BankImportJob{}, e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO bank_import_jobs(id,book_id,upload_key,request_sha256,source_name,source_bytes,source_sha256,status,created_at,created_by) VALUES(?,?,?,?,?,?,?,'UPLOADED',?,?)`, id, bookID, key, requestHash, name, data, sourceHash, storesqlite.UTCNow(), s.actor)
	if e != nil {
		return BankImportJob{}, storesqlite.MapError("store bank upload", e)
	}
	if _, e = storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{Actor: s.actor, Command: "bank-import upload", AggregateType: "bank_import_job", AggregateID: id, Payload: map[string]any{"book": normalizeCode(book), "source_sha256": sourceHash, "options": options, "request_sha256": requestHash}}); e != nil {
		return BankImportJob{}, e
	}
	result, e := readBankImportJob(ctx, tx, book, id)
	if e != nil {
		return BankImportJob{}, e
	}
	if e = tx.Commit(); e != nil {
		return BankImportJob{}, storesqlite.MapError("commit bank upload", e)
	}
	return result, nil
}

func (s *Service) GetBankImportJob(ctx context.Context, book, id string) (BankImportJob, error) {
	return readBankImportJob(ctx, s.store.DB(), book, id)
}
func readBankImportJob(ctx context.Context, q queryer, book, id string) (BankImportJob, error) {
	var j BankImportJob
	var document, documentHash, code, message, receipt, receiptHash, requestHash string
	e := q.QueryRowContext(ctx, `SELECT j.id,b.code,j.source_name,j.source_sha256,j.status,COALESCE(j.parser_version,''),COALESCE(j.document_json,''),COALESCE(j.document_sha256,''),COALESCE(j.error_code,''),COALESCE(j.error_message,''),COALESCE(j.receipt_json,''),COALESCE(j.receipt_sha256,''),j.created_at,j.created_by,COALESCE(j.completed_at,''),j.request_sha256 FROM bank_import_jobs j JOIN books b ON b.id=j.book_id WHERE j.id=? AND b.code=?`, id, normalizeCode(book)).Scan(&j.ID, &j.Book, &j.SourceName, &j.SourceSHA256, &j.Status, &j.Parser, &document, &documentHash, &code, &message, &receipt, &receiptHash, &j.CreatedAt, &j.CreatedBy, &j.CompletedAt, &requestHash)
	if e == sql.ErrNoRows {
		return j, apperr.New(apperr.NotFound, "IMPORT_JOB_NOT_FOUND", "import job was not found in this company")
	}
	if e != nil {
		return j, e
	}
	rows, e := q.QueryContext(ctx, `SELECT payload_json FROM audit_events WHERE command='bank-import upload' AND aggregate_type='bank_import_job' AND aggregate_id=?`, id)
	if e != nil {
		return j, e
	}
	var payload string
	count := 0
	for rows.Next() {
		count++
		if e = rows.Scan(&payload); e != nil {
			_ = rows.Close()
			return j, e
		}
	}
	if e = rows.Err(); e != nil {
		_ = rows.Close()
		return j, e
	}
	if e = rows.Close(); e != nil {
		return j, e
	}
	var evidence struct {
		Book          string          `json:"book"`
		SourceSHA256  string          `json:"source_sha256"`
		Options       banking.Options `json:"options"`
		RequestSHA256 string          `json:"request_sha256"`
	}
	if count != 1 || json.Unmarshal([]byte(payload), &evidence) != nil || evidence.Book != j.Book || evidence.SourceSHA256 != j.SourceSHA256 {
		return j, apperr.New(apperr.Integrity, "IMPORT_EVIDENCE_INVALID", "upload audit evidence does not match the job")
	}
	j.Options, e = banking.NormalizeOptions(evidence.Options)
	if e != nil {
		return j, apperr.New(apperr.Integrity, "IMPORT_EVIDENCE_INVALID", "invalid saved source options")
	}
	if bankHash(banking.UploadRequest(j.SourceName, j.SourceSHA256, j.Options)) != requestHash || (evidence.RequestSHA256 != "" && evidence.RequestSHA256 != requestHash) {
		return j, apperr.New(apperr.Integrity, "IMPORT_EVIDENCE_INVALID", "upload options or request hash do not match")
	}
	if document != "" {
		if bankHash([]byte(document)) != documentHash {
			return j, apperr.New(apperr.Integrity, "IMPORT_EVIDENCE_INVALID", "parsed evidence hash does not match")
		}
		if e = json.Unmarshal([]byte(document), &j.Document); e != nil {
			return j, e
		}
	}
	if code != "" {
		j.Error = &BankImportError{Code: code, Message: message}
	}
	if receipt != "" {
		if bankHash([]byte(receipt)) != receiptHash {
			return j, apperr.New(apperr.Integrity, "IMPORT_RECEIPT_INVALID", "import receipt hash does not match")
		}
		if e = json.Unmarshal([]byte(receipt), &j.Receipt); e != nil {
			return j, e
		}
	}
	return j, nil
}
func (s *Service) BankImportSource(ctx context.Context, book, id string) ([]byte, error) {
	return bankImportSource(ctx, s.store.DB(), book, id)
}
func bankImportSource(ctx context.Context, q queryer, book, id string) ([]byte, error) {
	var data []byte
	var digest string
	e := q.QueryRowContext(ctx, `SELECT j.source_bytes,j.source_sha256 FROM bank_import_jobs j JOIN books b ON b.id=j.book_id WHERE b.code=? AND j.id=?`, normalizeCode(book), id).Scan(&data, &digest)
	if e == sql.ErrNoRows {
		return nil, apperr.New(apperr.NotFound, "IMPORT_JOB_NOT_FOUND", "import job was not found in this company")
	}
	if e != nil {
		return nil, e
	}
	if bankHash(data) != digest {
		return nil, apperr.New(apperr.Integrity, "IMPORT_EVIDENCE_INVALID", "source evidence hash does not match")
	}
	return data, nil
}

// ProcessBankImport is restartable: parsing has no side effects and competing
// workers recheck state before publishing one immutable result.
func (s *Service) ProcessBankImport(ctx context.Context, book, id string) (BankImportJob, error) {
	if e := s.requireActor(); e != nil {
		return BankImportJob{}, e
	}
	job, e := s.GetBankImportJob(ctx, book, id)
	if e != nil {
		return job, e
	}
	if job.Status != "UPLOADED" {
		return job, nil
	}
	data, e := s.BankImportSource(ctx, book, id)
	if e != nil {
		return job, e
	}
	doc, parseError := banking.ParseFile(data, job.Options)
	tx, e := s.store.Begin(ctx)
	if e != nil {
		return job, e
	}
	defer func() { _ = tx.Rollback() }()
	job, e = readBankImportJob(ctx, tx, book, id)
	if e != nil {
		return job, e
	}
	if job.Status != "UPLOADED" {
		return job, nil
	}
	if _, e = bankBookID(ctx, tx, book); e != nil {
		return job, e
	}
	documentHash := ""
	parser := doc.Parser
	if parseError != nil {
		parser = banking.StatementParserVersion
	}
	if parseError != nil {
		code, message := "IMPORT_PARSE_FAILED", "statement could not be parsed"
		if a, ok := apperr.As(parseError); ok {
			code, message = a.Code, a.Message
		}
		_, e = tx.ExecContext(ctx, `UPDATE bank_import_jobs SET status='FAILED',parser_version=?,error_code=?,error_message=?,completed_at=? WHERE id=?`, parser, code, message, storesqlite.UTCNow(), id)
	} else {
		document, marshalError := bankJSON(doc)
		if marshalError != nil {
			return job, marshalError
		}
		documentHash = bankHash(document)
		_, e = tx.ExecContext(ctx, `UPDATE bank_import_jobs SET status='READY',parser_version=?,document_json=?,document_sha256=?,completed_at=? WHERE id=?`, parser, string(document), bankHash(document), storesqlite.UTCNow(), id)
	}
	if e != nil {
		return job, storesqlite.MapError("store parsed statement", e)
	}
	if _, e = storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{Actor: s.actor, Command: "bank-import parse", AggregateType: "bank_import_job", AggregateID: id, Payload: map[string]any{"book": normalizeCode(book), "parser": parser, "failed": parseError != nil, "document_sha256": documentHash}}); e != nil {
		return job, e
	}
	result, e := readBankImportJob(ctx, tx, book, id)
	if e != nil {
		return job, e
	}
	if e = tx.Commit(); e != nil {
		return job, storesqlite.MapError("commit parsed statement", e)
	}
	return result, nil
}

func (s *Service) PendingBankImportIDs(ctx context.Context, book string, limit int) ([]string, error) {
	if limit < 1 || limit > 100 {
		return nil, apperr.New(apperr.Invalid, "PAGE_LIMIT_INVALID", "limit must be between 1 and 100")
	}
	rows, e := s.store.DB().QueryContext(ctx, `SELECT j.id FROM bank_import_jobs j JOIN books b ON b.id=j.book_id WHERE b.code=? AND j.status='UPLOADED' ORDER BY j.created_at,j.id LIMIT ?`, normalizeCode(book), limit)
	if e != nil {
		return nil, e
	}
	defer func() { _ = rows.Close() }()
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			return nil, e
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
