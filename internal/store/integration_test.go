package store

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/hostpack/hostpack/internal/config"
)

func runBackendContract(t *testing.T, backend Backend) {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(filepath.Join(source, "world", "dimensions"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "world", "level.dat"), []byte("contract-world"), 0o640); err != nil {
		t.Fatal(err)
	}
	s := New(backend, filepath.Join(root, "tmp"), func(string) string { return "pack-lock" })
	m, err := s.Commit(context.Background(), "contract", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	if m.Generation != 1 {
		t.Fatalf("generation = %d", m.Generation)
	}
	if _, err = s.Commit(context.Background(), "contract", source, 0); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	destination := filepath.Join(root, "restored")
	if _, err = s.Restore(context.Background(), "contract", destination); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(destination, "world", "level.dat"))
	if err != nil || string(got) != "contract-world" {
		t.Fatalf("restore = %q, %v", got, err)
	}
}

func TestRcloneContract(t *testing.T) {
	if _, err := exec.LookPath("rclone"); err != nil {
		t.Skip("rclone is not installed")
	}
	runBackendContract(t, NewRclone(":local:"+filepath.Join(t.TempDir(), "remote")))
}

func TestS3Contract(t *testing.T) {
	endpoint := os.Getenv("TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("TEST_S3_ENDPOINT is not configured")
	}
	cfg := config.S3Config{Endpoint: endpoint, Bucket: "hostpack-tests", Prefix: "contract", Region: "us-east-1"}
	backend, err := NewS3(cfg)
	if err != nil {
		t.Fatal(err)
	}
	err = backend.client.MakeBucket(context.Background(), cfg.Bucket, minioMakeBucketOptions(cfg.Region))
	if err != nil {
		exists, existsErr := backend.client.BucketExists(context.Background(), cfg.Bucket)
		if existsErr != nil || !exists {
			t.Fatalf("create bucket: %v / %v", err, existsErr)
		}
	}
	runBackendContract(t, backend)
}
