package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"

	"tableTop/main/internal/app/model"
	"tableTop/main/internal/app/persistence"
)

func TestSaveMapPreservesBoardAndArtwork(t *testing.T) {
	dir := t.TempDir()
	maps, err := persistence.NewMapLibrary(dir)
	if err != nil {
		t.Fatal(err)
	}
	image := testPNG(t, 4, 2)
	definition, err := maps.Create(model.MapDefinition{Name: "Forest", Width: 20, Height: 10}, image)
	if err != nil {
		t.Fatal(err)
	}
	state := model.NewState()
	token := state.AddRandomUnit(model.PCUnitImageID)
	state.UpdateUnitPosition(token.ID, 240, 320)
	ws := NewWebSocketHandler(state)
	handler := newTestEndpointHandler(t, state, ws, newTestLibrary(t), maps)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	request := func(path string, want int) {
		t.Helper()
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, nil))
		if response.Code != want {
			t.Fatalf("%s: %d: %s", path, response.Code, response.Body.String())
		}
	}
	request("/board/save", http.StatusForbidden)
	request("/board/save?gm=true", http.StatusConflict)
	ws.currentMap = &definition
	stroke := penStroke{Type: "penStroke", ID: "author/1", Owner: "author", Color: "#123456", Width: 5, Points: []rulerPoint{{X: 1, Y: 2}, {X: 3, Y: 4}}}
	ws.strokes[stroke.ID] = stroke
	ws.erasedStrokes["author/2"] = true
	ws.fogMarks = []fogMark{
		{Type: "fogMark", ID: "author/1", Mode: "paint", Width: 2, Points: []rulerPoint{{X: 2, Y: 2}}},
		{Type: "fogMark", ID: "author/2", Mode: "erase", Width: 1, Points: []rulerPoint{{X: 2, Y: 2}}},
	}
	ruler := boardRuler{Type: "rulerChanged", ID: "author", Start: &rulerPoint{X: 1, Y: 1}, End: &rulerPoint{X: 4, Y: 5}}
	ws.rulers[ruler.ID] = ruler
	request("/board/save?gm=true", http.StatusNoContent)
	reloaded, err := persistence.NewMapLibrary(dir)
	if err != nil {
		t.Fatal(err)
	}
	saved, ok := reloaded.MapByID(definition.ID)
	if !ok || saved.Board == nil {
		t.Fatal("board was not persisted")
	}
	stroke.Finished = true
	want := model.SavedBoard{Units: state.Units(), Strokes: []model.PenStroke{stroke}, Fog: ws.fogMarks, Rulers: []model.BoardRuler{ruler}}
	if !reflect.DeepEqual(*saved.Board, want) {
		t.Fatalf("saved board = %+v, want %+v", *saved.Board, want)
	}
	if ws.strokes[stroke.ID].Finished {
		t.Fatal("saving changed live stroke")
	}
	path, _ := reloaded.ImagePath(definition.ID)
	storedImage, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(storedImage, image) {
		t.Fatal("saved map lost its artwork")
	}
	// Subsequent live changes must not mutate the saved library data.
	ws.fogMarks[0].Points[0].X = 8
	changed := ws.strokes[stroke.ID]
	changed.Points[0].X = 9
	ws.strokes[stroke.ID] = changed
	inMemory, _ := maps.MapByID(definition.ID)
	if inMemory.Board.Fog[0].Points[0].X != 2 || inMemory.Board.Strokes[0].Points[0].X != 1 {
		t.Fatal("saved drawings share mutable live data")
	}
	// Saving again replaces this entry, including removal of old content.
	clear(ws.strokes)
	clear(ws.rulers)
	ws.fogMarks = nil
	state.ClearUnits()
	request("/board/save?gm=true", http.StatusNoContent)
	reloaded, err = persistence.NewMapLibrary(dir)
	if err != nil {
		t.Fatal(err)
	}
	saved, _ = reloaded.MapByID(definition.ID)
	if len(saved.Board.Units)+len(saved.Board.Strokes)+len(saved.Board.Fog)+len(saved.Board.Rulers) != 0 {
		t.Fatal("repeat save retained removed content")
	}
	// Clear Map leaves the saved entry and artwork available.
	request("/board/clear?gm=true", http.StatusNoContent)
	if _, ok := maps.MapByID(definition.ID); !ok {
		t.Fatal("clear deleted save")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("clear deleted artwork")
	}
	// A missing image cannot produce a successful save or replace the previous one.
	ws.currentMap = &definition
	if err := os.Rename(path, path+".missing"); err != nil {
		t.Fatal(err)
	}
	request("/board/save?gm=true", http.StatusInternalServerError)
}
