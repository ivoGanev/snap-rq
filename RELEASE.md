# Releasing

Releases are cut from the **Release** workflow (Actions tab → *Run workflow*).
Enter the version as `X.Y.Z` (no `v` prefix, no pre-release suffix).

## What it does

1. **Validates** the version: strict `X.Y.Z`, not already released, higher than the latest release.
2. **Tests** the Go backend.
3. **Builds** Windows, macOS (universal) and Linux, with the version stamped into the binary and build metadata (`scripts/stamp-version.mjs`).
4. **Publishes** a GitHub Release `vX.Y.Z` on the commit that was built, with a `SHA256SUMS` file.

| Platform | Self-update asset | Also published |
|----------|-------------------|----------------|
| Windows  | `*-windows-amd64.exe` | `*-setup.exe` (per-user installer) |
| macOS    | `*-darwin-universal.zip` | `*.dmg` |
| Linux    | `*-linux-amd64` | `.AppImage`, `.deb`, `.rpm` |

## Self-update

Settings → **Check for updates** compares the app's version with the latest GitHub release.
If newer, it downloads the self-update asset for the running OS, verifies it against
`SHA256SUMS`, replaces the binary and restarts. Which asset is chosen lives in
`backend/selfupdate`; keep it in sync with the file names in the workflow.

Local builds are `0.0.0-dev` and always see the latest release as newer.

## Trying it out

- **Locally:** `node scripts/stamp-version.mjs 9.9.9`, then `git checkout -- build/` to undo.
- **On GitHub:** run the workflow with a throwaway version, then remove it with
  `gh release delete vX.Y.Z --cleanup-tag`.
- **Update flow:** build an old version (`wails3 task windows:build VERSION=0.0.1`), publish a newer
  release, then use *Check for updates*.

## Known limitations

- Binaries are not signed or notarized. Windows SmartScreen and macOS Gatekeeper will warn,
  and Gatekeeper may block an updated macOS build.
- Only `amd64` is built for Windows and Linux.
