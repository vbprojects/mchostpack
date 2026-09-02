package store

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hostpack/hostpack/internal/config"
	"github.com/klauspost/compress/zstd"
)

var ErrGenerationConflict = errors.New("backup generation conflict")

type Manifest struct {
	ID             string    `json:"id"`
	Generation     uint64    `json:"generation"`
	SHA256         string    `json:"sha256"`
	Size           int64     `json:"size"`
	PackLockDigest string    `json:"packLockDigest"`
	CreatedAt      time.Time `json:"createdAt"`
}

type Store interface {
	Head(context.Context, string) (Manifest, bool, error)
	Restore(context.Context, string, string) (Manifest, error)
	Commit(context.Context, string, string, uint64) (Manifest, error)
}

type Backend interface {
	List(context.Context, string) ([]string, error)
	Open(context.Context, string) (io.ReadCloser, error)
	Put(context.Context, string, io.Reader, int64) error
}

type ArchiveStore struct {
	backend    Backend
	lockDigest func(string) string
	tempRoot   string
}

func New(backend Backend, tempRoot string, lockDigest func(string) string) *ArchiveStore {
	return &ArchiveStore{backend: backend, tempRoot: tempRoot, lockDigest: lockDigest}
}

func (s *ArchiveStore) Head(ctx context.Context, id string) (Manifest, bool, error) {
	keys, err := s.backend.List(ctx, id+"/saves/")
	if err != nil {
		return Manifest{}, false, err
	}
	manifests := make([]string, 0)
	for _, k := range keys {
		if strings.HasSuffix(k, ".json") {
			manifests = append(manifests, k)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(manifests)))
	for _, key := range manifests {
		r, e := s.backend.Open(ctx, key)
		if e != nil {
			continue
		}
		var m Manifest
		e = json.NewDecoder(r).Decode(&m)
		_ = r.Close()
		if e == nil && m.ID == id && m.Generation > 0 {
			archiveKey := fmt.Sprintf("%s/saves/%020d.tar.zst", id, m.Generation)
			archive, openErr := s.backend.Open(ctx, archiveKey)
			if openErr != nil {
				continue
			}
			h := sha256.New()
			n, copyErr := io.Copy(h, archive)
			closeErr := archive.Close()
			if copyErr == nil && closeErr == nil && n == m.Size && hex.EncodeToString(h.Sum(nil)) == m.SHA256 {
				return m, true, nil
			}
		}
	}
	return Manifest{}, false, nil
}

func (s *ArchiveStore) Commit(ctx context.Context, id, source string, expected uint64) (Manifest, error) {
	head, ok, err := s.Head(ctx, id)
	if err != nil {
		return Manifest{}, err
	}
	if (ok && head.Generation != expected) || (!ok && expected != 0) {
		return Manifest{}, ErrGenerationConflict
	}
	if err := os.MkdirAll(s.tempRoot, 0o700); err != nil {
		return Manifest{}, err
	}
	f, err := os.CreateTemp(s.tempRoot, "hostpack-*.tar.zst")
	if err != nil {
		return Manifest{}, err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	h := sha256.New()
	count := &countWriter{}
	zw, err := zstd.NewWriter(io.MultiWriter(f, h, count))
	if err != nil {
		f.Close()
		return Manifest{}, err
	}
	tw := tar.NewWriter(zw)
	err = filepath.WalkDir(source, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, e := filepath.Rel(source, path)
		if e != nil {
			return e
		}
		if rel == "." {
			return nil
		}
		if strings.HasPrefix(rel, "logs"+string(filepath.Separator)) {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		hdr, e := tar.FileInfoHeader(info, "")
		if e != nil {
			return e
		}
		hdr.Name = filepath.ToSlash(rel)
		if e = tw.WriteHeader(hdr); e != nil {
			return e
		}
		if info.Mode().IsRegular() {
			rf, e := os.Open(path)
			if e != nil {
				return e
			}
			_, e = io.Copy(tw, rf)
			ce := rf.Close()
			if e != nil {
				return e
			}
			return ce
		}
		return nil
	})
	if closeErr := tw.Close(); err == nil {
		err = closeErr
	}
	if closeErr := zw.Close(); err == nil {
		err = closeErr
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return Manifest{}, err
	}
	gen := expected + 1
	m := Manifest{ID: id, Generation: gen, SHA256: hex.EncodeToString(h.Sum(nil)), Size: count.n, CreatedAt: time.Now().UTC()}
	if s.lockDigest != nil {
		m.PackLockDigest = s.lockDigest(id)
	}
	af, err := os.Open(tmp)
	if err != nil {
		return Manifest{}, err
	}
	archiveKey := fmt.Sprintf("%s/saves/%020d.tar.zst", id, gen)
	err = s.backend.Put(ctx, archiveKey, af, count.n)
	_ = af.Close()
	if err != nil {
		return Manifest{}, err
	}
	mb, _ := json.MarshalIndent(m, "", "  ")
	manifestKey := fmt.Sprintf("%s/saves/%020d.json", id, gen)
	if err = s.backend.Put(ctx, manifestKey, strings.NewReader(string(mb)), int64(len(mb))); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func (s *ArchiveStore) Restore(ctx context.Context, id, destination string) (Manifest, error) {
	m, ok, err := s.Head(ctx, id)
	if err != nil {
		return Manifest{}, err
	}
	if !ok {
		return Manifest{}, os.ErrNotExist
	}
	key := fmt.Sprintf("%s/saves/%020d.tar.zst", id, m.Generation)
	r, err := s.backend.Open(ctx, key)
	if err != nil {
		return Manifest{}, err
	}
	defer r.Close()
	parent := filepath.Dir(destination)
	if err = os.MkdirAll(parent, 0o750); err != nil {
		return Manifest{}, err
	}
	tmp, err := os.MkdirTemp(parent, ".restore-")
	if err != nil {
		return Manifest{}, err
	}
	defer os.RemoveAll(tmp)
	h := sha256.New()
	zr, err := zstd.NewReader(io.TeeReader(r, h))
	if err != nil {
		return Manifest{}, err
	}
	tr := tar.NewReader(zr)
	for {
		hdr, e := tr.Next()
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			zr.Close()
			return Manifest{}, e
		}
		clean := filepath.Clean(hdr.Name)
		if clean == "." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			zr.Close()
			return Manifest{}, fmt.Errorf("unsafe archive path %q", hdr.Name)
		}
		target := filepath.Join(tmp, clean)
		if !strings.HasPrefix(target, tmp+string(filepath.Separator)) {
			zr.Close()
			return Manifest{}, fmt.Errorf("archive path escaped destination")
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			e = os.MkdirAll(target, fs.FileMode(hdr.Mode))
		case tar.TypeReg:
			if e = os.MkdirAll(filepath.Dir(target), 0o750); e == nil {
				var wf *os.File
				wf, e = os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, fs.FileMode(hdr.Mode))
				if e == nil {
					_, e = io.Copy(wf, tr)
					ce := wf.Close()
					if e == nil {
						e = ce
					}
				}
			}
		case tar.TypeSymlink:
			e = fmt.Errorf("symlinks are not accepted in backups")
		default:
			e = fmt.Errorf("unsupported archive entry %q", hdr.Name)
		}
		if e != nil {
			zr.Close()
			return Manifest{}, e
		}
	}
	zr.Close()
	if got := hex.EncodeToString(h.Sum(nil)); got != m.SHA256 {
		return Manifest{}, fmt.Errorf("backup checksum mismatch: got %s", got)
	}
	if _, err = os.Stat(destination); err == nil {
		return Manifest{}, fmt.Errorf("restore destination already exists")
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Manifest{}, err
	}
	if err = os.Rename(tmp, destination); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

type countWriter struct{ n int64 }

func (c *countWriter) Write(p []byte) (int, error) { c.n += int64(len(p)); return len(p), nil }

func FromConfig(c config.StorageConfig, tempRoot string, digest func(string) string) (Store, error) {
	switch c.Driver {
	case "filesystem":
		return New(NewFilesystem(c.Filesystem.Root), tempRoot, digest), nil
	case "s3":
		b, err := NewS3(c.S3)
		if err != nil {
			return nil, err
		}
		return New(b, tempRoot, digest), nil
	case "rclone":
		return New(NewRclone(c.Rclone.Remote), tempRoot, digest), nil
	default:
		return nil, fmt.Errorf("unsupported store %q", c.Driver)
	}
}
