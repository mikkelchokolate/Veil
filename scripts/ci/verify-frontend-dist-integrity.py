#!/usr/bin/env python3
# scripts/ci/verify-frontend-dist-integrity.py — regression gate for #1147.
#
# dist.sha256 records SHA256s of REGULAR files only. Before the fix, an
# artifact carrying a symlink/FIFO/socket/device inside dist/ passed the
# manifest check untouched and `cp -a` restored the unverified object into
# web/dist. This verifier fabricates artifact trees and drives the real
# scripts/ci/prepare-frontend-dist.sh to prove non-regular entries are now
# rejected — and that a clean artifact still restores.
#
# Runs under lint.sh (Linux bash env: git, ln, mkfifo available).

import hashlib
import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "scripts/ci/prepare-frontend-dist.sh"
WEB_DIST = ROOT / "web/dist"


def fail(msg: str) -> None:
    print(f"verify-frontend-dist-integrity: {msg}", file=sys.stderr)
    sys.exit(1)


def sha256_manifest(dist: Path) -> str:
    """Reproduce `find . -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum`."""
    entries = []
    for p in dist.rglob("*"):
        if p.is_file() and not p.is_symlink():
            rel = "./" + p.relative_to(dist).as_posix()
            digest = hashlib.sha256(p.read_bytes()).hexdigest()
            entries.append(f"{digest}  {rel}")
    entries.sort()  # LC_ALL=C byte sort; repo dist paths are ASCII
    return "\n".join(entries) + "\n"


def make_artifact(base: Path, head: str) -> Path:
    artifact = base / "artifact"
    dist = artifact / "dist"
    (dist / "assets").mkdir(parents=True)
    (dist / "index.html").write_text("<!doctype html><title>t</title><script src=assets/app.js></script>")
    (dist / "assets/app.js").write_text("console.log('artifact app');\n")
    (artifact / "source.sha").write_text(head + "\n")
    (artifact / "dist.sha256").write_text(sha256_manifest(dist))
    return artifact


def run_prepare(artifact: Path, work: Path) -> subprocess.CompletedProcess:
    env = dict(os.environ)
    env["CI_FRONTEND_DIST_ARTIFACT_DIR"] = str(artifact)
    env["CI_ARTIFACT_DIR"] = str(work / "artifacts")
    return subprocess.run(
        ["bash", str(SCRIPT)],
        cwd=ROOT,
        env=env,
        capture_output=True,
        text=True,
    )


def main() -> None:
    head = subprocess.run(
        ["git", "rev-parse", "HEAD"], cwd=ROOT, capture_output=True, text=True, check=True
    ).stdout.strip()

    # Snapshot web/dist — prepare-frontend-dist.sh deletes and repopulates it.
    had_dist = WEB_DIST.exists()
    bak = ROOT / "web/dist.integrity-bak"
    if bak.exists():
        shutil.rmtree(bak)
    if had_dist:
        WEB_DIST.rename(bak)

    try:
        with tempfile.TemporaryDirectory(prefix="fdi-") as td:
            work = Path(td)

            # Case 1: a clean artifact still restores.
            artifact = make_artifact(work / "case1", head)
            r = run_prepare(artifact, work / "case1")
            if r.returncode != 0:
                fail(f"clean artifact rejected (rc={r.returncode}):\n{r.stderr}")
            if not (WEB_DIST / "assets/app.js").is_file():
                fail("clean artifact did not restore web/dist content")

            # Case 2: an injected symlink must be rejected, even though the
            # manifest hashes only regular files and would still match.
            artifact = make_artifact(work / "case2", head)
            (artifact / "dist/evil").symlink_to("/etc/passwd")
            r = run_prepare(artifact, work / "case2")
            if r.returncode == 0:
                fail("artifact containing a symlink was ACCEPTED")
            if "non-regular" not in r.stderr:
                fail(f"symlink rejection did not explain itself:\n{r.stderr}")
            if (WEB_DIST / "evil").exists() or (WEB_DIST / "evil").is_symlink():
                fail("rejected symlink artifact still landed in web/dist")

            # Case 3: a FIFO is equally invisible to the regular-file manifest.
            artifact = make_artifact(work / "case3", head)
            os.mkfifo(artifact / "dist/pipe")
            r = run_prepare(artifact, work / "case3")
            if r.returncode == 0:
                fail("artifact containing a fifo was ACCEPTED")
            if "non-regular" not in r.stderr:
                fail(f"fifo rejection did not explain itself:\n{r.stderr}")

            # Case 4 (sanity): a tampered regular file still fails the manifest.
            artifact = make_artifact(work / "case4", head)
            (artifact / "dist/index.html").write_text("<!doctype html>tampered")
            r = run_prepare(artifact, work / "case4")
            if r.returncode == 0:
                fail("tampered regular file passed the manifest check")

            print("frontend-dist integrity: clean restores, symlink/fifo/tamper rejected")
    finally:
        # web/dist is left containing the last restored artifact; drop it and
        # put back whatever the caller had (usually nothing or the stub).
        if WEB_DIST.exists() or WEB_DIST.is_symlink():
            shutil.rmtree(WEB_DIST)
        if had_dist:
            bak.rename(WEB_DIST)


if __name__ == "__main__":
    main()
