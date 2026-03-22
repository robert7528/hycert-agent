VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X 'main.version=$(VERSION)' \
	-X 'main.commit=$(COMMIT)' \
	-X 'main.date=$(DATE)'

.PHONY: build build-linux build-windows clean

build:
	go build -ldflags "$(LDFLAGS)" -o bin/hycert-agent ./cmd/hycert-agent

build-linux:
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/hycert-agent-linux-amd64 ./cmd/hycert-agent

build-windows:
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/hycert-agent-windows-amd64.exe ./cmd/hycert-agent

build-all: build-linux build-windows

clean:
	rm -rf bin/
