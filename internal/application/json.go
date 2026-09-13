package application

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/dispatchlabs-ai/books/internal/apperr"
)

// DecodeRequest rejects ambiguous duplicate keys as well as unknown fields and
// trailing values. Both CLI and HTTP previews use this contract.
func DecodeRequest(data []byte, v any) error { return DecodeRequestLimit(data, v, 2<<20) }

// DecodeRequestLimit is used for already bounded artifact contents.
func DecodeRequestLimit(data []byte, v any, limit int) error {
	bad := func() error {
		return apperr.New(apperr.Input, "REQUEST_JSON_INVALID", "request must be one JSON object with known, nonduplicate fields")
	}
	if limit < 1 || len(data) > limit {
		return bad()
	}
	tokens := json.NewDecoder(bytes.NewReader(data))
	if e := walkJSON(tokens, 0); e != nil {
		return bad()
	}
	if _, e := tokens.Token(); e != io.EOF {
		return bad()
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return bad()
	}
	return nil
}
func walkJSON(d *json.Decoder, depth int) error {
	if depth > 32 {
		return io.ErrUnexpectedEOF
	}
	t, e := d.Token()
	if e != nil {
		return e
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		keys := map[string]bool{}
		for d.More() {
			key, e := d.Token()
			if e != nil {
				return e
			}
			name, ok := key.(string)
			if !ok || keys[name] {
				return io.ErrUnexpectedEOF
			}
			keys[name] = true
			if e := walkJSON(d, depth+1); e != nil {
				return e
			}
		}
	case '[':
		for d.More() {
			if e := walkJSON(d, depth+1); e != nil {
				return e
			}
		}
	default:
		return io.ErrUnexpectedEOF
	}
	_, e = d.Token()
	return e
}
