// Package store keeps the recording history and its audio in the app's
// data directory, outside the source tree.
package store

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"voiceinput/internal/config"
)

const (
	StatusInserted    = "inserted"
	StatusNotInserted = "not-inserted"
	StatusFailed      = "failed"
	StatusCancelled   = "cancelled"

	KindRetranscribe = "retranscribe"
	KindRecleanup    = "recleanup"
	KindRevision     = "revision"

	SampleRate = 16000
)

// Version is a later result for the same recording: a rerun or a revision
// the user saved. Versions are appended; earlier results are never replaced.
type Version struct {
	VoiceTaskID  string        `json:"voiceTaskId,omitempty"`
	VoiceBilling *VoiceBilling `json:"voiceBilling,omitempty"`
	ID           string        `json:"id"`
	Kind         string        `json:"kind"`
	Text         string        `json:"text"`
	BasedOn      string        `json:"basedOn,omitempty"`
	Note         string        `json:"note,omitempty"`
	// Model is the cleanup model of a KindRecleanup version.
	Model string    `json:"model,omitempty"`
	At    time.Time `json:"at"`
}

// Timing keeps the delays apart instead of one "response time" (§7).
type Timing struct {
	StopToResultMs     int64 `json:"stopToResultMs,omitempty"` // integrated paid result received
	FirstPartialMs     int64 `json:"firstPartialMs"`
	StopToFinalMs      int64 `json:"stopToFinalMs"`
	CleanupMs          int64 `json:"cleanupMs"`
	FinalToInsertMs    int64 `json:"finalToInsertMs"`
	PasteConfirmedMs   int64 `json:"pasteConfirmedMs,omitempty"`
	ClipboardRestoreMs int64 `json:"clipboardRestoreMs,omitempty"`
	StartToListeningMs int64 `json:"startToListeningMs,omitempty"`
}

type Record struct {
	VoiceTaskID         string        `json:"voiceTaskId,omitempty"`
	VoiceBilling        *VoiceBilling `json:"voiceBilling,omitempty"`
	TranslationLanguage string        `json:"translationLanguage,omitempty"`
	ID                  string        `json:"id"`
	StartedAt           time.Time     `json:"startedAt"`
	DurationMs          int64         `json:"durationMs"`
	Status              string        `json:"status"`
	StatusDetail        string        `json:"statusDetail,omitempty"`
	TargetApp           string        `json:"targetApp,omitempty"`
	TargetBundleID      string        `json:"targetBundleID,omitempty"`

	// RawText is the ASR's final transcript as received. CorrectedText is
	// it after the confirmed dictionary rules, empty when none applied.
	RawText       string   `json:"rawText"`
	Corrected     bool     `json:"corrected,omitempty"` // Distinguishes an intentionally empty result.
	CorrectedText string   `json:"correctedText,omitempty"`
	AppliedRules  []string `json:"appliedRules,omitempty"`

	CleanupRequested bool   `json:"cleanupRequested"`
	CleanedText      string `json:"cleanedText,omitempty"`
	CleanupError     string `json:"cleanupError,omitempty"`
	CleanupModel     string `json:"cleanupModel,omitempty"`
	ASRModel         string `json:"asrModel,omitempty"`

	// InsertedText is what was written to the target; empty unless Status
	// is StatusInserted.
	InsertedText string `json:"insertedText,omitempty"`
	SendShortcut string `json:"sendShortcut,omitempty"`
	SendSkipped  string `json:"sendSkipped,omitempty"`

	// AudioFile is empty when audio saving was off for this recording, and
	// AudioRemoved says a saved file was deleted or expired since.
	AudioFile    string `json:"audioFile,omitempty"`
	AudioRemoved string `json:"audioRemoved,omitempty"`

	Versions    []Version `json:"versions,omitempty"`
	Timing      Timing    `json:"timing"`
	HotwordNote string    `json:"hotwordNote,omitempty"`
	ObserveNote string    `json:"observeNote,omitempty"`
}

// VoiceBilling stores the server receipt, never a credential. The task ID
// survives failures so a lost response can be retrieved without a new charge.
type VoiceBilling struct {
	BillableCharacters int    `json:"billableCharacters"`
	Currency           string `json:"currency"`
	ChargeUSD          string `json:"chargeUsd"`
	PriceVersion       string `json:"priceVersion"`
	UsageLogID         int64  `json:"usageLogId"`
}

// Transcript is the transcript the dictionary produced, or the raw one.
func (r *Record) Transcript() string {
	if r.Corrected || r.CorrectedText != "" {
		return r.CorrectedText
	}
	return r.RawText
}

func (r *Record) HasAudio() bool { return r.AudioFile != "" && r.AudioRemoved == "" }

type Store struct {
	mu      sync.Mutex
	dir     string
	records []Record // newest first
	loadErr error
}

func Open(dir string) (*Store, error) {
	s := &Store{dir: dir}
	if err := os.MkdirAll(filepath.Join(dir, "audio"), 0o700); err != nil {
		s.loadErr = err
		return s, err
	}
	err := config.LoadJSON(s.file(), func(b []byte) error {
		var records []Record
		if err := json.Unmarshal(b, &records); err != nil {
			return err
		}
		s.records = records
		return nil
	})
	if err == os.ErrNotExist {
		return s, nil
	}
	if err != nil && !config.JSONRecovered(err) {
		s.loadErr = err
	}
	return s, err
}

func (s *Store) file() string { return filepath.Join(s.dir, "history.json") }

func (s *Store) save(records []Record, prune bool) error {
	if s.loadErr != nil {
		return s.loadErr
	}
	write := config.WriteJSON
	if prune {
		write = config.WriteJSONPruned
	}
	err := write(s.file(), records)
	if config.JSONCommitted(err) {
		s.records = records
	}
	return err
}

func cloneRecord(r Record) Record {
	r.AppliedRules = slices.Clone(r.AppliedRules)
	r.Versions = slices.Clone(r.Versions)
	if r.VoiceBilling != nil {
		b := *r.VoiceBilling
		r.VoiceBilling = &b
	}
	for i := range r.Versions {
		if r.Versions[i].VoiceBilling != nil {
			b := *r.Versions[i].VoiceBilling
			r.Versions[i].VoiceBilling = &b
		}
	}
	return r
}

// AudioPath is where the audio of a record is, or would be.
func (s *Store) AudioPath(name string) string { return filepath.Join(s.dir, "audio", name) }

func NewID() string { return time.Now().Format("20060102-150405.000000") }

func (s *Store) Add(r Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.save(append([]Record{cloneRecord(r)}, s.records...), false)
}

// Update changes one record, and reports whether it exists.
func (s *Store) Update(id string, fn func(r *Record)) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.records {
		if s.records[i].ID == id {
			next := slices.Clone(s.records)
			next[i] = cloneRecord(next[i])
			fn(&next[i])
			return true, s.save(next, false)
		}
	}
	return false, nil
}

func (s *Store) Get(id string) (Record, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.records {
		if r.ID == id {
			return cloneRecord(r), true
		}
	}
	return Record{}, false
}

// List returns the records containing query, newest first.
func (s *Store) List(query string) []Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	q := strings.ToLower(strings.TrimSpace(query))
	var out []Record
	for _, r := range s.records {
		if q == "" || matches(&r, q) {
			out = append(out, cloneRecord(r))
		}
	}
	return out
}

func matches(r *Record, q string) bool {
	for _, t := range []string{r.RawText, r.CorrectedText, r.CleanedText, r.InsertedText, r.TargetApp} {
		if strings.Contains(strings.ToLower(t), q) {
			return true
		}
	}
	for _, v := range r.Versions {
		if strings.Contains(strings.ToLower(v.Text), q) {
			return true
		}
	}
	return false
}

// Delete removes a record and its audio.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, r := range s.records {
		if r.ID == id {
			next := slices.Delete(slices.Clone(s.records), i, i+1)
			err := s.save(next, true)
			if !config.JSONCommitted(err) {
				return err
			}
			if r.AudioFile != "" {
				os.Remove(s.AudioPath(r.AudioFile))
			}
			return err
		}
	}
	return nil
}

// Clear removes every record and every audio file.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.save(nil, true)
	if !config.JSONCommitted(err) {
		return err
	}
	// Recovery may leave audio outside the restored index. Clear owns the
	// whole audio directory and recovery snapshots, not only indexed records.
	problems := []error{err}
	audio := filepath.Join(s.dir, "audio")
	info, statErr := os.Lstat(audio)
	if statErr != nil {
		problems = append(problems, statErr)
	} else if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		problems = append(problems, errors.New("audio storage is not a real directory"))
	} else {
		entries, readErr := os.ReadDir(audio)
		problems = append(problems, readErr)
		for _, entry := range entries {
			// Remove unlinks a symlink itself; it never follows its target.
			if e := os.Remove(filepath.Join(audio, entry.Name())); e != nil && !errors.Is(e, os.ErrNotExist) {
				problems = append(problems, fmt.Errorf("remove recording %s: %w", entry.Name(), e))
			}
		}
		problems = append(problems, syncDirectory(audio))
	}
	archives, globErr := filepath.Glob(s.file() + ".corrupt-*")
	problems = append(problems, globErr)
	for _, path := range archives {
		if e := os.Remove(path); e != nil && !errors.Is(e, os.ErrNotExist) {
			problems = append(problems, fmt.Errorf("remove recovery snapshot: %w", e))
		}
	}
	problems = append(problems, syncDirectory(s.dir))
	return errors.Join(problems...)
}

func syncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// Purge applies the retention periods: audio older than audioDays is
// deleted and its record says so, and records older than textDays go
// altogether. A period of 0 keeps everything.
func (s *Store) Purge(audioDays, textDays int, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	var kept []Record
	var remove []string
	for _, r := range s.records {
		age := now.Sub(r.StartedAt)
		if textDays > 0 && age > time.Duration(textDays)*24*time.Hour {
			if r.AudioFile != "" {
				remove = append(remove, r.AudioFile)
			}
			changed = true
			continue
		}
		if audioDays > 0 && r.HasAudio() && age > time.Duration(audioDays)*24*time.Hour {
			remove = append(remove, r.AudioFile)
			r.AudioRemoved = fmt.Sprintf("expired after %d days", audioDays)
			changed = true
		}
		kept = append(kept, r)
	}
	if !changed {
		return nil
	}
	err := s.save(kept, true)
	if config.JSONCommitted(err) {
		for _, name := range remove {
			os.Remove(s.AudioPath(name))
		}
	}
	return err
}

// WriteWAV saves PCM16 mono 16 kHz samples, as sent to the ASR.
func (s *Store) WriteWAV(name string, pcm []byte) error {
	if s.loadErr != nil {
		return s.loadErr
	}
	h := make([]byte, 44)
	copy(h[0:], "RIFF")
	binary.LittleEndian.PutUint32(h[4:], uint32(36+len(pcm)))
	copy(h[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1) // PCM
	binary.LittleEndian.PutUint16(h[22:], 1) // mono
	binary.LittleEndian.PutUint32(h[24:], SampleRate)
	binary.LittleEndian.PutUint32(h[28:], SampleRate*2)
	binary.LittleEndian.PutUint16(h[32:], 2)
	binary.LittleEndian.PutUint16(h[34:], 16)
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], uint32(len(pcm)))
	return config.WritePrivateFile(s.AudioPath(name), append(h, pcm...))
}

// ReadPCM returns the samples of a file WriteWAV wrote.
func (s *Store) ReadPCM(name string) ([]byte, error) {
	b, err := os.ReadFile(s.AudioPath(name))
	if err != nil {
		return nil, err
	}
	if len(b) < 44 || string(b[0:4]) != "RIFF" {
		return nil, errors.New("not a recording saved by Voicemore")
	}
	return b[44:], nil
}
