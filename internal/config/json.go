package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// JSONRecoveryError is a successful recovery, with a warning for the UI. All
// other load errors leave the owner read-only until it is reopened/repaired.
type JSONRecoveryError struct{ Path, Preserved string }

func (e *JSONRecoveryError) Error() string {
	return fmt.Sprintf("%s: recovered previous saved version; original preserved at %s", e.Path, e.Preserved)
}

func JSONRecovered(err error) bool {
	var recovered *JSONRecoveryError
	return errors.As(err, &recovered)
}

// JSONCommitError means rename succeeded but the directory flush failed. The
// new file is visible, so memory must follow it, while reporting uncertain
// power-loss durability. A pre-commit error never publishes new state.
type JSONCommitError struct{ Err error }

func (e *JSONCommitError) Error() string { return "saved, but directory sync failed: " + e.Err.Error() }
func (e *JSONCommitError) Unwrap() error { return e.Err }
func JSONCommitted(err error) bool {
	// Only the top-level result of the primary write denotes a commit. A
	// wrapped recovery/backup error must not make a later blocked mutation
	// appear successful merely because one of its earlier steps renamed a file.
	_, committed := err.(*JSONCommitError)
	return err == nil || committed
}

// LoadJSON calls decode with fresh bytes; decode must publish only on success.
// It never treats a damaged/inaccessible backup as a fresh installation.
func LoadJSON(path string, decode func([]byte) error) error {
	return loadJSON(path, decode, true)
}

// Settings backups may predate a newly added private app. Inspect the backup
// for repair, but never promote it to an apparently healthy primary file.
func loadSettingsJSON(path string, decode func([]byte) error) error {
	return loadJSON(path, decode, false)
}

func loadJSON(path string, decode func([]byte) error, restore bool) error {
	raw, err := os.ReadFile(path)
	if err == nil {
		err = decode(raw)
		if err == nil {
			return nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	backup, backupErr := os.ReadFile(path + ".bak")
	if backupErr != nil {
		if errors.Is(err, os.ErrNotExist) && errors.Is(backupErr, os.ErrNotExist) {
			return os.ErrNotExist
		}
		return fmt.Errorf("%s: cannot load state or backup: %w", path, errors.Join(err, backupErr))
	}
	if backupErr = decode(backup); backupErr != nil {
		return fmt.Errorf("%s: cannot load state or backup: %w", path, errors.Join(err, backupErr))
	}
	if !restore {
		return fmt.Errorf("%s: settings require repair; the previous backup may omit private apps; original and backup left unchanged", path)
	}
	preserved := path + ".bak"
	if raw != nil {
		// Copy, never rename away the only damaged original. If recovery cannot
		// commit, the primary still exists and later writes remain disabled.
		f, e := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".corrupt-*")
		if e != nil {
			return fmt.Errorf("preserve damaged state: %w", e)
		}
		preserved = f.Name()
		_, writeErr := f.Write(raw)
		syncErr := f.Sync()
		closeErr := f.Close()
		if e = errors.Join(writeErr, syncErr, closeErr); e != nil {
			return fmt.Errorf("preserve damaged state: %w", e)
		}
	}
	if e := WritePrivateFile(path, backup); e != nil {
		return fmt.Errorf("restore saved backup: %w", e)
	}
	return &JSONRecoveryError{Path: path, Preserved: preserved}
}

// WriteJSON keeps the last valid version before replacing the primary. The
// caller serializes writes and publishes its new memory only after commit.
func WriteJSON(path string, value any) error {
	return writeJSON(path, value, false)
}

// WriteJSONPruned retains only the pruned snapshot in the backup too, so
// explicit deletion/retention cannot resurrect deleted private text later.
func WriteJSONPruned(path string, value any) error {
	return writeJSON(path, value, true)
}

func writeJSON(path string, value any, prune bool) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	previous, err := os.ReadFile(path)
	switch {
	case err == nil:
		if !json.Valid(previous) {
			return fmt.Errorf("%s: refusing to overwrite damaged JSON", path)
		}
		if prune {
			previous = raw
		}
		if e := WritePrivateFile(path+".bak", previous); e != nil {
			// Even a committed backup failure precedes the primary commit.
			return fmt.Errorf("save previous version: %v", e)
		}
	case !errors.Is(err, os.ErrNotExist):
		return err
	}
	return WritePrivateFile(path, raw)
}

// WritePrivateFile uses unique 0600 files in the same directory. The old
// name remains intact until rename; audio writes share the same guarantees.
func WritePrivateFile(path string, raw []byte) error {
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	// Fail before committing on filesystems that do not support directory sync.
	if err := dir.Sync(); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(raw)
	syncErr := f.Sync()
	closeErr := f.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	if err := dir.Sync(); err != nil {
		return &JSONCommitError{Err: err}
	}
	return nil
}

// ObjectJSON rejects null, which otherwise silently decodes to an empty
// settings/dictionary object. Empty legacy history arrays may still use null.
func ObjectJSON(raw []byte) error {
	if b := bytes.TrimSpace(raw); len(b) == 0 || b[0] != '{' {
		return errors.New("expected a JSON object")
	}
	return nil
}
