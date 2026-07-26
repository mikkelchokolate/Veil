BINARY=veil
VERSION?=dev
DOCKER_IMAGE?=ghcr.io/mikkelchokolate/veil
GOARCH?=$(shell go env GOARCH)
MAINTAINER?=Veil Maintainers <veil@users.noreply.github.com>

.PHONY: test build tidy docker release-check dist package package-deb package-rpm package-apk sbom e2e generate-sdk verify-sdk verify-openapi verify-release

test:
	go test ./...

e2e:
	go test -tags e2e ./test/e2e/... -count=1

verify-openapi:
	@set -eu; \
	. ./scripts/ci/versions.sh; \
	if command -v redocly >/dev/null 2>&1 && [ "$$(redocly --version)" = "$$CI_REDOCLY_VERSION" ]; then \
		redocly lint docs/openapi.yaml --config .redocly.yaml; \
	else \
		npm_config_update_notifier=false npx --yes --package="@redocly/cli@$$CI_REDOCLY_VERSION" -- redocly lint docs/openapi.yaml --config .redocly.yaml; \
	fi

generate-sdk:
	go generate ./sdk/go

verify-sdk:
	go generate ./sdk/go
	git diff --exit-code -- sdk/go/veilclient.gen.go

build:
	mkdir -p bin
	go build -ldflags "-X main.version=$(VERSION)" -o bin/$(BINARY) ./cmd/veil

# web builds the React SPA into web/dist (embedded into the binary).
.PHONY: web
web:
	cd web && pnpm install --frozen-lockfile && pnpm build

# dist builds a release-style static binary into dist/veil for the current
# GOARCH. Packaging targets consume this artifact.
dist: web
	mkdir -p dist
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o dist/$(BINARY) ./cmd/veil

# package builds .deb, .rpm, and .apk for GOARCH using nfpm. Requires `nfpm`
# (https://nfpm.goreleaser.com) and a prior `make dist`.
package: dist package-deb package-rpm package-apk

package-deb:
	VEIL_VERSION=$(VERSION) VEIL_ARCH=$(GOARCH) VEIL_MAINTAINER="$(MAINTAINER)" \
		nfpm package --config packaging/nfpm.yaml --packager deb --target dist/

package-rpm:
	VEIL_VERSION=$(VERSION) VEIL_ARCH=$(GOARCH) VEIL_MAINTAINER="$(MAINTAINER)" \
		nfpm package --config packaging/nfpm.yaml --packager rpm --target dist/

package-apk:
	VEIL_VERSION=$(VERSION) VEIL_ARCH=$(GOARCH) VEIL_MAINTAINER="$(MAINTAINER)" \
		nfpm package --config packaging/nfpm.yaml --packager apk --target dist/

# sbom writes an SPDX software bill of materials for the module using syft
# (https://github.com/anchore/syft), matching the release workflow.
sbom:
	mkdir -p dist
	syft scan dir:. -o spdx-json=dist/veil.sbom.spdx.json

tidy:
	go mod tidy

release-check:
	go vet ./...
	go test ./... -count=1
	go test -tags e2e ./test/e2e/... -count=1
	make build
	bash -n scripts/install.sh scripts/uninstall.sh
	bash scripts/install.sh --help >/dev/null
	bash scripts/uninstall.sh --help >/dev/null
	git diff --check
	@test -z "$$(git status --short)" || (git status --short && exit 1)

verify-release: release-check verify-openapi verify-sdk
	@echo "Release verification passed"

docker:
	docker build -t $(DOCKER_IMAGE):$(VERSION) .
	@echo "Built $(DOCKER_IMAGE):$(VERSION)"

# ---------------------------------------------------------------------------
# Local CI (see docs/development/ci.md)
#
#   make ci-fast    quick pre-commit checks on the host (NOT a full CI)
#   make ci         optional local duplicate: base checks in smolvm + image build
#   make ci-full    optional local duplicate incl. browser, protocol E2E, packages, systemd
#   make ci-pr      optional ci-full on the temporary merge with origin/main
#   make ci-host    diagnostic: run the standard job set directly on the host
#   make ci-job JOB=<job>        one job in a VM
#   make ci-job-host JOB=<job>   one job directly on the host (diagnostic)
#   make ci-stress  race/shuffle stress for historically flaky tests
#   make ci-image   build/refresh the CI OCI images (content-keyed)
#   make ci-clean   remove artifacts, temp VMs/worktrees (images: --images)
#
# Backend: CI_BACKEND=smolvm (default for the optional local duplicate; requires
# KVM). Hosted GitHub CI remains the required gate. CI_BACKEND=docker is an
# explicit diagnostic fallback for hosts without virtualization and is never
# selected automatically.
# Resources: CI_CPUS (4), CI_MEMORY (8GiB), CI_VM_TIMEOUT (5400s).
# Clean run:  CI_CLEAN=1 (dependency caches off; images rebuild only via
#             `make ci-image CI_CLEAN=1`).
# ---------------------------------------------------------------------------
.PHONY: ci-fast ci ci-full ci-pr ci-host ci-job ci-job-host ci-stress ci-image ci-clean

ci-fast:
	bash scripts/ci/fast.sh

ci:
	bash scripts/ci/run-job.sh standard

ci-full:
	bash scripts/ci/run-job.sh full

ci-pr:
	bash scripts/ci/pr-merge.sh

ci-host:
	bash scripts/ci/host-run.sh standard

ci-job:
	@test -n "$(JOB)" || (echo "usage: make ci-job JOB=<frontend|test|lint|browser-e2e|privilege-boundary|e2e|package-smoke|image-build|full|stress>" >&2; exit 1)
	bash scripts/ci/run-job.sh $(JOB)

ci-job-host:
	@test -n "$(JOB)" || (echo "usage: make ci-job-host JOB=<job>" >&2; exit 1)
	bash scripts/ci/host-run.sh $(JOB)

ci-stress:
	bash scripts/ci/run-job.sh stress

ci-image:
	bash scripts/ci/vm-build.sh

ci-clean:
	bash scripts/ci/cleanup.sh $(CI_CLEAN_ARGS)
