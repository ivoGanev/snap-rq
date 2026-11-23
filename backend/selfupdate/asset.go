// Package selfupdate holds the release-asset rules used by the in-app updater.
package selfupdate

import "strings"

// IsUpdateAsset reports whether a release asset is the file the updater should
// download for the given OS/architecture. The updater replaces the running
// binary, so it must pick the raw executable (or the .zip holding the macOS
// .app), never an installer, .deb, .rpm, .AppImage or .dmg.
//
// Asset names are produced by .github/workflows/release.yml:
//
//	snaprq-windows-amd64.exe
//	snaprq-darwin-universal.zip
//	snaprq-linux-amd64
func IsUpdateAsset(name, goos, goarch string) bool {
	n := strings.ToLower(name)
	switch goos {
	case "windows":
		return strings.HasSuffix(n, "-windows-"+goarch+".exe")
	case "darwin":
		// One universal build serves both Intel and Apple Silicon.
		return strings.HasSuffix(n, "-darwin-universal.zip")
	case "linux":
		return strings.HasSuffix(n, "-linux-"+goarch)
	}
	return false
}
