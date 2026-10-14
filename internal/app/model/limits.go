package model

import "errors"

var ErrCapacity = errors.New("the server or board has reached its capacity; remove unused content before adding more")

const (
	MaxTabletops       = 32
	MaxTabletopMembers = 64
	MaxBoardUnits      = 500
	MaxBoardStrokes    = 1000
	MaxBoardPoints     = 50000
	MaxFogMarks        = 5000
	MaxErasedStrokes   = 4096
)

func (board *SavedBoard) WithinLimits() bool {
	if board == nil {
		return true
	}
	if len(board.Units) > MaxBoardUnits || len(board.Strokes) > MaxBoardStrokes || len(board.Fog) > MaxFogMarks || len(board.Rulers) > MaxTabletopMembers {
		return false
	}
	points := 0
	for _, stroke := range board.Strokes {
		points += len(stroke.Points)
	}
	for _, mark := range board.Fog {
		points += len(mark.Points)
	}
	return points <= MaxBoardPoints
}
