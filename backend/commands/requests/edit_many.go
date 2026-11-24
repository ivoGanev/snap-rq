package requests

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// edit_many.go — apply a list of {id, field, value} changes in one
// transaction and one undo step. Field names are request.get's.

type EditMany struct{ tx *sql.Tx }

func NewEditMany(tx *sql.Tx) commands.Command { return &EditMany{tx: tx} }

func (EditMany) Info() commands.Info {
	return commands.Info{FunctionID: "request.edit_many", Label: "Edit Requests"}
}

// editFields are the text columns a change may write, named as request.get
// returns them. The column name comes from this fixed list, never from input.
var editFields = map[string]bool{
	"name": true, "url": true, "method": true, "request_headers": true,
	"body": true, "body_type": true, "body_form": true,
}

// EditFieldNames lists editFields for error messages.
const EditFieldNames = "name, url, method, request_headers, body, body_type, body_form"

type editChange struct {
	ID    int64  `json:"id"`
	Field string `json:"field"`
	Value any    `json:"value"`
}

type editManyInput struct {
	Changes []editChange `json:"changes"`
}

func decodeEditMany(input json.RawMessage) (editManyInput, error) {
	var in editManyInput
	if err := json.Unmarshal(input, &in); err != nil {
		return in, err
	}
	if len(in.Changes) == 0 {
		return in, errors.New("changes must not be empty")
	}
	for i, c := range in.Changes {
		if !editFields[c.Field] {
			hint := ""
			if c.Field == "headers" {
				hint = " (headers are request_headers, as get_request names them)"
			}
			return in, fmt.Errorf("change %d: unknown field %q%s; use one of %s", i+1, c.Field, hint, EditFieldNames)
		}
		if _, ok := c.Value.(string); !ok {
			return in, fmt.Errorf("change %d (request %d): %w", i+1, c.ID, notText(c.Field, c.Value))
		}
	}
	return in, nil
}

// Snapshot keeps every touched cell's previous value, in change order.
func (c EditMany) Snapshot(input json.RawMessage) (any, error) {
	in, err := decodeEditMany(input)
	if err != nil {
		return nil, err
	}
	saved := make([]editChange, 0, len(in.Changes))
	for _, ch := range in.Changes {
		var value any
		if err := c.tx.QueryRow(
			fmt.Sprintf(`SELECT %s FROM http_requests WHERE id = ?`, ch.Field), ch.ID,
		).Scan(&value); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, fmt.Errorf("request %d doesn't exist", ch.ID)
			}
			return nil, fmt.Errorf("loading request %d: %w", ch.ID, err)
		}
		saved = append(saved, editChange{ID: ch.ID, Field: ch.Field, Value: value})
	}
	return saved, nil
}

// Run returns the changed requests, each once, in the order first named.
func (c EditMany) Run(input json.RawMessage) (any, error) {
	in, err := decodeEditMany(input)
	if err != nil {
		return nil, err
	}
	order := []int64{}
	seen := map[int64]bool{}
	for _, ch := range in.Changes {
		value := ch.Value.(string)
		switch ch.Field {
		case "body_type":
			value = normalizeBodyType(value)
		case "method":
			value = strings.ToUpper(strings.TrimSpace(value))
		}
		if _, err := c.tx.Exec(
			fmt.Sprintf(`UPDATE http_requests SET %s = ? WHERE id = ?`, ch.Field), value, ch.ID,
		); err != nil {
			return nil, fmt.Errorf("updating request %d: %w", ch.ID, err)
		}
		if !seen[ch.ID] {
			seen[ch.ID] = true
			order = append(order, ch.ID)
		}
	}
	updated := make([]models.HttpRequest, 0, len(order))
	for _, id := range order {
		req, err := queryRequest(c.tx, id)
		if err != nil {
			return nil, fmt.Errorf("reading request %d back: %w", id, err)
		}
		updated = append(updated, req)
	}
	return updated, nil
}

// Undo writes the previous values back, last change first, so a cell changed
// twice in one call ends at its original value.
func (c EditMany) Undo(snapshot, _ json.RawMessage) error {
	var saved []editChange
	if err := json.Unmarshal(snapshot, &saved); err != nil {
		return err
	}
	for i := len(saved) - 1; i >= 0; i-- {
		ch := saved[i]
		if !editFields[ch.Field] {
			return fmt.Errorf("unknown field %q in the undo log", ch.Field)
		}
		if _, err := c.tx.Exec(
			fmt.Sprintf(`UPDATE http_requests SET %s = ? WHERE id = ?`, ch.Field), ch.Value, ch.ID,
		); err != nil {
			return err
		}
	}
	return nil
}

func (EditMany) Changes(input, _ json.RawMessage) []commands.RowChange {
	return []commands.RowChange{{Component: "request", IDs: commands.IDsOf(input, "id")}}
}
