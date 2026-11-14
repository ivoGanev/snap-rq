package requests

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"snap-rq/backend/commands"
)

// bulk_set.go — write one field onto many requests. Snapshot saves the
// column and every row's previous value; undo writes them all back.

type BulkSet struct{ tx *sql.Tx }

func NewBulkSet(tx *sql.Tx) commands.Command { return &BulkSet{tx: tx} }

func (BulkSet) Info() commands.Info {
	return commands.Info{FunctionID: "request.bulk_set", Label: "Edit Requests"}
}

// bulkFields maps the payload's field name to the column it writes; the
// names are fixed vocabulary, never taken from input directly.
var bulkFields = map[string]string{
	"name":      "name",
	"url":       "url",
	"method":    "method",
	"headers":   "request_headers",
	"body":      "body",
	"body_type": "body_type",
	"body_form": "body_form",
	"color":     "user_color",
}

type bulkSetInput struct {
	IDs   []int64 `json:"ids"`
	Field string  `json:"field"`
	Value any     `json:"value"`
}

// bulkSnapshot is the column being written and each row's previous value.
type bulkSnapshot struct {
	Column string     `json:"column"`
	Rows   []oldValue `json:"rows"`
}

type oldValue struct {
	ID    int64 `json:"id"`
	Value any   `json:"value"`
}

func (c BulkSet) Snapshot(input json.RawMessage) (any, error) {
	var in bulkSetInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	column, known := bulkFields[in.Field]
	if !known {
		return nil, fmt.Errorf("unknown field %q", in.Field)
	}

	saved := bulkSnapshot{Column: column}
	for _, id := range in.IDs {
		var value any
		if err := c.tx.QueryRow(
			fmt.Sprintf(`SELECT %s FROM http_requests WHERE id = ?`, column), id,
		).Scan(&value); err != nil {
			return nil, fmt.Errorf("loading request %d: %w", id, err)
		}
		saved.Rows = append(saved.Rows, oldValue{ID: id, Value: value})
	}
	return saved, nil
}

func (c BulkSet) Run(input json.RawMessage) (any, error) {
	var in bulkSetInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	column, known := bulkFields[in.Field]
	if !known {
		return nil, fmt.Errorf("unknown field %q", in.Field)
	}
	if len(in.IDs) == 0 {
		return nil, nil
	}

	if in.Field == "color" {
		slot, err := bulkColorValue(in.Value)
		if err != nil {
			return nil, err
		}
		in.Value = slot
	}

	for _, id := range in.IDs {
		if _, err := c.tx.Exec(
			fmt.Sprintf(`UPDATE http_requests SET %s = ? WHERE id = ?`, column),
			in.Value, id,
		); err != nil {
			return nil, fmt.Errorf("updating request %d: %w", id, err)
		}
	}
	return nil, nil
}

// Undo writes the previous values back.
func (c BulkSet) Undo(snapshot, _ json.RawMessage) error {
	var saved bulkSnapshot
	if err := json.Unmarshal(snapshot, &saved); err != nil {
		return err
	}
	for _, row := range saved.Rows {
		if _, err := c.tx.Exec(
			fmt.Sprintf(`UPDATE http_requests SET %s = ? WHERE id = ?`, saved.Column),
			row.Value, row.ID,
		); err != nil {
			return err
		}
	}
	return nil
}

func (BulkSet) Changes(input, _ json.RawMessage) []commands.RowChange {
	return []commands.RowChange{{Component: "request", IDs: commands.IDsOf(input, "ids")}}
}

// bulkColorValue validates the value of a "color" bulk edit: nil or an empty
// string clears the colour, anything else must be a colour slot.
func bulkColorValue(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	text, ok := value.(string)
	if !ok {
		return nil, fmt.Errorf("colour must be a string, got %T", value)
	}
	return commands.ColorSlotValue(&text)
}
