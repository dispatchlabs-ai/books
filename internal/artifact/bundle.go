package artifact

import (
	"bytes"
	"context"
	"io/fs"
	"strings"
	"time"
)

// File names are logical members of an immutable bundle, never OS paths.
type File struct {
	Name     string `json:"name"`
	Artifact string `json:"artifact"`
}
type bundle map[string][]byte

func Bundle(ctx context.Context, files []File) (fs.FS, error) {
	if len(files) < 1 || len(files) > 256 {
		return nil, invalid("bundle requires 1 to 256 named files")
	}
	result := bundle{}
	var size int64
	for _, file := range files {
		if !fs.ValidPath(file.Name) || file.Name == "." || len(file.Name) > 512 || strings.Contains(file.Name, "\\") {
			return nil, invalid("bundle member name must be a relative logical path")
		}
		if _, ok := result[file.Name]; ok {
			return nil, invalid("duplicate bundle member")
		}
		for name := range result {
			if strings.HasPrefix(name, file.Name+"/") || strings.HasPrefix(file.Name, name+"/") {
				return nil, invalid("bundle file/directory collision")
			}
		}
		data, err := Bytes(ctx, file.Artifact, MaxFile-size)
		if err != nil {
			return nil, err
		}
		size += int64(len(data))
		result[file.Name] = data
	}
	return result, nil
}
func (b bundle) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	if data, ok := b[name]; ok {
		return &bundleFile{Reader: *bytes.NewReader(data), info: bundleInfo{name: name, size: int64(len(data))}}, nil
	}
	for key := range b {
		if name == "." || strings.HasPrefix(key, name+"/") {
			return &bundleFile{info: bundleInfo{name: name, dir: true}}, nil
		}
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

type bundleFile struct {
	bytes.Reader
	info bundleInfo
}

func (f *bundleFile) Stat() (fs.FileInfo, error) { return f.info, nil }
func (f *bundleFile) Close() error               { return nil }

type bundleInfo struct {
	name string
	size int64
	dir  bool
}

func (i bundleInfo) Name() string { return i.name }
func (i bundleInfo) Size() int64  { return i.size }
func (i bundleInfo) Mode() fs.FileMode {
	if i.dir {
		return fs.ModeDir | 0500
	}
	return 0400
}
func (i bundleInfo) ModTime() time.Time { return time.Time{} }
func (i bundleInfo) IsDir() bool        { return i.dir }
func (i bundleInfo) Sys() any           { return nil }

// Sources exposes only authorized artifact IDs in an absolute logical namespace.
// These names are not paths on the server filesystem.
func Sources(ctx context.Context) fs.FS { return sourceFiles{ctx} }

type sourceFiles struct{ ctx context.Context }

func (s sourceFiles) Open(name string) (fs.File, error) {
	id := strings.TrimPrefix(name, "/artifacts/")
	if name == id || !hexID.MatchString(id) {
		return nil, unavailable()
	}
	data, err := Bytes(s.ctx, id, MaxFile)
	if err != nil {
		return nil, err
	}
	return &bundleFile{Reader: *bytes.NewReader(data), info: bundleInfo{name: name, size: int64(len(data))}}, nil
}
