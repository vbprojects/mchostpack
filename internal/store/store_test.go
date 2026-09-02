package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFilesystemContract(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(filepath.Join(source, "world"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "world", "level.dat"), []byte("world-data"), 0o640); err != nil {
		t.Fatal(err)
	}
	s := New(NewFilesystem(filepath.Join(root, "backups")), filepath.Join(root, "tmp"), func(string) string { return "lock" })
	if _, ok, err := s.Head(ctx, "pack"); err != nil || ok {
		t.Fatalf("head before commit: %v %v", ok, err)
	}
	m, err := s.Commit(ctx, "pack", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	if m.Generation != 1 || m.SHA256 == "" {
		t.Fatalf("bad manifest: %+v", m)
	}
	if _, err = s.Commit(ctx, "pack", source, 0); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	dest := filepath.Join(root, "restored")
	if _, err = s.Restore(ctx, "pack", dest); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dest, "world", "level.dat"))
	if err != nil || string(b) != "world-data" {
		t.Fatalf("restored %q: %v", b, err)
	}
}
func TestFilesystemRejectsEscape(t *testing.T) {
	f := NewFilesystem(t.TempDir())
	if _, err := f.path("../escape"); err == nil {
		t.Fatal("accepted escape")
	}
}
