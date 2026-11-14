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
//	snap-rq-wails-v3-windows-amd64.exe
//	snap-rq-wails-v3-darwin-universal.zip
//	snap-rq-wails-v3-linux-amd64
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
