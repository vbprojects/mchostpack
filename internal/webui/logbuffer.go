package webui

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// LogBuffer retains recent structured log lines in memory and in one bounded,
// rotated file. The persistent copy survives Machine stops and pack eviction.
type LogBuffer struct {
	mu         sync.Mutex
	path       string
	file       *os.File
	entries    []string
	maxEntries int
	maxBytes   int64
	size       int64
}

func NewLogBuffer(path string, maxEntries int, maxBytes int64) (*LogBuffer, error) {
	if maxEntries <= 0 || maxBytes <= 0 {
		return nil, errors.New("log limits must be positive")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	b := &LogBuffer{path: path, maxEntries: maxEntries, maxBytes: maxBytes}
	for _, candidate := range []string{path + ".1", path} {
		if err := b.load(candidate); err != nil {
			return nil, err
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return nil, err
	}
	b.file = f
	if info, err := f.Stat(); err == nil {
		b.size = info.Size()
	}
	return b, nil
}

func (b *LogBuffer) load(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		b.appendEntry(scanner.Text())
	}
	return scanner.Err()
}

func (b *LogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.size+int64(len(p)) > b.maxBytes {
		if err := b.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := b.file.Write(p)
	b.size += int64(n)
	for _, line := range strings.Split(strings.TrimSuffix(string(p[:n]), "\n"), "\n") {
		if line != "" {
			b.appendEntry(line)
		}
	}
	return n, err
}

func (b *LogBuffer) rotate() error {
	if err := b.file.Close(); err != nil {
		return err
	}
	if err := os.Remove(b.path + ".1"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(b.path, b.path+".1"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	f, err := os.OpenFile(b.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	b.file = f
	b.size = 0
	return nil
}

func (b *LogBuffer) appendEntry(line string) {
	b.entries = append(b.entries, line)
	if len(b.entries) > b.maxEntries {
		copy(b.entries, b.entries[len(b.entries)-b.maxEntries:])
		b.entries = b.entries[:b.maxEntries]
	}
}

func (b *LogBuffer) Recent(limit int) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if limit <= 0 || limit > len(b.entries) {
		limit = len(b.entries)
	}
	result := make([]string, limit)
	copy(result, b.entries[len(b.entries)-limit:])
	return result
}

func (b *LogBuffer) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.file == nil {
		return nil
	}
	err := b.file.Close()
	b.file = nil
	return err
}
