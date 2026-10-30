package services

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"snap-rq/backend/commands"
)

// FavouriteDeleteCollectionCommandName is the public command name for
// deleting a favourite collection.
const FavouriteDeleteCollectionCommandName = "favourite_collection.delete"

// RegisterCommands makes this service's commands runnable and routes its
// mutations through the history.
func (s *FavouriteService) RegisterCommands(history *commands.History) error {
	s.history = history
	return history.Register(s.deleteCollectionCommand())
}

// deleteCollectionCommand removes a favourite collection together with its
// memberships and appearance. Undo puts all of it back as it was.
func (s *FavouriteService) deleteCollectionCommand() *commands.Command {
	return &commands.Command{
		Name:     FavouriteDeleteCollectionCommandName,
		Label:    "Delete Favourite",
		Undoable: true,
		Apply: func(tx *sql.Tx, rec *commands.Recorder, input json.RawMessage) (any, error) {
			var payload struct {
				ID int64 `json:"id"`
			}
			if err := json.Unmarshal(input, &payload); err != nil {
				return nil, err
			}
			if payload.ID == 0 {
				return nil, errors.New("favourite collection id is required")
			}

			itemIDs, err := selectIDsTx(tx,
				`SELECT id FROM favourite_items WHERE favourite_collection_id = ?`, payload.ID)
			if err != nil {
				return nil, fmt.Errorf("listing favourite items: %w", err)
			}
			appearanceIDs, err := selectIDsTx(tx,
				`SELECT id FROM favourite_appearances WHERE favourite_collection_id = ?`, payload.ID)
			if err != nil {
				return nil, fmt.Errorf("listing favourite appearance: %w", err)
			}

			// Parent table first: rows are put back in save order.
			if err := rec.SaveRows("favourite_collections", payload.ID); err != nil {
				return nil, err
			}
			if err := rec.SaveRows("favourite_items", itemIDs...); err != nil {
				return nil, err
			}
			if err := rec.SaveRows("favourite_appearances", appearanceIDs...); err != nil {
				return nil, err
			}

			if _, err := tx.Exec(
				`DELETE FROM favourite_collections WHERE id = ?`, payload.ID,
			); err != nil {
				return nil, fmt.Errorf("deleting favourite collection: %w", err)
			}

			rec.Touched("favourite_collection", "delete", payload.ID)
			return nil, nil
		},
	}
}
