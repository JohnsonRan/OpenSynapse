#!/usr/bin/env python3
"""Fail when REQUIREMENTS.md has an unfilled, malformed or expired entry.

Checked:
- "Commands": Command, Runs, Wall-time limit and Baseline are filled.
- "Numbers": Limit and Baseline are filled.
- "Rule files": every AGENTS.md under the root is listed, with Checked yes or no; a file
  marked no has a reason in Note, and a file marked yes exists. The search skips .git,
  node_modules, vendor, third_party and hidden directories other than .github.
- "Enforcement": one row per rule group of every checked rule file (a "### " heading, named
  "path: heading", or a rule tagged [rule: <id>], named "path#id"), and no row for a group
  that does not exist. A row fills Gate command, Review criterion or both, or else only
  Does not apply because. A gate command does not start with a document (.md); a review
  criterion names its file in backticks.
- Commands and links: a "Commands" command and a gate command are written in backticks. In
  a backticked command, a word that ends in a script suffix (.sh, .py, .md, ...) or starts
  with an existing directory must exist under the root.
- "Not yet enforced": each row names an existing row as "Section: row", states its current
  state and plan, and has one Due date (YYYY-MM-DD) that has not passed.
- Every checked table: a separator row under the header, as many cells per row as the
  header, and no two rows with the same first cell.

This check reads files; it runs nothing. Whether a gate runs in CI and turns red on a planted
violation is shown by CI and checked in review.

A row listed under "Not yet enforced" may stay unfilled; once it is filled, its deferral must go.
A cell that holds only a placeholder (TBD, TODO, n/a, none, ?, -) counts as empty.
Write a pipe inside a cell as \\|.
The "Example" section and fenced code blocks are never checked.

Exit codes: 0 clean, 1 violations found, 2 could not run (file, table or column missing, or no rule group).
Usage: check-requirements.py [--today YYYY-MM-DD] [--root DIR] [path/to/REQUIREMENTS.md]
"""
import argparse
import datetime
import os
import re
import shlex
import sys

FILLED = {
    "Commands": ["Command", "Runs (where, when)", "Wall-time limit", "Baseline (date, host, load)"],
    "Numbers": ["Limit", "Baseline (date, host, load)"],
}
ENFORCED = ["Gate command", "Review criterion", "Does not apply because"]
COLUMNS = {
    "Rule files": ["Checked", "Note"],
    **FILLED,
    "Enforcement": ENFORCED,
    "Not yet enforced": ["Current state", "Plan", "Due"],
}
PLACEHOLDERS = {"", "tbd", "todo", "n/a", "na", "none", "?", "-", "--", "...", "…", "xxx"}
SKIP_DIRS = {".git", "node_modules", "vendor", "third_party"}
SCRIPT_SUFFIXES = (".sh", ".py", ".js", ".mjs", ".ts", ".rb", ".pl", ".md")
RULE_ID = re.compile(r"\[rule: ([a-z0-9][a-z0-9-]*)\]")


class CannotRun(Exception):
    """The input does not have the shape this check reads. Maps to exit code 2."""


def blank(cell):
    return cell.strip("`* ").lower() in PLACEHOLDERS


def cells(line):
    line = line.strip()
    if line.startswith("|"):
        line = line[1:]
    if line.endswith("|") and not line.endswith("\\|"):
        line = line[:-1]
    return [c.strip().replace("\\|", "|") for c in re.split(r"(?<!\\)\|", line)]


def is_separator(row):
    return all(re.fullmatch(r":?-+:?", c) for c in row)


def tables(text):
    """Map each level-2 heading to its first table: (header, has separator row, rows)."""
    out, section, table, fenced = {}, None, None, False

    def close():
        nonlocal table
        if table is not None and section not in out:
            out[section] = table
        table = None

    for line in text.splitlines():
        if line.lstrip().startswith(("```", "~~~")):
            fenced = not fenced
            close()
        elif fenced:
            continue
        elif line.startswith("## "):
            close()
            section = line[3:].strip()
        elif line.lstrip().startswith("|") and section and section not in out:
            row = cells(line)
            if table is None:
                table = (row, False, [])
            elif not table[1] and not table[2] and is_separator(row):
                table = (table[0], True, [])
            else:
                table[2].append(row)
        else:
            close()
    close()
    return out


class Repo:
    """The repository files the check reads, by path relative to the root."""

    def __init__(self, root):
        self.root = root

    def exists(self, rel):
        return os.path.exists(os.path.join(self.root, rel))

    def read(self, rel):
        with open(os.path.join(self.root, rel), encoding="utf-8") as handle:
            return handle.read()

    def agents_files(self):
        """Every AGENTS.md under the root, so that none stays out of "Rule files" unnoticed."""
        found = []
        for top, dirs, files in os.walk(self.root):
            dirs[:] = sorted(d for d in dirs if d not in SKIP_DIRS and (d == ".github" or not d.startswith(".")))
            if "AGENTS.md" in files:
                found.append(os.path.relpath(os.path.join(top, "AGENTS.md"), self.root).replace(os.sep, "/"))
        return found


def rule_groups(rel, text):
    """Return the Enforcement row names a rule file asks for: "path: heading" and "path#id"."""
    found, fenced = [], False
    for line in text.splitlines():
        if line.lstrip().startswith(("```", "~~~")):
            fenced = not fenced
        elif not fenced:
            if line.startswith("### "):
                found.append(f"{rel}: {line[4:].strip()}")
            found.extend(f"{rel}#{rule}" for rule in RULE_ID.findall(line))
    return found


def missing_paths(cell, repo):
    """Return the repository paths named in the backticked commands of a cell that do not exist."""
    absent = []
    for command in re.findall(r"`([^`]+)`", cell):
        try:
            words = shlex.split(command)
        except ValueError:
            words = command.split()
        for word in words:
            word = word[2:] if word.startswith("./") else word
            if not word or word[0] in "-/~" or re.search(r"[$*?\[\]{}<>=:;|&()]|\.\.\.", word):
                continue
            scripted = word.endswith(SCRIPT_SUFFIXES)
            in_repo = "/" in word and repo.exists(word.split("/", 1)[0])
            if (scripted or in_repo) and not repo.exists(word):
                absent.append(word)
    return absent


def check(text, today, repo):
    """Return the list of violations. Raise CannotRun when the input has the wrong shape."""
    found = tables(text)
    missing = [name for name in COLUMNS if name not in found]
    if missing:
        raise CannotRun(f"no table under: {', '.join(missing)}")

    problems = []
    good = {}
    for section, wanted in COLUMNS.items():
        header, separated, rows = found[section]
        absent = [w for w in wanted if w not in header]
        if absent:
            raise CannotRun(f"the {section} table lacks columns: {', '.join(absent)}")
        if not separated:
            raise CannotRun(f"the {section} table has no separator row under its header")
        if not rows and section != "Not yet enforced":
            raise CannotRun(f"the {section} table has no rows")
        seen, kept = set(), {}
        for row in rows:
            name = row[0]
            if len(row) != len(header):
                problems.append(
                    f"{section}: {name or '(no name)'}: {len(row)} cells, the header has "
                    f"{len(header)} (write a pipe inside a cell as \\|)"
                )
            elif not name:
                problems.append(f"{section}: a row has an empty first cell")
            elif name in seen:
                problems.append(f"{section}: {name}: listed twice")
            else:
                seen.add(name)
                kept[name] = dict(zip(header, row))
        good[section] = kept

    headings = []
    for rel, row in good["Rule files"].items():
        if row["Checked"] not in ("yes", "no"):
            problems.append(f"Rule files: {rel}: Checked must be yes or no, not {row['Checked']!r}")
        elif row["Checked"] == "no":
            if blank(row["Note"]):
                problems.append(f"Rule files: {rel}: say in Note why it is not checked")
        elif not repo.exists(rel):
            problems.append(f"Rule files: {rel}: no such file")
        else:
            headings.extend(rule_groups(rel, repo.read(rel)))
    for rel in repo.agents_files():
        if rel not in good["Rule files"]:
            problems.append(f"Rule files: {rel}: not listed (list it with Checked yes, or no and a reason)")
    if not headings:
        raise CannotRun('no ### heading in any file checked under "Rule files"')

    deferred = set()
    for name, row in good["Not yet enforced"].items():
        section, _, target = name.partition(": ")
        if section not in ("Commands", "Numbers", "Enforcement") or target not in good[section]:
            problems.append(
                f"Not yet enforced: {name}: names no row (write Section: row, "
                f"for example Numbers: Slowest test package)"
            )
            continue
        deferred.add((section, target))
        empty = [c for c in COLUMNS["Not yet enforced"] if blank(row[c])]
        if empty:
            problems.append(f"Not yet enforced: {name}: empty {', '.join(empty)}")
            continue
        try:
            due = datetime.date.fromisoformat(row["Due"]) if re.fullmatch(r"\d{4}-\d{2}-\d{2}", row["Due"]) else None
        except ValueError:
            due = None
        if due is None:
            problems.append(f"Not yet enforced: {name}: Due must be one date, YYYY-MM-DD, not {row['Due']!r}")
        elif due < today:
            problems.append(f"Not yet enforced: {name}: Due {due} has passed (do it, or move it with the owner's approval)")

    def unfilled(section, row):
        if section == "Enforcement":
            return [] if any(not blank(row[c]) for c in ENFORCED) else ENFORCED
        return [w for w in FILLED[section] if blank(row[w])]

    for section in ("Commands", "Numbers", "Enforcement"):
        for name, row in good[section].items():
            empty = unfilled(section, row)
            if (section, name) in deferred and not empty:
                problems.append(f"{section}: {name}: is filled; remove its row from Not yet enforced")
            elif empty and (section, name) not in deferred:
                problems.append(
                    f"{section}: {name}: empty {', '.join(empty)} (fill it or list it under Not yet enforced)"
                )

    rows = good["Enforcement"]
    for name, row in rows.items():
        if name not in headings:
            problems.append(
                f"Enforcement: {name}: no such ### heading or rule id in any checked rule file (renamed or removed?)"
            )
        gate, review = row["Gate command"], row["Review criterion"]
        if not blank(row["Does not apply because"]) and not (blank(gate) and blank(review)):
            problems.append(f"Enforcement: {name}: fill a gate or review, or a reason it does not apply, not both")
        if not blank(gate) and "`" not in gate:
            problems.append(f"Enforcement: {name}: write the gate command in backticks")
        elif re.search(r"`\s*[^`\s]+\.md[`\s]", gate):
            problems.append(f"Enforcement: {name}: the gate command starts with a document; a review link goes under Review criterion")
        if not blank(review) and not re.search(r"`[^`\s]+\.md`", review):
            problems.append(f"Enforcement: {name}: name the review file in backticks, for example `REVIEW.md`")
        for cell in (gate, review):
            problems.extend(f"Enforcement: {name}: no such path: {path}" for path in missing_paths(cell, repo))
    for heading in headings:
        if heading not in rows:
            problems.append(f"Enforcement: {heading}: no row (every rule group needs one)")
    for name, row in good["Commands"].items():
        command = row["Command"]
        if not blank(command) and "`" not in command:
            problems.append(f"Commands: {name}: write the command in backticks")
        problems.extend(f"Commands: {name}: no such path: {path}" for path in missing_paths(command, repo))
    return problems


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("path", nargs="?", default="REQUIREMENTS.md")
    parser.add_argument("--root", help="repository root, where rule files and command paths are looked up (default: the file's directory)")
    parser.add_argument(
        "--today",
        type=datetime.date.fromisoformat,
        default=datetime.date.today(),
        help="date to compare Due dates with (YYYY-MM-DD); for tests",
    )
    args = parser.parse_args(argv)
    root = args.root or os.path.dirname(os.path.abspath(args.path))
    try:
        with open(args.path, encoding="utf-8") as handle:
            text = handle.read()
        problems = check(text, args.today, Repo(root))
    except (OSError, UnicodeDecodeError, CannotRun) as err:
        print(f"check-requirements: {args.path}: {err}", file=sys.stderr)
        return 2
    for problem in problems:
        print(problem)
    return 1 if problems else 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as err:  # a crash must not exit 1, which means "violations found"
        print(f"check-requirements: internal error: {err!r}", file=sys.stderr)
        sys.exit(2)
