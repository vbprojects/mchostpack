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
	cmd.Dir = l.DataLink
	env := append([]string{}, os.Environ()...)
	env = append(env, "EULA=TRUE", "SERVER_IP=127.0.0.1", "SERVER_PORT=25566", "ENABLE_STATUS=TRUE", "ONLINE_MODE=TRUE", "ENABLE_RCON=TRUE", "RCON_PORT=25575", "RCON_PASSWORD="+l.RCONPassword, "MEMORY="+strconv.Itoa(p.MemoryMB)+"M", "SKIP_CHOWN_DATA=TRUE")
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
