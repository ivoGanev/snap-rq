package services

import (
	"encoding/json"
	"errors"
	"fmt"

	"snap-rq/backend/commands"
)

// FavouriteDeleteCollectionCommandName is the public command name for
// deleting a favourite collection (see specs/command-framework.md).
const FavouriteDeleteCollectionCommandName = "favourite_collection.delete"

// RegisterCommands registers this service's commands on the bus and routes
// this service's mutations through it.
func (s *FavouriteService) RegisterCommands(bus *commands.Bus) error {
	s.bus = bus
	return bus.Register(s.deleteCollectionCommand())
}

// deleteCollectionCommand removes a favourite collection and its memberships
// as one undoable entry. Undo restores the collection, its appearance and
// every favourite_items row under their original ids.
func (s *FavouriteService) deleteCollectionCommand() *commands.Command {
	return &commands.Command{
		Name:     FavouriteDeleteCollectionCommandName,
		Label:    "Delete Favourite",
		Category: commands.Undoable,
		Handler: func(ctx *commands.Context, payload json.RawMessage) (any, error) {
			var pl struct {
				ID int64 `json:"id"`
			}
			if err := json.Unmarshal(payload, &pl); err != nil {
				return nil, err
			}
			if pl.ID == 0 {
				return nil, errors.New("favourite collection id is required")
			}

			itemIDs, err := selectIDsTx(ctx.Tx,
				`SELECT id FROM favourite_items WHERE favourite_collection_id = ?`, pl.ID)
			if err != nil {
				return nil, fmt.Errorf("listing favourite items: %w", err)
			}
			appearanceIDs, err := selectIDsTx(ctx.Tx,
				`SELECT id FROM favourite_appearances WHERE favourite_collection_id = ?`, pl.ID)
			if err != nil {
				return nil, fmt.Errorf("listing favourite appearance: %w", err)
			}

			// Parent table first: capture order becomes restore order.
			if err := ctx.Snapshot("favourite_collections", pl.ID); err != nil {
				return nil, err
			}
			if err := ctx.Snapshot("favourite_items", itemIDs...); err != nil {
				return nil, err
			}
			if err := ctx.Snapshot("favourite_appearances", appearanceIDs...); err != nil {
				return nil, err
			}

			if _, err := ctx.Tx.Exec(
				`DELETE FROM favourite_collections WHERE id = ?`, pl.ID,
			); err != nil {
				return nil, fmt.Errorf("deleting favourite collection: %w", err)
			}

			ctx.Affected("favourite_collection", "delete", pl.ID)
			return nil, nil
		},
	}
}
