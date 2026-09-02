package store

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

type Rclone struct{ remote string }

func NewRclone(remote string) *Rclone      { return &Rclone{remote: strings.TrimSuffix(remote, "/")} }
func (r *Rclone) target(key string) string { return r.remote + "/" + key }
func (r *Rclone) List(ctx context.Context, prefix string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "rclone", "lsf", "--recursive", "--files-only", r.target(prefix))
	b, err := cmd.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 3 {
			return nil, nil
		}
		return nil, fmt.Errorf("rclone lsf: %w", err)
	}
	var out []string
	s := bufio.NewScanner(strings.NewReader(string(b)))
	for s.Scan() {
		out = append(out, prefix+s.Text())
	}
	return out, s.Err()
}
func (r *Rclone) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	cmd := exec.CommandContext(ctx, "rclone", "cat", r.target(key))
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	return &commandReader{ReadCloser: pipe, cmd: cmd}, nil
}
func (r *Rclone) Put(ctx context.Context, key string, src io.Reader, _ int64) error {
	cmd := exec.CommandContext(ctx, "rclone", "rcat", r.target(key))
	cmd.Stdin = src
	if b, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("rclone rcat: %w: %s", err, strings.TrimSpace(string(b)))
	}
	return nil
}

type commandReader struct {
	io.ReadCloser
	cmd *exec.Cmd
}

func (c *commandReader) Close() error {
	readErr := c.ReadCloser.Close()
	waitErr := c.cmd.Wait()
	if waitErr != nil {
		return waitErr
	}
	return readErr
}
