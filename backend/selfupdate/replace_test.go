package selfupdate

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTargetIsTheAppBundleOnMacAndTheProgramElsewhere(t *testing.T) {
	for _, tc := range []struct{ exe, goos, want string }{
		{"/Applications/SnapRQ.app/Contents/MacOS/SnapRQ", "darwin", "/Applications/SnapRQ.app"},
		{"/Users/demo-user/Applications/snap-rq-wails-v3.app/Contents/MacOS/snap-rq-wails-v3", "darwin",
			"/Users/demo-user/Applications/snap-rq-wails-v3.app"},
		{"/usr/local/bin/snaprq", "darwin", "/usr/local/bin/snaprq"},
		{"/usr/bin/snaprq", "linux", "/usr/bin/snaprq"},
		{`C:\Users\demo-user\AppData\Local\SnapRQ\snaprq.exe`, "windows", `C:\Users\demo-user\AppData\Local\SnapRQ\snaprq.exe`},
	} {
		if got := Target(tc.exe, tc.goos); got != tc.want {
			t.Errorf("Target(%s, %s) = %s, want %s", tc.exe, tc.goos, got, tc.want)
		}
	}
}

func TestWritable(t *testing.T) {
	dir := t.TempDir()
	if err := Writable(dir); err != nil {
		t.Errorf("a temp folder isn't writable: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("the check left %d files behind", len(entries))
	}
	if err := Writable(filepath.Join(dir, "missing")); err == nil {
		t.Error("a missing folder counts as writable")
	}
	if runtime.GOOS == "windows" {
		return // Windows doesn't apply a folder's read-only mode to new files.
	}
	readOnly := filepath.Join(dir, "read-only")
	if err := os.Mkdir(readOnly, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(readOnly, 0o755) })
	if os.Geteuid() != 0 && Writable(readOnly) == nil {
		t.Error("a read-only folder counts as writable")
	}
}

func TestBlockedExplainsAFolderThatCantBeWritten(t *testing.T) {
	if got := Blocked(filepath.Join(t.TempDir(), "snaprq"), runtime.GOOS); got != "" {
		t.Errorf("a writable install is blocked: %s", got)
	}

	missing := filepath.Join(t.TempDir(), "missing")
	for goos, advice := range map[string]string{
		"darwin":  "Move SnapRQ to the Applications folder in your home folder",
		"linux":   "Install the new version's package",
		"windows": "Download the new version and install it yourself",
	} {
		got := Blocked(filepath.Join(missing, "snaprq"), goos)
		if !strings.Contains(got, "can't update itself in "+missing) || !strings.Contains(got, advice) {
			t.Errorf("%s: %q, want the folder and %q", goos, got, advice)
		}
	}
}
