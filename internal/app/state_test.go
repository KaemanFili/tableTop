package app

import "testing"

func TestMovementPreservesUnitImage(t *testing.T) {
	state := NewState()
	unit := state.AddRandomUnit()
	if unit.ImageID != DefaultUnitImageID {
		t.Fatalf("spawned image = %q, want %q", unit.ImageID, DefaultUnitImageID)
	}
	if !state.UpdateUnitPosition(unit.ID, 250, 300) {
		t.Fatal("existing unit was not updated")
	}
	want := unit
	want.Left, want.Top = 250, 300
	if got, exists := state.UnitByID(unit.ID); !exists || got != want {
		t.Fatalf("moved unit = %+v, want %+v", got, want)
	}
	if state.UpdateUnitPosition("missing", 1, 2) || state.UnitCount() != 1 {
		t.Fatal("movement must not create unknown units")
	}
}
