package favourites

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// update.go — rename a favourite collection. Snapshot saves the old name.

type Update struct{ tx *sql.Tx }

func NewUpdate(tx *sql.Tx) commands.Command { return &Update{tx: tx} }

func (Update) Info() commands.Info {
	return commands.Info{FunctionID: "favourite.update", Label: "Rename Favourite"}
}

func (c Update) Snapshot(input json.RawMessage) (any, error) {
	var in struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	var oldName string
	if err := c.tx.QueryRow(
		`SELECT name FROM favourite_collections WHERE id = ?`, in.ID,
	).Scan(&oldName); err != nil {
		return nil, fmt.Errorf("loading favourite collection: %w", err)
	}
	return map[string]any{"id": in.ID, "name": oldName}, nil
}

func (c Update) Run(input json.RawMessage) (any, error) {
	var col models.FavouriteCollection
	if err := json.Unmarshal(input, &col); err != nil {
		return nil, err
	}
	if col.ID == 0 {
		return nil, fmt.Errorf("favourite collection id is required")
	}
	if _, err := c.tx.Exec(
		`UPDATE favourite_collections SET name = ? WHERE id = ?`, col.Name, col.ID,
	); err != nil {
		return nil, fmt.Errorf("updating favourite collection: %w", err)
	}
	return col, nil
}

func (c Update) Undo(snapshot, _ json.RawMessage) error {
	var old struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(snapshot, &old); err != nil {
		return err
	}
	_, err := c.tx.Exec(
		`UPDATE favourite_collections SET name = ? WHERE id = ?`, old.Name, old.ID)
	return err
}

func (Update) Affected(_, result json.RawMessage) []commands.Affected {
	return []commands.Affected{{Component: "favourite", IDs: commands.IDsOf(result, "id")}}
}
