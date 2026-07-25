#!/usr/bin/env python3
"""Fail when repository-owned dependency/tool pins drift apart."""

from __future__ import annotations

import json
import re
from pathlib import Path
from typing import NoReturn

ROOT = Path(__file__).resolve().parents[2]


def fail(message: str) -> NoReturn:
    raise SystemExit(f"version consistency error: {message}")


def match(pattern: str, text: str, label: str) -> str:
    found = re.search(pattern, text, re.MULTILINE)
    if found is None:
        fail(f"cannot read {label}")
    return found.group(1)


versions_text = (ROOT / "scripts/ci/versions.sh").read_text()
versions = {
    name: value
    for name, value in re.findall(r'^([A-Z][A-Z0-9_]*)="([^"]+)"', versions_text, re.MULTILINE)
}

required = {
    "CI_GO_VERSION",
    "CI_NODE_VERSION",
    "CI_PNPM_VERSION",
    "CI_PLAYWRIGHT_VERSION",
    "CI_CADDY_VERSION",
    "CI_FORWARDPROXY_VERSION",
    "CI_UBUNTU_BASE",
}
missing = sorted(required - versions.keys())
if missing:
    fail(f"versions.sh is missing {', '.join(missing)}")


def expect(label: str, actual: str, expected: str) -> None:
    if actual != expected:
        fail(f"{label}: found {actual!r}, expected {expected!r}")


go_mod = (ROOT / "go.mod").read_text()
expect(
    "go.mod Go version",
    match(r"^go\s+(\S+)$", go_mod, "go.mod Go version"),
    versions["CI_GO_VERSION"],
)

web_package = json.loads((ROOT / "web/package.json").read_text())
expect(
    "web packageManager",
    web_package.get("packageManager", "").removeprefix("pnpm@"),
    versions["CI_PNPM_VERSION"],
)
expect(
    "web Playwright",
    web_package["devDependencies"]["playwright"].lstrip("^~"),
    versions["CI_PLAYWRIGHT_VERSION"],
)

browser_package = json.loads((ROOT / "test/browser/package.json").read_text())
expect(
    "browser Playwright",
    browser_package["devDependencies"]["@playwright/test"].lstrip("^~"),
    versions["CI_PLAYWRIGHT_VERSION"],
)

dockerfile = (ROOT / "Dockerfile").read_text()
expect(
    "Dockerfile Node image",
    match(r"^ARG NODE_IMAGE=node:([0-9.]+)-alpine@", dockerfile, "Dockerfile Node image"),
    versions["CI_NODE_VERSION"],
)
expect(
    "Dockerfile Go image",
    match(r"^ARG GO_IMAGE=golang:([0-9.]+)-alpine@", dockerfile, "Dockerfile Go image"),
    versions["CI_GO_VERSION"],
)
expect(
    "Dockerfile pnpm",
    match(r"^ARG PNPM_VERSION=([0-9.]+)$", dockerfile, "Dockerfile pnpm"),
    versions["CI_PNPM_VERSION"],
)

image_lock = (ROOT / "ci/vm/image.lock").read_text()
locked_digest = match(
    r"^UBUNTU_24_04_DIGEST=(sha256:[0-9a-f]{64})$",
    image_lock,
    "Ubuntu image lock",
)
expect(
    "Ubuntu image lock",
    f"ubuntu:24.04@{locked_digest}",
    versions["CI_UBUNTU_BASE"],
)
ci_containerfile = (ROOT / "ci/vm/Containerfile").read_text()
expect(
    "CI Containerfile Ubuntu default",
    match(
        r"^ARG UBUNTU_BASE=(ubuntu:24\.04@sha256:[0-9a-f]{64})$",
        ci_containerfile,
        "CI Containerfile Ubuntu default",
    ),
    versions["CI_UBUNTU_BASE"],
)

runtime_install = (ROOT / "internal/runtimeinstall/runtimeinstall.go").read_text()
for label, value in (
    ("Caddy", versions["CI_CADDY_VERSION"]),
    ("forwardproxy", versions["CI_FORWARDPROXY_VERSION"]),
):
    if value not in runtime_install:
        fail(f"runtime installer does not contain the {label} pin {value}")

print("version pins are consistent")
