// Package cookiejar holds the cookie jar commands, one operation per file.
// shared.go holds only the helpers several files here use.
package cookiejar

import (
	"database/sql"
	"errors"
	"strings"

	"snap-rq/backend/models"
)

const cookieColumns = `id, project_id, domain, host_only, path, name, value,
	expires_at, secure, http_only, same_site, created_at`

type scanner interface{ Scan(dest ...any) error }

func scanCookie(s scanner) (models.Cookie, error) {
	var c models.Cookie
	err := s.Scan(&c.ID, &c.ProjectID, &c.Domain, &c.HostOnly, &c.Path, &c.Name, &c.Value,
		&c.ExpiresAt, &c.Secure, &c.HTTPOnly, &c.SameSite, &c.CreatedAt)
	return c, err
}

// loadCookie reads one cookie by id.
func loadCookie(tx *sql.Tx, id int64) (models.Cookie, error) {
	c, err := scanCookie(tx.QueryRow(`SELECT `+cookieColumns+` FROM cookies WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return models.Cookie{}, errors.New("cookie not found")
	}
	return c, err
}

// insertCookie writes a cookie row; a non-zero ID keeps that id (undo).
func insertCookie(tx *sql.Tx, c models.Cookie) (int64, error) {
	if c.ID != 0 {
		_, err := tx.Exec(`INSERT INTO cookies (`+cookieColumns+`)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			c.ID, c.ProjectID, c.Domain, c.HostOnly, c.Path, c.Name, c.Value,
			c.ExpiresAt, c.Secure, c.HTTPOnly, c.SameSite, c.CreatedAt)
		return c.ID, err
	}
	res, err := tx.Exec(`INSERT INTO cookies
		(project_id, domain, host_only, path, name, value, expires_at, secure, http_only, same_site, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ProjectID, c.Domain, c.HostOnly, c.Path, c.Name, c.Value,
		c.ExpiresAt, c.Secure, c.HTTPOnly, c.SameSite, c.CreatedAt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// normalize trims and defaults the fields a person typed.
func normalize(c *models.Cookie) error {
	c.Domain = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(c.Domain), "."))
	c.Name = strings.TrimSpace(c.Name)
	c.Path = strings.TrimSpace(c.Path)
	if c.Path == "" {
		c.Path = "/"
	}
	if c.Path[0] != '/' {
		return errors.New("path must start with /")
	}
	if c.Domain == "" {
		return errors.New("domain is required")
	}
	if c.Name == "" {
		return errors.New("name is required")
	}
	switch c.SameSite {
	case "", "Lax", "Strict", "None":
	default:
		return errors.New("same site must be Lax, Strict or None")
	}
	return nil
}
