package artifact

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestBoundedTransfer(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	ctx := Bind(WithRoot(context.Background(), root), "alice", "company:demo")
	data := bytes.Repeat([]byte("source\n"), 400000)
	sum := sha256.Sum256(data)
	in := BeginRequest{Key: "upload-one", Name: "statement.csv", Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
	ref, err := Begin(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Begin(ctx, in)
	if err != nil || again.ID != ref.ID {
		t.Fatal("begin retry", err)
	}
	changed := in
	changed.Name = "different.csv"
	if _, err = Begin(ctx, changed); err == nil {
		t.Fatal("key conflict accepted")
	}
	if _, err = Read(ctx, ReadRequest{ID: ref.ID}); err == nil {
		t.Fatal("read incomplete")
	}
	for offset := 0; offset < len(data); offset += MaxChunk {
		end := min(offset+MaxChunk, len(data))
		req := ChunkRequest{ID: ref.ID, Offset: int64(offset), Base64: base64.StdEncoding.EncodeToString(data[offset:end])}
		if _, err = Write(ctx, req); err != nil {
			t.Fatal(err)
		}
		if _, err = Write(ctx, req); err != nil {
			t.Fatal("chunk retry", err)
		}
	}
	ref, err = Finish(ctx, ref.ID)
	if err != nil || !ref.Complete {
		t.Fatal(err)
	}
	for _, bad := range []context.Context{Bind(WithRoot(context.Background(), root), "bob", "company:demo"), Bind(WithRoot(context.Background(), root), "alice", "company:other")} {
		if _, err = Read(bad, ReadRequest{ID: ref.ID}); err == nil {
			t.Fatal("scope leak")
		}
		if _, err = Write(bad, ChunkRequest{ID: ref.ID, Base64: "YQ=="}); err == nil {
			t.Fatal("scope write")
		}
	}
	var downloaded []byte
	for offset := int64(0); ; {
		chunk, err := Read(ctx, ReadRequest{ID: ref.ID, Offset: offset})
		if err != nil {
			t.Fatal(err)
		}
		value, err := base64.StdEncoding.DecodeString(chunk.Base64)
		if err != nil {
			t.Fatal(err)
		}
		downloaded = append(downloaded, value...)
		offset += int64(len(value))
		if chunk.EOF {
			break
		}
	}
	if !bytes.Equal(downloaded, data) {
		t.Fatal("download mismatch")
	}
	if _, err = Bytes(ctx, ref.ID, 1); err == nil {
		t.Fatal("operation size limit ignored")
	}
	if _, err = Write(ctx, ChunkRequest{ID: ref.ID, Base64: "YQ=="}); err == nil {
		t.Fatal("completed file overwritten")
	}
	if _, err = Read(ctx, ReadRequest{ID: "../../outside"}); err == nil {
		t.Fatal("traversal")
	}
	if _, err = Write(ctx, ChunkRequest{ID: ref.ID, Base64: base64.StdEncoding.EncodeToString(make([]byte, MaxChunk+1))}); err == nil {
		t.Fatal("oversized chunk")
	}
	if err = os.WriteFile(filepath.Join(root, ref.ID+".data"), []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Bytes(ctx, ref.ID, MaxFile); err == nil {
		t.Fatal("digest tampering")
	}
}
func TestSymlinkAndQuota(t *testing.T) {
	root := t.TempDir()
	_ = os.Chmod(root, 0700)
	ctx := Bind(WithRoot(context.Background(), root), "alice", "database:demo")
	sum := sha256.Sum256(nil)
	for i := 0; i < 4; i++ {
		if _, err := Begin(ctx, BeginRequest{Key: string(rune('a' + i)), Name: "backup.db", Size: MaxFile, SHA256: hex.EncodeToString(sum[:])}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Begin(ctx, BeginRequest{Key: "excess", Name: "backup.db", Size: 1, SHA256: hex.EncodeToString(sum[:])}); err == nil {
		t.Fatal("quota ignored")
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Begin(Bind(WithRoot(context.Background(), link), "alice", "database:demo"), BeginRequest{Key: "test", Name: "empty", SHA256: hex.EncodeToString(sum[:])}); err == nil {
		t.Fatal("symlink root accepted")
	}
}
func TestResumePartialChunk(t *testing.T) {
	root := t.TempDir()
	_ = os.Chmod(root, 0700)
	ctx := Bind(WithRoot(context.Background(), root), "alice", "company:demo")
	data := []byte("complete chunk bytes")
	sum := sha256.Sum256(data)
	in := BeginRequest{Key: "resume", Name: "source.csv", Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
	ref, err := Begin(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate interruption after a short write, before the metadata receipt.
	if err = os.WriteFile(filepath.Join(root, ref.ID+".data"), data[:5], 0600); err != nil {
		t.Fatal(err)
	}
	ref, err = Write(ctx, ChunkRequest{ID: ref.ID, Base64: base64.StdEncoding.EncodeToString(data)})
	if err != nil || ref.Received != int64(len(data)) {
		t.Fatal("resume", ref, err)
	}
	if _, err = Finish(ctx, ref.ID); err != nil {
		t.Fatal(err)
	}
}

func TestCanceledLockWait(t *testing.T) {
	root := t.TempDir()
	_ = os.Chmod(root, 0700)
	f, err := os.OpenFile(filepath.Join(root, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()
	ctx, cancel := context.WithTimeout(Bind(WithRoot(context.Background(), root), "alice", "company:demo"), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	err = locked(ctx, func(binding) error { t.Fatal("entered held lock"); return nil })
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > time.Second {
		t.Fatal("canceled wait", err)
	}
}

func TestDiscardTombstone(t *testing.T) {
	root := t.TempDir()
	_ = os.Chmod(root, 0700)
	ctx := Bind(WithRoot(context.Background(), root), "alice", "company:demo")
	sum := sha256.Sum256(nil)
	in := BeginRequest{Key: "discard", Name: "empty", SHA256: hex.EncodeToString(sum[:])}
	ref, err := Begin(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Finish(ctx, ref.ID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if ref, err = Discard(ctx, ref.ID); err != nil || !ref.Discarded {
			t.Fatal(err)
		}
	}
	if _, err = Begin(ctx, in); err == nil {
		t.Fatal("discarded handle reused")
	}
	if _, err = Read(ctx, ReadRequest{ID: ref.ID}); err == nil {
		t.Fatal("discarded read")
	}
}
