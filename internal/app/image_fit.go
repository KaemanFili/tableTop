package app

import (
	"errors"
	"math"
)

// ImageFit frames artwork within the token, independently of its grid size.
// Offsets are percentages of the token's width and height.
type ImageFit struct {
	Zoom    float64 `json:"zoom"`
	OffsetX float64 `json:"offsetX"`
	OffsetY float64 `json:"offsetY"`
}

func DefaultImageFit() ImageFit {
	return ImageFit{Zoom: 1}
}

func (fit ImageFit) Validate() error {
	if !finiteRange(fit.Zoom, 0.25, 4) || !finiteRange(fit.OffsetX, -50, 50) || !finiteRange(fit.OffsetY, -50, 50) {
		return errors.New("Image zoom must be between 25% and 400%, and its position must stay within the preview.")
	}
	return nil
}

func finiteRange(value, min, max float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= min && value <= max
}
