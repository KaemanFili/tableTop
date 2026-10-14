package model

// UnitDefinition is a reusable unit in the library, separate from a placed token.
type UnitDefinition struct {
	ReadOnly bool              `json:"-" bson:"-"`
	Artwork  *ArtworkReference `json:"artwork,omitempty" bson:"artwork,omitempty"`
	Source   *ContentSource    `json:"source,omitempty" bson:"source,omitempty"`
	ID       string            `json:"id" bson:"id"`
	Name     string            `json:"name" bson:"name"`
	UnitType string            `json:"unitType" bson:"unitType"`
	ImageID  string            `json:"imageID" bson:"imageID"`
	Size     UnitSize          `json:"size" bson:"size"`
	ImageFit ImageFit          `json:"imageFit" bson:"imageFit"`
}
