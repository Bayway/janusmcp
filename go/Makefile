BINARY      := janusmcp
PKG         := ./cmd/janusmcp
BIN_DIR     := bin
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS     := -s -w -X main.version=$(VERSION)

.PHONY: all build test vet tidy run clean install build-all

all: build

build:
	@mkdir -p $(BIN_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(PKG)

test:
	go test ./... -count=1

vet:
	go vet ./...

tidy:
	go mod tidy

run: build
	./$(BIN_DIR)/$(BINARY) serve

install: build
	install -m 0755 $(BIN_DIR)/$(BINARY) $${PREFIX:-/usr/local/bin}/$(BINARY)

clean:
	rm -rf $(BIN_DIR)

# Cross-compile release binaries.
build-all:
	@mkdir -p $(BIN_DIR)
	@for t in darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64; do \
		os=$${t%/*}; arch=$${t#*/}; ext=""; [ "$$os" = windows ] && ext=".exe"; \
		echo "→ $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch go build -ldflags "$(LDFLAGS)" \
			-o $(BIN_DIR)/$(BINARY)-$$os-$$arch$$ext $(PKG); \
	done
