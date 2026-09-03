package runtime

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/hostpack/hostpack/internal/config"
)

type Process interface {
	Wait() error
	Signal(os.Signal) error
	Kill() error
}
type Launcher interface {
	Start(context.Context, string, config.Pack, config.LockedPack) (Process, error)
}
type ItzgLauncher struct {
	StateRoot, DataLink, StartCommand, RCONPassword string
	Logger                                          *slog.Logger
}

func (l *ItzgLauncher) Start(ctx context.Context, id string, p config.Pack, lp config.LockedPack) (Process, error) {
	instance := filepath.Join(l.StateRoot, "instances", id, "server")
	if err := os.MkdirAll(instance, 0o750); err != nil {
		return nil, err
	}
	current := filepath.Join(l.StateRoot, "runtime", "current")
	if err := os.MkdirAll(filepath.Dir(current), 0o750); err != nil {
		return nil, err
	}
	tmp := current + ".new"
	_ = os.Remove(tmp)
	if err := os.Symlink(instance, tmp); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, current); err != nil {
		return nil, err
	}
	start := l.StartCommand
	if start == "" {
		start = "/start"
	}
	cmd := exec.CommandContext(ctx, start)
	// Run from the real instance path. Some upstream installers emit a SERVER
	// path relative to the process working directory; using the nested /data
	// symlink can otherwise produce paths such as ../state/... that do not
	// resolve from the instance directory on the final exec.
	cmd.Dir = instance
	uid, err := childID("HOSTPACK_MINECRAFT_UID", 1000)
	if err != nil {
		return nil, err
	}
	gid, err := childID("HOSTPACK_MINECRAFT_GID", 1000)
	if err != nil {
		return nil, err
	}
	for _, root := range []string{filepath.Dir(instance), instance} {
		if err := ensureTreeOwner(root, uid, gid); err != nil {
			return nil, err
		}
	}
	if p.Provider == "modrinth" {
		if err := repairModrinthServerPath(instance, uid, gid); err != nil {
			return nil, err
		}
	}
	home := filepath.Join(l.StateRoot, "runtime", "tmp", "minecraft-home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, fmt.Errorf("create Minecraft runtime home: %w", err)
	}
	if err := os.Chown(home, int(uid), int(gid)); err != nil {
		return nil, fmt.Errorf("set Minecraft runtime home ownership: %w", err)
	}
	if err := os.Chmod(home, 0o700); err != nil {
		return nil, fmt.Errorf("set Minecraft runtime home permissions: %w", err)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: gid, NoSetGroups: true}}
	env := childEnvironment(os.Environ())
	env = append(env, "HOME="+home, "PWD="+instance, "EULA=TRUE", "SERVER_IP=127.0.0.1", "SERVER_PORT=25566", "ENABLE_STATUS=TRUE", "ONLINE_MODE=TRUE", "ENABLE_RCON=TRUE", "RCON_PORT=25575", "RCON_PASSWORD="+l.RCONPassword, "MEMORY="+strconv.Itoa(p.MemoryMB)+"M", "SKIP_CHOWN_DATA=TRUE")
	if p.Java == 17 {
		env = append(env, "PATH=/opt/java17/bin:"+os.Getenv("PATH"), "JAVA_HOME=/opt/java17")
	} else {
		env = append(env, "PATH=/opt/java21/bin:"+os.Getenv("PATH"), "JAVA_HOME=/opt/java21")
	}
	if p.Provider == "modrinth" {
		env = append(env, "TYPE=MODRINTH", "MODRINTH_MODPACK="+p.ProjectID, "MODRINTH_VERSION="+p.VersionID)
	} else {
		env = append(env, "TYPE=AUTO_CURSEFORGE", "CF_SLUG="+lp.Slug, "CF_FILE_ID="+strconv.FormatInt(p.FileID, 10))
	}
	cmd.Env = env
	cmd.Stdout = &logWriter{logger: l.Logger, level: slog.LevelInfo, id: id}
	cmd.Stderr = &logWriter{logger: l.Logger, level: slog.LevelError, id: id}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start minecraft: %w", err)
	}
	return &commandProcess{cmd: cmd}, nil
}

func ensureTreeOwner(root string, uid, gid uint32) error {
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat.Uid == uid && stat.Gid == gid {
		return nil
	}
	if err := filepath.WalkDir(root, func(path string, _ os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		return os.Lchown(path, int(uid), int(gid))
	}); err != nil {
		return fmt.Errorf("set Minecraft instance ownership: %w", err)
	}
	return nil
}

func childID(name string, fallback uint64) (uint32, error) {
	value := os.Getenv(name)
	if value == "" {
		value = strconv.FormatUint(fallback, 10)
	}
	id, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%s must be a numeric user or group ID: %w", name, err)
	}
	return uint32(id), nil
}

func childEnvironment(parent []string) []string {
	blocked := map[string]bool{
		"AWS_ACCESS_KEY_ID":      true,
		"AWS_SECRET_ACCESS_KEY":  true,
		"AWS_SESSION_TOKEN":      true,
		"FLY_API_TOKEN":          true,
		"FLY_TOKEN":              true,
		"HOME":                   true,
		"HOSTPACK_FLY_API_TOKEN": true,
		"PWD":                    true,
	}
	result := make([]string, 0, len(parent))
	for _, entry := range parent {
		name, _, _ := strings.Cut(entry, "=")
		if blocked[name] || strings.HasPrefix(name, "RCLONE_CONFIG_") {
			continue
		}
		result = append(result, entry)
	}
	return result
}

func repairModrinthServerPath(instance string, uid, gid uint32) error {
	path := filepath.Join(instance, ".install-modrinth.env")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read Modrinth installer results: %w", err)
	}
	lines := strings.Split(string(data), "\n")
	changed := false
	for i, line := range lines {
		if !strings.HasPrefix(line, "SERVER=") {
			continue
		}
		value := strings.Trim(strings.TrimPrefix(line, "SERVER="), "\"'")
		if value != "run.sh" && filepath.Base(value) == "run.sh" {
			if _, statErr := os.Stat(filepath.Join(instance, "run.sh")); statErr == nil {
				lines[i] = `SERVER="run.sh"`
				changed = true
			}
		}
		break
	}
	if !changed {
		return nil
	}
	tmp, err := os.CreateTemp(instance, ".install-modrinth-repair-")
	if err != nil {
		return fmt.Errorf("repair Modrinth installer results: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err = tmp.WriteString(strings.Join(lines, "\n")); err == nil {
		err = tmp.Sync()
	}
	if err == nil {
		err = tmp.Chmod(0o640)
	}
	if err == nil {
		err = tmp.Chown(int(uid), int(gid))
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("repair Modrinth installer results: %w", err)
	}
	if err = os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace Modrinth installer results: %w", err)
	}
	return nil
}

type commandProcess struct{ cmd *exec.Cmd }

func (p *commandProcess) Wait() error                { return p.cmd.Wait() }
func (p *commandProcess) Signal(sig os.Signal) error { return p.cmd.Process.Signal(sig) }
func (p *commandProcess) Kill() error                { return p.cmd.Process.Kill() }

type logWriter struct {
	logger *slog.Logger
	level  slog.Level
	id     string
}

func (w *logWriter) Write(p []byte) (int, error) {
	if w.logger != nil {
		w.logger.Log(context.Background(), w.level, "minecraft", "pack", w.id, "output", string(p))
	}
	return len(p), nil
}

var _ io.Writer = (*logWriter)(nil)
