# Releasing snap-rq

Releases are created manually with one button, and installed apps update
themselves automatically.

## How it works (the short version)

1. You click a button and type a version number (e.g. `1.2.3`).
2. GitHub builds the app for Windows, macOS, and Linux.
3. The artifacts are attached to a new GitHub Release tagged `v1.2.3`.
4. Running apps see the new tag, download their matching file, verify it,
   swap themselves, and restart.

## Triggering a release

1. Go to the repo on GitHub → **Actions** tab.
2. Select the **Release** workflow on the left.
3. Click **Run workflow**.
4. Enter the version (without a leading `v`, e.g. `1.2.3`) and run it.

That's it. No tagging, no manual uploads.

## What the pipeline does

For the version you enter, it:

- Stamps the version into the build metadata (`build/config.yml`,
  `build/windows/info.json`, `build/darwin/Info.plist`, `build/linux/nfpm/nfpm.yaml`).
- Compiles the version into the binary (`-ldflags -X main.currentVersion=...`).
- Builds three artifacts in parallel:

| Platform | Artifacts |
|----------|-----------|
| Windows  | `...-windows-amd64.exe` (self-update) + `...-windows-amd64-setup.exe` (installer) |
| macOS    | `...-darwin-universal.zip` (self-update) + `...-darwin-universal.dmg` |
| Linux    | `...-linux-amd64` (self-update) + `.AppImage`, `.deb`, `.rpm` |

- Generates a `SHA256SUMS` file next to the artifacts (used to verify downloads).
- Creates the tagged GitHub Release (`v1.2.3`) and attaches everything.

## How self-updating works

Every release is compared against the version baked into the running app at
build time.

- The app knows its own version (set during the build).
- On "App → Check for Updates…", it asks GitHub for the latest release and
  compares the tag with its own version using SemVer.
- If the tag is **higher**, it downloads the artifact that matches the
  running OS/architecture, verifies its SHA-256 checksum, swaps the binary,
  and restarts.

The rules that make this work:

- Versions must be valid SemVer (`1.2.3`, not `release-1.2`).
- Release assets must contain the OS and architecture in the filename
  (`windows`, `darwin`, `linux`, `amd64`, `arm64`, …).
- Releases must be published in ascending version order — the updater
  compares against GitHub's "latest" release, so a patch published after a
  newer major would be ignored.

## Notes / current limitations

- Binaries are **not code-signed yet**. Windows SmartScreen and macOS
  Gatekeeper will warn users. The signing tasks exist in the Taskfiles for
  when certificates are added.
- macOS/Linux packaging follows the Wails task names but the exact artifact
  filenames should be confirmed on the first CI run.
- `build/linux/nfpm/nfpm.yaml` still has placeholder metadata ("My Company",
  `snap-rq-wails-v3.exe`). Clean it up before the first public release.
