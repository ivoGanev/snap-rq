# Backups

Started 2026-11-20 on `feat/backups` (built on `feat/encrypted-cookies`, PR #56: it uses the
secrets key).

## Decisions

- **D1. A backup is the whole database**, one consistent snapshot: requests, responses,
  history, undo log, settings. Taken with `VACUUM INTO`, gzip-compressed, AES-256-GCM
  encrypted. Measured on a 54 MB database: 7.7 MB, under a second.
- **D2. No periodic backups** (the app isn't always open). Backups are taken **before risky
  operations**, **before an app update** runs migrations, **before a restore**, and on
  **"Back up now"**.
- **D3. Risky operations** (each switchable in Settings → Backups): delete project, delete
  collection with requests, delete environment with variables, delete **10 or more**
  requests at once (threshold adjustable), permanent delete / empty Trash, OpenAPI import,
  "Start over" for secrets. **Every** risky change gets its own backup, however recent the
  last one (a 2-minute throttle was tried and removed: it skipped backups silently and could
  miss changes). If the backup fails, the operation is cancelled.
- **D4. Encrypted with the existing secrets key.** Each backup has its own data key, sealed
  with the secrets key and, when the optional backup password is set, also with a key
  derived from it (Argon2id). Either opens it. The header records the key's fingerprint.
- **D5. The files are the source of truth.** The history is read from the backup folder, so
  it works even when the database is broken. File name: `<date time> – <reason>.snaprq-backup`.
  The reason is readable (also in the header); the data is not.
- **D6. Retention by count, per kind:** the newest **10** backups before risky changes and,
  separately, the newest **5** before updates (both adjustable). Manual and "before restore"
  backups are never deleted automatically. No age limit.
- **D7. Restore** checks the backup, takes a "before restore" backup, and swaps the database
  in on the next start, before it is opened. Backups from a newer format are refused.

## Losing the key

| Situation | Restore |
|---|---|
| Same computer, key present | Works directly |
| New computer or key lost, recovery key at hand | Enter the recovery key |
| Key replaced ("Start over") since the backup | The **old** recovery key, or the backup password |
| Key and recovery key lost | Only with the backup password; otherwise unrecoverable |

After a key is replaced a fresh backup is taken with the new key, so the newest backup
always opens with the current recovery key.

## Steps

- [x] **B1. Format and engine** (`backend/backup`): header + envelope encryption, Begin
  (snapshot + sealed key, before a change) / Finish (compress, encrypt, write; can run in the
  background), Extract, List, leftover cleanup. Tests.
- [x] **B2. Settings and history commands**: backup settings in `app_settings`;
  `backup.list`, `backup.create`, `backup.delete`; retention.
- [x] **B3. Before risky operations**: commands declare `BackupBefore`; the runner takes the
  snapshot first; throttle; toast.
- [x] **B4. Before app updates**: open the database, back up when the version changed, then
  migrate.
- [x] **B5. Restore**: pending restore, swap on start, asking for the recovery key or password.
- [x] **B6. Settings → Backups UI.** Automatic backups switch with each trigger and the
  request threshold; folder (Change… / Default / Open); keep N for D days; optional backup
  password; history with Restore / Show / Delete; Back up now; Restore from file…. A reminder
  to save the recovery key while it's unseen. Restore asks for the recovery key or password
  when the backup was made with another key, then restarts. A "Backup saved" toast's "Show"
  opens this page.

## Manual checks

| Step | Check |
|---|---|
| B6 | Back up now: a file appears in the folder; the history shows it. |
| B3 | Delete a collection with requests: toast "Backup saved…". Delete another right after: a second backup. Delete 9 requests: no backup; 10: backup. |
| B5 | Restore a backup: the app restarts with that data; a "before restore" backup exists. On another computer: asks for the recovery key. |
