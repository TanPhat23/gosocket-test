.PHONY: all test test-backend test-frontend lint staticcheck check build clean

all: lint test build

# Run all static analysis gates
lint: staticcheck
	cd web && npm run lint || true

staticcheck:
	@./scripts/staticcheck.sh

# Run all test suites
test: test-backend test-frontend

test-backend:
	go test -v -race ./...

test-frontend:
	npm test --prefix web -- --run

# Comprehensive local quality gate check
check: staticcheck
	go vet ./...
	go test ./...
	npm test --prefix web -- --run
	cd web && npx tsc -b

# Build backend binaries and web assets
build:
	go build -o bin/server ./cmd/server
	go build -o bin/migrate ./cmd/migrate
	npm run build --prefix web

clean:
	rm -rf bin/ dist/ web/dist/
