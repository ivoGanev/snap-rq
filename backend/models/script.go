package models

// Script is code a user writes once per project and attaches to many
// requests. They run when a request they are attached to is sent (see
// backend/scripting).
type Script struct {
	ID         int64            `json:"id"`
	ProjectID  int64            `json:"project_id"`
	Name       string           `json:"name"`
	Content    string           `json:"content"`
	Appearance ScriptAppearance `json:"appearance"`
}
