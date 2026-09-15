package mcpserver

import (
	"github.com/dispatchlabs-ai/books/internal/operations"
	"strings"
	"testing"
)

func TestGuidanceCoversCatalog(t *testing.T) {
	var descriptors []operations.Descriptor
	for _, op := range operations.CompanyOperations() {
		descriptors = append(descriptors, op.Descriptor())
	}
	for _, op := range operations.DatabaseOperations() {
		descriptors = append(descriptors, op.Descriptor())
	}
	for _, op := range operations.RegistryOperations() {
		descriptors = append(descriptors, op.Descriptor())
	}
	for _, op := range operations.MaintenanceOperations() {
		descriptors = append(descriptors, op.Descriptor())
	}
	used := map[string]bool{}
	for _, d := range descriptors {
		used[d.ID] = true
		if strings.TrimSpace(operationDescriptions[d.ID]) == "" {
			t.Errorf("missing intent for %s/%s", d.Scope, d.ID)
		}
		text := toolDescription(d)
		if !strings.Contains(text, "Requires "+d.Grant) {
			t.Errorf("missing grant for %s", d.ID)
		}
		if d.Scope == "database" && !strings.Contains(text, "every entity") {
			t.Errorf("missing database scope for %s", d.ID)
		}
	}
	for id := range operationDescriptions {
		if !used[id] {
			t.Errorf("guidance for nonexistent operation %s", id)
		}
	}
}
