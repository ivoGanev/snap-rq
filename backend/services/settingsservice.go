package services

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"snap-rq/backend/models"
)

const (
	validateSSLCertificatesKey = "validate_ssl_certificates"
	requestTimeoutSecondsKey   = "request_timeout_seconds"

	// Request timeout bounds and default, in seconds.
	DefaultRequestTimeoutSeconds = 30
	MinRequestTimeoutSeconds     = 1
	MaxRequestTimeoutSeconds     = 99999
)

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

	if err := s.readSetting(validateSSLCertificatesKey, &settings.ValidateSSLCertificates); err != nil {
		return defaultAppSettings(), err
	}
	if err := s.readSetting(requestTimeoutSecondsKey, &settings.RequestTimeoutSeconds); err != nil {
		return defaultAppSettings(), err
	}
	if settings.RequestTimeoutSeconds < MinRequestTimeoutSeconds || settings.RequestTimeoutSeconds > MaxRequestTimeoutSeconds {
		settings.RequestTimeoutSeconds = DefaultRequestTimeoutSeconds
	}

	return settings, nil
}

// UpdateSettings persists the provided application settings and returns them.
// A zero request timeout is treated as the default; values outside the
// allowed range are rejected.
func (s *SettingsService) UpdateSettings(settings models.AppSettings) (models.AppSettings, error) {
	if settings.RequestTimeoutSeconds == 0 {
		settings.RequestTimeoutSeconds = DefaultRequestTimeoutSeconds
	}
	if settings.RequestTimeoutSeconds < MinRequestTimeoutSeconds || settings.RequestTimeoutSeconds > MaxRequestTimeoutSeconds {
		return settings, fmt.Errorf(
			"request timeout must be between %d and %d seconds",
			MinRequestTimeoutSeconds, MaxRequestTimeoutSeconds,
		)
	}

	if err := s.writeSetting(validateSSLCertificatesKey, settings.ValidateSSLCertificates); err != nil {
		return settings, err
	}
	if err := s.writeSetting(requestTimeoutSecondsKey, settings.RequestTimeoutSeconds); err != nil {
		return settings, err
	}

	return settings, nil
}

func (s *SettingsService) readSetting(key string, dest any) error {
	var raw string
	err := s.db.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading app settings: %w", err)
	}
	if err := json.Unmarshal([]byte(raw), dest); err != nil {
		return fmt.Errorf("parsing %s setting: %w", key, err)
	}
	return nil
}

func (s *SettingsService) writeSetting(key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encoding %s setting: %w", key, err)
	}
	_, err = s.db.Exec(
		`INSERT INTO app_settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key,
		string(raw),
	)
	if err != nil {
		return fmt.Errorf("saving app settings: %w", err)
	}
	return nil
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

// RequestTimeout returns the configured HTTP request deadline. Errors fall
// back to the default.
func (s *SettingsService) RequestTimeout() time.Duration {
	settings, err := s.GetSettings()
	if err != nil {
		return DefaultRequestTimeoutSeconds * time.Second
	}
	return time.Duration(settings.RequestTimeoutSeconds) * time.Second
}

func defaultAppSettings() models.AppSettings {
	return models.AppSettings{
		ValidateSSLCertificates: true,
		RequestTimeoutSeconds:   DefaultRequestTimeoutSeconds,
	}
}
