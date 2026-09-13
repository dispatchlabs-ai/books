package mcpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
