APP_NAME = fiber-api
BUILD_DIR = ./bin
MAIN_FILE = ./cmd/server/main.go

.PHONY: all build run dev test clean tidy lint migrate seed docker-up docker-down

all: tidy build

## Build production binary
build:
	@echo "Building $(APP_NAME)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o $(BUILD_DIR)/$(APP_NAME) $(MAIN_FILE)
	@echo "Build complete: $(BUILD_DIR)/$(APP_NAME)"

## Run in production mode
run: build
	$(BUILD_DIR)/$(APP_NAME)

## Run with hot-reload (requires air: go install github.com/air-verse/air@latest)
dev:
	air

## Run tests
test:
	go test -v -cover ./...

## Run tests with race detector
test-race:
	go test -race -v ./...

## Clean build artifacts
clean:
	@rm -rf $(BUILD_DIR)
	@echo "Cleaned build directory"

## Download and tidy dependencies
tidy:
	go mod tidy
	go mod download

## Run go vet + staticcheck
lint:
	go vet ./...

## Format code
fmt:
	gofmt -s -w .

## Generate migration files (requires golang-migrate)
migrate-up:
	migrate -path ./database/migrations -database "mysql://$(DB_USER_JO):$(DB_PASS_JO)@tcp($(DB_HOST_JO):$(DB_PORT_JO))/$(DB_NAME_JO)" up

migrate-down:
	migrate -path ./database/migrations -database "mysql://$(DB_USER_JO):$(DB_PASS_JO)@tcp($(DB_HOST_JO):$(DB_PORT_JO))/$(DB_NAME_JO)" down 1

## Docker commands
docker-build:
	docker build -t $(APP_NAME):latest .

docker-up:
	docker-compose up -d

docker-down:
	docker-compose down

## Install development tools
tools:
	go install github.com/air-verse/air@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

## Show help
help:
	@echo "Available targets:"
	@echo "  build       - Build production binary"
	@echo "  run         - Build and run"
	@echo "  dev         - Run with hot-reload (requires air)"
	@echo "  test        - Run tests"
	@echo "  clean       - Clean build artifacts"
	@echo "  tidy        - Download and tidy dependencies"
	@echo "  lint        - Run linters"
	@echo "  fmt         - Format code"
	@echo "  docker-up   - Start docker containers"
	@echo "  docker-down - Stop docker containers"
	@echo "  tools       - Install development tools"
