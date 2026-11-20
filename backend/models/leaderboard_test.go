package models_test

import (
	"testing"

	"islandBattles/backend/models"
)

// Ratings round to the nearest whole number, halves up, as a browser's
// Math.round does.
func TestRoundRating(t *testing.T) {
	for rating, want := range map[float64]int{
		1500:      1500,
		1520.4999: 1520,
		1520.5:    1521,
		1519.6:    1520,
		1479.5:    1480,
		-0.5:      0,
		-1.6:      -2,
	} {
		if got := models.RoundRating(rating); got != want {
			t.Errorf("RoundRating(%g) = %d, want %d", rating, got, want)
		}
	}
}
