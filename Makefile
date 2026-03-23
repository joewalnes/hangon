BINARY   = hangon
MODULE   = github.com/joewalnes/hangon
VERSION  = $(shell grep 'const version' main.go | cut -d'"' -f2)
GOFLAGS  = -trimpath -ldflags="-s -w"

.PHONY: all build install clean test e2e fmt vet check

all: check build

build:
	go build $(GOFLAGS) -o $(BINARY) .

install:
	go install $(GOFLAGS) .

build-ghostty: ghostty/lib/libghostty-vt.a
	go build $(GOFLAGS) -tags ghostty -o $(BINARY) .

install-ghostty: ghostty/lib/libghostty-vt.a
	go install $(GOFLAGS) -tags ghostty .

clean:
	rm -f $(BINARY) $(BINARY)-*
	rm -rf dist/
	go clean

test:
	go test -v ./...

# --- libghostty build ---
# Requires: zig (>= 0.13), git
# Fetches and builds libghostty-vt from the Ghostty repository.

GHOSTTY_REPO    = https://github.com/ghostty-org/ghostty.git
GHOSTTY_DIR     = ghostty/src
GHOSTTY_LIB_DIR = ghostty/lib
GHOSTTY_INC_DIR = ghostty/include

.PHONY: ghostty ghostty-clean

ghostty: ghostty/lib/libghostty-vt.a

ghostty/lib/libghostty-vt.a: $(GHOSTTY_DIR)/build.zig
	@echo "Building libghostty-vt..."
	@mkdir -p $(GHOSTTY_LIB_DIR) $(GHOSTTY_INC_DIR)
	cd $(GHOSTTY_DIR) && zig build lib-vt -Doptimize=ReleaseFast
	@# Copy the built library and headers to our expected locations.
	@cp $(GHOSTTY_DIR)/zig-out/lib/libghostty-vt.a $(GHOSTTY_LIB_DIR)/ 2>/dev/null || \
		cp $(GHOSTTY_DIR)/zig-out/lib/libghostty_vt.a $(GHOSTTY_LIB_DIR)/libghostty-vt.a 2>/dev/null || \
		(echo "Could not find built library. Check $(GHOSTTY_DIR)/zig-out/lib/" && exit 1)
	@cp $(GHOSTTY_DIR)/zig-out/include/ghostty.h $(GHOSTTY_INC_DIR)/ 2>/dev/null || \
		cp $(GHOSTTY_DIR)/include/ghostty.h $(GHOSTTY_INC_DIR)/ 2>/dev/null || \
		(echo "Could not find ghostty.h header. Check $(GHOSTTY_DIR)/" && exit 1)
	@echo "libghostty-vt built successfully."

$(GHOSTTY_DIR)/build.zig:
	@echo "Fetching Ghostty source..."
	@mkdir -p ghostty
	git clone --depth 1 $(GHOSTTY_REPO) $(GHOSTTY_DIR)

ghostty-clean:
	rm -rf ghostty/

e2e:
	@bash test/e2e.sh

fmt:
	gofmt -s -w .

vet:
	go vet ./...

check: fmt vet test e2e

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
