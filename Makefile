BINARY_NAME   := tinydm
BUILD_DIR     := ./bin
CMD_DIR       := ./cmd/tinydm
GO            := go
GOFLAGS       := -trimpath
LDFLAGS       := -s -w

.PHONY: all build build-all test lint clean run sqlc migrate docker-build docker-run

all: build

## build: compile for the current platform
build:
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_DIR)

## build-all: cross-compile for all target platforms
build-all:
	@mkdir -p $(BUILD_DIR)
	GOOS=linux   GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64   $(CMD_DIR)
	GOOS=linux   GOARCH=arm64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64   $(CMD_DIR)
	GOOS=darwin  GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64  $(CMD_DIR)
	GOOS=darwin  GOARCH=arm64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64  $(CMD_DIR)
	GOOS=windows GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe $(CMD_DIR)

## test: run all tests with race detector
test:
	$(GO) test ./... -v -race -timeout 60s

## lint: run golangci-lint (install: https://golangci-lint.run/usage/install/)
lint:
	golangci-lint run ./...

## run: run the server locally
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
	docker build -t $(BINARY_NAME):latest .

## docker-run: run the Docker image with a local data volume
docker-run:
	docker run --rm -p 8080:8080 -v "$(PWD)/data:/data" $(BINARY_NAME):latest

## help: list available targets
help:
	@grep -E '^## ' Makefile | sed 's/## /  /'
