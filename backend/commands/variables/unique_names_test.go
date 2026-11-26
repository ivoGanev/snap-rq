package variables_test

import (
	"encoding/json"
	"strings"
	"testing"

	"snap-rq/backend/models"
)

func (f fixture) runErr(command string, in any) error {
	f.t.Helper()
	raw, _ := json.Marshal(in)
	_, err := f.runner.Run(command, raw)
	return err
}

func (f fixture) create(environmentID int64, key, value string) models.EnvironmentVariable {
	f.t.Helper()
	return f.run("variable.create", map[string]any{"environment_id": environmentID, "key": key, "value": value}).(models.EnvironmentVariable)
}

func expectTaken(t *testing.T, err error, key string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "A variable named "+key+" already exists in this environment.") {
		t.Errorf("error = %v, want %s refused as taken", err, key)
	}
}

func TestANameTakenInTheEnvironmentIsRefused(t *testing.T) {
	f := newFixture(t)
	f.create(f.envID, "TOKEN", "a")
	expectTaken(t, f.runErr("variable.create", map[string]any{"environment_id": f.envID, "key": "TOKEN", "value": "b"}), "TOKEN")

	other := f.create(f.envID, "OTHER", "c")
	expectTaken(t, f.runErr("variable.update", map[string]any{"id": other.ID, "key": "TOKEN", "value": "c"}), "TOKEN")

	var n int
	_ = f.db.QueryRow(`SELECT COUNT(*) FROM environment_variables WHERE environment_id = ? AND key = 'TOKEN'`, f.envID).Scan(&n)
	if n != 1 {
		t.Errorf("%d variables named TOKEN, want 1", n)
	}
}

func TestNewUnnamedRowsAndOtherEnvironmentsAreFine(t *testing.T) {
	f := newFixture(t)
	f.create(f.envID, "", "")
	f.create(f.envID, "", "") // the editor adds rows unnamed

	res, err := f.db.Exec(`INSERT INTO environments (project_id, name) SELECT project_id, 'Other' FROM environments WHERE id = ?`, f.envID)
	if err != nil {
		t.Fatal(err)
	}
	otherEnv, _ := res.LastInsertId()
	f.create(f.envID, "TOKEN", "staging")
	f.create(otherEnv, "TOKEN", "prod") // each environment has its own
}

func TestNamesAreCaseSensitiveLikeFillingIn(t *testing.T) {
	f := newFixture(t)
	f.create(f.envID, "TOKEN", "a")
	f.create(f.envID, "token", "b")
}

func TestAVariableKeepsItsOwnNameAndAnExistingDuplicateCanBeEdited(t *testing.T) {
	f := newFixture(t)
	v := f.create(f.envID, "TOKEN", "a")
	f.run("variable.update", map[string]any{"id": v.ID, "key": "TOKEN", "value": "b"})

	// A duplicate saved before names had to be unique still takes value edits.
	if _, err := f.db.Exec(`INSERT INTO environment_variables (environment_id, key, value, secret) VALUES (?, 'TOKEN', 'old', 0)`, f.envID); err != nil {
		t.Fatal(err)
	}
	var dupID int64
	_ = f.db.QueryRow(`SELECT id FROM environment_variables WHERE value = 'old'`).Scan(&dupID)
	f.run("variable.update", map[string]any{"id": dupID, "key": "TOKEN", "value": "new"})
	if value, _ := f.stored(dupID); value != "new" {
		t.Errorf("value = %q, want the edit saved", value)
	}
}
