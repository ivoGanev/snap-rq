package database

import (
	"os"
	"path/filepath"
	"testing"
)

func writeMarker(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.db"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readMarker(t *testing.T, dir string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestMovePreviousDataDirMovesAnEarlierName(t *testing.T) {
	parent := t.TempDir()
	writeMarker(t, filepath.Join(parent, "Older"), "older")

	from, err := movePreviousDataDir(parent, "Current", []string{"Old", "Older"})
	if err != nil {
		t.Fatal(err)
	}
	if from != filepath.Join(parent, "Older") {
		t.Fatalf("moved %q, want the Older folder", from)
	}
	if got := readMarker(t, filepath.Join(parent, "Current")); got != "older" {
		t.Fatalf("current folder holds %q, want the moved data", got)
	}
}

func TestMovePreviousDataDirNeverReplacesTheCurrentFolder(t *testing.T) {
	parent := t.TempDir()
	writeMarker(t, filepath.Join(parent, "Current"), "current")
	writeMarker(t, filepath.Join(parent, "Old"), "old")

	from, err := movePreviousDataDir(parent, "Current", []string{"Old"})
	if err != nil {
		t.Fatal(err)
	}
	if from != "" {
		t.Fatalf("moved %q over an existing folder", from)
	}
	if got := readMarker(t, filepath.Join(parent, "Current")); got != "current" {
		t.Fatalf("current folder holds %q, want its own data", got)
	}
	if got := readMarker(t, filepath.Join(parent, "Old")); got != "old" {
		t.Fatalf("old folder holds %q, want it untouched", got)
	}
}

func TestMovePreviousDataDirWithNothingToMove(t *testing.T) {
	from, err := movePreviousDataDir(t.TempDir(), "Current", []string{"Old"})
	if err != nil || from != "" {
		t.Fatalf("got (%q, %v), want nothing moved", from, err)
	}
}
