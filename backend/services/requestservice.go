package services

import (
	"database/sql"
	"fmt"
	"io"
	"net/http"
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

	req.ID = id
	return req, nil
}

// GetRequest retrieves a single HTTP request by ID.
func (s *RequestService) GetRequest(id int64) (models.HttpRequest, error) {
	var req models.HttpRequest
	row := s.db.QueryRow(
		`SELECT id, collection_id, name, url, method, body, request_headers, status_code, response_id
		 FROM http_requests WHERE id = ?`,
		id,
	)
	err := row.Scan(&req.ID, &req.CollectionID, &req.Name, &req.URL, &req.Method, &req.Body, &req.RequestHeaders, &req.StatusCode, &req.ResponseID)
	if err != nil {
		if err == sql.ErrNoRows {
			return models.HttpRequest{}, fmt.Errorf("request not found")
		}
		return models.HttpRequest{}, fmt.Errorf("getting request: %w", err)
	}
	return req, nil
}

// ExecuteRequest runs the saved HTTP request, stores the response, updates the
// request's latest status/response id and returns the stored response. Network
// or client errors are captured as a response with the error message in the body
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
		return s.storeErrorResponse(id, fmt.Errorf("building request: %w", err))
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
	httpResp, err := client.Do(httpReq)
	if err != nil {
		return s.storeErrorResponse(id, fmt.Errorf("request failed: %w", err))
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return s.storeErrorResponse(id, fmt.Errorf("reading response body: %w", err))
	}

	var respHeaders strings.Builder
	for name, values := range httpResp.Header {
		for _, value := range values {
			respHeaders.WriteString(fmt.Sprintf("%s: %s\n", name, value))
		}
	}

	resp := models.HttpResponse{
		RequestID:  id,
		StatusCode: httpResp.StatusCode,
		Headers:    strings.TrimSpace(respHeaders.String()),
		Body:       string(respBody),
	}

	created, err := s.CreateResponse(resp)
	if err != nil {
		return models.HttpResponse{}, fmt.Errorf("saving response: %w", err)
	}

	_, err = s.db.Exec(
		`UPDATE http_requests SET status_code = ?, response_id = ? WHERE id = ?`,
		created.StatusCode, created.ID, id,
	)
	if err != nil {
		return models.HttpResponse{}, fmt.Errorf("updating request: %w", err)
	}

	return created, nil
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

func (s *RequestService) storeErrorResponse(requestID int64, execErr error) (models.HttpResponse, error) {
	resp := models.HttpResponse{
		RequestID:  requestID,
		StatusCode: 0,
		Headers:    "",
		Body:       execErr.Error(),
	}
	created, err := s.CreateResponse(resp)
	if err != nil {
		return models.HttpResponse{}, fmt.Errorf("saving error response: %w", err)
	}

	_, err = s.db.Exec(
		`UPDATE http_requests SET status_code = ?, response_id = ? WHERE id = ?`,
		0, created.ID, requestID,
	)
	if err != nil {
		return models.HttpResponse{}, fmt.Errorf("updating request after error: %w", err)
	}

	return created, nil
}

// GetAllRequests returns all saved HTTP requests ordered by name.
func (s *RequestService) GetAllRequests() ([]models.HttpRequest, error) {
	rows, err := s.db.Query(
		`SELECT id, collection_id, name, url, method, body, request_headers, status_code, response_id
		 FROM http_requests ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing requests: %w", err)
	}
	defer rows.Close()

	var requests []models.HttpRequest
	for rows.Next() {
		var req models.HttpRequest
		if err := rows.Scan(&req.ID, &req.CollectionID, &req.Name, &req.URL, &req.Method, &req.Body, &req.RequestHeaders, &req.StatusCode, &req.ResponseID); err != nil {
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
		`SELECT id, collection_id, name, url, method, body, request_headers, status_code, response_id
		 FROM http_requests
		 WHERE collection_id = ?
		 ORDER BY name`,
		collectionID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing requests: %w", err)
	}
	defer rows.Close()

	var requests []models.HttpRequest
	for rows.Next() {
		var req models.HttpRequest
		if err := rows.Scan(&req.ID, &req.CollectionID, &req.Name, &req.URL, &req.Method, &req.Body, &req.RequestHeaders, &req.StatusCode, &req.ResponseID); err != nil {
			return nil, fmt.Errorf("scanning request: %w", err)
		}
		requests = append(requests, req)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating requests: %w", err)
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

	return req, nil
}

// DeleteRequest removes an HTTP request by ID.
func (s *RequestService) DeleteRequest(id int64) error {
	_, err := s.db.Exec(`DELETE FROM http_requests WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting request: %w", err)
	}
	return nil
}

// CreateResponse saves a new response for a request and returns it with its generated ID.
func (s *RequestService) CreateResponse(resp models.HttpResponse) (models.HttpResponse, error) {
	result, err := s.db.Exec(
		`INSERT INTO responses (request_id, headers, status_code, body)
		 VALUES (?, ?, ?, ?)`,
		resp.RequestID, resp.Headers, resp.StatusCode, resp.Body,
	)
	if err != nil {
		return models.HttpResponse{}, fmt.Errorf("creating response: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return models.HttpResponse{}, fmt.Errorf("getting last insert id: %w", err)
	}

	resp.ID = id
	return resp, nil
}

// GetResponse retrieves a single response by ID.
func (s *RequestService) GetResponse(id int64) (models.HttpResponse, error) {
	var resp models.HttpResponse
	row := s.db.QueryRow(
		`SELECT id, request_id, headers, status_code, body FROM responses WHERE id = ?`,
		id,
	)
	err := row.Scan(&resp.ID, &resp.RequestID, &resp.Headers, &resp.StatusCode, &resp.Body)
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
		`SELECT id, request_id, headers, status_code, body
		 FROM responses
		 WHERE request_id = ?
		 ORDER BY id DESC`,
		requestID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing responses: %w", err)
	}
	defer rows.Close()

	var responses []models.HttpResponse
	for rows.Next() {
		var resp models.HttpResponse
		if err := rows.Scan(&resp.ID, &resp.RequestID, &resp.Headers, &resp.StatusCode, &resp.Body); err != nil {
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
