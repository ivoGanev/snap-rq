// Package icons holds the icon registry maintenance command.
package icons

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"snap-rq/backend/commands"
)

// cleanup.go — reset appearance rows that reference icon ids no longer in
// the registry. Upkeep, not user intent: not undoable.

type Cleanup struct{ tx *sql.Tx }

func NewCleanup(tx *sql.Tx) commands.Command { return &Cleanup{tx: tx} }

func (Cleanup) Info() commands.Info {
	return commands.Info{FunctionID: "icon.cleanup", Label: "Clean Up Icon References"}
}

func (c Cleanup) Run(input json.RawMessage) (any, error) {
	var in struct {
		ValidIDs  []string `json:"valid_ids"`
		DefaultID string   `json:"default_id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if len(in.ValidIDs) == 0 {
		return nil, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(in.ValidIDs)), ",")
	args := make([]any, 0, len(in.ValidIDs)+1)
	for _, id := range in.ValidIDs {
		args = append(args, id)
	}
	args = append(args, in.DefaultID)

	for _, table := range []string{"collection_appearances", "favourite_appearances", "tag_appearances"} {
		if _, err := c.tx.Exec(
			fmt.Sprintf(`UPDATE %s SET appearance_value = ?
				WHERE appearance_type = 'icon' AND appearance_value NOT IN (%s)`,
				table, placeholders),
			args...,
		); err != nil {
			return nil, fmt.Errorf("cleaning %s: %w", table, err)
		}
	}
	return nil, nil
}
