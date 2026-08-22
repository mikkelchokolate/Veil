#!/usr/bin/env python3
"""Merge Go coverprofiles produced by independent test processes.

Go's coverprofile format has one record per instrumented source block.  A
sharded run produces duplicate records for package initialization and any
blocks exercised by more than one shard, so concatenating files is invalid.
This merger preserves the profile mode, validates block metadata, and combines
counts with the mode's semantics.
"""

from __future__ import annotations

import argparse
from collections import OrderedDict
from pathlib import Path


def read_profile(path: Path) -> tuple[str, list[tuple[str, int, int]]]:
    lines = path.read_text(encoding="utf-8").splitlines()
    if not lines or not lines[0].startswith("mode: "):
        raise ValueError(f"{path}: missing coverage mode")
    mode = lines[0][len("mode: ") :].strip()
    records: list[tuple[str, int, int]] = []
    for line_number, line in enumerate(lines[1:], start=2):
        if not line.strip():
            continue
        fields = line.rsplit(" ", 2)
        if len(fields) != 3:
            raise ValueError(f"{path}:{line_number}: malformed coverage record")
        location, statements_text, count_text = fields
        try:
            statements = int(statements_text)
            count = int(count_text)
        except ValueError as exc:
            raise ValueError(f"{path}:{line_number}: non-integer coverage record") from exc
        if statements < 0 or count < 0:
            raise ValueError(f"{path}:{line_number}: negative coverage record")
        records.append((location, statements, count))
    return mode, records


def merge(output: Path, inputs: list[Path]) -> None:
    if not inputs:
        raise ValueError("no coverage profiles supplied")

    mode: str | None = None
    merged: OrderedDict[str, tuple[int, int]] = OrderedDict()
    for path in inputs:
        current_mode, records = read_profile(path)
        if mode is None:
            mode = current_mode
        elif current_mode != mode:
            raise ValueError(f"coverage mode mismatch: {path} has {current_mode}, expected {mode}")

        for location, statements, count in records:
            previous = merged.get(location)
            if previous is None:
                merged[location] = (statements, count)
                continue
            old_statements, old_count = previous
            if old_statements != statements:
                raise ValueError(f"statement-count mismatch for coverage block {location}")
            if mode == "set":
                merged[location] = (statements, max(old_count, count))
            else:
                merged[location] = (statements, old_count + count)

    assert mode is not None
    output.parent.mkdir(parents=True, exist_ok=True)
    with output.open("w", encoding="utf-8") as stream:
        stream.write(f"mode: {mode}\n")
        for location, (statements, count) in merged.items():
            stream.write(f"{location} {statements} {count}\n")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("output", type=Path)
    parser.add_argument("inputs", type=Path, nargs="+")
    args = parser.parse_args()
    try:
        merge(args.output, args.inputs)
    except (OSError, ValueError) as exc:
        parser.error(str(exc))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
