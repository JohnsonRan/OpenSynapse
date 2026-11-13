# REQUIREMENTS.md: numbers, gates and exceptions

Commands, numbers, gates and exceptions for this repository. The rules themselves are in
`AGENTS.md`, `.github/AGENTS.md`, `web/AGENTS.md`, `cmd/opensynapse/AGENTS.md` and
`internal/AGENTS.md` and are not repeated here. When another file in this repository disagrees with
this one on a command, a number, a gate or an exception, this file wins.

`scripts/check-requirements.py` checks this file in the merge gate. It reads files and runs
nothing. Write a pipe inside a cell as `\|`. Write a command in backticks; the check confirms that
a script named in it exists.

## Rule files

Every `AGENTS.md` in this repository, by path. The check reads rule groups only from the files
marked `yes`, and fails on an `AGENTS.md` it finds that is not listed. It searches every directory
except `.git`, `node_modules`, `vendor`, `third_party` and hidden directories other than
`.github`. Mark a file `no`, with the reason in Note, when its rules are not this project's (a
vendored package, a generated tree).

| File | Checked | Note |
| --- | --- | --- |
| AGENTS.md | yes | |
| .github/AGENTS.md | yes | |
| web/AGENTS.md | yes | |
| cmd/opensynapse/AGENTS.md | yes | |
| internal/AGENTS.md | yes | |

## Commands

One row per tier in `.github/AGENTS.md`, "Tiers, jobs and gates". Write the exact command that a
person and CI both run. Fill in this order: the command; then three runs on the same host, recorded
as the baseline; then the limit, as the baseline plus a margin (start at 20%).

The local host is the owner's workstation (Ryzen 9 9950X3D, Windows 11, Go and Bun caches warm).
The CI host is GitHub's `windows-latest`.

| Tier | Command | Runs (where, when) | Wall-time limit | Baseline (date, host, load) |
| --- | --- | --- | --- | --- |
| Pre-commit | `scripts/check.sh --fast` | locally, every commit, by `scripts/hooks/pre-commit` once `git config core.hooksPath scripts/hooks` is set | 1.5 s | 1.18 s, 1.14 s, 1.12 s (2026-11-14, local, CPU 1%) |
| Merge gate | `scripts/check.sh` | CI (`ci.yml`), every push to master and every pull request, and on a `v*` tag, called by `release.yml` before it publishes; locally before a pull request | 5 s locally | 4.1 s, 3.0 s, 3.0 s (2026-11-14, local, CPU 1%, Go test cache warm) |
| Release | `bun run build -- -platform windows/amd64 -upload` | CI (`release.yml`), on a `v*` tag the owner pushes | 4 min 10 s (whole workflow) | 3 min 26 s, 2 min 44 s, 2 min 45 s (2026-10-28 to 2026-10-30, CI, load not reported) |

## Numbers

Every limit has a measured baseline next to it. Record the date, the host and its load for each
baseline. Leave the limit empty until it has been measured, and list it under "Not yet enforced"
meanwhile.

| Item | Limit | Baseline (date, host, load) | Enforced by |
| --- | --- | --- | --- |
| Type scale (role: size / line height) | page title 26 px; card title 15 px; body 14 px; section heading 13 px; control text 13 px; card heading 12 px; small 12 px; meta 11 px; field value 16 px; battery figure 48 px and its unit 18 px; key cap 10 px. Line height 1, 1.3 (titles), 1.6 (prose) | a setting, not measured: the `--text-*` and `--leading-*` custom properties on `:root` in `web/src/style.css` (2026-11-14) | review |
| Spacing scale (section, card, inside card, label to control) | page padding 28 / 40 / 48 px; title row or tabs to content 20 px; between page sections 24 px; inside a card 18 × 20 px; a row in a list card 16 × 20 px; between cards 14 px; between fields in a card 12 px; label to control 6 px; between buttons in a row 8 px | a setting, not measured: the spacing custom properties on `:root` in `web/src/style.css` (2026-11-14) | review |
| Content width; readable column width | content at most 1240 px (`--content-width`); readable column 640 px (`--readable-width`) | 804 px at the minimum window, 868 px at the default window (1024 DIP, on a 2560 × 1440 screen at 150%); computed from `style.css` and `main.go`, not measured on a screenshot (2026-11-14) | screenshots |
| Window sizes | minimum 960 × 620; default 60% × 75% of the work area, kept within 960–1320 × 620–900 | a setting, not measured (`cmd/opensynapse/main.go`, `windowSize`) | screenshots |
| Full Go test run, wall time (`go test -count=1 ./...`) | 2 s | 1.51 s, 1.64 s, 1.54 s (2026-11-14, local, CPU 1%) | review |
| Slowest test package | 0.7 s | 0.55 s, `internal/walkplay` (2026-11-14, local, CPU 1%) | review |
| Page JavaScript, gzip (one chunk, so also the entry chunk) | 46 kB | 38.0 kB (2026-11-14, `bun run build:web`, local) | review |
| Page JavaScript + CSS, gzip | 52 kB | 43.0 kB (2026-11-14, `bun run build:web`, local) | review |
| Background process memory, window closed (private working set, the Memory column of Task Manager) | 20 MB | 16.4 MB, 16.5 MB, 16.5 MB (2026-11-14, local, v0.8.0 portable running two days in the tray, CPU 5%) | review |
| Source file size, p90 of this repository (pass bar) | 442 lines | 442 lines over 79 Go, TS, CSS, Python and HTML files, generated `web/src/mygo.ts` left out (2026-11-14) | review |
| Wait timeout before a stuck step is recorded and left | 30 min | a setting, not measured | agent |
| Context handoff limit (share of the model's window) | 60% | a setting, not measured | orchestrator |
| Status check interval while several jobs run | 10 min | a setting, not measured | orchestrator |
| Commit subject length | 72 characters | a setting, not measured | review |

## Enforcement

One row per rule group of every file checked under "Rule files": each `###` heading, named
`path: heading`, and each rule tagged `[rule: <id>]`, named `path#id`. The check reports a group
with no row, and a row whose group no longer exists, so renaming a heading means renaming its row.

Fill one or both of:

- Gate command: the command, in backticks, that turns red on a violation, run by a tier in
  "Commands";
- Review criterion: the `REVIEW.md` section that checks it, with the file name in backticks.

Or fill only "Does not apply because" with what the project lacks. A tagged rule marked as not
applying needs the owner's approval, quoted in the commit body.

| Rule group | Gate command | Review criterion | Does not apply because |
| --- | --- | --- | --- |
| AGENTS.md: Change risk | | `REVIEW.md`, "Scope" | |
| AGENTS.md: Workflow | | `REVIEW.md`, "Pass bar" | |
| AGENTS.md: When unsure | | `REVIEW.md`, "Every finding has evidence" | |
| AGENTS.md: Waiting | | `REVIEW.md`, "Report" | |
| AGENTS.md: Reporting | | `REVIEW.md`, "Every finding has evidence" | |
| AGENTS.md: Code quality | `go vet ./...` | `REVIEW.md`, "Pass bar" | |
| AGENTS.md: Impact and edge cases | | `REVIEW.md`, "Scope" | |
| AGENTS.md: Secrets | | `REVIEW.md`, "Checking against the rules", Updates and releases | |
| AGENTS.md: Performance baseline | | `REVIEW.md`, "Pass bar" | |
| AGENTS.md: Tests (if present) | `go test ./...` | `REVIEW.md`, "Every finding has evidence" | |
| AGENTS.md: Merging | | `REVIEW.md`, "Report" | |
| AGENTS.md: Model usage | | `REVIEW.md`, "Before you start" | |
| AGENTS.md: Documentation | | `REVIEW.md`, "CI, docs and commits" | |
| AGENTS.md: Writing (docs, comments, reports, PR and issue text) | | `REVIEW.md`, "CI, docs and commits" | |
| AGENTS.md: Commits and pull requests | | `REVIEW.md`, "CI, docs and commits" | |
| AGENTS.md: Page–Go mapping | `bun run typecheck` | `REVIEW.md`, "Pass bar" | |
| .github/AGENTS.md: Tiers, jobs and gates | `scripts/check.sh` | `REVIEW.md`, "CI, docs and commits" | |
| web/AGENTS.md: Components and modularity | | `REVIEW.md`, "Pass bar" | |
| web/AGENTS.md: Consistency | | `REVIEW.md`, "Checking against the rules", Layout | |
| web/AGENTS.md: Layout, type and loading | | `REVIEW.md`, "Checking against the rules", Layout | |
| web/AGENTS.md: i18n (if present) | `bun run typecheck`; `go test ./cmd/opensynapse -run TestMessageKeys` | `REVIEW.md`, "Checking against the rules", Copy and i18n | |
| web/AGENTS.md: Usability | | `REVIEW.md`, "Checking against the rules", Layout | |
| web/AGENTS.md: Accessibility | | `REVIEW.md`, "Checking against the rules", Accessibility | |
| web/AGENTS.md: Performance | | `REVIEW.md`, "Pass bar" | |
| cmd/opensynapse/AGENTS.md: Bindings and modularity | `bun run typecheck` | `REVIEW.md`, "Pass bar" | |
| cmd/opensynapse/AGENTS.md: Compatibility | `go test ./cmd/opensynapse -run "TestLoadConfig\|TestImportConfig"` | `REVIEW.md`, "Checking against the rules", Settings and backups | |
| cmd/opensynapse/AGENTS.md: Logging | | `REVIEW.md`, "Pass bar" | |
| cmd/opensynapse/AGENTS.md: Security | | `REVIEW.md`, "Checking against the rules", Updates and releases | |
| cmd/opensynapse/AGENTS.md#signed-updates | | `REVIEW.md`, "Checking against the rules", Updates and releases | |
| cmd/opensynapse/AGENTS.md: Background work (if present) | | `REVIEW.md`, "Every finding has evidence" | |
| cmd/opensynapse/AGENTS.md: Releases | | `REVIEW.md`, "Checking against the rules", Updates and releases | |
| cmd/opensynapse/AGENTS.md: Performance | | `REVIEW.md`, "Pass bar" | |
| cmd/opensynapse/AGENTS.md: Verification | | `REVIEW.md`, "Every finding has evidence" | |
| internal/AGENTS.md: Package boundaries | `go vet ./...` | `REVIEW.md`, "Pass bar" | |
| internal/AGENTS.md: Device writes | `go test ./cmd/opensynapse ./internal/... -run "Rollback\|Original\|Unsure\|Reset"` | `REVIEW.md`, "Checking against the rules", Device writes | |
| internal/AGENTS.md#device-writes | `go test ./cmd/opensynapse -run TestViaModelsAndUnsure` | `REVIEW.md`, "Checking against the rules", Device writes | |
| internal/AGENTS.md: Requests and replies | `go test ./internal/via ./internal/evision -run "ClosedReply\|StaleReply"` | `REVIEW.md`, "Checking against the rules", Device writes | |
| internal/AGENTS.md: Catalogs and data | | `REVIEW.md`, "Pass bar" | |
| internal/AGENTS.md: Tests | `go test ./internal/...` | `REVIEW.md`, "Every finding has evidence" | |
| internal/AGENTS.md: Performance | | `REVIEW.md`, "Pass bar" | |

## Exceptions

Bound Go methods and events the page does not call. Audited 2026-11-14 (every method and event in
`web/src/mygo.ts` has a caller in `web/src/main.ts`; `Via` and `Lights` are called through `kbApi`).

| Binding | Caller | Reason |
| --- | --- | --- |
| none | | |

Intentional designs that look like defects:

| What | Why | Owner ruling (date, quote) |
| --- | --- | --- |
| On a window wider than the content, the content stops at `--content-width` (1240 px) and sits centred, leaving blank sides | a settings page reads badly stretched across a wide screen; Windows Settings caps its content the same way. It is the one exception to "No large blank areas" in `web/AGENTS.md` | 2026-11-14, the owner left the five layout questions to the agent's proposal: "1-5都按你的想法来" |

## Deferred options

Options and findings set aside (root `AGENTS.md`, "Workflow"). The owner decides from this list
after a release.

| Option or finding | Source | Why it waits |
| --- | --- | --- |
| LEOBOG Reset keys removes the macro bindings and saves before the keymap write, and a failed write is not marked for rewrite (`keyboard_keys.go`, `ResetKeys`); `SetKey` reports success when the keymap write fails | code survey, 2026-11-14 | Medium; needs a busy or unplugged keyboard mid-reset; the owner chose the first four findings |
| A settings section that fails to decode is replaced by its default without a log line or a `.bad` copy, and lost at the next save (`config.go`, `parseConfig`) | code survey, 2026-11-14 | Medium; needs a hand-edited file or a downgrade |
| No background job recovers from a panic; a hand-edited scene with a highlight key of 126 or more panics `runAuto` on every start | code survey, 2026-11-14 | Medium; needs a hand-edited file; the wrapper touches every job |
| A `razer.Device` from an older scan reopens a HID handle that nothing closes; the Razer hub has no `scanMu` | code survey, 2026-11-14 | Low to medium; a handle per stale round |
| Music lighting spins at 100 Hz without frames after `AUDCLNT_E_DEVICE_INVALIDATED` (`winaudio/loopback_windows.go`) | code survey, 2026-11-14 | Low to medium; the invalidation is from the WASAPI docs, not seen on hardware |
| Backup import accepts a binding on the left button and duplicate triggers; import forgets the bindings of a mouse that is asleep | code survey, 2026-11-14 | Low; needs a hand-made backup |
| Switching a VIA keyboard between models with a different matrix width (GMMK Numpad and WOBKEY Rainy 75 2.4G) moves its bindings to other keys | code survey, 2026-11-14 | Low; one pair in the catalog |
| Plugging in a Razer device still re-applies the scene to every Razer device, not only the new one | a950475, cddd50b | the smaller fix covered unrelated devices; a per-device reset needs to know which ID a receiver answers for |
| Game detection may miss a fullscreen game that hovers around 15% 3D load (it needs five samples in a row) or one that keeps decoding video (menus, cutscenes); counting N of the last M samples and a small decode threshold would loosen it | review of 53b2a54, 2026-11-14 | no game was measured; a game it misses can be listed under Programs |
| `runAuto` keeps its 1 s ticker, a JSON marshal and a language lookup while scenes and smart polling are off; music lighting wakes about 120 times a second while silent | code survey, 2026-11-14 | an optimization, measure against the memory and CPU baseline first |
| The page: deleting a macro, scene or EQ preset and importing a backup have no confirmation; some Go calls have no `.catch` | page survey, 2026-11-14 | Low; the owner chose the first four findings |
| The page: scene card heads and DPI stage cards cannot be used with the keyboard; sliders have no accessible name; the toast has no live region; no `prefers-reduced-motion` | page survey, 2026-11-14 | accessibility pass, one change |
| The three HID port readers (`via`, `walkplay`, `evision`) and the five device hubs repeat the same code | code survey, 2026-11-14 | a refactor without a behaviour change |
| Feature ideas: per-key colors, music lighting and scene lighting for Razer keyboards; scenes matched by window title, and a scene locked from the tray; text and launch steps in macros | survey, 2026-11-14 | features; the owner decides after release |

## Not yet enforced

A row of "Commands", "Numbers" or "Enforcement" that is not filled yet goes here, named
`Section: row`, for example `Numbers: Slowest test package`. The merge gate fails on:

- an unfilled row that is not listed here;
- a listed row that is already filled, or that does not exist;
- a Due date that has passed.

Due holds one date (YYYY-MM-DD). Moving it later needs the owner's approval, quoted in the commit
body.

| Row | Current state | Plan | Due |
| --- | --- | --- | --- |
