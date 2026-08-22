#!/usr/bin/env python3
"""Generate Veil's signed SLSA v1 release provenance statement."""

import argparse
import hashlib
import json
import re
from pathlib import Path


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--dist", required=True)
    parser.add_argument("--repository", required=True)
    parser.add_argument("--tag", required=True)
    parser.add_argument("--commit", "--source-commit", dest="commit", required=True)
    parser.add_argument("--workflow", default=".github/workflows/release.yml")
    parser.add_argument("--go-version", required=True)
    parser.add_argument("--node-version", required=True)
    parser.add_argument("--pnpm-version", required=True)
    parser.add_argument("--dependency-manifest", action="append", required=True)
    parser.add_argument("--output", required=True)
    args = parser.parse_args()

    if not re.fullmatch(r"[0-9a-f]{40}", args.commit):
        raise SystemExit("--commit must be an exact 40-character lowercase Git SHA")
    if not re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?", args.tag):
        raise SystemExit("--tag must be an exact release tag")

    dist = Path(args.dist).resolve()
    output = Path(args.output).resolve()
    candidates = []
    for path in sorted(dist.iterdir()):
        if not path.is_file() or path.resolve() == output:
            continue
        if path.name.endswith(".bundle") or path.name.endswith(".sig"):
            continue
        if path.name == "veil.provenance.json":
            continue
        candidates.append({"name": path.name, "digest": {"sha256": sha256(path)}})

    if not any(subject["name"] == "checksums.txt" for subject in candidates):
        raise SystemExit("checksums.txt is required before provenance generation")
    if not any(subject["name"].startswith("veil_linux_") and subject["name"].endswith(".tar.gz") for subject in candidates):
        raise SystemExit("at least one Veil release archive is required")

    identity = f"https://github.com/{args.repository}/{args.workflow}@refs/tags/{args.tag}"
    resolved_dependencies = [
        {
            "uri": f"git+https://github.com/{args.repository}@refs/tags/{args.tag}",
            "digest": {"gitCommit": args.commit},
        }
    ]
    for manifest_value in args.dependency_manifest:
        manifest = Path(manifest_value).resolve()
        if not manifest.is_file():
            raise SystemExit(f"dependency manifest not found: {manifest_value}")
        resolved_dependencies.append(
            {"uri": f"file://{manifest.name}", "digest": {"sha256": sha256(manifest)}}
        )
    statement = {
        "_type": "https://in-toto.io/Statement/v1",
        "subject": candidates,
        "predicateType": "https://slsa.dev/provenance/v1",
        "predicate": {
            "buildDefinition": {
                "buildType": identity,
                "externalParameters": {
                    "repository": f"https://github.com/{args.repository}",
                    "ref": f"refs/tags/{args.tag}",
                    "sourceCommit": args.commit,
                    "sourceTag": args.tag,
                    "workflow": args.workflow,
                    "toolchain": {
                        "go": args.go_version,
                        "node": args.node_version,
                        "pnpm": args.pnpm_version,
                    },
                },
                "internalParameters": {},
                "resolvedDependencies": resolved_dependencies,
            },
            "runDetails": {
                "builder": {"id": identity},
                "metadata": {"invocationId": args.commit},
                "byproducts": [],
            },
        },
    }
    output.write_text(json.dumps(statement, sort_keys=True, separators=(",", ":")) + "\n", encoding="utf-8")


if __name__ == "__main__":
    main()
