package cli

import (
	"encoding/json"
	"io"
	"os"

	"github.com/dispatchlabs-ai/books/internal/apperr"
)

func readJSONInput(path string, destination any) error {
	var reader io.Reader
	var file *os.File
	if path == "-" {
		reader = os.Stdin
	} else {
		var err error
		file, err = os.Open(path)
		if err != nil {
			return apperr.Wrap(apperr.Input, "INPUT_OPEN_FAILED", "open input file", err)
		}
		defer func(closer interface{ Close() error }) { _ = closer.Close() }(file)
		reader = file
	}
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return apperr.Wrap(apperr.Input, "INPUT_JSON_INVALID", "decode JSON input", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return apperr.New(apperr.Input, "INPUT_JSON_INVALID", "input contains multiple JSON values")
		}
		return apperr.Wrap(apperr.Input, "INPUT_JSON_INVALID", "decode JSON input", err)
	}
	return nil
}
