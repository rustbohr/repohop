BINARY := repohop
PKG    := ./cmd/repohop
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test vet fmt lint tidy clean install run check clean-check

build:
	go build -ldflags '$(LDFLAGS)' -o $(BINARY) $(PKG)

install:
	go install -ldflags '$(LDFLAGS)' $(PKG)

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

lint: vet
	staticcheck ./... || true

# Everything CI checks, before pushing.
check: build vet test clean-check
	@test -z "$$(gofmt -l .)" || { echo "not gofmt-clean:"; gofmt -l .; exit 1; }

clean-check:
	@./scripts/check-clean.sh

tidy:
	go mod tidy

run: build
	./$(BINARY)

clean:
	rm -f $(BINARY) $(BINARY).exe
	rm -rf dist
