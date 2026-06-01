BINARY=veil
VERSION?=dev
DOCKER_IMAGE?=veil-panel/veil
GOARCH?=$(shell go env GOARCH)
MAINTAINER?=Veil Maintainers <veil@users.noreply.github.com>

.PHONY: test build tidy docker release-check dist package package-deb package-rpm package-apk sbom

test:
	go test ./...

build:
	mkdir -p bin
	go build -ldflags "-X main.version=$(VERSION)" -o bin/$(BINARY) ./cmd/veil

# dist builds a release-style static binary into dist/veil for the current
# GOARCH. Packaging targets consume this artifact.
dist:
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
	make build
	bash -n scripts/install.sh scripts/uninstall.sh
	bash scripts/install.sh --help >/dev/null
	bash scripts/uninstall.sh --help >/dev/null
	git diff --check
	@test -z "$$(git status --short)" || (git status --short && exit 1)

docker:
	docker build -t $(DOCKER_IMAGE):$(VERSION) .
	@echo "Built $(DOCKER_IMAGE):$(VERSION)"
