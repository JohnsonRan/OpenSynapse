# cmd/opensynapse/AGENTS.md: app rules

Rules for every change to the files under this directory: the app's Go side, which owns the
window, the tray, the settings file, scenes and game detection, macros, backups and updates, and
drives the device packages in `internal/`. The root `AGENTS.md` (rule priority, workflow, writing,
commits) applies as well; numbers are in `REQUIREMENTS.md`.

## App

### Bindings and modularity

- Clear boundaries: device protocol code belongs in `internal/<package>`; this package composes
  the devices, the settings and the window.
- Types and methods the page calls are bound through MyGo; after changing one, regenerate
  `web/src/mygo.ts` with `bun run generate` in the same commit.
- No hardcoded paths. The settings live in `%APPDATA%`, or in the portable folder next to the
  executable when the `portable` marker is there; go through the existing helpers.
- No bound method that the page can call but that always fails.

### Compatibility

- A settings file written by any earlier release keeps loading. A field an older version lacked
  gets its default on load; a renamed or removed field is migrated, never dropped silently.
- A settings file that does not parse is backed up before anything else is written, and saving
  stays blocked when that backup fails.
- A backup exported by an earlier release keeps importing.
- Installed and portable copies keep updating: the updater's public key and the release asset
  names that the updater and `install.ps1` look for change only with the owner's go.

### Logging

- One log format: the standard `log` package. Levels by meaning: a failure the user should know
  about also reaches the page as a notice.
- Do not log expected cases as errors (a device unplugged, a game that is not running).
- Do not swallow errors silently.
- No secrets in logs (root `AGENTS.md`, "Secrets").

### Security

- **Updates.** [rule: signed-updates]
  - An update installs only when its signature verifies against the public key in
    `mygo.config.ts`; nothing adds a path that installs an unsigned or unchecked file.
  - The private key exists only as the CI secret `MYGO_UPDATER_PRIVATE_KEY`.
- **Untrusted input.** An imported backup, a settings file, a keyboard definition and a device
  reply are validated before use. A malformed one is refused with a notice, never a panic or a
  partial write.
- **Network.** The update check is the only network access. Changing that changes the README's
  promise and needs the owner's go.
- **Other processes.** Game detection and scene matching read process names, window titles and
  window state only. A window title is read only while a switched-on feature has a title rule; it
  is compared in memory and never logged, stored or sent to the page. Nothing injects into or
  writes to another process.

### Background work (if present)

The tray, game detection, battery polling, audio-reactive lighting and the music and light shows
run in the background while the window is closed.

- A failing or panicking round is contained: it is logged with its job name, it reaches the page
  as a notice when the window is open, and the app and the other jobs keep running.
- A job stops when its device is unplugged and resumes when it returns; it does not spin on a
  closed device.
- No busy loop; every poll has an interval in the code that names why it is that long.

### Releases

- A release is built only from `master`, by `release.yml`, from a tag `v<version>` that matches
  `package.json`.
- Every release step waits for the owner's explicit go (root `AGENTS.md`, hard limits).
- The release carries the license texts the bundled data needs (`LICENSES/`).

### Performance

- Watch the background process's memory and CPU against the baseline in `REQUIREMENTS.md`; the
  README promises a small background process.
- No unnecessary dependencies.

### Verification

- Tests run on a temporary `APPDATA` (`TestMain` in `main_test.go`); a test never reads or writes
  the user's real settings.
- A change to device behaviour is verified on a real device of that model by the owner, or by an
  agent with the owner's leave. The report names the device, its firmware, and what was done.
