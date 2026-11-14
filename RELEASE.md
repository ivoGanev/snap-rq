# Release process

Releases are driven by a manual GitHub Actions workflow. Each release tags a
SemVer version, builds distributables for all three platforms, and publishes
them to a GitHub Release that the in-app updater consumes.

## Triggering a release

Run the **Release** workflow from the **Actions** tab and provide the target
version (SemVer, no `v` prefix, e.g. `1.2.3`). The workflow tags, builds, and
publishes automatically — no manual `git tag` or asset uploads.

## Pipeline

For the given version the workflow:

1. Stamps the version into all build metadata (`build/config.yml`,
   `build/windows/info.json`, `build/darwin/Info.plist`,
   `build/linux/nfpm/nfpm.yaml`) via `scripts/stamp-version.mjs`.
2. Compiles the version into the binary via
   `-ldflags "-X main.currentVersion=<version>"` (threaded through the
   platform Taskfiles' `VERSION` var).
3. Builds three artifacts in a matrix:

   | Platform | Artifacts |
   |----------|-----------|
   | Windows  | raw `-windows-amd64.exe` (updater) + `-windows-amd64-setup.exe` (NSIS installer) |
   | macOS    | `-darwin-universal.zip` (`.app`, updater) + `-darwin-universal.dmg` |
   | Linux    | raw `-linux-amd64` (updater) + `.AppImage`, `.deb`, `.rpm` |

4. Emits a `SHA256SUMS` sidecar covering every artifact.
5. Creates the GitHub Release at tag `v<version>` with `--generate-notes` and
   attaches the artifacts and checksums.

## Self-update

The app embeds its version at build time (`main.currentVersion`) and compares
it against the release tag on `App → Check for Updates…` via
`app.Updater` (Wails v3 `pkg/updater`, GitHub Releases provider). If the tag
is strictly greater under SemVer, it downloads the asset matching the running
OS/architecture, verifies the SHA-256 digest against `SHA256SUMS`, swaps the
binary, and restarts.

Constraints:

- Versions must be valid SemVer.
- Asset filenames must contain the OS and architecture so the default matcher
  resolves them (`windows`/`darwin`/`linux`, `amd64`/`arm64`/`386`).
- The updater queries GitHub's "latest" release, so releases must be published
  in ascending version order.

## Known limitations

- Binaries are unsigned; SmartScreen/Gatekeeper will warn. Signing tasks exist
  in the platform Taskfiles for when certificates are provisioned.
- macOS/Linux packaging uses the stock Wails task names; confirm exact output
  filenames on the first run.
- `build/linux/nfpm/nfpm.yaml` still carries template metadata (vendor, name)
  and should be corrected before the first public release.
