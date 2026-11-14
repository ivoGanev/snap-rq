package commands

import (
	"fmt"

	"snap-rq/backend/models"
)

// ColorSlotValue turns a colour from a command payload into the value to
// store: nil (no colour) for a missing or empty colour, the slot itself
// otherwise. Anything that is not a known slot is rejected, so the database
// never holds a theme-specific colour.
func ColorSlotValue(color *string) (any, error) {
	if color == nil || *color == "" {
		return nil, nil
	}
	if !models.IsColorSlot(*color) {
		return nil, fmt.Errorf("invalid colour slot %q", *color)
	}
	return *color, nil
}
