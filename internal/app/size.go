package app

type UnitSize string

const (
	SizeTiny       UnitSize = "tiny"
	SizeSmall      UnitSize = "small"
	SizeMedium     UnitSize = "medium"
	SizeLarge      UnitSize = "large"
	SizeHuge       UnitSize = "huge"
	SizeGargantuan UnitSize = "gargantuan"
)

type UnitSizeOption struct {
	Value     UnitSize
	Label     string
	Footprint string
	Squares   float64
}

// UnitSizes lists the D&D footprints; Squares is the width and height in 5-foot cells.
func UnitSizes() []UnitSizeOption {
	return []UnitSizeOption{
		{SizeTiny, "Tiny", "2½ × 2½ ft · ½ × ½ square", 0.5},
		{SizeSmall, "Small", "5 × 5 ft · 1 × 1 square", 1},
		{SizeMedium, "Medium", "5 × 5 ft · 1 × 1 square", 1},
		{SizeLarge, "Large", "10 × 10 ft · 2 × 2 squares", 2},
		{SizeHuge, "Huge", "15 × 15 ft · 3 × 3 squares", 3},
		{SizeGargantuan, "Gargantuan", "20 × 20 ft · 4 × 4 squares", 4},
	}
}

func (s UnitSize) Valid() bool {
	for _, option := range UnitSizes() {
		if option.Value == s {
			return true
		}
	}
	return false
}

func (s UnitSize) Label() string {
	for _, option := range UnitSizes() {
		if option.Value == s {
			return option.Label
		}
	}
	return "Medium"
}

func (s UnitSize) Squares() float64 {
	for _, option := range UnitSizes() {
		if option.Value == s {
			return option.Squares
		}
	}
	return 1
}
