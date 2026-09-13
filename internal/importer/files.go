package importer

import (
	"context"
	"io/fs"
	"os"
)

// LocalFiles preserves trusted CLI paths. Remote callers supply a closed fs.FS
// containing only their already-authorized immutable bundle members.
type LocalFiles struct{}

func (LocalFiles) Open(name string) (fs.File, error) { return os.Open(name) }
func BuildFromFS(ctx context.Context, input Request, files fs.FS) (Plan, error) {
	return build(ctx, input, files)
}
