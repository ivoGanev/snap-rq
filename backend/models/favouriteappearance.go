package models

// FavouriteAppearance stores the visual appearance of a favourite collection.
// A favourite collection always has exactly one appearance row: either an icon or a color.
type FavouriteAppearance struct {
	ID                int64  `json:"id"`
	FavouriteCollectionID int64  `json:"favourite_collection_id"`
	AppearanceType    string `json:"appearance_type"`
	AppearanceValue   string `json:"appearance_value"`
}

// DefaultFavouriteAppearance returns the default appearance for a new favourite collection.
func DefaultFavouriteAppearance() FavouriteAppearance {
	return FavouriteAppearance{
		AppearanceType:  "icon",
		AppearanceValue: "default",
	}
}
