.PHONY: test lint fmt tidy cover test-race

test:
	@go test ./...

lint:
	@golangci-lint run

fmt:
	@gofmt -s -w .
	@goimports -w .

tidy:
	@go mod tidy

cover:
	@go test -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o coverage.html

test-race:
	@go test -race ./...
