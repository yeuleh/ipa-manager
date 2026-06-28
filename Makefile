.PHONY: build run test vet fmt tidy lint clean

BIN := bin/ipa-manager
VERSION ?= dev

build:
	go build -ldflags "-X main.Version=$(VERSION)" -o $(BIN) ./cmd/ipa-manager

run:
	go run ./cmd/ipa-manager

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -s -w .

tidy:
	go mod tidy

lint:
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not installed; skipping"; exit 0; }
	golangci-lint run

clean:
	rm -rf bin
