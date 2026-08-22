// Package apperr defines stable application errors for the CLI contract.
package apperr

import (
	"errors"
	"fmt"
)

type Kind int

const (
	Invalid Kind = iota + 1
	NotFound
	Validation
	Conflict
	Integrity
	Input
	Unavailable
)

type Error struct {
	Kind    Kind
	Code    string
	Message string
	Hint    string
	Err     error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

func (e *Error) Unwrap() error { return e.Err }

func New(kind Kind, code, message string) error {
	return &Error{Kind: kind, Code: code, Message: message}
}

func Wrap(kind Kind, code, message string, err error) error {
	if err == nil {
		return nil
	}
	return &Error{Kind: kind, Code: code, Message: message, Err: err}
}

func As(err error) (*Error, bool) {
	var target *Error
	return target, errors.As(err, &target)
}
