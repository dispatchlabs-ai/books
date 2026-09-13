package sqlite

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/dispatchlabs-ai/books/internal/apperr"
	"os"
	"path/filepath"
	"slices"
	"syscall"
)

type maintenanceKey struct{}
type fileLease struct{ files []*os.File }

func (l *fileLease) Close() error {
	if l == nil {
		return nil
	}
	var errs []error
	for _, f := range l.files {
		errs = append(errs, f.Close())
	}
	l.files = nil
	return errors.Join(errs...)
}
func leaseNames(path string) ([]string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	names := []string{abs}
	// Resolve the longest existing ancestor: restore may create a directory tree.
	ancestor, tail := abs, ""
	var resolved string
	for {
		resolved, err = filepath.EvalSymlinks(ancestor)
		if err == nil {
			resolved = filepath.Join(resolved, tail)
			break
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return nil, err
		}
		tail = filepath.Join(filepath.Base(ancestor), tail)
		ancestor = parent
	}

	names = append(names, resolved)
	slices.Sort(names)
	return slices.Compact(names), nil
}
func acquireLease(ctx context.Context, path string, exclusive bool) (*fileLease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	names, err := leaseNames(path)
	if err != nil {
		return nil, err
	}
	owned, _ := ctx.Value(maintenanceKey{}).(map[string]bool)
	if !exclusive {
		all := true
		for _, name := range names {
			if !owned[name] {
				all = false
			}
		}
		if all {
			return &fileLease{}, nil
		}
	}
	// A fixed per-OS-owner runtime directory coordinates CLI and servers without
	// requiring writes beside read-only databases. Never unlink active lock files.
	root := filepath.Join("/tmp", fmt.Sprintf("books-locks-%d", os.Getuid()))
	if err := os.Mkdir(root, 0700); err != nil && !os.IsExist(err) {
		return nil, err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
		return nil, apperr.New(apperr.Unavailable, "MAINTENANCE_LOCK_INVALID", "maintenance lock directory must be private and owned by the Books user")
	}
	lease := &fileLease{}
	for _, name := range names {
		f, err := os.OpenFile(filepath.Join(root, fmt.Sprintf("%x.lock", sha256.Sum256([]byte(name)))), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
		if err != nil {
			_ = lease.Close()
			return nil, err
		}
		info, err := f.Stat()
		if err != nil {
			_ = f.Close()
			_ = lease.Close()
			return nil, err
		}
		duplicate := false
		for _, held := range lease.files {
			other, err := held.Stat()
			if err != nil {
				_ = f.Close()
				_ = lease.Close()
				return nil, err
			}
			if os.SameFile(info, other) {
				duplicate = true
				break
			}
		}
		if duplicate {
			_ = f.Close()
			continue
		}
		mode := syscall.LOCK_SH
		if exclusive {
			mode = syscall.LOCK_EX
		}
		if err = syscall.Flock(int(f.Fd()), mode|syscall.LOCK_NB); err != nil {
			_ = f.Close()
			_ = lease.Close()
			return nil, apperr.New(apperr.Conflict, "DATABASE_BUSY", "database maintenance requires all other Books connections to close; retry when they are idle")
		}
		lease.files = append(lease.files, f)
	}
	return lease, nil
}

// WithMaintenance prevents replacement/migration while another Books process
// still holds a connection, including connections opened through a symlink.
func WithMaintenance(ctx context.Context, path string, run func(context.Context) error) error {
	names, err := leaseNames(path)
	if err != nil {
		return err
	}
	owned, _ := ctx.Value(maintenanceKey{}).(map[string]bool)
	all := true
	for _, name := range names {
		if !owned[name] {
			all = false
		}
	}
	if all {
		return run(ctx)
	}

	lease, err := acquireLease(ctx, path, true)
	if err != nil {
		return err
	}
	defer func() { _ = lease.Close() }()
	names, err = leaseNames(path)
	if err != nil {
		return err
	}
	owned = map[string]bool{}
	for _, name := range names {
		owned[name] = true
	}
	return run(context.WithValue(ctx, maintenanceKey{}, owned))
}
