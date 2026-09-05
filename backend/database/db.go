package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
	_ "modernc.org/sqlite"
)

// Open opens the SQLite database stored in the user's application data
// directory, runs migrations, and ensures default collections exist.
func Open() (*sql.DB, error) {
	dbPath := filepath.Join(xdg.DataHome, "snap-rq-wails-v3", "app.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("creating data directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return nil, fmt.Errorf("enabling foreign keys: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	if err := migrate(db); err != nil {
		return nil, fmt.Errorf("migrating database: %w", err)
	}

	if err := ensureDefaultCollection(db); err != nil {
		return nil, fmt.Errorf("ensuring default collection: %w", err)
	}

	if err := ensureDefaultFavouriteCollections(db); err != nil {
		return nil, fmt.Errorf("ensuring default favourite collections: %w", err)
	}

	return db, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS profiles (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS projects (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			profile_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			FOREIGN KEY (profile_id) REFERENCES profiles(id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS idx_projects_profile_id ON projects(profile_id);

		CREATE TABLE IF NOT EXISTS collections (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS idx_collections_project_id ON collections(project_id);

		CREATE TABLE IF NOT EXISTS http_requests (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			collection_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			url TEXT NOT NULL,
			method TEXT NOT NULL,
			body TEXT,
			request_headers TEXT,
			status_code INTEGER NOT NULL DEFAULT 0,
			response_id INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY (collection_id) REFERENCES collections(id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS idx_http_requests_collection_id ON http_requests(collection_id);
		CREATE INDEX IF NOT EXISTS idx_http_requests_name ON http_requests(name);

		CREATE TABLE IF NOT EXISTS responses (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			request_id INTEGER NOT NULL,
			headers TEXT,
			status_code INTEGER NOT NULL DEFAULT 0,
			body TEXT,
			FOREIGN KEY (request_id) REFERENCES http_requests(id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS idx_responses_request_id ON responses(request_id);

		CREATE TABLE IF NOT EXISTS environments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS idx_environments_project_id ON environments(project_id);

		CREATE TABLE IF NOT EXISTS environment_variables (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			environment_id INTEGER NOT NULL,
			key TEXT NOT NULL,
			value TEXT NOT NULL,
			FOREIGN KEY (environment_id) REFERENCES environments(id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS idx_environment_variables_environment_id ON environment_variables(environment_id);

		CREATE TABLE IF NOT EXISTS favourite_collections (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			profile_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (profile_id) REFERENCES profiles(id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS idx_favourite_collections_profile_id ON favourite_collections(profile_id);

		CREATE TABLE IF NOT EXISTS favourite_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			favourite_collection_id INTEGER NOT NULL,
			http_request_id INTEGER NOT NULL,
			added_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			sort_order INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY (favourite_collection_id) REFERENCES favourite_collections(id) ON DELETE CASCADE,
			FOREIGN KEY (http_request_id) REFERENCES http_requests(id) ON DELETE CASCADE,
			UNIQUE (favourite_collection_id, http_request_id)
		);
		CREATE INDEX IF NOT EXISTS idx_favourite_items_collection_id ON favourite_items(favourite_collection_id);
		CREATE INDEX IF NOT EXISTS idx_favourite_items_request_id ON favourite_items(http_request_id);

		CREATE TABLE IF NOT EXISTS tags (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE
		);
		CREATE INDEX IF NOT EXISTS idx_tags_name ON tags(name);

		CREATE TABLE IF NOT EXISTS request_tags (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			request_id INTEGER NOT NULL,
			tag_id INTEGER NOT NULL,
			FOREIGN KEY (request_id) REFERENCES http_requests(id) ON DELETE CASCADE,
			FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE,
			UNIQUE (request_id, tag_id)
		);
		CREATE INDEX IF NOT EXISTS idx_request_tags_request_id ON request_tags(request_id);
		CREATE INDEX IF NOT EXISTS idx_request_tags_tag_id ON request_tags(tag_id);
	`, "")
	if err != nil {
		return err
	}

	if err := createCollectionAppearancesTable(db); err != nil {
		return err
	}
	if err := migrateCollectionAppearancesFromLegacyColumns(db); err != nil {
		return err
	}
	if err := dropCollectionColumnIfExists(db, "icon_id"); err != nil {
		return err
	}
	if err := dropCollectionColumnIfExists(db, "color"); err != nil {
		return err
	}

	return nil
}

func createCollectionAppearancesTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS collection_appearances (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			collection_id INTEGER NOT NULL,
			appearance_type TEXT NOT NULL CHECK(appearance_type IN ('icon', 'color')),
			appearance_value TEXT NOT NULL,
			FOREIGN KEY (collection_id) REFERENCES collections(id) ON DELETE CASCADE,
			UNIQUE (collection_id)
		);
		CREATE INDEX IF NOT EXISTS idx_collection_appearances_collection_id ON collection_appearances(collection_id);
	`)
	if err != nil {
		return fmt.Errorf("creating collection_appearances table: %w", err)
	}
	return nil
}

func migrateCollectionAppearancesFromLegacyColumns(db *sql.DB) error {
	hasIconID := columnExists(db, "collections", "icon_id")
	hasColor := columnExists(db, "collections", "color")
	if !hasIconID && !hasColor {
		return nil
	}

	query := "SELECT id"
	if hasIconID {
		query += ", icon_id"
	}
	if hasColor {
		query += ", color"
	}
	query += " FROM collections"

	rows, err := db.Query(query)
	if err != nil {
		return fmt.Errorf("listing collections for appearance migration: %w", err)
	}
	defer rows.Close()

	type legacy struct {
		id     int64
		iconID string
		color  string
	}

	var items []legacy
	for rows.Next() {
		var item legacy
		dest := []interface{}{&item.id}
		if hasIconID {
			dest = append(dest, &item.iconID)
		}
		if hasColor {
			dest = append(dest, &item.color)
		}
		if err := rows.Scan(dest...); err != nil {
			return fmt.Errorf("scanning collection for appearance migration: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating collections for appearance migration: %w", err)
	}

	for _, item := range items {
		if appearanceExists(db, item.id) {
			continue
		}

		appearanceType := "icon"
		appearanceValue := "default"
		if item.iconID != "" {
			appearanceValue = item.iconID
		} else if item.color != "" {
			appearanceType = "color"
			appearanceValue = item.color
		}

		_, err := db.Exec(
			"INSERT INTO collection_appearances (collection_id, appearance_type, appearance_value) VALUES (?, ?, ?)",
			item.id, appearanceType, appearanceValue,
		)
		if err != nil {
			return fmt.Errorf("migrating appearance for collection %d: %w", item.id, err)
		}
	}

	return nil
}

func appearanceExists(db *sql.DB, collectionID int64) bool {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM collection_appearances WHERE collection_id = ?", collectionID).Scan(&count)
	if err != nil {
		return false
	}
	return count > 0
}

func columnExists(db *sql.DB, table, column string) bool {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var typ string
		var notnull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dfltValue, &pk); err != nil {
			return false
		}
		if name == column {
			return true
		}
	}
	return false
}

func dropCollectionColumnIfExists(db *sql.DB, column string) error {
	if !columnExists(db, "collections", column) {
		return nil
	}
	_, err := db.Exec(fmt.Sprintf("ALTER TABLE collections DROP COLUMN %s", column))
	if err != nil {
		return fmt.Errorf("dropping collections %s column: %w", column, err)
	}
	return nil
}

func ensureDefaultCollection(db *sql.DB) error {
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM collections").Scan(&count); err != nil {
		return fmt.Errorf("counting collections: %w", err)
	}
	if count > 0 {
		return nil
	}

	result, err := db.Exec("INSERT INTO profiles (name) VALUES (?)", "Default Profile")
	if err != nil {
		return fmt.Errorf("creating default profile: %w", err)
	}
	profileID, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("getting default profile id: %w", err)
	}

	result, err = db.Exec("INSERT INTO projects (profile_id, name) VALUES (?, ?)", profileID, "Default Project")
	if err != nil {
		return fmt.Errorf("creating default project: %w", err)
	}
	projectID, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("getting default project id: %w", err)
	}

	_, err = db.Exec("INSERT INTO collections (project_id, name) VALUES (?, ?)", projectID, "my stuff")
	if err != nil {
		return fmt.Errorf("creating default collection: %w", err)
	}

	_, err = db.Exec("INSERT INTO favourite_collections (profile_id, name) VALUES (?, ?)", profileID, "default")
	if err != nil {
		return fmt.Errorf("creating default favourite collection: %w", err)
	}

	return nil
}

// ensureDefaultFavouriteCollections creates a 'default' favourite collection for
// every profile that does not already have one. This backfills existing
// profiles created before the favourites feature was added.
func ensureDefaultFavouriteCollections(db *sql.DB) error {
	rows, err := db.Query(`
		SELECT p.id FROM profiles p
		WHERE NOT EXISTS (
			SELECT 1 FROM favourite_collections fc WHERE fc.profile_id = p.id
		)
	`)
	if err != nil {
		return fmt.Errorf("finding profiles without favourite collections: %w", err)
	}
	defer rows.Close()

	var profileIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scanning profile id: %w", err)
		}
		profileIDs = append(profileIDs, id)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating profile ids: %w", err)
	}

	for _, profileID := range profileIDs {
		_, err := db.Exec(
			"INSERT INTO favourite_collections (profile_id, name) VALUES (?, ?)",
			profileID, "default",
		)
		if err != nil {
			return fmt.Errorf("creating default favourite collection for profile %d: %w", profileID, err)
		}
	}

	return nil
}
