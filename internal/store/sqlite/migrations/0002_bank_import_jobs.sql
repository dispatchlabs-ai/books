-- Durable source uploads and immutable previews are backed up with the ledger.
PRAGMA user_version = 2;

CREATE TABLE bank_import_jobs (
 id TEXT PRIMARY KEY,
 book_id TEXT NOT NULL REFERENCES books(id) ON DELETE RESTRICT,
 upload_key TEXT NOT NULL CHECK(length(upload_key) BETWEEN 1 AND 128),
 request_sha256 TEXT NOT NULL CHECK(length(request_sha256) = 64),
 source_name TEXT NOT NULL CHECK(length(source_name) BETWEEN 1 AND 200),
 source_bytes BLOB NOT NULL CHECK(length(source_bytes) BETWEEN 1 AND 8388608),
 source_sha256 TEXT NOT NULL CHECK(length(source_sha256) = 64),
 status TEXT NOT NULL CHECK(status IN ('UPLOADED','READY','FAILED','APPLIED')),
 parser_version TEXT,
 document_json TEXT CHECK(document_json IS NULL OR json_valid(document_json)),
 document_sha256 TEXT,
 error_code TEXT,
 error_message TEXT,
 applied_plan_id TEXT REFERENCES bank_import_plans(id) ON DELETE RESTRICT,
 receipt_json TEXT CHECK(receipt_json IS NULL OR json_valid(receipt_json)),
 receipt_sha256 TEXT,
 created_at TEXT NOT NULL,
 created_by TEXT NOT NULL,
 completed_at TEXT,
 UNIQUE(book_id, upload_key),
 CHECK (
  (status = 'UPLOADED' AND document_json IS NULL AND error_code IS NULL AND receipt_json IS NULL AND parser_version IS NULL AND completed_at IS NULL) OR
  (status = 'FAILED' AND document_json IS NULL AND error_code IS NOT NULL AND error_message IS NOT NULL AND receipt_json IS NULL AND parser_version IS NOT NULL AND completed_at IS NOT NULL) OR
  (status = 'READY' AND document_json IS NOT NULL AND document_sha256 IS NOT NULL AND error_code IS NULL AND receipt_json IS NULL AND parser_version IS NOT NULL AND completed_at IS NOT NULL) OR
  (status = 'APPLIED' AND document_json IS NOT NULL AND document_sha256 IS NOT NULL AND error_code IS NULL AND receipt_json IS NOT NULL AND receipt_sha256 IS NOT NULL AND applied_plan_id IS NOT NULL AND parser_version IS NOT NULL AND completed_at IS NOT NULL)
 )
) STRICT;
CREATE INDEX bank_import_jobs_book_cursor ON bank_import_jobs(book_id, created_at, id);
CREATE INDEX bank_import_jobs_pending ON bank_import_jobs(status, created_at, id);

CREATE TABLE bank_import_plans (
 id TEXT PRIMARY KEY,
 job_id TEXT NOT NULL REFERENCES bank_import_jobs(id) ON DELETE RESTRICT,
 preview_key TEXT NOT NULL CHECK(length(preview_key) BETWEEN 1 AND 128),
 request_sha256 TEXT NOT NULL CHECK(length(request_sha256) = 64),
 ledger_revision TEXT NOT NULL CHECK(length(ledger_revision) = 64),
 plan_json TEXT NOT NULL CHECK(json_valid(plan_json)),
 plan_sha256 TEXT NOT NULL CHECK(length(plan_sha256) = 64),
 created_at TEXT NOT NULL,
 created_by TEXT NOT NULL,
 UNIQUE(job_id, preview_key)
) STRICT;
CREATE TRIGGER bank_import_plan_immutable_update BEFORE UPDATE ON bank_import_plans BEGIN
 SELECT RAISE(ABORT, 'bank import plans are immutable');
END;
CREATE TRIGGER bank_import_plan_immutable_delete BEFORE DELETE ON bank_import_plans BEGIN
 SELECT RAISE(ABORT, 'bank import plans are immutable');
END;
CREATE TRIGGER bank_import_job_immutable_delete BEFORE DELETE ON bank_import_jobs BEGIN
 SELECT RAISE(ABORT, 'bank import evidence is immutable');
END;
CREATE TRIGGER bank_import_job_lifecycle BEFORE UPDATE ON bank_import_jobs BEGIN
 SELECT CASE WHEN NEW.id <> OLD.id OR NEW.book_id <> OLD.book_id OR NEW.upload_key <> OLD.upload_key
  OR NEW.request_sha256 <> OLD.request_sha256 OR NEW.source_name <> OLD.source_name OR NEW.source_bytes <> OLD.source_bytes
  OR NEW.source_sha256 <> OLD.source_sha256 OR NEW.created_at <> OLD.created_at OR NEW.created_by <> OLD.created_by
  THEN RAISE(ABORT, 'bank import source evidence is immutable') END;
 SELECT CASE WHEN NOT ((OLD.status = 'UPLOADED' AND NEW.status IN ('READY','FAILED')) OR (OLD.status = 'READY' AND NEW.status = 'APPLIED'))
  THEN RAISE(ABORT, 'invalid bank import job transition') END;
 SELECT CASE WHEN OLD.status = 'READY' AND (NEW.parser_version IS NOT OLD.parser_version OR NEW.document_json IS NOT OLD.document_json OR NEW.document_sha256 IS NOT OLD.document_sha256)
  THEN RAISE(ABORT, 'parsed evidence is immutable') END;
 SELECT CASE WHEN NEW.status = 'APPLIED' AND NOT EXISTS (SELECT 1 FROM bank_import_plans p WHERE p.id = NEW.applied_plan_id AND p.job_id = NEW.id)
  THEN RAISE(ABORT, 'applied plan must belong to the job') END;
END;
