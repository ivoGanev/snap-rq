package variables

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
	"snap-rq/backend/secrets"
)

// secret.go — the secret side of variables: how a value is stored, and the
// two commands that only make sense for secrets (set_secret, reveal).

// storedValue is what goes into the value column: the encrypted form for a
// secret, the plain value otherwise. An empty secret stays empty, so "no
// value yet" needs no key.
func storedValue(tx *sql.Tx, secret bool, value string) (string, error) {
	if !secret || value == "" {
		return value, nil
	}
	return secrets.Default().Encrypt(tx, value)
}

// forFrontend hides a secret's value: the frontend only learns that it has one.
func forFrontend(v models.EnvironmentVariable) models.EnvironmentVariable {
	v.HasValue = v.Value != ""
	if v.Secret {
		v.Value = ""
	}
	return v
}

// SetSecret turns a variable into a secret (encrypting its value) or back
// into a plain one (decrypting it). Not undoable, like variable.update: an
// undo entry would have to hold the plain value.
type SetSecret struct{ tx *sql.Tx }

func NewSetSecret(tx *sql.Tx) commands.Command { return &SetSecret{tx: tx} }

func (SetSecret) Info() commands.Info {
	return commands.Info{FunctionID: "variable.set_secret", Label: "Set Variable Secret"}
}

func (c SetSecret) Run(input json.RawMessage) (any, error) {
	var in struct {
		ID     int64 `json:"id"`
		Secret bool  `json:"secret"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	v, err := load(c.tx, in.ID)
	if err != nil {
		return nil, err
	}
	if v.Secret == in.Secret {
		return forFrontend(v), nil
	}

	value := v.Value
	switch {
	case in.Secret:
		if value, err = storedValue(c.tx, true, v.Value); err != nil {
			return nil, err
		}
	case v.Value != "":
		if value, err = secrets.Default().Decrypt(c.tx, v.Value); err != nil {
			return nil, err
		}
	}
	if _, err := c.tx.Exec(`UPDATE environment_variables SET value = ?, secret = ? WHERE id = ?`,
		value, in.Secret, in.ID); err != nil {
		return nil, err
	}
	v.Value, v.Secret = value, in.Secret
	return forFrontend(v), nil
}

func (SetSecret) Changes(input, _ json.RawMessage) []commands.RowChange {
	return []commands.RowChange{{Component: "variable", IDs: commands.IDsOf(input, "id")}}
}

// Reveal returns one secret's value, for the eye button in the editor.
type Reveal struct{ tx *sql.Tx }

func NewReveal(tx *sql.Tx) commands.Command { return &Reveal{tx: tx} }

func (Reveal) Info() commands.Info {
	return commands.Info{FunctionID: "variable.reveal", Label: "Reveal Variable", ReadOnly: true}
}

func (c Reveal) Run(input json.RawMessage) (any, error) {
	var in struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	v, err := load(c.tx, in.ID)
	if err != nil {
		return nil, err
	}
	if !v.Secret || v.Value == "" {
		return v.Value, nil
	}
	return secrets.Default().Decrypt(c.tx, v.Value)
}

// load reads one variable row as stored (a secret's value still encrypted).
func load(tx *sql.Tx, id int64) (models.EnvironmentVariable, error) {
	if id == 0 {
		return models.EnvironmentVariable{}, errors.New("variable id is required")
	}
	var v models.EnvironmentVariable
	err := tx.QueryRow(
		`SELECT id, environment_id, key, value, secret FROM environment_variables WHERE id = ?`, id,
	).Scan(&v.ID, &v.EnvironmentID, &v.Key, &v.Value, &v.Secret)
	if errors.Is(err, sql.ErrNoRows) {
		return v, fmt.Errorf("variable %d not found", id)
	}
	return v, err
}

// nameTaken refuses a name another variable of the environment already has:
// two variables with one name would make {{name}} ambiguous. A new row is
// unnamed until typed into, so empty names are exempt.
func nameTaken(tx *sql.Tx, environmentID int64, key string, exceptID int64) error {
	if key == "" {
		return nil
	}
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM environment_variables
		WHERE environment_id = ? AND key = ? AND id != ?`, environmentID, key, exceptID).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("A variable named %s already exists in this environment.", key)
	}
	return nil
}
