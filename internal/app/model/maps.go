package model

import (
	"errors"
	"math"
	"strings"
	"unicode/utf8"
)

type MapDefinition struct {
	ReadOnly      bool              `json:"-" bson:"-"`
	Artwork       *ArtworkReference `json:"artwork,omitempty" bson:"artwork,omitempty"`
	ID            string            `json:"id" bson:"id"`
	Name          string            `json:"name" bson:"name"`
	Width         float64           `json:"width" bson:"width"`
	Height        float64           `json:"height" bson:"height"`
	OffsetX       float64           `json:"offsetX" bson:"offsetX"`
	OffsetY       float64           `json:"offsetY" bson:"offsetY"`
	HasSavedBoard bool              `json:"-" bson:"-"`
	Source        *ContentSource    `json:"source,omitempty" bson:"source,omitempty"`
	Board         *SavedBoard       `json:"board,omitempty" bson:"board,omitempty"`
}

func (m MapDefinition) Validate() error {
	if strings.TrimSpace(m.Name) == "" || !utf8.ValidString(m.Name) || utf8.RuneCountInString(m.Name) > 80 {
		return errors.New("Enter a map name between 1 and 80 characters.")
	}
	for _, value := range []float64{m.Width, m.Height} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0.1 || value > 64 {
			return errors.New("Map width and height must be between 0.1 and 64 grid squares.")
		}
	}
	for _, value := range []float64{m.OffsetX, m.OffsetY} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < -64 || value > 64 {
			return errors.New("Map position must be between -64 and 64 grid squares.")
		}
	}
	return nil
}
