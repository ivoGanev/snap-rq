#!/usr/bin/env python3
"""
Seed the Wails SQLite database with a dedicated "Mock Server" collection and a
large randomized load-test dataset for stress testing.

The script is idempotent: re-running it will not duplicate the mock collection or
its generated random collections. Use --force to drop and recreate everything.

Usage:
    python tools/seed_scale_test.py
    python tools/seed_scale_test.py --force
    python tools/seed_scale_test.py --random-collections 30 --requests-per-collection 500
"""

import argparse
import json
import os
import random
import sqlite3
import string
import sys
from pathlib import Path

MOCK_COLLECTION_NAME = "Mock Server"
MOCK_BASE_URL = "http://localhost:18080"
RANDOM_COLLECTION_PREFIX = "Random Load"
EDGE_CASE_COLLECTION_NAME = "Edge Cases"
DEFAULT_RANDOM_COLLECTION_COUNT = 30
DEFAULT_RANDOM_REQUESTS_PER_COLLECTION = 400

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

RANDOM_HTTP_METHODS = ["GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"]
RANDOM_STATUS_CODES = [200, 201, 202, 204, 400, 401, 403, 404, 409, 422, 500, 503]
RANDOM_COLLECTION_COLORS = [
    ("color", "#38bdf8"),
    ("color", "#a78bfa"),
    ("color", "#34d399"),
    ("color", "#f59e0b"),
    ("color", "#f472b6"),
    ("color", "#fb7185"),
]
RANDOM_RESOURCE_NAMES = [
    "users",
    "orders",
    "products",
    "invoices",
    "customers",
    "sessions",
    "reports",
    "inventory",
    "payments",
    "webhooks",
    "shipments",
    "accounts",
    "teams",
    "projects",
    "search",
    "notifications",
    "jobs",
]
RANDOM_ACTION_NAMES = [
    "list",
    "summary",
    "detail",
    "sync",
    "search",
    "query",
    "update",
    "create",
    "delete",
    "refresh",
    "preview",
    "publish",
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
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (request_id) REFERENCES http_requests(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_responses_request_id ON responses(request_id);
CREATE INDEX IF NOT EXISTS idx_responses_created_at ON responses(created_at);

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


def random_token(rng: random.Random, length: int = 12) -> str:
    """Generate a simple lowercase+digit token."""
    alphabet = string.ascii_lowercase + string.digits
    return "".join(rng.choice(alphabet) for _ in range(length))


def generate_random_headers(rng: random.Random) -> str:
    """Create a JSON object of random request headers."""
    headers = {
        "Accept": rng.choice([
            "application/json",
            "application/json, text/plain, */*",
            "application/xml",
            "text/plain",
            "*/*",
        ]),
        "Authorization": f"Bearer {random_token(rng, 24)}",
        "Content-Type": rng.choice([
            "application/json",
            "application/xml",
            "application/x-www-form-urlencoded",
            "text/plain",
        ]),
        "X-Trace-Id": f"trace-{random_token(rng, 12)}",
        "X-Request-Id": f"req-{rng.randint(1000, 99999999)}",
        "X-Tenant": f"tenant-{rng.randint(1, 42)}",
        "User-Agent": rng.choice([
            "SnapRQ/1.0",
            "Mozilla/5.0 (Windows NT 10.0; Win64; x64)",
            "curl/8.0.0",
            "PostmanRuntime/7.0",
        ]),
    }
    for _ in range(rng.randint(0, 3)):
        key = f"X-{rng.choice(['Env', 'Region', 'Region-Code', 'Tenant', 'Partition', 'Debug'])}-{rng.randint(1, 8)}"
        headers[key] = random_token(rng, 10)
    return json.dumps(headers, separators=(",", ":"))


def generate_random_body(method: str, rng: random.Random) -> str:
    """Generate a realistic payload for non-GET requests."""
    if method in {"GET", "DELETE", "OPTIONS"}:
        return ""

    if rng.random() < 0.5:
        payload = {
            "id": rng.randint(1000, 999999),
            "tenantId": rng.randint(1, 42),
            "status": rng.choice(["active", "pending", "queued", "archived"]),
            "createdAt": f"2026-10-{rng.randint(1, 28):02d}T{rng.randint(0, 23):02d}:{rng.randint(0, 59):02d}:00Z",
            "meta": {
                "requestId": random_token(rng, 12),
                "region": rng.choice(["us-east", "us-west", "eu-central", "ap-south"]),
            },
            "items": [
                {
                    "sku": f"SKU-{rng.randint(1000, 9999)}",
                    "qty": rng.randint(1, 8),
                    "price": round(rng.uniform(5.0, 999.99), 2),
                }
                for _ in range(rng.randint(1, 4))
            ],
        }
        return json.dumps(payload, separators=(",", ":"))

    return (
        "payload="
        + "&".join(
            f"{rng.choice(['filter', 'q', 'mode', 'page', 'sort', 'status'])}={random_token(rng, 8)}"
            for _ in range(rng.randint(2, 6))
        )
    )


def generate_random_response_headers(rng: random.Random) -> str:
    """Create realistic response headers for a fake API call."""
    headers = {
        "Content-Type": rng.choice(["application/json", "application/xml", "text/plain"]),
        "X-Request-Id": f"req-{rng.randint(1000, 99999999)}",
        "X-Response-Time": f"{rng.randint(14, 480)}ms",
        "Cache-Control": rng.choice(["no-cache", "private, max-age=60", "public, max-age=300"]),
    }
    if rng.random() < 0.4:
        headers["X-Trace-Id"] = f"trace-{random_token(rng, 12)}"
    return json.dumps(headers, separators=(",", ":"))


def generate_random_response_body(status_code: int, rng: random.Random) -> str:
    """Return a varied response body for a fake HTTP call."""
    if status_code >= 400:
        return json.dumps({
            "error": {"code": status_code, "message": rng.choice([
                "request rejected",
                "resource not found",
                "validation failed",
                "service unavailable",
                "permission denied",
            ])},
            "requestId": random_token(rng, 12),
        }, separators=(",", ":"))

    data = {
        "ok": True,
        "status": status_code,
        "requestId": random_token(rng, 12),
        "results": [
            {
                "id": rng.randint(1000, 999999),
                "name": rng.choice(["alpha", "beta", "gamma", "delta", "omega"]),
                "value": rng.randint(1, 5000),
            }
            for _ in range(rng.randint(1, 5))
        ],
    }
    return json.dumps(data, separators=(",", ":"))


def generate_random_request(collection_index: int, request_index: int, rng: random.Random) -> dict:
    """Build one randomized request object for a load-test collection."""
    method = rng.choice(RANDOM_HTTP_METHODS)
    resource = rng.choice(RANDOM_RESOURCE_NAMES)
    action = rng.choice(RANDOM_ACTION_NAMES)
    item_id = rng.randint(1, 9999)
    url = f"https://api-{rng.choice(['alpha', 'beta', 'gamma', 'west', 'east'])}.example.com/{resource}/{action}/{item_id}"
    if rng.random() < 0.5:
        url += f"?page={rng.randint(1, 25)}&filter={random_token(rng, 6)}"

    body = generate_random_body(method, rng)
    headers = generate_random_headers(rng)
    status_code = rng.choice(RANDOM_STATUS_CODES)
    response_headers = generate_random_response_headers(rng)
    response_body = generate_random_response_body(status_code, rng)

    request_name = f"{method} {resource} {action} {request_index + 1}"
    if collection_index % 2 == 0:
        request_name = f"{request_name} - {random_token(rng, 4)}"

    return {
        "name": request_name,
        "method": method,
        "url": url,
        "body": body,
        "headers": headers,
        "status_code": status_code,
        "response_headers": response_headers,
        "response_body": response_body,
    }


def generate_random_collections(count: int, requests_per_collection: int, rng: random.Random | None = None) -> list[dict]:
    """Return a list of random collection dictionaries to be inserted into SQLite."""
    rng = rng or random.Random()
    collection_payload = []
    for collection_index in range(1, count + 1):
        requests = []
        for request_index in range(requests_per_collection):
            requests.append(generate_random_request(collection_index, request_index, rng))
        collection_payload.append({
            "name": f"{RANDOM_COLLECTION_PREFIX} {collection_index:02d}",
            "appearance": RANDOM_COLLECTION_COLORS[(collection_index - 1) % len(RANDOM_COLLECTION_COLORS)],
            "requests": requests,
        })
    return collection_payload


def generate_edge_case_requests() -> list[dict]:
    """Return a small collection of intentionally broken requests for QA validation."""
    return [
        {
            "name": "Wrong content type",
            "method": "POST",
            "url": "https://api.example.com/users/create",
            "body": '{"name":"Ada","role":"admin"}',
            "headers": '{"Accept":"application/json"}',
            "status_code": 200,
            "response_headers": '{"Content-Type":"application/json"}',
            "response_body": '{"ok":true,"message":"content-type header is missing"}',
        },
        {
            "name": "Malformed JSON",
            "method": "PUT",
            "url": "https://api.example.com/orders/42",
            "body": '{"id":42,"items":[1,2,3}',
            "headers": '{"Content-Type":"application/json","Authorization":"Bearer invalid-token"}',
            "status_code": 400,
            "response_headers": '{"Content-Type":"application/json"}',
            "response_body": '{"error":{"code":400,"message":"malformed JSON body"}}',
        },
        {
            "name": "Missing auth header",
            "method": "DELETE",
            "url": "https://api.example.com/admin/users/99",
            "body": '',
            "headers": '{"Content-Type":"application/json"}',
            "status_code": 401,
            "response_headers": '{"Content-Type":"application/json"}',
            "response_body": '{"error":{"code":401,"message":"unauthorized"}}',
        },
        {
            "name": "Payload mismatch",
            "method": "PATCH",
            "url": "https://api.example.com/projects/7",
            "body": '"just a plain string instead of an object"',
            "headers": '{"Content-Type":"application/json","X-Request-Id":"abc123"}',
            "status_code": 422,
            "response_headers": '{"Content-Type":"application/json"}',
            "response_body": '{"error":{"code":422,"message":"payload schema mismatch"}}',
        },
        {
            "name": "Large invalid body",
            "method": "POST",
            "url": "https://api.example.com/uploads",
            "body": 'A' * 20000,
            "headers": '{"Content-Type":"text/plain","Authorization":"Bearer chunked-token"}',
            "status_code": 413,
            "response_headers": '{"Content-Type":"application/json"}',
            "response_body": '{"error":{"code":413,"message":"request body too large"}}',
        },
        {
            "name": "Wrong status code",
            "method": "GET",
            "url": "https://api.example.com/health",
            "body": '',
            "headers": '{"Accept":"application/json","X-Trace-Id":"trace-xyz"}',
            "status_code": 500,
            "response_headers": '{"Content-Type":"application/json"}',
            "response_body": '{"error":{"code":500,"message":"upstream timeout"}}',
        },
        {
            "name": "Malformed braces in body",
            "method": "POST",
            "url": "https://api.example.com/rules/parse",
            "body": '{"data": {{"nested": "broken"}}',
            "headers": '{"Content-Type":"application/json","X-Request-Id":"bad-braces-01"}',
            "status_code": 400,
            "response_headers": '{"Content-Type":"application/json"}',
            "response_body": '{"error":{"code":400,"message":"malformed braces in request body"}}',
        },
        {
            "name": "Large HTTP request name " + ("x" * 600),
            "method": "POST",
            "url": "https://api.example.com/large-name",
            "body": '{"message":"valid but long name should be preserved"}',
            "headers": '{"Content-Type":"application/json","X-Request-Id":"large-name-1"}',
            "status_code": 200,
            "response_headers": '{"Content-Type":"application/json"}',
            "response_body": '{"ok":true,"message":"large name accepted"}',
        },
        {
            "name": "Overlong URL",
            "method": "GET",
            "url": "https://api.example.com/" + ("segment/" * 200) + "?q=" + ("value-" * 200),
            "body": '',
            "headers": '{"Accept":"application/json"}',
            "status_code": 414,
            "response_headers": '{"Content-Type":"application/json"}',
            "response_body": '{"error":{"code":414,"message":"request URI too long"}}',
        },
    ]


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
    """Create the schema if it does not exist and apply additive migrations."""
    conn.executescript(SCHEMA)
    cursor = conn.cursor()
    cursor.execute("PRAGMA table_info(responses)")
    columns = {row[1] for row in cursor.fetchall()}
    if "created_at" not in columns:
        cursor.execute(
            "ALTER TABLE responses ADD COLUMN created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP"
        )
        cursor.execute("CREATE INDEX IF NOT EXISTS idx_responses_created_at ON responses(created_at)")
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


def find_random_collections(conn: sqlite3.Connection, project_id: int) -> set[str]:
    """Return all random collection names already seeded for the project."""
    cursor = conn.cursor()
    cursor.execute(
        "SELECT name FROM collections WHERE project_id = ? AND name LIKE ? ORDER BY name",
        (project_id, f"{RANDOM_COLLECTION_PREFIX}%"),
    )
    return {row[0] for row in cursor.fetchall()}


def find_edge_case_collection(conn: sqlite3.Connection, project_id: int) -> int | None:
    """Return the edge-case collection id if it exists, otherwise None."""
    cursor = conn.cursor()
    cursor.execute(
        "SELECT id FROM collections WHERE project_id = ? AND name = ?",
        (project_id, EDGE_CASE_COLLECTION_NAME),
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
        cursor.execute("DELETE FROM collection_appearances WHERE collection_id = ?", (collection_id,))
        cursor.execute("DELETE FROM collections WHERE id = ?", (collection_id,))
        conn.commit()
        print(f"Removed existing '{MOCK_COLLECTION_NAME}' collection.")


def delete_random_collections(conn: sqlite3.Connection, project_id: int) -> None:
    """Delete all random load-test collections for the project."""
    cursor = conn.cursor()
    cursor.execute(
        "SELECT id, name FROM collections WHERE project_id = ? AND name LIKE ?",
        (project_id, f"{RANDOM_COLLECTION_PREFIX}%"),
    )
    rows = cursor.fetchall()
    for collection_id, _ in rows:
        cursor.execute("DELETE FROM collection_appearances WHERE collection_id = ?", (collection_id,))
        cursor.execute("DELETE FROM collections WHERE id = ?", (collection_id,))
    conn.commit()
    if rows:
        print(f"Removed {len(rows)} random load collection(s).")


def delete_edge_case_collection(conn: sqlite3.Connection, project_id: int) -> None:
    """Delete the edge-case collection if it exists."""
    collection_id = find_edge_case_collection(conn, project_id)
    if collection_id is None:
        return
    cursor = conn.cursor()
    cursor.execute("DELETE FROM collection_appearances WHERE collection_id = ?", (collection_id,))
    cursor.execute("DELETE FROM collections WHERE id = ?", (collection_id,))
    conn.commit()
    print(f"Removed '{EDGE_CASE_COLLECTION_NAME}' collection.")


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


def create_random_collection(
    conn: sqlite3.Connection,
    project_id: int,
    collection_name: str,
    requests: list[dict],
    rng: random.Random,
) -> int:
    """Insert a single random collection and all of its requests/responses."""
    cursor = conn.cursor()
    cursor.execute(
        "INSERT INTO collections (project_id, name) VALUES (?, ?)",
        (project_id, collection_name),
    )
    collection_id = cursor.lastrowid

    appearance_type, appearance_value = rng.choice(RANDOM_COLLECTION_COLORS)
    cursor.execute(
        "INSERT INTO collection_appearances (collection_id, appearance_type, appearance_value) VALUES (?, ?, ?)",
        (collection_id, appearance_type, appearance_value),
    )

    for req in requests:
        cursor.execute(
            """
            INSERT INTO http_requests (
                collection_id, name, url, method, body, request_headers, status_code, response_id
            ) VALUES (?, ?, ?, ?, ?, ?, ?, 0)
            """,
            (
                collection_id,
                req["name"],
                req["url"],
                req["method"],
                req["body"],
                req["headers"],
                req["status_code"],
            ),
        )
        request_id = cursor.lastrowid
        cursor.execute(
            "INSERT INTO responses (request_id, headers, status_code, body, created_at) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)",
            (request_id, req["response_headers"], req["status_code"], req["response_body"]),
        )
        response_id = cursor.lastrowid
        cursor.execute(
            "UPDATE http_requests SET response_id = ? WHERE id = ?",
            (response_id, request_id),
        )

    conn.commit()
    return collection_id


def create_random_collections(
    conn: sqlite3.Connection,
    project_id: int,
    collection_count: int,
    requests_per_collection: int,
    rng: random.Random,
) -> tuple[int, int]:
    """Create any missing random collections and return created counts."""
    existing = find_random_collections(conn, project_id)
    created_collections = 0
    created_requests = 0

    for index in range(1, collection_count + 1):
        name = f"{RANDOM_COLLECTION_PREFIX} {index:02d}"
        if name in existing:
            continue

        payload = generate_random_collections(1, requests_per_collection, rng)[0]
        payload["name"] = name
        create_random_collection(conn, project_id, name, payload["requests"], rng)
        created_collections += 1
        created_requests += len(payload["requests"])
        existing.add(name)

    return created_collections, created_requests


def create_edge_case_collection(conn: sqlite3.Connection, project_id: int, rng: random.Random) -> int:
    """Insert a dedicated collection for malformed data and incorrect headers."""
    if find_edge_case_collection(conn, project_id) is not None:
        return 0

    cursor = conn.cursor()
    cursor.execute(
        "INSERT INTO collections (project_id, name) VALUES (?, ?)",
        (project_id, EDGE_CASE_COLLECTION_NAME),
    )
    collection_id = cursor.lastrowid
    cursor.execute(
        "INSERT INTO collection_appearances (collection_id, appearance_type, appearance_value) VALUES (?, ?, ?)",
        (collection_id, "color", "#ef4444"),
    )

    for req in generate_edge_case_requests():
        cursor.execute(
            """
            INSERT INTO http_requests (
                collection_id, name, url, method, body, request_headers, status_code, response_id
            ) VALUES (?, ?, ?, ?, ?, ?, ?, 0)
            """,
            (
                collection_id,
                req["name"],
                req["url"],
                req["method"],
                req["body"],
                req["headers"],
                req["status_code"],
            ),
        )
        request_id = cursor.lastrowid
        cursor.execute(
            "INSERT INTO responses (request_id, headers, status_code, body, created_at) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)",
            (request_id, req["response_headers"], req["status_code"], req["response_body"]),
        )
        response_id = cursor.lastrowid
        cursor.execute(
            "UPDATE http_requests SET response_id = ? WHERE id = ?",
            (response_id, request_id),
        )

    conn.commit()
    return collection_id


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Seed the Snap RQ database with a Mock Server collection and a large randomized load-test dataset."
    )
    parser.add_argument(
        "--force",
        action="store_true",
        help="Remove and recreate the mock collection and all random load-test collections.",
    )
    parser.add_argument(
        "--random-collections",
        type=int,
        default=DEFAULT_RANDOM_COLLECTION_COUNT,
        help=f"Number of randomized load-test collections to create (default: {DEFAULT_RANDOM_COLLECTION_COUNT}).",
    )
    parser.add_argument(
        "--requests-per-collection",
        type=int,
        default=DEFAULT_RANDOM_REQUESTS_PER_COLLECTION,
        help=f"Number of randomized requests per collection (default: {DEFAULT_RANDOM_REQUESTS_PER_COLLECTION}).",
    )
    parser.add_argument(
        "--seed",
        type=int,
        default=1337,
        help="Random seed for reproducible large-load data generation.",
    )
    args = parser.parse_args()

    if args.random_collections < 1:
        parser.error("--random-collections must be at least 1")
    if args.requests_per_collection < 1:
        parser.error("--requests-per-collection must be at least 1")

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
        if existing_id is None or args.force:
            collection_id = create_mock_collection(conn, project_id)
            requests = create_mock_requests(conn, collection_id)
            print(f"Done. Inserted {requests} mock request(s) into '{MOCK_COLLECTION_NAME}'.")

        if args.force:
            delete_random_collections(conn, project_id)
            delete_edge_case_collection(conn, project_id)

        rng = random.Random(args.seed)
        created_collections, created_requests = create_random_collections(
            conn,
            project_id,
            args.random_collections,
            args.requests_per_collection,
            rng,
        )
        if created_collections:
            print(
                f"Created {created_collections} random load collection(s) with {created_requests} request(s)."
            )
        else:
            print(
                f"Random load collections already exist. "
                f"Target count: {args.random_collections}, requests per collection: {args.requests_per_collection}."
            )

        edge_collection_id = create_edge_case_collection(conn, project_id, rng)
        if edge_collection_id:
            print(f"Created '{EDGE_CASE_COLLECTION_NAME}' collection with edge-case request payloads.")
        else:
            print(f"'{EDGE_CASE_COLLECTION_NAME}' collection already exists.")
    finally:
        conn.close()

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
