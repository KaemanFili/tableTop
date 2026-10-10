package app

import (
	"errors"
	"sync"
	"testing"
)

func TestResizeStepsLimitsAndPreservedFields(t *testing.T) {
	s := NewState()
	definition := UnitDefinition{Name: "Scout", UnitType: "npc", ImageID: NPCUnitImageID, Size: SizeTiny, ImageFit: ImageFit{Zoom: 2, OffsetX: 10, OffsetY: -20}}
	token := s.AddUnit(definition)
	other := s.AddUnit(definition)
	sizes := UnitSizes()
	check := func(direction string, want UnitSize, wantChanged bool) {
		t.Helper()
		got, changed, err := s.ResizeUnit(token.ID, direction)
		expected := token
		expected.Size = want
		if err != nil || changed != wantChanged || got != expected {
			t.Fatalf("resize %s = %+v, %v, %v; want %+v, %v", direction, got, changed, err, expected, wantChanged)
		}
	}
	check("down", SizeTiny, false)
	for _, size := range sizes[1:] {
		check("up", size.Value, true)
	}
	check("up", SizeGargantuan, false)
	for i := len(sizes) - 2; i >= 0; i-- {
		check("down", sizes[i].Value, true)
	}
	check("down", SizeTiny, false)
	if got, _ := s.UnitByID(other.ID); got != other {
		t.Fatal("resizing changed another copy")
	}
	if _, changed, err := s.ResizeUnit(token.ID, "invalid"); changed || !errors.Is(err, ErrInvalidSizeDirection) {
		t.Fatal("invalid direction accepted")
	}
	if _, _, err := s.ResizeUnit("missing", "up"); !errors.Is(err, ErrUnitNotFound) {
		t.Fatal("missing token accepted")
	}
	s.ResizeUnit(token.ID, "up")
	s.UpdateUnitPosition(token.ID, 400, 500)
	got, _ := s.UnitByID(token.ID)
	if got.Size != SizeSmall || got.Left != 400 || got.Top != 500 || got.ImageFit != token.ImageFit {
		t.Fatal("movement lost resized size or framing")
	}
	s.RemoveUnit(token.ID)
	if _, _, err := s.ResizeUnit(token.ID, "down"); !errors.Is(err, ErrUnitNotFound) || s.UnitCount() != 1 {
		t.Fatal("resize recreated removed token")
	}
}

func TestConcurrentResizeUsesStoredSize(t *testing.T) {
	s := NewState()
	token := s.AddUnit(UnitDefinition{Size: SizeTiny})
	var work sync.WaitGroup
	// Four requests must advance four categories, even when they arrive together.
	for range 4 {
		work.Go(func() {
			if _, _, err := s.ResizeUnit(token.ID, "up"); err != nil {
				t.Error(err)
			}
		})
	}
	work.Go(func() {
		for i := range 100 {
			s.UpdateUnitPosition(token.ID, i, i)
		}
	})
	work.Wait()
	got, _ := s.UnitByID(token.ID)
	if got.Size != SizeHuge || got.Left != 99 {
		t.Fatalf("concurrent operations lost fields: %+v", got)
	}
}
