package models

// TagAppearance stores the visual appearance of a tag.
// A tag always has exactly one appearance row: either an icon or a color.
type TagAppearance struct {
	ID              int64  `json:"id"`
	TagID           int64  `json:"tag_id"`
	AppearanceType  string `json:"appearance_type"`
	AppearanceValue string `json:"appearance_value"`
}

// DefaultTagAppearance returns the default appearance for a new tag.
func DefaultTagAppearance() TagAppearance {
	return TagAppearance{
		AppearanceType:  "icon",
		AppearanceValue: "default",
	}
}
