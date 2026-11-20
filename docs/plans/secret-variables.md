# Secret variables

Started 2026-11-20 on `feat/secret-variables`. Phase 1 of the security plan: a "Secret"
tick box per environment variable. Secret values are encrypted at rest, never shown
unless revealed, and only decrypted when a request is sent.

## Decisions

- **D1. Per variable**, not per environment (as Postman and Insomnia do).
- **D2. Only secret values are encrypted.** Names, the secret flag, normal variables,
  requests, cookies and responses are unchanged, so loading and saving stay as fast as
  today. Cookies follow in a later phase (always encrypted, no option).
- **D3. One key.** 32 random bytes (AES-256-GCM), kept in the OS credential store under
  service `SnapRQ`, account `variable-encryption-key`, read once per session and cached
  in memory. Library: `github.com/zalando/go-keyring` (Windows Credential Manager,
  macOS Keychain via `/usr/bin/security`, Linux Secret Service over D-Bus).
- **D4. Decrypt only when needed.** `variable.list` never returns a secret value, not even
  encrypted. Decryption happens in `request.send` (the backend already interpolates),
  in `variable.reveal`, and when MCP masks its output.
- **D5. Never create a new key while encrypted values exist.** A key check (a fixed text
  encrypted with the key, in `app_settings`) tells "first use" from "key missing" and
  "wrong key". Missing or wrong key = locked mode: nothing is overwritten.
- **D6. Recovery key.** The key, shown as `SNRQ-…` groups, can be copied or saved when the
  first secret is created and from Settings later. Entering it in locked mode restores
  everything (also moving to a new computer).
- **D7. No keyring (Linux without Secret Service):** the key goes into `secret.key` next to
  the database with permissions `0600`, and Settings says so.

## Design

- `backend/secrets`: the vault. `Encrypt`/`Decrypt` take the caller's transaction for
  the key check (SQLite allows one writer; a separate connection would deadlock with
  the runner's transaction). Stored form: `v1:` + base64(nonce ‖ ciphertext).
- `environment_variables.secret` (0/1). For a secret the `value` column holds the
  encrypted form; the undo log therefore only ever holds encrypted values.
- Commands:
  - `variable.list` → secret rows have `value: ""`, `has_value`.
  - `variable.create` makes a secret **empty**: the runner keeps an undoable command's
    input in the undo log, so a value there would be stored in plain text.
  - `variable.update` (never logged) encrypts a secret's new value, and leaves it alone
    when no value is given (renaming a secret).
  - `variable.set_secret` → encrypt the current value, or decrypt it back to plain.
  - `variable.reveal` → one decrypted value (read only).
  - `secrets.status`, `secrets.recovery_key`, `secrets.restore`, `secrets.reset`.
- `request.send` takes `environment_id` and fills in that environment's secrets itself;
  the frontend never has them.
- MCP: any secret value in tool output becomes `[REDACTED]`.

## Steps

- [x] **S1. Vault** (`backend/secrets`): key stores (OS keyring, file, memory for tests),
  encrypt/decrypt, key check, locked states, recovery key format. Tests.
- [x] **S2. Secret column and variable commands.** Tests.
- [x] **S3. Send fills in secrets** from `environment_id`. Test.
- [x] **S4. Status / recovery / restore / reset commands.** Tests.
- [x] **S5. MCP masks secret values.** Test.
- [ ] **S6. Variables editor:** Secret tick box, masked value, reveal, write-only typing;
  chip tooltip says "secret"; send passes `environment_id`.
- [ ] **S7. Locked banner and recovery key UI** (first-time prompt, Settings → Security).

## Manual checks

| Step | Check |
|---|---|
| S6 | Tick Secret on a variable: the value turns into dots. Reveal shows it; click away hides it. Click into it and type: the value is replaced; click away without typing: unchanged. Send a request using it: the server gets the real value. Hover its `{{chip}}`: tooltip says "secret". Copy as cURL: shows `{{KEY}}`. Untick: asks first, then shows the plain value. Undo a deleted secret variable: it comes back still secret. |
| S7 | First secret: the recovery key is shown once with Copy / Save. Delete the `SnapRQ` entry from the OS credential store and restart: locked banner; sending a request with that secret fails with a clear message; entering the recovery key unlocks everything. "Start over" clears values but keeps names. |
| all | Open `app.db` in a SQLite viewer: secret values are `v1:…`, never plain. |
