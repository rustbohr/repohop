BINARY := repohop
PKG    := ./cmd/repohop
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test vet fmt lint tidy clean install run

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

tidy:
	go mod tidy

run: build
	./$(BINARY)

clean:
	rm -f $(BINARY) $(BINARY).exe
	rm -rf dist
