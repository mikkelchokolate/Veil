BINARY=veil
VERSION?=dev
DOCKER_IMAGE?=veil-panel/veil

.PHONY: test build tidy docker release-check

test:
	go test ./...

build:
	mkdir -p bin
	go build -ldflags "-X main.version=$(VERSION)" -o bin/$(BINARY) ./cmd/veil

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
