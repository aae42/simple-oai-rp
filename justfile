# This list
default:
  just --list --unsorted

set dotenv-load := true

# Build
build:
  mkdir -p build/linux/amd64
  mkdir -p build/macos/arm64
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o build/linux/amd64/simple-oai-rp main.go
  CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o build/macos/arm64/simple-oai-rp main.go

# Run (with migrations)
run:
  @mkdir -p ./data
  just dbm-up
  SIMPLE_DATA_PATH=./data go run main.go

# Install dependencies
install:
  go mod download
  go mod tidy

# Clean build artifacts
clean:
  @echo "Cleaning..."
  rm -rf build/
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

export DATABASE_URL := "sqlite:./data/main.db"

# Create a new migration file
dbm-new NAME:
  dbmate new {{NAME}}

# Run all migrations
dbm-up:
  mkdir -p data
  dbmate up

# Rollback the last migration
dbm-rollback:
  dbmate rollback

# Show migration status
dbm-status:
  dbmate status

# Dump the schema to db/schema.sql
dbm-dump:
  dbmate dump

# SERVER INTERACTION: list users
admin-user-list:
  curl $SERVER_URL/admin/users/list \
    -H "Authorization: Bearer $ADMIN_KEY" \
    -H "Content-Type: application/json"

# SERVER INTERACTION: show user activity
admin-user-activity:
  curl $SERVER_URL/admin/users/activity \
    -H "Authorization: Bearer $ADMIN_KEY" \
    -H "Content-Type: application/json"

# SERVER INTERACTION: create an API user
admin-user-create USER:
  curl -X POST $SERVER_URL/admin/users \
    -H "Authorization: Bearer $ADMIN_KEY" \
    -H "Content-Type: application/json" \
    -d '{"username": "{{USER}}"}'

# SERVER INTERACTION: test user interaction with server
user-chat *CHAT:
  curl $SERVER_URL/v1/chat/completions \
    -H "Authorization: Bearer $USER_KEY" \
    -H "Content-Type: application/json" \
    -d '{"messages": [{"role": "user", "content": "{{CHAT}}"}]}'
