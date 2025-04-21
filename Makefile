.PHONY: build run clean image dist test env debug

# Variables
BINARY_NAME=observer
DIST_DIR=dist
DOCKER_IMAGE=observer

# Environment variables
ENV_FILE := .env
ifneq (,$(wildcard $(ENV_FILE)))
    include $(ENV_FILE)
    export
endif

dist:
	@mkdir -p $(DIST_DIR)

build: env
	@echo "Building binary..."
	@go build -o $(DIST_DIR)/$(BINARY_NAME) main.go
	@echo "Binary built successfully in $(DIST_DIR)/$(BINARY_NAME)"

run: env
	@echo "Running application with environment variables..."
	@INFLUXDB_URL=$(INFLUXDB_URL) \
	INFLUXDB_TOKEN=$(INFLUXDB_TOKEN) \
	INFLUXDB_ORG=$(INFLUXDB_ORG) \
	INFLUXDB_BUCKET=$(INFLUXDB_BUCKET) \
	REDIS_HOST=$(REDIS_HOST) \
	REDIS_PORT=$(REDIS_PORT) \
	REDIS_PASSWORD=$(REDIS_PASSWORD) \
	go run main.go

image:
	@echo "Building Docker image..."
	@docker build -t $(DOCKER_IMAGE) .
	@echo "Docker image built successfully"

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	@rm -rf $(DIST_DIR)
	@echo "Clean complete"

# Run tests with environment variables
test: env
	@echo "Running tests with environment variables..."
	@go test -v ./...
	@echo "Tests completed"

# Set environment variables
env:
	@echo "Setting environment variables from .env file..."
	@export $(cat $(ENV_FILE) | xargs)
	@echo "Environment variables set:"
	@echo "REDIS_HOST: $(REDIS_HOST)"
	@echo "REDIS_PORT: $(REDIS_PORT)"
	@echo "INFLUXDB_URL: $(INFLUXDB_URL)"
	@echo "INFLUXDB_ORG: $(INFLUXDB_ORG)"
	@echo "INFLUXDB_BUCKET: $(INFLUXDB_BUCKET)"
	@echo "INFLUXDB_TOKEN: $(INFLUXDB_TOKEN)"

debug:
	@echo "Debugging environment variables..."
	@echo "Current environment:"
	@env | grep -E "REDIS|INFLUXDB"
	@echo "Checking InfluxDB connection..."
	@curl -s $(INFLUXDB_URL)/health || echo "Failed to connect to InfluxDB"

# Default target
all: build
