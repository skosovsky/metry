GO      := go
FUZZTIME ?= 30s
FUZZPARALLEL ?= 2
MODULES := $(shell find . -type d \( -name ".*" -not -name "." -o -name "vendor" \) -prune -o -type f -name "go.mod" -exec dirname {} \;)
# Consumer fixtures are tested and linted but are not published modules.
RELEASE_MODULES := $(filter-out ./integration/%,$(MODULES))

.PHONY: lint fix test test-examples test-release bench bench-hotpath fuzz cover release-patch release-break

lint:
	@for dir in $(MODULES); do \
		echo "golangci-lint - $$dir"; \
		(cd "$$dir" && golangci-lint run ./...) || exit 1; \
	done

fix:
	@if [ -f "go.work" ]; then $(GO) work sync; fi
	@for dir in $(MODULES); do \
		echo "fix & tidy - $$dir"; \
		(cd "$$dir" && $(GO) fix ./... && $(GO) mod tidy) || exit 1; \
		(cd "$$dir" && golangci-lint run --fix ./...) || exit 1; \
	done

test:
	@for dir in $(MODULES); do \
		echo "test - $$dir"; \
		(cd "$$dir" && $(GO) test -v -race ./...) || exit 1; \
	done

test-examples:
	@for d in examples/enrich examples/propagation_map examples/async_handle examples/metrics_registry examples/scope examples/executor examples/genai_recorder examples/genai_standalone examples/agent_workflow; do \
		echo "test-example - $$d"; \
		(cd "$$d" && $(GO) run .) || exit 1; \
	done

bench:
	@for dir in $(MODULES); do \
		echo "bench - $$dir"; \
		(cd "$$dir" && $(GO) test -bench=. -run=^$$ ./...) || exit 1; \
	done

fuzz:
	@for dir in $(MODULES); do \
		echo "fuzz - $$dir"; \
		(cd "$$dir" && \
			for pkg in $$($(GO) list -tags=fuzz ./...); do \
				targets=$$($(GO) test -tags=fuzz -list '^Fuzz' "$$pkg") || exit 1; \
				for target in $$(printf '%s\n' "$$targets" | sed -n '/^Fuzz[A-Za-z0-9_]*$$/p'); do \
					echo "fuzz - $$pkg/$$target"; \
					$(GO) test -tags=fuzz -run='^$$' -fuzz="^$$target$$" -fuzztime=$(FUZZTIME) -parallel=$(FUZZPARALLEL) "$$pkg" || exit 1; \
				done; \
			done \
		) || exit 1; \
	done

cover:
	@for dir in $(MODULES); do \
		echo "cover - $$dir"; \
		(cd "$$dir" && $(GO) test -coverprofile=coverage.out ./... && $(GO) tool cover -func=coverage.out) || exit 1; \
	done

test-release:
	@bash scripts/release_test.sh

release-patch: lint test test-release ## v0.5.0 -> v0.5.1
	@chmod +x ./scripts/release.sh
	@./scripts/release.sh patch "$(RELEASE_MODULES)"

release-break: lint test test-release ## v0.5.1 -> v0.6.0
	@chmod +x ./scripts/release.sh
	@./scripts/release.sh break "$(RELEASE_MODULES)"
