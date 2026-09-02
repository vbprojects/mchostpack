package runtime

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Phase string

const (
	Idle       Phase = "IDLE"
	Loading    Phase = "LOADING"
	Ready      Phase = "READY"
	Draining   Phase = "DRAINING"
	Saving     Phase = "SAVING"
	Recovering Phase = "RECOVERING"
	Failed     Phase = "FAILED"
)

type State struct {
	Phase          Phase      `json:"phase"`
	ActiveID       string     `json:"activeId,omitempty"`
	PackLockDigest string     `json:"packLockDigest,omitempty"`
	Generation     uint64     `json:"generation"`
	EmptySince     *time.Time `json:"emptySince,omitempty"`
	BackupPending  bool       `json:"backupPending"`
	LastError      string     `json:"lastError,omitempty"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}
type StateFile struct {
	path string
	mu   sync.Mutex
}

func NewStateFile(path string) *StateFile { return &StateFile{path: path} }
func (s *StateFile) Load() (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return State{Phase: Idle}, nil
	}
	if err != nil {
		return State{}, err
	}
	var st State
	if err = json.Unmarshal(b, &st); err != nil {
		return State{}, err
	}
	if st.Phase == "" {
		st.Phase = Idle
	}
	return st, nil
}
func (s *StateFile) Save(st State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st.UpdatedAt = time.Now().UTC()
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".active-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	if ce := f.Close(); err == nil {
		err = ce
	}
	if err != nil {
		return err
	}
	if err = os.Rename(tmp, s.path); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(s.path))
	if err == nil {
		err = d.Sync()
		_ = d.Close()
	}
	return err
}
