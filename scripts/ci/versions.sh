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
CI_UBUNTU_BASE="ubuntu:24.04@sha256:a61567bd31828687156d735ea8eb01ba4e37636e225dd6a48ba94136a70d9d61"

# --- Core toolchain ---------------------------------------------------------
CI_GO_VERSION="1.27.1"
CI_GO_TARBALL_SHA256="63d339f0da5ab53635a56f2490a7984dfe12dfcff22ad749f63edaf590168445" # go1.27.1.linux-amd64.tar.gz
# The live-host network path intermittently stalls Go's HTTP/2 proxy connections
# with bytes stuck in Send-Q. Pin HTTP/1.1 transport for deterministic clean-cache
# downloads in both local CI and GitHub Actions. Fall back to checksum-verified
# direct module fetches on any official proxy transport failure.
CI_GO_GODEBUG="http2client=0"
CI_GO_GOPROXY="https://proxy.golang.org|direct"

CI_NODE_VERSION="26.8.2"
CI_NODE_TARBALL_SHA256="40e1d3225c1c9ae9a2671c98ecb9857e4d5555026394f348645676798840d5c5" # node-v26.8.2-linux-x64.tar.xz
CI_NODE_MUSL_TARBALL_SHA256="e174fe6faf29bf1b02531e81e97989cb62690c305684e7364cdc1c24f482c237" # node-v26.8.2-linux-x64-musl.tar.xz

CI_NPM_VERSION="12.0.2"
CI_PNPM_VERSION="12.4.1"

# --- Go CI tools ------------------------------------------------------------
CI_STATICCHECK_VERSION="2026.2.1" # honnef.co/go/tools/cmd/staticcheck@2026.2.1
CI_GOVULNCHECK_VERSION="v1.7.0"   # golang.org/x/vuln/cmd/govulncheck
CI_NFPM_VERSION="v2.47.0"         # github.com/goreleaser/nfpm/v2/cmd/nfpm
CI_REDOCLY_VERSION="2.52.1"       # @redocly/cli (OpenAPI lint)

# Docker CLI and Buildx used by the system image to talk to the mounted host daemon.
CI_DOCKER_CLI_VERSION="29.8.0"
CI_DOCKER_CLI_SHA256="cc21815cf1e2efed867dc9c8b96b46ffed8ea176ffab32b0aacb54726ded8f25"
CI_DOCKER_BUILDX_VERSION="0.37.1"
CI_DOCKER_BUILDX_SHA256="9447199cdb435f25880548343c128a4b6650e8891ee598905d8d29d39a8e359b"

# --- Browser ----------------------------------------------------------------
# Must match test/browser/package.json @playwright/test and web/package.json
# playwright. vm-build.sh asserts this at image build time.
CI_PLAYWRIGHT_VERSION="1.63.0"

# --- Protocol runtimes (server + clients) ------------------------------------
# Server runtimes are what `veil runtime install` would fetch; for required CI
# they are pre-installed into the CI image from these pinned, checksum-verified
# artifacts instead of resolving /releases/latest at test time.
CI_HYSTERIA_TAG="app/v2.12.2"
CI_HYSTERIA_ASSET="hysteria-linux-amd64"
CI_HYSTERIA_SHA256="6493dfffd55b5883f64c76c63880ecc32988f0c568c9ca9014907877b4d55f94"

CI_MITA_TAG="v3.36.1"
CI_MITA_ASSET="mita_3.36.1_linux_amd64.tar.gz"
CI_MITA_SHA256="8e6ae525bbcaa688a8446aebf3c2ecbcb4ce606838d23edfa3f7f2fad506a8f7"

CI_MIERU_CLIENT_TAG="v3.36.1"
CI_MIERU_CLIENT_ASSET="mieru_3.36.1_linux_amd64.tar.gz"
CI_MIERU_CLIENT_SHA256="d586405d33b10e907ec28e4f420b74e25d3240af826d8a396e127e76781f6316"

CI_NAIVE_CLIENT_TAG="v150.0.7871.63-1"
CI_NAIVE_CLIENT_ASSET="naiveproxy-v150.0.7871.63-1-linux-x64.tar.xz"
CI_NAIVE_CLIENT_SHA256="0c4f506ce66a7881892fd6932b542c53fc06ac2351987756096c61e753c687bf"

CI_SINGBOX_TAG="v1.14.0"
CI_SINGBOX_ASSET="sing-box-1.14.0-linux-amd64.tar.gz"
CI_SINGBOX_SHA256="2375de6999f4f56ab46b4fc5ddf26a6aba1d3e61a0f4e7ddec2f4690457d5f63"

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
