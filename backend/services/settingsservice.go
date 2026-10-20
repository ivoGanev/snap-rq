package services

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"snap-rq/backend/models"
)

const validateSSLCertificatesKey = "validate_ssl_certificates"

// SettingsService reads and writes general application settings.
type SettingsService struct {
	db *sql.DB
}

// NewSettingsService returns a SettingsService backed by the given database.
func NewSettingsService(db *sql.DB) *SettingsService {
	return &SettingsService{db: db}
}

// GetSettings returns the current application settings. Missing values fall
// back to secure defaults.
func (s *SettingsService) GetSettings() (models.AppSettings, error) {
	settings := defaultAppSettings()

	var raw string
	err := s.db.QueryRow(
		`SELECT value FROM app_settings WHERE key = ?`,
		validateSSLCertificatesKey,
	).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return settings, nil
	}
	if err != nil {
		return settings, fmt.Errorf("reading app settings: %w", err)
	}

	if err := json.Unmarshal([]byte(raw), &settings.ValidateSSLCertificates); err != nil {
		return defaultAppSettings(), fmt.Errorf("parsing %s setting: %w", validateSSLCertificatesKey, err)
	}

	return settings, nil
}

// UpdateSettings persists the provided application settings and returns them.
func (s *SettingsService) UpdateSettings(settings models.AppSettings) (models.AppSettings, error) {
	raw, err := json.Marshal(settings.ValidateSSLCertificates)
	if err != nil {
		return settings, fmt.Errorf("encoding %s setting: %w", validateSSLCertificatesKey, err)
	}

	_, err = s.db.Exec(
		`INSERT INTO app_settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		validateSSLCertificatesKey,
		string(raw),
	)
	if err != nil {
		return settings, fmt.Errorf("saving app settings: %w", err)
	}

	return settings, nil
}

// ValidateSSLCertificates reports whether HTTPS certificates should be
// validated. Errors fall back to the secure default.
func (s *SettingsService) ValidateSSLCertificates() bool {
	settings, err := s.GetSettings()
	if err != nil {
		return true
	}
	return settings.ValidateSSLCertificates
}

func defaultAppSettings() models.AppSettings {
	return models.AppSettings{ValidateSSLCertificates: true}
}
