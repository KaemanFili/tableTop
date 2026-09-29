package app

import "testing"

func TestMovementPreservesUnitImage(t *testing.T) {
	state := NewState()
	unit := state.AddRandomUnit(DefaultUnitImageID)
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

func TestSpawnCopiesKeepIdentityAndHaveUniqueIDs(t *testing.T) {
	state := NewState()
	definition := UnitDefinition{Name: "Scout", UnitType: "npc", ImageID: NPCUnitImageID, Size: SizeHuge, ImageFit: ImageFit{Zoom: 2, OffsetX: 10, OffsetY: -20}}
	for range 150 {
		unit := state.AddUnit(definition)
		if unit.ImageFit != definition.ImageFit || unit.Size != definition.Size || unit.Name != definition.Name || unit.UnitType != definition.UnitType || unit.ImageID != definition.ImageID {
			t.Fatalf("spawn lost library information: %+v", unit)
		}
		state.UpdateUnitPosition(unit.ID, 400, 500)
		want := unit
		want.Left, want.Top = 400, 500
		if got, _ := state.UnitByID(unit.ID); got != want {
			t.Fatal("movement changed unit identity")
		}
	}
	if state.UnitCount() != 150 {
		t.Fatal("spawning copies reused existing token IDs")
	}
}
