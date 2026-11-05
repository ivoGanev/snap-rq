package favourites

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"snap-rq/backend/commands"
)

// set_color.go — set (or clear) the colour on many requests' memberships
// in one favourite collection. Snapshot saves the old colours; undo
// restores them.

type SetColor struct{ tx *sql.Tx }

func NewSetColor(tx *sql.Tx) commands.Command { return &SetColor{tx: tx} }

func (SetColor) Info() commands.Info {
	return commands.Info{FunctionID: "favourite.set_color", Label: "Set Favourite Colours"}
}

type oldColor struct {
	ID        int64   `json:"id"`
	UserColor *string `json:"user_color"`
}

func (c SetColor) Snapshot(input json.RawMessage) (any, error) {
	var in struct {
		FavouriteCollectionID int64   `json:"favourite_collection_id"`
		RequestIDs            []int64 `json:"request_ids"`
		Color                 *string `json:"color"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}

	ph := strings.TrimSuffix(strings.Repeat("?,", len(in.RequestIDs)), ",")
	args := []any{in.FavouriteCollectionID}
	for _, id := range in.RequestIDs {
		args = append(args, id)
	}
	rows, err := c.tx.Query(`
		SELECT id, user_color FROM favourite_items
		WHERE favourite_collection_id = ? AND http_request_id IN (`+ph+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("loading colours: %w", err)
	}
	defer rows.Close()

	var saved []oldColor
	for rows.Next() {
		var o oldColor
		if err := rows.Scan(&o.ID, &o.UserColor); err != nil {
			return nil, err
		}
		saved = append(saved, o)
	}
	return saved, rows.Err()
}

func (c SetColor) Run(input json.RawMessage) (any, error) {
	var in struct {
		FavouriteCollectionID int64   `json:"favourite_collection_id"`
		RequestIDs            []int64 `json:"request_ids"`
		Color                 *string `json:"color"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if len(in.RequestIDs) == 0 {
		return nil, nil
	}

	var value any
	if in.Color != nil && *in.Color != "" {
		value = *in.Color
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(in.RequestIDs)), ",")
	args := []any{value, in.FavouriteCollectionID}
	for _, id := range in.RequestIDs {
		args = append(args, id)
	}
	_, err := c.tx.Exec(`
		UPDATE favourite_items SET user_color = ?
		WHERE favourite_collection_id = ? AND http_request_id IN (`+ph+`)`, args...)
	return nil, err
}

func (c SetColor) Undo(snapshot, _ json.RawMessage) error {
	var saved []oldColor
	if err := json.Unmarshal(snapshot, &saved); err != nil {
		return err
	}
	for _, o := range saved {
		if _, err := c.tx.Exec(
			`UPDATE favourite_items SET user_color = ? WHERE id = ?`, o.UserColor, o.ID,
		); err != nil {
			return err
		}
	}
	return nil
}

func (SetColor) Changes(input, _ json.RawMessage) []commands.RowChange {
	return []commands.RowChange{{Component: "favourite", IDs: commands.IDsOf(input, "favourite_collection_id")}}
}
