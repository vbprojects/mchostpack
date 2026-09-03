package runtime

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/hostpack/hostpack/internal/config"
)

type resizeSizer struct {
	changed bool
	err     error
	calls   int
}

func (s *resizeSizer) Ensure(context.Context, config.Pack) (bool, error) {
	s.calls++
	return s.changed, s.err
}

type recordingLauncher struct{ starts int }

func (l *recordingLauncher) Start(context.Context, string, config.Pack, config.LockedPack) (Process, error) {
	l.starts++
	return nil, errors.New("launcher should not run")
}

func TestResizeHappensBeforeStorageOrJava(t *testing.T) {
	root := t.TempDir()
	sizer := &resizeSizer{changed: true}
	launcher := &recordingLauncher{}
	m := &Manager{
		cfg: &config.Config{Packs: map[string]config.Pack{"pack": {
			DisplayName: "Pack", MachineMemoryMB: 4096, MachineCPUs: 2,
		}}},
		sizer:     sizer,
		launcher:  launcher,
		stateFile: NewStateFile(filepath.Join(root, "runtime", "active.json")),
		stateRoot: root,
		state:     State{Phase: Loading, ActiveID: "pack"},
		log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		ctx:       context.Background(),
	}
	m.start("pack", false)
	if sizer.calls != 1 || launcher.starts != 0 {
		t.Fatalf("unexpected calls: resize=%d launch=%d", sizer.calls, launcher.starts)
	}
	if m.state.Phase != Resizing || m.state.ActiveID != "pack" {
		t.Fatalf("resize intent was not persisted: %+v", m.state)
	}
	loaded, err := m.stateFile.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Phase != Resizing || loaded.ActiveID != "pack" {
		t.Fatalf("unexpected persisted state: %+v", loaded)
	}
}

func TestResizeFailureFailsClosedBeforeJava(t *testing.T) {
	root := t.TempDir()
	sizer := &resizeSizer{err: errors.New("denied")}
	launcher := &recordingLauncher{}
	m := &Manager{
		cfg:       &config.Config{Packs: map[string]config.Pack{"pack": {DisplayName: "Pack", MachineMemoryMB: 4096, MachineCPUs: 2}}},
		sizer:     sizer,
		launcher:  launcher,
		stateFile: NewStateFile(filepath.Join(root, "runtime", "active.json")),
		stateRoot: root,
		state:     State{Phase: Loading, ActiveID: "pack"},
		log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		ctx:       context.Background(),
	}
	m.start("pack", false)
	if launcher.starts != 0 || m.state.Phase != Failed {
		t.Fatalf("resize failure did not fail closed: starts=%d state=%+v", launcher.starts, m.state)
	}
	if _, err := os.Stat(filepath.Join(root, "instances", "pack", "server")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("instance directory was touched before resize: %v", err)
	}
}
