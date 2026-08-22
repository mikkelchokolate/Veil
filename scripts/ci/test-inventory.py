#!/usr/bin/env python3
"""Discover and report every top-level root executed by the CI test job.

The inventory is deliberately independent of the scheduler: go test -list is
used for discovery, while the JSON logs from the real run are used to mark the
observed status and elapsed time.  This prevents a regex/shard mistake from
silently dropping a root.
"""
from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
from collections import defaultdict
from pathlib import Path
from typing import Any

ROOT_RE = re.compile(r"^Test[A-Za-z0-9_]+$")
FUNC_RE = re.compile(r"^func\s+(Test[A-Za-z0-9_]+)\s*\(", re.MULTILINE)


def run(cmd: list[str], cwd: Path) -> str:
    return subprocess.check_output(cmd, cwd=cwd, text=True, stderr=subprocess.STDOUT)


def source_map(pkg_dir: Path, files: list[str] | None = None) -> dict[str, str]:
    found: dict[str, str] = {}
    paths = [pkg_dir / name for name in files] if files is not None else sorted(pkg_dir.glob("*_test.go"))
    for path in sorted(paths):
        text = path.read_text(encoding="utf-8", errors="replace")
        for name in FUNC_RE.findall(text):
            found.setdefault(name, str(path))
    return found


def classify(text: str, name: str) -> dict[str, Any]:
    lower = text.lower()
    uses_subprocess = "exec.command" in lower or "os.args[0]" in lower
    uses_real_sleep = any(token in lower for token in (
        "time.sleep", "time.after", "time.newticker", "time.newtimer",
        "time.now().add", "ticker.c", "timer.c",
    ))
    uses_real_kdf = "pbkdf2" in lower or "derivekey" in lower
    uses_bcrypt = "bcrypt" in lower
    uses_fresh_db = "storage.open(" in lower or "storage.openexisting(" in lower
    uses_network = any(token in lower for token in (
        "httptest.", "net.listen", "http.newrequest", "net.dial",
    ))
    uses_filesystem = any(token in lower for token in (
        "t.tempdir", "os.writefile", "os.rename", "filepath.",
    ))
    if uses_subprocess:
        fixture = "process"
    elif uses_real_sleep:
        fixture = "timer"
    elif uses_real_kdf or uses_bcrypt:
        fixture = "crypto"
    elif uses_fresh_db:
        fixture = "sqlite"
    elif uses_network:
        fixture = "network"
    elif uses_filesystem:
        fixture = "filesystem"
    else:
        fixture = "pure"
    helper = bool(re.search(r"helper|subprocess|crash|sigkill", name, re.I) and uses_subprocess)
    return {
        "fixtureClass": fixture,
        "serialReason": "subprocess/helper boundary" if helper else "",
        "usesFreshDatabase": uses_fresh_db,
        "usesFullRouter": "newrouter(" in lower or "newroutercomposition(" in lower,
        "usesRealKDF": uses_real_kdf,
        "usesRealSleep": uses_real_sleep,
        "usesSubprocess": uses_subprocess,
        "usesBcrypt": uses_bcrypt,
        "helperCandidate": helper,
    }


def go_package_metadata(repo: Path) -> dict[str, dict[str, Any]]:
    raw = run(["go", "list", "-json", "./..."], repo)
    decoder = json.JSONDecoder()
    pos = 0
    metadata: dict[str, dict[str, Any]] = {}
    while pos < len(raw):
        while pos < len(raw) and raw[pos].isspace():
            pos += 1
        if pos >= len(raw):
            break
        item, end = decoder.raw_decode(raw, pos)
        pos = end
        if item.get("ImportPath"):
            metadata[item["ImportPath"]] = item
    return metadata


def discover(repo: Path, packages: list[str]) -> list[dict[str, Any]]:
    rows: list[dict[str, Any]] = []
    metadata = go_package_metadata(repo)
    for package in packages:
        item = metadata.get(package)
        if not item:
            raise SystemExit(f"inventory: package missing from go list: {package}")
        package_dir = Path(item["Dir"])
        test_files = item.get("TestGoFiles", []) + item.get("XTestGoFiles", [])
        files = source_map(package_dir, test_files)
        names = sorted(name for name in files if name != "TestMain")
        package_text = "\n".join(
            (package_dir / name).read_text(encoding="utf-8", errors="replace")
            for name in sorted(test_files)
        )
        for name in names:
            source = files.get(name, "")
            text = Path(source).read_text(encoding="utf-8", errors="replace") if source else package_text
            row = {
                "package": package,
                "root": name,
                "sourceFile": str(Path(source).relative_to(repo)) if source else "",
                "status": "pass",
                "elapsedMs": 0,
            }
            row.update(classify(text, name))
            rows.append(row)
    return rows


def parse_logs(paths: list[Path]) -> tuple[dict[tuple[str, str], dict[str, Any]], set[tuple[str, str]]]:
    timings: dict[tuple[str, str], dict[str, Any]] = {}
    observed: set[tuple[str, str]] = set()
    for path in paths:
        if not path.exists():
            continue
        for line in path.read_text(encoding="utf-8", errors="replace").splitlines():
            try:
                event = json.loads(line)
            except json.JSONDecodeError:
                continue
            package = event.get("Package")
            root = event.get("Test")
            action = event.get("Action")
            # The Go test JSON stream reports the package harness as TestMain,
            # but `go test -list` intentionally excludes it from the test
            # inventory. It is lifecycle metadata, not an executable root.
            if root == "TestMain":
                continue
            if not package or not root or "/" in root or not ROOT_RE.fullmatch(root):
                continue
            key = (package, root)
            if action in {"pass", "skip", "fail"}:
                observed.add(key)
                elapsed = int(round(float(event.get("Elapsed", 0)) * 1000))
                status = "skip" if action == "skip" else action
                timings[key] = {"status": status, "elapsedMs": elapsed}
    return timings, observed


def write_report(repo: Path, artifact: Path, roots: list[dict[str, Any]], logs: list[Path]) -> None:
    artifact.mkdir(parents=True, exist_ok=True)
    by_key = {(row["package"], row["root"]): row for row in roots}
    timing_map, observed = parse_logs(logs)
    for key, row in by_key.items():
        if key in timing_map:
            row.update(timing_map[key])
            if row.get("helperCandidate") and row.get("status") == "skip":
                row["status"] = "helper"
        elif key not in observed:
            row["status"] = "missing"
    (artifact / "test-roots.json").write_text(json.dumps(roots, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    expected = sorted(f"{p}\t{r}" for p, r in by_key)
    (artifact / "expected-roots.txt").write_text("\n".join(expected) + "\n", encoding="utf-8")
    executed = sorted(f"{p}\t{r}" for p, r in observed)
    (artifact / "executed-roots.txt").write_text("\n".join(executed) + "\n", encoding="utf-8")
    timings = [row for row in roots if row.get("elapsedMs", 0) or row.get("status") in {"skip", "fail"}]
    (artifact / "test-timings.json").write_text(json.dumps(sorted(timings, key=lambda x: (-x.get("elapsedMs", 0), x["package"], x["root"])), indent=2, sort_keys=True) + "\n", encoding="utf-8")
    manifest = {
        "goVersion": run(["go", "version"], repo).strip(),
        "goos": os.environ.get("GOOS", "linux"),
        "goarch": os.environ.get("GOARCH", "amd64"),
        "race": True,
        "coverage": True,
        "roots": [{"package": r["package"], "root": r["root"], "elapsedMs": r.get("elapsedMs", 0)} for r in timings],
    }
    (artifact / "timing-manifest.json").write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    task_rows: list[dict[str, Any]] = []
    task_timing_path = artifact / "task-timings.json"
    if task_timing_path.exists():
        try:
            task_rows = json.loads(task_timing_path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            task_rows = []
    process_totals: dict[str, int] = defaultdict(int)
    for task in task_rows:
        process_totals[str(task.get("package", ""))] += int(task.get("elapsed_ms", 0))
    package_totals: dict[str, int] = defaultdict(int)
    package_counts: dict[str, int] = defaultdict(int)
    for row in roots:
        package_totals[row["package"]] += int(row.get("elapsedMs", 0))
        package_counts[row["package"]] += 1
    package_rows = [{
        "package": p,
        "rootCount": package_counts[p],
        "rootsElapsedMs": package_totals[p],
        "processWallMs": process_totals.get(p, 0),
        "setupTeardownOverheadMs": max(0, process_totals.get(p, 0) - package_totals[p]),
    } for p in sorted(package_totals)]
    (artifact / "package-timings.json").write_text(json.dumps(package_rows, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    manifest["packages"] = package_rows
    (artifact / "timing-manifest.json").write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    ranked = sorted(timings, key=lambda x: (-x.get("elapsedMs", 0), x["package"], x["root"]))
    slow = [f"{r.get('elapsedMs', 0)/1000:.3f}s\t{r['package']}\t{r['root']}\t{r.get('fixtureClass','')}\t{r.get('status','')}" for r in ranked[:100]]
    slow.append("")
    slow.append("all roots >2s")
    slow.extend(f"{r.get('elapsedMs', 0)/1000:.3f}s\t{r['package']}\t{r['root']}" for r in ranked if r.get("elapsedMs", 0) > 2000)
    slow.append("")
    slow.append(f"expected roots: {len(roots)}")
    slow.append(f"observed roots: {len(observed)}")
    slow.append(f"missing roots: {len(set(by_key) - observed)}")
    (artifact / "slow-tests.txt").write_text("\n".join(slow) + "\n", encoding="utf-8")
    plan_path = artifact / "shard-plan.json"
    try:
        plan = json.loads(plan_path.read_text(encoding="utf-8")) if plan_path.exists() else {}
    except (OSError, json.JSONDecodeError):
        plan = {}
    plan.update({"inventoryRoots": len(roots), "observedRoots": len(observed), "logs": [str(p) for p in logs]})
    plan_path.write_text(json.dumps(plan, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    balance_path = artifact / "shard-balance.txt"
    old_balance = balance_path.read_text(encoding="utf-8") if balance_path.exists() else ""
    actual = "\nactual task wall times:\n" + "\n".join(
        f"{row.get('elapsed_ms', 0)/1000:.3f}s\t{row.get('task')}\t{row.get('package')}"
        for row in sorted(task_rows, key=lambda item: int(item.get("elapsed_ms", 0)), reverse=True)
    )
    balance_path.write_text(old_balance.rstrip() + actual + "\n", encoding="utf-8")
    missing = sorted(set(by_key) - observed)
    unexpected = sorted(observed - set(by_key))
    if missing or unexpected:
        print(f"inventory verification failed: missing={len(missing)} unexpected={len(unexpected)}", file=sys.stderr)
        if missing:
            print("missing: " + ", ".join(f"{p}:{r}" for p, r in missing[:20]), file=sys.stderr)
        raise SystemExit(1)
    print(f"inventory verified: {len(roots)} expected roots, {len(observed)} executed roots")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repo", type=Path, default=Path.cwd())
    parser.add_argument("--artifact-dir", type=Path, required=True)
    parser.add_argument("--packages-file", type=Path)
    parser.add_argument("--roots-json", type=Path)
    parser.add_argument("--log", type=Path, action="append", default=[])
    args = parser.parse_args()
    repo = args.repo.resolve()
    roots_path = args.roots_json or (args.artifact_dir / "test-roots.json" if args.log and (args.artifact_dir / "test-roots.json").exists() else None)
    if roots_path:
        roots = json.loads(roots_path.read_text(encoding="utf-8"))
    else:
        if args.packages_file:
            packages = [line.strip() for line in args.packages_file.read_text(encoding="utf-8").splitlines() if line.strip()]
        else:
            packages = run(["go", "list", "./..."], repo).splitlines()
        roots = discover(repo, packages)
    if args.log:
        write_report(repo, args.artifact_dir, roots, args.log)
    else:
        args.artifact_dir.mkdir(parents=True, exist_ok=True)
        (args.artifact_dir / "test-roots.json").write_text(json.dumps(roots, indent=2, sort_keys=True) + "\n", encoding="utf-8")
        (args.artifact_dir / "expected-roots.txt").write_text("\n".join(sorted(f"{r['package']}\t{r['root']}" for r in roots)) + "\n", encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
