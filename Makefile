BINARY_NAME   := tinydm
BUILD_DIR     := ./bin
CMD_DIR       := ./cmd/tinydm
GO            := go
GOFLAGS       := -trimpath

# Version metadata — baked into the binary at link time.
VERSION       := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT        := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS       := -s -w \
                 -X main.version=$(VERSION) \
                 -X main.commit=$(COMMIT) \
                 -X main.buildDate=$(BUILD_DATE)

.PHONY: all build build-all dist test bench lint clean run sqlc docker-build docker-run help

all: build

## build: compile for the current platform
build:
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_DIR)

## build-all: cross-compile for all target platforms
build-all:
	@mkdir -p $(BUILD_DIR)
	GOOS=linux   GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64     $(CMD_DIR)
	GOOS=linux   GOARCH=arm64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64     $(CMD_DIR)
	GOOS=darwin  GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64   $(CMD_DIR)
	GOOS=darwin  GOARCH=arm64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64   $(CMD_DIR)
	GOOS=windows GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe $(CMD_DIR)
	GOOS=windows GOARCH=arm64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-windows-arm64.exe $(CMD_DIR)
	@echo "Build complete — binaries in $(BUILD_DIR):"
	@ls -lh $(BUILD_DIR)/$(BINARY_NAME)-*

## dist: build all platforms and package into compressed archives
dist: build-all
	@mkdir -p $(BUILD_DIR)/dist
	@echo "Packaging archives..."
	tar -czf $(BUILD_DIR)/dist/$(BINARY_NAME)-$(VERSION)-linux-amd64.tar.gz   -C $(BUILD_DIR) $(BINARY_NAME)-linux-amd64
	tar -czf $(BUILD_DIR)/dist/$(BINARY_NAME)-$(VERSION)-linux-arm64.tar.gz   -C $(BUILD_DIR) $(BINARY_NAME)-linux-arm64
	tar -czf $(BUILD_DIR)/dist/$(BINARY_NAME)-$(VERSION)-darwin-amd64.tar.gz  -C $(BUILD_DIR) $(BINARY_NAME)-darwin-amd64
	tar -czf $(BUILD_DIR)/dist/$(BINARY_NAME)-$(VERSION)-darwin-arm64.tar.gz  -C $(BUILD_DIR) $(BINARY_NAME)-darwin-arm64
	cd $(BUILD_DIR) && zip -q dist/$(BINARY_NAME)-$(VERSION)-windows-amd64.zip $(BINARY_NAME)-windows-amd64.exe
	cd $(BUILD_DIR) && zip -q dist/$(BINARY_NAME)-$(VERSION)-windows-arm64.zip $(BINARY_NAME)-windows-arm64.exe
	@echo "Distribution archives in $(BUILD_DIR)/dist:"
	@ls -lh $(BUILD_DIR)/dist/

## test: run all tests with race detector
test:
	$(GO) test ./... -race -timeout 60s

## bench: run all benchmarks (no tests) and report allocations
bench:
	$(GO) test ./... -bench=. -benchmem -benchtime=3s -run='^$$'

## lint: run golangci-lint (install: https://golangci-lint.run/usage/install/)
lint:
	golangci-lint run ./...

## run: run the server locally (requires TINYDM_JWT_SECRET)
run:
	$(GO) run $(CMD_DIR)

## sqlc: generate type-safe Go from SQL queries
sqlc:
	sqlc generate

## clean: remove build artefacts and local databases
clean:
	rm -rf $(BUILD_DIR)
	find . -name "*.db" -not -path "./.git/*" -delete

## docker-build: build the Docker image
docker-build:
	docker build -t $(BINARY_NAME):$(VERSION) -t $(BINARY_NAME):latest .

## docker-run: run the Docker image with a local data volume
docker-run:
	docker run --rm -p 8080:8080 \
		-e TINYDM_JWT_SECRET=changeme \
		-v "$(PWD)/data:/data" \
		$(BINARY_NAME):latest

## help: list available targets
help:
	@grep -E '^## ' Makefile | sed 's/## /  /'
