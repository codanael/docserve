VERSION ?= dev
BINARY  := bin/docserve
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

.PHONY: build test test-integration lint clean

build:
	CGO_ENABLED=0 go build $(LDFLAGS) -o $(BINARY) ./cmd/docserve

test:
	go test ./...

test-integration:
	go test -tags integration ./...

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/
