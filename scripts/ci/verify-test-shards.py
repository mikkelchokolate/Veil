#!/usr/bin/env python3
"""Verify that sharded Go test logs cover each top-level test root once."""

from __future__ import annotations

import json
import sys
from collections import Counter
from pathlib import Path


def main() -> int:
    if len(sys.argv) < 3:
        print(f"usage: {sys.argv[0]} EXPECTED_ROOTS JSON_LOG...", file=sys.stderr)
        return 2

    expected_path = Path(sys.argv[1])
    expected = [line.strip() for line in expected_path.read_text(encoding="utf-8").splitlines() if line.strip()]
    expected_set = set(expected)
    observed: list[str] = []
    failures: list[str] = []

    for log_name in sys.argv[2:]:
        path = Path(log_name)
        for line_number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), start=1):
            try:
                event = json.loads(line)
            except json.JSONDecodeError as exc:
                raise SystemExit(f"{path}:{line_number}: invalid go test JSON: {exc}") from exc
            test_name = event.get("Test")
            action = event.get("Action")
            if not test_name or "/" in test_name:
                continue
            if action in {"pass", "skip", "fail"}:
                observed.append(test_name)
                if action == "fail":
                    failures.append(test_name)

    counts = Counter(observed)
    missing = sorted(expected_set - counts.keys())
    unexpected = sorted(counts.keys() - expected_set)
    duplicate = sorted(name for name, count in counts.items() if count != 1)
    if missing or unexpected or duplicate or failures:
        if missing:
            print(f"missing top-level tests: {', '.join(missing)}", file=sys.stderr)
        if unexpected:
            print(f"unexpected top-level tests: {', '.join(unexpected)}", file=sys.stderr)
        if duplicate:
            print(f"duplicate top-level tests: {', '.join(duplicate)}", file=sys.stderr)
        if failures:
            print(f"failed top-level tests: {', '.join(sorted(set(failures)))}", file=sys.stderr)
        return 1

    print(f"verified {len(expected)} top-level tests across {len(sys.argv) - 2} API shards")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
