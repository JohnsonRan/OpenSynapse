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
| Pre-commit | `scripts/check.sh --fast` | locally, before every commit (no hook installed) | 1.5 s | 1.18 s, 1.14 s, 1.12 s (2026-11-14, local, CPU 1%) |
| Merge gate | `scripts/check.sh` | CI (`ci.yml`), every push to master and every pull request; locally before a pull request | 5 s locally | 4.1 s, 3.0 s, 3.0 s (2026-11-14, local, CPU 1%, Go test cache warm) |
| Release | `bun run build -- -platform windows/amd64 -upload` | CI (`release.yml`), on a `v*` tag the owner pushes | 4 min 10 s (whole workflow) | 3 min 26 s, 2 min 44 s, 2 min 45 s (2026-10-28 to 2026-10-30, CI, load not reported) |

## Numbers

Every limit has a measured baseline next to it. Record the date, the host and its load for each
baseline. Leave the limit empty until it has been measured, and list it under "Not yet enforced"
meanwhile.

| Item | Limit | Baseline (date, host, load) | Enforced by |
| --- | --- | --- | --- |
| Type scale (role: size / line height) | | | style.css custom properties |
| Spacing scale (section, card, inside card, label to control) | | | style.css custom properties |
| Content width; readable column width | | | screenshots |
| Window sizes | minimum 960 × 620; default 60% × 75% of the work area, kept within 960–1320 × 620–900 | a setting, not measured (`cmd/opensynapse/main.go`, `windowSize`) | screenshots |
| Full Go test run, wall time (`go test -count=1 ./...`) | 2 s | 1.51 s, 1.64 s, 1.54 s (2026-11-14, local, CPU 1%) | review |
| Slowest test package | 0.7 s | 0.55 s, `internal/walkplay` (2026-11-14, local, CPU 1%) | review |
| Page JavaScript, gzip (one chunk, so also the entry chunk) | 46 kB | 38.0 kB (2026-11-14, `bun run build:web`, local) | review |
| Page JavaScript + CSS, gzip | 52 kB | 43.0 kB (2026-11-14, `bun run build:web`, local) | review |
| Background process memory, window closed | | | review |
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

Bound Go methods and events the page does not call. Not audited yet; the first change that audits
the bindings fills this table.

| Binding | Caller | Reason |
| --- | --- | --- |

Intentional designs that look like defects:

| What | Why | Owner ruling (date, quote) |
| --- | --- | --- |

## Deferred options

Options and findings set aside (root `AGENTS.md`, "Workflow"). The owner decides from this list
after a release.

| Option or finding | Source | Why it waits |
| --- | --- | --- |

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
| Numbers: Type scale (role: size / line height) | `style.css` uses ten literal font sizes (10 to 48 px) | name the sizes per role as custom properties on `:root`, then measure | 2027-03-31 |
| Numbers: Spacing scale (section, card, inside card, label to control) | gaps and paddings are literal pixel values | name the gaps per relationship as custom properties on `:root`, then measure | 2027-03-31 |
| Numbers: Content width; readable column width | not measured | measure the page widths at the minimum and the default window size | 2027-03-31 |
| Numbers: Background process memory, window closed | not measured; the README states about 20–30 MB | measure the working set after start with the window closed, three times | 2027-01-31 |
