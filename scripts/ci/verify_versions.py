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
    "CI_GO_TARBALL_SHA256",
    "CI_GO_TARBALL_SHA256_ARM64",
    "CI_NODE_VERSION",
    "CI_PNPM_VERSION",
    "CI_PLAYWRIGHT_VERSION",
    "CI_CADDY_VERSION",
    "CI_FORWARDPROXY_VERSION",
    "CI_UBUNTU_BASE",
    # Protocol runtime pins: the checksum-verified asset/sha pins consumed by
    # runtimes.sh and vm-build.sh must be gated alongside the tags — deleting
    # or emptying them otherwise greens this job while runtime install drifts
    # or fails closed later (issues #678, #684).
    "CI_HYSTERIA_TAG",
    "CI_HYSTERIA_ASSET",
    "CI_HYSTERIA_SHA256",
    "CI_MITA_TAG",
    "CI_MITA_ASSET",
    "CI_MITA_SHA256",
    "CI_MIERU_CLIENT_TAG",
    "CI_MIERU_CLIENT_ASSET",
    "CI_MIERU_CLIENT_SHA256",
    "CI_NAIVE_CLIENT_TAG",
    "CI_NAIVE_CLIENT_ASSET",
    "CI_NAIVE_CLIENT_SHA256",
    "CI_SINGBOX_TAG",
    "CI_SINGBOX_ASSET",
    "CI_SINGBOX_SHA256",
    "CI_OAPI_CODEGEN_VERSION",
    "CI_SHELLCHECK_VERSION",
    "CI_SYFT_VERSION",
    "CI_NODE_IMAGE_DIGEST",
    "CI_GO_IMAGE_DIGEST",
    "CI_ALPINE_VERSION",
    "CI_ALPINE_IMAGE_DIGEST",
    # Coverage floors are part of the gate contract: an empty or zeroed pin
    # turns the coverage stage into a no-op (issue #682).
    "CI_COVERAGE_THRESHOLD",
    "CI_SDK_COVERAGE_FLOOR",
}
missing = sorted(required - versions.keys())
if missing:
    fail(f"versions.sh is missing {', '.join(missing)}")


def require_numeric_floor(name: str, minimum: float) -> None:
    try:
        value = float(versions[name])
    except ValueError:
        fail(f"{name} must be numeric, found {versions[name]!r}")
    if value < minimum:
        fail(f"{name}={value} is below the documented minimum {minimum}")


# The product coverage threshold implements the documented 70% contract and
# the SDK floor proves the merged sdk profile is really measured — both must
# stay numeric and non-zero or `test.sh`'s `cov >= min` awk check goes soft
# (issue #682). Raising a floor is allowed; lowering it is a policy change.
require_numeric_floor("CI_COVERAGE_THRESHOLD", 70.0)
require_numeric_floor("CI_SDK_COVERAGE_FLOOR", 1.0)

# The versioned release assets must carry the pinned tag so a tag bump without
# the matching asset rename (or vice versa) cannot drift (issues #678, #684).
# CI_HYSTERIA_ASSET ("hysteria-linux-amd64") is intentionally exempt: upstream
# ships the same arch-only asset name on every release.
for tag_var, asset_var in (
    ("CI_MITA_TAG", "CI_MITA_ASSET"),
    ("CI_MIERU_CLIENT_TAG", "CI_MIERU_CLIENT_ASSET"),
    ("CI_NAIVE_CLIENT_TAG", "CI_NAIVE_CLIENT_ASSET"),
    ("CI_SINGBOX_TAG", "CI_SINGBOX_ASSET"),
):
    tag = versions[tag_var]
    asset = versions[asset_var]
    if tag not in asset and tag.removeprefix("v") not in asset:
        fail(f"{asset_var} {asset!r} does not carry the {tag_var} version {tag!r}")


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
browser_lock = json.loads((ROOT / "test/browser/package-lock.json").read_text())
foreign_browser_urls = sorted(
    {
        package["resolved"]
        for package in browser_lock.get("packages", {}).values()
        if package.get("resolved")
        and not package["resolved"].startswith("https://registry.npmjs.org/")
    }
)
if foreign_browser_urls:
    fail("browser lock uses foreign registry URLs: " + ", ".join(foreign_browser_urls))

dockerfile = (ROOT / "Dockerfile").read_text()
expect(
    "Dockerfile Node image",
    match(r"^ARG NODE_IMAGE=node:([0-9.]+)-alpine@", dockerfile, "Dockerfile Node image"),
    versions["CI_NODE_VERSION"],
)
expect(
    "Dockerfile Node image digest",
    match(
        r"^ARG NODE_IMAGE=node:[0-9.]+-alpine@(sha256:[0-9a-f]{64})$",
        dockerfile,
        "Dockerfile Node image digest",
    ),
    versions["CI_NODE_IMAGE_DIGEST"],
)
expect(
    "Dockerfile Go image",
    match(r"^ARG GO_IMAGE=golang:([0-9.]+)-alpine@", dockerfile, "Dockerfile Go image"),
    versions["CI_GO_VERSION"],
)
expect(
    "Dockerfile Go image digest",
    match(
        r"^ARG GO_IMAGE=golang:[0-9.]+-alpine@(sha256:[0-9a-f]{64})$",
        dockerfile,
        "Dockerfile Go image digest",
    ),
    versions["CI_GO_IMAGE_DIGEST"],
)
expect(
    "Dockerfile Alpine tag",
    match(r"^ARG ALPINE_IMAGE=alpine:([0-9.]+)@", dockerfile, "Dockerfile Alpine tag"),
    versions["CI_ALPINE_VERSION"],
)
expect(
    "Dockerfile Alpine digest",
    match(
        r"^ARG ALPINE_IMAGE=alpine:[0-9.]+@(sha256:[0-9a-f]{64})$",
        dockerfile,
        "Dockerfile Alpine digest",
    ),
    versions["CI_ALPINE_IMAGE_DIGEST"],
)
expect(
    "Dockerfile GODEBUG",
    match(r"^ARG GO_GODEBUG=([^\s]+)$", dockerfile, "Dockerfile GODEBUG"),
    versions["CI_GO_GODEBUG"],
)
expect(
    "Dockerfile GOPROXY",
    match(r"^ARG GO_GOPROXY=([^\s]+)$", dockerfile, "Dockerfile GOPROXY"),
    versions["CI_GO_GOPROXY"],
)
expect(
    "Dockerfile npm",
    match(r"^ARG NPM_VERSION=([0-9.]+)$", dockerfile, "Dockerfile npm"),
    versions["CI_NPM_VERSION"],
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

# The installer's embedded Go toolchain pins must match versions.sh — a drifted
# tarball SHA silently breaks `veil runtime install` on hosts without Go (#402).
gotoolchain = (ROOT / "internal/runtimeinstall/gotoolchain.go").read_text()
expect(
    "gotoolchain defaultGoVersion",
    match(r'defaultGoVersion\s*=\s*"([^"]+)"', gotoolchain, "defaultGoVersion"),
    versions["CI_GO_VERSION"],
)
expect(
    "gotoolchain linux-amd64 SHA256",
    match(r'"linux-amd64":\s*"([0-9a-f]{64})"', gotoolchain, "linux-amd64 SHA256"),
    versions["CI_GO_TARBALL_SHA256"],
)
expect(
    "gotoolchain linux-arm64 SHA256",
    match(r'"linux-arm64":\s*"([0-9a-f]{64})"', gotoolchain, "linux-arm64 SHA256"),
    versions["CI_GO_TARBALL_SHA256_ARM64"],
)
expect(
    "sing-box catalog version",
    match(r'singBoxVersion\s*=\s*"([^"]+)"', runtime_install, "singBoxVersion"),
    versions["CI_SINGBOX_TAG"],
)
expect(
    "sing-box amd64 digest",
    # Scope to the singBoxDigests map so an unrelated amd64 digest elsewhere in
    # the file cannot satisfy the check by accident.
    match(r'singBoxDigests[^}]*"amd64":\s*"([0-9a-f]{64})"', runtime_install, "sing-box amd64 digest"),
    versions["CI_SINGBOX_SHA256"],
)

# Protocol plugin runtime pins must match versions.sh (#402).
hysteria_runtime = (ROOT / "internal/protocols/hysteria2/runtime.go").read_text()
expect(
    "hysteria2 runtime tag",
    match(r'Version:\s*"([^"]+)"', hysteria_runtime, "hysteria2 Version"),
    versions["CI_HYSTERIA_TAG"],
)
mieru_runtime = (ROOT / "internal/protocols/mieru/runtime.go").read_text()
expect(
    "mieru runtime tag",
    match(r'Version:\s*"([^"]+)"', mieru_runtime, "mieru Version"),
    versions["CI_MITA_TAG"],
)

# The SDK generator pin lives in the //go:generate directive (#402).
sdk_generate = (ROOT / "sdk/go/generate.go").read_text()
expect(
    "oapi-codegen generate pin",
    match(r"oapi-codegen@([^\s]+)", sdk_generate, "oapi-codegen version"),
    versions["CI_OAPI_CODEGEN_VERSION"],
)

# packages.lock records the shellcheck candidate the CI image installs; it must
# carry the pinned upstream version (#420).
packages_lock = (ROOT / "ci/vm/packages.lock").read_text()
expect(
    "packages.lock shellcheck",
    match(r"^shellcheck\s+(\S+)$", packages_lock, "packages.lock shellcheck"),
    f"{versions['CI_SHELLCHECK_VERSION']}-1",
)

# Every workflow `uses:` ref must be a full-length commit SHA so dependabot
# retargets and tag rewrites cannot silently change gate behaviour (#417).
for workflow in sorted((ROOT / ".github/workflows").glob("*.yml")):
    for line_no, line in enumerate(workflow.read_text().splitlines(), start=1):
        ref = re.search(r"\buses:\s*[^\s@]+@([^\s#]+)", line)
        if ref and not re.fullmatch(r"[0-9a-f]{40}", ref.group(1)):
            fail(
                f"{workflow.name}:{line_no}: action ref {ref.group(1)!r} is not a "
                "pinned 40-char commit SHA"
            )

docker_entrypoint = (ROOT / "packaging/docker/entrypoint.sh").read_text()
# Assert the full VAR:-default marker, not a bare path substring — "/etc/veil"
# alone would false-green on the KEY_PATH line without verifying APPLY_ROOT
# at all (issue #393). The leaf defaults derive from the VEIL_VAR_DIR /
# VEIL_ETC_DIR roots so a custom-roots container is not pinned back onto the
# packaged tree; explicit leaf env still wins (issue #672).
for label, value in (
    ("state", "VEIL_STATE_PATH:-${VEIL_VAR_DIR:-/var/lib/veil}/state.json"),
    ("apply", "VEIL_APPLY_ROOT:-${VEIL_VAR_DIR:-/var/lib/veil}/staging"),
    ("key", "VEIL_KEY_PATH:-${VEIL_ETC_DIR:-/etc/veil}/state.key"),
):
    if value not in docker_entrypoint:
        fail(f"Docker entrypoint does not contain the {label} default {value}")

# package-smoke must run against the digest-pinned distro images declared here,
# never floating tags (issue #394), and the Alpine smoke base must stay
# identical to the shipped container's runtime base.
smoke = (ROOT / "scripts/package-smoke.sh").read_text()
for var in ("CI_SMOKE_DEBIAN_IMAGE", "CI_SMOKE_ROCKYLINUX_IMAGE", "CI_SMOKE_ALPINE_IMAGE"):
    if var not in versions:
        fail(f"versions.sh is missing {var}")
    if "@" not in versions[var]:
        fail(f"{var} must be pinned by digest (image@sha256:...)")
    if var not in smoke:
        fail(f"scripts/package-smoke.sh does not use the pinned ${var}")
expect(
    "smoke Alpine image == Dockerfile ALPINE_IMAGE",
    versions["CI_SMOKE_ALPINE_IMAGE"],
    match(r"^ARG ALPINE_IMAGE=(\S+)$", dockerfile, "Dockerfile Alpine image"),
)
for floating in ("debian:", "rockylinux:", "alpine:"):
    if floating in smoke:
        fail(f"scripts/package-smoke.sh contains floating distro reference {floating!r}")

print("version pins are consistent")
