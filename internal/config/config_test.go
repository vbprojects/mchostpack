package config

import (
	"testing"
	"time"
)

func validConfig() *Config {
	return &Config{Schema: 1, Domain: "mc.example.com", Runtime: Runtime{StartupWait: Duration{time.Second}, StartupTimeout: Duration{time.Minute}, StatusIdleExit: Duration{time.Second}, EmptyBeforeSwitch: Duration{time.Second}, IdleBeforeStop: Duration{time.Second}, BackendPollInterval: Duration{time.Second}, ShutdownTimeout: Duration{time.Second}, MaxConnections: 2, ConnectionsPerMinute: 3, ListenAddress: ":1", BackendAddress: "127.0.0.1:2"}, Capacity: Capacity{MemoryMB: 4096, CPUs: 4}, Storage: StorageConfig{Driver: "filesystem", Filesystem: FilesystemConfig{Root: "/tmp/b"}}, Packs: map[string]Pack{"alpha": {DisplayName: "Alpha", Provider: "modrinth", ProjectID: "p", VersionID: "v", Java: 17, MemoryMB: 2048, MachineMemoryMB: 3072, MachineCPUs: 2}}}
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

func TestPackMachineResourcesMustFitCapacity(t *testing.T) {
	c := validConfig()
	p := c.Packs["alpha"]
	p.MachineMemoryMB = 4352
	p.MachineCPUs = 5
	c.Packs["alpha"] = p
	if err := c.Validate(); err == nil {
		t.Fatal("allowed pack resources above configured capacity")
	}
}

func TestPackMachineMemoryLeavesNonHeapHeadroom(t *testing.T) {
	c := validConfig()
	p := c.Packs["alpha"]
	p.MachineMemoryMB = p.MemoryMB
	c.Packs["alpha"] = p
	if err := c.Validate(); err == nil {
		t.Fatal("allowed Java heap to consume the entire Machine memory")
	}
}

func TestModrinthExclusionsAreProviderSpecific(t *testing.T) {
	c := validConfig()
	p := c.Packs["alpha"]
	p.ModrinthExcludeFiles = []string{"client-only"}
	c.Packs["alpha"] = p
	if err := c.Validate(); err != nil {
		t.Fatalf("valid Modrinth exclusion rejected: %v", err)
	}
	p.Provider = "curseforge"
	p.VersionID = ""
	p.FileID = 1
	c.Packs["alpha"] = p
	if err := c.Validate(); err == nil {
		t.Fatal("allowed Modrinth exclusions on a CurseForge pack")
	}
}

func TestModrinthExclusionsRejectAmbiguousSeparators(t *testing.T) {
	c := validConfig()
	p := c.Packs["alpha"]
	p.ModrinthExcludeFiles = []string{"one,two"}
	c.Packs["alpha"] = p
	if err := c.Validate(); err == nil {
		t.Fatal("allowed a comma inside a Modrinth exclusion")
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

func TestPackResourcesDoNotChangeWorldIdentity(t *testing.T) {
	p := validConfig().Packs["alpha"]
	before := packIdentityDigest(p)
	p.MemoryMB = 1024
	p.MachineMemoryMB = 2048
	p.MachineCPUs = 1
	p.ModrinthExcludeFiles = []string{"client-only"}
	if after := packIdentityDigest(p); after != before {
		t.Fatalf("resource tuning changed immutable world identity: %s != %s", after, before)
	}
}
