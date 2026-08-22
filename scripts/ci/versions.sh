#!/usr/bin/env bash
# scripts/ci/versions.sh — single source of truth for every tool/runtime version
# used by CI, both locally (VM images) and in GitHub Actions.
#
# Rules:
#   - This file is the ONLY place a tool/runtime version may be named.
# shellcheck disable=SC2034  # constants are consumed by sourcing scripts
#     Makefile, Containerfiles and scripts must source it, never restate a value.
#   - Every downloadable artifact carries a SHA256 here; downloads are verified
#     before unpacking or execution (see vm-build.sh / Containerfile).
#   - Bumping a version = edit here; the CI image key changes automatically.

# --- Base image -------------------------------------------------------------
# ubuntu:24.04 manifest digest (linux/amd64). Also mirrored in ci/vm/image.lock.
CI_UBUNTU_BASE="ubuntu:24.04@sha256:52df9b1ee71626e0088f7d400d5c6b5f7bb916f8f0c82b474289a4ece6cf3faf"

# --- Core toolchain ---------------------------------------------------------
CI_GO_VERSION="1.27.0"
CI_GO_TARBALL_SHA256="675c26c449cbb18fc24b74650de1eabbae6e16f64326fd85a283fb3b58280685" # go1.27.0.linux-amd64.tar.gz
# The live-host network path intermittently stalls Go's HTTP/2 proxy connections
# with bytes stuck in Send-Q. Pin HTTP/1.1 transport for deterministic clean-cache
# downloads in both local CI and GitHub Actions. Fall back to checksum-verified
# direct module fetches on any official proxy transport failure.
CI_GO_GODEBUG="http2client=0"
CI_GO_GOPROXY="https://proxy.golang.org|direct"

CI_NODE_VERSION="26.7.0"
CI_NODE_TARBALL_SHA256="982aa24dd8be4c889c6a8ab337ddff3b0896645b20f4239356e80552c16277ee" # node-v26.7.0-linux-x64.tar.xz

CI_NPM_VERSION="12.0.2"
CI_PNPM_VERSION="11.22.0"

# --- Go CI tools ------------------------------------------------------------
CI_STATICCHECK_VERSION="2026.2.1" # honnef.co/go/tools/cmd/staticcheck@2026.2.1
CI_GOVULNCHECK_VERSION="v1.7.0"   # golang.org/x/vuln/cmd/govulncheck
CI_NFPM_VERSION="v2.47.0"         # github.com/goreleaser/nfpm/v2/cmd/nfpm
CI_REDOCLY_VERSION="2.47.0"       # @redocly/cli (OpenAPI lint)

# Docker CLI and Buildx used by the system image to talk to the mounted host daemon.
CI_DOCKER_CLI_VERSION="29.6.2"
CI_DOCKER_CLI_SHA256="d6204aea92238e2453d5445c885b9d2e5eb8f82915568ec50edf9dbe12a3ac74"
CI_DOCKER_BUILDX_VERSION="0.35.0"
CI_DOCKER_BUILDX_SHA256="d41ece72044243b4f58b343441ae37446d9c29a7d6b5e11c61847bbcf8f7dfda"

# --- Browser ----------------------------------------------------------------
# Must match test/browser/package.json @playwright/test and web/package.json
# playwright. vm-build.sh asserts this at image build time.
CI_PLAYWRIGHT_VERSION="1.62.1"

# --- Protocol runtimes (server + clients) ------------------------------------
# Server runtimes are what `veil runtime install` would fetch; for required CI
# they are pre-installed into the CI image from these pinned, checksum-verified
# artifacts instead of resolving /releases/latest at test time.
CI_HYSTERIA_TAG="app/v2.12.1"
CI_HYSTERIA_ASSET="hysteria-linux-amd64"
CI_HYSTERIA_SHA256="ffc032c7ca6b78676d337097ca7f61bebc3a90a4f3a656693adf368f304cdbc7"

CI_MITA_TAG="v3.36.0"
CI_MITA_ASSET="mita_3.36.0_linux_amd64.tar.gz"
CI_MITA_SHA256="d61f35c463f101580a108dd6b969e1a3dca1b84836332b2533302a62e70f04bb"

CI_MIERU_CLIENT_TAG="v3.36.0"
CI_MIERU_CLIENT_ASSET="mieru_3.36.0_linux_amd64.tar.gz"
CI_MIERU_CLIENT_SHA256="b3f8b32a8b5728c01f31e33ff7a71b3b33f3fd8e1341684fcb98d5ecebb7db7a"

CI_NAIVE_CLIENT_TAG="v150.0.7871.63-1"
CI_NAIVE_CLIENT_ASSET="naiveproxy-v150.0.7871.63-1-linux-x64.tar.xz"
CI_NAIVE_CLIENT_SHA256="0c4f506ce66a7881892fd6932b542c53fc06ac2351987756096c61e753c687bf"

CI_SINGBOX_TAG="v1.13.19"
CI_SINGBOX_ASSET="sing-box-1.13.19-linux-amd64.tar.gz"
CI_SINGBOX_SHA256="ef88a9e577d474210867bd708933d042e9b70106529df2656182c9db90106aa1"

# Caddy-with-forwardproxy (naiveproxy server) is source-built by the product
# installer (internal/runtimeinstall), which pins caddy v2.11.4 and the
# klzgrad/forwardproxy fork via a go.mod replace. Built once at image build
# time and shared by base/browser/system targets.
CI_CADDY_VERSION="v2.11.4"
CI_FORWARDPROXY_VERSION="d62c80d3dd2c706b6b87579844d2397bddd18317"

# --- Local VM runtime ---------------------------------------------------------
# Minimum smolvm version required for the local VM backend.
CI_SMOLVM_MIN_VERSION="1.6.13"

# --- Test parameters -----------------------------------------------------------
CI_COVERAGE_THRESHOLD="70.0"

# --- Locale / timezone (parity contract) ---------------------------------------
export LANG="C.UTF-8"
export LC_ALL="C.UTF-8"
export TZ="UTC"
export DEBIAN_FRONTEND="noninteractive"
