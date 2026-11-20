package backup

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/adrg/xdg"

	"snap-rq/backend/secrets"
)

// settings.go — the user's backup settings, one JSON value in app_settings.

const settingsKey = "backup_settings"

// Trigger names an operation that makes a backup before it runs.
type Trigger string

const (
	TriggerDeleteProject     Trigger = "delete_project"
	TriggerDeleteCollection  Trigger = "delete_collection"
	TriggerDeleteEnvironment Trigger = "delete_environment"
	TriggerDeleteRequests    Trigger = "delete_requests"
	TriggerPermanentDelete   Trigger = "permanent_delete"
	TriggerOpenAPIImport     Trigger = "openapi_import"
	TriggerSecretsReset      Trigger = "secrets_reset"
	TriggerAppUpdate         Trigger = "app_update"
)

// AllTriggers lists every trigger, in the order Settings shows them.
var AllTriggers = []Trigger{
	TriggerDeleteProject, TriggerDeleteCollection, TriggerDeleteEnvironment, TriggerDeleteRequests,
	TriggerPermanentDelete, TriggerOpenAPIImport, TriggerSecretsReset, TriggerAppUpdate,
}

// Settings are the backup preferences.
type Settings struct {
	// Enabled switches every automatic backup at once.
	Enabled bool `json:"enabled"`
	// Triggers switches each automatic backup; missing means on.
	Triggers map[Trigger]bool `json:"triggers"`
	// RequestDeleteThreshold is how many requests deleted at once count as
	// risky.
	RequestDeleteThreshold int `json:"request_delete_threshold"`
	// Folder is where backups go; empty means DefaultFolder.
	Folder string `json:"folder"`
	// KeepAutomatic and KeepDays limit automatic backups; manual ones stay.
	KeepAutomatic int `json:"keep_automatic"`
	KeepDays      int `json:"keep_days"`
	// HasPassword says a backup password is set (it is never returned).
	HasPassword bool `json:"has_password"`
	// PasswordSealed is the backup password, encrypted by the secrets vault:
	// automatic backups need it without asking.
	PasswordSealed string `json:"password_sealed,omitempty"`
}

// DefaultFolder is Documents/SnapRQ Backups.
func DefaultFolder() string { return filepath.Join(xdg.UserDirs.Documents, "SnapRQ Backups") }

// DefaultSettings: automatic backups on, every trigger on.
func DefaultSettings() Settings {
	return Settings{
		Enabled:                true,
		Triggers:               map[Trigger]bool{},
		RequestDeleteThreshold: 10,
		KeepAutomatic:          20,
		KeepDays:               90,
	}
}

// LoadSettings reads the settings, filling in defaults.
func LoadSettings(q secrets.Querier) (Settings, error) {
	s := DefaultSettings()
	var raw string
	err := q.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, settingsKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return DefaultSettings(), nil
	}
	if s.Triggers == nil {
		s.Triggers = map[Trigger]bool{}
	}
	s.HasPassword = s.PasswordSealed != ""
	return s.withLimits(), nil
}

// SaveSettings stores the settings as given (PasswordSealed included).
func SaveSettings(q secrets.Querier, s Settings) error {
	s = s.withLimits()
	s.HasPassword = s.PasswordSealed != ""
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_, err = q.Exec(`INSERT INTO app_settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, settingsKey, string(raw))
	return err
}

// SetPassword seals a new backup password, or clears it with "".
func (s *Settings) SetPassword(q secrets.Querier, password string) error {
	if password == "" {
		s.PasswordSealed = ""
		return nil
	}
	sealed, err := secrets.Default().Encrypt(q, password)
	if err != nil {
		return fmt.Errorf("storing the backup password: %w", err)
	}
	s.PasswordSealed = sealed
	return nil
}

// Public is the settings as the frontend sees them: no sealed password,
// the folder resolved.
func (s Settings) Public() Settings {
	s.PasswordSealed = ""
	s.Folder = s.ResolvedFolder()
	return s
}

// ResolvedFolder is Folder, or the default.
func (s Settings) ResolvedFolder() string {
	if s.Folder == "" {
		return DefaultFolder()
	}
	return s.Folder
}

// Wants reports whether an automatic backup should happen for a trigger.
func (s Settings) Wants(t Trigger) bool {
	if !s.Enabled {
		return false
	}
	on, set := s.Triggers[t]
	return !set || on
}

func (s Settings) withLimits() Settings {
	if s.RequestDeleteThreshold < 1 {
		s.RequestDeleteThreshold = 10
	}
	if s.KeepAutomatic < 1 {
		s.KeepAutomatic = 20
	}
	if s.KeepDays < 1 {
		s.KeepDays = 90
	}
	return s
}
