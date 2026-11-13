# AGENTS.md: development rules

Every agent that plans, writes or reviews a change in this repository follows this file and the
files it names.

OpenSynapse is a Windows desktop app that configures Razer mice and other peripherals over HID. It
is one Go process (`cmd/opensynapse`, with the device protocols in `internal/`) that shows a web
page (`web/`) in a WebView2 window through MyGo. There is no server, no account and no database:
what the app must protect is the user's devices, the user's settings and the update path.

## Rule priority

When two rules disagree, the higher one wins. Within a level, the more specific rule wins over the
general one, and the owner's newer statement wins over an older one.

1. **Hard limits.** Nothing below overrides these:
   - no secret in logs, the settings file, the page's storage or source (the update signing key
     lives only in the CI secret `MYGO_UPDATER_PRIVATE_KEY`);
   - no release step without the owner's explicit go: pushing a `v*` tag, publishing a draft
     release, uploading assets, or changing the update public key in `mygo.config.ts`;
   - no test or check that writes to a real device or to the user's real settings file unless the
     owner allows it (tests run on a temporary `APPDATA`, see `cmd/opensynapse/main_test.go`);
   - no AI attribution (when an upstream project requires AI disclosure, stop and ask the owner;
     write no disclosure yourself);
   - no bypassing commit signing, update signing, review or a gate by switching it off
     (quarantining a flaky test with an owner and a date is the one allowed exception).
2. **The owner's explicit instruction for the current task.**
3. **The owner's recorded rulings.** A ruled behaviour is not a finding and is not asked again.
4. **This repository's files.**
   - `REQUIREMENTS.md` decides numbers, gates and exceptions.
   - This `AGENTS.md` decides rules.
   - An `AGENTS.md` in a subdirectory decides for the files under it.
5. **Shared skills, templates and tool defaults.**

When the rules agree but the goals pull apart, weigh them in this order:

1. the user's devices: no write that leaves a device unusable or in a state the user did not ask
   for, and only signed updates install;
2. integrity of the user's settings, scenes, macros and backups;
3. not breaking existing users: older settings files, installed and portable copies, the update
   path;
4. the agreed schedule;
5. the smallest change (no over-design);
6. consistency;
7. polish.

A higher goal never justifies more than the smallest change that meets it. When this order does not
settle a conflict, write it into the open-questions list and ask the owner.

## Where the rules live

| File | Read by | Holds |
| --- | --- | --- |
| `AGENTS.md` (this file) | every agent, loaded automatically (`CLAUDE.md` imports it) | priority, hard limits, workflow, reporting, writing, commits |
| `.github/AGENTS.md` | agents changing `.github/` or `scripts/check*` | CI tiers and gate rules |
| `web/AGENTS.md` | agents changing files under `web/` | layout, type, loading, i18n, accessibility |
| `cmd/opensynapse/AGENTS.md` | agents changing files under `cmd/opensynapse/` | bindings, settings compatibility, logging, updates, background work |
| `internal/AGENTS.md` | agents changing files under `internal/` | device protocols, device writes, catalogs |
| `REVIEW.md` | reviewers; a review brief names it | how to review and report |
| `REQUIREMENTS.md` | everyone | numbers, gates, exceptions |

Read the `AGENTS.md` of every directory a change touches before editing it; not every tool loads
nested files on its own. Each rule is written in one file only. A rule a tool can check belongs in
a gate (test, typecheck, `scripts/check.sh`); prose is for rules that need judgment.

## Scope

- Apply these rules to the project as it is. Not every change involves every item.
- Skip sections the project does not have, and mark their headings under "Does not apply because"
  in `REQUIREMENTS.md`, "Enforcement".
- Do not add features, abstractions or tests only to satisfy this checklist.
- An item marked "(if present)" applies only when that feature already exists.
- These files hold rules only. The project's numbers, gates and exceptions (test time limits,
  sizes, window sizes, timeouts) live in `REQUIREMENTS.md`, which wins on any number.
- Every `###` heading in every rule file checked under `REQUIREMENTS.md`, "Rule files", is a rule
  group with one row in "Enforcement": a gate where a tool can measure it, a review criterion
  where only judgment can, both, or the reason it does not apply. The requirements check fails on
  a group without a row. Do not invent metrics for judgment rules. Use `###` only for rule groups;
  explanations go in a section's opening text.
- A rule that needs its own gate, apart from its heading, carries a tag `[rule: <id>]` and gets
  its own row, named `path#id`. Tag only a rule whose failure damages a device, lets an unsigned
  update in, exposes a secret, or loses the user's settings.

---

## General (every change)

### Change risk

Class each change by what it can break, not by the files it touches. The class sets how much proof
the change needs; it never relaxes a hard limit. When unsure between two classes, take the higher.

| Class | Examples | Plan | Proof | Review |
| --- | --- | --- | --- | --- |
| Low | copy, translations, docs, styling, a test-only change | the commit message | the checks of the touched area | the author's own diff read and the automated checks |
| Medium | a feature, a binding change, a refactor, a read-only device query, a new catalog entry | a short plan in the pull request | tests for the changed behaviour; impact checked | one diff review in a fresh context |
| High | a new or changed device write (key map, macro, saved lighting, DPI stages, polling rate, factory reset), the settings-file format, backup import, the updater, release signing or the release workflow, deleting user data | a written plan, reviewed before coding | a test that fails without the fix; every behaviour hunk ablated; the write tested against recorded device replies, then on the real device by the owner or with the owner's leave | an independent review by a different model family |

### Workflow

- Plan before coding, as far as the change's risk class asks: scope, impact, and how the change
  will be verified.
- Do not stop between milestones to wait on something that can run in the background.
- No over-engineering, no over-fixing, no cutting corners.
- A change must be a real improvement, not added complexity or a scattered patch.
- Fix a low-severity finding before release only when all three hold:
  - it is reachable;
  - it does concrete harm: a device gets a write the user did not ask for, the user's settings
    are lost, or a secret reaches a log or the settings file;
  - the fix is small.
- Record every option or finding you set aside in the deferred-options list (`REQUIREMENTS.md`,
  "Deferred options"), with its source and the reason it waits. The owner decides from that list
  after release.

### When unsure

- Fix a verified defect inside the agreed scope, then report it.
- Do not decide or change anything when you cannot tell whether it is intentional, or when the
  change goes beyond the agreed scope.
- Write the question into an open-questions list and keep working on other unblocked tasks.
- Ask the owner all open questions together at a good stopping point. Ask at once only when the
  question blocks all remaining work.
- Check reachability before asking about or coding an edge case. A state that cannot occur gets no
  design. A device model or firmware nobody owns counts as reachable only when the catalog lists
  it.

### Waiting

- Do not sit idle on one long wait (build, test, CI, subagent, owner reply).
- Wait with one background command or a completion notification, with a long timeout as the
  fallback. Never poll at short intervals.
- Do other work in the meantime.
- With several jobs in flight (workflows, subagents, CI runs), and a tool that can schedule a
  wake-up, set one at the status check interval in `REQUIREMENTS.md`. At each wake-up, read the
  status of every job in one pass and write it to the STATE file. Collect what finished, report
  what failed, and record what is stuck. A completion notification can be lost; the wake-up is
  what finds it.
- A wake-up reads status and returns to work; it never waits for a job. Its interval is minutes,
  not seconds, so it is not polling.
- If something is still stuck after the timeout (`REQUIREMENTS.md`), record it with its current
  state and move on.

### Reporting

- Report what was actually run: the commands, the tests and their results.
- Say clearly what was not verified, and on which device and firmware a device change was tried.
  Never claim "tested" without running it.
- A check that reported nothing is evidence only when it saw real content. A screenshot of the
  "no devices" state proves nothing about a device page.

### Code quality

- Readability first; keep the logic clear.
- No spaghetti code.
- Remove useless comments, duplicate code and leftovers (dead code, debug output, temp files).
- Use the current idioms of Go and TypeScript, but nothing new for its own sake. Adopt a newer
  library or language feature when it removes code or a known defect; say which in the pull
  request.
- When rewriting, match the existing code style, unless the owner has set a different convention.

### Impact and edge cases

- Check how the change affects other areas: the tray menu, scenes and game detection, and every
  vendor package that shares the changed code.
- Do not "fix" an intentional design choice as a bug (see "When unsure").
- Handle edge cases reasonably (a device unplugged mid-request, a second instance, a read-only
  portable folder); no excessive defensive code.

### Secrets

- Never write keys, tokens or credentials to logs.
- Never store them in the page's storage (localStorage, sessionStorage, IndexedDB, cookies) or in
  the settings file.
- Never hardcode them in source. The update public key in `mygo.config.ts` is not a secret.

### Performance baseline

- Measure a baseline before a rewrite or an optimization.
- After the change, results must not be worse than the baseline.
- Measure both sides on the same host with its load recorded. Repeat a run before calling a
  difference real.

### Tests (if present)

- Keep tests lean and focused on critical paths.
- A bug fix comes with a test that fails without the fix. When a fix cannot have a test (build
  config, docs, a one-off script, behaviour only real hardware shows), say so in the report and
  give the command or the device steps that verified it.
- A new check must be shown to fail on a planted violation before it counts.
- Ablation applies to a fix with two or more behaviour hunks. Remove each hunk once; a test must
  turn red.
  - In a High-risk change, ablate every hunk.
  - Elsewhere, ablate every hunk, or list in the report the hunks that were not ablated.
  - A one-hunk fix needs no extra step: the test that fails without the fix already shows it.
- The project sets a test time limit and measures against it. It also states which tier runs where
  and when (pre-commit, merge, release). Both are in `REQUIREMENTS.md`, "Commands".

### Merging

- A change merges once the gate is green and the review its risk class asks for has passed.
- A visible UI change also waits until the owner has seen screenshots or a preview.
- A release never merges or tags on these rules alone; it waits for the owner.

### Model usage

The orchestrator applies these; a sub-agent cannot choose its own model.

- Rank models by importance and pick the highest tier the change calls for:
  - device writes, the updater, settings compatibility and architecture decisions get the
    strongest model;
  - non-core code and UI specs get the next tier;
  - mechanical, fully specified work, translation passes, search and log triage get a cheaper
    tier.
- Review a High-risk change with a different model family from the writer; for other changes, a
  fresh agent with none of the writer's context is enough. When only one family is available, the
  owner reviews High-risk changes.
- Never trust a cheap model's output directly; verify it first.
- When a request names a model and effort, write them into the brief and the launch command by
  full id, never as an alias or a substitute. Within the first minute, read the served model from
  the run's own record (transcript or event log) and stop a run on the wrong model. Copy the
  recorded id into the report.
- Long jobs:
  - Update the STATE file at every milestone.
  - Hand the job to a fresh agent, started from the STATE file, when the tool shows context use
    above the handoff limit in `REQUIREMENTS.md`, or when the agent re-reads files it already
    read or repeats a step that failed.
  - Prefer a phase boundary (plan, build, review) as the handoff point.
  - A job that fits in one context does not hand off.

### Documentation

- When behaviour changes, update the text that describes it in the same change: `README.md`,
  `docs/README.zh-CN.md` and `docs/README.ja.md` together, and `CHANGELOG.md` for a change users
  notice.
- When a page shown in `docs/screenshots/` changes visibly, retake its shots in every locale, or
  list the stale shots in the report.

### Writing (docs, comments, reports, PR and issue text)

- **Order.**
  - Lead with the result, failure, decision or required action.
  - One action, result, reason or constraint per sentence.
  - Name the actor and the object; one term per concept.
- **Evidence.**
  - Verify every command, path, option, version, default and performance claim before writing it.
  - Label what is unverified. Never turn an expectation into a fact.
- **Style.**
  - Delete introductions, recaps, repeated conclusions, filler and promotional adjectives (robust,
    seamless, leverage, crucial, enhance).
  - No Emoji decoration, no bold wall, no em-dash rhythm.
  - Headings are sequential and name a task or subject.
- **Document type.** One reader goal per page, in one type: tutorial, how-to, reference or
  explanation.
- **README.** It opens with what the project is, how to install it, and the shortest successful
  use.
- **Comments.**
  - Comments state a contract or a reason the code cannot express: an invariant, a protocol
    quirk, a compatibility constraint, a workaround.
  - Delete comments that restate the code.
  - Comments are in English, like the rest of the code.
- **Chinese and Japanese text.**
  - Follow the Microsoft localization style guides for Simplified Chinese and Japanese
    (https://learn.microsoft.com/globalization/reference/microsoft-style-guides).
  - One script per document; never mix Simplified and Traditional.
  - Put a space between CJK and Latin letters or digits.
  - Use the established technical term, and the term Windows uses for the same thing.
- **Interface copy.**
  - Labels name the action.
  - An error says what happened and what to do next.
  - Users never see a raw HID, Windows or Go error without that sentence.

### Commits and pull requests

- **Scope.** One logical change per commit. Formatting and import reordering go in their own
  commit.
- **Subject.**
  - Short and imperative, at most 72 characters, naming the behaviour that changed.
  - In the repository's form, `type(scope): subject` in lower case, for example
    `fix(via): ask for the model when several match the keyboard equally`. Types in use: `feat`,
    `fix`, `docs`, `test`, `build`, `ci`, `chore`.
  - In English.
- **Body.**
  - Why: the prior failure, why the new behaviour is correct, the compatibility consequence, and
    the issue id.
  - No work diary.
  - No routine test report: a green gate is a precondition, not a result.
- **Releases.** A release commit is `chore(release): <version>` and changes only `package.json`'s
  version and `CHANGELOG.md`.
- **Signing and mechanics.**
  - Every commit is signed (the repository signs with SSH). Never turn signing off to get past a
    stuck agent; fix the agent.
  - Write the message to a file and use `git commit -F`; use `--body-file` for PRs, never inline
    text with backticks.
  - Do not override the repository or global git config on the command line.
- **No AI attribution anywhere:**
  - no `Co-Authored-By` for a model;
  - no "generated with";
  - no tool or model names;
  - no session links.

  The author is the maintainer.
- **Fixups.** Squash `fixup!` and `squash!` commits before pushing.
- **Pull request description.** In this order, when each applies:
  1. the risk class and what in the diff puts it there;
  2. what fails or is needed;
  3. why;
  4. what changes and why that is correct;
  5. how the risky path was verified, with the exact commands and the devices tried;
  6. what remains unverified or limited.

  Leave out empty template sections and social closings.

---

## Both sides

### Page–Go mapping

- The page calls Go only through the generated bindings in `web/src/mygo.ts`. Regenerate them with
  `bun run generate` after changing a bound Go type; never edit `mygo.ts` by hand.
- Every bound method or event the page does not use has a written reason (the tray, the updater
  window), or it is removed.
- Errors from Go map to the page's notice levels by meaning: a refused or failed device write is
  an error, an unplugged device is a warning or an empty state.
- The page makes no network request of its own. The only network access is the Go side's update
  check.
