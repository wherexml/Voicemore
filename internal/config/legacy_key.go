package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ClearLegacyCleanupKey is called only after the encrypted store has returned
// verified. Preflight every snapshot before changing any of them: an older,
// different credential must not silently lose its only remaining copy.
func (st *Store) ClearLegacyCleanupKey(verified string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.loadErr != nil {
		return st.loadErr
	}
	if verified == "" {
		return errors.New("encrypted credential was not verified")
	}
	archives, err := filepath.Glob(st.path + ".corrupt-*")
	if err != nil {
		return err
	}
	paths := append([]string{st.path, st.path + ".bak"}, archives...)
	type snapshot struct {
		path  string
		clean []byte
	}
	var pending []snapshot
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf("read legacy credential snapshot: %w", err)
		}
		var fields map[string]json.RawMessage
		// Corrupt snapshots may contain a complete object with trailing damage.
		if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&fields); err != nil {
			if bytes.Contains(raw, []byte("cleanupApiKey")) {
				return errors.New("damaged credential snapshot requires repair")
			}
			continue
		}
		var legacy string
		if field, ok := fields["cleanupApiKey"]; ok {
			if err := json.Unmarshal(field, &legacy); err != nil {
				return errors.New("invalid legacy credential snapshot")
			}
		}
		if legacy == "" {
			continue
		}
		if legacy != verified {
			return errors.New("legacy credential differs from encrypted credential; original retained")
		}
		delete(fields, "cleanupApiKey")
		clean, err := json.MarshalIndent(fields, "", "  ")
		if err != nil {
			return err
		}
		pending = append(pending, snapshot{path, clean})
	}
	// Save secondary snapshots first, then atomically publish sanitized settings
	// and a sanitized backup. Partial failures are reported and can be retried.
	for _, snap := range pending {
		if snap.path == st.path {
			continue
		}
		if err := WritePrivateFile(snap.path, snap.clean); err != nil {
			return fmt.Errorf("sanitize credential snapshot: %w", err)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	next := st.s
	next.CleanupAPIKey = ""
	err = WriteJSONPruned(st.path, next)
	if JSONCommitted(err) {
		st.s = next
	}
	return err
}
