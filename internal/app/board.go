package app

// Board coordinates use grid squares for drawings and pixels for placed units.
type BoardPoint struct {
	X float64 `json:"x" bson:"x"`
	Y float64 `json:"y" bson:"y"`
}

type BoardRuler struct {
	Type       string      `json:"type" bson:"type"`
	ID         string      `json:"id" bson:"id"`
	Start      *BoardPoint `json:"start,omitempty" bson:"start,omitempty"`
	End        *BoardPoint `json:"end,omitempty" bson:"end,omitempty"`
	Clear      bool        `json:"clear,omitempty" bson:"clear,omitempty"`
	Generation uint64      `json:"generation" bson:"generation"`
}

type PenStroke struct {
	Type       string       `json:"type" bson:"type"`
	ID         string       `json:"id" bson:"id"`
	Owner      string       `json:"owner" bson:"owner"`
	Offset     int          `json:"offset" bson:"offset"`
	Points     []BoardPoint `json:"points" bson:"points"`
	Finished   bool         `json:"finished" bson:"finished"`
	Color      string       `json:"color" bson:"color"`
	Width      float64      `json:"width" bson:"width"`
	Generation uint64       `json:"generation" bson:"generation"`
}

type FogMark struct {
	Type       string       `json:"type" bson:"type"`
	ID         string       `json:"id" bson:"id"`
	Owner      string       `json:"owner" bson:"owner"`
	Mode       string       `json:"mode" bson:"mode"`
	Width      float64      `json:"width" bson:"width"`
	Points     []BoardPoint `json:"points" bson:"points"`
	Generation uint64       `json:"generation" bson:"generation"`
}

type SavedBoard struct {
	Units   []UnitData   `json:"units" bson:"units"`
	Strokes []PenStroke  `json:"strokes" bson:"strokes"`
	Fog     []FogMark    `json:"fog" bson:"fog"`
	Rulers  []BoardRuler `json:"rulers" bson:"rulers"`
}
