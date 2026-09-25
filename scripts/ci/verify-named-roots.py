#!/usr/bin/env python3
"""Reconcile named-root CI job scripts against the test roots in the tree.

The dedicated jobs select coverage by literal test name: e2e.sh runs every
``TestRequired*`` protocol root by name, sigkill.sh runs every abrupt-death
durability root, multi-process.sh runs the cross-process fencing roots, and
privilege-boundary.sh names every linuxintegration root that must PASS. The
``test`` job's inventory cannot see these roots (the e2e/linuxintegration
files are build-tag gated and the internal/ roots are absorbed into the big
suite), so without this check a new root that belongs in a named job is never
selected — CI stays green while the gate silently covers less (issue #1014).

Two directions are enforced:

* forward — every ``func Test<name>(`` root matching a job's declared family
  pattern must appear in that job's script, unless the root is explicitly
  exempted in scripts/ci/named-root-exemptions.txt;
* reverse — every ``Test<name>`` token a named-root script carries (in -run
  expressions, ci_assert_test_passed calls, or comments) must resolve to a
  real test root in this repository, so a renamed or deleted test cannot
  leave a stale name behind.

Exit 2 is a script/configuration error; exit 1 reports the reconciliation
failures.
"""
from __future__ import annotations

import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent.parent

FUNC_RE = re.compile(r"^func\s+(Test[A-Za-z0-9_]+)\s*\(", re.MULTILINE)
NAME_TOKEN_RE = re.compile(r"Test[A-Za-z0-9_]+")

# Forward reconciliation: (job script, directories scanned for roots, name
# pattern). Membership is by name family — a new root matching the pattern
# must be named in the script or explicitly exempted.
FAMILIES: list[tuple[str, list[str], re.Pattern[str], str]] = [
    (
        "scripts/ci/e2e.sh",
        ["test/e2e"],
        re.compile(r"^TestRequired"),
        "every TestRequired* protocol root under test/e2e must run (and PASS-assert) in the e2e job",
    ),
    (
        "scripts/ci/sigkill.sh",
        ["internal", "test"],
        re.compile(r"SIGKILL|ProcessKill"),
        "every Test*SIGKILL*/*ProcessKill* durability root must run (and PASS-assert) in the sigkill job",
    ),
    (
        "scripts/ci/multi-process.sh",
        ["internal", "test"],
        re.compile(r"AcrossOSProcesses|AcrossProcesses"),
        "every cross-OS-process fencing root must run (and PASS-assert) in the multi-process job",
    ),
    (
        "scripts/ci/privilege-boundary.sh",
        ["test/linuxintegration"],
        re.compile(r"^Test"),
        "every linuxintegration root must be named in privilege-boundary.sh so a soft-skip cannot green it",
    ),
]

# Job scripts whose literal root names are reconciled for staleness in both
# directions. filesystem-faults has no name-family; its curated list is still
# verified root-by-root below.
NAMED_ROOT_SCRIPTS = [
    "scripts/ci/e2e.sh",
    "scripts/ci/sigkill.sh",
    "scripts/ci/filesystem-faults.sh",
    "scripts/ci/multi-process.sh",
    "scripts/ci/privilege-boundary.sh",
]

# Test<name> tokens a script legitimately carries that are NOT roots in this
# repository — e2e.sh resolves TestLocalThroughputSoak inside the pinned
# upstream olcrtc module directory, not this tree.
EXTERNAL_ROOTS: dict[str, set[str]] = {
    "scripts/ci/e2e.sh": {"TestLocalThroughputSoak"},
}

# scripts/ci/named-root-exemptions.txt — roots matching a family pattern that
# are deliberately NOT selected by the named job (e.g. a helper-only root).
# Format: <package-or-dir><TAB><root>, one per line, # comments allowed.
EXEMPTIONS_PATH = ROOT / "scripts" / "ci" / "named-root-exemptions.txt"


def iter_test_roots(directories: list[str]) -> dict[str, set[str]]:
    """Return {root name: {relative file paths}} for dirs, all build tags."""
    roots: dict[str, set[str]] = {}
    for directory in directories:
        base = ROOT / directory
        for path in sorted(base.rglob("*_test.go")):
            try:
                text = path.read_text(encoding="utf-8", errors="replace")
            except OSError:
                continue
            for name in FUNC_RE.findall(text):
                roots.setdefault(name, set()).add(str(path.relative_to(ROOT)))
    return roots


def load_exemptions() -> set[str]:
    exemptions: set[str] = set()
    if not EXEMPTIONS_PATH.exists():
        return exemptions
    for line in EXEMPTIONS_PATH.read_text(encoding="utf-8").splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        parts = line.split("\t")
        if len(parts) != 2:
            raise SystemExit(f"{EXEMPTIONS_PATH}: malformed line {line!r} — want <package-or-dir><TAB><root>")
        exemptions.add(parts[1].strip())
    return exemptions


def script_text(script: str) -> str:
    path = ROOT / script
    if not path.exists():
        raise SystemExit(f"{script}: job script not found")
    return path.read_text(encoding="utf-8")


def main() -> int:
    exemptions = load_exemptions()
    all_roots = iter_test_roots(["internal", "test", "cmd", "pkg"])
    failures: list[str] = []

    # Forward: family membership.
    for script, directories, pattern, description in FAMILIES:
        text = script_text(script)
        # Match on whole tokens — a comment mentioning the root is not
        # selection evidence unless it is the -run/assert name itself; the
        # token check is intentionally simple because every naming site in
        # the job scripts (run flags, ci_assert_test_passed args, required
        # loops) carries the bare name.
        named = set(NAME_TOKEN_RE.findall(text))
        roots = iter_test_roots(directories)
        for name, files in sorted(roots.items()):
            if not pattern.search(name):
                continue
            if name in exemptions:
                continue
            if name not in named:
                failures.append(
                    f"{sorted(files)[0]}: {name} is not selected by {script} — "
                    f"{description}. Add it to the job or exempt it in "
                    f"scripts/ci/named-root-exemptions.txt"
                )

    # Reverse: every Test<name> token in a named-root script must be a real
    # root here (or a declared external root).
    for script in NAMED_ROOT_SCRIPTS:
        external = EXTERNAL_ROOTS.get(script, set())
        for token in sorted(set(NAME_TOKEN_RE.findall(script_text(script)))):
            if token in external:
                continue
            if token not in all_roots:
                failures.append(
                    f"{script}: names {token}, which is not a test root in this "
                    f"repository — stale rename or missing test; reconcile the "
                    f"script (or declare an external root in verify-named-roots.py)"
                )

    # The exemptions file must not rot: every entry must still exist as a
    # root, or it hides nothing and misleads the next audit.
    for name in sorted(exemptions):
        if name not in all_roots:
            failures.append(
                f"named-root-exemptions.txt: {name} is not a test root — remove the stale exemption"
            )

    if failures:
        for failure in failures:
            print(f"named-root reconciliation: {failure}", file=sys.stderr)
        return 1
    print(
        "named-root reconciliation: all family roots selected, all script names resolve "
        f"({len(NAMED_ROOT_SCRIPTS)} job scripts)"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
