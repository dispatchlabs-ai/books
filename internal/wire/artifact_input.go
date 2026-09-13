package wire

import (
	"context"
	"encoding/json"
	"github.com/dispatchlabs-ai/books/internal/application"
	"github.com/dispatchlabs-ai/books/internal/artifact"
	"reflect"
)

// DecodeOperation accepts an inline request or a single scoped artifact reference.
// The caller must bind the authenticated actor and verified company/database.
func DecodeOperation(ctx context.Context, data []byte, value any) error {
	var fields map[string]json.RawMessage
	if err := application.DecodeRequest(data, &fields); err != nil {
		return err
	}
	if _, ok := fields["input_artifact"]; !ok {
		return Decode(data, value)
	}
	var ref struct {
		ID string `json:"input_artifact"`
	}
	if err := application.DecodeRequest(data, &ref); err != nil {
		return err
	}
	contents, err := artifact.Bytes(ctx, ref.ID, artifact.MaxFile)
	if err != nil {
		return err
	}
	return decodeLimit(contents, value, int(artifact.MaxFile))
}
func OperationInputSchema(t reflect.Type) map[string]any {
	return map[string]any{"anyOf": []any{Schema(t), map[string]any{"type": "object", "required": []string{"input_artifact"}, "properties": map[string]any{"input_artifact": map[string]any{"type": "string", "description": "Completed JSON artifact owned by this actor and selected company/database."}}, "additionalProperties": false}}}
}
