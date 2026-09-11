#!/usr/bin/env python3
"""
Seed the Wails SQLite database with a dedicated "Mock Server" collection
containing one request per mock-server endpoint.

The script is idempotent: re-running it will not duplicate the mock collection
or its requests. Use --force to drop and recreate the mock collection.

Usage:
    python tools/seed_scale_test.py
    python tools/seed_scale_test.py --force
"""

import argparse
import os
import sqlite3
import sys
from pathlib import Path

MOCK_COLLECTION_NAME = "Mock Server"
MOCK_BASE_URL = "http://localhost:18080"

# Each tuple is (name, method, path, optional_body, optional_headers).
MOCK_ENDPOINTS = [
    # Content-type mocks
    ("JSON response", "GET", "/mock/json", None, None),
    ("CSV response", "GET", "/mock/csv", None, None),
    ("HTML response", "GET", "/mock/html", None, None),
    ("Plain text response", "GET", "/mock/text", None, None),
    ("XML response", "GET", "/mock/xml", None, None),
    ("Binary download", "GET", "/mock/binary", None, None),
    # Status mocks
    ("200 OK", "GET", "/mock/status/200", None, None),
    ("201 Created", "GET", "/mock/status/201", None, None),
    ("204 No Content", "GET", "/mock/status/204", None, None),
    ("400 Bad Request", "GET", "/mock/status/400", None, None),
    ("401 Unauthorized", "GET", "/mock/status/401", None, None),
    ("404 Not Found", "GET", "/mock/status/404", None, None),
    ("500 Internal Server Error", "GET", "/mock/status/500", None, None),
    # Special mocks
    ("Delayed response", "GET", "/mock/delay?ms=2000", None, None),
    ("Empty response", "GET", "/mock/empty", None, None),
    # Echo endpoint
    ("Echo endpoint", "POST", "/echo", '{"hello":"world"}', "Content-Type: application/json"),
]

# Distinct appearance for the mock collection so it stands out.
MOCK_COLLECTION_APPEARANCE = ("color", "#f97316")  # orange-500

SCHEMA = """
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

CREATE TABLE IF NOT EXISTS collection_appearances (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    collection_id INTEGER NOT NULL,
    appearance_type TEXT NOT NULL CHECK(appearance_type IN ('icon', 'color')),
    appearance_value TEXT NOT NULL,
    FOREIGN KEY (collection_id) REFERENCES collections(id) ON DELETE CASCADE,
    UNIQUE (collection_id)
);
CREATE INDEX IF NOT EXISTS idx_collection_appearances_collection_id ON collection_appearances(collection_id);

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
"""


def default_db_path() -> Path:
    """Return the default Wails app database path for the current OS."""
    if sys.platform == "win32":
        base = Path(os.environ.get("LOCALAPPDATA", Path.home() / "AppData" / "Local"))
    elif sys.platform == "darwin":
        base = Path.home() / "Library" / "Application Support"
    else:
        base = Path(os.environ.get("XDG_DATA_HOME", Path.home() / ".local" / "share"))
    return base / "snap-rq-wails-v3" / "app.db"


def ensure_schema(conn: sqlite3.Connection) -> None:
    """Create the schema if it does not exist."""
    conn.executescript(SCHEMA)
    conn.commit()


def ensure_profile_and_project(conn: sqlite3.Connection) -> tuple[int, int]:
    """Return the first existing profile/project ids, creating defaults if needed."""
    cursor = conn.cursor()

    cursor.execute("SELECT id FROM profiles ORDER BY id LIMIT 1")
    row = cursor.fetchone()
    if row:
        profile_id = row[0]
    else:
        cursor.execute("INSERT INTO profiles (name) VALUES (?)", ("Default Profile",))
        profile_id = cursor.lastrowid
        print(f"Created profile '{profile_id}'.")

    cursor.execute("SELECT id FROM projects WHERE profile_id = ? ORDER BY id LIMIT 1", (profile_id,))
    row = cursor.fetchone()
    if row:
        project_id = row[0]
    else:
        cursor.execute(
            "INSERT INTO projects (profile_id, name) VALUES (?, ?)",
            (profile_id, "Default Project"),
        )
        project_id = cursor.lastrowid
        print(f"Created project '{project_id}'.")

    conn.commit()
    return profile_id, project_id


def find_mock_collection(conn: sqlite3.Connection, project_id: int) -> int | None:
    """Return the mock collection id if it exists, otherwise None."""
    cursor = conn.cursor()
    cursor.execute(
        "SELECT id FROM collections WHERE project_id = ? AND name = ?",
        (project_id, MOCK_COLLECTION_NAME),
    )
    row = cursor.fetchone()
    return row[0] if row else None


def delete_mock_collection(conn: sqlite3.Connection, project_id: int) -> None:
    """Remove the mock collection and all associated requests/responses."""
    cursor = conn.cursor()
    cursor.execute(
        "SELECT id FROM collections WHERE project_id = ? AND name = ?",
        (project_id, MOCK_COLLECTION_NAME),
    )
    row = cursor.fetchone()
    if row:
        collection_id = row[0]
        # Cascading deletes will remove requests and responses; manually clean up
        # appearance row first to avoid orphan rows in older schemas.
        cursor.execute("DELETE FROM collection_appearances WHERE collection_id = ?", (collection_id,))
        cursor.execute("DELETE FROM collections WHERE id = ?", (collection_id,))
        conn.commit()
        print(f"Removed existing '{MOCK_COLLECTION_NAME}' collection.")


def create_mock_collection(conn: sqlite3.Connection, project_id: int) -> int:
    """Create the mock collection with a distinct appearance and return its id."""
    cursor = conn.cursor()
    cursor.execute(
        "INSERT INTO collections (project_id, name) VALUES (?, ?)",
        (project_id, MOCK_COLLECTION_NAME),
    )
    collection_id = cursor.lastrowid

    appearance_type, appearance_value = MOCK_COLLECTION_APPEARANCE
    cursor.execute(
        """
        INSERT INTO collection_appearances (collection_id, appearance_type, appearance_value)
        VALUES (?, ?, ?)
        """,
        (collection_id, appearance_type, appearance_value),
    )

    conn.commit()
    print(f"Created '{MOCK_COLLECTION_NAME}' collection with {appearance_type} appearance.")
    return collection_id


def create_mock_requests(conn: sqlite3.Connection, collection_id: int) -> int:
    """Insert one request per mock endpoint into the collection. Returns request count."""
    cursor = conn.cursor()
    count = 0

    for name, method, path, body, headers in MOCK_ENDPOINTS:
        url = f"{MOCK_BASE_URL}{path}"
        body = body if body is not None else ""
        headers = headers if headers is not None else ""

        cursor.execute(
            """
            INSERT INTO http_requests (collection_id, name, url, method, body, request_headers, status_code, response_id)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?)
            """,
            (collection_id, name, url, method, body, headers, 0, 0),
        )
        count += 1

    conn.commit()
    return count


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Seed the Snap RQ database with a Mock Server collection."
    )
    parser.add_argument(
        "--force",
        action="store_true",
        help="Remove and recreate the mock collection if it already exists.",
    )
    args = parser.parse_args()

    db_path = default_db_path().resolve()
    print(f"Database: {db_path}")

    db_path.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(db_path)

    try:
        ensure_schema(conn)
        _profile_id, project_id = ensure_profile_and_project(conn)

        existing_id = find_mock_collection(conn, project_id)
        if existing_id is not None:
            if args.force:
                delete_mock_collection(conn, project_id)
            else:
                print(
                    f"'{MOCK_COLLECTION_NAME}' collection already exists (id={existing_id}). "
                    "Use --force to recreate it."
                )
                return 0

        collection_id = create_mock_collection(conn, project_id)
        requests = create_mock_requests(conn, collection_id)
        print(f"Done. Inserted {requests} mock request(s) into '{MOCK_COLLECTION_NAME}'.")
    finally:
        conn.close()

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
