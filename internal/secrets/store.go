// Package secrets stores application credentials locally without Keychain access.
// The random encryption key is protected by filesystem permissions, not a password.
// Processes running as the same user can decrypt the store. The app is single-instance.
package secrets

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const header = "VOICEINPUT-SECRETS-1\n"

type Store struct {
	mu      sync.Mutex
	dir     string
	aead    cipher.AEAD
	values  map[string]string
	initErr error
}

// Open returns a disabled store on failure, so subsequent writes cannot destroy
// credentials whose encryption key or ciphertext is damaged.
func Open(dataDir string) (*Store, error) {
	s := &Store{dir: filepath.Join(dataDir, "credentials"), values: map[string]string{}}
	s.initErr = s.open()
	return s, s.initErr
}

func privateFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("credential file is not a regular file")
	}
	if err := os.Chmod(path, 0600); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func (s *Store) open() error {
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(s.dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("credential directory is not a real directory")
	}
	if err := os.Chmod(s.dir, 0700); err != nil {
		return err
	}
	encrypted, readErr := privateFile(filepath.Join(s.dir, "secrets.enc"))
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	keyPath := filepath.Join(s.dir, "master.key")
	key, err := privateFile(keyPath)
	if errors.Is(err, os.ErrNotExist) {
		if readErr == nil {
			return errors.New("encryption key is missing; existing credentials were left unchanged")
		}
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return err
		}
		// Publish a fully written key without ever replacing an existing key.
		tmp, err := writeTemp(s.dir, key)
		if err != nil {
			return err
		}
		defer os.Remove(tmp)
		if err := os.Link(tmp, keyPath); err != nil {
			return err
		}
		if err := syncDir(s.dir); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if len(key) != 32 {
		return errors.New("invalid local encryption key; credentials were left unchanged")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	s.aead, err = cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return err
	}
	if errors.Is(readErr, os.ErrNotExist) {
		return nil
	}
	if !bytes.HasPrefix(encrypted, []byte(header)) {
		return errors.New("unsupported or damaged credential file")
	}
	plain, err := s.aead.Open(nil, nil, encrypted[len(header):], []byte(header))
	if err != nil {
		return errors.New("local credentials could not be decrypted; files were left unchanged")
	}
	if err := json.Unmarshal(plain, &s.values); err != nil {
		return errors.New("invalid encrypted credential contents")
	}
	if s.values == nil {
		return errors.New("invalid encrypted credential contents")
	}
	return nil
}

func (s *Store) Get(account string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initErr != nil {
		return "", s.initErr
	}
	return s.values[account], nil
}

// Set removes the account when value is empty. Memory only advances after save.
func (s *Store) Set(account, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initErr != nil {
		return s.initErr
	}
	next := make(map[string]string, len(s.values)+1)
	for k, v := range s.values {
		next[k] = v
	}
	if value == "" {
		delete(next, account)
	} else {
		next[account] = value
	}
	plain, err := json.Marshal(next)
	if err != nil {
		return err
	}
	encrypted := s.aead.Seal([]byte(header), nil, plain, []byte(header))
	tmp, err := writeTemp(s.dir, encrypted)
	if err != nil {
		return fmt.Errorf("could not save encrypted credentials: %w", err)
	}
	defer os.Remove(tmp)
	if err := os.Rename(tmp, filepath.Join(s.dir, "secrets.enc")); err != nil {
		return err
	}
	if err := syncDir(s.dir); err != nil {
		return err
	}
	s.values = next
	return nil
}

func (s *Store) Delete(account string) error { return s.Set(account, "") }

func writeTemp(dir string, data []byte) (string, error) {
	f, err := os.CreateTemp(dir, ".credential-*")
	if err != nil {
		return "", err
	}
	name := f.Name()
	ok := false
	defer func() {
		f.Close()
		if !ok {
			os.Remove(name)
		}
	}()
	if _, err := f.Write(data); err != nil {
		return "", err
	}
	if err := f.Sync(); err != nil {
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	ok = true
	return name, nil
}

func syncDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
