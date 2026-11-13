#!/usr/bin/env python3
"""Tests for check-requirements.py. Run: python3 scripts/test_check_requirements.py

Each test plants one violation in a clean file and expects the check to name it.
"""
import datetime
import importlib.util
import os
import subprocess
import sys
import tempfile
import unittest

SCRIPT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "check-requirements.py")
spec = importlib.util.spec_from_file_location("check_requirements", SCRIPT)
cr = importlib.util.module_from_spec(spec)
spec.loader.exec_module(cr)

TODAY = datetime.date(2026, 11, 12)
FILES = {
    "AGENTS.md": "## General\n### Workflow\n",
    "web/AGENTS.md": "### i18n (if present)\n",
    "server/AGENTS.md": "### Admin panel (if present)\n- **Endpoints.** [rule: authz]\n```text\n### Not a rule [rule: not-one]\n```\n",
    "REVIEW.md": "",
    "scripts/check.sh": "",
    "scripts/check-authz.sh": "",
}

GOOD = """\
# REQUIREMENTS.md

## Rule files

| File | Checked | Note |
| --- | --- | --- |
| AGENTS.md | yes | |
| web/AGENTS.md | yes | |
| server/AGENTS.md | yes | |

## Commands

| Tier | Command | Runs (where, when) | Wall-time limit | Baseline (date, host, load) |
| --- | --- | --- | --- | --- |
| Pre-commit | `scripts/check.sh --fast` | locally, every commit | 5 s | 3.8 s (2026-11-12, ci-1, load 0.4) |
| Merge gate | `scripts/check.sh \\| tee log` | CI, every merge | 15 min | 11 min (2026-11-12, ci-1, load 1.2) |

## Numbers

| Item | Limit | Baseline (date, host, load) | Enforced by |
| --- | --- | --- | --- |
| Slowest test package | 30 s | 22 s (2026-11-12, ci-1, load 0.4) | merge gate |
| Entry chunk, gzip | | | |

## Enforcement

| Rule group | Gate command | Review criterion | Does not apply because |
| --- | --- | --- | --- |
| AGENTS.md: Workflow | | `REVIEW.md`, pass bar | |
| web/AGENTS.md: i18n (if present) | | | |
| server/AGENTS.md: Admin panel (if present) | | | no admin panel |
| server/AGENTS.md#authz | `scripts/check-authz.sh` | `REVIEW.md`, Authorization | |

## Not yet enforced

| Row | Current state | Plan | Due |
| --- | --- | --- | --- |
| Numbers: Entry chunk, gzip | not measured | measure after the build lands | 2027-01-31 |
| Enforcement: web/AGENTS.md: i18n (if present) | strings checked by hand | add scripts/check-i18n.sh | 2027-01-31 |

## Example (illustrative values; measure your own)

| Tier | Command | Runs (where, when) | Wall-time limit | Baseline (date, host, load) |
| --- | --- | --- | --- | --- |
| Pre-commit | | | | |
"""


def swap(old, new, text=GOOD):
    assert text.count(old) == 1, old
    return text.replace(old, new)


class FakeRepo:
    """Stands in for cr.Repo: the same three methods over a dict of path to content."""

    def __init__(self, files):
        self.files = files

    def exists(self, rel):
        rel = rel.rstrip("/")
        return rel in self.files or any(name.startswith(rel + "/") for name in self.files)

    def read(self, rel):
        return self.files[rel]

    def agents_files(self):
        return [name for name in self.files if name.rsplit("/", 1)[-1] == "AGENTS.md"]


def without(*names):
    return {name: body for name, body in FILES.items() if name not in names}


def check(text, today=TODAY, files=FILES):
    return cr.check(text, today, FakeRepo(files))


class Violations(unittest.TestCase):
    def expect(self, text, *wanted, **kw):
        problems = check(text, **kw)
        for part in wanted:
            self.assertTrue(any(part in p for p in problems), f"{part!r} not in {problems}")
        return problems

    def test_clean_file_has_no_problems(self):
        self.assertEqual(check(GOOD), [])

    def test_example_section_and_fenced_blocks_are_ignored(self):
        fenced = GOOD + "\n```text\n| Tier | Command |\n| --- | --- |\n| x | |\n```\n"
        self.assertEqual(check(fenced), [])

    def test_escaped_pipe_stays_inside_its_cell(self):
        self.assertEqual(cr.cells("| a | `x \\| y` | |"), ["a", "`x | y`", ""])

    def test_empty_command(self):
        self.expect(swap("`scripts/check.sh --fast`", ""), "Commands: Pre-commit: empty Command")

    def test_placeholder_counts_as_empty(self):
        self.expect(swap("| 30 s |", "| TBD |"), "Numbers: Slowest test package: empty Limit")

    def test_not_applicable_excludes_gate_and_review(self):
        text = swap("| | | no admin panel |", "| | `REVIEW.md`, pass bar | no admin panel |")
        self.expect(text, "Enforcement: server/AGENTS.md: Admin panel (if present): fill a gate or review, or a reason")

    def test_review_link_is_not_a_gate_command(self):
        text = swap("| `scripts/check-authz.sh` | `REVIEW.md`, Authorization |", "| `REVIEW.md`, Authorization | |")
        self.expect(text, "Enforcement: server/AGENTS.md#authz: the gate command starts with a document")

    def test_review_criterion_must_name_its_file(self):
        text = swap("| | `REVIEW.md`, pass bar | |", "| | looks fine to me | |")
        self.expect(text, "Enforcement: AGENTS.md: Workflow: name the review file in backticks")

    def test_reason_that_says_nothing_counts_as_empty(self):
        text = swap("| | | no admin panel |", "| | | n/a |")
        self.expect(text, "Enforcement: server/AGENTS.md: Admin panel (if present): empty Gate command")

    def test_unfilled_group_without_deferral(self):
        text = swap("| | | no admin panel |", "| | | |")
        self.expect(text, "Enforcement: server/AGENTS.md: Admin panel (if present): empty Gate command, Review criterion")


    def test_every_heading_needs_a_row(self):
        text = swap("| AGENTS.md: Workflow | | `REVIEW.md`, pass bar | |\n", "")
        self.expect(text, "Enforcement: AGENTS.md: Workflow: no row")

    def test_row_for_a_missing_heading(self):
        files = {**FILES, "AGENTS.md": "### Renamed\n"}
        self.expect(GOOD, "Enforcement: AGENTS.md: Workflow: no such ### heading", files=files)

    def test_tagged_rule_needs_its_own_row(self):
        text = swap("| server/AGENTS.md#authz | `scripts/check-authz.sh` | `REVIEW.md`, Authorization | |\n", "")
        self.expect(text, "Enforcement: server/AGENTS.md#authz: no row")

    def test_row_for_a_removed_rule_tag(self):
        files = {**FILES, "server/AGENTS.md": "### Admin panel (if present)\n"}
        self.expect(GOOD, "Enforcement: server/AGENTS.md#authz: no such ### heading or rule id", files=files)

    def test_gate_script_must_exist(self):
        wanted = "Enforcement: server/AGENTS.md#authz: no such path: scripts/check-authz.sh"
        self.expect(GOOD, wanted, files=without("scripts/check-authz.sh"))

    def test_review_link_must_exist(self):
        self.expect(GOOD, "Enforcement: AGENTS.md: Workflow: no such path: REVIEW.md", files=without("REVIEW.md"))

    def test_command_script_must_exist(self):
        text = swap("`scripts/check.sh --fast`", "`scripts/chekc.sh --fast`")
        self.expect(text, "Commands: Pre-commit: no such path: scripts/chekc.sh")

    def test_gate_command_must_be_in_backticks(self):
        text = swap("`scripts/check-authz.sh`", "scripts/check-authz.sh")
        self.expect(text, "Enforcement: server/AGENTS.md#authz: write the gate command in backticks")

    def test_command_must_be_in_backticks(self):
        text = swap("`scripts/check.sh --fast`", "scripts/check.sh --fast")
        self.expect(text, "Commands: Pre-commit: write the command in backticks")

    def test_words_that_are_not_repository_paths_are_left_alone(self):
        command = "`git diff origin/main --stat && go test ./... -run=web/x $HOME/x.sh /usr/bin/lint.sh`"
        self.assertEqual(check(swap("`scripts/check.sh --fast`", command)), [])

    def test_unlisted_agents_file(self):
        files = {**FILES, "pkg/AGENTS.md": "### Extra\n"}
        self.expect(GOOD, "Rule files: pkg/AGENTS.md: not listed", files=files)

    def test_file_marked_no_asks_for_no_rows(self):
        files = {**FILES, "pkg/AGENTS.md": "### Extra\n"}
        text = swap("| server/AGENTS.md | yes | |\n", "| server/AGENTS.md | yes | |\n| pkg/AGENTS.md | no | vendored |\n")
        self.assertEqual(check(text, files=files), [])

    def test_file_marked_no_needs_a_reason(self):
        text = swap("| server/AGENTS.md | yes | |\n", "| server/AGENTS.md | yes | |\n| pkg/AGENTS.md | no | |\n")
        self.expect(text, "Rule files: pkg/AGENTS.md: say in Note why")

    def test_checked_must_be_yes_or_no(self):
        self.expect(swap("| web/AGENTS.md | yes | |", "| web/AGENTS.md | maybe | |"), "Checked must be yes or no")

    def test_listed_rule_file_must_exist(self):
        self.expect(GOOD, "Rule files: web/AGENTS.md: no such file", files=without("web/AGENTS.md"))

    def test_same_name_in_two_tables_needs_its_own_deferral(self):
        row = "| Pre-commit | `scripts/check.sh --fast` | locally, every commit | 5 s | 3.8 s (2026-11-12, ci-1, load 0.4) |"
        self.expect(swap(row, "| Entry chunk, gzip | | | | |"), "Commands: Entry chunk, gzip: empty Command")

    def test_deferral_must_name_section_and_row(self):
        self.expect(swap("| Numbers: Entry chunk, gzip |", "| Entry chunk, gzip |"), "Not yet enforced: Entry chunk, gzip: names no row")

    def test_deferral_of_a_row_that_does_not_exist(self):
        self.expect(swap("| Numbers: Entry chunk, gzip |", "| Numbers: Entry chunk |"), "names no row")

    def test_deferral_needs_a_state(self):
        self.expect(swap("| not measured |", "| |"), "Not yet enforced: Numbers: Entry chunk, gzip: empty Current state")

    def test_due_must_be_one_date(self):
        text = swap("| measure after the build lands | 2027-01-31 |", "| measure after the build lands | 2026-10-01, moved to 2099-01-01 |")
        self.expect(text, "Due must be one date")

    def test_due_must_be_a_real_date(self):
        text = swap("| measure after the build lands | 2027-01-31 |", "| measure after the build lands | 2027-13-40 |")
        self.expect(text, "Due must be one date")

    def test_due_expires(self):
        self.expect(GOOD, "has passed", today=datetime.date(2027, 2, 1))

    def test_due_is_valid_on_its_last_day(self):
        self.assertEqual(check(GOOD, today=datetime.date(2027, 1, 31)), [])

    def test_filled_row_must_leave_the_deferred_list(self):
        text = swap("| Entry chunk, gzip | | | |", "| Entry chunk, gzip | 90 kB | 80 kB (2026-11-12, ci-1) | |")
        self.expect(text, "Numbers: Entry chunk, gzip: is filled; remove its row")

    def test_unfilled_row_without_deferral(self):
        text = swap("| Numbers: Entry chunk, gzip | not measured | measure after the build lands | 2027-01-31 |\n", "")
        self.expect(text, "Numbers: Entry chunk, gzip: empty Limit, Baseline")

    def test_duplicate_row(self):
        self.expect(swap("| Merge gate |", "| Pre-commit |"), "Commands: Pre-commit: listed twice")

    def test_short_row_is_reported_not_a_crash(self):
        self.expect(swap("| Slowest test package | 30 s | 22 s (2026-11-12, ci-1, load 0.4) | merge gate |", "| Slowest test package |"), "Numbers: Slowest test package: 1 cells")

    def test_unescaped_pipe_is_reported(self):
        self.expect(swap("\\| tee log", "| tee log"), "Commands: Merge gate: 6 cells")

    def test_all_empty_row(self):
        self.expect(swap("| Entry chunk, gzip | | | |", "| | | | |"), "Numbers: a row has an empty first cell")

    def test_table_right_before_a_heading_is_still_read(self):
        text = swap("| Entry chunk, gzip | | | |\n\n## Enforcement", "| Entry chunk, gzip | | | |\n| Extra | | | |\n## Enforcement")
        self.expect(text, "Numbers: Extra: empty Limit")


class CannotRun(unittest.TestCase):
    def test_missing_table(self):
        with self.assertRaises(cr.CannotRun):
            check(GOOD.replace("## Enforcement", "## Enforcment"))

    def test_missing_column(self):
        with self.assertRaises(cr.CannotRun):
            check(swap("| Row | Current state | Plan | Due |", "| Row | Current state | Plan |"))

    def test_table_without_rows(self):
        text = swap("| Slowest test package | 30 s | 22 s (2026-11-12, ci-1, load 0.4) | merge gate |\n| Entry chunk, gzip | | | |\n", "")
        with self.assertRaises(cr.CannotRun):
            check(text)

    def test_table_without_separator_row(self):
        text = swap("| Rule group | Gate command | Review criterion | Does not apply because |\n| --- | --- | --- | --- |\n", "| Rule group | Gate command | Review criterion | Does not apply because |\n")
        with self.assertRaises(cr.CannotRun):
            check(text)

    def test_no_rule_headings(self):
        with self.assertRaises(cr.CannotRun):
            check(GOOD, files={**FILES, "AGENTS.md": "", "web/AGENTS.md": "", "server/AGENTS.md": ""})


class RepoFiles(unittest.TestCase):
    def test_rule_groups_reads_headings_and_tags_and_skips_fenced(self):
        self.assertEqual(
            cr.rule_groups("server/AGENTS.md", FILES["server/AGENTS.md"]),
            ["server/AGENTS.md: Admin panel (if present)", "server/AGENTS.md#authz"],
        )

    def test_agents_files_skips_vendored_and_hidden(self):
        files = ["AGENTS.md", "web/AGENTS.md", ".github/AGENTS.md", "node_modules/x/AGENTS.md", ".cache/AGENTS.md"]
        with tempfile.TemporaryDirectory() as root:
            for rel in files:
                os.makedirs(os.path.dirname(os.path.join(root, rel)) or root, exist_ok=True)
                with open(os.path.join(root, rel), "w", encoding="utf-8") as handle:
                    handle.write("### Heading\n")
            self.assertEqual(
                sorted(cr.Repo(root).agents_files()),
                [".github/AGENTS.md", "AGENTS.md", "web/AGENTS.md"],
            )


class ExitCodes(unittest.TestCase):
    def run_on(self, text, *args, agents=True):
        with tempfile.TemporaryDirectory() as root:
            path = os.path.join(root, "REQUIREMENTS.md")
            if text is not None:
                with open(path, "w", encoding="utf-8") as handle:
                    handle.write(text)
            if agents:
                for rel, body in FILES.items():
                    os.makedirs(os.path.dirname(os.path.join(root, rel)) or root, exist_ok=True)
                    with open(os.path.join(root, rel), "w", encoding="utf-8") as handle:
                        handle.write(body)
            done = subprocess.run(
                [sys.executable, SCRIPT, "--today", "2026-11-12", *args, path],
                capture_output=True, text=True,
            )
            return done.returncode, done.stdout, done.stderr

    def test_clean_is_0(self):
        self.assertEqual(self.run_on(GOOD)[0], 0)

    def test_violation_is_1_and_printed(self):
        code, out, _ = self.run_on(swap("`scripts/check.sh --fast`", ""))
        self.assertEqual(code, 1)
        self.assertIn("Commands: Pre-commit", out)

    def test_missing_file_is_2(self):
        self.assertEqual(self.run_on(None)[0], 2)

    def test_missing_table_is_2(self):
        self.assertEqual(self.run_on("# nothing here\n")[0], 2)

    def test_no_agents_file_is_2(self):
        self.assertEqual(self.run_on(GOOD, agents=False)[0], 2)

    def test_bad_today_is_2(self):
        self.assertEqual(self.run_on(GOOD, "--today", "tomorrow")[0], 2)


if __name__ == "__main__":
    unittest.main()
