package budget

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/dispatchlabs-ai/books/internal/apperr"
)

// Planning documents are separate from immutable accounting. Each revision is
// retained; atomic replacement and a process lock prevent lost concurrent edits.
type stored struct {
	Identity string `json:"identity"`
	Actor    string `json:"actor"`
	At       string `json:"at"`
	Plan     Plan   `json:"plan"`
}

func filename(root, identity string) string {
	hash := sha256.Sum256([]byte(identity))
	return filepath.Join(root, hex.EncodeToString(hash[:]))
}
func Load(root, identity string) (Plan, error) {
	raw, e := os.ReadFile(filename(root, identity) + ".json")
	if errors.Is(e, os.ErrNotExist) {
		return Plan{Buckets: []Bucket{}, Assignments: []Assignment{}}, nil
	}
	if e != nil {
		return Plan{}, e
	}
	var d stored
	if e = json.Unmarshal(raw, &d); e != nil {
		return Plan{}, e
	}
	if d.Identity != identity {
		return Plan{}, fmt.Errorf("budget identity mismatch")
	}
	return d.Plan, nil
}
func Save(ctx context.Context, root, identity, actor string, p Plan) (Plan, error) {
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	if e := os.MkdirAll(root, 0700); e != nil {
		return Plan{}, e
	}
	path := filename(root, identity)
	lock, e := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return Plan{}, e
	}
	defer func() { _ = lock.Close() }()
	for {
		e = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if e == nil {
			break
		}
		if !errors.Is(e, syscall.EWOULDBLOCK) {
			return Plan{}, e
		}
		select {
		case <-ctx.Done():
			return Plan{}, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) }()
	old, e := Load(root, identity)
	if e != nil {
		return Plan{}, e
	}
	if old.Revision != p.Revision {
		return Plan{}, apperr.New(apperr.Conflict, "BUDGET_CHANGED", "Budget changed in another session. Reload before saving.")
	}
	old.Revision = ""
	candidate := p
	candidate.Revision = ""
	a, _ := json.Marshal(old)
	b, _ := json.Marshal(candidate)
	if string(a) == string(b) {
		return p, nil
	}
	d := stored{Identity: identity, Actor: actor, At: time.Now().UTC().Format(time.RFC3339Nano), Plan: candidate}
	raw, e := json.Marshal(d)
	if e != nil {
		return Plan{}, e
	}
	hash := sha256.Sum256(raw)
	p.Revision = hex.EncodeToString(hash[:])
	d.Plan = p
	raw, e = json.MarshalIndent(d, "", "  ")
	if e != nil {
		return Plan{}, e
	}
	history := path + ".history"
	if e = os.MkdirAll(history, 0700); e != nil {
		return Plan{}, e
	}
	f, e := os.OpenFile(filepath.Join(history, p.Revision+".json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return Plan{}, e
	}
	_, e = f.Write(raw)
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		return Plan{}, e
	}
	if closeErr != nil {
		return Plan{}, closeErr
	}
	temp, e := os.CreateTemp(root, ".budget-*")
	if e != nil {
		return Plan{}, e
	}
	defer func() { _ = os.Remove(temp.Name()) }()
	_, e = temp.Write(raw)
	if e == nil {
		e = temp.Sync()
	}
	closeErr = temp.Close()
	if e != nil {
		return Plan{}, e
	}
	if closeErr != nil {
		return Plan{}, closeErr
	}
	if e = os.Rename(temp.Name(), path+".json"); e != nil {
		return Plan{}, e
	}
	dir, e := os.Open(root)
	if e != nil {
		return Plan{}, e
	}
	defer func() { _ = dir.Close() }()
	if e = dir.Sync(); e != nil {
		return Plan{}, e
	}
	return p, nil
}
