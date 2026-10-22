#!/usr/bin/env python3
"""Require successful trusted main CI and CodeQL for the exact release commit."""

import argparse
import json
import os
import re
import subprocess
import sys
import time

REPOSITORY = "mikkelchokolate/Veil"
WORKFLOWS = ("ci.yml", "codeql.yml")
WAIT_SECONDS = 20 * 60
POLL_SECONDS = 30
PENDING = {"queued", "requested", "waiting", "pending", "in_progress"}
REQUIRED_JOBS = {
    "ci.yml": {
        "frontend", "test", "lint", "multi-process", "sigkill", "filesystem-faults", "image-build",
        "privilege-boundary", "e2e", "browser-e2e", "install-acceptance (amd64)",
        "install-acceptance (arm64)", "package-smoke", "test-shard (1)", "test-shard (2)",
        "test-shard (3)", "package-smoke-matrix (amd64)", "package-smoke-matrix (arm64)",
    },
    "codeql.yml": {"analyze (go)", "analyze (javascript-typescript)", "analyze (actions)"},
}


class GateError(Exception):
    pass


def positive_id(value):
    return type(value) is int and value > 0


def api(endpoint, deadline, fields=(), paginate=False):
    remaining = deadline - time.monotonic()
    if remaining <= 0:
        raise GateError("timed out waiting for exact-commit main CI and CodeQL")
    command = ["gh", "api", "--hostname", "github.com", "--method", "GET", endpoint]
    for key, value in fields:
        command.extend(["-f", f"{key}={value}"])
    if paginate:
        command.extend(["--paginate", "--slurp"])
    try:
        result = subprocess.run(command, capture_output=True, text=True, timeout=min(60, remaining), check=False)
    except (OSError, UnicodeError, subprocess.TimeoutExpired) as error:
        raise GateError(f"GitHub API request failed for {endpoint}: {error}") from error
    if result.returncode:
        raise GateError(f"GitHub API request failed for {endpoint}: {result.stderr.strip()}")
    try:
        return json.loads(result.stdout)
    except (json.JSONDecodeError, TypeError) as error:
        raise GateError(f"malformed GitHub API JSON for {endpoint}") from error


def workflow_id(filename, deadline):
    metadata = api(f"repos/{REPOSITORY}/actions/workflows/{filename}", deadline)
    if (not isinstance(metadata, dict) or not positive_id(metadata.get("id"))
            or metadata.get("path") != f".github/workflows/{filename}" or metadata.get("state") != "active"):
        raise GateError(f"untrusted or malformed workflow identity for {filename}")
    return metadata["id"]


def validate_run(run, filename, identity, sha):
    if not isinstance(run, dict):
        raise GateError(f"malformed workflow run for {filename}")
    expected = {
        "workflow_id": identity, "path": f".github/workflows/{filename}",
        "event": "push", "head_branch": "main", "head_sha": sha,
    }
    for field, value in expected.items():
        if run.get(field) != value:
            raise GateError(f"untrusted {filename} run: {field} does not match {value}")
    for field in ("repository", "head_repository"):
        repository = run.get(field)
        if not isinstance(repository, dict) or repository.get("full_name") != REPOSITORY:
            raise GateError(f"untrusted {filename} run: {field} is not {REPOSITORY}")
    if not all(positive_id(run.get(field)) for field in ("id", "run_number", "run_attempt", "workflow_id")):
        raise GateError(f"malformed {filename} run identifiers")
    status, conclusion = run.get("status"), run.get("conclusion")
    if not isinstance(status, str) or status not in PENDING | {"completed"} or (status != "completed" and conclusion is not None):
        raise GateError(f"malformed {filename} run status/conclusion")
    if status == "completed" and not isinstance(conclusion, str):
        raise GateError(f"malformed {filename} completed conclusion")


def collection(pages, key, label):
    if not isinstance(pages, list) or not pages:
        raise GateError(f"malformed paginated {label}")
    rows = []
    total = None
    for page in pages:
        if (not isinstance(page, dict) or type(page.get("total_count")) is not int
                or page["total_count"] < 0 or not isinstance(page.get(key), list)):
            raise GateError(f"malformed {label} listing")
        if total is not None and total != page["total_count"]:
            raise GateError(f"{label} listing changed during pagination")
        total = page["total_count"]
        rows.extend(page[key])
    if len(rows) != total:
        raise GateError(f"incomplete {label} listing")
    return rows


def latest_run(filename, identity, sha, deadline):
    pages = api(f"repos/{REPOSITORY}/actions/workflows/{identity}/runs", deadline,
                (("event", "push"), ("branch", "main"), ("head_sha", sha), ("per_page", "100")), paginate=True)
    runs = collection(pages, "workflow_runs", filename)
    for run in runs:
        validate_run(run, filename, identity, sha)
    if not runs:
        return None
    if len({run["id"] for run in runs}) != len(runs):
        raise GateError(f"duplicate run identifiers for {filename}")
    selected = max(runs, key=lambda run: (run["run_number"], run["id"]))
    # Fetch the current run, not an old attempt URL: a failed/pending rerun
    # must invalidate an earlier green attempt of the same workflow run.
    current = api(f"repos/{REPOSITORY}/actions/runs/{selected['id']}", deadline)
    validate_run(current, filename, identity, sha)
    if (current["id"] != selected["id"] or current["run_number"] != selected["run_number"]
            or current["run_attempt"] < selected["run_attempt"]):
        raise GateError(f"stale or mismatched run response for {filename}")
    return current


def verify_jobs(filename, run, sha, deadline):
    endpoint = f"repos/{REPOSITORY}/actions/runs/{run['id']}/attempts/{run['run_attempt']}/jobs"
    jobs = collection(api(endpoint, deadline, (("per_page", "100"),), paginate=True), "jobs", filename)
    names, identifiers = set(), set()
    for job in jobs:
        if (not isinstance(job, dict) or not positive_id(job.get("id"))
                or not positive_id(job.get("run_id")) or not positive_id(job.get("run_attempt"))
                or not isinstance(job.get("name"), str) or not job["name"]
                or job.get("run_id") != run["id"] or job.get("run_attempt") != run["run_attempt"]
                or job.get("head_sha") != sha):
            raise GateError(f"untrusted or malformed {filename} attempt job")
        if job["id"] in identifiers or job["name"] in names:
            raise GateError(f"duplicate {filename} attempt job")
        identifiers.add(job["id"])
        names.add(job["name"])
        if job.get("status") != "completed" or job.get("conclusion") != "success":
            raise GateError(f"{filename} job {job['name']} did not succeed: {job.get('status')}/{job.get('conclusion')}")
    missing = REQUIRED_JOBS[filename] - names
    if missing:
        raise GateError(f"{filename} missing required jobs: {', '.join(sorted(missing))}")


def verify(repository, sha):
    if repository != REPOSITORY or not re.fullmatch(r"[0-9a-f]{40}", sha):
        raise GateError("release must name the trusted repository and an exact lowercase 40-character commit SHA")
    deadline = time.monotonic() + WAIT_SECONDS
    identities = {filename: workflow_id(filename, deadline) for filename in WORKFLOWS}
    while time.monotonic() < deadline:
        chosen = {}
        for filename, identity in identities.items():
            run = latest_run(filename, identity, sha, deadline)
            if run is None:
                print(f"[release] waiting for {filename}: main push at {sha}", flush=True)
                continue
            url = f"https://github.com/{REPOSITORY}/actions/runs/{run['id']}/attempts/{run['run_attempt']}"
            print(f"[release] {filename}: {run['status']}/{run['conclusion']} {url} source={sha}", flush=True)
            if run["status"] == "completed":
                if run["conclusion"] != "success":
                    raise GateError(f"latest {filename} attempt did not succeed: {run['conclusion']} ({url})")
                verify_jobs(filename, run, sha, deadline)
                fresh = latest_run(filename, identity, sha, deadline)
                evidence = ("id", "run_number", "run_attempt", "status", "conclusion", "head_sha",
                            "workflow_id", "path", "event", "head_branch")
                if fresh is None or any(fresh[field] != run[field] for field in evidence):
                    print(f"[release] {filename} changed while verifying jobs; checking latest attempt again", flush=True)
                    continue
                chosen[filename] = run["id"]
        if len(chosen) == len(WORKFLOWS):
            if time.monotonic() >= deadline:
                raise GateError("timed out waiting for exact-commit main CI and CodeQL")
            return chosen
        remaining = deadline - time.monotonic()
        if remaining > 0:
            time.sleep(min(POLL_SECONDS, remaining))
    raise GateError("timed out waiting for exact-commit main CI and CodeQL after 20 minutes")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repository", required=True)
    parser.add_argument("--sha", required=True)
    args = parser.parse_args()
    try:
        runs = verify(args.repository, args.sha)
        output = f"ci_run_id={runs['ci.yml']}\ncodeql_run_id={runs['codeql.yml']}\n"
        if os.environ.get("GITHUB_OUTPUT"):
            with open(os.environ["GITHUB_OUTPUT"], "a", encoding="utf-8") as stream:
                stream.write(output)
        print(output, end="", flush=True)
    except (GateError, OSError) as error:
        print(f"release-ci-gate: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
