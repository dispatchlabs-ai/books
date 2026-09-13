package operations

import (
	"context"
	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/application"
	"github.com/dispatchlabs-ai/books/internal/artifact"
	"reflect"
	"slices"
	"strings"
)

type MaintenanceOperation interface {
	Descriptor() Descriptor
	NewInput() any
	Execute(context.Context, *application.DatabaseTarget, DatabaseAccess, any) (any, error)
}
type maintenanceOperation[I, O any] struct {
	descriptor Descriptor
	run        func(context.Context, *application.DatabaseTarget, string, I) (O, error)
}

func (o maintenanceOperation[I, O]) Descriptor() Descriptor { return o.descriptor }
func (o maintenanceOperation[I, O]) NewInput() any          { return new(I) }
func (o maintenanceOperation[I, O]) Execute(c context.Context, t *application.DatabaseTarget, a DatabaseAccess, input any) (any, error) {
	if t == nil || strings.TrimSpace(a.actor) == "" || (!a.local && (a.key != t.Key() || !slices.Contains(a.grants, "read") || !slices.Contains(a.grants, "admin"))) {
		return nil, apperr.New(apperr.NotFound, "DATABASE_NOT_FOUND", "database administration is not available to this principal")
	}
	r, ok := input.(*I)
	if !ok || r == nil {
		return nil, apperr.New(apperr.Invalid, "OPERATION_INPUT_INVALID", "operation input type does not match")
	}
	if err := c.Err(); err != nil {
		return nil, err
	}
	identity, err := t.ArtifactIdentity(c)
	if err != nil {
		return nil, err
	}
	c = artifact.Bind(c, a.actor, "database:"+identity)
	return o.run(c, t, a.actor, *r)
}
func maintenanceOp[I, O any](id string, run func(context.Context, *application.DatabaseTarget, string, I) (O, error)) MaintenanceOperation {
	return maintenanceOperation[I, O]{Descriptor{ID: id, Version: 1, Scope: "database", Grant: "admin", Effect: "write", Input: reflect.TypeFor[I](), Output: reflect.TypeFor[O]()}, run}
}

type InitializeRequest struct {
	Currency string `json:"currency"`
}
type MigrateRequest struct {
	DryRun bool `json:"dry_run"`
}
type BackupRequest struct {
	Key string `json:"key"`
}
type RestoreRequest struct {
	Artifact string `json:"artifact"`
	Confirm  string `json:"confirm"`
	DryRun   bool   `json:"dry_run"`
}

func MaintenanceOperations() []MaintenanceOperation {
	return []MaintenanceOperation{
		maintenanceOp("db_init", func(c context.Context, t *application.DatabaseTarget, a string, r InitializeRequest) (application.MaintenanceStatus, error) {
			if r.Currency == "" {
				r.Currency = "USD"
			}
			return t.Initialize(c, a, r.Currency)
		}),
		maintenanceOp("db_migrate", func(c context.Context, t *application.DatabaseTarget, _ string, r MigrateRequest) (application.MaintenanceStatus, error) {
			return t.Migrate(c, r.DryRun)
		}),
		maintenanceOp("db_backup", func(c context.Context, t *application.DatabaseTarget, a string, r BackupRequest) (application.DatabaseBackup, error) {
			return t.Backup(c, a, r.Key)
		}),
		maintenanceOp("db_restore", func(c context.Context, t *application.DatabaseTarget, a string, r RestoreRequest) (application.DatabaseRestore, error) {
			return t.Restore(c, a, r.Artifact, r.Confirm, r.DryRun)
		}),
	}
}
func LookupMaintenanceOperation(id string) (MaintenanceOperation, bool) {
	for _, op := range MaintenanceOperations() {
		if op.Descriptor().ID == id {
			return op, true
		}
	}
	return nil, false
}
