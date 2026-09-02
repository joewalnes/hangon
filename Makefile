BINARY   = hangon
MODULE   = github.com/joewalnes/hangon
VERSION  = $(shell date -u +'%Y-%m-%d %H:%M') $(shell git rev-parse --abbrev-ref HEAD) $(shell git rev-parse --short HEAD) dev
GOFLAGS  = -trimpath -ldflags="-s -w -X 'main.version=$(VERSION)'"

.PHONY: all build install clean test e2e fmt fmt-check vet check

all: check build

build:
	go build $(GOFLAGS) -o $(BINARY) .

install:
	go install $(GOFLAGS) .

clean:
	rm -f $(BINARY) $(BINARY)-*
	rm -rf dist/
	go clean

test:
	go test -v ./...

e2e:
	@bash test/e2e.sh

fmt:
	gofmt -s -w .

# Fails (instead of rewriting) when files need formatting, so check/CI
# report drift rather than silently self-healing it.
fmt-check:
	@out="$$(gofmt -s -l .)"; if [ -n "$$out" ]; then echo "gofmt: needs formatting (run 'make fmt'):"; echo "$$out"; exit 1; fi

vet:
	go vet ./...

check: fmt-check vet test e2e

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
