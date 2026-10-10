package app

import (
	"sync"
	"testing"
)

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

func TestRemovalClearsPlacedUnitAndPreservesOtherCopies(t *testing.T) {
	state := NewState()
	definition := UnitDefinition{Name: "Scout", ImageID: NPCUnitImageID, Size: SizeSmall}
	removed := state.AddUnit(definition)
	other := state.AddUnit(definition)
	if !state.RemoveUnit(removed.ID) {
		t.Fatal("existing token was not removed")
	}
	if _, exists := state.UnitByID(removed.ID); exists {
		t.Fatal("removed token remains in server state")
	}
	if units := state.UnitsSnapshot(); len(units) != 1 || units[0] != other {
		t.Fatalf("board after removal = %+v, want only %+v", units, other)
	}
	if state.UnitCount() != 1 || state.RemoveUnit(removed.ID) || state.RemoveUnit("missing") {
		t.Fatal("repeated removal must not change remaining tokens")
	}
	if state.UpdateUnitPosition(removed.ID, 999, 999) {
		t.Fatal("late movement recreated a removed token")
	}
	if replacement := state.AddUnit(definition); replacement.ID == removed.ID || replacement.ID == other.ID {
		t.Fatal("spawning reused a removed token ID")
	}
}

func TestRemovalDuringConcurrentMovement(t *testing.T) {
	state := NewState()
	unit := state.AddRandomUnit(DefaultUnitImageID)
	var workers sync.WaitGroup
	workers.Go(func() {
		for i := range 100 {
			state.UpdateUnitPosition(unit.ID, i, i)
			state.UnitsSnapshot()
		}
	})
	workers.Go(func() { state.RemoveUnit(unit.ID) })
	workers.Wait()
	if state.UnitCount() != 0 || state.UpdateUnitPosition(unit.ID, 1, 2) {
		t.Fatal("concurrent movement restored a removed token")
	}
}
