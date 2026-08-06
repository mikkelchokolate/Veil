#!/usr/bin/env python3
"""Run the required product tests with one bounded, duration-aware worker pool.

Non-API packages and API root shards are tasks in the same pool.  A task is a
separate `go test` process, so package globals and SQLite files never cross a
worker boundary.  No retry is performed; any failed task fails the job.
"""
from __future__ import annotations

import argparse
import json
import math
import os
import re
import signal
import shutil
import subprocess
import sys
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass, asdict
from pathlib import Path
from typing import Any

ROOT_RE = re.compile(r"^Test[A-Za-z0-9_]+$")
PACKAGE_FALLBACK_WEIGHTS = {
    "github.com/mikkelchokolate/Veil/internal/client": 372_000,
    "github.com/mikkelchokolate/Veil/internal/backup": 250_000,
    "github.com/mikkelchokolate/Veil/internal/cli": 237_000,
    "github.com/mikkelchokolate/Veil/internal/privileged": 156_000,
    "github.com/mikkelchokolate/Veil/internal/apply": 154_000,
    "github.com/mikkelchokolate/Veil/internal/runtimeinstall": 97_000,
    "github.com/mikkelchokolate/Veil/internal/statecommit": 90_000,
    "github.com/mikkelchokolate/Veil/internal/storage": 30_000,
    "github.com/mikkelchokolate/Veil/internal/service": 21_000,
    "github.com/mikkelchokolate/Veil/internal/generatedconfig": 21_000,
}


@dataclass
class Task:
    name: str
    package: str
    roots: list[str]
    command: list[str]
    profile: str
    weight_ms: int
    serial: bool = False


@dataclass
class Result:
    task: str
    package: str
    roots: list[str]
    profile: str
    rc: int
    elapsed_ms: int
    log: str
    serial: bool


def load_roots(repo: Path, package: str) -> list[str]:
    raw = subprocess.check_output(
        ["go", "test", package, "-short", "-run", "^$", "-list", "^Test"],
        cwd=repo,
        text=True,
        stderr=subprocess.STDOUT,
    )
    return sorted({line.strip() for line in raw.splitlines() if ROOT_RE.fullmatch(line.strip())})


def load_manifest(path: Path | None) -> dict[tuple[str, str], int]:
    if not path or not path.exists():
        return {}
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return {}
    rows = data if isinstance(data, list) else data.get("roots", []) + [
        {"package": row.get("package"), "root": "__package__", "elapsedMs": row.get("processWallMs", 0)}
        for row in data.get("packages", [])
    ]
    out: dict[tuple[str, str], int] = {}
    for row in rows:
        if isinstance(row, dict) and row.get("package") and row.get("root"):
            try:
                out[(row["package"], row["root"])] = max(1, int(row.get("elapsedMs", 0)))
            except (TypeError, ValueError):
                pass
    return out


def lpt_partition(roots: list[str], weights: dict[str, int], count: int) -> list[list[str]]:
    count = max(1, min(count, len(roots)))
    lanes: list[list[str]] = [[] for _ in range(count)]
    totals = [0] * count
    for root in sorted(roots, key=lambda item: (-weights.get(item, 1000), item)):
        lane = min(range(count), key=lambda index: (totals[index], index))
        lanes[lane].append(root)
        totals[lane] += weights.get(root, 1000)
    return lanes


def regex_for(roots: list[str]) -> str:
    if not roots:
        raise ValueError("cannot build an empty root regex")
    return "^(?:" + "|".join(re.escape(root) for root in roots) + ")$"


def terminate(proc: subprocess.Popen[Any]) -> None:
    try:
        os.killpg(proc.pid, signal.SIGTERM)
        time.sleep(0.2)
        os.killpg(proc.pid, signal.SIGKILL)
    except ProcessLookupError:
        pass


def run_task(task: Task, repo: Path, env: dict[str, str], timeout_s: int, log_dir: Path) -> Result:
    log_path = log_dir / f"{task.name}.json"
    start = time.monotonic()
    log_dir.mkdir(parents=True, exist_ok=True)
    with log_path.open("w", encoding="utf-8") as stream:
        stream.write(json.dumps({"Action": "TaskStart", "Task": task.name, "Package": task.package}) + "\n")
        stream.flush()
        proc = subprocess.Popen(
            task.command,
            cwd=repo,
            env=env,
            stdout=stream,
            stderr=subprocess.STDOUT,
            start_new_session=True,
        )
        try:
            rc = proc.wait(timeout=timeout_s)
        except subprocess.TimeoutExpired:
            terminate(proc)
            rc = 124
            stream.write(json.dumps({"Action": "TaskTimeout", "Task": task.name}) + "\n")
    elapsed_ms = int(round((time.monotonic() - start) * 1000))
    return Result(task.name, task.package, task.roots, task.profile, rc, elapsed_ms, str(log_path), task.serial)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repo", type=Path, default=Path.cwd())
    parser.add_argument("--api-package", required=True)
    parser.add_argument("--packages-file", type=Path, required=True)
    parser.add_argument("--artifact-dir", type=Path, required=True)
    parser.add_argument("--coverage-dir", type=Path, required=True)
    parser.add_argument("--workers", type=int, default=0)
    parser.add_argument("--api-shards", type=int, default=0)
    parser.add_argument("--task-timeout", type=int, default=1800)
    parser.add_argument("--timing-manifest", type=Path)
    parser.add_argument("--roots-json", type=Path)
    parser.add_argument("--serial-roots", default="TestRollbackPreservesRuntimeIdentityAndProtocolConfigBytes")
    args = parser.parse_args()
    repo = args.repo.resolve()
    artifact = args.artifact_dir.resolve()
    coverage_dir = args.coverage_dir.resolve()
    logs_dir = artifact / "test-tasks"
    artifact.mkdir(parents=True, exist_ok=True)
    coverage_dir.mkdir(parents=True, exist_ok=True)
    shutil.rmtree(logs_dir, ignore_errors=True)
    logs_dir.mkdir(parents=True, exist_ok=True)

    workers = args.workers or min(os.cpu_count() or 1, 4)
    workers = max(1, workers)
    api_shards = args.api_shards or workers
    api_shards = max(1, min(api_shards, workers))
    task_gomaxprocs = os.environ.get("CI_TEST_TASK_GOMAXPROCS", "1")
    manifest = load_manifest(args.timing_manifest)
    packages = [line.strip() for line in args.packages_file.read_text(encoding="utf-8").splitlines() if line.strip()]
    if args.roots_json and args.roots_json.exists():
        inventory_rows = json.loads(args.roots_json.read_text(encoding="utf-8"))
        api_roots = sorted(row["root"] for row in inventory_rows if row.get("package") == args.api_package)
    else:
        api_roots = load_roots(repo, args.api_package)
    serial_names = {name for name in args.serial_roots.split(",") if name}
    bad_serial = serial_names - set(api_roots)
    if bad_serial:
        raise SystemExit(f"serial roots are not listed: {', '.join(sorted(bad_serial))}")

    parallel_roots = [root for root in api_roots if root not in serial_names]
    root_weights = {root: manifest.get((args.api_package, root), 1000) for root in parallel_roots}
    lanes = lpt_partition(parallel_roots, root_weights, api_shards)
    tasks: list[Task] = []
    task_index = 0
    for package in packages:
        profile = str(coverage_dir / f"coverage-package-{task_index:03d}.out")
        tasks.append(Task(
            name=f"package-{task_index:03d}", package=package, roots=[],
            command=["go", "test", package, "-race", "-count=1", "-timeout", f"{args.task_timeout}s", "-json", f"-coverprofile={profile}"],
            profile=profile, weight_ms=manifest.get((package, "__package__"), PACKAGE_FALLBACK_WEIGHTS.get(package, 1000)),
        ))
        task_index += 1
    plan_rows: list[dict[str, Any]] = []
    for index, lane in enumerate(lanes, start=1):
        profile = str(coverage_dir / f"coverage-api-{index:03d}.out")
        weight = sum(root_weights[root] for root in lane)
        tasks.append(Task(
            name=f"api-shard-{index:03d}", package=args.api_package, roots=lane,
            command=["go", "test", args.api_package, "-race", "-count=1", "-timeout", f"{args.task_timeout}s", "-json", "-run", regex_for(lane), f"-coverprofile={profile}"],
            profile=profile, weight_ms=weight,
        ))
        plan_rows.append({"task": f"api-shard-{index:03d}", "roots": lane, "estimatedMs": weight})
    serial_roots = sorted(serial_names)
    if serial_roots:
        profile = str(coverage_dir / "coverage-api-serial.out")
        tasks.append(Task(
            name="api-serial", package=args.api_package, roots=serial_roots,
            command=["go", "test", args.api_package, "-race", "-count=1", "-timeout", f"{args.task_timeout}s", "-json", "-run", regex_for(serial_roots), f"-coverprofile={profile}"],
            profile=profile, weight_ms=sum(manifest.get((args.api_package, root), 1000) for root in serial_roots), serial=True,
        ))
        plan_rows.append({"task": "api-serial", "roots": serial_roots, "estimatedMs": tasks[-1].weight_ms, "serial": True})

    (artifact / "shard-plan.json").write_text(json.dumps({
        "workers": workers, "taskGOMAXPROCS": task_gomaxprocs,
        "apiShards": plan_rows, "packages": packages,
        "timingManifest": str(args.timing_manifest) if args.timing_manifest else None,
    }, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    total_by_lane = [sum(root_weights[root] for root in lane) for lane in lanes]
    balance = [f"workers={workers} api_shards={api_shards}", "API estimated lanes:"]
    balance.extend(f"lane-{i}: {value/1000:.3f}s ({len(lanes[i])} roots)" for i, value in enumerate(total_by_lane))
    if total_by_lane:
        balance.append(f"skew={max(total_by_lane)-min(total_by_lane):.3f}ms")
    (artifact / "shard-balance.txt").write_text("\n".join(balance) + "\n", encoding="utf-8")

    child_env = dict(os.environ)
    child_env["GOMAXPROCS"] = task_gomaxprocs
    child_env["CI_TEST_WORKERS"] = str(workers)
    child_env["CI_TEST_ROOT_SCHEDULER"] = "bounded-lpt"
    results: list[Result] = []
    non_serial = sorted((task for task in tasks if not task.serial), key=lambda task: (-task.weight_ms, task.name))
    print(f"[ci] test orchestrator: {len(non_serial)} parallel tasks, {len([t for t in tasks if t.serial])} serial tasks, workers={workers}", flush=True)
    # Submit only a bounded initial window.  This keeps the number of live
    # subprocesses bounded by the worker budget rather than relying on the
    # executor's unbounded internal queue.
    with ThreadPoolExecutor(max_workers=workers) as pool:
        pending = {}
        next_index = 0
        while next_index < min(workers, len(non_serial)):
            task = non_serial[next_index]
            pending[pool.submit(run_task, task, repo, child_env, args.task_timeout, logs_dir)] = task
            next_index += 1
        while pending:
            done = next(as_completed(pending))
            task = pending.pop(done)
            result = done.result()
            results.append(result)
            print(f"[ci] {result.task}: rc={result.rc} elapsed={result.elapsed_ms/1000:.3f}s log={result.log}", flush=True)
            if next_index < len(non_serial):
                task = non_serial[next_index]
                pending[pool.submit(run_task, task, repo, child_env, args.task_timeout, logs_dir)] = task
                next_index += 1
    failed = [result for result in results if result.rc != 0]
    if failed:
        for result in sorted(failed, key=lambda item: item.task):
            print(f"[ci] failed task {result.task}: rc={result.rc} log={result.log}", file=sys.stderr)
        return 1

    for task in [task for task in tasks if task.serial]:
        result = run_task(task, repo, child_env, args.task_timeout, logs_dir)
        results.append(result)
        print(f"[ci] {result.task}: rc={result.rc} elapsed={result.elapsed_ms/1000:.3f}s log={result.log}", flush=True)
        if result.rc != 0:
            print(f"[ci] failed task {result.task}: rc={result.rc} log={result.log}", file=sys.stderr)
            return 1

    (artifact / "task-timings.json").write_text(json.dumps([asdict(result) for result in sorted(results, key=lambda item: item.elapsed_ms, reverse=True)], indent=2, sort_keys=True) + "\n", encoding="utf-8")
    (artifact / "test-task-logs.txt").write_text("\n".join(result.log for result in sorted(results, key=lambda item: item.task)) + "\n", encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
