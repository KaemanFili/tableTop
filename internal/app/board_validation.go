package app

import "errors"

// Validate checks stored content before any live board is cleared.
func (b *SavedBoard) Validate() error {
	if !b.WithinLimits() {
		return ErrCapacity
	}
	if b == nil {
		return nil
	}
	for _, unit := range b.Units {
		if err := validateSavedUnit(unit); err != nil {
			return err
		}
	}
	for _, stroke := range b.Strokes {
		if err := validateSavedStroke(stroke); err != nil {
			return err
		}
	}
	for _, mark := range b.Fog {
		if err := validateSavedFog(mark); err != nil {
			return err
		}
	}
	for _, ruler := range b.Rulers {
		if err := validateSavedRuler(ruler); err != nil {
			return err
		}
	}
	return nil
}

func validBoardPoint(point BoardPoint) bool {
	return finiteRange(point.X, 0, 64) && finiteRange(point.Y, 0, 64)
}

func validateSavedUnit(unit UnitData) error {
	if unit.Size != "" && !unit.Size.Valid() {
		return errors.New("invalid saved token size")
	}
	if unit.ImageFit != (ImageFit{}) {
		if err := unit.ImageFit.Validate(); err != nil {
			return err
		}
	}
	if unit.UnitType != "" && unit.UnitType != "pc" && unit.UnitType != "npc" {
		return errors.New("invalid saved token type")
	}
	return nil
}

func validateSavedStroke(stroke PenStroke) error {
	if stroke.Type != "penStroke" || !statusColorPattern.MatchString(stroke.Color) || !finiteRange(stroke.Width, 1, 12) || len(stroke.Points) == 0 || len(stroke.Points) > 8192 {
		return errors.New("invalid saved stroke")
	}
	for _, point := range stroke.Points {
		if !validBoardPoint(point) {
			return errors.New("invalid saved stroke point")
		}
	}
	return nil
}

func validateSavedFog(mark FogMark) error {
	if mark.Type != "fogMark" || (mark.Mode != "paint" && mark.Mode != "erase") || !finiteRange(mark.Width, 0.25, 6) || len(mark.Points) == 0 {
		return errors.New("invalid saved fog")
	}
	for _, point := range mark.Points {
		if !validBoardPoint(point) {
			return errors.New("invalid saved fog point")
		}
	}
	return nil
}

func validateSavedRuler(ruler BoardRuler) error {
	if ruler.Type != "rulerChanged" || ruler.Start == nil || ruler.End == nil || !validBoardPoint(*ruler.Start) || !validBoardPoint(*ruler.End) {
		return errors.New("invalid saved ruler")
	}
	return nil
}
