package handlers

import (
	"encoding/json"
	"testing"

	"tableTop/main/internal/app"
)

func TestWholeBoardFogPermissionsStateAndValidation(t *testing.T) {
	state := app.NewState()
	token := state.AddRandomUnit(app.DefaultUnitImageID)
	h := NewWebSocketHandler(state)
	mark := []byte(`{"type":"fogMark","id":"1","mode":"paint","width":6,"points":[{"x":1,"y":1},{"x":2,"y":2}]}`)
	if err := h.updateFog("gm", mark); err != nil {
		t.Fatal(err)
	}
	stroke := []byte(`{"type":"penStroke","id":"1","color":"#123456","width":240,"finished":true,"points":[{"x":3,"y":3}]}`)
	if err := h.updatePen("gm", stroke); err != nil {
		t.Fatal(err)
	}
	cover := []byte(`{"type":"fogSet","covered":true,"generation":0}`)
	h.handleBoardUtilitiesMessage("player", cover, false)
	if len(h.fogMarks) != 1 || h.fogMarks[0].Mode != "paint" {
		t.Fatal("player changed fog")
	}
	h.handleBoardUtilitiesMessage("gm", []byte(`{"type":"fogSet","covered":true,"generation":1}`), true)
	if h.fogMarks[0].Mode != "paint" {
		t.Fatal("stale action changed fog")
	}
	h.handleBoardUtilitiesMessage("gm", cover, true)
	if len(h.fogMarks) != 1 || h.fogMarks[0].Mode != "fill" || h.drawingPoints != 1 {
		t.Fatal("cover did not replace fog history and release capacity")
	}
	// Validate the serialized content used by saved maps, including the wider pen.
	saved := app.SavedBoard{Fog: h.fogMarks, Strokes: []app.PenStroke{h.strokes["gm/1"]}}
	encoded, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var restored app.SavedBoard
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	if err := restored.Validate(); err != nil {
		t.Fatalf("saved full fog and thick pen invalid: %v", err)
	}
	h.handleBoardUtilitiesMessage("gm", []byte(`{"type":"fogSet","covered":false}`), true)
	if len(h.fogMarks) != 0 || h.drawingPoints != 1 || len(h.strokes) != 1 {
		t.Fatal("clear changed drawings or retained fog")
	}
	if got, _ := state.UnitByID(token.ID); got != token {
		t.Fatal("fog action changed token")
	}
	// Saved map loading must restore the full cover before subsequent erasures.
	h.restoreFogLocked(restored.Fog, "loaded")
	if len(h.fogMarks) != 1 || h.fogMarks[0].Mode != "fill" {
		t.Fatal("saved fog was not restored")
	}
	if err := h.updateFog("gm", []byte(`{"type":"fogMark","id":"2","mode":"erase","width":6,"points":[{"x":32,"y":32}]}`)); err != nil {
		t.Fatal(err)
	}
	if len(h.fogMarks) != 2 || h.fogMarks[1].Mode != "erase" {
		t.Fatal("full cover cannot be erased")
	}
}
