// Package vault holds the commands for the secret-values key: its status,
// the recovery key, restoring it, and starting over when it's lost. None is
// undoable. See docs/plans/secret-variables.md.
package vault

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"snap-rq/backend/commands"
	"snap-rq/backend/secrets"
)

// Status says whether secret values can be used, and where the key lives.
type Status struct{ tx *sql.Tx }

func NewStatus(tx *sql.Tx) commands.Command { return &Status{tx: tx} }

func (Status) Info() commands.Info {
	return commands.Info{FunctionID: "secrets.status", Label: "Secrets Status", ReadOnly: true}
}

// StatusResult is what secrets.status returns.
type StatusResult struct {
	State secrets.State `json:"state"` // none | unlocked | locked
	Store string        `json:"store"` // keyring | file | memory
}

func (c Status) Run(json.RawMessage) (any, error) {
	vault := secrets.Default()
	state, err := vault.Status(c.tx)
	if err != nil {
		return nil, err
	}
	return StatusResult{State: state, Store: vault.StoreKind()}, nil
}

// RecoveryKey returns the key as SNRQ-XXXX-…, to copy or save.
type RecoveryKey struct{ tx *sql.Tx }

func NewRecoveryKey(tx *sql.Tx) commands.Command { return &RecoveryKey{tx: tx} }

func (RecoveryKey) Info() commands.Info {
	return commands.Info{FunctionID: "secrets.recovery_key", Label: "Show Recovery Key", ReadOnly: true}
}

func (c RecoveryKey) Run(json.RawMessage) (any, error) {
	return secrets.Default().RecoveryKey(c.tx)
}

// Restore puts a recovery key back, unlocking the secret values.
type Restore struct{ tx *sql.Tx }

func NewRestore(tx *sql.Tx) commands.Command { return &Restore{tx: tx} }

func (Restore) Info() commands.Info {
	return commands.Info{FunctionID: "secrets.restore", Label: "Restore Recovery Key"}
}

func (c Restore) Run(input json.RawMessage) (any, error) {
	var in struct {
		RecoveryKey string `json:"recovery_key"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.RecoveryKey) == "" {
		return nil, errors.New("recovery key is required")
	}
	return nil, secrets.Default().Restore(c.tx, in.RecoveryKey)
}

func (Restore) Changes(_, _ json.RawMessage) []commands.RowChange {
	return []commands.RowChange{{Component: "variable"}}
}

// Reset is "start over" for locked secrets: it clears every secret value
// (names and the secret flags stay) and forgets the key check, so the next
// secret works with the key on this computer.
type Reset struct{ tx *sql.Tx }

func NewReset(tx *sql.Tx) commands.Command { return &Reset{tx: tx} }

func (Reset) Info() commands.Info {
	return commands.Info{FunctionID: "secrets.reset", Label: "Clear Secret Values"}
}

func (c Reset) Run(json.RawMessage) (any, error) {
	if _, err := c.tx.Exec(`UPDATE environment_variables SET value = '' WHERE secret = 1`); err != nil {
		return nil, err
	}
	return nil, secrets.Default().Forget(c.tx)
}

func (Reset) Changes(_, _ json.RawMessage) []commands.RowChange {
	return []commands.RowChange{{Component: "variable"}}
}
