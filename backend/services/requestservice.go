package services

import (
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"snap-rq/backend/models"
)

var interpolationRegex = regexp.MustCompile(`{{\s*([a-zA-Z0-9_-]+)\s*}}`)

// RequestService provides CRUD operations for saved HTTP requests.
type RequestService struct {
	db *sql.DB
}

// NewRequestService returns a RequestService backed by the given database.
func NewRequestService(db *sql.DB) *RequestService {
	return &RequestService{db: db}
}

// DuplicateRequest creates a copy of an existing request, including its tags and
// favourite memberships. The copy receives a new ID, a "(copy)" name suffix and
// a fresh status/response.
func (s *RequestService) DuplicateRequest(id int64) (models.HttpRequest, error) {
	original, err := s.GetRequest(id)
	if err != nil {
		return models.HttpRequest{}, fmt.Errorf("loading original request: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return models.HttpRequest{}, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.Exec(
		`INSERT INTO http_requests (collection_id, name, url, method, body, request_headers, status_code, response_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		original.CollectionID, original.Name+" (copy)", original.URL, original.Method, original.Body,
		original.RequestHeaders, 0, 0,
	)
	if err != nil {
		return models.HttpRequest{}, fmt.Errorf("duplicating request: %w", err)
	}

	newID, err := result.LastInsertId()
	if err != nil {
		return models.HttpRequest{}, fmt.Errorf("getting duplicated request id: %w", err)
	}

	_, err = tx.Exec(
		`INSERT INTO request_tags (request_id, tag_id)
		 SELECT ?, tag_id FROM request_tags WHERE request_id = ?`,
		newID, id,
	)
	if err != nil {
		return models.HttpRequest{}, fmt.Errorf("copying tags: %w", err)
	}

	_, err = tx.Exec(
		`INSERT INTO favourite_items (favourite_collection_id, http_request_id, sort_order)
		 SELECT favourite_collection_id, ?, sort_order FROM favourite_items WHERE http_request_id = ?`,
		newID, id,
	)
	if err != nil {
		return models.HttpRequest{}, fmt.Errorf("copying favourites: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return models.HttpRequest{}, fmt.Errorf("committing duplicate: %w", err)
	}

	return s.GetRequest(newID)
}

// CreateRequest saves a new HTTP request and returns it with its generated ID.
func (s *RequestService) CreateRequest(req models.HttpRequest) (models.HttpRequest, error) {
	if req.CollectionID == 0 {
		return models.HttpRequest{}, fmt.Errorf("collection id is required")
	}

	result, err := s.db.Exec(
		`INSERT INTO http_requests (collection_id, name, url, method, body, request_headers, status_code, response_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		req.CollectionID, req.Name, req.URL, req.Method, req.Body, req.RequestHeaders, req.StatusCode, req.ResponseID,
	)
	if err != nil {
		return models.HttpRequest{}, fmt.Errorf("creating request: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return models.HttpRequest{}, fmt.Errorf("getting last insert id: %w", err)
	}

	return s.GetRequest(id)
}

// GetRequest retrieves a single HTTP request by ID.
func (s *RequestService) GetRequest(id int64) (models.HttpRequest, error) {
	var req models.HttpRequest
	row := s.db.QueryRow(
		`SELECT hr.id, hr.collection_id, c.project_id, hr.name, hr.url, hr.method, hr.body, hr.request_headers, hr.status_code, hr.response_id
		 FROM http_requests hr
		 JOIN collections c ON c.id = hr.collection_id
		 WHERE hr.id = ?`,
		id,
	)
	err := row.Scan(&req.ID, &req.CollectionID, &req.ProjectID, &req.Name, &req.URL, &req.Method, &req.Body, &req.RequestHeaders, &req.StatusCode, &req.ResponseID)
	if err != nil {
		if err == sql.ErrNoRows {
			return models.HttpRequest{}, fmt.Errorf("request not found")
		}
		return models.HttpRequest{}, fmt.Errorf("getting request: %w", err)
	}
	return req, nil
}

// ExecuteRequest runs the saved HTTP request and returns the response data.
// The response is not persisted; callers are responsible for creating/updating
// the response record (e.g. via CreateResponse/UpdateResponse). Network or
// client errors are captured as a response with the error message in the body
// and status code 0.
// If environmentID is non-zero, any {{variable_name}} placeholders in the URL,
// headers and body are interpolated using variables from that environment.
func (s *RequestService) ExecuteRequest(id int64, environmentID int64) (models.HttpResponse, error) {
	req, err := s.GetRequest(id)
	if err != nil {
		return models.HttpResponse{}, fmt.Errorf("loading request: %w", err)
	}

	variables, err := s.loadVariables(environmentID)
	if err != nil {
		return models.HttpResponse{}, fmt.Errorf("loading variables: %w", err)
	}

	req.URL = interpolate(req.URL, variables)
	req.Body = interpolate(req.Body, variables)
	req.RequestHeaders = interpolate(req.RequestHeaders, variables)

	fmt.Printf("[ExecuteRequest] id=%d envID=%d url=%q body=%q\n", id, environmentID, req.URL, req.Body)

	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}

	var bodyReader io.Reader
	if req.Body != "" {
		bodyReader = strings.NewReader(req.Body)
	}

	httpReq, err := http.NewRequest(method, req.URL, bodyReader)
	if err != nil {
		return models.HttpResponse{
			RequestID:  id,
			StatusCode: 0,
			Body:       fmt.Errorf("building request: %w", err).Error(),
			DurationMs: 0,
		}, nil
	}

	for _, line := range strings.Split(req.RequestHeaders, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		httpReq.Header.Set(strings.TrimSpace(key), strings.TrimSpace(value))
	}

	client := &http.Client{Timeout: 30 * time.Second}
	start := time.Now()
	httpResp, err := client.Do(httpReq)
	duration := time.Since(start).Milliseconds()
	if err != nil {
		return models.HttpResponse{
			RequestID:  id,
			StatusCode: 0,
			Body:       fmt.Errorf("request failed: %w", err).Error(),
			DurationMs: duration,
		}, nil
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return models.HttpResponse{
			RequestID:  id,
			StatusCode: 0,
			Body:       fmt.Errorf("reading response body: %w", err).Error(),
			DurationMs: duration,
		}, nil
	}

	var respHeaders strings.Builder
	for name, values := range httpResp.Header {
		for _, value := range values {
			respHeaders.WriteString(fmt.Sprintf("%s: %s\n", name, value))
		}
	}

	return models.HttpResponse{
		RequestID:  id,
		StatusCode: httpResp.StatusCode,
		Headers:    strings.TrimSpace(respHeaders.String()),
		Body:       string(respBody),
		DurationMs: duration,
	}, nil
}

func (s *RequestService) loadVariables(environmentID int64) (map[string]string, error) {
	if environmentID == 0 {
		return nil, nil
	}

	rows, err := s.db.Query(
		`SELECT key, value FROM environment_variables WHERE environment_id = ?`,
		environmentID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying variables: %w", err)
	}
	defer rows.Close()

	variables := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("scanning variable: %w", err)
		}
		variables[key] = value
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating variables: %w", err)
	}

	return variables, nil
}

func interpolate(input string, variables map[string]string) string {
	if variables == nil {
		return input
	}

	return interpolationRegex.ReplaceAllStringFunc(input, func(match string) string {
		name := interpolationRegex.FindStringSubmatch(match)[1]
		if value, ok := variables[name]; ok {
			return value
		}
		return match
	})
}

// GetAllRequests returns all saved HTTP requests ordered by name.
func (s *RequestService) GetAllRequests() ([]models.HttpRequest, error) {
	rows, err := s.db.Query(
		`SELECT hr.id, hr.collection_id, c.project_id, hr.name, hr.url, hr.method, hr.body, hr.request_headers, hr.status_code, hr.response_id
		 FROM http_requests hr
		 JOIN collections c ON c.id = hr.collection_id
		 ORDER BY hr.name`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing requests: %w", err)
	}
	defer rows.Close()

	var requests []models.HttpRequest
	for rows.Next() {
		var req models.HttpRequest
		if err := rows.Scan(&req.ID, &req.CollectionID, &req.ProjectID, &req.Name, &req.URL, &req.Method, &req.Body, &req.RequestHeaders, &req.StatusCode, &req.ResponseID); err != nil {
			return nil, fmt.Errorf("scanning request: %w", err)
		}
		requests = append(requests, req)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating requests: %w", err)
	}

	return requests, nil
}

// GetRequestsForCollection returns all requests belonging to a collection.
func (s *RequestService) GetRequestsForCollection(collectionID int64) ([]models.HttpRequest, error) {
	rows, err := s.db.Query(
		`SELECT hr.id, hr.collection_id, c.project_id, hr.name, hr.url, hr.method, hr.body, hr.request_headers, hr.status_code, hr.response_id
		 FROM http_requests hr
		 JOIN collections c ON c.id = hr.collection_id
		 WHERE hr.collection_id = ?
		 ORDER BY hr.name`,
		collectionID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing requests: %w", err)
	}
	defer rows.Close()

	var requests []models.HttpRequest
	for rows.Next() {
		var req models.HttpRequest
		if err := rows.Scan(&req.ID, &req.CollectionID, &req.ProjectID, &req.Name, &req.URL, &req.Method, &req.Body, &req.RequestHeaders, &req.StatusCode, &req.ResponseID); err != nil {
			return nil, fmt.Errorf("scanning request: %w", err)
		}
		requests = append(requests, req)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating requests: %w", err)
	}

	return requests, nil
}

// GetRequestsForProject returns every HTTP request across all collections in a project.
func (s *RequestService) GetRequestsForProject(projectID int64) ([]models.HttpRequest, error) {
	rows, err := s.db.Query(
		`SELECT hr.id, hr.collection_id, c.project_id, hr.name, hr.url, hr.method, hr.body, hr.request_headers, hr.status_code, hr.response_id
		 FROM http_requests hr
		 JOIN collections c ON c.id = hr.collection_id
		 WHERE c.project_id = ?
		 ORDER BY hr.name`,
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing project requests: %w", err)
	}
	defer rows.Close()

	var requests []models.HttpRequest
	for rows.Next() {
		var req models.HttpRequest
		if err := rows.Scan(&req.ID, &req.CollectionID, &req.ProjectID, &req.Name, &req.URL, &req.Method, &req.Body, &req.RequestHeaders, &req.StatusCode, &req.ResponseID); err != nil {
			return nil, fmt.Errorf("scanning request: %w", err)
		}
		requests = append(requests, req)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating project requests: %w", err)
	}

	return requests, nil
}

// GetAllRequestSummaries returns a lightweight projection of all saved HTTP
// requests ordered by name. It omits body and headers to keep IPC payloads small.
func (s *RequestService) GetAllRequestSummaries() ([]models.HttpRequestSummary, error) {
	rows, err := s.db.Query(
		`SELECT hr.id, hr.collection_id, c.project_id, hr.name, hr.url, hr.method, hr.status_code, hr.response_id
		 FROM http_requests hr
		 JOIN collections c ON c.id = hr.collection_id
		 ORDER BY hr.name`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing request summaries: %w", err)
	}
	defer rows.Close()

	var requests []models.HttpRequestSummary
	for rows.Next() {
		var req models.HttpRequestSummary
		if err := rows.Scan(&req.ID, &req.CollectionID, &req.ProjectID, &req.Name, &req.URL, &req.Method, &req.StatusCode, &req.ResponseID); err != nil {
			return nil, fmt.Errorf("scanning request summary: %w", err)
		}
		requests = append(requests, req)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating request summaries: %w", err)
	}

	return requests, nil
}

// GetRequestSummariesForCollection returns a lightweight projection of all
// requests belonging to a collection.
func (s *RequestService) GetRequestSummariesForCollection(collectionID int64) ([]models.HttpRequestSummary, error) {
	rows, err := s.db.Query(
		`SELECT hr.id, hr.collection_id, c.project_id, hr.name, hr.url, hr.method, hr.status_code, hr.response_id
		 FROM http_requests hr
		 JOIN collections c ON c.id = hr.collection_id
		 WHERE hr.collection_id = ?
		 ORDER BY hr.name`,
		collectionID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing request summaries: %w", err)
	}
	defer rows.Close()

	var requests []models.HttpRequestSummary
	for rows.Next() {
		var req models.HttpRequestSummary
		if err := rows.Scan(&req.ID, &req.CollectionID, &req.ProjectID, &req.Name, &req.URL, &req.Method, &req.StatusCode, &req.ResponseID); err != nil {
			return nil, fmt.Errorf("scanning request summary: %w", err)
		}
		requests = append(requests, req)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating request summaries: %w", err)
	}

	return requests, nil
}

// GetRequestSummariesForProject returns a lightweight projection of every HTTP
// request across all collections in a project.
func (s *RequestService) GetRequestSummariesForProject(projectID int64) ([]models.HttpRequestSummary, error) {
	rows, err := s.db.Query(
		`SELECT hr.id, hr.collection_id, c.project_id, hr.name, hr.url, hr.method, hr.status_code, hr.response_id
		 FROM http_requests hr
		 JOIN collections c ON c.id = hr.collection_id
		 WHERE c.project_id = ?
		 ORDER BY hr.name`,
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing project request summaries: %w", err)
	}
	defer rows.Close()

	var requests []models.HttpRequestSummary
	for rows.Next() {
		var req models.HttpRequestSummary
		if err := rows.Scan(&req.ID, &req.CollectionID, &req.ProjectID, &req.Name, &req.URL, &req.Method, &req.StatusCode, &req.ResponseID); err != nil {
			return nil, fmt.Errorf("scanning request summary: %w", err)
		}
		requests = append(requests, req)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating project request summaries: %w", err)
	}

	return requests, nil
}

// UpdateRequest updates an existing HTTP request.
func (s *RequestService) UpdateRequest(req models.HttpRequest) (models.HttpRequest, error) {
	if req.ID == 0 {
		return models.HttpRequest{}, fmt.Errorf("request id is required")
	}
	if req.CollectionID == 0 {
		return models.HttpRequest{}, fmt.Errorf("collection id is required")
	}

	_, err := s.db.Exec(
		`UPDATE http_requests
		 SET collection_id = ?, name = ?, url = ?, method = ?, body = ?, request_headers = ?, status_code = ?, response_id = ?
		 WHERE id = ?`,
		req.CollectionID, req.Name, req.URL, req.Method, req.Body, req.RequestHeaders, req.StatusCode, req.ResponseID, req.ID,
	)
	if err != nil {
		return models.HttpRequest{}, fmt.Errorf("updating request: %w", err)
	}

	return s.GetRequest(req.ID)
}

// DeleteRequest moves an HTTP request to the trash bin.
func (s *RequestService) DeleteRequest(id int64) error {
	_, err := s.BinRequest(id)
	return err
}

// BulkDeleteRequests moves multiple HTTP requests to the trash bin.
func (s *RequestService) BulkDeleteRequests(ids []int64) error {
	_, err := s.BinRequests(ids)
	return err
}

// RequestToCurl converts an HttpRequest into an equivalent curl command string.
func (s *RequestService) RequestToCurl(req models.HttpRequest) string {
	return RequestToCurl(req)
}

// CurlToRequest parses a curl command string and returns an HttpRequest with
// the given collection ID attached. The request is not persisted.
func (s *RequestService) CurlToRequest(collectionID int64, curl string) (models.HttpRequest, error) {
	return CurlToRequest(collectionID, curl)
}

// CreateResponse saves a new response for a request and returns it with its generated ID.
func (s *RequestService) CreateResponse(resp models.HttpResponse) (models.HttpResponse, error) {
	if resp.CreatedAt == "" {
		resp.CreatedAt = time.Now().UTC().Format("2006-01-02 15:04:05")
	}

	result, err := s.db.Exec(
		`INSERT INTO responses (request_id, headers, status_code, body, created_at, duration_ms)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		resp.RequestID, resp.Headers, resp.StatusCode, resp.Body, resp.CreatedAt, resp.DurationMs,
	)
	if err != nil {
		return models.HttpResponse{}, fmt.Errorf("creating response: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return models.HttpResponse{}, fmt.Errorf("getting last insert id: %w", err)
	}

	resp.ID = id
	return s.GetResponse(id)
}

// UpdateResponse updates an existing response record.
func (s *RequestService) UpdateResponse(resp models.HttpResponse) (models.HttpResponse, error) {
	if resp.ID == 0 {
		return models.HttpResponse{}, fmt.Errorf("response id is required")
	}

	_, err := s.db.Exec(
		`UPDATE responses SET request_id = ?, headers = ?, status_code = ?, body = ?, created_at = ?, duration_ms = ? WHERE id = ?`,
		resp.RequestID, resp.Headers, resp.StatusCode, resp.Body, resp.CreatedAt, resp.DurationMs, resp.ID,
	)
	if err != nil {
		return models.HttpResponse{}, fmt.Errorf("updating response: %w", err)
	}

	return s.GetResponse(resp.ID)
}

// GetResponse retrieves a single response by ID.
func (s *RequestService) GetResponse(id int64) (models.HttpResponse, error) {
	var resp models.HttpResponse
	row := s.db.QueryRow(
		`SELECT id, request_id, headers, status_code, body, created_at, duration_ms FROM responses WHERE id = ?`,
		id,
	)
	err := row.Scan(&resp.ID, &resp.RequestID, &resp.Headers, &resp.StatusCode, &resp.Body, &resp.CreatedAt, &resp.DurationMs)
	if err != nil {
		if err == sql.ErrNoRows {
			return models.HttpResponse{}, fmt.Errorf("response not found")
		}
		return models.HttpResponse{}, fmt.Errorf("getting response: %w", err)
	}
	return resp, nil
}

// GetResponsesForRequest returns all responses for a given request ID, newest first.
func (s *RequestService) GetResponsesForRequest(requestID int64) ([]models.HttpResponse, error) {
	rows, err := s.db.Query(
		`SELECT id, request_id, headers, status_code, body, created_at, duration_ms
		 FROM responses
		 WHERE request_id = ?
		 ORDER BY created_at DESC, id DESC`,
		requestID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing responses: %w", err)
	}
	defer rows.Close()

	var responses []models.HttpResponse
	for rows.Next() {
		var resp models.HttpResponse
		if err := rows.Scan(&resp.ID, &resp.RequestID, &resp.Headers, &resp.StatusCode, &resp.Body, &resp.CreatedAt, &resp.DurationMs); err != nil {
			return nil, fmt.Errorf("scanning response: %w", err)
		}
		responses = append(responses, resp)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating responses: %w", err)
	}

	return responses, nil
}

// DeleteResponse removes a response by ID.
func (s *RequestService) DeleteResponse(id int64) error {
	_, err := s.db.Exec(`DELETE FROM responses WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting response: %w", err)
	}
	return nil
}

// SaveResponseToFile writes a response's body to the given file path.
func (s *RequestService) SaveResponseToFile(responseID int64, filePath string) error {
	resp, err := s.GetResponse(responseID)
	if err != nil {
		return fmt.Errorf("getting response: %w", err)
	}
	if err := os.WriteFile(filePath, []byte(resp.Body), 0644); err != nil {
		return fmt.Errorf("writing response file: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Trash bin
// ---------------------------------------------------------------------------

func (s *RequestService) getRequestWithProjectTx(tx *sql.Tx, id int64) (models.HttpRequest, error) {
	var req models.HttpRequest
	row := tx.QueryRow(`
		SELECT hr.id, hr.collection_id, c.project_id, hr.name, hr.url, hr.method, hr.body, hr.request_headers, hr.status_code, hr.response_id
		FROM http_requests hr
		JOIN collections c ON c.id = hr.collection_id
		WHERE hr.id = ?`, id)
	err := row.Scan(&req.ID, &req.CollectionID, &req.ProjectID, &req.Name, &req.URL, &req.Method, &req.Body, &req.RequestHeaders, &req.StatusCode, &req.ResponseID)
	if err != nil {
		if err == sql.ErrNoRows {
			return models.HttpRequest{}, fmt.Errorf("request not found")
		}
		return models.HttpRequest{}, fmt.Errorf("loading request: %w", err)
	}
	return req, nil
}

func (s *RequestService) getBinnedResponsesTx(tx *sql.Tx, requestID int64) ([]models.BinnedResponseSnapshot, error) {
	rows, err := tx.Query(`
		SELECT id, headers, status_code, body, created_at, duration_ms
		FROM responses
		WHERE request_id = ?
		ORDER BY id`, requestID)
	if err != nil {
		return nil, fmt.Errorf("loading responses: %w", err)
	}
	defer rows.Close()

	var snapshots []models.BinnedResponseSnapshot
	for rows.Next() {
		var snap models.BinnedResponseSnapshot
		if err := rows.Scan(&snap.ID, &snap.Headers, &snap.StatusCode, &snap.Body, &snap.CreatedAt, &snap.DurationMs); err != nil {
			return nil, fmt.Errorf("scanning response: %w", err)
		}
		snapshots = append(snapshots, snap)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating responses: %w", err)
	}
	return snapshots, nil
}

func (s *RequestService) getBinnedTagsTx(tx *sql.Tx, requestID int64) ([]models.BinnedTagSnapshot, error) {
	rows, err := tx.Query(`
		SELECT t.id, t.name
		FROM request_tags rt
		JOIN tags t ON t.id = rt.tag_id
		WHERE rt.request_id = ?
		ORDER BY t.name`, requestID)
	if err != nil {
		return nil, fmt.Errorf("loading tags: %w", err)
	}
	defer rows.Close()

	var snapshots []models.BinnedTagSnapshot
	for rows.Next() {
		var snap models.BinnedTagSnapshot
		if err := rows.Scan(&snap.ID, &snap.Name); err != nil {
			return nil, fmt.Errorf("scanning tag: %w", err)
		}
		snapshots = append(snapshots, snap)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating tags: %w", err)
	}
	return snapshots, nil
}

func (s *RequestService) getBinnedFavouritesTx(tx *sql.Tx, requestID int64) ([]models.BinnedFavouriteSnapshot, error) {
	rows, err := tx.Query(`
		SELECT fc.id, fc.name
		FROM favourite_items fi
		JOIN favourite_collections fc ON fc.id = fi.favourite_collection_id
		WHERE fi.http_request_id = ?
		ORDER BY fc.name`, requestID)
	if err != nil {
		return nil, fmt.Errorf("loading favourites: %w", err)
	}
	defer rows.Close()

	var snapshots []models.BinnedFavouriteSnapshot
	for rows.Next() {
		var snap models.BinnedFavouriteSnapshot
		if err := rows.Scan(&snap.ID, &snap.Name); err != nil {
			return nil, fmt.Errorf("scanning favourite: %w", err)
		}
		snapshots = append(snapshots, snap)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating favourites: %w", err)
	}
	return snapshots, nil
}

// binRequestTx moves a single request into the trash bin inside an existing
// transaction. It returns the generated bin row id.
func (s *RequestService) binRequestTx(tx *sql.Tx, id int64) (int64, error) {
	req, err := s.getRequestWithProjectTx(tx, id)
	if err != nil {
		return 0, err
	}

	responses, err := s.getBinnedResponsesTx(tx, id)
	if err != nil {
		return 0, err
	}

	tags, err := s.getBinnedTagsTx(tx, id)
	if err != nil {
		return 0, err
	}

	favourites, err := s.getBinnedFavouritesTx(tx, id)
	if err != nil {
		return 0, err
	}

	snapshot := models.BinnedRequestSnapshot{
		Responses:  responses,
		Tags:       tags,
		Favourites: favourites,
	}
	snapshotJSON, err := snapshot.MarshalJSON()
	if err != nil {
		return 0, fmt.Errorf("serialising snapshot: %w", err)
	}

	var collectionName string
	if err := tx.QueryRow("SELECT name FROM collections WHERE id = ?", req.CollectionID).Scan(&collectionName); err != nil {
		collectionName = ""
	}

	result, err := tx.Exec(`
		INSERT INTO binned_requests (
			original_request_id, project_id, collection_id, original_collection_id, original_collection_name,
			name, url, method, body, request_headers, status_code, response_id, snapshot_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.ID, req.ProjectID, req.CollectionID, req.CollectionID, collectionName,
		req.Name, req.URL, req.Method, req.Body, req.RequestHeaders, req.StatusCode, req.ResponseID, snapshotJSON)
	if err != nil {
		return 0, fmt.Errorf("inserting binned request: %w", err)
	}

	binID, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("getting bin id: %w", err)
	}

	if _, err := tx.Exec(`DELETE FROM http_requests WHERE id = ?`, id); err != nil {
		return 0, fmt.Errorf("deleting original request: %w", err)
	}

	return binID, nil
}

// BinRequest moves a single request and its related data into the trash bin.
// It returns the generated bin row id.
func (s *RequestService) BinRequest(id int64) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("beginning bin transaction: %w", err)
	}
	defer tx.Rollback()

	binID, err := s.binRequestTx(tx, id)
	if err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("committing bin transaction: %w", err)
	}
	return binID, nil
}

// BinRequests moves multiple requests into the trash bin atomically. It returns
// the bin row ids in the same order as the input ids.
func (s *RequestService) BinRequests(ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning bulk bin transaction: %w", err)
	}
	defer tx.Rollback()

	binIDs := make([]int64, 0, len(ids))
	for _, id := range ids {
		binID, err := s.binRequestTx(tx, id)
		if err != nil {
			return nil, fmt.Errorf("binning request %d: %w", id, err)
		}
		binIDs = append(binIDs, binID)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing bulk bin transaction: %w", err)
	}
	return binIDs, nil
}

// GetBinnedRequestsForProject returns every binned request for a project,
// newest first.
func (s *RequestService) GetBinnedRequestsForProject(projectID int64) ([]models.HttpBinnedRequestSummary, error) {
	rows, err := s.db.Query(`
		SELECT id, original_request_id, project_id, collection_id, original_collection_id, original_collection_name,
		       name, url, method, status_code, response_id, deleted_at
		FROM binned_requests
		WHERE project_id = ?
		ORDER BY deleted_at DESC, id DESC`, projectID)
	if err != nil {
		return nil, fmt.Errorf("listing binned requests: %w", err)
	}
	defer rows.Close()

	var summaries []models.HttpBinnedRequestSummary
	for rows.Next() {
		var summary models.HttpBinnedRequestSummary
		if err := rows.Scan(
			&summary.ID, &summary.OriginalRequestID, &summary.ProjectID, &summary.CollectionID,
			&summary.OriginalCollectionID, &summary.OriginalCollectionName,
			&summary.Name, &summary.URL, &summary.Method, &summary.StatusCode,
			&summary.ResponseID, &summary.DeletedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning binned request: %w", err)
		}
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating binned requests: %w", err)
	}
	return summaries, nil
}

// RestoreBinnedRequest restores a binned request into the given collection.
// The request is recreated with a new id and any still-existing tags and
// favourites are re-attached.
func (s *RequestService) RestoreBinnedRequest(binID int64, targetCollectionID int64) (models.HttpRequest, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return models.HttpRequest{}, fmt.Errorf("beginning restore transaction: %w", err)
	}
	defer tx.Rollback()

	var targetProjectID int64
	if err := tx.QueryRow("SELECT project_id FROM collections WHERE id = ?", targetCollectionID).Scan(&targetProjectID); err != nil {
		if err == sql.ErrNoRows {
			return models.HttpRequest{}, fmt.Errorf("target collection not found")
		}
		return models.HttpRequest{}, fmt.Errorf("loading target collection: %w", err)
	}

	var binned struct {
		ProjectID       int64
		Name            string
		URL             string
		Method          string
		Body            string
		RequestHeaders  string
		StatusCode      int
		ResponseID      int64
		SnapshotJSON    string
	}
	if err := tx.QueryRow(`
		SELECT project_id, name, url, method, body, request_headers, status_code, response_id, snapshot_json
		FROM binned_requests
		WHERE id = ?`, binID).Scan(
		&binned.ProjectID, &binned.Name, &binned.URL, &binned.Method,
		&binned.Body, &binned.RequestHeaders, &binned.StatusCode,
		&binned.ResponseID, &binned.SnapshotJSON,
	); err != nil {
		if err == sql.ErrNoRows {
			return models.HttpRequest{}, fmt.Errorf("binned request not found")
		}
		return models.HttpRequest{}, fmt.Errorf("loading binned request: %w", err)
	}

	if binned.ProjectID != targetProjectID {
		return models.HttpRequest{}, fmt.Errorf("target collection does not belong to the binned request's project")
	}

	snapshot, err := models.UnmarshalBinnedRequestSnapshot(binned.SnapshotJSON)
	if err != nil {
		return models.HttpRequest{}, fmt.Errorf("parsing snapshot: %w", err)
	}

	result, err := tx.Exec(`
		INSERT INTO http_requests (collection_id, name, url, method, body, request_headers, status_code, response_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		targetCollectionID, binned.Name, binned.URL, binned.Method,
		binned.Body, binned.RequestHeaders, binned.StatusCode, 0)
	if err != nil {
		return models.HttpRequest{}, fmt.Errorf("restoring request: %w", err)
	}
	newRequestID, err := result.LastInsertId()
	if err != nil {
		return models.HttpRequest{}, fmt.Errorf("getting restored request id: %w", err)
	}

	responseIDMap := make(map[int64]int64)
	for _, resp := range snapshot.Responses {
		res, err := tx.Exec(`
			INSERT INTO responses (request_id, headers, status_code, body, created_at, duration_ms)
			VALUES (?, ?, ?, ?, ?, ?)`,
			newRequestID, resp.Headers, resp.StatusCode, resp.Body, resp.CreatedAt, resp.DurationMs)
		if err != nil {
			return models.HttpRequest{}, fmt.Errorf("restoring response: %w", err)
		}
		newResponseID, err := res.LastInsertId()
		if err != nil {
			return models.HttpRequest{}, fmt.Errorf("getting restored response id: %w", err)
		}
		responseIDMap[resp.ID] = newResponseID
	}

	if binned.ResponseID != 0 {
		if newResponseID, ok := responseIDMap[binned.ResponseID]; ok {
			if _, err := tx.Exec(`UPDATE http_requests SET response_id = ? WHERE id = ?`, newResponseID, newRequestID); err != nil {
				return models.HttpRequest{}, fmt.Errorf("updating response_id: %w", err)
			}
		}
	}

	for _, tag := range snapshot.Tags {
		var tagID int64
		err := tx.QueryRow(`SELECT id FROM tags WHERE id = ? AND project_id = ?`, tag.ID, targetProjectID).Scan(&tagID)
		if err != nil {
			if err != sql.ErrNoRows {
				return models.HttpRequest{}, fmt.Errorf("looking up tag: %w", err)
			}
			err = tx.QueryRow(`SELECT id FROM tags WHERE name = ? AND project_id = ?`, tag.Name, targetProjectID).Scan(&tagID)
			if err != nil {
				if err != sql.ErrNoRows {
					return models.HttpRequest{}, fmt.Errorf("looking up tag by name: %w", err)
				}
				continue
			}
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO request_tags (request_id, tag_id) VALUES (?, ?)`, newRequestID, tagID); err != nil {
			return models.HttpRequest{}, fmt.Errorf("reattaching tag: %w", err)
		}
	}

	for _, fav := range snapshot.Favourites {
		var favID int64
		err := tx.QueryRow(`SELECT id FROM favourite_collections WHERE id = ? AND project_id = ?`, fav.ID, targetProjectID).Scan(&favID)
		if err != nil {
			if err != sql.ErrNoRows {
				return models.HttpRequest{}, fmt.Errorf("looking up favourite: %w", err)
			}
			err = tx.QueryRow(`SELECT id FROM favourite_collections WHERE name = ? AND project_id = ?`, fav.Name, targetProjectID).Scan(&favID)
			if err != nil {
				if err != sql.ErrNoRows {
					return models.HttpRequest{}, fmt.Errorf("looking up favourite by name: %w", err)
				}
				continue
			}
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO favourite_items (favourite_collection_id, http_request_id) VALUES (?, ?)`, favID, newRequestID); err != nil {
			return models.HttpRequest{}, fmt.Errorf("reattaching favourite: %w", err)
		}
	}

	if _, err := tx.Exec(`DELETE FROM binned_requests WHERE id = ?`, binID); err != nil {
		return models.HttpRequest{}, fmt.Errorf("deleting binned row: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return models.HttpRequest{}, fmt.Errorf("committing restore: %w", err)
	}

	restored, err := s.GetRequest(newRequestID)
	if err != nil {
		return models.HttpRequest{}, fmt.Errorf("loading restored request: %w", err)
	}
	return restored, nil
}

// RestoreBinnedRequests restores multiple binned requests. Each input carries
// its own target collection id so the frontend can resolve fallbacks per
// request.
func (s *RequestService) RestoreBinnedRequests(inputs []models.RestoreBinnedRequestInput) ([]models.HttpRequest, error) {
	restored := make([]models.HttpRequest, 0, len(inputs))
	for _, input := range inputs {
		req, err := s.RestoreBinnedRequest(input.BinID, input.TargetCollectionID)
		if err != nil {
			return nil, fmt.Errorf("restoring binned request %d: %w", input.BinID, err)
		}
		restored = append(restored, req)
	}
	return restored, nil
}

// PermanentlyDeleteBinnedRequest removes a single binned request forever.
func (s *RequestService) PermanentlyDeleteBinnedRequest(binID int64) error {
	_, err := s.db.Exec(`DELETE FROM binned_requests WHERE id = ?`, binID)
	if err != nil {
		return fmt.Errorf("deleting binned request: %w", err)
	}
	return nil
}

// PermanentlyDeleteBinnedRequests removes multiple binned requests forever.
func (s *RequestService) PermanentlyDeleteBinnedRequests(binIDs []int64) error {
	if len(binIDs) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("beginning permanent delete transaction: %w", err)
	}
	defer tx.Rollback()

	for _, binID := range binIDs {
		if _, err := tx.Exec(`DELETE FROM binned_requests WHERE id = ?`, binID); err != nil {
			return fmt.Errorf("deleting binned request %d: %w", binID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing permanent delete: %w", err)
	}
	return nil
}

