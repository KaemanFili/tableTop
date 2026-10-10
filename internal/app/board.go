package app

// Board coordinates use grid squares for drawings and pixels for placed units.
type BoardPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type BoardRuler struct {
	Type       string      `json:"type"`
	ID         string      `json:"id"`
	Start      *BoardPoint `json:"start,omitempty"`
	End        *BoardPoint `json:"end,omitempty"`
	Clear      bool        `json:"clear,omitempty"`
	Generation uint64      `json:"generation"`
}

type PenStroke struct {
	Type       string       `json:"type"`
	ID         string       `json:"id"`
	Owner      string       `json:"owner"`
	Offset     int          `json:"offset"`
	Points     []BoardPoint `json:"points"`
	Finished   bool         `json:"finished"`
	Color      string       `json:"color"`
	Width      float64      `json:"width"`
	Generation uint64       `json:"generation"`
}

type FogMark struct {
	Type       string       `json:"type"`
	ID         string       `json:"id"`
	Owner      string       `json:"owner"`
	Mode       string       `json:"mode"`
	Width      float64      `json:"width"`
	Points     []BoardPoint `json:"points"`
	Generation uint64       `json:"generation"`
}

type SavedBoard struct {
	Units   []UnitData   `json:"units"`
	Strokes []PenStroke  `json:"strokes"`
	Fog     []FogMark    `json:"fog"`
	Rulers  []BoardRuler `json:"rulers"`
}
