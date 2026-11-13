# .github/AGENTS.md: CI rules

Rules for every change to CI configuration and gate scripts. The root `AGENTS.md` applies as well;
tier time limits and commands are in `REQUIREMENTS.md`. Tools load this file only for files under
`.github/`; name it in the brief when a change touches a gate script under `scripts/`
(`check.sh`, `check-requirements.py`, `test_check_requirements.py`).

## CI

### Tiers, jobs and gates

Both mistakes are wrong: over-designing CI, and putting everything into one CI run. This project
has three tiers. Its whole suite runs in seconds, so the merge gate runs all of it and there is no
scheduled run; add one only when a suite becomes too slow for the merge gate.

- **Split by cost and timing.** Each tier has its own time limit in `REQUIREMENTS.md`.
  - Pre-commit (`scripts/check.sh --fast`) runs the read-only fast checks, in seconds.
  - The merge gate (`scripts/check.sh`, from `.github/workflows/ci.yml`) runs every check and
    test, on `windows-latest` because the HID code is Windows only.
  - A gate's self-test plants one violation and expects red; that is what shows the gate is
    wired. The requirements check's self-test runs in every merge gate.
  - The release (`.github/workflows/release.yml`, on a `v*` tag) adds release-only steps: the
    version check against `package.json`, the signed installer and update files, the portable
    zip. It runs only on the owner's go. It also calls the merge gate (`ci.yml`) on the tagged
    commit, beside the build; publishing waits for that gate, so a red gate leaves the release
    a draft. This is the one deliberate second run of the merge gate: it ties the signed release
    to the exact commit that passed.
- **One job per concern, grouped by the area it guards.** Do not build one giant job, where one red
  step hides the rest and every rerun costs everything. Do not build one job per tiny check either.
- **No CI nobody needs.**
  - No OS or version matrix the project does not support (it supports 64-bit Windows only).
  - No cache layer that saves seconds.
  - No duplicate runs of the same suite in two tiers without a reason.
  - Required checks are only the ones that decide a merge.
- **Same script in CI and locally.** CI calls `scripts/check.sh`. Use an existing action pinned by
  SHA instead of copying its code into the repository.
- **Checks that can turn red.**
  - A rule without a check that turns red is a preference.
  - Every check fails on a planted violation, and fails when it scans nothing.
  - Exit code 1 means a violation was found; exit code 2 means the check could not run.
- **Flaky tests.** Fix a flaky test, or quarantine it with an owner and a date. Never retry it
  silently. Quarantine is the only allowed way to take a check out of a gate; it is not "switching
  the gate off" under the hard limits.
- **Release secrets.** `MYGO_UPDATER_PRIVATE_KEY` is passed only to the step that signs, never to
  a step that runs code from a pull request.
