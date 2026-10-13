// Package plugins reads versioned declarative content packs. It executes no plugin code.
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
	"sync"
	"tableTop/main/internal/app"

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
	reader *reader
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
	for _, dir := range dirs {
		packs, hasBuiltin := discoverRoot(dir, bounds)
		candidates = append(candidates, packs...)
		builtinOnDisk = builtinOnDisk || hasBuiltin
	}
	if !builtinOnDisk {
		candidates = append(candidates, discoverBundled(bounds)...)
	}
	result := loadUniquePacks(candidates)
	sort.Slice(result, func(i, j int) bool { return packLess(result[i], result[j]) })
	return result
}

func discoverRoot(dir string, bounds Bounds) ([]Pack, bool) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false
	}
	if err != nil {
		log.Printf("Read plugin directory %s: %v", dir, err)
		return nil, false
	}
	packs := []Pack{}
	builtin := false
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		versions, hasBuiltin := discoverVersions(dir, entry.Name(), bounds)
		packs = append(packs, versions...)
		builtin = builtin || hasBuiltin
	}
	return packs, builtin
}
func discoverVersions(dir, id string, bounds Bounds) ([]Pack, bool) {
	entries, err := os.ReadDir(filepath.Join(dir, id))
	if err != nil {
		log.Printf("Read plugin versions: %v", err)
		return nil, false
	}
	packs := []Pack{}
	builtin := false
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		builtin = builtin || (id == "builtin" && entry.Name() == "1")
		root := filepath.Join(dir, id, entry.Name())
		pack, err := openVersion(root, id, entry.Name(), bounds)
		if err != nil {
			log.Printf("Invalid plugin %s: %v", root, err)
			continue
		}
		packs = append(packs, pack)
	}
	return packs, builtin
}
func openVersion(root, id, version string, bounds Bounds) (Pack, error) {
	pack, err := openPack(os.DirFS(root), root, bounds)
	if err != nil {
		return pack, err
	}
	if pack.ID != id || strconv.Itoa(pack.Version) != version {
		return pack, errors.New("plugin identity does not match folder")
	}
	return pack, nil
}
func discoverBundled(bounds Bounds) []Pack {
	files, err := fs.Sub(bundled.Builtin, "builtin/1")
	if err != nil {
		log.Printf("Invalid bundled plugin: %v", err)
		return nil
	}
	pack, err := openPack(files, "", bounds)
	if err != nil {
		log.Printf("Invalid bundled plugin: %v", err)
		return nil
	}
	return []Pack{pack}
}

type packCacheKey struct {
	root   string
	bounds Bounds
}

var validatedPacks = struct {
	sync.Mutex
	packs map[packCacheKey]Pack
}{packs: make(map[packCacheKey]Pack)}

func loadCachedPack(p *Pack) error {
	key := packCacheKey{p.root, p.bounds}
	validatedPacks.Lock()
	defer validatedPacks.Unlock()
	if cached, ok := validatedPacks.packs[key]; ok && cached.Manifest == p.Manifest {
		if err := cached.Validate(); err == nil {
			p.reader = cached.reader
			return nil
		}
	}
	if err := p.load(); err != nil {
		delete(validatedPacks.packs, key)
		return err
	}
	// Bound memory when administrators replace or install many versions.
	if len(validatedPacks.packs) >= 32 {
		for keyToEvict := range validatedPacks.packs {
			delete(validatedPacks.packs, keyToEvict)
			break
		}
	}
	validatedPacks.packs[key] = *p
	return nil
}

func loadUniquePacks(candidates []Pack) []Pack {
	counts := map[string]int{}
	for _, p := range candidates {
		counts[p.Selection()]++
	}
	result := []Pack{}
	for _, p := range candidates {
		if counts[p.Selection()] != 1 {
			log.Printf("Duplicate plugin version %s is unavailable", p.Selection())
			continue
		}
		if err := loadCachedPack(&p); err != nil {
			log.Printf("Invalid plugin %s: %v", p.Selection(), err)
			continue
		}
		result = append(result, p)
	}
	return result
}
func packLess(a, b Pack) bool {
	if (a.ID == "builtin") != (b.ID == "builtin") {
		return a.ID == "builtin"
	}
	if a.ID == b.ID {
		return a.Version < b.Version
	}
	return a.Name < b.Name
}
func (p Pack) Selection() string { return p.ID + "/" + strconv.Itoa(p.Version) }
func Resolve(dirs []string, bounds Bounds, binding app.PluginBinding) (*Pack, error) {
	for _, p := range Discover(dirs, bounds) {
		if p.ID != binding.PluginID || p.Version != binding.Version {
			continue
		}
		if p.Binding() != binding {
			return nil, errors.New("plugin version has changed; restore its original files")
		}
		return &p, nil
	}
	return nil, errors.New("required plugin version is unavailable; restore its folder")
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
	if p.SchemaVersion != 2 || !slug.MatchString(p.ID) || len(p.ID) > 80 || p.Version < 1 || strings.TrimSpace(p.Name) == "" || len(p.Name) > 320 || len(p.Description) > 4096 {
		return p, errors.New("invalid plugin metadata")
	}
	for _, path := range []string{p.Manifest.Maps, p.Manifest.Statuses, p.Manifest.Units} {
		if !fs.ValidPath(path) || path == "." {
			return p, errors.New("invalid content path")
		}
	}
	return p, nil
}
func (p Pack) validatePath(path string) error {
	if !fs.ValidPath(path) || path == "." {
		return errors.New("plugin path must be relative and contained")
	}
	if p.root == "" {
		return nil
	}
	root, err := filepath.EvalSymlinks(p.root)
	if err != nil {
		return err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	target, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("plugin symlink escapes directory")
	}
	return nil
}
func (p Pack) open(path string, limit int64) (*boundedFile, error) {
	if err := p.validatePath(path); err != nil {
		return nil, err
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
	return &boundedFile{File: f, remaining: limit + 1}, nil
}

type boundedFile struct {
	fs.File
	remaining int64
}

func (f *boundedFile) Read(b []byte) (int, error) {
	if f.remaining <= 0 {
		return 0, errors.New("plugin file exceeds size limit")
	}
	if int64(len(b)) > f.remaining {
		b = b[:f.remaining]
	}
	n, err := f.File.Read(b)
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
