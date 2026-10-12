package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Simulate both a committed write with a lost response and an unavailable
// database. This exercises uncertainty that a pre-write error cannot reproduce.
type uncertainDeleteStore[T any] struct {
	documents            map[string]T
	id                   func(T) string
	commitDelete         bool
	deleteErr, existsErr error
	puts                 int
}

func (s *uncertainDeleteStore[T]) Load() ([]T, bool, error) {
	var entries []T
	for _, entry := range s.documents {
		entries = append(entries, entry)
	}
	return entries, true, nil
}
func (s *uncertainDeleteStore[T]) Initialize([]T) error { return nil }
func (s *uncertainDeleteStore[T]) Put(entry T) error {
	s.puts++
	s.documents[s.id(entry)] = entry
	return nil
}
func (s *uncertainDeleteStore[T]) Delete(id string) error {
	if s.commitDelete {
		delete(s.documents, id)
	}
	return s.deleteErr
}
func (s *uncertainDeleteStore[T]) Exists(id string) (bool, error) {
	if s.existsErr != nil {
		return false, s.existsErr
	}
	_, exists := s.documents[id]
	return exists, nil
}

func newMapForDeleteTest(t *testing.T) (*MapLibrary, *uncertainDeleteStore[MapDefinition], MapDefinition, string) {
	t.Helper()
	dir := t.TempDir()
	store := &uncertainDeleteStore[MapDefinition]{documents: make(map[string]MapDefinition), id: func(m MapDefinition) string { return m.ID }}
	library, err := NewMapLibraryWithStore(dir, store)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := library.Create(MapDefinition{Name: "Forest", Width: 20, Height: 20}, []byte("artwork"))
	if err != nil {
		t.Fatal(err)
	}
	return library, store, definition, filepath.Join(dir, "maps", definition.ID+".png")
}

func TestCommittedMapDeleteWithLostResponseIsConfirmed(t *testing.T) {
	library, store, definition, image := newMapForDeleteTest(t)
	store.commitDelete, store.deleteErr = true, context.DeadlineExceeded
	if err := library.Delete(definition.ID); err != nil {
		t.Fatal(err)
	}
	if _, exists := library.MapByID(definition.ID); exists || len(library.Maps()) != 0 {
		t.Fatal("confirmed delete left a cached map")
	}
	if _, err := os.Stat(image); !os.IsNotExist(err) {
		t.Fatal("confirmed delete retained map artwork")
	}
	if err := library.SaveBoard(definition, SavedBoard{}); err == nil {
		t.Fatal("save recreated a deleted map")
	}
}

func TestUnconfirmedMapDeleteBlocksLoadAndSaveUntilRetry(t *testing.T) {
	for _, commit := range []bool{false, true} {
		name := "document still exists"
		if commit {
			name = "confirmation unavailable"
		}
		t.Run(name, func(t *testing.T) {
			library, store, definition, image := newMapForDeleteTest(t)
			store.commitDelete, store.deleteErr = commit, context.DeadlineExceeded
			if commit {
				store.existsErr = errors.New("database unavailable")
			}
			if err := library.Delete(definition.ID); err == nil {
				t.Fatal("unconfirmed deletion reported success")
			}
			if _, exists := library.MapByID(definition.ID); exists {
				t.Fatal("unconfirmed map remained loadable")
			}
			if len(library.Maps()) != 1 {
				t.Fatal("map disappeared from the list needed to retry deletion")
			}
			puts := store.puts
			if err := library.SaveBoard(definition, SavedBoard{}); err == nil || store.puts != puts {
				t.Fatal("unconfirmed map was saved")
			}
			if _, err := os.Stat(image); err != nil {
				t.Fatal("unconfirmed deletion removed artwork")
			}
			store.commitDelete, store.deleteErr, store.existsErr = true, nil, nil
			if err := library.Delete(definition.ID); err != nil {
				t.Fatal(err)
			}
			if len(library.Maps()) != 0 {
				t.Fatal("retry retained deleted map")
			}
			if _, err := os.Stat(image); !os.IsNotExist(err) {
				t.Fatal("retry retained artwork")
			}
		})
	}
}

func TestUnitDeleteWithLostResponsePreservesArtwork(t *testing.T) {
	for _, confirmationFails := range []bool{false, true} {
		name := "confirmed"
		if confirmationFails {
			name = "retry required"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			store := &uncertainDeleteStore[UnitDefinition]{documents: make(map[string]UnitDefinition), id: func(u UnitDefinition) string { return u.ID }}
			library, err := NewUnitLibraryWithStore(dir, store)
			if err != nil {
				t.Fatal(err)
			}
			unit, err := library.Create("Scout", "npc", SizeMedium, DefaultImageFit(), []byte("artwork"))
			if err != nil {
				t.Fatal(err)
			}
			store.commitDelete, store.deleteErr = true, context.DeadlineExceeded
			if confirmationFails {
				store.existsErr = errors.New("database unavailable")
			}
			err = library.Delete(unit.ID)
			if confirmationFails != (err != nil) {
				t.Fatalf("unexpected delete result: %v", err)
			}
			if _, exists := library.UnitByID(unit.ID); exists {
				t.Fatal("deleted or unconfirmed unit remained spawnable")
			}
			if confirmationFails {
				if len(library.Units()) != 1 {
					t.Fatal("unit missing from deletion retry list")
				}
				store.deleteErr, store.existsErr = nil, nil
				if err := library.Delete(unit.ID); err != nil {
					t.Fatal(err)
				}
			}
			if len(library.Units()) != 0 {
				t.Fatal("confirmed deletion left cached unit")
			}
			if _, err := os.Stat(filepath.Join(dir, "images", unit.ID+".png")); err != nil {
				t.Fatal("unit deletion lost shared artwork")
			}
		})
	}
}
