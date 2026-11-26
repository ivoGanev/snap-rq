package selfupdate

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Target is what an update replaces: the .app bundle the program runs from
// on macOS, the program file elsewhere. It matches the Wails updater, which
// backs the target up beside it before swapping in the new version.
func Target(exe, goos string) string {
	if goos == "darwin" {
		parts := strings.Split(path.Clean(exe), "/")
		for i, p := range parts {
			if strings.HasSuffix(p, ".app") {
				return "/" + path.Join(parts[1:i+1]...)
			}
		}
	}
	return exe
}

// Writable reports whether files can be created in dir, by creating one.
func Writable(dir string) error {
	f, err := os.CreateTemp(dir, ".snaprq-update-check-*")
	if err != nil {
		return err
	}
	name := f.Name()
	f.Close()
	return os.Remove(name)
}

// Blocked explains why an update can't replace the program at exe, or is "".
// The updater writes beside the program, so a folder the user can't write
// (a system Applications or bin folder) would leave the app closed and
// unchanged; this is checked before anything is downloaded or quit.
func Blocked(exe, goos string) string {
	dir := filepath.Dir(Target(exe, goos))
	if Writable(dir) == nil {
		return ""
	}
	switch goos {
	case "darwin":
		return fmt.Sprintf("SnapRQ can't update itself in %s: your user can't write there. "+
			"Move SnapRQ to the Applications folder in your home folder, or download the new version and replace it yourself.", dir)
	case "linux":
		return fmt.Sprintf("SnapRQ can't update itself in %s: your user can't write there. "+
			"Install the new version's package, or download it yourself.", dir)
	default:
		return fmt.Sprintf("SnapRQ can't update itself in %s: your user can't write there. "+
			"Download the new version and install it yourself.", dir)
	}
}

// RunningBlocked is Blocked for the running program.
func RunningBlocked(goos string) string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return Blocked(exe, goos)
}
