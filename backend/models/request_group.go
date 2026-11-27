package models

// RequestGroup is a named group of requests inside one collection.
type RequestGroup struct {
	ID           int64   `json:"id"`
	CollectionID int64   `json:"collection_id"`
	Name         string  `json:"name"`
	Color        *string `json:"color"`
	SortOrder    int     `json:"sort_order"`
}
