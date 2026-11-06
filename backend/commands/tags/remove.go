package tags

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"snap-rq/backend/commands"
)

//  — remove a tag from a request. Snapshot saves the link row;
// undo re-inserts it.

type Remove struct{ tx *sql.Tx }

func NewRemove(tx *sql.Tx) commands.Command { return &Remove{tx: tx} }

func (Remove) Info() commands.Info {
	return commands.Info{FunctionID: "tag.remove", Label: "Remove Tag"}
}

type savedLink struct {
	ID            int64   `json:"id"`
	RequestID     int64   `json:"request_id"`
	TagID         int64   `json:"tag_id"`
	UserSortOrder int     `json:"user_sort_order"`
	UserColor     *string `json:"user_color"`
}

func (c Remove) Snapshot(input json.RawMessage) (any, error) {
	var in struct {
		RequestID int64  `json:"request_id"`
		TagName   string `json:"tag_name"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	var link savedLink
	err := c.tx.QueryRow(`
		SELECT rt.id, rt.request_id, rt.tag_id, rt.user_sort_order, rt.user_color
		FROM request_tags rt JOIN tags t ON t.id = rt.tag_id
		WHERE rt.request_id = ? AND t.name = ?`, in.RequestID, in.TagName,
	).Scan(&link.ID, &link.RequestID, &link.TagID, &link.UserSortOrder, &link.UserColor)
	if err != nil {
		return nil, fmt.Errorf("tag not found on request: %w", err)
	}
	return link, nil
}

func (c Remove) Run(input json.RawMessage) (any, error) {
	var in struct {
		RequestID int64  `json:"request_id"`
		TagName   string `json:"tag_name"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	_, err := c.tx.Exec(`
		DELETE FROM request_tags WHERE request_id = ? AND tag_id =
		  (SELECT id FROM tags WHERE name = ?)`, in.RequestID, in.TagName)
	return nil, err
}

func (c Remove) Undo(snapshot, _ json.RawMessage) error {
	var link savedLink
	if err := json.Unmarshal(snapshot, &link); err != nil {
		return err
	}
	_, err := c.tx.Exec(`
		INSERT INTO request_tags (id, request_id, tag_id, user_sort_order, user_color)
		VALUES (?, ?, ?, ?, ?)`,
		link.ID, link.RequestID, link.TagID, link.UserSortOrder, link.UserColor)
	return err
}

func (Remove) Changes(_, _ json.RawMessage) []commands.RowChange {
	return []commands.RowChange{{Component: "tag"}}
}
