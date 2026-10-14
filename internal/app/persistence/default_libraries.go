package persistence

import (
	"errors"
	"os"
	"path/filepath"

	"tableTop/main/internal/app/model"
)

func (l *MapLibrary) SetDefaults(defaults PluginDefaults)  { l.defaults = defaults }
func (l *UnitLibrary) SetDefaults(defaults PluginDefaults) { l.defaults = defaults }
func (l *MapLibrary) ValidateDefaults() error {
	if l.defaults != nil {
		return l.defaults.Validate()
	}
	return nil
}
func (l *UnitLibrary) ValidateDefaults() error {
	if l.defaults != nil {
		return l.defaults.Validate()
	}
	return nil
}

func (l *MapLibrary) Artwork(id string) (string, []byte, error) {
	m, ok := l.MetadataByID(id)
	if !ok {
		return "", nil, os.ErrNotExist
	}
	return l.mapArtwork(m)
}
func (l *MapLibrary) mapArtwork(m model.MapDefinition) (string, []byte, error) {
	if m.Artwork != nil {
		data, err := pluginImage(l.defaults, m.Artwork)
		return "", data, err
	}
	path := filepath.Join(l.dir, "maps", m.ID+".png")
	_, err := os.Stat(path)
	return path, nil, err
}
func (l *UnitLibrary) PluginArtwork(id string) ([]byte, error) {
	if l.defaults == nil {
		return nil, os.ErrNotExist
	}
	binding := l.defaults.Binding()
	return l.defaults.Image(model.ArtworkReference{PluginID: binding.PluginID, Version: binding.Version, Kind: "unit", ImageID: id})
}
func (l *MapLibrary) Copy(id, name string) (model.MapDefinition, error) {
	m, err := l.LoadMap(id)
	if err != nil {
		return model.MapDefinition{}, err
	}
	if !m.ReadOnly {
		return model.MapDefinition{}, errors.New("choose a plugin default to copy")
	}
	if !m.Board.WithinLimits() {
		return model.MapDefinition{}, model.ErrCapacity
	}
	m.Name = name
	return l.Create(m, nil)
}

// SaveMap copies a default on first save; owned maps retain their ID.
func (l *MapLibrary) SaveMap(m model.MapDefinition, board model.SavedBoard) (model.MapDefinition, error) {
	if !board.WithinLimits() {
		return model.MapDefinition{}, model.ErrCapacity
	}
	metadata, ok := l.MetadataByID(m.ID)
	if !ok {
		return model.MapDefinition{}, os.ErrNotExist
	}
	if metadata.ReadOnly {
		m.Source, m.Artwork = metadata.Source, metadata.Artwork
		m.Board = &board
		return l.Create(m, nil)
	}
	if err := l.SaveBoard(m, board); err != nil {
		return model.MapDefinition{}, err
	}
	m.Board = nil
	return m, nil
}
