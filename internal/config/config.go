package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Duration struct{ time.Duration }

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	v, err := time.ParseDuration(node.Value)
	if err != nil {
		return err
	}
	d.Duration = v
	return nil
}

type Config struct {
	Schema   int             `yaml:"schema" json:"schema"`
	Domain   string          `yaml:"domain" json:"domain"`
	Runtime  Runtime         `yaml:"runtime" json:"runtime"`
	Capacity Capacity        `yaml:"capacity" json:"capacity"`
	Storage  StorageConfig   `yaml:"storage" json:"storage"`
	Packs    map[string]Pack `yaml:"packs" json:"packs"`
}

type Runtime struct {
	StartupWait          Duration `yaml:"startup_wait" json:"startupWait"`
	StatusIdleExit       Duration `yaml:"status_idle_exit" json:"statusIdleExit"`
	EmptyBeforeSwitch    Duration `yaml:"empty_before_switch" json:"emptyBeforeSwitch"`
	IdleBeforeStop       Duration `yaml:"idle_before_stop" json:"idleBeforeStop"`
	BackendPollInterval  Duration `yaml:"backend_poll_interval" json:"backendPollInterval"`
	ShutdownTimeout      Duration `yaml:"shutdown_timeout" json:"shutdownTimeout"`
	MaxConnections       int      `yaml:"max_connections" json:"maxConnections"`
	ConnectionsPerMinute int      `yaml:"connections_per_minute" json:"connectionsPerMinute"`
	ListenAddress        string   `yaml:"listen_address" json:"listenAddress"`
	BackendAddress       string   `yaml:"backend_address" json:"backendAddress"`
}

type Capacity struct {
	MemoryMB int `yaml:"memory_mb" json:"memoryMb"`
	CPUs     int `yaml:"cpus" json:"cpus"`
}

type StorageConfig struct {
	Driver           string           `yaml:"driver" json:"driver"`
	EvictAfterBackup bool             `yaml:"evict_after_backup,omitempty" json:"evictAfterBackup,omitempty"`
	Filesystem       FilesystemConfig `yaml:"filesystem,omitempty" json:"filesystem,omitempty"`
	S3               S3Config         `yaml:"s3,omitempty" json:"s3,omitempty"`
	Rclone           RcloneConfig     `yaml:"rclone,omitempty" json:"rclone,omitempty"`
}
type FilesystemConfig struct {
	Root string `yaml:"root" json:"root"`
}
type S3Config struct {
	Endpoint string `yaml:"endpoint" json:"endpoint"`
	Bucket   string `yaml:"bucket" json:"bucket"`
	Prefix   string `yaml:"prefix" json:"prefix"`
	Region   string `yaml:"region" json:"region"`
	Secure   *bool  `yaml:"secure,omitempty" json:"secure,omitempty"`
}
type RcloneConfig struct {
	Remote string `yaml:"remote" json:"remote"`
}

type Pack struct {
	DisplayName     string `yaml:"display_name" json:"displayName"`
	Provider        string `yaml:"provider" json:"provider"`
	ProjectID       string `yaml:"project_id" json:"projectId"`
	VersionID       string `yaml:"version_id,omitempty" json:"versionId,omitempty"`
	FileID          int64  `yaml:"file_id,omitempty" json:"fileId,omitempty"`
	Java            int    `yaml:"java" json:"java"`
	MemoryMB        int    `yaml:"memory_mb" json:"memoryMb"`
	MachineMemoryMB int    `yaml:"machine_memory_mb" json:"machineMemoryMb"`
	MachineCPUs     int    `yaml:"machine_cpus" json:"machineCpus"`
}

var dnsLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) Validate() error {
	var problems []string
	if c.Schema != 1 {
		problems = append(problems, "schema must be 1")
	}
	c.Domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(c.Domain), "."))
	if c.Domain == "" || len(c.Domain) > 253 {
		problems = append(problems, "domain must be a valid non-empty DNS name")
	}
	for _, p := range strings.Split(c.Domain, ".") {
		if !dnsLabel.MatchString(p) {
			problems = append(problems, "domain contains invalid DNS label")
			break
		}
	}
	if c.Capacity.MemoryMB <= 0 {
		problems = append(problems, "capacity.memory_mb must be positive")
	}
	if c.Capacity.CPUs <= 0 {
		problems = append(problems, "capacity.cpus must be positive")
	}
	if c.Runtime.StartupWait.Duration <= 0 || c.Runtime.StatusIdleExit.Duration <= 0 || c.Runtime.EmptyBeforeSwitch.Duration <= 0 || c.Runtime.IdleBeforeStop.Duration <= 0 || c.Runtime.BackendPollInterval.Duration <= 0 || c.Runtime.ShutdownTimeout.Duration <= 0 {
		problems = append(problems, "all runtime durations must be positive")
	}
	if c.Runtime.ListenAddress == "" || c.Runtime.BackendAddress == "" {
		problems = append(problems, "runtime listen and backend addresses are required")
	}
	if c.Runtime.MaxConnections <= 0 || c.Runtime.ConnectionsPerMinute <= 0 {
		problems = append(problems, "connection limits must be positive")
	}
	switch c.Storage.Driver {
	case "filesystem":
		if c.Storage.Filesystem.Root == "" {
			problems = append(problems, "storage.filesystem.root is required")
		}
		if c.Storage.EvictAfterBackup {
			problems = append(problems, "storage.evict_after_backup requires s3 or rclone storage")
		}
	case "s3":
		if c.Storage.S3.Endpoint == "" || c.Storage.S3.Bucket == "" {
			problems = append(problems, "storage.s3 endpoint and bucket are required")
		}
	case "rclone":
		if c.Storage.Rclone.Remote == "" {
			problems = append(problems, "storage.rclone.remote is required")
		}
	default:
		problems = append(problems, "storage.driver must be filesystem, s3, or rclone")
	}
	for id, pack := range c.Packs {
		if !dnsLabel.MatchString(id) {
			problems = append(problems, fmt.Sprintf("pack %q is not a lowercase DNS label", id))
		}
		if pack.DisplayName == "" || pack.ProjectID == "" {
			problems = append(problems, fmt.Sprintf("pack %q requires display_name and project_id", id))
		}
		if pack.Java != 17 && pack.Java != 21 {
			problems = append(problems, fmt.Sprintf("pack %q java must be 17 or 21", id))
		}
		if pack.MemoryMB <= 0 {
			problems = append(problems, fmt.Sprintf("pack %q memory_mb must be positive", id))
		}
		if pack.MachineMemoryMB <= 0 || pack.MachineMemoryMB > c.Capacity.MemoryMB || pack.MachineMemoryMB%256 != 0 {
			problems = append(problems, fmt.Sprintf("pack %q machine_memory_mb must be a multiple of 256 that fits capacity", id))
		}
		if pack.MemoryMB+256 > pack.MachineMemoryMB {
			problems = append(problems, fmt.Sprintf("pack %q machine_memory_mb must leave at least 256 MB outside the Java heap", id))
		}
		if pack.MachineCPUs <= 0 || pack.MachineCPUs > c.Capacity.CPUs {
			problems = append(problems, fmt.Sprintf("pack %q machine_cpus must fit capacity", id))
		}
		switch pack.Provider {
		case "modrinth":
			if pack.VersionID == "" || pack.FileID != 0 {
				problems = append(problems, fmt.Sprintf("pack %q requires version_id and no file_id", id))
			}
		case "curseforge":
			if pack.FileID <= 0 || pack.VersionID != "" {
				problems = append(problems, fmt.Sprintf("pack %q requires file_id and no version_id", id))
			}
		default:
			problems = append(problems, fmt.Sprintf("pack %q has unsupported provider", id))
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func (c *Config) PackForHost(host string) (string, Pack, bool) {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	suffix := "." + c.Domain
	if !strings.HasSuffix(host, suffix) {
		return "", Pack{}, false
	}
	id := strings.TrimSuffix(host, suffix)
	if strings.Contains(id, ".") || id == "" {
		return "", Pack{}, false
	}
	p, ok := c.Packs[id]
	return id, p, ok
}

type identityPack struct {
	Provider, ProjectID, VersionID string
	FileID                         int64
	Java                           int
}

func (c *Config) IdentityDigest() string {
	ids := make([]string, 0, len(c.Packs))
	for id := range c.Packs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	ordered := make([]any, 0, len(ids))
	for _, id := range ids {
		p := c.Packs[id]
		ordered = append(ordered, struct {
			ID   string
			Pack identityPack
		}{id, identityPack{p.Provider, p.ProjectID, p.VersionID, p.FileID, p.Java}})
	}
	b, _ := json.Marshal(ordered)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
