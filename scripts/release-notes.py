#!/usr/bin/env python3
"""Extract one exact tagged section from CHANGELOG.md."""

import argparse
import re
from pathlib import Path


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--changelog", required=True)
    parser.add_argument("--tag", required=True)
    parser.add_argument("--output", required=True)
    args = parser.parse_args()

    if not re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?", args.tag):
        raise SystemExit("tag is not an exact release version")
    text = Path(args.changelog).read_text(encoding="utf-8")
    heading = re.compile(rf"^## \[{re.escape(args.tag)}\](?:\s+-\s+[^\n]+)?\s*$", re.MULTILINE)
    matches = list(heading.finditer(text))
    if len(matches) != 1:
        raise SystemExit(f"CHANGELOG must contain exactly one section for {args.tag}")
    start = matches[0].start()
    following = re.search(r"^##\s+", text[matches[0].end():], re.MULTILINE)
    end = matches[0].end() + following.start() if following else len(text)
    section = text[start:end].strip() + "\n"
    if "## Unreleased" in section or len(section.splitlines()) < 2:
        raise SystemExit("tagged changelog section is empty or ambiguous")
    Path(args.output).write_text(section, encoding="utf-8")


if __name__ == "__main__":
    main()
