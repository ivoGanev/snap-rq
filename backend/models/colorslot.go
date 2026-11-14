package models

import "strings"

// ColorSlots are the only values stored in a user_color column. A slot names
// a position in the palette, never an actual colour: each UI theme decides
// what colour a slot shows, so the database stays theme independent.
var ColorSlots = []string{
	"primary",
	"secondary",
	"tertiary",
	"quaternary",
	"quinary",
	"senary",
}

// IsColorSlot reports whether v is a valid colour slot.
func IsColorSlot(v string) bool {
	for _, slot := range ColorSlots {
		if v == slot {
			return true
		}
	}
	return false
}

// legacyColorSlots maps the hex values stored before colour slots existed
// (lower case) to the slot that replaced them.
var legacyColorSlots = map[string]string{
	"#aecbfa": "primary",
	"#b6e2c6": "secondary",
	"#f7e6a3": "tertiary",
}

// LegacyColorSlot returns the slot a pre-slot hex value migrates to.
func LegacyColorSlot(hex string) (string, bool) {
	slot, ok := legacyColorSlots[strings.ToLower(hex)]
	return slot, ok
}

// LegacyColorHexes lists the pre-slot hex values that have a slot.
func LegacyColorHexes() []string {
	hexes := make([]string, 0, len(legacyColorSlots))
	for hex := range legacyColorSlots {
		hexes = append(hexes, hex)
	}
	return hexes
}
