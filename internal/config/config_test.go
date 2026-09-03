package config

import (
	"testing"
	"time"
)

func validConfig() *Config {
	return &Config{Schema: 1, Domain: "mc.example.com", Runtime: Runtime{StartupWait: Duration{time.Second}, StatusIdleExit: Duration{time.Second}, EmptyBeforeSwitch: Duration{time.Second}, IdleBeforeStop: Duration{time.Second}, BackendPollInterval: Duration{time.Second}, ShutdownTimeout: Duration{time.Second}, MaxConnections: 2, ConnectionsPerMinute: 3, ListenAddress: ":1", BackendAddress: "127.0.0.1:2"}, Capacity: Capacity{MemoryMB: 4096}, Storage: StorageConfig{Driver: "filesystem", Filesystem: FilesystemConfig{Root: "/tmp/b"}}, Packs: map[string]Pack{"alpha": {DisplayName: "Alpha", Provider: "modrinth", ProjectID: "p", VersionID: "v", Java: 17, MemoryMB: 2048}}}
}

func TestPackForHost(t *testing.T) {
	c := validConfig()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	id, _, ok := c.PackForHost("ALPHA.mc.example.com:25565")
	if !ok || id != "alpha" {
		t.Fatalf("got %q %v", id, ok)
	}
	if _, _, ok := c.PackForHost("alpha.evil.example"); ok {
		t.Fatal("accepted wrong domain")
	}
}
func TestValidation(t *testing.T) {
	c := validConfig()
	c.Packs["Bad_ID"] = c.Packs["alpha"]
	if c.Validate() == nil {
		t.Fatal("expected invalid DNS label")
	}
}

func TestFilesystemStorageCannotEvictSource(t *testing.T) {
	c := validConfig()
	c.Storage.EvictAfterBackup = true
	if err := c.Validate(); err == nil {
		t.Fatal("allowed eviction with filesystem backup storage")
	}
}
func TestLockMatch(t *testing.T) {
	c := validConfig()
	_ = c.Validate()
	p := c.Packs["alpha"]
	l := &LockFile{Schema: 1, ConfigDigest: c.IdentityDigest(), Packs: map[string]LockedPack{"alpha": {Provider: p.Provider, ProjectID: p.ProjectID, VersionID: p.VersionID, Java: p.Java, IdentityDigest: packIdentityDigest(p)}}}
	if err := l.Matches(c); err != nil {
		t.Fatal(err)
	}
	p.VersionID = "changed"
	c.Packs["alpha"] = p
	if l.Matches(c) == nil {
		t.Fatal("expected immutable mismatch")
	}
}

func TestImmutableLock(t *testing.T) {
	p := validConfig().Packs["alpha"]
	old := &LockFile{Packs: map[string]LockedPack{"alpha": {IdentityDigest: packIdentityDigest(p)}}}
	if err := CheckImmutable(old, &LockFile{Packs: map[string]LockedPack{}}); err == nil {
		t.Fatal("allowed removal")
	}
	p.VersionID = "new"
	if err := CheckImmutable(old, &LockFile{Packs: map[string]LockedPack{"alpha": {IdentityDigest: packIdentityDigest(p)}}}); err == nil {
		t.Fatal("allowed identity change")
	}
}
