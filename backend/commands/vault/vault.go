// Package vault holds the commands for the secret-values key: its status,
// the recovery key, restoring it, and starting over when it's lost. None is
// undoable. See docs/plans/secret-variables.md.
package vault

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"snap-rq/backend/backup"
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
	// SecretVariables counts variables marked secret: the locked banner only
	// shows when there are some to unlock.
	SecretVariables int `json:"secret_variables"`
	// RecoveryKey says whether the user has seen the current recovery key:
	// unseen (never), seen (this key), changed (an older key: announce it).
	RecoveryKey RecoveryKeyState `json:"recovery_key"`
}

// RecoveryKeyState is whether the user has seen the current recovery key.
type RecoveryKeyState string

const (
	RecoveryKeyUnseen  RecoveryKeyState = "unseen"
	RecoveryKeySeen    RecoveryKeyState = "seen"
	RecoveryKeyChanged RecoveryKeyState = "changed"
)

// seenSetting holds the fingerprint of the recovery key the user was last
// shown (see secrets.Vault.Fingerprint), as a JSON string in app_settings.
const seenSetting = "secrets_recovery_key_seen"

// recoveryKeyState compares the fingerprint of the key the user saw with the
// current key. With nothing seen yet it doesn't touch the key at all, so a
// fresh install reads nothing from the keyring at startup. A locked key can't
// be compared and counts as seen: the locked banner handles that case.
func recoveryKeyState(tx *sql.Tx) (RecoveryKeyState, error) {
	var raw, seen string
	err := tx.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, seenSetting).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return RecoveryKeyUnseen, nil
	}
	if err != nil {
		return "", err
	}
	if err := json.Unmarshal([]byte(raw), &seen); err != nil || seen == "" {
		return RecoveryKeyUnseen, nil
	}
	current, err := secrets.Default().Fingerprint(tx)
	if err != nil {
		return RecoveryKeySeen, nil
	}
	if current != seen {
		return RecoveryKeyChanged, nil
	}
	return RecoveryKeySeen, nil
}

// RecoveryKeyReminder is the startup check: should the app announce a new
// recovery key? Separate from secrets.status so starting the app doesn't
// unlock the key unless a recovery key was seen before.
type RecoveryKeyReminder struct{ tx *sql.Tx }

func NewRecoveryKeyReminder(tx *sql.Tx) commands.Command { return &RecoveryKeyReminder{tx: tx} }

func (RecoveryKeyReminder) Info() commands.Info {
	return commands.Info{FunctionID: "secrets.recovery_key_state", Label: "Recovery Key State", ReadOnly: true}
}

func (c RecoveryKeyReminder) Run(json.RawMessage) (any, error) {
	return recoveryKeyState(c.tx)
}

// MarkRecoveryKeySeen records that the user was shown the current recovery
// key (its fingerprint), so it isn't announced again.
type MarkRecoveryKeySeen struct{ tx *sql.Tx }

func NewMarkRecoveryKeySeen(tx *sql.Tx) commands.Command { return &MarkRecoveryKeySeen{tx: tx} }

func (MarkRecoveryKeySeen) Info() commands.Info {
	return commands.Info{FunctionID: "secrets.mark_recovery_key_seen", Label: "Mark Recovery Key Seen"}
}

func (c MarkRecoveryKeySeen) Run(json.RawMessage) (any, error) {
	fingerprint, err := secrets.Default().Fingerprint(c.tx)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(fingerprint)
	_, err = c.tx.Exec(`INSERT INTO app_settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, seenSetting, string(raw))
	return nil, err
}

func (c Status) Run(json.RawMessage) (any, error) {
	vault := secrets.Default()
	state, err := vault.Status(c.tx)
	if err != nil {
		return nil, err
	}
	result := StatusResult{State: state, Store: vault.StoreKind()}
	if err := c.tx.QueryRow(`SELECT COUNT(*) FROM environment_variables WHERE secret = 1`).
		Scan(&result.SecretVariables); err != nil {
		return nil, err
	}
	if result.RecoveryKey, err = recoveryKeyState(c.tx); err != nil {
		return nil, err
	}
	return result, nil
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

// SaveRecoveryKey writes the recovery key to a file the user picked in a save
// dialog, readable only by them. Never offered to MCP.
type SaveRecoveryKey struct{ tx *sql.Tx }

func NewSaveRecoveryKey(tx *sql.Tx) commands.Command { return &SaveRecoveryKey{tx: tx} }

func (SaveRecoveryKey) Info() commands.Info {
	return commands.Info{FunctionID: "secrets.save_recovery_key", Label: "Save Recovery Key", ReadOnly: true}
}

func (c SaveRecoveryKey) Run(input json.RawMessage) (any, error) {
	var in struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.FilePath) == "" {
		return nil, errors.New("file path is required")
	}
	key, err := secrets.Default().RecoveryKey(c.tx)
	if err != nil {
		return nil, err
	}
	text := "SnapRQ recovery key\n\n" + key + "\n\n" +
		"Keep this somewhere safe, such as a password manager. It unlocks your secret\n" +
		"variables if this computer's credential store loses SnapRQ's key, or on a new computer.\n"
	if err := os.WriteFile(in.FilePath, []byte(text), 0o600); err != nil {
		return nil, fmt.Errorf("saving the recovery key: %w", err)
	}
	return nil, nil
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
// (names and the secret flags stay) and every cookie (also encrypted with the
// key), and forgets the key check, so the next secret works with the key on
// this computer.
type Reset struct{ tx *sql.Tx }

func NewReset(tx *sql.Tx) commands.Command { return &Reset{tx: tx} }

func (Reset) Info() commands.Info {
	return commands.Info{FunctionID: "secrets.reset", Label: "Clear Secret Values"}
}

func (c Reset) Run(json.RawMessage) (any, error) {
	if _, err := c.tx.Exec(`UPDATE environment_variables SET value = '' WHERE secret = 1`); err != nil {
		return nil, err
	}
	if _, err := c.tx.Exec(`DELETE FROM cookies`); err != nil {
		return nil, err
	}
	return nil, secrets.Default().Forget(c.tx)
}

func (Reset) Changes(_, _ json.RawMessage) []commands.RowChange {
	return []commands.RowChange{{Component: "variable"}, {Component: "cookie"}}
}

// BackupBefore: clearing secret values can't be undone, so it always takes a
// backup first (the values in it stay encrypted with the old key).
func (Reset) BackupBefore(json.RawMessage) (commands.BackupNeed, bool, error) {
	return commands.BackupNeed{
		Trigger: string(backup.TriggerSecretsReset),
		Reason:  "before Clear secret values",
	}, true, nil
}
