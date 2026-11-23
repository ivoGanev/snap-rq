package database

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTheLegacyDataFolderMovesOnce(t *testing.T) {
	parent := t.TempDir()
	legacy := filepath.Join(parent, legacyDataDirName)
	if err := os.MkdirAll(filepath.Join(legacy, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"app.db", "secret.key", filepath.Join("nested", "x")} {
		if err := os.WriteFile(filepath.Join(legacy, f), []byte(f), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	moved, err := moveLegacyDataDir(parent)
	if err != nil || !moved {
		t.Fatalf("moved = %v, %v", moved, err)
	}
	for _, f := range []string{"app.db", "secret.key", filepath.Join("nested", "x")} {
		if data, err := os.ReadFile(filepath.Join(parent, dataDirName, f)); err != nil || string(data) != f {
			t.Errorf("%s after the move: %q, %v", f, data, err)
		}
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Error("the legacy folder is still there")
	}

	if moved, err := moveLegacyDataDir(parent); moved || err != nil {
		t.Errorf("second run: moved = %v, %v", moved, err)
	}
}

func TestANewDataFolderIsNeverReplaced(t *testing.T) {
	parent := t.TempDir()
	for _, dir := range []string{legacyDataDirName, dataDirName} {
		if err := os.MkdirAll(filepath.Join(parent, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(parent, dir, "app.db"), []byte(dir), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if moved, err := moveLegacyDataDir(parent); moved || err != nil {
		t.Fatalf("moved = %v, %v", moved, err)
	}
	if data, _ := os.ReadFile(filepath.Join(parent, dataDirName, "app.db")); string(data) != dataDirName {
		t.Errorf("the new folder's database was replaced: %q", data)
	}
}

func TestNothingToMoveIsFine(t *testing.T) {
	if moved, err := moveLegacyDataDir(t.TempDir()); moved || err != nil {
		t.Errorf("moved = %v, %v", moved, err)
	}
}
