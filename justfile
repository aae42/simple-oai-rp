# This list
default:
  just --list --unsorted

# Build
build:
  go build -o simple-oai-rp main.go

# Run
run:
  SIMPLE_DATA_PATH=./data go run main.go

# Install dependencies
install:
  go mod download
  go mod tidy

# Clean build artifacts
clean:
  @echo "Cleaning..."
  rm -f simple-oai-rp
  rm -rf data/

# Run tests
test:
  @echo "Running tests..."
  go test -v ./...

# Format code
fmt:
  @echo "Formatting code..."
  go fmt ./...

# Lint code (requires golangci-lint)
lint:
  @echo "Linting code..."
  @if command -v golangci-lint > /dev/null; then \
    golangci-lint run; \
  else \
    echo "golangci-lint not found. Install from: https://golangci-lint.run/usage/install/"; \
  fi

admin-user-list:
  curl http://localhost:8081/admin/users/list \
    -H "Authorization: Bearer $ADMIN_KEY" \
    -H "Content-Type: application/json"

admin-user-create USER:
  curl -X POST http://localhost:8081/admin/users \
    -H "Authorization: Bearer $ADMIN_KEY" \
    -H "Content-Type: application/json" \
    -d '{"username": "{{USER}}"}'
