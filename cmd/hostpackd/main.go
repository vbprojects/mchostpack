package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/hostpack/hostpack/internal/config"
	"github.com/hostpack/hostpack/internal/flymachine"
	"github.com/hostpack/hostpack/internal/router"
	hpruntime "github.com/hostpack/hostpack/internal/runtime"
	"github.com/hostpack/hostpack/internal/store"
	"github.com/hostpack/hostpack/internal/webui"
)

const usage = `hostpackd <command>

Commands:
  serve              Run the Minecraft router and supervisor
  config validate    Validate packs.yaml and packs.lock.json
  lock [--check]     Generate or verify packs.lock.json
  backup list        List the newest backup for each configured pack
  backup restore ID  Restore a pack into an absent instance directory
  guest export       Generate the sanitized GitHub Pages pack catalog
  doctor             Check runtime prerequisites and storage connectivity
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("command failed", "error", err)
		os.Exit(1)
	}
}
func run(args []string) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	switch args[0] {
	case "serve":
		return serve(args[1:])
	case "config":
		if len(args) > 1 && args[1] == "validate" {
			return validate(args[2:])
		}
	case "lock":
		return lockCommand(args[1:])
	case "backup":
		return backupCommand(args[1:])
	case "guest":
		return guestCommand(args[1:])
	case "doctor":
		return doctor(args[1:])
	case "help", "--help", "-h":
		fmt.Print(usage)
		return nil
	}
	return fmt.Errorf("unknown command\n%s", usage)
}

func guestCommand(args []string) error {
	if len(args) == 0 || args[0] != "export" {
		return errors.New("guest requires export")
	}
	fs := flag.NewFlagSet("guest export", flag.ContinueOnError)
	configPath := fs.String("config", envDefault("HOSTPACK_CONFIG", "/app/config/packs.yaml"), "packs.yaml path")
	outputPath := fs.String("output", "guest/catalog.json", "generated catalog path")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(webui.Catalog(cfg), "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(*outputPath, append(data, '\n'), 0o644)
}

type commonFlags struct{ configPath, lockPath, stateRoot string }

func addCommon(fs *flag.FlagSet) *commonFlags {
	c := &commonFlags{}
	fs.StringVar(&c.configPath, "config", envDefault("HOSTPACK_CONFIG", "/app/config/packs.yaml"), "packs.yaml path")
	fs.StringVar(&c.lockPath, "lock", envDefault("HOSTPACK_LOCK", "/app/config/packs.lock.json"), "packs.lock.json path")
	fs.StringVar(&c.stateRoot, "state", envDefault("HOSTPACK_STATE", "/state"), "persistent state root")
	return c
}
func load(c *commonFlags) (*config.Config, *config.LockFile, error) {
	cfg, err := config.Load(c.configPath)
	if err != nil {
		return nil, nil, err
	}
	lock, err := config.LoadLock(c.lockPath)
	if err != nil {
		return nil, nil, err
	}
	if err = lock.Matches(cfg); err != nil {
		return nil, nil, err
	}
	return cfg, lock, nil
}
func validate(args []string) error {
	fs := flag.NewFlagSet("config validate", flag.ContinueOnError)
	c := addCommon(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, lock, err := load(c)
	if err != nil {
		return err
	}
	fmt.Printf("valid: %d packs, domain %s, lock %s\n", len(cfg.Packs), cfg.Domain, lock.ConfigDigest)
	return nil
}
func lockCommand(args []string) error {
	fs := flag.NewFlagSet("lock", flag.ContinueOnError)
	c := addCommon(fs)
	check := fs.Bool("check", false, "verify committed lock without provider calls")
	previousPath := fs.String("previous", "", "optional previous lock used to enforce immutable IDs")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(c.configPath)
	if err != nil {
		return err
	}
	if *check {
		l, err := config.LoadLock(c.lockPath)
		if err != nil {
			return err
		}
		if err = l.Matches(cfg); err != nil {
			return err
		}
		if *previousPath != "" {
			previous, previousErr := config.LoadLock(*previousPath)
			if previousErr != nil {
				return previousErr
			}
			if err = config.CheckImmutable(previous, l); err != nil {
				return err
			}
		}
		fmt.Println("lock matches config")
		return nil
	}
	resolver := config.Resolver{CurseForgeKey: os.Getenv("CF_API_KEY")}
	l, err := resolver.Generate(context.Background(), cfg)
	if err != nil {
		return err
	}
	if previous, previousErr := config.LoadLock(c.lockPath); previousErr == nil {
		if err = config.CheckImmutable(previous, l); err != nil {
			return err
		}
	} else if !errors.Is(previousErr, os.ErrNotExist) {
		return previousErr
	}
	b, err := l.Marshal()
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return atomicWrite(c.lockPath, b, 0o644)
}
func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	c := addCommon(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, lock, err := load(c)
	if err != nil {
		return err
	}
	webPassword := os.Getenv("HOSTPACK_WEB_PASSWORD")
	if len(webPassword) < 16 {
		return errors.New("HOSTPACK_WEB_PASSWORD must contain at least 16 characters")
	}
	logBuffer, err := webui.NewLogBuffer(filepath.Join(c.stateRoot, "runtime", "dashboard.log"), 500, 4<<20)
	if err != nil {
		return fmt.Errorf("open dashboard log: %w", err)
	}
	defer logBuffer.Close()
	logger := slog.New(slog.NewJSONHandler(io.MultiWriter(os.Stdout, logBuffer), nil))
	slog.SetDefault(logger)
	st, err := makeStore(cfg, lock, c.stateRoot)
	if err != nil {
		return err
	}
	launcher := &hpruntime.ItzgLauncher{StateRoot: c.stateRoot, DataLink: "/data", StartCommand: envDefault("HOSTPACK_START_COMMAND", "/start"), RCONPassword: os.Getenv("RCON_PASSWORD"), Logger: logger}
	sizer, err := flymachine.FromEnv()
	if err != nil {
		return err
	}
	manager := hpruntime.NewManager(cfg, lock, st, launcher, sizer, c.stateRoot, logger)
	defer manager.Close()
	minecraftListener, err := net.Listen("tcp", cfg.Runtime.ListenAddress)
	if err != nil {
		return err
	}
	webListener, err := net.Listen("tcp", cfg.Runtime.WebListenAddress)
	if err != nil {
		_ = minecraftListener.Close()
		return err
	}
	signalCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()
	ctx, cancel := context.WithCancel(signalCtx)
	defer cancel()
	webServer := &http.Server{
		Handler:           webui.New(cfg, manager, logBuffer, envDefault("HOSTPACK_WEB_USERNAME", "hostpack"), webPassword).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	webErr := make(chan error, 1)
	go func() {
		err := webServer.Serve(webListener)
		webErr <- err
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			cancel()
		}
	}()
	logger.Info("hostpackd listening", "minecraft_address", cfg.Runtime.ListenAddress, "web_address", cfg.Runtime.WebListenAddress, "domain", cfg.Domain)
	serveErr := router.New(cfg, manager, logger).Serve(ctx, minecraftListener)
	webShutdownCtx, webShutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	webShutdownErr := webServer.Shutdown(webShutdownCtx)
	webShutdownCancel()
	webServeErr := <-webErr
	if errors.Is(webServeErr, http.ErrServerClosed) {
		webServeErr = nil
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.Runtime.ShutdownTimeout.Duration)
	defer shutdownCancel()
	shutdownErr := manager.Shutdown(shutdownCtx)
	return errors.Join(serveErr, webServeErr, webShutdownErr, shutdownErr)
}
func backupCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("backup requires list or restore")
	}
	fs := flag.NewFlagSet("backup "+args[0], flag.ContinueOnError)
	c := addCommon(fs)
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	cfg, lock, err := load(c)
	if err != nil {
		return err
	}
	st, err := makeStore(cfg, lock, c.stateRoot)
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		ids := make([]string, 0, len(cfg.Packs))
		for id := range cfg.Packs {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			m, ok, e := st.Head(context.Background(), id)
			if e != nil {
				return e
			}
			if !ok {
				fmt.Printf("%s: no backup\n", id)
			} else {
				fmt.Printf("%s: generation=%d created=%s sha256=%s\n", id, m.Generation, m.CreatedAt.Format(time.RFC3339), m.SHA256)
			}
		}
		return nil
	case "restore":
		rest := fs.Args()
		if len(rest) != 1 {
			return errors.New("backup restore requires one pack ID")
		}
		id := rest[0]
		if _, ok := cfg.Packs[id]; !ok {
			return fmt.Errorf("unknown pack %q", id)
		}
		dest := filepath.Join(c.stateRoot, "instances", id, "server")
		m, e := st.Restore(context.Background(), id, dest)
		if e != nil {
			return e
		}
		fmt.Printf("restored %s generation %d to %s\n", id, m.Generation, dest)
		return nil
	default:
		return fmt.Errorf("unknown backup action %q", args[0])
	}
}
func doctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	c := addCommon(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, lock, err := load(c)
	if err != nil {
		return err
	}
	checks := []struct {
		name string
		err  error
	}{{"state runtime", writable(filepath.Join(c.stateRoot, "runtime"))}, {"Java 17", executable("/opt/java17/bin/java")}, {"Java 21", executable("/opt/java21/bin/java")}, {"start command", executable(envDefault("HOSTPACK_START_COMMAND", "/start"))}, {"dashboard password", minimumEnvironmentLength("HOSTPACK_WEB_PASSWORD", 16)}}
	st, storeErr := makeStore(cfg, lock, c.stateRoot)
	if storeErr == nil {
		_, _, storeErr = st.Head(context.Background(), firstPack(cfg))
	}
	checks = append(checks, struct {
		name string
		err  error
	}{"backup store", storeErr})
	flySizer, flyErr := flymachine.FromEnv()
	if flyErr == nil && flySizer != nil {
		checkCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		flyErr = flySizer.Check(checkCtx)
		cancel()
	}
	checks = append(checks, struct {
		name string
		err  error
	}{"Fly resize API", flyErr})
	failed := false
	for _, check := range checks {
		if check.err != nil {
			fmt.Printf("FAIL %-16s %v\n", check.name, check.err)
			failed = true
		} else {
			fmt.Printf("OK   %s\n", check.name)
		}
	}
	if failed {
		return errors.New("one or more doctor checks failed")
	}
	return nil
}
func makeStore(cfg *config.Config, lock *config.LockFile, stateRoot string) (store.Store, error) {
	return store.FromConfig(cfg.Storage, filepath.Join(stateRoot, "runtime", "tmp"), func(id string) string {
		if p, ok := lock.Packs[id]; ok {
			return p.IdentityDigest
		}
		return ""
	})
}
func firstPack(c *config.Config) string {
	for id := range c.Packs {
		return id
	}
	return "doctor"
}
func minimumEnvironmentLength(name string, minimum int) error {
	if len(os.Getenv(name)) < minimum {
		return fmt.Errorf("%s must contain at least %d characters", name, minimum)
	}
	return nil
}
func writable(path string) error {
	if err := os.MkdirAll(path, 0o750); err != nil {
		return err
	}
	f, err := os.CreateTemp(path, ".doctor-")
	if err != nil {
		return err
	}
	name := f.Name()
	if err = f.Close(); err != nil {
		return err
	}
	return os.Remove(name)
}
func executable(path string) error {
	if strings.ContainsRune(path, os.PathSeparator) {
		st, err := os.Stat(path)
		if err != nil {
			return err
		}
		if st.Mode()&0o111 == 0 {
			return fmt.Errorf("not executable")
		}
		return nil
	}
	_, err := exec.LookPath(path)
	return err
}
func envDefault(key, value string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return value
}
func atomicWrite(path string, b []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".write-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	if ce := f.Close(); err == nil {
		err = ce
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
