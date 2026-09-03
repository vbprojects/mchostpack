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
	"github.com/hostpack/hostpack/internal/store"
)

type backupStore struct {
	commitErr error
}

func (s *backupStore) Head(context.Context, string) (store.Manifest, bool, error) {
	return store.Manifest{}, false, nil
}

func (s *backupStore) Restore(context.Context, string, string) (store.Manifest, error) {
	return store.Manifest{}, errors.New("not implemented")
}

func (s *backupStore) Commit(_ context.Context, id, source string, expected uint64) (store.Manifest, error) {
	if _, err := os.Stat(filepath.Join(source, "world", "level.dat")); err != nil {
		return store.Manifest{}, err
	}
	if s.commitErr != nil {
		return store.Manifest{}, s.commitErr
	}
	return store.Manifest{ID: id, Generation: expected + 1, SHA256: "verified"}, nil
}

func TestVerifiedRemoteBackupEvictsServerDirectory(t *testing.T) {
	root := t.TempDir()
	serverDir := createServerDirectory(t, root)
	m := storageTestManager(root, &backupStore{})
	if err := m.stopAndBackup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(serverDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("server directory still exists after verified backup: %v", err)
	}
	if m.state.Generation != 1 || m.state.BackupPending {
		t.Fatalf("unexpected state after eviction: %+v", m.state)
	}
}

func TestFailedRemoteBackupRetainsServerDirectory(t *testing.T) {
	root := t.TempDir()
	serverDir := createServerDirectory(t, root)
	m := storageTestManager(root, &backupStore{commitErr: errors.New("tigris unavailable")})
	if err := m.stopAndBackup(); err == nil {
		t.Fatal("expected backup failure")
	}
	if _, err := os.Stat(serverDir); err != nil {
		t.Fatalf("server directory was not retained: %v", err)
	}
	if !m.state.BackupPending {
		t.Fatalf("backup failure was not persisted: %+v", m.state)
	}
}

func storageTestManager(root string, st store.Store) *Manager {
	return &Manager{
		cfg:       &config.Config{Storage: config.StorageConfig{Driver: "s3", EvictAfterBackup: true}},
		store:     st,
		stateFile: NewStateFile(filepath.Join(root, "runtime", "active.json")),
		stateRoot: root,
		state:     State{Phase: Ready, ActiveID: "pack"},
		log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func createServerDirectory(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, "instances", "pack", "server")
	if err := os.MkdirAll(filepath.Join(dir, "world"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "world", "level.dat"), []byte("world"), 0o640); err != nil {
		t.Fatal(err)
	}
	return dir
}
