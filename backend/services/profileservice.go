package services

import (
	"database/sql"
	"fmt"

	"snap-rq/backend/models"
)

// ProfileService provides CRUD operations for profiles.
type ProfileService struct {
	db *sql.DB
}

// NewProfileService returns a ProfileService backed by the given database.
func NewProfileService(db *sql.DB) *ProfileService {
	return &ProfileService{db: db}
}

// CreateProfile saves a new profile, seeds a default favourite collection for it,
// and returns the profile with its generated ID.
func (s *ProfileService) CreateProfile(profile models.Profile) (models.Profile, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return models.Profile{}, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.Exec("INSERT INTO profiles (name) VALUES (?)", profile.Name)
	if err != nil {
		return models.Profile{}, fmt.Errorf("creating profile: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return models.Profile{}, fmt.Errorf("getting last insert id: %w", err)
	}
	profile.ID = id

	_, err = tx.Exec(
		"INSERT INTO favourite_collections (profile_id, name) VALUES (?, ?)",
		profile.ID, "default",
	)
	if err != nil {
		return models.Profile{}, fmt.Errorf("creating default favourite collection: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return models.Profile{}, fmt.Errorf("committing transaction: %w", err)
	}

	return profile, nil
}

// GetProfile retrieves a single profile by ID.
func (s *ProfileService) GetProfile(id int64) (models.Profile, error) {
	var profile models.Profile
	row := s.db.QueryRow("SELECT id, name FROM profiles WHERE id = ?", id)
	err := row.Scan(&profile.ID, &profile.Name)
	if err != nil {
		if err == sql.ErrNoRows {
			return models.Profile{}, fmt.Errorf("profile not found")
		}
		return models.Profile{}, fmt.Errorf("getting profile: %w", err)
	}
	return profile, nil
}

// GetAllProfiles returns all profiles ordered by name.
func (s *ProfileService) GetAllProfiles() ([]models.Profile, error) {
	rows, err := s.db.Query("SELECT id, name FROM profiles ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("listing profiles: %w", err)
	}
	defer rows.Close()

	var profiles []models.Profile
	for rows.Next() {
		var profile models.Profile
		if err := rows.Scan(&profile.ID, &profile.Name); err != nil {
			return nil, fmt.Errorf("scanning profile: %w", err)
		}
		profiles = append(profiles, profile)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating profiles: %w", err)
	}

	return profiles, nil
}

// UpdateProfile updates an existing profile.
func (s *ProfileService) UpdateProfile(profile models.Profile) (models.Profile, error) {
	if profile.ID == 0 {
		return models.Profile{}, fmt.Errorf("profile id is required")
	}
	_, err := s.db.Exec("UPDATE profiles SET name = ? WHERE id = ?", profile.Name, profile.ID)
	if err != nil {
		return models.Profile{}, fmt.Errorf("updating profile: %w", err)
	}
	return profile, nil
}

// DeleteProfile removes a profile by ID.
func (s *ProfileService) DeleteProfile(id int64) error {
	_, err := s.db.Exec("DELETE FROM profiles WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("deleting profile: %w", err)
	}
	return nil
}
