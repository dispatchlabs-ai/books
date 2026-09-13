package httpapi

import (
	"io"
	"mime"
	"mime/multipart"
	"net/http"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/application"
	"github.com/dispatchlabs-ai/books/internal/banking"
)

func readStatementUpload(w http.ResponseWriter, r *http.Request) ([]byte, string, banking.Options, error) {
	var options banking.Options
	name := r.URL.Query().Get("name")
	fail := func(code, message string) ([]byte, string, banking.Options, error) {
		return nil, "", options, apperr.New(apperr.Input, code, message)
	}
	kind, params, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil {
		return fail("CONTENT_TYPE_INVALID", "upload requires application/octet-stream or multipart/form-data")
	}
	if kind == "application/octet-stream" {
		data, e := io.ReadAll(http.MaxBytesReader(w, r.Body, banking.MaxBytes))
		if e != nil {
			return fail("IMPORT_SIZE_INVALID", "upload exceeds the byte limit or could not be read")
		}
		return data, name, options, nil
	}
	if kind != "multipart/form-data" || params["boundary"] == "" {
		return fail("CONTENT_TYPE_INVALID", "upload requires application/octet-stream or multipart/form-data")
	}
	reader := multipart.NewReader(http.MaxBytesReader(w, r.Body, banking.MaxBytes+banking.MaxOptionsBytes+(64<<10)), params["boundary"])
	seen := map[string]bool{}
	var data []byte
	for {
		part, e := reader.NextPart()
		if e == io.EOF {
			break
		}
		if e != nil {
			return fail("IMPORT_MULTIPART_INVALID", "invalid or oversized multipart upload")
		}
		field := part.FormName()
		if seen[field] || (field != "file" && field != "options") {
			_ = part.Close()
			return fail("IMPORT_MULTIPART_INVALID", "provide exactly one file and at most one options field")
		}
		seen[field] = true
		limit := banking.MaxBytes
		if field == "options" {
			limit = banking.MaxOptionsBytes
		}
		value, e := io.ReadAll(io.LimitReader(part, int64(limit)+1))
		if e != nil || len(value) > limit {
			_ = part.Close()
			return fail("IMPORT_SIZE_INVALID", "multipart field exceeds its byte limit")
		}
		if field == "file" {
			data = value
			if name == "" {
				name = part.FileName()
			}
		} else {
			if e = application.DecodeRequest(value, &options); e != nil {
				_ = part.Close()
				return nil, "", options, e
			}
		}
		if e = part.Close(); e != nil {
			return fail("IMPORT_MULTIPART_INVALID", "multipart field could not be read")
		}
	}
	if !seen["file"] {
		return fail("IMPORT_MULTIPART_INVALID", "a file field is required")
	}
	return data, name, options, nil
}
