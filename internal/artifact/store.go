// Package artifact provides bounded, resumable, principal-and-scope-bound files.
// Clients supply opaque IDs, never server paths. Roots are operator configuration.
package artifact

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"time"

	"github.com/dispatchlabs-ai/books/internal/apperr"
)

const MaxFile = 256 << 20
const MaxChunk = 256 << 10
const MaxReserved = 1 << 30

type binding struct{ root, actor, scope string }
type contextKey struct{}

func WithRoot(ctx context.Context, root string) context.Context {
	return context.WithValue(ctx, contextKey{}, binding{root: root})
}
func Bind(ctx context.Context, actor, scope string) context.Context {
	b, _ := ctx.Value(contextKey{}).(binding)
	b.actor = actor
	b.scope = scope
	return context.WithValue(ctx, contextKey{}, b)
}

type BeginRequest struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}
type ChunkRequest struct {
	ID     string `json:"id"`
	Offset int64  `json:"offset"`
	Base64 string `json:"base64"`
}
type ReadRequest struct {
	ID     string `json:"id"`
	Offset int64  `json:"offset"`
	Length int    `json:"length"`
}
type Reference struct {
	Retained  bool   `json:"retained"`
	Discarded bool   `json:"discarded"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	Complete  bool   `json:"complete"`
	Received  int64  `json:"received"`
}
type Chunk struct {
	Reference Reference `json:"reference"`
	Offset    int64     `json:"offset"`
	Base64    string    `json:"base64"`
	EOF       bool      `json:"eof"`
}
type record struct {
	Reference
	Actor string `json:"actor"`
	Scope string `json:"scope"`
	Key   string `json:"key"`
}

var hexID = regexp.MustCompile(`^[0-9a-f]{64}$`)

func invalid(message string) error { return apperr.New(apperr.Invalid, "ARTIFACT_INVALID", message) }
func unavailable() error {
	return apperr.New(apperr.NotFound, "ARTIFACT_NOT_FOUND", "artifact is not available to this principal and scope")
}
func locked(ctx context.Context, fn func(binding) error) error {
	b, _ := ctx.Value(contextKey{}).(binding)
	if !filepath.IsAbs(b.root) || b.actor == "" || b.scope == "" {
		return apperr.New(apperr.Unavailable, "ARTIFACTS_DISABLED", "an explicit artifact directory and authenticated scope are required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(b.root, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(b.root)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return invalid("artifact directory must be private and must not be a symlink")
	}
	f, err := os.OpenFile(filepath.Join(b.root, ".lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return err
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(b)
}
func readRecord(b binding, id string) (record, error) {
	var r record
	if !hexID.MatchString(id) {
		return r, unavailable()
	}
	f, err := os.OpenFile(filepath.Join(b.root, id+".json"), os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return r, unavailable()
	}
	defer func() { _ = f.Close() }()
	if err = json.NewDecoder(io.LimitReader(f, 8192)).Decode(&r); err != nil {
		return r, err
	}
	if r.ID != id || r.Actor != b.actor || r.Scope != b.scope {
		return record{}, unavailable()
	}
	return r, nil
}
func save(b binding, r record) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(b.root, ".record-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(name, filepath.Join(b.root, r.ID+".json")); err != nil {
		return err
	}
	d, err := os.Open(b.root)
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	return d.Sync()
}
func openData(b binding, id string, flags int) (*os.File, error) {
	return os.OpenFile(filepath.Join(b.root, id+".data"), flags|syscall.O_NOFOLLOW, 0600)
}
func Begin(ctx context.Context, in BeginRequest) (Reference, error) {
	var out Reference
	if len(in.Key) < 1 || len(in.Key) > 128 || len(in.Name) < 1 || len(in.Name) > 255 || filepath.Base(in.Name) != in.Name || in.Name == "." || in.Name == ".." || bytes.ContainsAny([]byte(in.Name), "\\\x00\r\n") || in.Size < 0 || in.Size > MaxFile || !hexID.MatchString(in.SHA256) {
		return out, invalid("key, safe filename, size up to 256 MiB and lowercase SHA-256 are required")
	}
	err := locked(ctx, func(b binding) error {
		identity, _ := json.Marshal([]string{b.actor, b.scope, in.Key})
		sum := sha256.Sum256(identity)
		id := hex.EncodeToString(sum[:])
		if old, err := readRecord(b, id); err == nil {
			if old.Name != in.Name || old.Size != in.Size || old.SHA256 != in.SHA256 {
				return apperr.New(apperr.Conflict, "ARTIFACT_KEY_CONFLICT", "artifact key was already used for different content")
			}
			if old.Discarded {
				return apperr.New(apperr.Conflict, "ARTIFACT_DISCARDED", "use a new key after discarding an artifact")
			}
			out = old.Reference
			return nil
		} else if _, err := os.Lstat(filepath.Join(b.root, id+".json")); !os.IsNotExist(err) {
			return unavailable()
		}
		entries, err := os.ReadDir(b.root)
		if err != nil {
			return err
		}
		var reserved int64
		count := 0
		for _, entry := range entries {
			if filepath.Ext(entry.Name()) != ".json" {
				continue
			}
			count++
			f, err := os.OpenFile(filepath.Join(b.root, entry.Name()), os.O_RDONLY|syscall.O_NOFOLLOW, 0)
			if err != nil {
				return err
			}
			var r record
			err = json.NewDecoder(io.LimitReader(f, 8192)).Decode(&r)
			_ = f.Close()
			if err != nil {
				return err
			}
			if !r.Discarded {
				reserved += r.Size
			}
		}
		if reserved+in.Size > MaxReserved || count >= 4096 {
			return apperr.New(apperr.Unavailable, "ARTIFACT_QUOTA_EXCEEDED", "artifact store reservation limit reached")
		}
		f, err := openData(b, id, os.O_CREATE|os.O_RDWR)
		if err != nil {
			return err
		}
		_ = f.Close()
		r := record{Reference: Reference{ID: id, Name: in.Name, Size: in.Size, SHA256: in.SHA256}, Actor: b.actor, Scope: b.scope, Key: in.Key}
		if err = save(b, r); err != nil {
			return err
		}
		out = r.Reference
		return nil
	})
	return out, err
}
func Write(ctx context.Context, in ChunkRequest) (Reference, error) {
	var out Reference
	if len(in.Base64) > base64.StdEncoding.EncodedLen(MaxChunk) || in.Offset < 0 {
		return out, invalid("chunk exceeds 256 KiB or has a negative offset")
	}
	data, err := base64.StdEncoding.Strict().DecodeString(in.Base64)
	if err != nil || len(data) == 0 {
		return out, invalid("chunk must contain nonempty base64 bytes")
	}
	err = locked(ctx, func(b binding) error {
		r, err := readRecord(b, in.ID)
		if err != nil {
			return err
		}
		if r.Discarded {
			return unavailable()
		}
		if in.Offset > r.Size || int64(len(data)) > r.Size-in.Offset {
			return invalid("chunk exceeds declared size")
		}
		f, err := openData(b, in.ID, os.O_RDWR)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		info, err := f.Stat()
		if err != nil {
			return err
		}
		overlap := int64(0)
		if in.Offset < info.Size() {
			overlap = min(int64(len(data)), info.Size()-in.Offset)
			existing := make([]byte, int(overlap))
			_, err = f.ReadAt(existing, in.Offset)
			if err != nil || !bytes.Equal(existing, data[:overlap]) {
				return apperr.New(apperr.Conflict, "ARTIFACT_CHUNK_CONFLICT", "retried chunk differs from retained bytes")
			}
		}
		if overlap < int64(len(data)) {
			if r.Complete || in.Offset+overlap != info.Size() {
				return apperr.New(apperr.Conflict, "ARTIFACT_OFFSET_CONFLICT", "append chunks at the received offset")
			}
			if _, err = f.WriteAt(data[overlap:], in.Offset+overlap); err != nil {
				return err
			}
			if err = f.Sync(); err != nil {
				return err
			}
		}

		info, err = f.Stat()
		if err != nil {
			return err
		}
		r.Received = info.Size()
		if err = save(b, r); err != nil {
			return err
		}
		out = r.Reference
		return nil
	})
	return out, err
}
func Finish(ctx context.Context, id string) (Reference, error) {
	var out Reference
	err := locked(ctx, func(b binding) error {
		r, err := readRecord(b, id)
		if err != nil {
			return err
		}
		if r.Discarded {
			return unavailable()
		}
		f, err := openData(b, id, os.O_RDONLY)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		h := sha256.New()
		n, err := io.Copy(h, io.LimitReader(f, MaxFile+1))
		if err != nil {
			return err
		}
		if n != r.Size || hex.EncodeToString(h.Sum(nil)) != r.SHA256 {
			return apperr.New(apperr.Conflict, "ARTIFACT_DIGEST_MISMATCH", "retained file does not match declared size and digest")
		}
		r.Complete = true
		r.Received = n
		if err = save(b, r); err != nil {
			return err
		}
		out = r.Reference
		return nil
	})
	return out, err
}
func Read(ctx context.Context, in ReadRequest) (Chunk, error) {
	var out Chunk
	if in.Offset < 0 || in.Length < 0 || in.Length > MaxChunk {
		return out, invalid("read length must be at most 256 KiB and offset nonnegative")
	}
	if in.Length == 0 {
		in.Length = MaxChunk
	}
	err := locked(ctx, func(b binding) error {
		r, err := readRecord(b, in.ID)
		if err != nil {
			return err
		}
		if r.Discarded {
			return unavailable()
		}
		if !r.Complete {
			return apperr.New(apperr.Conflict, "ARTIFACT_INCOMPLETE", "finish and verify the artifact first")
		}
		if in.Offset > r.Size {
			return invalid("offset exceeds artifact size")
		}
		f, err := openData(b, in.ID, os.O_RDONLY)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		data := make([]byte, in.Length)
		n, err := f.ReadAt(data, in.Offset)
		if err != nil && err != io.EOF {
			return err
		}
		out = Chunk{Reference: r.Reference, Offset: in.Offset, Base64: base64.StdEncoding.EncodeToString(data[:n]), EOF: in.Offset+int64(n) == r.Size}
		return nil
	})
	return out, err
}
func Bytes(ctx context.Context, id string, limit int64) ([]byte, error) {
	var out []byte
	err := locked(ctx, func(b binding) error {
		r, err := readRecord(b, id)
		if err != nil {
			return err
		}
		if r.Discarded {
			return unavailable()
		}
		if !r.Complete {
			return apperr.New(apperr.Conflict, "ARTIFACT_INCOMPLETE", "finish and verify the artifact first")
		}
		if r.Size > limit {
			return invalid("artifact exceeds this operation's size limit")
		}
		f, err := openData(b, id, os.O_RDONLY)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		out, err = io.ReadAll(io.LimitReader(f, limit+1))
		if err != nil {
			return err
		}
		sum := sha256.Sum256(out)
		if int64(len(out)) != r.Size || hex.EncodeToString(sum[:]) != r.SHA256 {
			return apperr.New(apperr.Conflict, "ARTIFACT_DIGEST_MISMATCH", "retained artifact changed")
		}
		return nil
	})
	return out, err
}

// Discard releases file storage and retains a tombstone so handles are never reused.
func Discard(ctx context.Context, id string) (Reference, error) {
	var out Reference
	err := locked(ctx, func(b binding) error {
		r, err := readRecord(b, id)
		if err != nil {
			return err
		}
		if r.Retained {
			return apperr.New(apperr.Conflict, "ARTIFACT_RETAINED", "artifact supports accounting evidence and cannot be discarded")
		}
		if err = os.Remove(filepath.Join(b.root, id+".data")); err != nil && !os.IsNotExist(err) {
			return err
		}
		r.Discarded = true
		r.Complete = false
		r.Received = 0
		if err = save(b, r); err != nil {
			return err
		}
		out = r.Reference
		return nil
	})
	return out, err
}

// Put retains a generated result using the same immutable transfer contract.
func Put(ctx context.Context, name string, data []byte) (Reference, error) {
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	ref, err := Begin(ctx, BeginRequest{Key: "generated-" + digest, Name: name, Size: int64(len(data)), SHA256: digest})
	if e, ok := apperr.As(err); ok && e.Code == "ARTIFACT_DISCARDED" {
		nonce := make([]byte, 16)
		if _, err = rand.Read(nonce); err != nil {
			return ref, err
		}
		ref, err = Begin(ctx, BeginRequest{Key: "generated-" + hex.EncodeToString(nonce) + digest, Name: name, Size: int64(len(data)), SHA256: digest})
	}

	if err != nil {
		return ref, err
	}
	for offset := 0; offset < len(data); offset += MaxChunk {
		ref, err = Write(ctx, ChunkRequest{ID: ref.ID, Offset: int64(offset), Base64: base64.StdEncoding.EncodeToString(data[offset:min(offset+MaxChunk, len(data))])})
		if err != nil {
			return ref, err
		}
	}
	return Finish(ctx, ref.ID)
}

// Retain prevents temporary-file cleanup from removing accounting evidence.
// Call before committing an operation that references these source bytes.
func Retain(ctx context.Context, ids []string) error {
	return locked(ctx, func(b binding) error {
		records := make([]record, 0, len(ids))
		for _, id := range ids {
			r, err := readRecord(b, id)
			if err != nil {
				return err
			}
			if !r.Complete || r.Discarded {
				return unavailable()
			}
			records = append(records, r)
		}
		for _, r := range records {
			r.Retained = true
			if err := save(b, r); err != nil {
				return err
			}
		}
		return nil
	})
}
