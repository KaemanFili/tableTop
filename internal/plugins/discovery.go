// Package plugins discovers and imports declarative content packs. It executes no plugin code.
package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	bundled "tableTop/main/plugins"
)

type Manifest struct {
	SchemaVersion int    `json:"schemaVersion"`
	ID            string `json:"id"`
	Version       int    `json:"version"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Maps          string `json:"maps"`
	Statuses      string `json:"statuses"`
	Units         string `json:"units"`
}
type Pack struct {
	Manifest
	files  fs.FS
	root   string
	bounds Bounds
}
type Bounds struct{ Metadata, Content int64 }

func BoundsFromEnvironment() (Bounds, error) {
	b := Bounds{1 << 20, 16 << 20}
	for key, target := range map[string]*int64{"TABLETOP_PLUGIN_METADATA_LIMIT_BYTES": &b.Metadata, "TABLETOP_PLUGIN_CONTENT_LIMIT_BYTES": &b.Content} {
		if raw := os.Getenv(key); raw != "" {
			n, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || n < 1 || n == int64(^uint64(0)>>1) {
				return b, fmt.Errorf("invalid %s", key)
			}
			*target = n
		}
	}
	return b, nil
}
func Directories(assetRoot string) []string {
	dirs := []string{filepath.Join(assetRoot, "plugins")}
	for _, dir := range filepath.SplitList(os.Getenv("TABLETOP_PLUGIN_DIRS")) {
		if dir != "" {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

var slug = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func Discover(dirs []string, bounds Bounds) []Pack {
	candidates := []Pack{}
	builtinOnDisk := false
	for index, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			log.Printf("Read plugin directory %s: %v", dir, err)
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			root := filepath.Join(dir, entry.Name())
			if index == 0 && entry.Name() == "builtin" {
				builtinOnDisk = true
			}
			p, err := openPack(os.DirFS(root), root, bounds)
			if err != nil {
				log.Printf("Invalid plugin %s: %v", root, err)
				continue
			}
			candidates = append(candidates, p)
		}
	}
	if !builtinOnDisk {
		files, _ := fs.Sub(bundled.Builtin, "builtin")
		p, err := openPack(files, "", bounds)
		if err == nil {
			candidates = append(candidates, p)
		} else {
			log.Printf("Invalid built-in plugin: %v", err)
		}
	}
	counts := map[string]int{}
	for _, p := range candidates {
		counts[p.ID]++
	}
	result := []Pack{}
	for _, p := range candidates {
		if counts[p.ID] != 1 {
			log.Printf("Duplicate plugin ID %s is unavailable", p.ID)
			continue
		}
		if err := p.validate(); err != nil {
			log.Printf("Invalid plugin %s: %v", p.ID, err)
			continue
		}
		result = append(result, p)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ID == "builtin" {
			return true
		}
		if result[j].ID == "builtin" {
			return false
		}
		return result[i].Name < result[j].Name
	})
	return result
}
func openPack(files fs.FS, root string, bounds Bounds) (Pack, error) {
	p := Pack{files: files, root: root, bounds: bounds}
	data, err := p.read("plugin.json", bounds.Metadata)
	if err != nil {
		return p, err
	}
	if err = json.Unmarshal(data, &p.Manifest); err != nil {
		return p, err
	}
	if p.SchemaVersion != 1 || !slug.MatchString(p.ID) || len(p.ID) > 80 || p.Version < 1 || strings.TrimSpace(p.Name) == "" || len(p.Name) > 320 || len(p.Description) > 4096 {
		return p, errors.New("invalid plugin metadata")
	}
	for _, path := range []string{p.Maps, p.Statuses, p.Units} {
		if !fs.ValidPath(path) || path == "." {
			return p, errors.New("invalid content path")
		}
	}
	return p, nil
}
func (p Pack) open(path string, limit int64) (io.ReadCloser, error) {
	if !fs.ValidPath(path) || path == "." {
		return nil, errors.New("plugin path must be relative and contained")
	}
	if p.root != "" {
		root, err := filepath.EvalSymlinks(p.root)
		if err != nil {
			return nil, err
		}
		root, err = filepath.Abs(root)
		if err != nil {
			return nil, err
		}
		target, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			return nil, err
		}
		relative, err := filepath.Rel(root, target)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, errors.New("plugin symlink escapes directory")
		}
	}
	f, err := p.files.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		f.Close()
		return nil, errors.New("plugin file exceeds size limit or is not regular")
	}
	return &boundedFile{ReadCloser: f, remaining: limit + 1}, nil
}

type boundedFile struct {
	io.ReadCloser
	remaining int64
}

func (f *boundedFile) Read(b []byte) (int, error) {
	if f.remaining <= 0 {
		return 0, errors.New("plugin file exceeds size limit")
	}
	if int64(len(b)) > f.remaining {
		b = b[:f.remaining]
	}
	n, err := f.ReadCloser.Read(b)
	f.remaining -= int64(n)
	return n, err
}
func (p Pack) read(path string, limit int64) ([]byte, error) {
	f, err := p.open(path, limit)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if int64(len(data)) > limit {
		return nil, errors.New("plugin file exceeds size limit")
	}
	return data, err
}
func (p Pack) list(path string, yield func(json.RawMessage) error) error {
	f, err := p.open(path, p.bounds.Content)
	if err != nil {
		return err
	}
	defer f.Close()
	d := json.NewDecoder(f)
	token, err := d.Token()
	if err != nil {
		return err
	}
	if token != json.Delim('[') {
		return errors.New("content must be a JSON array")
	}
	for d.More() {
		var raw json.RawMessage
		if err := d.Decode(&raw); err != nil {
			return err
		}
		if err := yield(raw); err != nil {
			return err
		}
	}
	if _, err := d.Token(); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return errors.New("unexpected trailing content")
	}
	return nil
}
