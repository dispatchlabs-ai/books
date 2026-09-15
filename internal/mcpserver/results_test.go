package mcpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/dispatchlabs-ai/books/internal/artifact"
	"os"
	"strings"
	"testing"
)

func TestCommittedResultDeliveryFallback(t *testing.T) {
	value := strings.Repeat("x", inlineResultLimit+1)
	for _, full := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "quota"}[full], func(t *testing.T) {
			ctx := context.Background()
			if full {
				root := t.TempDir()
				_ = os.Chmod(root, 0700)
				ctx = artifact.Bind(artifact.WithRoot(ctx, root), "reader", "database:synthetic")
				sum := sha256.Sum256(nil)
				for i := 0; i < 4; i++ {
					if _, err := artifact.Begin(ctx, artifact.BeginRequest{Key: string(rune('a' + i)), Name: "reserve", Size: artifact.MaxFile, SHA256: hex.EncodeToString(sum[:])}); err != nil {
						t.Fatal(err)
					}
				}
			}
			result, err := operationResult(ctx, value, "write")
			if err != nil || result.IsError {
				t.Fatal("committed result reported failure", err, result)
			}
			got := result.StructuredContent.(map[string]any)
			if got["result"] != value || got["delivery_warning"] == nil {
				t.Fatal("lost committed result")
			}
		})
	}
}

func TestReadResultArtifactRoundTrip(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	ctx := artifact.Bind(artifact.WithRoot(context.Background(), root), "reader", "company:synthetic")
	value := map[string]any{"id": "retained-id", "valid": false, "errors": []string{"missing evidence"}, "amount": "9007199254740993", "detail": strings.Repeat("x", inlineReadTarget+1)}
	result, err := operationResult(ctx, value, "read")
	if err != nil || result.IsError {
		t.Fatal(err, result)
	}
	envelope := result.StructuredContent.(map[string]any)
	ref := envelope["artifact"].(map[string]any)
	data, err := artifact.Bytes(ctx, ref["id"].(string), inlineResultLimit)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	original := decoded["result"].(map[string]any)
	if original["id"] != "retained-id" || original["valid"] != false || original["amount"] != "9007199254740993" || original["errors"].([]any)[0] != "missing evidence" {
		t.Fatal("result data lost", original)
	}
	// Default-size chunks exceed the read target but must remain directly readable.
	chunk, err := artifact.Read(ctx, artifact.ReadRequest{ID: ref["id"].(string)})
	if err != nil {
		t.Fatal(err)
	}
	chunkResult, err := operationResult(ctx, chunk, "read")
	if err != nil || chunkResult.IsError || chunkResult.StructuredContent.(map[string]any)["result"] == nil {
		t.Fatal("recursive artifact spill", err, chunkResult)
	}
	other := artifact.Bind(artifact.WithRoot(context.Background(), root), "reader", "company:other")
	if _, err := artifact.Read(other, artifact.ReadRequest{ID: ref["id"].(string)}); err == nil {
		t.Fatal("cross-scope artifact read allowed")
	}
}

func TestReadTargetCompatibility(t *testing.T) {
	for _, size := range []int{inlineReadTarget - 100, inlineReadTarget + 100, inlineResultLimit + 100} {
		result, err := operationResult(context.Background(), strings.Repeat("x", size), "read")
		if err != nil {
			t.Fatal(err)
		}
		if result.IsError != (size > inlineResultLimit) {
			t.Fatalf("unexpected artifact-disabled result for %d", size)
		}
	}
	// Mutation IDs, validation and status remain inline above the read target.
	result, err := operationResult(context.Background(), map[string]any{"id": "committed", "status": "POSTED", "detail": strings.Repeat("x", inlineReadTarget+100)}, "write")
	if err != nil || result.IsError || result.StructuredContent.(map[string]any)["result"].(map[string]any)["status"] != "POSTED" {
		t.Fatal("mutation receipt lost", err, result)
	}
}
