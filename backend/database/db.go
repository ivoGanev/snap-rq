package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"snap-rq/backend/cookies"
	"snap-rq/backend/identity"
	"snap-rq/backend/models"

	"github.com/adrg/xdg"
	_ "modernc.org/sqlite"
)

// Open opens the SQLite database stored in the user's application data
// directory, runs migrations, and ensures default collections exist.
func Open() (*sql.DB, error) {
	return OpenPath(Path())
}

// Path is where the app's database lives.
func Path() string { return filepath.Join(DataDir(), identity.DatabaseFile) }

// DataDirEnv names an environment variable that moves the data folder, e.g.
// to a throwaway folder for the end-to-end tests.
const DataDirEnv = "SNAPRQ_DATA_DIR"

// DataDir is the folder holding the app's database (and, without an OS
// keyring, the secrets key file): $SNAPRQ_DATA_DIR when set.
func DataDir() string {
	if dir := os.Getenv(DataDirEnv); dir != "" {
		return dir
	}
	return filepath.Join(xdg.DataHome, identity.DataDirName)
}

// MovePreviousDataDir moves the data folder from an earlier name
// (identity.PreviousDataDirNames), so a renamed app keeps its profile. It
// reports the folder it moved, or "" when there was nothing to move.
func MovePreviousDataDir() (string, error) {
	if CustomDataDir() {
		return "", nil
	}
	return movePreviousDataDir(xdg.DataHome, identity.DataDirName, identity.PreviousDataDirNames)
}

// An existing current folder is never replaced: it may already hold data.
func movePreviousDataDir(parent, current string, previous []string) (string, error) {
	to := filepath.Join(parent, current)
	if _, err := os.Stat(to); err == nil {
		return "", nil
	}
	for _, name := range previous {
		from := filepath.Join(parent, name)
		if _, err := os.Stat(from); err != nil {
			continue
		}
		if err := os.Rename(from, to); err != nil {
			return "", fmt.Errorf("moving the data folder %s to %s: %w", from, to, err)
		}
		return from, nil
	}
	return "", nil
}

// CustomDataDir reports whether the data folder was moved with
// $SNAPRQ_DATA_DIR. Such a folder is a separate profile: its secrets key
// lives in the folder, never in the OS credential store.
func CustomDataDir() bool { return os.Getenv(DataDirEnv) != "" }

// OpenPath is Open for a database file at an explicit path (tests use a temp file).
func OpenPath(dbPath string) (*sql.DB, error) { return OpenWith(dbPath, nil) }

// OpenWith is OpenPath with a hook that runs on the opened database before
// any migration, e.g. to back it up when the app was updated.
func OpenWith(dbPath string, beforeMigrate func(*sql.DB)) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("creating data directory: %w", err)
	}

	// PRAGMAs go in the DSN so every pooled connection gets them.
	// foreign_keys(1) makes cascades fire on DELETE; journal_mode(WAL)
	// keeps bulk writes from paying rollback-journal overhead. Setting
	// these with db.Exec would only affect ONE pooled connection — the
	// runner's transactions would silently miss the cascades.
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	if beforeMigrate != nil {
		beforeMigrate(db)
	}

	if err := dropTagTables(db); err != nil {
		return nil, fmt.Errorf("migrating database: %w", err)
	}

	if err := migrate(db); err != nil {
		return nil, fmt.Errorf("migrating database: %w", err)
	}

	if err := addBinnedRequestsCollectionIdColumn(db); err != nil {
		return nil, fmt.Errorf("migrating database: %w", err)
	}

	if err := addResponseCreatedAtColumn(db); err != nil {
		return nil, fmt.Errorf("migrating database: %w", err)
	}

	if err := addResponseDurationMsColumn(db); err != nil {
		return nil, fmt.Errorf("migrating database: %w", err)
	}

	if err := addBodyTypeColumns(db); err != nil {
		return nil, fmt.Errorf("migrating database: %w", err)
	}

	if err := addBodyFormColumn(db); err != nil {
		return nil, fmt.Errorf("migrating database: %w", err)
	}

	if err := widenBodyTypeCheck(db); err != nil {
		return nil, fmt.Errorf("migrating database: %w", err)
	}

	if err := addUserSortOrderColumns(db); err != nil {
		return nil, fmt.Errorf("migrating database: %w", err)
	}

	if err := addUserColorColumns(db); err != nil {
		return nil, fmt.Errorf("migrating database: %w", err)
	}

	if err := migrateUserColorsToSlots(db); err != nil {
		return nil, fmt.Errorf("migrating database: %w", err)
	}

	if err := addRequestLastUsedAt(db); err != nil {
		return nil, fmt.Errorf("migrating database: %w", err)
	}

	if err := createCookiesTable(db); err != nil {
		return nil, fmt.Errorf("migrating database: %w", err)
	}

	if err := createScriptTables(db); err != nil {
		return nil, fmt.Errorf("migrating database: %w", err)
	}

	if err := addVariableSecretColumn(db); err != nil {
		return nil, fmt.Errorf("migrating database: %w", err)
	}

	if err := addScriptRunColumns(db); err != nil {
		return nil, fmt.Errorf("migrating database: %w", err)
	}

	if err := addUsageScoreColumn(db); err != nil {
		return nil, fmt.Errorf("migrating database: %w", err)
	}

	if err := makeGroupNamesUnique(db); err != nil {
		return nil, fmt.Errorf("migrating database: %w", err)
	}

	if err := ensureDefaultCollection(db); err != nil {
		return nil, fmt.Errorf("ensuring default collection: %w", err)
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
			body_type TEXT NOT NULL DEFAULT 'json' CHECK(body_type IN ('json', 'text', 'form-data')),
			request_headers TEXT,
			status_code INTEGER NOT NULL DEFAULT 0,
			response_id INTEGER NOT NULL DEFAULT 0,
			user_sort_order INTEGER NOT NULL DEFAULT 0,
			user_color TEXT,
			last_used_at INTEGER NOT NULL DEFAULT 0,
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
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			duration_ms INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY (request_id) REFERENCES http_requests(id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS idx_responses_request_id ON responses(request_id);

		DROP TABLE IF EXISTS request_frequency;

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
			project_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS idx_favourite_collections_project_id ON favourite_collections(project_id);

		CREATE TABLE IF NOT EXISTS favourite_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			favourite_collection_id INTEGER NOT NULL,
			http_request_id INTEGER NOT NULL,
			added_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			user_sort_order INTEGER NOT NULL DEFAULT 0,
			user_color TEXT,
			FOREIGN KEY (favourite_collection_id) REFERENCES favourite_collections(id) ON DELETE CASCADE,
			FOREIGN KEY (http_request_id) REFERENCES http_requests(id) ON DELETE CASCADE,
			UNIQUE (favourite_collection_id, http_request_id)
		);
		CREATE INDEX IF NOT EXISTS idx_favourite_items_collection_id ON favourite_items(favourite_collection_id);
		CREATE INDEX IF NOT EXISTS idx_favourite_items_request_id ON favourite_items(http_request_id);

		CREATE TABLE IF NOT EXISTS binned_requests (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			original_request_id INTEGER NOT NULL,
			project_id INTEGER NOT NULL,
			collection_id INTEGER NOT NULL DEFAULT 0,
			original_collection_id INTEGER NOT NULL,
			original_collection_name TEXT NOT NULL,
			name TEXT NOT NULL,
			url TEXT NOT NULL,
			method TEXT NOT NULL,
			body TEXT,
			body_type TEXT NOT NULL DEFAULT 'json' CHECK(body_type IN ('json', 'text', 'form-data')),
			request_headers TEXT,
			status_code INTEGER NOT NULL DEFAULT 0,
			response_id INTEGER NOT NULL DEFAULT 0,
			deleted_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			snapshot_json TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_binned_requests_project_id ON binned_requests(project_id);
		CREATE INDEX IF NOT EXISTS idx_binned_requests_deleted_at ON binned_requests(deleted_at);

		CREATE TABLE IF NOT EXISTS app_settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS undo_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			command TEXT NOT NULL,
			label TEXT NOT NULL,
			input TEXT NOT NULL,
			snapshot TEXT NOT NULL,
			result TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			state TEXT NOT NULL DEFAULT 'undo' CHECK(state IN ('undo', 'redo'))
		);

		CREATE TABLE IF NOT EXISTS mcp_audit_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			at INTEGER NOT NULL,
			tool TEXT NOT NULL,
			client TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			summary TEXT NOT NULL DEFAULT '',
			args TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '',
			duration_ms INTEGER NOT NULL DEFAULT 0
		);

		INSERT OR IGNORE INTO app_settings (key, value) VALUES ('validate_ssl_certificates', 'true');
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
	if err := createFavouriteAppearancesTable(db); err != nil {
		return err
	}
	if err := ensureFavouriteAppearances(db); err != nil {
		return err
	}

	return nil
}

func createFavouriteAppearancesTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS favourite_appearances (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			favourite_collection_id INTEGER NOT NULL,
			appearance_type TEXT NOT NULL CHECK(appearance_type IN ('icon', 'color')),
			appearance_value TEXT NOT NULL,
			FOREIGN KEY (favourite_collection_id) REFERENCES favourite_collections(id) ON DELETE CASCADE,
			UNIQUE (favourite_collection_id)
		);
		CREATE INDEX IF NOT EXISTS idx_favourite_appearances_collection_id ON favourite_appearances(favourite_collection_id);
	`)
	if err != nil {
		return fmt.Errorf("creating favourite_appearances table: %w", err)
	}
	return nil
}

func ensureFavouriteAppearances(db *sql.DB) error {
	rows, err := db.Query("SELECT id FROM favourite_collections")
	if err != nil {
		return fmt.Errorf("listing favourite collections for appearance backfill: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scanning favourite collection id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating favourite collection ids: %w", err)
	}

	for _, id := range ids {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM favourite_appearances WHERE favourite_collection_id = ?", id).Scan(&count); err != nil {
			return fmt.Errorf("checking favourite appearance exists: %w", err)
		}
		if count > 0 {
			continue
		}
		defaultAppearance := models.DefaultFavouriteAppearance()
		_, err := db.Exec(
			"INSERT INTO favourite_appearances (favourite_collection_id, appearance_type, appearance_value) VALUES (?, ?, ?)",
			id, defaultAppearance.AppearanceType, defaultAppearance.AppearanceValue,
		)
		if err != nil {
			return fmt.Errorf("creating default favourite appearance for %d: %w", id, err)
		}
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
		appearanceValue := "ico_1a9d4"
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

func addBinnedRequestsCollectionIdColumn(db *sql.DB) error {
	if !columnExists(db, "binned_requests", "collection_id") {
		_, err := db.Exec("ALTER TABLE binned_requests ADD COLUMN collection_id INTEGER NOT NULL DEFAULT 0")
		if err != nil {
			return fmt.Errorf("adding collection_id column to binned_requests: %w", err)
		}
	}
	return nil
}

func addResponseCreatedAtColumn(db *sql.DB) error {
	if !columnExists(db, "responses", "created_at") {
		_, err := db.Exec("ALTER TABLE responses ADD COLUMN created_at TEXT")
		if err != nil {
			return fmt.Errorf("adding created_at column to responses: %w", err)
		}
		_, err = db.Exec("UPDATE responses SET created_at = CURRENT_TIMESTAMP WHERE created_at IS NULL")
		if err != nil {
			return fmt.Errorf("backfilling response created_at: %w", err)
		}
	}

	_, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_responses_created_at ON responses(created_at)")
	if err != nil {
		return fmt.Errorf("creating responses created_at index: %w", err)
	}

	return nil
}

func addResponseDurationMsColumn(db *sql.DB) error {
	if !columnExists(db, "responses", "duration_ms") {
		_, err := db.Exec("ALTER TABLE responses ADD COLUMN duration_ms INTEGER NOT NULL DEFAULT 0")
		if err != nil {
			return fmt.Errorf("adding duration_ms column to responses: %w", err)
		}
	}

	return nil
}

func addBodyTypeColumns(db *sql.DB) error {
	added := false
	if !columnExists(db, "http_requests", "body_type") {
		_, err := db.Exec("ALTER TABLE http_requests ADD COLUMN body_type TEXT NOT NULL DEFAULT 'json'")
		if err != nil {
			return fmt.Errorf("adding body_type column to http_requests: %w", err)
		}
		added = true
	}
	if !columnExists(db, "binned_requests", "body_type") {
		_, err := db.Exec("ALTER TABLE binned_requests ADD COLUMN body_type TEXT NOT NULL DEFAULT 'json'")
		if err != nil {
			return fmt.Errorf("adding body_type column to binned_requests: %w", err)
		}
		added = true
	}

	if added {
		if err := backfillRequestBodyTypes(db, "http_requests"); err != nil {
			return err
		}
		if err := backfillRequestBodyTypes(db, "binned_requests"); err != nil {
			return err
		}
	}
	return nil
}

// addBodyFormColumn stores the structured form rows (JSON) used to build a
// multipart/form-data body at send time. Text and file entries, with file
// paths, live here rather than in the raw body text.
func addBodyFormColumn(db *sql.DB) error {
	for _, table := range []string{"http_requests", "binned_requests"} {
		if columnExists(db, table, "body_form") {
			continue
		}
		if _, err := db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN body_form TEXT", table)); err != nil {
			return fmt.Errorf("adding body_form column to %s: %w", table, err)
		}
	}
	return nil
}

// widenBodyTypeCheck widens body_type to allow 'form-data'. SQLite cannot
// alter a CHECK constraint, so tables with the old constraint are rebuilt
// (foreign keys disabled on a dedicated connection during the swap).
func widenBodyTypeCheck(db *sql.DB) error {
	for _, table := range []string{"http_requests", "binned_requests"} {
		allows, err := bodyTypeAllowsFormData(db, table)
		if err != nil {
			return err
		}
		if allows {
			continue
		}
		if err := rebuildBodyTypeTable(db, table); err != nil {
			return err
		}
	}
	return nil
}

// bodyTypeAllowsFormData reports whether a table's stored CHECK already
// accepts 'form-data'.
func bodyTypeAllowsFormData(db *sql.DB, table string) (bool, error) {
	var sqlText string
	if err := db.QueryRow(
		`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`, table,
	).Scan(&sqlText); err != nil {
		return false, err
	}
	return strings.Contains(sqlText, "form-data"), nil
}

// rebuildBodyTypeTable recreates one request table with the widened body_type
// CHECK, carrying over rows and marking any that used form data as 'form-data'.
func rebuildBodyTypeTable(db *sql.DB, table string) error {
	hasMode := columnExists(db, table, "body_mode")
	typeExpr := "body_type"
	if hasMode {
		typeExpr = "CASE WHEN body_type = 'form-data' OR body_mode = 'form-data' OR COALESCE(body_form, '') != '' THEN 'form-data' ELSE body_type END"
	} else {
		typeExpr = "CASE WHEN body_type = 'form-data' OR COALESCE(body_form, '') != '' THEN 'form-data' ELSE body_type END"
	}

	var newCreate, targetCols string
	var indexes []string
	switch table {
	case "http_requests":
		newCreate = `CREATE TABLE http_requests_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			collection_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			url TEXT NOT NULL,
			method TEXT NOT NULL,
			body TEXT,
			body_type TEXT NOT NULL DEFAULT 'json' CHECK(body_type IN ('json', 'text', 'form-data')),
			request_headers TEXT,
			status_code INTEGER NOT NULL DEFAULT 0,
			response_id INTEGER NOT NULL DEFAULT 0,
			user_sort_order INTEGER NOT NULL DEFAULT 0,
			user_color TEXT,
			last_used_at INTEGER NOT NULL DEFAULT 0,
			body_form TEXT,
			FOREIGN KEY (collection_id) REFERENCES collections(id) ON DELETE CASCADE
		)`
		targetCols = "id, collection_id, name, url, method, body, body_type, request_headers, status_code, response_id, user_sort_order, user_color, last_used_at, body_form"
		indexes = []string{
			"CREATE INDEX idx_http_requests_collection_id ON http_requests(collection_id)",
			"CREATE INDEX idx_http_requests_name ON http_requests(name)",
			"CREATE INDEX idx_http_requests_collection_sort ON http_requests(collection_id, user_sort_order)",
			"CREATE INDEX idx_http_requests_last_used_at ON http_requests(last_used_at DESC)",
		}
	case "binned_requests":
		newCreate = `CREATE TABLE binned_requests_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			original_request_id INTEGER NOT NULL,
			project_id INTEGER NOT NULL,
			collection_id INTEGER NOT NULL DEFAULT 0,
			original_collection_id INTEGER NOT NULL,
			original_collection_name TEXT NOT NULL,
			name TEXT NOT NULL,
			url TEXT NOT NULL,
			method TEXT NOT NULL,
			body TEXT,
			body_type TEXT NOT NULL DEFAULT 'json' CHECK(body_type IN ('json', 'text', 'form-data')),
			request_headers TEXT,
			status_code INTEGER NOT NULL DEFAULT 0,
			response_id INTEGER NOT NULL DEFAULT 0,
			deleted_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			snapshot_json TEXT NOT NULL,
			body_form TEXT
		)`
		targetCols = "id, original_request_id, project_id, collection_id, original_collection_id, original_collection_name, name, url, method, body, body_type, request_headers, status_code, response_id, deleted_at, snapshot_json, body_form"
		indexes = []string{
			"CREATE INDEX idx_binned_requests_project_id ON binned_requests(project_id)",
			"CREATE INDEX idx_binned_requests_deleted_at ON binned_requests(deleted_at)",
		}
	default:
		return fmt.Errorf("unknown table %q", table)
	}

	srcCols := strings.Replace(targetCols, "body_type", typeExpr, 1)
	return rebuildTable(db, table, newCreate, targetCols, srcCols, indexes)
}

// rebuildTable performs the SQLite table-rebuild dance: create the new table,
// copy rows, drop the old table, rename, and recreate indexes.
func rebuildTable(db *sql.DB, table, newCreate, targetCols, srcCols string, indexes []string) error {
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return err
	}
	defer func() {
		_, _ = conn.ExecContext(ctx, "PRAGMA foreign_keys = ON")
	}()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	newTable := table + "_new"
	if _, err := tx.Exec(newCreate); err != nil {
		return fmt.Errorf("creating new %s: %w", table, err)
	}
	if _, err := tx.Exec(fmt.Sprintf("INSERT INTO %s (%s) SELECT %s FROM %s", newTable, targetCols, srcCols, table)); err != nil {
		return fmt.Errorf("copying %s: %w", table, err)
	}
	if _, err := tx.Exec(fmt.Sprintf("DROP TABLE %s", table)); err != nil {
		return fmt.Errorf("dropping old %s: %w", table, err)
	}
	if _, err := tx.Exec(fmt.Sprintf("ALTER TABLE %s RENAME TO %s", newTable, table)); err != nil {
		return fmt.Errorf("renaming %s: %w", table, err)
	}
	for _, idx := range indexes {
		if _, err := tx.Exec(idx); err != nil {
			return fmt.Errorf("recreating index on %s: %w", table, err)
		}
	}
	return tx.Commit()
}

// backfillRequestBodyTypes marks existing rows as plain text when their body is
// not valid JSON, so the stored type matches what the editor would infer.
func backfillRequestBodyTypes(db *sql.DB, table string) error {
	rows, err := db.Query(fmt.Sprintf("SELECT id, body FROM %s WHERE body IS NOT NULL AND body <> ''", table))
	if err != nil {
		return fmt.Errorf("listing %s for body_type backfill: %w", table, err)
	}
	defer rows.Close()

	var textIDs []int64
	for rows.Next() {
		var id int64
		var body string
		if err := rows.Scan(&id, &body); err != nil {
			return fmt.Errorf("scanning %s for body_type backfill: %w", table, err)
		}
		if !json.Valid([]byte(body)) {
			textIDs = append(textIDs, id)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating %s for body_type backfill: %w", table, err)
	}

	for _, id := range textIDs {
		if _, err := db.Exec(fmt.Sprintf("UPDATE %s SET body_type = 'text' WHERE id = ?", table), id); err != nil {
			return fmt.Errorf("backfilling body_type for %s %d: %w", table, id, err)
		}
	}
	return nil
}

// addUserSortOrderColumns introduces the per-view user ordering columns.
//
//   - http_requests.user_sort_order orders a request within its collection.
//   - favourite_items.sort_order is renamed to user_sort_order for naming
//     consistency (values are preserved).
//
// Legacy rows keep 0; every consumer orders by user_sort_order with the
// previous natural tiebreaker (name), so existing ordering is unchanged until
// the user drags for the first time.
func addUserSortOrderColumns(db *sql.DB) error {
	if !columnExists(db, "http_requests", "user_sort_order") {
		_, err := db.Exec("ALTER TABLE http_requests ADD COLUMN user_sort_order INTEGER NOT NULL DEFAULT 0")
		if err != nil {
			return fmt.Errorf("adding user_sort_order column to http_requests: %w", err)
		}
	}

	if columnExists(db, "favourite_items", "sort_order") && !columnExists(db, "favourite_items", "user_sort_order") {
		_, err := db.Exec("ALTER TABLE favourite_items RENAME COLUMN sort_order TO user_sort_order")
		if err != nil {
			return fmt.Errorf("renaming favourite_items.sort_order to user_sort_order: %w", err)
		}
	}

	_, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_http_requests_collection_sort ON http_requests(collection_id, user_sort_order)")
	if err != nil {
		return fmt.Errorf("creating http_requests collection sort index: %w", err)
	}

	return nil
}

// addUserColorColumns introduces the per-view user colour columns.
//
// - http_requests.user_color colours a request within its collection.
// - favourite_items.user_color colours a request within a favourite collection.
//
// Columns are nullable; NULL means "no colour".
func addUserColorColumns(db *sql.DB) error {
	for _, table := range []string{"http_requests", "favourite_items"} {
		if columnExists(db, table, "user_color") {
			continue
		}
		_, err := db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN user_color TEXT", table))
		if err != nil {
			return fmt.Errorf("adding user_color column to %s: %w", table, err)
		}
	}
	return nil
}

// migrateUserColorsToSlots rewrites the hex colours stored before colour slots
// existed (user_color used to hold the picked pastel's hex) into theme
// independent slot names, in the tables that carry a colour and in the
// undo history, whose stored payloads would otherwise write a hex back on
// undo. It is idempotent: values that are already slots, and hex values it
// has no slot for, are left untouched.
func migrateUserColorsToSlots(db *sql.DB) error {
	// Look up the schema before the transaction opens: it holds a
	// connection, and a second one would not see an in-memory database.
	var tables []string
	for _, table := range []string{"http_requests", "favourite_items"} {
		if tableExists(db, table) && columnExists(db, table, "user_color") {
			tables = append(tables, table)
		}
	}
	hasUndoLog := tableExists(db, "undo_log")

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("starting colour slot migration: %w", err)
	}
	defer tx.Rollback()

	for _, hex := range models.LegacyColorHexes() {
		slot, _ := models.LegacyColorSlot(hex)

		for _, table := range tables {
			if _, err := tx.Exec(
				fmt.Sprintf("UPDATE %s SET user_color = ? WHERE lower(user_color) = ?", table),
				slot, hex,
			); err != nil {
				return fmt.Errorf("migrating %s colours to slots: %w", table, err)
			}
		}

		if !hasUndoLog {
			continue
		}
		// Payloads are JSON, so a colour appears as a quoted string. The
		// UI only ever wrote the upper-case form, but match both.
		for _, variant := range []string{hex, strings.ToUpper(hex)} {
			needle, replacement := `"`+variant+`"`, `"`+slot+`"`
			for _, column := range []string{"input", "snapshot", "result"} {
				if _, err := tx.Exec(
					fmt.Sprintf("UPDATE undo_log SET %[1]s = replace(%[1]s, ?, ?) WHERE instr(%[1]s, ?) > 0", column),
					needle, replacement, needle,
				); err != nil {
					return fmt.Errorf("migrating undo_log %s colours to slots: %w", column, err)
				}
			}
		}
	}

	return tx.Commit()
}

// addRequestLastUsedAt folds recency into the request row. Last-used time
// used to live in request_usage; keeping it on http_requests means a delete
// undo restores it with the rest of the row, so the Recent view recovers.
func addRequestLastUsedAt(db *sql.DB) error {
	if !columnExists(db, "http_requests", "last_used_at") {
		_, err := db.Exec("ALTER TABLE http_requests ADD COLUMN last_used_at INTEGER NOT NULL DEFAULT 0")
		if err != nil {
			return fmt.Errorf("adding last_used_at column to http_requests: %w", err)
		}
	}

	if tableExists(db, "request_usage") {
		_, err := db.Exec(`
			UPDATE http_requests
			SET last_used_at = COALESCE(
				(SELECT ru.last_used_at FROM request_usage ru WHERE ru.request_id = http_requests.id),
				last_used_at)`)
		if err != nil {
			return fmt.Errorf("copying request_usage into http_requests: %w", err)
		}
		if _, err := db.Exec("DROP TABLE request_usage"); err != nil {
			return fmt.Errorf("dropping request_usage: %w", err)
		}
	}

	_, err := db.Exec(
		"CREATE INDEX IF NOT EXISTS idx_http_requests_last_used_at ON http_requests(last_used_at DESC)",
	)
	if err != nil {
		return fmt.Errorf("creating http_requests last_used_at index: %w", err)
	}
	return nil
}

// createCookiesTable adds the per-project cookie jar. Cookies are unique by
// project, domain, path and name, as RFC 6265 defines their identity.
func createCookiesTable(db *sql.DB) error {
	_, err := db.Exec(cookies.Schema)
	return err
}

// addVariableSecretColumn marks environment variables as secret. A secret's
// value column holds the vault's encrypted form (see backend/secrets).
func addVariableSecretColumn(db *sql.DB) error {
	if columnExists(db, "environment_variables", "secret") {
		return nil
	}
	if _, err := db.Exec("ALTER TABLE environment_variables ADD COLUMN secret INTEGER NOT NULL DEFAULT 0"); err != nil {
		return fmt.Errorf("adding secret column to environment_variables: %w", err)
	}
	return nil
}

// addUsageScoreColumn adds how much each request is used lately: a score
// each send adds 1 to and that halves every week (commands/requests/usage.go).
// Requests already sent start at one use, so they show some usage at once.
func addUsageScoreColumn(db *sql.DB) error {
	if columnExists(db, "http_requests", "use_score") {
		return nil
	}
	if _, err := db.Exec(`ALTER TABLE http_requests ADD COLUMN use_score REAL NOT NULL DEFAULT 0`); err != nil {
		return fmt.Errorf("adding use_score to http_requests: %w", err)
	}
	if _, err := db.Exec(`UPDATE http_requests SET use_score = 1 WHERE last_used_at > 0`); err != nil {
		return fmt.Errorf("seeding use_score: %w", err)
	}
	return nil
}

// addScriptRunColumns lets scripts run on send (see backend/scripting):
//   - request_scripts.run_order: the order a request's scripts run in
//     (user_sort_order orders requests inside a script's Attachments list);
//     existing links run in the order they were attached.
//   - request_scripts.enabled: a script can be switched off for one request.
//   - responses.script_log: what the scripts did in that send (JSON).
func addScriptRunColumns(db *sql.DB) error {
	if !columnExists(db, "request_scripts", "run_order") {
		if _, err := db.Exec(`ALTER TABLE request_scripts ADD COLUMN run_order INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("adding run_order to request_scripts: %w", err)
		}
		if _, err := db.Exec(`UPDATE request_scripts SET run_order = id`); err != nil {
			return fmt.Errorf("ordering existing script links: %w", err)
		}
	}
	if !columnExists(db, "request_scripts", "enabled") {
		if _, err := db.Exec(`ALTER TABLE request_scripts ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1`); err != nil {
			return fmt.Errorf("adding enabled to request_scripts: %w", err)
		}
	}
	if !columnExists(db, "responses", "script_log") {
		if _, err := db.Exec(`ALTER TABLE responses ADD COLUMN script_log TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("adding script_log to responses: %w", err)
		}
	}
	return nil
}

// createScriptTables adds user scripts: named code a user writes per project
// and attaches to many requests. They replace tags (see dropTagTables);
// a script keeps a per-request order and colour and a sidebar icon or
// colour.
func createScriptTables(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS scripts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			content TEXT NOT NULL DEFAULT '',
			FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
			UNIQUE (project_id, name)
		);

		CREATE TABLE IF NOT EXISTS request_scripts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			request_id INTEGER NOT NULL,
			script_id INTEGER NOT NULL,
			user_sort_order INTEGER NOT NULL DEFAULT 0,
			user_color TEXT,
			FOREIGN KEY (request_id) REFERENCES http_requests(id) ON DELETE CASCADE,
			FOREIGN KEY (script_id) REFERENCES scripts(id) ON DELETE CASCADE,
			UNIQUE (request_id, script_id)
		);
		CREATE INDEX IF NOT EXISTS idx_request_scripts_script_sort ON request_scripts(script_id, user_sort_order);

		CREATE TABLE IF NOT EXISTS script_appearances (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			script_id INTEGER NOT NULL,
			appearance_type TEXT NOT NULL CHECK(appearance_type IN ('icon', 'color')),
			appearance_value TEXT NOT NULL,
			FOREIGN KEY (script_id) REFERENCES scripts(id) ON DELETE CASCADE,
			UNIQUE (script_id)
		);
	`)
	if err != nil {
		return fmt.Errorf("creating script tables: %w", err)
	}
	return nil
}

func tableExists(db *sql.DB, table string) bool {
	var count int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table,
	).Scan(&count); err != nil {
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

	_, err = db.Exec("INSERT INTO favourite_collections (project_id, name) VALUES (?, ?)", projectID, "default")
	if err != nil {
		return fmt.Errorf("creating default favourite collection: %w", err)
	}

	return nil
}

// makeGroupNamesUnique gives collections and favourite collections a name
// that is unique within their project, compared without regard to case.
// Databases created before this rule can hold duplicates, which would make
// the unique index fail, so the later duplicate is renamed ("name (2)")
// first. Nothing is deleted.
func makeGroupNamesUnique(db *sql.DB) error {
	for _, table := range []string{"collections", "favourite_collections"} {
		if err := renameDuplicateNames(db, table); err != nil {
			return err
		}
		_, err := db.Exec(fmt.Sprintf(
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_%s_project_name ON %s(project_id, name COLLATE NOCASE)`,
			table, table))
		if err != nil {
			return fmt.Errorf("creating unique name index on %s: %w", table, err)
		}
	}
	return nil
}

func renameDuplicateNames(db *sql.DB, table string) error {
	rows, err := db.Query(fmt.Sprintf(`SELECT id, project_id, name FROM %s ORDER BY project_id, id`, table))
	if err != nil {
		return fmt.Errorf("reading %s names: %w", table, err)
	}
	type row struct {
		id, projectID int64
		name          string
	}
	var all []row
	taken := map[string]bool{}
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.projectID, &r.name); err != nil {
			rows.Close()
			return err
		}
		all = append(all, r)
		taken[fmt.Sprintf("%d/%s", r.projectID, strings.ToLower(r.name))] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	seen := map[string]bool{}
	for _, r := range all {
		key := fmt.Sprintf("%d/%s", r.projectID, strings.ToLower(r.name))
		if !seen[key] {
			seen[key] = true
			continue
		}
		newName := ""
		for n := 2; ; n++ {
			newName = fmt.Sprintf("%s (%d)", r.name, n)
			k := fmt.Sprintf("%d/%s", r.projectID, strings.ToLower(newName))
			if !taken[k] {
				taken[k] = true
				seen[k] = true
				break
			}
		}
		if _, err := db.Exec(fmt.Sprintf(`UPDATE %s SET name = ? WHERE id = ?`, table), newName, r.id); err != nil {
			return fmt.Errorf("renaming duplicate in %s: %w", table, err)
		}
	}
	return nil
}

// dropTagTables removes tags, which user scripts replaced (see
// docs/plans/tags-to-scripts.md). Existing tags are deleted, not converted.
// Undo entries of tag commands are removed too: those commands no longer
// exist, so they could not be undone. Older request-delete and trash
// snapshots still carry tag fields; they are ignored from now on.
func dropTagTables(db *sql.DB) error {
	hasUndoLog := tableExists(db, "undo_log")

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("starting tag removal: %w", err)
	}
	defer tx.Rollback()

	for _, table := range []string{"request_tags", "tag_appearances", "tags"} {
		if _, err := tx.Exec("DROP TABLE IF EXISTS " + table); err != nil {
			return fmt.Errorf("dropping %s: %w", table, err)
		}
	}
	if hasUndoLog {
		if _, err := tx.Exec(`DELETE FROM undo_log WHERE command LIKE 'tag.%'`); err != nil {
			return fmt.Errorf("removing tag undo entries: %w", err)
		}
	}
	return tx.Commit()
}
