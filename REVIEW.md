# REVIEW.md: reviewing a change

Every agent that reviews a change in this repository follows this file. The rules the change must
meet are in `AGENTS.md`, `.github/AGENTS.md`, `web/AGENTS.md`, `cmd/opensynapse/AGENTS.md`,
`internal/AGENTS.md` and `REQUIREMENTS.md`; this file says how to review against them and what a
review report must contain. It does not repeat them.

## Rule priority

The rule priority in the root `AGENTS.md` applies. For a review it means: a finding against a hard
limit, a device write, update signing or the user's settings outranks every structure or style
finding, and a style finding never blocks a fix of a higher goal.

## Before you start

- Review read-only. Never edit, commit to, rebase or push the branch under review.
- Record three SHAs in the report: the target's base, the branch head, and their merge-base.
  Review the fixed `base...head` diff, not the two branch tips.
- Read, in order:
  1. the root `AGENTS.md`, the `AGENTS.md` of every directory the diff touches, and
     `REQUIREMENTS.md`;
  2. the plan or brief of this change;
  3. the owner's recorded rulings (`REQUIREMENTS.md`, "Exceptions").
- A behaviour the owner has ruled on is not a finding.
- Do not read other reviews, bot comments or the PR discussion until your own pass is finished.
  Then re-check what they found and you missed, merge duplicates, and mark which findings came from
  them.
- Review in a fresh context with none of the writer's history; for a High-risk change (root
  `AGENTS.md`, "Change risk"), use a different model family from the writer. Put the model id from
  your own run's record in the report. When only one family is available, the owner reviews
  High-risk changes.

## Scope

- Check the risk class against what the diff touches (root `AGENTS.md`, "Change risk"). A class
  set too low is the first finding; review at the correct class.
- Review this change, not the repository. Pre-existing defects are out of scope.
- Exception: a change that makes an existing defect reachable for the first time. Report it, in a
  separate list.
- If the plan has a change map, compare it with the diff first:
  - every node marked as changed must appear in the diff;
  - every file the diff touches must be on the map.
  - A mismatch is the first finding.
- When the change patches a shape that no longer holds, the first finding is "rewrite this, do not
  patch it", naming the function. Signs of such a shape:
  - a third patch in the same place;
  - no invariant anyone can state;
  - callers that need a special case after the fix.

## Every finding has evidence

- A behaviour finding says the code does the wrong thing when it runs: a wrong write to a device,
  lost settings, a crash, a wrong value on screen. Before it goes into the report, reproduce it
  yourself with one of:
  - a failing test (with recorded device replies where a device is involved);
  - a command and its output;
  - a reproduction in the running app, with a temporary `APPDATA`.
- A structure finding says the code is shaped wrongly while it still behaves correctly: duplicate
  or dead code, logic in the wrong layer, file size, naming, unused keys. A precise reading is
  enough evidence for it:
  - `path:line` for every place the claim covers;
  - the search command and its output; for "nothing uses this", show that the search read real
    files;
  - for "this can be deleted", what breaks if the claim is wrong.
- A finding with reading-only evidence is Medium at most. To rate it higher, reproduce it.
- Exception: a behaviour that cannot be triggered safely, because it needs a write to a real
  device, a device model nobody at hand owns, or a real release. Its evidence is a complete static
  trace: `path:line` for every step from input to effect, with the condition at each branch. Mark
  it "static, not reproduced: <reason>". It keeps the severity the trace supports.
- A pass-bar item blocks through the pass bar, not through its severity.
- Report what you ran and what it showed.
- What you could neither reproduce nor show by a precise reading goes into a separate
  "Hypotheses" list, with the command that would settle it. A hypothesis never counts as a
  finding.
- Spend reproduction effort in proportion to the risk class and the severity. A suspected defect
  that would take longer to reproduce than the change took to write goes to "Hypotheses" with its
  command, unless it is High-risk.
- Check reachability first. A state that cannot occur with the catalogued devices and real
  settings files is not reported.
- Count carefully. Measure exactly the thing the sentence claims, not a wider set (for example,
  calls versus comment lines that mention the name).

## Severity proves itself

| Severity | Meaning | Required evidence |
| --- | --- | --- |
| Blocker | A device left unusable or holding a write the user did not ask for, an unsigned update installed, a secret exposed, the user's settings destroyed, or the app unable to start. | The trigger path: input, state, what happens. Reproduced, or a complete static trace under the exception above. |
| High | The same classes, behind a precondition ordinary use can meet. Or a core flow that fails for a whole device family or locale. | Trigger path and reproduction, or a complete static trace under the exception above. |
| Medium | A real defect with limited reach: a setting that does not stick, a wrong value the user would notice, a recoverable stuck state. | Reproduction, or a precise reading for a structure finding. |
| Low | Correct but fragile, misleading or untidy. | Reproduction or a precise reading. |

- A severity without a trigger path is downgraded until it has one.
- Do not report as a finding a change that removes a protection on purpose and within scope.
  Report it only when you judge the author missed a consequence, and name that consequence.

## Pass bar

These block a pass unless the author states why this case is different:

- A clearly simpler way would remove a whole block of complexity, and the change keeps it.
- A file moves above the repository's p90 size (`REQUIREMENTS.md`, "Numbers") with no reason
  given for not splitting it.
- A special case is inserted into an existing path and makes it harder to read.
- A local problem is solved by scattering device or vendor checks through shared code.
- A wrapper, conversion or optional layer is added that buys no clarity.
- A near-duplicate of an existing helper is written.
- A mechanism the platform, MyGo or a dependency already provides is rewritten or wrapped.
- A script or CI workflow carries more lines than its peers in comparable projects, without a
  reason for each extra line.
- A new comment restates the code, or is denser than the code around it.
- Logic sits in the wrong layer (protocol code in `cmd/opensynapse`, app decisions in
  `internal/`, device logic in the page).

Behaviour being correct is not a reason to pass; it only means nothing regressed.

## Checking against the rules

Go through `web/AGENTS.md`, `cmd/opensynapse/AGENTS.md` and `internal/AGENTS.md` for the files
the diff touches, rule by rule. This section only says how to verify the rules a diff cannot show.

- **Layout, type and loading.**
  - Open the screenshots yourself: minimum and default window size, normal and glass appearance,
    English and a CJK locale, a real device.
  - Measure edges, gaps and title positions on the shot; do not judge by eye.
  - A shot of "no device connected" does not count for a device page.
- **Copy and i18n.** Run `bun run typecheck` and `go test ./cmd/opensynapse -run TestMessageKeys`.
  Read every changed string in every locale.
- **Accessibility.** Tab through the changed flow. Measure contrast with a tool.
- **Device writes.**
  - Reproduce with recorded replies: the ambiguous model, the unsupported command, a reply that
    does not match, an unplug in the middle of a write, a write that fails part-way.
  - Check that the value replaced is kept and restored on failure.
  - A test that only checks the bytes sent never proves the rollback.
- **Settings and backups.** Load the settings file of the previous release and a corrupt one
  under a temporary `APPDATA`; import a backup the previous release exported.
- **Updates and releases.** For a diff touching `update.go`, `mygo.config.ts` or
  `release.yml`: trace that only a file signed by the release key installs, and that the secret
  reaches only the signing step.

Review-only checks that no development rule covers:

- **Local setup.** Flag a change that breaks other people's local run:
  - a new tool or a changed version in `go.mod` or `package.json`;
  - a new generated file that must exist before `go build` works;
  - a script that must run once before development can continue.
- **Hidden controls.** A control hidden for a device (a factory reset it lacks) is also refused
  on the Go side.
- **Newly reachable defects.** List a pre-existing defect that this change makes reachable for the
  first time.

## CI, docs and commits

- **CI.**
  - Each check sits in the right tier: pre-commit, merge gate or release.
  - No new giant all-in-one job, and no one-job-per-trivial-check sprawl.
  - A new check is shown to fail on a planted violation, and fails when it scans nothing.
  - Every gate command in `REQUIREMENTS.md`, "Enforcement", is run by a tier in "Commands" (name
    where) and fails on a planted violation. The requirements check only shows that its files
    exist.
- **Docs.**
  - Behaviour changes update the three READMEs and `CHANGELOG.md` in the same change.
  - Every command, path and number in the docs matches the code.
  - A Due date in `REQUIREMENTS.md`, "Not yet enforced", moved later carries the owner's approval,
    quoted in the commit body. So does a tagged rule (`path#id`) marked as not applying.
- **Commits.**
  - One logical change each; a `type(scope): subject` in the imperative; a body that says why.
  - Signed.
  - No AI attribution, no work diary, no routine test report.
  - No `fixup!` or `squash!` subjects left.

## Report

```text
Verdict: PASS | FIX FIRST | BLOCK        (one line, first; rule below)
Model: <id from the run record>   Base / head / merge-base: <sha> <sha> <sha>
Covered: files and flows read; Not read: what and why
Findings, by severity, then by the order below:
  [severity] path:line, one-sentence defect
    scenario: input → state → wrong result
    evidence: command or test and what it showed; or path:line and the search output
    wanted: the smallest change that closes it
Made reachable by this change (pre-existing defects): ...
Hypotheses (no evidence yet): each with the command that would settle it
Over-design: against the pass bar
Deferred-options candidates: options set aside, with their reason
From other reviewers: which findings came from them
```

- Verdict rule:
  - BLOCK when there is any Blocker;
  - FIX FIRST when there is any High or Medium, or any pass-bar item;
  - PASS when only Lows remain; each Low is fixed or put on the deferred-options list by the low
    rule in the root `AGENTS.md`.
- Order findings by severity first. Within one severity, order them by structure:
  1. structural regression (harder to read, more coupled, more stateful);
  2. complexity that could be deleted outright;
  3. branch complexity added to an existing path;
  4. boundary and contract problems;
  5. file and module size;
  6. readability.
- Do not bury one large problem under ten small ones. Three high-confidence findings beat fifteen
  mixed ones.
- Keep the report short. It holds no restated diff and no list of checks that passed.
