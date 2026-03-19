GO_ENV = GOCACHE=/tmp/metry-go-build-cache
LINT_ENV = GOCACHE=/tmp/metry-go-build-cache GOLANGCI_LINT_CACHE=/tmp/metry-golangci-lint-cache

.PHONY: test test-root test-grpc lint lint-root lint-grpc fmt fmt-root fmt-grpc tidy cover test-race test-race-root test-race-grpc

test:
	@$(GO_ENV) go test ./...
	@cd middleware/grpc && $(GO_ENV) go test ./...

test-root:
	@$(GO_ENV) go test ./...

test-grpc:
	@cd middleware/grpc && $(GO_ENV) go test ./...

lint:
	@$(LINT_ENV) golangci-lint run ./...
	@cd middleware/grpc && $(LINT_ENV) golangci-lint run ./...

lint-root:
	@$(LINT_ENV) golangci-lint run ./...

lint-grpc:
	@cd middleware/grpc && $(LINT_ENV) golangci-lint run ./...

fmt: fmt-root fmt-grpc

fmt-root:
	@gofmt -s -w .
	@goimports -local github.com/skosovsky/metry -w .

fmt-grpc:
	@cd middleware/grpc && gofmt -s -w .
	@cd middleware/grpc && goimports -local github.com/skosovsky/metry -w .

tidy:
	@go mod tidy
	@cd middleware/grpc && go mod tidy
	@go work sync

cover:
	@$(GO_ENV) go test -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o coverage.html

test-race:
	@$(GO_ENV) go test -race ./...
	@cd middleware/grpc && $(GO_ENV) go test -race ./...

test-race-root:
	@$(GO_ENV) go test -race ./...

test-race-grpc:
	@cd middleware/grpc && $(GO_ENV) go test -race ./...
