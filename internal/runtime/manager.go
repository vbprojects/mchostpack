package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/hostpack/hostpack/internal/config"
	"github.com/hostpack/hostpack/internal/mcproto"
	"github.com/hostpack/hostpack/internal/rcon"
	"github.com/hostpack/hostpack/internal/store"
)

type Manager struct {
	cfg          *config.Config
	lock         *config.LockFile
	store        store.Store
	launcher     Launcher
	stateFile    *StateFile
	stateRoot    string
	rcon         rcon.Client
	log          *slog.Logger
	mu           sync.Mutex
	state        State
	process      Process
	expectedStop bool
	started      time.Time
	loginSeen    bool
	done         chan struct{}
	doneOnce     sync.Once
	ctx          context.Context
	cancel       context.CancelFunc
}

func NewManager(cfg *config.Config, lock *config.LockFile, st store.Store, launcher Launcher, stateRoot string, logger *slog.Logger) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{cfg: cfg, lock: lock, store: st, launcher: launcher, stateFile: NewStateFile(filepath.Join(stateRoot, "runtime", "active.json")), stateRoot: stateRoot, rcon: rcon.Client{Address: "127.0.0.1:25575", Password: os.Getenv("RCON_PASSWORD")}, log: logger, started: time.Now(), done: make(chan struct{}), ctx: ctx, cancel: cancel}
	m.state, _ = m.stateFile.Load()
	if m.state.Phase != Idle && m.state.ActiveID != "" {
		m.state.Phase = Recovering
		_ = m.stateFile.Save(m.state)
		go m.start(m.state.ActiveID, true)
	}
	go m.monitor()
	return m
}
func (m *Manager) Done() <-chan struct{} { return m.done }
func (m *Manager) Close()                { m.cancel() }
func (m *Manager) State() State          { m.mu.Lock(); defer m.mu.Unlock(); return m.state }
func (m *Manager) Ensure(id string) (bool, string) {
	m.mu.Lock()
	m.loginSeen = true
	switch {
	case m.state.Phase == Idle:
		m.state = State{Phase: Loading, ActiveID: id, PackLockDigest: m.lock.Packs[id].IdentityDigest}
		_ = m.stateFile.Save(m.state)
		m.mu.Unlock()
		go m.start(id, false)
		return m.waitReady(id)
	case m.state.ActiveID == id && (m.state.Phase == Ready):
		m.mu.Unlock()
		return true, ""
	case m.state.ActiveID == id && (m.state.Phase == Loading || m.state.Phase == Recovering):
		m.mu.Unlock()
		return m.waitReady(id)
	case m.state.Phase == Failed && m.state.ActiveID == id && m.process == nil:
		m.state.Phase = Loading
		m.state.LastError = ""
		_ = m.stateFile.Save(m.state)
		m.mu.Unlock()
		go m.start(id, false)
		return m.waitReady(id)
	default:
		active := m.state.ActiveID
		phase := m.state.Phase
		empty := m.state.EmptySince
		m.mu.Unlock()
		if phase == Ready && empty != nil && time.Since(*empty) >= m.cfg.Runtime.EmptyBeforeSwitch.Duration {
			go m.switchTo(id)
			return false, "Switching to " + m.cfg.Packs[id].DisplayName + ". Please reconnect shortly."
		}
		name := active
		if p, ok := m.cfg.Packs[active]; ok {
			name = p.DisplayName
		}
		return false, fmt.Sprintf("%s is currently %s. %s cannot start yet.", name, phase, m.cfg.Packs[id].DisplayName)
	}
}
func (m *Manager) waitReady(id string) (bool, string) {
	deadline := time.Now().Add(m.cfg.Runtime.StartupWait.Duration)
	for time.Now().Before(deadline) {
		time.Sleep(250 * time.Millisecond)
		m.mu.Lock()
		phase, active, errText := m.state.Phase, m.state.ActiveID, m.state.LastError
		m.mu.Unlock()
		if active == id && phase == Ready {
			return true, ""
		}
		if phase == Failed {
			return false, "Failed to start " + m.cfg.Packs[id].DisplayName + ": " + errText
		}
	}
	return false, m.cfg.Packs[id].DisplayName + " is starting. Please reconnect shortly."
}
func (m *Manager) start(id string, recovery bool) {
	p, ok := m.cfg.Packs[id]
	if !ok {
		m.fail(id, fmt.Errorf("pack removed from config"))
		return
	}
	instanceRoot := filepath.Join(m.stateRoot, "instances", id)
	dir := filepath.Join(instanceRoot, "server")
	_, statErr := os.Stat(dir)
	head, exists, headErr := m.store.Head(m.ctx, id)
	if errors.Is(statErr, os.ErrNotExist) {
		if headErr != nil {
			m.fail(id, fmt.Errorf("check backup before first start: %w", headErr))
			return
		} else if exists {
			if _, restoreErr := m.store.Restore(m.ctx, id, dir); restoreErr != nil {
				m.fail(id, fmt.Errorf("restore: %w", restoreErr))
				return
			}
		}
	} else if statErr != nil {
		m.fail(id, statErr)
		return
	}
	if err := m.ensureInstanceIdentity(instanceRoot, m.lock.Packs[id]); err != nil {
		m.fail(id, err)
		return
	}
	m.mu.Lock()
	if exists {
		m.state.Generation = head.Generation
	}
	if headErr != nil {
		m.state.BackupPending = true
	}
	_ = m.stateFile.Save(m.state)
	m.mu.Unlock()
	proc, err := m.launcher.Start(m.ctx, id, p, m.lock.Packs[id])
	if err != nil {
		m.fail(id, err)
		return
	}
	m.mu.Lock()
	if m.state.ActiveID != id {
		m.mu.Unlock()
		_ = proc.Kill()
		return
	}
	m.process = proc
	m.expectedStop = false
	m.mu.Unlock()
	go m.waitProcess(id, proc)
	deadline := time.Now().Add(maxDuration(m.cfg.Runtime.StartupWait.Duration*12, 5*time.Minute))
	for time.Now().Before(deadline) {
		if _, err := mcproto.QueryStatus(time.Now().Add(2*time.Second), m.cfg.Runtime.BackendAddress, id+"."+m.cfg.Domain); err == nil {
			m.mu.Lock()
			if m.state.ActiveID == id {
				m.state.Phase = Ready
				m.state.LastError = ""
				if recovery {
					m.state.Phase = Ready
				}
				_ = m.stateFile.Save(m.state)
			}
			m.mu.Unlock()
			return
		}
		select {
		case <-m.ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
	_ = proc.Kill()
	m.fail(id, fmt.Errorf("minecraft did not become ready"))
}
func (m *Manager) waitProcess(id string, p Process) {
	err := p.Wait()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.process != p {
		return
	}
	m.process = nil
	if m.expectedStop {
		m.expectedStop = false
		return
	}
	m.state.Phase = Failed
	m.state.LastError = fmt.Sprintf("minecraft exited: %v", err)
	_ = m.stateFile.Save(m.state)
}
func (m *Manager) fail(id string, err error) {
	m.log.Error("pack failed", "pack", id, "error", err)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Phase = Failed
	m.state.ActiveID = id
	m.state.LastError = err.Error()
	_ = m.stateFile.Save(m.state)
}
func (m *Manager) switchTo(id string) {
	m.mu.Lock()
	if m.state.Phase != Ready || m.state.ActiveID == id {
		m.mu.Unlock()
		return
	}
	m.state.Phase = Draining
	_ = m.stateFile.Save(m.state)
	m.mu.Unlock()
	if err := m.stopAndBackup(); err != nil {
		m.fail(m.state.ActiveID, err)
		m.signalDone()
		return
	}
	m.mu.Lock()
	m.state = State{Phase: Loading, ActiveID: id, PackLockDigest: m.lock.Packs[id].IdentityDigest}
	_ = m.stateFile.Save(m.state)
	m.mu.Unlock()
	m.start(id, false)
}
func (m *Manager) stopAndBackup() error {
	m.mu.Lock()
	id := m.state.ActiveID
	proc := m.process
	m.state.Phase = Saving
	_ = m.stateFile.Save(m.state)
	m.mu.Unlock()
	if proc != nil {
		ctx, cancel := context.WithTimeout(context.Background(), m.cfg.Runtime.ShutdownTimeout.Duration)
		defer cancel()
		if _, err := m.rcon.Command(ctx, "save-all flush"); err != nil {
			return fmt.Errorf("save-all: %w", err)
		}
		m.mu.Lock()
		m.expectedStop = true
		m.mu.Unlock()
		if _, err := m.rcon.Command(ctx, "stop"); err != nil {
			m.mu.Lock()
			m.expectedStop = false
			m.mu.Unlock()
			return fmt.Errorf("stop: %w", err)
		}
		deadline := time.Now().Add(m.cfg.Runtime.ShutdownTimeout.Duration)
		for time.Now().Before(deadline) {
			m.mu.Lock()
			running := m.process != nil
			m.mu.Unlock()
			if !running {
				break
			}
			time.Sleep(250 * time.Millisecond)
		}
		m.mu.Lock()
		running := m.process != nil
		m.mu.Unlock()
		if running {
			_ = proc.Kill()
			return fmt.Errorf("minecraft did not stop in time")
		}
	}
	m.mu.Lock()
	generation := m.state.Generation
	m.mu.Unlock()
	manifest, err := m.store.Commit(context.Background(), id, filepath.Join(m.stateRoot, "instances", id, "server"), generation)
	m.mu.Lock()
	if err != nil {
		m.state.BackupPending = true
		m.state.LastError = "backup pending: " + err.Error()
		_ = m.stateFile.Save(m.state)
		m.mu.Unlock()
		return fmt.Errorf("backup: %w", err)
	}
	m.state.Generation = manifest.Generation
	m.state.BackupPending = false
	m.state.LastError = ""
	_ = m.stateFile.Save(m.state)
	m.mu.Unlock()
	if m.cfg.Storage.EvictAfterBackup {
		serverDir := filepath.Join(m.stateRoot, "instances", id, "server")
		if err := os.RemoveAll(serverDir); err != nil {
			return fmt.Errorf("evict verified local pack %q: %w", id, err)
		}
		m.log.Info("evicted verified local pack", "pack", id, "generation", manifest.Generation)
	}
	return nil
}

func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	if m.state.Phase == Idle || m.process == nil {
		m.mu.Unlock()
		return nil
	}
	m.state.Phase = Draining
	_ = m.stateFile.Save(m.state)
	m.mu.Unlock()
	done := make(chan error, 1)
	go func() { done <- m.stopAndBackup() }()
	select {
	case err := <-done:
		if err == nil {
			m.mu.Lock()
			m.state = State{Phase: Idle}
			_ = m.stateFile.Save(m.state)
			m.mu.Unlock()
		}
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) ensureInstanceIdentity(root string, locked config.LockedPack) error {
	path := filepath.Join(root, "pack.lock.json")
	b, err := os.ReadFile(path)
	if err == nil {
		var prior struct {
			IdentityDigest string `json:"identityDigest"`
		}
		if json.Unmarshal(b, &prior) != nil || prior.IdentityDigest != locked.IdentityDigest {
			return fmt.Errorf("instance pack identity differs from packs.lock.json")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return err
	}
	b, _ = json.MarshalIndent(locked, "", "  ")
	f, err := os.CreateTemp(root, ".pack-lock-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
func (m *Manager) monitor() {
	ticker := time.NewTicker(m.cfg.Runtime.BackendPollInterval.Duration)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.tick()
		}
	}
}
func (m *Manager) tick() {
	m.mu.Lock()
	phase := m.state.Phase
	id := m.state.ActiveID
	login := m.loginSeen
	m.mu.Unlock()
	if phase == Idle && !login && time.Since(m.started) >= m.cfg.Runtime.StatusIdleExit.Duration {
		m.signalDone()
		return
	}
	if phase != Ready {
		return
	}
	players, err := mcproto.QueryStatus(time.Now().Add(3*time.Second), m.cfg.Runtime.BackendAddress, id+"."+m.cfg.Domain)
	m.mu.Lock()
	if err != nil {
		m.state.EmptySince = nil
		_ = m.stateFile.Save(m.state)
		m.mu.Unlock()
		return
	}
	now := time.Now().UTC()
	if players > 0 {
		m.state.EmptySince = nil
	} else if m.state.EmptySince == nil {
		m.state.EmptySince = &now
	}
	empty := m.state.EmptySince
	_ = m.stateFile.Save(m.state)
	m.mu.Unlock()
	if empty != nil && time.Since(*empty) >= m.cfg.Runtime.IdleBeforeStop.Duration {
		go func() {
			m.mu.Lock()
			if m.state.Phase != Ready {
				m.mu.Unlock()
				return
			}
			m.state.Phase = Draining
			_ = m.stateFile.Save(m.state)
			m.mu.Unlock()
			if err := m.stopAndBackup(); err != nil {
				m.fail(id, err)
				m.signalDone()
				return
			}
			m.mu.Lock()
			m.state = State{Phase: Idle}
			_ = m.stateFile.Save(m.state)
			m.mu.Unlock()
			m.signalDone()
		}()
	}
}
func (m *Manager) signalDone() { m.doneOnce.Do(func() { close(m.done) }) }
func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}
