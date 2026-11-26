package environments

import (
	"database/sql"
	"encoding/json"
	"errors"

	"snap-rq/backend/commands"
)

// select.go — the environment a project uses. Sending, trying a script and
// copying as cURL read it from here, so the app and MCP use the same one.

type Select struct{ tx *sql.Tx }

func NewSelect(tx *sql.Tx) commands.Command { return &Select{tx: tx} }

func (Select) Info() commands.Info {
	return commands.Info{FunctionID: "environment.select", Label: "Select Environment"}
}

func (c Select) Run(input json.RawMessage) (any, error) {
	var in struct {
		ProjectID int64 `json:"project_id"`
		// EnvironmentID 0 selects none.
		EnvironmentID int64 `json:"environment_id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if in.EnvironmentID != 0 {
		var projectID int64
		err := c.tx.QueryRow(`SELECT project_id FROM environments WHERE id = ?`, in.EnvironmentID).Scan(&projectID)
		if errors.Is(err, sql.ErrNoRows) || projectID != in.ProjectID {
			return nil, errors.New("that environment isn't in this project")
		}
		if err != nil {
			return nil, err
		}
	}
	result, err := c.tx.Exec(`UPDATE projects SET selected_environment_id = NULLIF(?, 0) WHERE id = ?`,
		in.EnvironmentID, in.ProjectID)
	if err != nil {
		return nil, err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return nil, errors.New("project not found")
	}
	return nil, nil
}

func (Select) Changes(input, _ json.RawMessage) []commands.RowChange {
	return []commands.RowChange{{Component: "project", IDs: commands.IDsOf(input, "project_id")}}
}
