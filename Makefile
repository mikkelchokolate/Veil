BINARY=veil
VERSION?=dev

.PHONY: test build tidy

test:
	go test ./...

build:
	mkdir -p bin
	go build -ldflags "-X main.version=$(VERSION)" -o bin/$(BINARY) ./cmd/veil

tidy:
	go mod tidy
