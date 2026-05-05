BINARY=veil
VERSION?=dev
DOCKER_IMAGE?=veil-panel/veil

.PHONY: test build tidy docker

test:
	go test ./...

build:
	mkdir -p bin
	go build -ldflags "-X main.version=$(VERSION)" -o bin/$(BINARY) ./cmd/veil

tidy:
	go mod tidy

docker:
	docker build -t $(DOCKER_IMAGE):$(VERSION) .
	@echo "Built $(DOCKER_IMAGE):$(VERSION)"
