package bin

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// get.go — one binned request with what it sends, e.g. to copy it as cURL
// before the bin is emptied. The id is the bin id.

type Get struct{ tx *sql.Tx }

func NewGet(tx *sql.Tx) commands.Command { return &Get{tx: tx} }

func (Get) Info() commands.Info {
	return commands.Info{FunctionID: "bin.get", Label: "Get Binned Request", ReadOnly: true}
}

func (c Get) Run(input json.RawMessage) (any, error) {
	var in struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	var req models.HttpRequest
	err := c.tx.QueryRow(`
		SELECT name, url, method, COALESCE(body, ''), body_type,
		       COALESCE(body_form, ''), COALESCE(request_headers, '')
		FROM binned_requests WHERE id = ?`, in.ID,
	).Scan(&req.Name, &req.URL, &req.Method, &req.Body, &req.BodyType, &req.BodyForm, &req.RequestHeaders)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("binned request %d doesn't exist", in.ID)
	}
	if err != nil {
		return nil, fmt.Errorf("loading binned request: %w", err)
	}
	return req, nil
}
