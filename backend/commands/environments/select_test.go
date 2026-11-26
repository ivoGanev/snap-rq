package environments

import (
	"reflect"
	"testing"

	"snap-rq/backend/commands"
)

// The frontend reloads its projects on this change, so the stored choice reaches it.
func TestSelectReportsItsProjectAsChanged(t *testing.T) {
	got := Select{}.Changes([]byte(`{"project_id": 7, "environment_id": 3}`), nil)
	want := []commands.RowChange{{Component: "project", IDs: []int64{7}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Changes = %+v, want %+v", got, want)
	}
}
