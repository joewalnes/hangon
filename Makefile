BINARY   = hangon
MODULE   = github.com/joewalnes/hangon
VERSION  = $(shell grep 'const version' main.go | cut -d'"' -f2)
GOFLAGS  = -trimpath -ldflags="-s -w"

.PHONY: build install clean test fmt vet check

build:
	go build $(GOFLAGS) -o $(BINARY) .

install:
	go install $(GOFLAGS) .

clean:
	rm -f $(BINARY)
	rm -rf dist/
	go clean

test:
	go test -v ./...

fmt:
	gofmt -s -w .

vet:
	go vet ./...

check: fmt vet test

# Cross-compilation targets
.PHONY: dist dist-darwin-arm64 dist-darwin-amd64 dist-linux-amd64 dist-linux-arm64

dist: dist-darwin-arm64 dist-darwin-amd64 dist-linux-amd64 dist-linux-arm64

dist-darwin-arm64:
	GOOS=darwin GOARCH=arm64 go build $(GOFLAGS) -o dist/$(BINARY)-darwin-arm64 .

dist-darwin-amd64:
	GOOS=darwin GOARCH=amd64 go build $(GOFLAGS) -o dist/$(BINARY)-darwin-amd64 .

dist-linux-amd64:
	GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -o dist/$(BINARY)-linux-amd64 .

dist-linux-arm64:
	GOOS=linux GOARCH=arm64 go build $(GOFLAGS) -o dist/$(BINARY)-linux-arm64 .
