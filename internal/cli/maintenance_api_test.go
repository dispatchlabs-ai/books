package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/application"
	booksconfig "github.com/dispatchlabs-ai/books/internal/config"
	"github.com/dispatchlabs-ai/books/internal/httpapi"
	"github.com/dispatchlabs-ai/books/internal/mcpserver"
	"github.com/dispatchlabs-ai/books/internal/operations"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMaintenanceAdapters(t *testing.T) {
	home := setupHumanCLIHome(t, "2026-01-01", "starter")
	executeHumanJSON(t, "account", "add", "bank", "Checking")
	executeHumanJSON(t, "receive", "100.00", "Revenue", "--date", "2026-01-15", "--key", "before-backup")
	configPath := filepath.Join(home, "books.toml")
	cfg, err := booksconfig.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	company, err := cfg.Resolve(configPath, "acme")
	if err != nil {
		t.Fatal(err)
	}
	target := application.NewDatabaseTarget("example", company.Database, company.Company.DatabaseUUID)
	for _, op := range operations.MaintenanceOperations() {
		for _, access := range []operations.DatabaseAccess{operations.ScopedDatabaseAccess("owner", "example", []string{"read", "manage"}), operations.ScopedDatabaseAccess("owner", "other", []string{"read", "manage", "admin"})} {
			_, err := op.Execute(context.Background(), target, access, op.NewInput())
			e, ok := apperr.As(err)
			if !ok || e.Code != "DATABASE_NOT_FOUND" {
				t.Fatal(op.Descriptor().ID, "admin scope bypass", err)
			}
		}
	}
	root := filepath.Join(home, "artifacts")
	token := strings.Repeat("m", 32)
	sum := sha256.Sum256([]byte(token))
	server, err := httpapi.New(context.Background(), configPath, httpapi.Config{Schema: "books.server/v4", Listen: "127.0.0.1:0", ArtifactDirectory: root, Databases: map[string]httpapi.DatabaseConfig{"example": {Path: company.Database, UUID: company.Company.DatabaseUUID}, "fresh": {Path: filepath.Join(filepath.Dir(company.Database), "fresh.sqlite")}}, Principals: []httpapi.Principal{{ID: "owner", TokenSHA256: hex.EncodeToString(sum[:]), Databases: map[string][]string{"example": {"read", "manage", "admin"}, "fresh": {"read", "manage", "admin"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = server.Close() }()
	callDatabase := func(handle, id string, input any) map[string]any {
		t.Helper()
		data, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest("POST", "/v1/databases/"+handle+"/operations/"+id, bytes.NewReader(data))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != 200 {
			t.Fatal(id, response.Code, response.Body.String())
		}
		var out map[string]any
		if err = json.Unmarshal(response.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out["data"].(map[string]any)
	}
	call := func(id string, input any) map[string]any { return callDatabase("example", id, input) }
	initialized := callDatabase("fresh", "db_init", map[string]any{"currency": "USD"})
	if initialized["database_id"] == "" {
		t.Fatal("initialization did not return identity")
	}
	callDatabase("fresh", "db_doctor", map[string]any{})
	freshBackup := callDatabase("fresh", "db_backup", map[string]any{"key": "checkpoint"})
	ms, err := mcpserver.New(context.Background(), mcpserver.Policy{Schema: "books.mcp-policy/v1", Actor: "owner", ArtifactDirectory: root, Databases: map[string]mcpserver.DatabasePolicy{"example": {Path: company.Database, UUID: company.Company.DatabaseUUID, Grants: []string{"read", "manage", "admin"}}}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ms.Close() }()
	left, right := mcp.NewInMemoryTransports()
	ss, err := ms.MCP.Connect(context.Background(), left, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ss.Close() }()
	client := mcp.NewClient(&mcp.Implementation{Name: "maintenance-test", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), right, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	backup, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "books_db_db_backup", Arguments: map[string]any{"database": "example", "input": map[string]any{"key": "checkpoint"}}})
	if err != nil || backup.IsError {
		t.Fatalf("backup %v %+v", err, backup)
	}
	saved := backup.StructuredContent.(map[string]any)["result"].(map[string]any)
	if saved["artifact"].(map[string]any)["id"] == freshBackup["artifact"].(map[string]any)["id"] {
		t.Fatal("neighboring database backups collided")
	}
	id := saved["artifact"].(map[string]any)["id"].(string)
	retry := call("db_backup", map[string]any{"key": "checkpoint"})
	if !reflect.DeepEqual(retry, saved) {
		t.Fatal("backup retry changed artifact")
	}
	executeHumanJSON(t, "receive", "5.00", "Revenue", "--date", "2026-01-16", "--key", "after-backup")
	call("db_restore", map[string]any{"artifact": id, "dry_run": true})
	call("db_restore", map[string]any{"artifact": id, "confirm": "example"})
	tb, _ := executeHumanJSON(t, "tb", "--as-of", "2026-01-31")
	if tb["data"].(map[string]any)["total_debit_cents"] != "100.00" {
		t.Fatal("restore did not replace ledger", tb)
	}
	call("db_migrate", map[string]any{"dry_run": true})
	call("db_migrate", map[string]any{})
	call("db_doctor", map[string]any{})
}
