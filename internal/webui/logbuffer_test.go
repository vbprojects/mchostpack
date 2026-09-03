package webui

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLogBufferKeepsRecentEntriesAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dashboard.log")
	logs, err := NewLogBuffer(path, 2, 1024)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"one\n", "two\n", "three\n"} {
		if _, err := logs.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	if got := logs.Recent(10); !reflect.DeepEqual(got, []string{"two", "three"}) {
		t.Fatalf("recent = %#v", got)
	}
	if err := logs.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewLogBuffer(path, 2, 1024)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if got := reopened.Recent(10); !reflect.DeepEqual(got, []string{"two", "three"}) {
		t.Fatalf("reopened recent = %#v", got)
	}
}

func TestLogBufferRotatesAtByteLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dashboard.log")
	logs, err := NewLogBuffer(path, 10, 8)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logs.Close() })
	_, _ = logs.Write([]byte("first\n"))
	_, _ = logs.Write([]byte("second\n"))
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("rotated file missing: %v", err)
	}
	if got := logs.Recent(2); !reflect.DeepEqual(got, []string{"first", "second"}) {
		t.Fatalf("recent after rotation = %#v", got)
	}
}
