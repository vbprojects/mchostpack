package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

type LockFile struct {
	Schema       int                   `json:"schema"`
	ConfigDigest string                `json:"configDigest"`
	Packs        map[string]LockedPack `json:"packs"`
}

type LockedPack struct {
	Provider       string `json:"provider"`
	ProjectID      string `json:"projectId"`
	VersionID      string `json:"versionId,omitempty"`
	FileID         int64  `json:"fileId,omitempty"`
	Slug           string `json:"slug,omitempty"`
	Minecraft      string `json:"minecraftVersion,omitempty"`
	Loader         string `json:"loader,omitempty"`
	Java           int    `json:"java"`
	ArtifactHash   string `json:"artifactHash"`
	IdentityDigest string `json:"identityDigest"`
}

func LoadLock(path string) (*LockFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var l LockFile
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, fmt.Errorf("decode lock: %w", err)
	}
	return &l, l.Validate()
}

func (l *LockFile) Validate() error {
	if l.Schema != 1 {
		return fmt.Errorf("lock schema must be 1")
	}
	if len(l.ConfigDigest) != 64 {
		return fmt.Errorf("invalid config digest")
	}
	for id, p := range l.Packs {
		if p.IdentityDigest == "" || p.ProjectID == "" || (p.Provider != "modrinth" && p.Provider != "curseforge") {
			return fmt.Errorf("invalid locked pack %q", id)
		}
		expected := packIdentityDigest(Pack{Provider: p.Provider, ProjectID: p.ProjectID, VersionID: p.VersionID, FileID: p.FileID, Java: p.Java})
		if p.IdentityDigest != expected {
			return fmt.Errorf("locked pack %q identity digest is invalid", id)
		}
	}
	return nil
}

func CheckImmutable(previous, next *LockFile) error {
	for id, old := range previous.Packs {
		current, ok := next.Packs[id]
		if !ok {
			return fmt.Errorf("locked pack %q cannot be removed", id)
		}
		if old.IdentityDigest != current.IdentityDigest {
			return fmt.Errorf("locked pack %q immutable identity changed; add a new ID instead", id)
		}
	}
	return nil
}

func (l *LockFile) Matches(c *Config) error {
	if l.ConfigDigest != c.IdentityDigest() {
		return fmt.Errorf("packs.lock.json does not match immutable fields in packs.yaml; run hostpackd lock")
	}
	if len(l.Packs) != len(c.Packs) {
		return fmt.Errorf("locked pack count differs from config")
	}
	for id, p := range c.Packs {
		lp, ok := l.Packs[id]
		if !ok {
			return fmt.Errorf("pack %q is missing from lock", id)
		}
		if lp.Provider != p.Provider || lp.ProjectID != p.ProjectID || lp.VersionID != p.VersionID || lp.FileID != p.FileID || lp.Java != p.Java {
			return fmt.Errorf("pack %q immutable fields differ from lock", id)
		}
	}
	return nil
}

func (l *LockFile) Marshal() ([]byte, error) { return json.MarshalIndent(l, "", "  ") }

type Resolver struct {
	Client                                      *http.Client
	ModrinthBase, CurseForgeBase, CurseForgeKey string
}

func (r *Resolver) Generate(ctx context.Context, c *Config) (*LockFile, error) {
	if r.Client == nil {
		r.Client = &http.Client{Timeout: 30 * time.Second}
	}
	if r.ModrinthBase == "" {
		r.ModrinthBase = "https://api.modrinth.com/v2"
	}
	if r.CurseForgeBase == "" {
		r.CurseForgeBase = "https://api.curseforge.com/v1"
	}
	result := &LockFile{Schema: 1, ConfigDigest: c.IdentityDigest(), Packs: map[string]LockedPack{}}
	ids := make([]string, 0, len(c.Packs))
	for id := range c.Packs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		p := c.Packs[id]
		var lp LockedPack
		var err error
		if p.Provider == "modrinth" {
			lp, err = r.resolveModrinth(ctx, p)
		} else {
			lp, err = r.resolveCurseForge(ctx, p)
		}
		if err != nil {
			return nil, fmt.Errorf("resolve pack %q: %w", id, err)
		}
		lp.Java = p.Java
		lp.IdentityDigest = packIdentityDigest(p)
		result.Packs[id] = lp
	}
	return result, nil
}

func packIdentityDigest(p Pack) string {
	b, _ := json.Marshal(identityPack{p.Provider, p.ProjectID, p.VersionID, p.FileID, p.Java})
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func (r *Resolver) get(ctx context.Context, url string, target any, key bool) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "hostpackd/1")
	if key {
		if r.CurseForgeKey == "" {
			return fmt.Errorf("CF_API_KEY is required")
		}
		req.Header.Set("x-api-key", r.CurseForgeKey)
	}
	resp, err := r.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("provider returned %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func (r *Resolver) resolveModrinth(ctx context.Context, p Pack) (LockedPack, error) {
	var v struct {
		ID, ProjectID         string
		GameVersions, Loaders []string
		Files                 []struct {
			Primary bool
			Hashes  map[string]string
		}
	}
	if err := r.get(ctx, r.ModrinthBase+"/version/"+p.VersionID, &v, false); err != nil {
		return LockedPack{}, err
	}
	if v.ProjectID != p.ProjectID {
		return LockedPack{}, fmt.Errorf("version belongs to project %s, not %s", v.ProjectID, p.ProjectID)
	}
	hash := ""
	for _, f := range v.Files {
		if f.Primary || hash == "" {
			if h := f.Hashes["sha512"]; h != "" {
				hash = "sha512:" + h
			} else if h := f.Hashes["sha1"]; h != "" {
				hash = "sha1:" + h
			}
		}
		if f.Primary {
			break
		}
	}
	if hash == "" {
		return LockedPack{}, fmt.Errorf("version has no usable file hash")
	}
	mc := ""
	if len(v.GameVersions) > 0 {
		mc = v.GameVersions[0]
	}
	loader := ""
	if len(v.Loaders) > 0 {
		loader = v.Loaders[0]
	}
	return LockedPack{Provider: "modrinth", ProjectID: p.ProjectID, VersionID: p.VersionID, Minecraft: mc, Loader: loader, ArtifactHash: hash}, nil
}

func (r *Resolver) resolveCurseForge(ctx context.Context, p Pack) (LockedPack, error) {
	var project struct {
		Data struct {
			ID   int64
			Slug string
		}
	}
	if err := r.get(ctx, r.CurseForgeBase+"/mods/"+p.ProjectID, &project, true); err != nil {
		return LockedPack{}, err
	}
	if strconv.FormatInt(project.Data.ID, 10) != p.ProjectID {
		return LockedPack{}, fmt.Errorf("project id mismatch")
	}
	var file struct {
		Data struct {
			ID, ModID int64
			Hashes    []struct {
				Value string
				Algo  int
			}
			GameVersions []string
		}
	}
	if err := r.get(ctx, r.CurseForgeBase+"/mods/"+p.ProjectID+"/files/"+strconv.FormatInt(p.FileID, 10), &file, true); err != nil {
		return LockedPack{}, err
	}
	if file.Data.ModID != project.Data.ID || file.Data.ID != p.FileID {
		return LockedPack{}, fmt.Errorf("file does not belong to configured project")
	}
	hash := ""
	for _, h := range file.Data.Hashes {
		algo := ""
		if h.Algo == 1 {
			algo = "sha1"
		} else if h.Algo == 2 {
			algo = "md5"
		}
		if algo != "" {
			hash = algo + ":" + strings.ToLower(h.Value)
			if algo == "sha1" {
				break
			}
		}
	}
	if hash == "" {
		return LockedPack{}, fmt.Errorf("file has no usable hash")
	}
	mc := ""
	loader := ""
	for _, v := range file.Data.GameVersions {
		if strings.Count(v, ".") >= 1 && mc == "" {
			mc = v
		}
		lv := strings.ToLower(v)
		if lv == "forge" || lv == "fabric" || lv == "neoforge" || lv == "quilt" {
			loader = lv
		}
	}
	return LockedPack{Provider: "curseforge", ProjectID: p.ProjectID, FileID: p.FileID, Slug: project.Data.Slug, Minecraft: mc, Loader: loader, ArtifactHash: hash}, nil
}
