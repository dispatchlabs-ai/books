package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/dispatchlabs-ai/books/internal/banking"
)

func importHash(data []byte) string { hash := sha256.Sum256(data); return hex.EncodeToString(hash[:]) }

// checkBankImports binds persisted blobs and JSON to the audit chain, independently
// of their mutable storage location. It never reparses with a newer parser version.
func (s *Store) checkBankImports(ctx context.Context) (int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT source_bytes,source_sha256,COALESCE(document_json,''),COALESCE(document_sha256,''),COALESCE(receipt_json,''),COALESCE(receipt_sha256,''),source_name,request_sha256,COALESCE((SELECT payload_json FROM audit_events a WHERE a.command='bank-import upload' AND a.aggregate_type='bank_import_job' AND a.aggregate_id=j.id),''),(SELECT COUNT(*) FROM audit_events a WHERE a.command='bank-import upload' AND a.aggregate_type='bank_import_job' AND a.aggregate_id=j.id) FROM bank_import_jobs j`)
	if err != nil {
		return 0, err
	}
	invalid := 0
	for rows.Next() {
		var raw []byte
		var source, doc, docHash, receipt, receiptHash, name, requestHash, payload string
		var uploadEvents int
		if err = rows.Scan(&raw, &source, &doc, &docHash, &receipt, &receiptHash, &name, &requestHash, &payload, &uploadEvents); err != nil {
			_ = rows.Close()
			return 0, err
		}
		var evidence struct {
			Options       banking.Options `json:"options"`
			RequestSHA256 string          `json:"request_sha256"`
		}
		optionsValid := json.Unmarshal([]byte(payload), &evidence) == nil
		options, e := banking.NormalizeOptions(evidence.Options)
		if uploadEvents != 1 || !optionsValid || e != nil || importHash(banking.UploadRequest(name, source, options)) != requestHash || (evidence.RequestSHA256 != "" && evidence.RequestSHA256 != requestHash) || importHash(raw) != source || doc != "" && importHash([]byte(doc)) != docHash || receipt != "" && importHash([]byte(receipt)) != receiptHash {
			invalid++
		}
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	if err = rows.Close(); err != nil {
		return 0, err
	}
	rows, err = s.db.QueryContext(ctx, `SELECT plan_json,plan_sha256 FROM bank_import_plans`)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var data, hash string
		if err = rows.Scan(&data, &hash); err != nil {
			_ = rows.Close()
			return 0, err
		}
		if importHash([]byte(data)) != hash {
			invalid++
		}
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	if err = rows.Close(); err != nil {
		return 0, err
	}
	var bindings int
	err = s.db.QueryRowContext(ctx, `SELECT
 (SELECT COUNT(*) FROM bank_import_jobs j JOIN books b ON b.id=j.book_id WHERE
 NOT EXISTS(SELECT 1 FROM audit_events a WHERE a.command='bank-import upload' AND a.aggregate_type='bank_import_job' AND a.aggregate_id=j.id AND json_extract(a.payload_json,'$.book')=b.code AND json_extract(a.payload_json,'$.source_sha256')=j.source_sha256)
 OR (j.status IN ('READY','APPLIED') AND NOT EXISTS(SELECT 1 FROM audit_events a WHERE a.command='bank-import parse' AND a.aggregate_type='bank_import_job' AND a.aggregate_id=j.id AND json_extract(a.payload_json,'$.document_sha256')=j.document_sha256 AND json_extract(a.payload_json,'$.parser')=j.parser_version))
 OR (j.status='APPLIED' AND (json_extract(j.receipt_json,'$.job_id') IS NOT j.id OR json_extract(j.receipt_json,'$.plan_id') IS NOT j.applied_plan_id OR json_extract(j.receipt_json,'$.plan_digest') IS NOT (SELECT p.plan_sha256 FROM bank_import_plans p WHERE p.id=j.applied_plan_id) OR NOT EXISTS(SELECT 1 FROM audit_events a WHERE a.command='bank-import apply' AND a.aggregate_type='bank_import_job' AND a.aggregate_id=j.id AND json_extract(a.payload_json,'$.receipt_sha256')=j.receipt_sha256 AND json_extract(a.payload_json,'$.plan_id')=j.applied_plan_id))))
 + (SELECT COUNT(*) FROM bank_import_plans p WHERE json_extract(p.plan_json,'$.id') IS NOT p.id OR json_extract(p.plan_json,'$.job_id') IS NOT p.job_id OR json_extract(p.plan_json,'$.ledger_revision') IS NOT p.ledger_revision OR NOT EXISTS(SELECT 1 FROM audit_events a WHERE a.command='bank-import preview' AND a.aggregate_type='bank_import_plan' AND a.aggregate_id=p.id AND json_extract(a.payload_json,'$.plan_digest')=p.plan_sha256 AND json_extract(a.payload_json,'$.job_id')=p.job_id))`).Scan(&bindings)
	return invalid + bindings, err
}
