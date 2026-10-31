package commands

import (
	"database/sql"
	"fmt"
	"sort"
)

// Manifest is the database's cascade graph: which rows die with which
// parent rows, and what the UI calls rows of each table. It is data about
// the schema, verified against the real schema at every boot — the app
// refuses to start if this file and the database disagree, so the engine's
// capture walk can never miss victims.
type Manifest struct {
	Tables map[string]TableRule `yaml:"tables"`
}

// TableRule describes one table's place in the graph.
type TableRule struct {
	// Entity is what the UI calls rows of this table ("request" for
	// http_requests); change events carry entity names.
	Entity string `yaml:"entity"`
	// Children maps child tables to edge rules. A child's rows die with
	// this table's rows (ON DELETE CASCADE).
	Children map[string]ChildRule `yaml:"children"`
}

// ChildRule is one cascade edge. Restore defaults to true; restore: false
// marks a real cascade whose rows undo never re-inserts (telemetry, logs).
type ChildRule struct {
	Restore *bool `yaml:"restore"`

	// fkColumn is the child's foreign key column pointing at the parent,
	// resolved from the real schema during validation.
	fkColumn string
}

// NoRestore opts a child edge out of undo restoration, for manifests built
// in code (tests). YAML uses `restore: false`.
var NoRestore = new(bool)

// restores reports whether undo re-inserts this child's rows.
func (c ChildRule) restores() bool {
	return c.Restore == nil || *c.Restore
}

// Edge is a resolved cascade edge: the child table and the foreign key
// column in it that points at the parent. Resolved once from the real
// schema at boot, so the capture walk never guesses.
type Edge struct {
	Child    string
	FKColumn string
	Restore  bool
}

// edgesFor returns the resolved cascade edges of one table, alphabetically
// for deterministic captures. Sibling order never matters for correctness
// — children reference parents, never each other.
func (m *Manifest) edgesFor(table string) []Edge {
	rule, ok := m.Tables[table]
	if !ok {
		return nil
	}
	children := make([]string, 0, len(rule.Children))
	for child := range rule.Children {
		children = append(children, child)
	}
	sort.Strings(children)

	edges := make([]Edge, 0, len(children))
	for _, child := range children {
		childRule := rule.Children[child]
		edges = append(edges, Edge{
			Child:    child,
			FKColumn: childRule.fkColumn,
			Restore:  childRule.restores(),
		})
	}
	return edges
}

// entity returns the UI name for a table.
func (m *Manifest) entity(table string) string {
	if rule, ok := m.Tables[table]; ok && rule.Entity != "" {
		return rule.Entity
	}
	return table
}

// realCascadeEdges reads the schema's actual ON DELETE CASCADE edges:
// child table → parent table → the child's foreign key column.
func realCascadeEdges(db *sql.DB) (map[string]map[string]string, error) {
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table'`)
	if err != nil {
		return nil, fmt.Errorf("listing tables: %w", err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, fmt.Errorf("reading a table name: %w", err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterating tables: %w", err)
	}
	rows.Close()

	real := map[string]map[string]string{}
	for _, table := range tables {
		fkRows, err := db.Query(fmt.Sprintf("PRAGMA foreign_key_list(%s)", table))
		if err != nil {
			return nil, fmt.Errorf("reading foreign keys of %s: %w", table, err)
		}
		for fkRows.Next() {
			var id, seq int
			var parent, fromColumn, to string
			var onUpdate, onDelete, match string
			if err := fkRows.Scan(&id, &seq, &parent, &fromColumn, &to, &onUpdate, &onDelete, &match); err != nil {
				fkRows.Close()
				return nil, fmt.Errorf("reading a foreign key of %s: %w", table, err)
			}
			if onDelete != "CASCADE" {
				continue
			}
			if real[table] == nil {
				real[table] = map[string]string{}
			}
			real[table][parent] = fromColumn
		}
		fkRows.Close()
		if err := fkRows.Err(); err != nil {
			return nil, fmt.Errorf("iterating foreign keys of %s: %w", table, err)
		}
	}
	return real, nil
}

// Validate checks the manifest against the real schema, in both
// directions, and resolves every edge's foreign key column:
//
//  1. every declared child edge must be a real foreign key with
//     ON DELETE CASCADE
//  2. every real CASCADE edge must be declared — an undeclared edge means
//     rows the engine would never save copies of, so undo would silently drop
//     them; that is a refusal to start, not a warning
func (m *Manifest) Validate(db *sql.DB) error {
	real, err := realCascadeEdges(db)
	if err != nil {
		return err
	}

	// Direction 1: declared edges must be real; resolve their FK columns.
	for parent, rule := range m.Tables {
		for child, childRule := range rule.Children {
			fkColumn, realEdge := real[child][parent]
			if !realEdge {
				return fmt.Errorf(
					"manifest declares %s as a child of %s, but the schema has no such ON DELETE CASCADE foreign key",
					child, parent)
			}
			childRule.fkColumn = fkColumn
			rule.Children[child] = childRule
		}
		m.Tables[parent] = rule
	}

	// Direction 2: real cascade edges must be declared.
	for child, parents := range real {
		for parent := range parents {
			rule, declared := m.Tables[parent]
			if !declared {
				return fmt.Errorf(
					"the schema cascades %s to %s, but the manifest does not declare it — undo could lose rows",
					parent, child)
			}
			if _, ok := rule.Children[child]; !ok {
				return fmt.Errorf(
					"the schema cascades %s to %s, but the manifest does not declare it — undo could lose rows",
					parent, child)
			}
		}
	}

	return nil
}
