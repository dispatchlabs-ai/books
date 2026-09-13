package operations

import (
	"context"
	"github.com/dispatchlabs-ai/books/internal/application"
	"github.com/dispatchlabs-ai/books/internal/artifact"
)

func companyArtifactOperations() []CompanyOperation {
	return []CompanyOperation{
		companyOp("artifact_discard", "read", "write", func(c context.Context, _ *application.Service, _ Access, r IDRequest) (artifact.Reference, error) {
			return artifact.Discard(c, r.ID)
		}),
		companyOp("artifact_begin", "import", "write", func(c context.Context, _ *application.Service, _ Access, r artifact.BeginRequest) (artifact.Reference, error) {
			return artifact.Begin(c, r)
		}),
		companyOp("artifact_write", "import", "write", func(c context.Context, _ *application.Service, _ Access, r artifact.ChunkRequest) (artifact.Reference, error) {
			return artifact.Write(c, r)
		}),
		companyOp("artifact_finish", "import", "write", func(c context.Context, _ *application.Service, _ Access, r IDRequest) (artifact.Reference, error) {
			return artifact.Finish(c, r.ID)
		}),
		companyOp("artifact_read", "read", "read", func(c context.Context, _ *application.Service, _ Access, r artifact.ReadRequest) (artifact.Chunk, error) {
			return artifact.Read(c, r)
		}),
	}
}
func databaseArtifactOperations() []DatabaseOperation {
	return []DatabaseOperation{
		databaseOpWithEffect("artifact_discard", "read", "write", func(c context.Context, _ *application.Database, _ string, r IDRequest) (artifact.Reference, error) {
			return artifact.Discard(c, r.ID)
		}),
		databaseOp("artifact_begin", "manage", func(c context.Context, _ *application.Database, _ string, r artifact.BeginRequest) (artifact.Reference, error) {
			return artifact.Begin(c, r)
		}),
		databaseOp("artifact_write", "manage", func(c context.Context, _ *application.Database, _ string, r artifact.ChunkRequest) (artifact.Reference, error) {
			return artifact.Write(c, r)
		}),
		databaseOp("artifact_finish", "manage", func(c context.Context, _ *application.Database, _ string, r IDRequest) (artifact.Reference, error) {
			return artifact.Finish(c, r.ID)
		}),
		databaseOp("artifact_read", "read", func(c context.Context, _ *application.Database, _ string, r artifact.ReadRequest) (artifact.Chunk, error) {
			return artifact.Read(c, r)
		}),
	}
}
