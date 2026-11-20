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
  "Start over" for secrets. Skipped when a backup was taken in the last 2 minutes. If the
  backup fails, the operation is cancelled.
- **D4. Encrypted with the existing secrets key.** Each backup has its own data key, sealed
  with the secrets key and, when the optional backup password is set, also with a key
  derived from it (Argon2id). Either opens it. The header records the key's fingerprint.
- **D5. The files are the source of truth.** The history is read from the backup folder, so
  it works even when the database is broken. File name: `<date time> – <reason>.snaprq-backup`.
  The reason is readable (also in the header); the data is not.
- **D6. Retention:** keep the last 20 automatic backups, at most 90 days. Manual backups are
  never deleted automatically.
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
- [ ] **B6. Settings → Backups UI.**

## Manual checks

| Step | Check |
|---|---|
| B6 | Back up now: a file appears in the folder; the history shows it. |
| B3 | Delete a collection with requests: toast "Backup saved…"; a second delete within 2 minutes: no new backup. Delete 9 requests: no backup; 10: backup. |
| B5 | Restore a backup: the app restarts with that data; a "before restore" backup exists. On another computer: asks for the recovery key. |
