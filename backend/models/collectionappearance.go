package models

// CollectionAppearance stores the visual appearance of a collection.
// A collection always has exactly one appearance row: either an icon or a color.
type CollectionAppearance struct {
	ID              int64  `json:"id"`
	CollectionID    int64  `json:"collection_id"`
	AppearanceType  string `json:"appearance_type"`
	AppearanceValue string `json:"appearance_value"`
}

// DefaultCollectionAppearance returns the default appearance for a new collection.
func DefaultCollectionAppearance() CollectionAppearance {
	return CollectionAppearance{
		AppearanceType:  "icon",
		AppearanceValue: "default",
	}
}
