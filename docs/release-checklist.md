# Release checklist

The pull request checks (`.github/workflows/tests.yml`) cover the code. These are the steps they can't do.

## Before releasing

1. `main` is green: the last merged pull request passed Go, Frontend and End to end.
2. Freeze this version's database, so every later release is tested against it. This uses the code being released, so do it on `main` right before the release:

   ```sh
   SNAPRQ_UPGRADE_FIXTURE=<version> go test ./backend/database -run TestWriteUpgradeFixture
   go test ./backend/database
   ```

   Commit `backend/database/testdata/upgrade/<version>.db`. The versions are plain `X.Y.Z` (no `-beta` suffix): `release.yml` accepts nothing else, and the updater only follows GitHub's latest release.

3. Run **Release** (Actions → Release → Run workflow) with that version.

## After releasing, on each platform

Install the published build, not a local one.

| | Windows | macOS | Linux |
|---|---|---|---|
| Installs and opens | setup `.exe` | `.zip` → Applications | `.AppImage` and `.deb` |
| Dock / taskbar icon is the SnapRQ logo | ☐ | ☐ | ☐ |
| A request sends, with a script and a secret variable | ☐ | ☐ | ☐ |
| Quit and reopen: the data is still there | ☐ | ☐ | ☐ |

- **macOS:** releases aren't signed, so macOS blocks the first launch: allow it under System Settings → Privacy & Security → Open Anyway.
- **Linux:** on a machine without a keyring (a bare window manager, no GNOME Keyring or KWallet), the app falls back to a key file. Check that a secret variable is still readable after reopening.

## Updating from the previous release

With the previous release installed and holding some data (a request with history, a secret variable, a script):

1. Open it, and go to Settings → Updates → Check for updates.
2. Install the update. The app restarts on the new version.
3. Everything is still there, the secret still decrypts, and Settings → Backups lists one taken "before update from <old> to <new>".

On Linux the updater replaces only the program file. The menu entry and icon stay as installed until the package is reinstalled.
