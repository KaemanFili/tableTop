package plugins

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"

	"tableTop/main/internal/app"
	"tableTop/main/internal/artwork"
)

type mapEntry struct {
	Key       string `json:"key"`
	Image     string `json:"image"`
	BoardFile string `json:"boardFile,omitempty"`
	app.MapDefinition
}
type unitEntry struct {
	Key   string `json:"key"`
	Image string `json:"image"`
	app.UnitDefinition
}
type asset struct {
	path  string
	limit int64
}
type reader struct {
	mu          sync.Mutex
	maps        map[string]mapEntry
	units       map[string]app.UnitDefinition
	byKey       map[string]app.UnitDefinition
	images      map[string]asset
	statuses    []app.UnitStatusOption
	statusNames map[app.UnitStatus]bool
	files       map[string]int64
	stamps      map[string]string
	hashes      map[string][32]byte
	fingerprint string
}

func StableID(plugin, kind, key string) string {
	sum := sha256.Sum256([]byte(plugin + "\x00" + kind + "\x00" + key))
	return hex.EncodeToString(sum[:16])
}
func (p Pack) Binding() app.PluginBinding {
	return app.PluginBinding{PluginID: p.ID, Version: p.Version, Fingerprint: p.reader.fingerprint}
}
func (p *Pack) load() error {
	p.reader = newReader(p.Manifest, p.bounds)
	if err := p.readStatuses(); err != nil {
		return err
	}
	if err := p.readUnits(); err != nil {
		return err
	}
	if err := p.readMaps(); err != nil {
		return err
	}
	return p.captureFingerprint()
}
func newReader(manifest Manifest, bounds Bounds) *reader {
	return &reader{
		maps: map[string]mapEntry{}, units: map[string]app.UnitDefinition{}, byKey: map[string]app.UnitDefinition{}, images: map[string]asset{},
		statusNames: map[app.UnitStatus]bool{}, files: map[string]int64{"plugin.json": bounds.Metadata, manifest.Maps: bounds.Content, manifest.Units: bounds.Content, manifest.Statuses: bounds.Content},
		stamps: map[string]string{}, hashes: map[string][32]byte{},
	}
}
func (p Pack) captureFingerprint() error {
	paths := make([]string, 0, len(p.reader.files))
	for path := range p.reader.files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	hash := sha256.New()
	for _, path := range paths {
		data, err := p.read(path, p.reader.files[path])
		if err != nil {
			return err
		}
		fmt.Fprintf(hash, "%d:%s:%d:", len(path), path, len(data))
		hash.Write(data)
		p.reader.hashes[path] = sha256.Sum256(data)
		stamp, err := p.stamp(path)
		if err != nil {
			return err
		}
		p.reader.stamps[path] = stamp
	}
	p.reader.fingerprint = hex.EncodeToString(hash.Sum(nil))
	return nil
}
func (p Pack) stamp(path string) (string, error) {
	f, err := p.open(path, p.reader.files[path])
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d/%d/%s", info.Size(), info.ModTime().UnixNano(), info.Mode()), nil
}

// Validate checks file identity without reading other maps' saved boards.
// Installed folders are immutable; changed files require restoring the version.
func (p Pack) Validate() error {
	p.reader.mu.Lock()
	defer p.reader.mu.Unlock()
	for path, expected := range p.reader.stamps {
		if err := p.validateFile(path, expected); err != nil {
			return err
		}
	}
	return nil
}
func (p Pack) validateFile(path, expected string) error {
	stamp, err := p.stamp(path)
	if err != nil {
		return fmt.Errorf("plugin %s unavailable: %w", p.Selection(), err)
	}
	if stamp == expected {
		return nil
	}
	data, err := p.read(path, p.reader.files[path])
	if err != nil {
		return err
	}
	if sha256.Sum256(data) != p.reader.hashes[path] {
		return errors.New("plugin version changed; restore its original folder")
	}
	p.reader.stamps[path] = stamp
	return nil
}
func (p Pack) source(key string) *app.ContentSource {
	return &app.ContentSource{PluginID: p.ID, Version: p.Version, Key: key}
}
func (p Pack) reference(kind, id string) *app.ArtworkReference {
	return &app.ArtworkReference{PluginID: p.ID, Version: p.Version, Kind: kind, ImageID: id}
}
func (p Pack) readStatuses() error {
	if err := p.list(p.Manifest.Statuses, func(raw json.RawMessage) error {
		var value app.UnitStatusOption
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		p.reader.statuses = append(p.reader.statuses, value)
		p.reader.statusNames[value.Value] = true
		if len(p.reader.statuses) > 256 {
			return errors.New("too many statuses")
		}
		return nil
	}); err != nil {
		return err
	}
	return app.ValidateUnitStatuses(p.reader.statuses)
}
func (p Pack) readUnits() error {
	return p.list(p.Manifest.Units, func(raw json.RawMessage) error {
		var entry unitEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			return err
		}
		if !slug.MatchString(entry.Key) || p.reader.byKey[entry.Key].ID != "" {
			return errors.New("invalid or duplicate unit key")
		}
		unit, err := p.defaultUnit(entry)
		if err != nil {
			return err
		}
		p.reader.byKey[entry.Key] = unit
		p.reader.units[unit.ID] = unit
		return nil
	})
}
func (p Pack) defaultUnit(entry unitEntry) (app.UnitDefinition, error) {
	unit := entry.UnitDefinition
	unit.ID = StableID(p.ID, "unit", entry.Key)
	unit.Source = p.source(entry.Key)
	unit.ReadOnly = true
	if unit.Size == "" {
		unit.Size = app.SizeMedium
	}
	if unit.ImageFit == (app.ImageFit{}) {
		unit.ImageFit = app.DefaultImageFit()
	}
	if err := app.ValidateUnitDefinition(unit.Name, unit.UnitType, unit.Size); err != nil {
		return unit, err
	}
	if err := unit.ImageFit.Validate(); err != nil {
		return unit, err
	}
	unit.ImageID = app.NPCUnitImageID
	if unit.UnitType == "pc" {
		unit.ImageID = app.PCUnitImageID
	}
	unit.Artwork = nil
	if entry.Image == "" {
		return unit, nil
	}
	unit.ImageID = unit.ID
	unit.Artwork = p.reference("unit", unit.ID)
	return unit, p.addImage("unit", unit.ID, entry.Image, 5<<20)
}
func (p Pack) addImage(kind, id, path string, limit int64) error {
	p.reader.files[path] = limit
	data, err := p.read(path, limit)
	if err != nil {
		return err
	}
	if err := artwork.Validate(data); err != nil {
		return err
	}
	p.reader.images[kind+"/"+id] = asset{path, limit}
	return nil
}
func (p Pack) readMaps() error {
	return p.list(p.Manifest.Maps, func(raw json.RawMessage) error {
		var entry mapEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			return err
		}
		id := StableID(p.ID, "map", entry.Key)
		if !slug.MatchString(entry.Key) || p.reader.maps[id].Key != "" {
			return errors.New("invalid or duplicate map key")
		}
		normalized, err := p.defaultMap(entry, id)
		if err != nil {
			return err
		}
		p.reader.maps[id] = normalized
		return nil
	})
}
func (p Pack) defaultMap(entry mapEntry, id string) (mapEntry, error) {
	if entry.Board != nil {
		return entry, errors.New("schema 2 maps use boardFile, not embedded board")
	}
	entry.ID = id
	entry.Source = p.source(entry.Key)
	entry.ReadOnly = true
	entry.HasSavedBoard = entry.BoardFile != ""
	entry.Artwork = p.reference("map", id)
	if err := entry.Validate(); err != nil {
		return entry, err
	}
	if err := p.addImage("map", id, entry.Image, 10<<20); err != nil {
		return entry, err
	}
	return entry, p.validateBoardFile(entry.BoardFile)
}
func (p Pack) validateBoardFile(path string) error {
	if path == "" {
		return nil
	}
	p.reader.files[path] = p.bounds.Content
	_, err := p.board(path)
	return err
}
func (p Pack) board(path string) (*app.SavedBoard, error) {
	data, err := p.read(path, p.bounds.Content)
	if err != nil {
		return nil, err
	}
	var b app.SavedBoard
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, err
	}
	for i := range b.Units {
		if err := p.resolveUnit(&b.Units[i]); err != nil {
			return nil, err
		}
	}
	return &b, b.Validate()
}
func (p Pack) resolveUnit(unit *app.UnitData) error {
	id, reference, err := p.unitImage(unit.ImageID)
	if err != nil {
		return err
	}
	unit.ImageID, unit.Artwork = id, reference
	if unit.Status != "" && !p.reader.statusNames[unit.Status] {
		return errors.New("unknown saved token status")
	}
	if unit.Size == "" {
		unit.Size = app.SizeMedium
	}
	if unit.ImageFit == (app.ImageFit{}) {
		unit.ImageFit = app.DefaultImageFit()
	}
	return nil
}
func (p Pack) unitImage(id string) (string, *app.ArtworkReference, error) {
	if unit, exists := p.reader.byKey[id]; exists {
		return unit.ImageID, unit.Artwork, nil
	}
	switch id {
	case app.PCUnitImageID, app.NPCUnitImageID, app.DefaultUnitImageID:
		return id, nil, nil
	}
	if unit, exists := p.reader.units[id]; exists && unit.ImageID == id {
		return id, unit.Artwork, nil
	}
	return "", nil, errors.New("unknown saved token image")
}
func (p Pack) Maps() []app.MapDefinition {
	result := []app.MapDefinition{}
	for _, e := range p.reader.maps {
		result = append(result, e.MapDefinition)
	}
	return result
}
func (p Pack) Units() []app.UnitDefinition {
	result := []app.UnitDefinition{}
	for _, u := range p.reader.units {
		result = append(result, u)
	}
	return result
}
func (p Pack) Statuses() []app.UnitStatusOption {
	return append([]app.UnitStatusOption(nil), p.reader.statuses...)
}
func (p Pack) LoadMap(id string) (app.MapDefinition, error) {
	if err := p.Validate(); err != nil {
		return app.MapDefinition{}, err
	}
	e, ok := p.reader.maps[id]
	if !ok {
		return app.MapDefinition{}, os.ErrNotExist
	}
	m := e.MapDefinition
	if e.BoardFile != "" {
		var err error
		m.Board, err = p.board(e.BoardFile)
		if err != nil {
			return app.MapDefinition{}, err
		}
	}
	return m, nil
}
func (p Pack) Image(ref app.ArtworkReference) ([]byte, error) {
	if ref.PluginID != p.ID || ref.Version != p.Version {
		return nil, errors.New("artwork belongs to another plugin version")
	}
	a, ok := p.reader.images[ref.Kind+"/"+ref.ImageID]
	if !ok {
		return nil, os.ErrNotExist
	}
	if err := p.ValidateMetadata(); err != nil {
		return nil, err
	}
	data, err := p.read(a.path, a.limit)
	if err != nil {
		return nil, err
	}
	// Every image was decoded at load time. Check this exact file's identity
	// without statting thousands of unrelated portraits for each thumbnail.
	if sha256.Sum256(data) != p.reader.hashes[a.path] {
		return nil, errors.New("plugin version changed; restore its original folder")
	}
	if bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
		return data, nil
	}
	return artwork.Normalize(data)
}

// ValidateMetadata checks the binding's content indexes. Accessed artwork and
// saved boards verify their own bytes; full Validate remains available on entry.
func (p Pack) ValidateMetadata() error {
	p.reader.mu.Lock()
	defer p.reader.mu.Unlock()
	for _, path := range []string{"plugin.json", p.Manifest.Maps, p.Manifest.Units, p.Manifest.Statuses} {
		if expected, exists := p.reader.stamps[path]; exists {
			if err := p.validateFile(path, expected); err != nil {
				return err
			}
		}
	}
	return nil
}

// UnitByID uses the validated default index without constructing a listing.
func (p Pack) UnitByID(id string) (app.UnitDefinition, bool) {
	unit, exists := p.reader.units[id]
	return unit, exists
}

func (p Pack) MetadataByID(id string) (app.MapDefinition, bool) {
	entry, exists := p.reader.maps[id]
	return entry.MapDefinition, exists
}
