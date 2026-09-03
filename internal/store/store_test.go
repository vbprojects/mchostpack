package store

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type streamingBackend struct {
	*Filesystem
	sizes []int64
}

func (b *streamingBackend) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	b.sizes = append(b.sizes, size)
	return b.Filesystem.Put(ctx, key, r, size)
}

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

func TestCommitStreamsArchiveWithoutLocalStaging(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(source, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "level.dat"), []byte("world"), 0o640); err != nil {
		t.Fatal(err)
	}
	backend := &streamingBackend{Filesystem: NewFilesystem(filepath.Join(root, "remote"))}
	s := New(backend, filepath.Join(root, "unused-temp"), nil)
	if _, err := s.Commit(context.Background(), "pack", source, 0); err != nil {
		t.Fatal(err)
	}
	if len(backend.sizes) < 2 || backend.sizes[0] != -1 {
		t.Fatalf("archive was not streamed with unknown size: %v", backend.sizes)
	}
	if _, err := os.Stat(filepath.Join(root, "unused-temp")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary archive directory was created: %v", err)
	}
}
