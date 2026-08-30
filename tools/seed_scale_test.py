#!/usr/bin/env python3
"""
Inject mock profiles, projects, collections, HTTP requests and responses into
the Wails SQLite database.

This script ALWAYS drops and recreates the target database file so each run
starts from a clean state.

Usage:
    python scripts/mock_requests.py
"""

import os
import random
import sqlite3
import string
import sys
from pathlib import Path

# -----------------------------------------------------------------------------
# Scale-test configuration. Edit these constants to change the generated data.
# -----------------------------------------------------------------------------
COLLECTION_COUNT = 20
MIN_REQUESTS_PER_COLLECTION = 10
MAX_REQUESTS_PER_COLLECTION = 500

REQUEST_MIN_LEN = 5
REQUEST_MAX_LEN = 1000

RESPONSE_MIN_LEN = 50
RESPONSE_MAX_LEN = 5000

# Probability that a request body is a large ~4000 character payload.
BIG_BODY_CHANCE = 0.1
BIG_BODY_SIZE = 4000

# Probability that a request has headers. The complement means many requests
# will have no headers at all.
REQUEST_HEADERS_CHANCE = 0.5
RESPONSE_HEADERS_CHANCE = 0.5

# Each request gets exactly one response so every status code is represented
# across the generated data set.
STATUS_CODES = [200, 201, 204, 301, 302, 400, 401, 403, 404, 422, 500, 502, 503]

METHODS = ["GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"]
ENVIRONMENT_NAMES = ["Production", "Staging", "Development", "Testing"]
VARIABLE_KEYS = [
    "API_BASE_URL",
    "API_KEY",
    "AUTH_TOKEN",
    "TIMEOUT",
    "RETRY_COUNT",
    "LOG_LEVEL",
    "FEATURE_FLAG",
    "REGION",
    "BUCKET_NAME",
    "DATABASE_URL",
]
COLLECTION_NAMES = [
    "auth",
    "users",
    "orders",
    "products",
    "search",
    "checkout",
    "webhooks",
    "payments",
    "inventory",
    "shipping",
    "notifications",
    "reports",
    "audit",
    "config",
    "health",
    "analytics",
    "jobs",
    "files",
    "comments",
    "dashboard",
]

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


def random_text(min_len: int, max_len: int) -> str:
    """Generate a random ASCII string of variable length."""
    length = random.randint(min_len, max_len)
    chars = string.ascii_letters + string.digits + string.punctuation + " \n"
    return "".join(random.choices(chars, k=length))


def random_name(min_len: int, max_len: int) -> str:
    """Generate a readable random name."""
    length = random.randint(min_len, max_len)
    words = ["user", "order", "product", "search", "login", "checkout", "profile", "api", "v1", "v2", "webhook", "auth", "token", "refresh", "list", "detail", "create", "update", "delete"]
    parts = []
    while len(" ".join(parts)) < length:
        parts.append(random.choice(words))
    name = " ".join(parts)
    if len(name) > max_len:
        name = name[:max_len].rstrip()
    return name


def random_url(min_len: int, max_len: int) -> str:
    """Generate a random URL, sometimes with query params and fragments."""
    hosts = ["api.example.com", "localhost:8080", "test.service.io", "dev.internal.net", "staging.gateway.org"]
    paths = ["users", "orders", "products", "search", "auth/login", "webhooks", "v1/resources", "v2/items"]
    url = f"https://{random.choice(hosts)}/{random.choice(paths)}"

    if random.random() < 0.7:
        url += f"/{random.randint(1, 99999)}"
    if random.random() < 0.5:
        params = "&".join(
            f"{random.choice(['q', 'id', 'page', 'limit', 'filter', 'sort'])}={random.randint(1, 9999)}"
            for _ in range(random.randint(1, 5))
        )
        url += f"?{params}"
    if random.random() < 0.2:
        url += "#section"

    if len(url) < min_len:
        url += "/" + "x" * (min_len - len(url) - 1)
    if len(url) > max_len:
        url = url[:max_len]
    return url


def random_request_body(min_len: int, max_len: int) -> str:
    """Generate a random JSON-like or plain text request body."""
    if random.random() < BIG_BODY_CHANCE:
        return random_text(BIG_BODY_SIZE, BIG_BODY_SIZE)

    choice = random.random()
    if choice < 0.3:
        return "" if random.random() < 0.5 else "{}"
    if choice < 0.7:
        size = random.randint(min_len, max_len)
        obj = {
            "id": random.randint(1, 999999),
            "name": random_name(5, 40),
            "active": random.choice([True, False]),
            "tags": [random_name(3, 12) for _ in range(random.randint(0, 10))],
            "payload": random_text(max(0, size - 100), max(0, size - 50)),
        }
        return str(obj).replace("'", '"')
    return random_text(min_len, max_len)


def random_response_body(min_len: int, max_len: int) -> str:
    """Generate a random response body, often JSON-like and large."""
    choice = random.random()
    if choice < 0.2:
        return ""
    if choice < 0.7:
        size = random.randint(min_len, max_len)
        obj = {
            "status": random.choice(["ok", "error", "pending"]),
            "data": {
                "id": random.randint(1, 999999),
                "name": random_name(5, 40),
                "items": [
                    {
                        "id": random.randint(1, 9999),
                        "value": random_text(10, 60),
                    }
                    for _ in range(random.randint(1, max(1, size // 200)))
                ],
                "notes": random_text(max(0, size - 300), max(0, size - 100)),
            },
            "meta": {"page": random.randint(1, 100), "total": random.randint(1, 10000)},
        }
        return str(obj).replace("'", '"')
    return random_text(min_len, max_len)


def random_headers(min_len: int, max_len: int, chance: float) -> str:
    """Generate random HTTP headers as a single string, or empty if rolled below chance."""
    if random.random() > chance:
        return ""
    headers = []
    header_pool = [
        ("Content-Type", random.choice(["application/json", "application/xml", "text/plain", "multipart/form-data"])),
        ("Authorization", f"Bearer {random_text(20, 60)}"),
        ("X-Request-ID", random_text(10, 40)),
        ("Accept", "application/json"),
        ("User-Agent", "MockClient/1.0"),
        ("X-Custom-Header", random_text(5, 30)),
    ]
    random.shuffle(header_pool)
    for name, value in header_pool[: random.randint(1, len(header_pool))]:
        headers.append(f"{name}: {value}")
    text = "\n".join(headers)
    if len(text) < min_len:
        text += "\n" + random_text(max(0, min_len - len(text) - 1), max(0, max_len - len(text) - 1))
    if len(text) > max_len:
        text = text[:max_len].rstrip()
    return text


def drop_database(db_path: Path) -> None:
    """Remove the existing database file and its WAL/SHM siblings."""
    for suffix in ("", "-wal", "-shm"):
        candidate = db_path.with_suffix(db_path.suffix + suffix) if suffix else db_path
        if candidate.exists():
            candidate.unlink()
            print(f"Removed {candidate}")


def ensure_schema(conn: sqlite3.Connection) -> None:
    """Create the schema if it does not exist."""
    conn.executescript(SCHEMA)
    conn.commit()


def create_hierarchy(conn: sqlite3.Connection) -> tuple[int, list[int]]:
    """Create a default profile, project and COLLECTION_COUNT collections. Returns project_id and collection ids."""
    cursor = conn.cursor()

    cursor.execute("INSERT INTO profiles (name) VALUES (?)", ("Default Profile",))
    profile_id = cursor.lastrowid

    cursor.execute(
        "INSERT INTO projects (profile_id, name) VALUES (?, ?)",
        (profile_id, "Default Project"),
    )
    project_id = cursor.lastrowid

    names = COLLECTION_NAMES[:COLLECTION_COUNT]
    while len(names) < COLLECTION_COUNT:
        names.append(f"Collection {len(names) + 1}")

    collection_ids = []
    for name in names:
        cursor.execute(
            "INSERT INTO collections (project_id, name) VALUES (?, ?)",
            (project_id, name),
        )
        collection_ids.append(cursor.lastrowid)

    conn.commit()
    print(f"Created profile, project and {len(collection_ids)} collection(s).")
    return project_id, collection_ids


def create_environments(conn: sqlite3.Connection, project_id: int) -> list[int]:
    """Create environments for the project with dummy variables. Returns environment ids."""
    cursor = conn.cursor()
    environment_ids = []

    for name in ENVIRONMENT_NAMES:
        cursor.execute(
            "INSERT INTO environments (project_id, name) VALUES (?, ?)",
            (project_id, name),
        )
        environment_id = cursor.lastrowid
        environment_ids.append(environment_id)

        # Create a random subset of variables for this environment.
        keys = random.sample(VARIABLE_KEYS, k=random.randint(3, len(VARIABLE_KEYS)))
        for key in keys:
            value = random_text(5, 60) if key not in ("TIMEOUT", "RETRY_COUNT", "LOG_LEVEL") else random.choice(["1000", "5000", "30", "60", "3", "5", "DEBUG", "INFO", "WARN"])
            cursor.execute(
                "INSERT INTO environment_variables (environment_id, key, value) VALUES (?, ?, ?)",
                (environment_id, key, value),
            )

    conn.commit()
    print(f"Created {len(environment_ids)} environment(s) with dummy variables.")
    return environment_ids


def insert_mock_data(conn: sqlite3.Connection, collection_ids: list[int]) -> tuple[int, int]:
    """Insert mock requests and responses. Returns (requests, responses)."""
    cursor = conn.cursor()
    total_requests = 0
    total_responses = 0
    status_cycle = iter(STATUS_CODES)

    for collection_id in collection_ids:
        request_count = random.randint(MIN_REQUESTS_PER_COLLECTION, MAX_REQUESTS_PER_COLLECTION)
        for i in range(request_count):
            field_min = random.randint(REQUEST_MIN_LEN, max(1, REQUEST_MAX_LEN // 4))
            field_max = random.randint(field_min, max(field_min, REQUEST_MAX_LEN))

            name = random_name(field_min, field_max)
            url = random_url(field_min, field_max)
            method = random.choice(METHODS)
            body = random_request_body(field_min, field_max)
            headers = random_headers(field_min, field_max, REQUEST_HEADERS_CHANCE)

            cursor.execute(
                """
                INSERT INTO http_requests (collection_id, name, url, method, body, request_headers, status_code, response_id)
                VALUES (?, ?, ?, ?, ?, ?, ?, ?)
                """,
                (collection_id, name, url, method, body, headers, 0, 0),
            )
            request_id = cursor.lastrowid
            total_requests += 1

            # Rotate through STATUS_CODES so every code appears at least once,
            # then fall back to random.
            try:
                resp_status = next(status_cycle)
            except StopIteration:
                resp_status = random.choice(STATUS_CODES)

            resp_field_min = random.randint(RESPONSE_MIN_LEN, max(1, RESPONSE_MAX_LEN // 4))
            resp_field_max = random.randint(resp_field_min, max(resp_field_min, RESPONSE_MAX_LEN))

            resp_headers = random_headers(resp_field_min, resp_field_max, RESPONSE_HEADERS_CHANCE)
            resp_body = random_response_body(resp_field_min, resp_field_max)

            cursor.execute(
                """
                INSERT INTO responses (request_id, headers, status_code, body)
                VALUES (?, ?, ?, ?)
                """,
                (request_id, resp_headers, resp_status, resp_body),
            )
            response_id = cursor.lastrowid
            total_responses += 1

            cursor.execute(
                "UPDATE http_requests SET status_code = ?, response_id = ? WHERE id = ?",
                (resp_status, response_id, request_id),
            )

            if total_requests % 500 == 0:
                conn.commit()
                print(f"  processed {total_requests} requests...")

    conn.commit()
    return total_requests, total_responses


def main() -> int:
    db_path = default_db_path().resolve()
    print(f"Database: {db_path}")
    print("Dropping existing database...")
    drop_database(db_path)

    print(
        f"Creating hierarchy and ~{COLLECTION_COUNT * (MIN_REQUESTS_PER_COLLECTION + MAX_REQUESTS_PER_COLLECTION) // 2} "
        "mock request(s) (one response each)..."
    )

    db_path.parent.mkdir(parents=True, exist_ok=True)

    conn = sqlite3.connect(db_path)
    try:
        ensure_schema(conn)
        project_id, collection_ids = create_hierarchy(conn)
        create_environments(conn, project_id)
        requests, responses = insert_mock_data(conn, collection_ids)
        print(f"Done. Inserted {requests} request(s), {responses} response(s), across {len(collection_ids)} collection(s).")
    finally:
        conn.close()

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
