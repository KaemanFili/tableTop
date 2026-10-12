package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type lazyMapTestStore struct {
	DocumentStore[MapDefinition]
	documents map[string]MapDefinition
	gets      []string
	readErr   error
}

func (s *lazyMapTestStore) List() ([]MapDefinition, bool, error) {
	result := []MapDefinition{}
	for _, m := range s.documents {
		result = append(result, mapMetadata(m))
	}
	return result, true, nil
}
func (s *lazyMapTestStore) Get(id string) (MapDefinition, error) {
	s.gets = append(s.gets, id)
	if s.readErr != nil {
		return MapDefinition{}, s.readErr
	}
	m, ok := s.documents[id]
	if !ok {
		return m, os.ErrNotExist
	}
	return m, nil
}
func (s *lazyMapTestStore) InitializeEach(func(func(MapDefinition) error) error) error {
	return errors.New("unexpected initialization")
}

func TestMapListingsAndImagePathsDoNotReadBoards(t *testing.T) {
	m := MapDefinition{ID: strings.Repeat("a", 32), Name: "Forest", Width: 20, Height: 20, Board: &SavedBoard{Units: []UnitData{{Name: "Scout"}}}}
	store := &lazyMapTestStore{documents: map[string]MapDefinition{m.ID: m}}
	library, err := NewMapLibraryWithStore(t.TempDir(), store)
	if err != nil {
		t.Fatal(err)
	}
	list := library.Maps()
	if len(list) != 1 || list[0].Board != nil || !list[0].HasSavedBoard {
		t.Fatal("listing contains board or lost presence flag")
	}
	if _, ok := library.ImagePath(m.ID); !ok {
		t.Fatal("image metadata unavailable")
	}
	if len(store.gets) != 0 {
		t.Fatal("listing or image path loaded a board")
	}
}
func TestLoadMapReadsOnlySelectedDocument(t *testing.T) {
	first := MapDefinition{ID: strings.Repeat("a", 32), Name: "First", Width: 20, Height: 20, Board: &SavedBoard{}}
	second := MapDefinition{ID: strings.Repeat("b", 32), Name: "Second", Width: 20, Height: 20, Board: &SavedBoard{}}
	store := &lazyMapTestStore{documents: map[string]MapDefinition{first.ID: first, second.ID: second}}
	library, err := NewMapLibraryWithStore(t.TempDir(), store)
	if err != nil {
		t.Fatal(err)
	}
	got, err := library.LoadMap(second.ID)
	if err != nil || !reflect.DeepEqual(got, second) {
		t.Fatalf("selected map: %v", err)
	}
	if !reflect.DeepEqual(store.gets, []string{second.ID}) {
		t.Fatal("selection read other documents")
	}
	if library.maps[second.ID].Board != nil {
		t.Fatal("selected board retained in metadata cache")
	}
}
func TestJSONMapRewritePreservesUntouchedDocuments(t *testing.T) {
	dir := t.TempDir()
	first := MapDefinition{ID: strings.Repeat("a", 32), Name: "First", Width: 20, Height: 20}
	second := MapDefinition{ID: strings.Repeat("b", 32), Name: "Second", Width: 20, Height: 20, Board: &SavedBoard{Units: []UnitData{{Name: "Keep me", Status: "Marked"}}}}
	raw, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	var extended map[string]any
	if err := json.Unmarshal(raw, &extended); err != nil {
		t.Fatal(err)
	}
	extended["futureField"] = "preserve"
	if err := writeJSONFile(filepath.Join(dir, "maps.json"), []any{first, extended}); err != nil {
		t.Fatal(err)
	}
	library, err := NewMapLibrary(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "maps", first.ID+".png"), []byte("art"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := library.SaveBoard(first, SavedBoard{}); err != nil {
		t.Fatal(err)
	}
	if err := library.Delete(first.ID); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "maps.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"futureField":"preserve"`) {
		t.Fatal("untouched fields were lost")
	}
	got, err := library.LoadMap(second.ID)
	if err != nil || !reflect.DeepEqual(got, second) {
		t.Fatalf("untouched board changed: %v", err)
	}
}
func TestMongoDeleteRollbackPreservesEmbeddedBoard(t *testing.T) {
	library, store, m, image := newMapForDeleteTest(t)
	board := SavedBoard{Units: []UnitData{{Name: "Keep me", Status: "Marked"}}}
	if err := library.SaveBoard(m, board); err != nil {
		t.Fatal(err)
	}
	store.commitDelete = true
	if err := os.Remove(image); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(image, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(image, "block"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := library.Delete(m.ID); err == nil {
		t.Fatal("image removal failure ignored")
	}
	restored := store.documents[m.ID]
	if restored.Board == nil || !reflect.DeepEqual(*restored.Board, board) {
		t.Fatal("rollback restored metadata without the saved board")
	}
}
