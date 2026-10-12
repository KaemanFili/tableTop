package app

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMongoImportsOversizedBoardWithoutBlockingLibrary(t *testing.T) {
	f := newMongoLibraryFixture(t)
	f.definition.Board = &SavedBoard{Units: make([]UnitData, MaxBoardUnits+1)}
	data, err := json.Marshal([]MapDefinition{f.definition})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.dir, "maps.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	_, maps := f.libraries(t, "first")
	if len(maps.Maps()) != 1 {
		t.Fatal("oversized map was not imported")
	}
	if err := maps.Delete(f.definition.ID); err != nil {
		t.Fatal(err)
	}
	_, maps = f.libraries(t, "first")
	if len(maps.Maps()) != 0 {
		t.Fatal("oversized map reappeared after deletion")
	}
}

type lostDeleteResponseStore[T any] struct{ DocumentStore[T] }

func (s lostDeleteResponseStore[T]) Delete(id string) error {
	if err := s.DocumentStore.Delete(id); err != nil {
		return err
	}
	return context.DeadlineExceeded
}

func TestMongoConfirmsDeleteAfterLostResponse(t *testing.T) {
	f := newMongoLibraryFixture(t)
	units, maps := f.libraries(t, "first")
	maps.store = lostDeleteResponseStore[MapDefinition]{maps.store}
	units.store = lostDeleteResponseStore[UnitDefinition]{units.store}
	if err := maps.Delete(f.definition.ID); err != nil {
		t.Fatal(err)
	}
	if err := units.Delete(f.unit.ID); err != nil {
		t.Fatal(err)
	}
	if _, exists := maps.MapByID(f.definition.ID); exists {
		t.Fatal("confirmed map deletion retained cached entry")
	}
	if _, exists := units.UnitByID(f.unit.ID); exists {
		t.Fatal("confirmed unit deletion retained cached entry")
	}
	units, maps = f.libraries(t, "first")
	if len(maps.Maps()) != 0 {
		t.Fatal("deleted map reappeared after reload")
	}
	if _, exists := units.UnitByID(f.unit.ID); exists {
		t.Fatal("deleted unit reappeared after reload")
	}
}

func TestMongoTabletopDeletionOnlyRemovesItsScope(t *testing.T) {
	f := newMongoLibraryFixture(t)
	scope, other := rand.Text(), rand.Text()
	f.libraries(t, scope)
	f.libraries(t, other)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := f.store.db.Collection("settings").InsertMany(ctx, []any{
		bson.M{"_id": "unit_statuses/" + scope},
		bson.M{"_id": "unit_statuses/" + other},
		bson.M{"_id": "unit_statuses"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := f.store.DeleteScope(scope); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"maps", "units"} {
		count, err := f.store.db.Collection(name).CountDocuments(ctx, bson.M{"scope": scope})
		if err != nil || count != 0 {
			t.Fatalf("%s retained deleted scope: %v", name, err)
		}
		count, err = f.store.db.Collection(name).CountDocuments(ctx, bson.M{"scope": other})
		if err != nil || count == 0 {
			t.Fatalf("%s lost other scope: %v", name, err)
		}
	}
	_, initialized, err := f.store.Maps(scope).Load()
	if err != nil || initialized {
		t.Fatal("map initialization marker survived deletion")
	}
	_, initialized, err = f.store.Units(scope).Load()
	if err != nil || initialized {
		t.Fatal("unit initialization marker survived deletion")
	}
	count, err := f.store.db.Collection("settings").CountDocuments(ctx, bson.M{"_id": "unit_statuses/" + scope})
	if err != nil || count != 0 {
		t.Fatal("session statuses survived deletion")
	}
	count, err = f.store.db.Collection("settings").CountDocuments(ctx, bson.M{"_id": bson.M{"$in": []string{"unit_statuses/" + other, "unit_statuses"}}})
	if err != nil || count != 2 {
		t.Fatal("deletion removed other or server-wide statuses")
	}
}

func TestEmbeddedBoardBSONSizeAndRoundTrip(t *testing.T) {
	board := SavedBoard{}
	for i := range MaxBoardUnits {
		board.Units = append(board.Units, UnitData{ID: strings.Repeat("u", 32), Name: strings.Repeat("界", 80), ImageID: strings.Repeat("i", 32), UnitType: "npc", Status: "Poisoned", Size: SizeMedium, ImageFit: DefaultImageFit(), Left: i, Top: i})
	}
	for range MaxBoardStrokes {
		board.Strokes = append(board.Strokes, PenStroke{ID: strings.Repeat("s", 64), Owner: strings.Repeat("o", 32), Type: "penStroke", Color: "#123456", Width: 5, Finished: true, Points: make([]BoardPoint, 25)})
	}
	for range MaxFogMarks {
		board.Fog = append(board.Fog, FogMark{ID: strings.Repeat("f", 64), Owner: strings.Repeat("o", 32), Type: "fogMark", Mode: "erase", Width: 2, Points: make([]BoardPoint, 5)})
	}
	for range MaxTabletopMembers {
		board.Rulers = append(board.Rulers, BoardRuler{ID: strings.Repeat("r", 64), Type: "rulerChanged", Start: &BoardPoint{X: 1, Y: 2}, End: &BoardPoint{X: 3, Y: 4}})
	}
	if !board.WithinLimits() {
		t.Fatal("fixture exceeds board limits")
	}
	definition := MapDefinition{ID: strings.Repeat("a", 32), Name: "Forest", Width: 64, Height: 64, Board: &board}
	store := &mongoDocumentStore[MapDefinition]{name: "maps", scope: "dev", id: func(m MapDefinition) string { return m.ID }}
	data, err := store.encode(definition)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("embedded board with all count limits filled: %d BSON bytes", len(data))
	var restored mongoDocument[MapDefinition]
	if err := bson.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored.Definition, definition) {
		t.Fatal("BSON round trip lost board data")
	}
	definition.Board.Strokes[0].Owner = strings.Repeat("x", maxMongoDocumentBytes)
	if _, err := store.encode(definition); !errors.Is(err, ErrCapacity) {
		t.Fatalf("oversized document: %v", err)
	}
}

// Set TABLETOP_TEST_MONGODB_URI to run against an isolated MongoDB server.
// Every test uses its own randomly named database and drops only that database.
func testMongoStore(t *testing.T) *MongoStore {
	t.Helper()
	uri := os.Getenv("TABLETOP_TEST_MONGODB_URI")
	if uri == "" {
		t.Skip("TABLETOP_TEST_MONGODB_URI is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	store, err := NewMongoStore(ctx, uri, "tabletop_test_"+rand.Text())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := store.db.Drop(ctx); err != nil {
			t.Error(err)
		}
		if err := store.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return store
}

type mongoLibraryFixture struct {
	store               *MongoStore
	dir                 string
	unit                UnitDefinition
	definition          MapDefinition
	board               SavedBoard
	unitsJSON, mapsJSON []byte
}

func newMongoLibraryFixture(t *testing.T) mongoLibraryFixture {
	t.Helper()
	store := testMongoStore(t)
	dir := t.TempDir()
	oldUnits, err := NewUnitLibrary(dir)
	if err != nil {
		t.Fatal(err)
	}
	unit, err := oldUnits.Create("Scout", "npc", SizeSmall, DefaultImageFit(), []byte("unit artwork"))
	if err != nil {
		t.Fatal(err)
	}
	oldMaps, err := NewMapLibrary(dir)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := oldMaps.Create(MapDefinition{Name: "Forest", Width: 20, Height: 10}, []byte("map artwork"))
	if err != nil {
		t.Fatal(err)
	}
	board := SavedBoard{Units: []UnitData{{ID: "U1", Name: "Scout", Status: "Poisoned", Size: SizeSmall, ImageID: unit.ImageID, ImageFit: DefaultImageFit(), Left: 120, Top: 150}}, Fog: []FogMark{{Mode: "paint", Points: []BoardPoint{{X: 2, Y: 3}}}, {Mode: "erase", Points: []BoardPoint{{X: 2, Y: 3}}}}}
	if err := oldMaps.SaveBoard(definition, board); err != nil {
		t.Fatal(err)
	}
	unitsJSON, err := os.ReadFile(filepath.Join(dir, "units.json"))
	if err != nil {
		t.Fatal(err)
	}
	mapsJSON, err := os.ReadFile(filepath.Join(dir, "maps.json"))
	if err != nil {
		t.Fatal(err)
	}
	return mongoLibraryFixture{store, dir, unit, definition, board, unitsJSON, mapsJSON}
}

func (f mongoLibraryFixture) libraries(t *testing.T, scope string) (*UnitLibrary, *MapLibrary) {
	t.Helper()
	units, err := NewUnitLibraryWithStore(f.dir, f.store.Units(scope))
	if err != nil {
		t.Fatal(err)
	}
	maps, err := NewMapLibraryWithStore(f.dir, f.store.Maps(scope))
	if err != nil {
		t.Fatal(err)
	}
	return units, maps
}

func TestMongoLibrariesImportJSON(t *testing.T) {
	f := newMongoLibraryFixture(t)
	units, maps := f.libraries(t, "first")
	if got, ok := units.UnitByID(f.unit.ID); !ok || got != f.unit {
		t.Fatal("unit import lost data")
	}
	got, ok := maps.MapByID(f.definition.ID)
	if !ok || !reflect.DeepEqual(got.Board, &f.board) {
		t.Fatal("map import lost embedded board or status")
	}
	for path, original := range map[string][]byte{"units.json": f.unitsJSON, "maps.json": f.mapsJSON} {
		data, err := os.ReadFile(filepath.Join(f.dir, path))
		if err != nil || string(data) != string(original) {
			t.Fatalf("import changed %s", path)
		}
	}
}

func TestMongoLibrariesCreateDocuments(t *testing.T) {
	store := testMongoStore(t)
	dir := t.TempDir()
	units, err := NewUnitLibraryWithStore(dir, store.Units("first"))
	if err != nil {
		t.Fatal(err)
	}
	unit, err := units.Create("Mage", "pc", SizeLarge, DefaultImageFit(), []byte("artwork"))
	if err != nil {
		t.Fatal(err)
	}
	maps, err := NewMapLibraryWithStore(dir, store.Maps("first"))
	if err != nil {
		t.Fatal(err)
	}
	definition, err := maps.Create(MapDefinition{Name: "Dungeon", Width: 20, Height: 10}, []byte("artwork"))
	if err != nil {
		t.Fatal(err)
	}
	units, err = NewUnitLibraryWithStore(dir, store.Units("first"))
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := units.UnitByID(unit.ID); !ok || got != unit {
		t.Fatal("created unit document did not survive reload")
	}
	maps, err = NewMapLibraryWithStore(dir, store.Maps("first"))
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := maps.MapByID(definition.ID); !ok || !reflect.DeepEqual(got, definition) {
		t.Fatal("created map document did not survive reload")
	}
	for _, name := range []string{"maps.json", "units.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("MongoDB mode wrote %s: %v", name, err)
		}
	}
}

func TestMongoMapSaveReplacesEmbeddedBoard(t *testing.T) {
	f := newMongoLibraryFixture(t)
	_, maps := f.libraries(t, "first")
	f.board.Units[0].Status = "Prone"
	if err := maps.SaveBoard(f.definition, f.board); err != nil {
		t.Fatal(err)
	}
	_, reloaded := f.libraries(t, "first")
	got, _ := reloaded.MapByID(f.definition.ID)
	if !reflect.DeepEqual(got.Board, &f.board) {
		t.Fatal("save did not replace embedded board")
	}
	if err := maps.SaveBoard(f.definition, SavedBoard{}); err != nil {
		t.Fatal(err)
	}
	_, reloaded = f.libraries(t, "first")
	got, _ = reloaded.MapByID(f.definition.ID)
	if !reflect.DeepEqual(got.Board, &SavedBoard{}) {
		t.Fatal("save retained removed board content")
	}
}

func TestMongoLibraryDeletionDoesNotReimportJSON(t *testing.T) {
	f := newMongoLibraryFixture(t)
	units, maps := f.libraries(t, "first")
	for _, u := range units.Units() {
		if err := units.Delete(u.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := maps.Delete(f.definition.ID); err != nil {
		t.Fatal(err)
	}
	units, maps = f.libraries(t, "first")
	if len(units.Units()) != 0 {
		t.Fatal("empty unit library resurrected entries")
	}
	if len(maps.Maps()) != 0 {
		t.Fatal("empty map library resurrected entries")
	}
	if _, err := os.Stat(filepath.Join(f.dir, "images", f.unit.ID+".png")); err != nil {
		t.Fatal("deleted unit lost shared artwork:", err)
	}
	if _, err := os.Stat(filepath.Join(f.dir, "maps", f.definition.ID+".png")); !os.IsNotExist(err) {
		t.Fatal("deleted map retained artwork:", err)
	}
}

func TestMongoLibrariesIsolateTabletops(t *testing.T) {
	f := newMongoLibraryFixture(t)
	firstUnits, firstMaps := f.libraries(t, "first")
	secondUnits, secondMaps := f.libraries(t, "second")
	// Import the same IDs into two scopes, then change only the first scope.
	if err := firstUnits.Delete(f.unit.ID); err != nil {
		t.Fatal(err)
	}
	f.board.Units[0].Status = "Prone"
	if err := firstMaps.SaveBoard(f.definition, f.board); err != nil {
		t.Fatal(err)
	}
	secondUnits, secondMaps = f.libraries(t, "second")
	if _, exists := secondUnits.UnitByID(f.unit.ID); !exists {
		t.Fatal("unit deletion crossed tabletop scopes")
	}
	got, _ := secondMaps.MapByID(f.definition.ID)
	if got.Board.Units[0].Status != "Poisoned" {
		t.Fatal("board save crossed tabletop scopes")
	}
	empty, err := NewMapLibraryWithStore(t.TempDir(), f.store.Maps("third"))
	if err != nil || len(empty.Maps()) != 0 {
		t.Fatalf("new tabletop inherited another library: %v", err)
	}
}

type failingDocumentStore[T any] struct {
	DocumentStore[T]
}

func (f failingDocumentStore[T]) Put(T) error         { return errors.New("database unavailable") }
func (f failingDocumentStore[T]) Delete(string) error { return errors.New("database unavailable") }

func TestMongoMapWriteFailurePreservesSavedBoard(t *testing.T) {
	f := newMongoLibraryFixture(t)
	_, maps := f.libraries(t, "first")
	maps.store = failingDocumentStore[MapDefinition]{maps.store}
	if err := maps.SaveBoard(f.definition, SavedBoard{}); err == nil {
		t.Fatal("failed save reported success")
	}
	if err := maps.Delete(f.definition.ID); err == nil {
		t.Fatal("failed deletion reported success")
	}
	entries := maps.Maps()
	if len(entries) != 1 || !reflect.DeepEqual(entries[0].Board, &f.board) {
		t.Fatal("failed write changed in-memory saved board")
	}
	if _, exists := maps.MapByID(f.definition.ID); exists {
		t.Fatal("unconfirmed deletion remained loadable")
	}
	if err := maps.SaveBoard(f.definition, SavedBoard{}); err == nil {
		t.Fatal("save allowed during unconfirmed deletion")
	}
	_, reloaded := f.libraries(t, "first")
	got, _ := reloaded.MapByID(f.definition.ID)
	if !reflect.DeepEqual(got.Board, &f.board) {
		t.Fatal("failed write changed persisted board")
	}
	if _, err := os.Stat(filepath.Join(f.dir, "maps", f.definition.ID+".png")); err != nil {
		t.Fatal("failed deletion removed artwork:", err)
	}
}

func TestMongoSessionStatusesSeedOnce(t *testing.T) {
	store := testMongoStore(t)
	custom := []UnitStatusOption{{Value: "Marked", Color: "#123456"}}
	if err := store.InitializeStatuses("first", custom); err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeStatuses("first", defaultUnitStatuses()); err != nil {
		t.Fatal(err)
	}
	got, err := store.LoadStatuses("first", defaultUnitStatuses())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, custom) {
		t.Fatal("retry replaced statuses")
	}
	other, err := store.LoadStatuses("second", defaultUnitStatuses())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(other, defaultUnitStatuses()) {
		t.Fatal("status definitions crossed scopes")
	}
}
func TestMongoLegacyStatusesCopiedOnce(t *testing.T) {
	store := testMongoStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	custom := []UnitStatusOption{{Value: "Marked", Color: "#123456"}}
	if _, err := store.db.Collection("settings").InsertOne(ctx, statusDocument{"unit_statuses", 1, custom}); err != nil {
		t.Fatal(err)
	}
	got, err := store.LoadStatuses("first", defaultUnitStatuses())
	if err != nil || !reflect.DeepEqual(got, custom) {
		t.Fatalf("legacy migration: %v", err)
	}
	if _, err := store.db.Collection("settings").UpdateOne(ctx, bson.M{"_id": "unit_statuses"}, bson.M{"$set": bson.M{"options": defaultUnitStatuses()}}); err != nil {
		t.Fatal(err)
	}
	got, err = store.LoadStatuses("first", defaultUnitStatuses())
	if err != nil || !reflect.DeepEqual(got, custom) {
		t.Fatalf("legacy statuses overwritten: %v", err)
	}
}
func TestMongoInvalidSessionStatusesRejected(t *testing.T) {
	store := testMongoStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := store.db.Collection("settings").InsertOne(ctx, statusDocument{"unit_statuses/first", 1, []UnitStatusOption{{Value: "Marked", Color: "invalid"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadStatuses("first", defaultUnitStatuses()); err == nil {
		t.Fatal("invalid stored statuses accepted")
	}
}
