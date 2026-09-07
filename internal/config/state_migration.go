package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/fileutil"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/product"
)

const v11MigrationMarker = ".migration-v11.json"

var ErrMigrationBusy = errors.New("state migration is already in progress")

// MigrationBusyError reports an active migration lock. Callers can use
// errors.Is(err, ErrMigrationBusy) to distinguish a retryable busy state from
// an I/O failure.
type MigrationBusyError struct {
	Path      string
	PID       int
	CreatedAt time.Time
}

func (e *MigrationBusyError) Error() string {
	if e == nil {
		return ErrMigrationBusy.Error()
	}
	if e.PID > 0 {
		return fmt.Sprintf("%s: pid %d still owns %s", ErrMigrationBusy, e.PID, e.Path)
	}
	return fmt.Sprintf("%s: %s", ErrMigrationBusy, e.Path)
}

func (e *MigrationBusyError) Unwrap() error { return ErrMigrationBusy }

// MigrationConflictError reports destination files that already contain
// divergent data. They are preserved; the marker is intentionally not written
// so an operator can resolve the paths and retry.
type MigrationConflictError struct {
	Paths []string
}

func (e *MigrationConflictError) Error() string {
	if e == nil || len(e.Paths) == 0 {
		return "state migration has unresolved destination conflicts"
	}
	return fmt.Sprintf("state migration has unresolved destination conflicts: %s", strings.Join(e.Paths, ", "))
}

type v11MigrationRecord struct {
	Version    int       `json:"version"`
	MigratedAt time.Time `json:"migratedAt"`
	LegacyRoot string    `json:"legacyRoot"`
	NewRoot    string    `json:"newRoot"`
}

// EnsureV11StateMigration copies the V2 user tree into the V3 identity once.
// Existing V3 files always win and the legacy tree is retained as the backup.
func EnsureV11StateMigration() error {
	dir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	legacyRoot := filepath.Join(dir, product.LegacyConfigDirName)
	newRoot := filepath.Join(dir, product.ConfigDirName)
	if _, err := os.Stat(legacyRoot); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	markerPath := filepath.Join(newRoot, v11MigrationMarker)
	if migrationMarkerValid(markerPath) {
		return nil
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	lockPath := filepath.Join(dir, ".orca-v11-migration.lock")
	release, acquired, err := acquireMigrationLock(lockPath)
	if err != nil {
		return err
	}
	if !acquired {
		return &MigrationBusyError{Path: lockPath}
	}
	defer release()
	// A corrupt marker is recoverable state. Remove it before the replacement
	// write; on Windows rename does not replace an existing destination.
	if !migrationMarkerValid(markerPath) {
		if _, statErr := os.Stat(markerPath); statErr == nil {
			if removeErr := os.Remove(markerPath); removeErr != nil {
				return removeErr
			}
		}
	} else {
		return nil
	}

	if err := copyTreeMissing(legacyRoot, newRoot); err != nil {
		return fmt.Errorf("migrate V2 state: %w", err)
	}
	record := v11MigrationRecord{Version: 11, MigratedAt: time.Now().UTC(), LegacyRoot: legacyRoot, NewRoot: newRoot}
	body, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteMigrationFile(markerPath, append(body, '\n'), 0o600)
}

func migrationMarkerValid(path string) bool {
	body, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var record v11MigrationRecord
	return json.Unmarshal(body, &record) == nil && record.Version == 11 &&
		!record.MigratedAt.IsZero() && strings.TrimSpace(record.LegacyRoot) != "" && strings.TrimSpace(record.NewRoot) != ""
}

func acquireMigrationLock(path string) (release func(), acquired bool, err error) {
	lock, openErr := openMigrationLock(path)
	if errors.Is(openErr, ErrMigrationBusy) {
		return nil, false, &MigrationBusyError{Path: path}
	}
	if openErr != nil {
		return nil, false, openErr
	}
	record := struct {
		PID       int       `json:"pid"`
		CreatedAt time.Time `json:"createdAt"`
	}{PID: os.Getpid(), CreatedAt: time.Now().UTC()}
	body, marshalErr := json.Marshal(record)
	if marshalErr == nil {
		if err := lock.Truncate(0); err != nil {
			marshalErr = err
		} else if _, err := lock.Seek(0, 0); err != nil {
			marshalErr = err
		} else if _, err := lock.Write(append(body, '\n')); err != nil {
			marshalErr = err
		} else {
			marshalErr = lock.Sync()
		}
	}
	if marshalErr != nil {
		releaseMigrationLock(lock)
		return nil, false, marshalErr
	}
	return func() { releaseMigrationLock(lock) }, true, nil
}

func copyTreeMissing(source, target string) error {
	var conflicts []string
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(target, 0o755)
		}
		dst := filepath.Join(target, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			// Never follow a legacy symlink outside the owned state tree.
			return nil
		}
		if entry.IsDir() {
			return os.MkdirAll(dst, info.Mode().Perm())
		}
		if _, err := os.Stat(dst); err == nil {
			same, compareErr := migrationFilesIdentical(path, dst)
			if compareErr != nil {
				return compareErr
			}
			if !same {
				conflicts = append(conflicts, dst)
			}
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return copyFileExclusive(path, dst, info.Mode().Perm())
	})
	if err != nil {
		return err
	}
	if len(conflicts) > 0 {
		return &MigrationConflictError{Paths: conflicts}
	}
	return nil
}

func migrationFilesIdentical(source, target string) (bool, error) {
	sourceInfo, err := os.Stat(source)
	if err != nil {
		return false, err
	}
	targetInfo, err := os.Stat(target)
	if err != nil {
		return false, err
	}
	if sourceInfo.IsDir() || targetInfo.IsDir() {
		return false, nil
	}
	if sourceInfo.Size() != targetInfo.Size() {
		return false, nil
	}
	hashFile := func(path string) ([sha256.Size]byte, error) {
		f, err := os.Open(path)
		if err != nil {
			return [sha256.Size]byte{}, err
		}
		defer f.Close()
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			return [sha256.Size]byte{}, err
		}
		var sum [sha256.Size]byte
		copy(sum[:], h.Sum(nil))
		return sum, nil
	}
	sourceHash, err := hashFile(source)
	if err != nil {
		return false, err
	}
	targetHash, err := hashFile(target)
	if err != nil {
		return false, err
	}
	return bytes.Equal(sourceHash[:], targetHash[:]), nil
}

func copyFileExclusive(source, target string, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(target), ".migration-v11-copy-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if _, err := os.Stat(target); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(tmpPath, target); err != nil {
		if _, statErr := os.Stat(target); statErr == nil {
			return nil
		}
		return err
	}
	return nil
}

func atomicWriteMigrationFile(path string, body []byte, mode fs.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".migration-v11-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return fileutil.ReplaceFile(tmpPath, path)
}
