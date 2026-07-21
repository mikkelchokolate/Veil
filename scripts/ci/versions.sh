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
CI_UBUNTU_BASE="ubuntu:24.04@sha256:4fbb8e6a8395de5a7550b33509421a2bafbc0aab6c06ba2cef9ebffbc7092d90"

# --- Core toolchain ---------------------------------------------------------
CI_GO_VERSION="1.26.5"
CI_GO_TARBALL_SHA256="5c2c3b16caefa1d968a94c1daca04a7ca301a496d9b086e17ad77bb81393f053" # go1.26.5.linux-amd64.tar.gz
# The live-host network path intermittently stalls Go's HTTP/2 proxy connections
# with bytes stuck in Send-Q. Pin HTTP/1.1 transport for deterministic clean-cache
# downloads in both local CI and GitHub Actions.
CI_GO_GODEBUG="http2client=0"

CI_NODE_VERSION="26.5.0"
CI_NODE_TARBALL_SHA256="9f619528f1db5ddc41dccf54211066fb42228d69a156733c69cb9d6cc92e358c" # node-v26.5.0-linux-x64.tar.xz

CI_PNPM_VERSION="11.10.0"

# --- Go CI tools ------------------------------------------------------------
CI_STATICCHECK_VERSION="2026.1"   # honnef.co/go/tools/cmd/staticcheck@2026.1
CI_GOVULNCHECK_VERSION="v1.1.4"   # golang.org/x/vuln/cmd/govulncheck
CI_NFPM_VERSION="v2.40.0"         # github.com/goreleaser/nfpm/v2/cmd/nfpm
CI_REDOCLY_VERSION="1.25.15"      # @redocly/cli (OpenAPI lint)

# --- Browser ----------------------------------------------------------------
# Must match test/browser/package.json @playwright/test and web/package.json
# playwright. vm-build.sh asserts this at image build time.
CI_PLAYWRIGHT_VERSION="1.61.1"

# --- Protocol runtimes (server + clients) ------------------------------------
# Server runtimes are what `veil runtime install` would fetch; for required CI
# they are pre-installed into the CI image from these pinned, checksum-verified
# artifacts instead of resolving /releases/latest at test time.
CI_HYSTERIA_TAG="app/v2.10.0"
CI_HYSTERIA_ASSET="hysteria-linux-amd64"
CI_HYSTERIA_SHA256="04f7804159ef1d798de12a817d73aab4b9040ebe45fc62e223000c5c59e987fe"

CI_MITA_TAG="v3.34.1"
CI_MITA_ASSET="mita_3.34.1_linux_amd64.tar.gz"
CI_MITA_SHA256="499c7390406175a32c140bf31b8b3e1fc2abfe7f4d523e067f09a6fc461e6325"

CI_MIERU_CLIENT_TAG="v3.34.1"
CI_MIERU_CLIENT_ASSET="mieru_3.34.1_linux_amd64.tar.gz"
CI_MIERU_CLIENT_SHA256="b01e374e4776a498c41a171d67d48bf93606eb73ad43a4e387c39b7ebc0611eb"

CI_NAIVE_CLIENT_TAG="v150.0.7871.63-1"
CI_NAIVE_CLIENT_ASSET="naiveproxy-v150.0.7871.63-1-linux-x64.tar.xz"
CI_NAIVE_CLIENT_SHA256="0c4f506ce66a7881892fd6932b542c53fc06ac2351987756096c61e753c687bf"

CI_SINGBOX_TAG="v1.13.14"
CI_SINGBOX_ASSET="sing-box-1.13.14-linux-amd64.tar.gz"
CI_SINGBOX_SHA256="f48703461a15476951ac4967cdad339d986f4b8096b4eb3ff0829a500502d697"

# Caddy-with-forwardproxy (naiveproxy server) is source-built by the product
# installer (internal/runtimeinstall), which pins caddy v2.11.4 and the
# klzgrad/forwardproxy fork via a go.mod replace. Built once at image build
# time; resolved module versions are recorded in the image metadata.

# --- Local VM runtime ---------------------------------------------------------
# Minimum smolvm version required for the authoritative local backend.
CI_SMOLVM_MIN_VERSION="1.6.13"

# --- Test parameters -----------------------------------------------------------
CI_COVERAGE_THRESHOLD="70.0"

# --- Locale / timezone (parity contract) ---------------------------------------
export LANG="C.UTF-8"
export LC_ALL="C.UTF-8"
export TZ="UTC"
export DEBIAN_FRONTEND="noninteractive"
