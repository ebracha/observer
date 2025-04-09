.PHONY: build run clean image dist test

# Variables
BINARY_NAME=observer
DIST_DIR=dist
DOCKER_IMAGE=observer

dist:
	@mkdir -p $(DIST_DIR)

build: dist
	@echo "Building binary..."
	@go build -o $(DIST_DIR)/$(BINARY_NAME) main.go
	@echo "Binary built successfully in $(DIST_DIR)/$(BINARY_NAME)"

run: build
	@echo "Running application..."
	@./$(DIST_DIR)/$(BINARY_NAME)

image:
	@echo "Building Docker image..."
	@docker build -t $(DOCKER_IMAGE) .
	@echo "Docker image built successfully"

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	@rm -rf $(DIST_DIR)
	@echo "Clean complete"

# Run tests
test:
	@echo "Running tests..."
	@go test -v ./...
	@echo "Tests completed"

# Default target
all: build
