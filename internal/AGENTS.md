# internal/AGENTS.md: device protocol rules

Rules for every change to the files under this directory: one package per device protocol
(`razer`, `via`, `leobog`, `evision`, `walkplay`), plus `lightshow` and `winaudio`. The root
`AGENTS.md` (rule priority, workflow, writing, commits) applies as well; numbers are in
`REQUIREMENTS.md`.

## Device packages

### Package boundaries

- Windows HID and COM calls live in `*_windows.go`; report building and parsing stay
  platform-independent, so that unit tests cover them.
- A package returns errors and does not log; `cmd/opensynapse` decides what the user sees.
- A protocol quirk that the code cannot express (a transaction id, a firmware that answers late)
  has a comment that names its source.

### Device writes

- A write that changes what the device stores (key map, macro, saved lighting, DPI stages,
  polling rate, factory reset) is sent only for a model the catalog identifies. When several
  models match equally, writes wait until the user picks one. [rule: device-writes]
- The value a write replaces is read and kept first, so that a failed write can be rolled back;
  a write that fails part-way leaves no binding that points at nothing.
- A command the device does not support is not sent; its control is hidden or refused before it
  reaches the device.

### Requests and replies

- A request accepts only a reply that matches the whole request, and ignores stale replies.
- Every read has a timeout. When the device is unplugged (its reply channel closes), a request
  ends at once with an error.
- Lengths, indexes and coordinates from a reply or a definition are bounds-checked before use; a
  value out of range is skipped or refused, never indexed.

### Catalogs and data

- `*/data/*` is generated: `internal/razer/data/devices.json` by `scripts/gendevices.py`,
  `internal/leobog/data/models.json` by `scripts/genkeyboards.ts`, and
  `internal/via/data/keyboards.json.gz` by `scripts/genvia.ts`. Change the generator and
  regenerate; never edit the data by hand.
- The generator's header names its source; when the source has commits, the generated file
  records the one it read. Only data is taken from a source, never code.
- Data under a license other than the project's carries that license in `LICENSES/` and in the
  release.

### Tests

- Protocol tests use recorded reports and byte tables; they never open a real device.
- A fix for a device's reply or timing comes with the recorded bytes that showed the defect.

### Performance

- Background reads (battery, lighting frames) keep within the report rate the device needs; no
  read in a tight loop.
- No unnecessary dependencies.
