package runtime

import (
	"path/filepath"
	"testing"
)

func TestStateRoundTrip(t *testing.T) {
	f := NewStateFile(filepath.Join(t.TempDir(), "runtime", "active.json"))
	before := State{Phase: Ready, ActiveID: "alpha", Generation: 7, BackupPending: true}
	if err := f.Save(before); err != nil {
		t.Fatal(err)
	}
	after, err := f.Load()
	if err != nil {
		t.Fatal(err)
	}
	if after.Phase != Ready || after.ActiveID != "alpha" || after.Generation != 7 || !after.BackupPending {
		t.Fatalf("bad state: %+v", after)
	}
}
