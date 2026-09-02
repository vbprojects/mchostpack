package store

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Filesystem struct{ root string }

func NewFilesystem(root string) *Filesystem { return &Filesystem{root: root} }
func (f *Filesystem) path(key string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(key))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid backup key")
	}
	return filepath.Join(f.root, clean), nil
}
func (f *Filesystem) List(_ context.Context, prefix string) ([]string, error) {
	root, err := f.path(prefix)
	if err != nil {
		return nil, err
	}
	var out []string
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
		if os.IsNotExist(e) {
			return filepath.SkipDir
		}
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		rel, e := filepath.Rel(f.root, path)
		if e == nil {
			out = append(out, filepath.ToSlash(rel))
		}
		return e
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	return out, err
}
func (f *Filesystem) Open(_ context.Context, key string) (io.ReadCloser, error) {
	p, e := f.path(key)
	if e != nil {
		return nil, e
	}
	return os.Open(p)
}
func (f *Filesystem) Put(_ context.Context, key string, r io.Reader, _ int64) error {
	p, e := f.path(key)
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(p), 0o750); e != nil {
		return e
	}
	tmp, e := os.CreateTemp(filepath.Dir(p), ".upload-")
	if e != nil {
		return e
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, e = io.Copy(tmp, r); e == nil {
		e = tmp.Sync()
	}
	if ce := tmp.Close(); e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	return os.Rename(name, p)
}
