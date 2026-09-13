package mcpserver

import (
	"context"
	"encoding/json"
	"github.com/dispatchlabs-ai/books/internal/artifact"
	"github.com/dispatchlabs-ai/books/internal/wire"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"reflect"
)

const inlineResultLimit = 512 << 10

func resultSchema(typ reflect.Type) map[string]any {
	return map[string]any{"type": "object", "oneOf": []any{
		map[string]any{"required": []string{"result"}, "properties": map[string]any{"result": wire.Schema(typ), "delivery_warning": map[string]any{"type": "string"}}, "additionalProperties": false},
		map[string]any{"required": []string{"artifact"}, "properties": map[string]any{"artifact": wire.Schema(reflect.TypeFor[artifact.Reference]())}, "additionalProperties": false},
	}}
}
func operationResult(ctx context.Context, value any, effect string) (*mcp.CallToolResult, error) {
	result := map[string]any{"result": wire.EncodeValue(reflect.ValueOf(value), true)}
	data, err := json.Marshal(result)
	if err != nil {
		return failure(err), nil
	}
	if len(data) > inlineResultLimit {
		ref, err := artifact.Put(ctx, "result.json", data)
		if err != nil {
			if effect == "read" {
				return failure(err), nil
			}
			// The operation already succeeded. Preserve its result; never report a
			// bookkeeping failure merely because optional result-file delivery failed.
			result["delivery_warning"] = "Operation succeeded. Artifact delivery was unavailable; result returned inline. Do not repeat the mutation."
			data, _ = json.Marshal(result)
			return &mcp.CallToolResult{StructuredContent: result, Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil
		}
		result = map[string]any{"artifact": wire.EncodeValue(reflect.ValueOf(ref), true)}
		data, err = json.Marshal(result)
		if err != nil {
			return failure(err), nil
		}
	}
	return &mcp.CallToolResult{StructuredContent: result, Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil
}

// Registry metadata has no database artifact scope and remains inline.
func inlineOperationResult(value any) (*mcp.CallToolResult, error) {
	result := map[string]any{"result": wire.EncodeValue(reflect.ValueOf(value), true)}
	data, err := json.Marshal(result)
	if err != nil {
		return failure(err), nil
	}
	return &mcp.CallToolResult{StructuredContent: result, Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil
}
