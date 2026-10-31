package runnables

import (
	"encoding/json"
	"errors"
	"fmt"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
	"snap-rq/backend/store"
)

// CreateFavouriteCollection saves a favourite collection with its default
// appearance. Undo removes both rows.
type CreateFavouriteCollection struct{}

func (CreateFavouriteCollection) Info() commands.Info {
	return commands.Info{
		FunctionID:      "favourite.create",
		Label:           "Create Favourite",
		UndoOperationID: "favourite.delete",
	}
}

func (CreateFavouriteCollection) Run(db store.Session, input json.RawMessage) (any, error) {
	var payload struct {
		ProjectID int64  `json:"project_id"`
		Name      string `json:"name"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, err
	}
	if payload.ProjectID == 0 {
		return nil, errors.New("project id is required")
	}

	id, err := db.Insert("favourite_collections", map[string]any{
		"project_id": payload.ProjectID,
		"name":       payload.Name,
	})
	if err != nil {
		return nil, err
	}
	defaultAppearance := models.DefaultFavouriteAppearance()
	if _, err := db.Insert("favourite_appearances", map[string]any{
		"favourite_collection_id": id,
		"appearance_type":         defaultAppearance.AppearanceType,
		"appearance_value":        defaultAppearance.AppearanceValue,
	}); err != nil {
		return nil, err
	}

	return map[string]any{"id": id, "project_id": payload.ProjectID, "name": payload.Name}, nil
}

// DeleteFavouriteCollection removes a favourite collection together with
// its memberships and appearance (the cascade tree is photographed from
// the manifest). The requests themselves are not touched. Undo puts it
// all back.
type DeleteFavouriteCollection struct{}

func (DeleteFavouriteCollection) Info() commands.Info {
	return commands.Info{
		FunctionID:      "favourite.delete",
		Label:           "Delete Favourite",
		UndoOperationID: "favourite.create",
	}
}

func (DeleteFavouriteCollection) Run(db store.Session, input json.RawMessage) (any, error) {
	var payload struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, err
	}
	if payload.ID == 0 {
		return nil, errors.New("favourite collection id is required")
	}
	return nil, db.Delete("favourite_collections", payload.ID)
}

var _ = fmt.Sprintf
