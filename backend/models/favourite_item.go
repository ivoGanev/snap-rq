package models

import "time"

// FavouriteItem links an HTTP request into a favourite collection.
type FavouriteItem struct {
	ID                     int64     `json:"id"`
	FavouriteCollectionID  int64     `json:"favourite_collection_id"`
	HTTPRequestID          int64     `json:"http_request_id"`
	AddedAt                time.Time `json:"added_at"`
	SortOrder              int       `json:"sort_order"`
}
