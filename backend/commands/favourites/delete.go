package favourites

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// delete.go — remove a favourite collection with its memberships and
// appearance; the requests themselves are untouched. Snapshot saves all
// three; undo re-inserts them under the original ids.

type Delete struct{ tx *sql.Tx }

func NewDelete(tx *sql.Tx) commands.Command { return &Delete{tx: tx} }

func (Delete) Info() commands.Info {
	return commands.Info{FunctionID: "favourite.delete", Label: "Delete Favourite"}
}

// savedFavourite is the collection with everything that dies with it.
type savedFavourite struct {
	Collection models.FavouriteCollection  `json:"collection"`
	Appearance *models.FavouriteAppearance `json:"appearance"`
	Items      []savedItem                 `json:"items"`
}

type savedItem struct {
	ID                    int64   `json:"id"`
	FavouriteCollectionID int64   `json:"favourite_collection_id"`
	HTTPRequestID         int64   `json:"http_request_id"`
	UserSortOrder         int     `json:"user_sort_order"`
	UserColor             *string `json:"user_color"`
}

func (c Delete) Snapshot(input json.RawMessage) (any, error) {
	var in struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}

	saved := savedFavourite{}
	err := c.tx.QueryRow(`
		SELECT fc.id, fc.project_id, fc.name, fc.created_at,
		       COALESCE(fa.appearance_type, 'icon'), COALESCE(fa.appearance_value, 'default')
		FROM favourite_collections fc
		LEFT JOIN favourite_appearances fa ON fa.favourite_collection_id = fc.id
		WHERE fc.id = ?`, in.ID,
	).Scan(&saved.Collection.ID, &saved.Collection.ProjectID, &saved.Collection.Name,
		&saved.Collection.CreatedAt,
		&saved.Appearance.AppearanceType, &saved.Appearance.AppearanceValue)
	if err != nil {
		return nil, fmt.Errorf("loading favourite collection: %w", err)
	}
	saved.Appearance.FavouriteCollectionID = saved.Collection.ID

	rows, err := c.tx.Query(
		`SELECT id, favourite_collection_id, http_request_id, user_sort_order, user_color
		 FROM favourite_items WHERE favourite_collection_id = ? ORDER BY id`, in.ID)
	if err != nil {
		return nil, fmt.Errorf("loading favourite items: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item savedItem
		if err := rows.Scan(&item.ID, &item.FavouriteCollectionID, &item.HTTPRequestID,
			&item.UserSortOrder, &item.UserColor); err != nil {
			return nil, err
		}
		saved.Items = append(saved.Items, item)
	}
	return saved, rows.Err()
}

func (c Delete) Run(input json.RawMessage) (any, error) {
	var in struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if in.ID == 0 {
		return nil, errors.New("favourite collection id is required")
	}
	_, err := c.tx.Exec(`DELETE FROM favourite_collections WHERE id = ?`, in.ID)
	return nil, err
}

// Undo re-inserts the collection, its appearance and its items.
func (c Delete) Undo(snapshot, _ json.RawMessage) error {
	var saved savedFavourite
	if err := json.Unmarshal(snapshot, &saved); err != nil {
		return err
	}
	col := saved.Collection
	if _, err := c.tx.Exec(
		`INSERT INTO favourite_collections (id, project_id, name, created_at) VALUES (?, ?, ?, ?)`,
		col.ID, col.ProjectID, col.Name, col.CreatedAt,
	); err != nil {
		return err
	}
	if saved.Appearance != nil {
		if _, err := c.tx.Exec(
			`INSERT INTO favourite_appearances (favourite_collection_id, appearance_type, appearance_value)
			 VALUES (?, ?, ?)`,
			saved.Appearance.FavouriteCollectionID, saved.Appearance.AppearanceType,
			saved.Appearance.AppearanceValue,
		); err != nil {
			return err
		}
	}
	for _, item := range saved.Items {
		if _, err := c.tx.Exec(
			`INSERT INTO favourite_items (id, favourite_collection_id, http_request_id, user_sort_order, user_color)
			 VALUES (?, ?, ?, ?, ?)`,
			item.ID, item.FavouriteCollectionID, item.HTTPRequestID, item.UserSortOrder, item.UserColor,
		); err != nil {
			return err
		}
	}
	return nil
}

func (Delete) Changes(input, _ json.RawMessage) []commands.RowChange {
	return []commands.RowChange{{Component: "favourite", IDs: commands.IDsOf(input, "id")}}
}
