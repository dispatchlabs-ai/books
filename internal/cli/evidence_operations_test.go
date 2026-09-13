package cli

import (
	"context"
	"encoding/json"
	"github.com/dispatchlabs-ai/books/internal/application"
	"github.com/dispatchlabs-ai/books/internal/artifact"
	"github.com/dispatchlabs-ai/books/internal/operations"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQuickBooksBundleOperations(t *testing.T) {
	home := setupHumanCLIHome(t, "2023-01-01", "empty")
	ctx := artifact.WithRoot(context.Background(), filepath.Join(home, "artifacts"))
	app, err := application.Open(ctx, filepath.Join(home, "books.toml"), "acme", "bundle-test", storesqlite.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Close() }()
	files := []artifact.File{}
	for _, name := range []string{"general_ledger.json", "accounts.json"} {
		data, err := os.ReadFile(filepath.Join("..", "importer", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		ref, err := artifact.Put(artifact.Bind(ctx, "bundle-test", "company:"+app.Identity()), name, data)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, artifact.File{Name: name, Artifact: ref.ID})
	}
	access := operations.CompanyAccess("bundle-test", "acme", []string{"read", "import", "post", "manage"})
	planOp, _ := operations.LookupCompanyOperation("import_quickbooks_plan")
	value, err := planOp.Invoke(ctx, app, access, &operations.QuickBooksPlanRequest{Files: files, From: "general_ledger.json", Accounts: "accounts.json"})
	if err != nil {
		t.Fatal(err)
	}
	plan := value.(application.QuickBooksPlan)
	if !plan.Ready || strings.Contains(plan.Source.Path, home) || filepath.IsAbs(plan.Source.Path) {
		t.Fatal("plan is not a logical-file plan", plan.Source)
	}
	apply, _ := operations.LookupCompanyOperation("import_quickbooks_apply")
	request := operations.QuickBooksApplyRequest{Files: files, Plan: plan, DryRun: true}
	if _, err = apply.Invoke(ctx, app, access, &request); err != nil {
		t.Fatal("preview", err)
	}
	request.DryRun = false
	var batch string
	for i := 0; i < 2; i++ {
		result, err := apply.Invoke(ctx, app, access, &request)
		if err != nil {
			t.Fatal("apply", err)
		}
		output := result.(application.QuickBooksResult)
		if output.Status != "POSTED" {
			t.Fatal(output)
		}
		if i > 0 && output.BatchID != batch {
			t.Fatal("retry duplicated import")
		}
		batch = output.BatchID
	}
	request.Plan.Source.Path = "/etc/passwd"
	if _, err = apply.Invoke(ctx, app, access, &request); err == nil {
		t.Fatal("edited plan accepted")
	}
	for _, file := range files {
		if _, err = artifact.Discard(artifact.Bind(ctx, "bundle-test", "company:"+app.Identity()), file.Artifact); err == nil {
			t.Fatal("consumed QuickBooks source discarded")
		}
	}
	executeHumanJSON(t, "doctor")
}
func TestPrecoverageArtifactOperation(t *testing.T) {
	path := accountIdentityCLIDatabase(t)
	inputPath, _, _ := precoverageCLIInput(t, path)
	store, err := storesqlite.Open(context.Background(), path, storesqlite.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	db, err := application.BindDatabase(context.Background(), "example", store, "")
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	root := t.TempDir()
	_ = os.Chmod(root, 0700)
	ctx := artifact.WithRoot(context.Background(), root)
	bound := artifact.Bind(ctx, "lifecycle-test", "database:"+db.Identity())
	raw, err := os.ReadFile(inputPath)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err = json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"closure_evidence", "zero_evidence"} {
		evidence := document[key].(map[string]any)
		path := evidence["source_path"].(string)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		ref, err := artifact.Put(bound, filepath.Base(path), data)
		if err != nil {
			t.Fatal(err)
		}
		evidence["source_path"] = "/artifacts/" + ref.ID
	}
	raw, err = json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := artifact.Put(bound, "lifecycle.json", raw)
	if err != nil {
		t.Fatal(err)
	}
	op, _ := operations.LookupDatabaseOperation("statement_account_lifecycle_close_before_coverage")
	access := operations.ScopedDatabaseAccess("lifecycle-test", "example", []string{"read", "manage"})
	request := operations.PrecoverageRequest{Input: ref.ID}
	preview, err := op.Execute(ctx, db, access, &request)
	if err != nil {
		t.Fatal(err)
	}
	if preview.(operations.PrecoverageResult).Committed {
		t.Fatal("preview committed")
	}
	request.Commit = true
	for i := 0; i < 2; i++ {
		value, err := op.Execute(ctx, db, access, &request)
		if err != nil {
			t.Fatal(err)
		}
		if !value.(operations.PrecoverageResult).Committed {
			t.Fatal("commit omitted")
		}
	}
	if _, err = artifact.Discard(bound, ref.ID); err == nil {
		t.Fatal("committed lifecycle evidence discarded")
	}
}
