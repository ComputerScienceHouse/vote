BINARY  := vote
GO      := $(shell which go)
LINT    := $(shell which golangci-lint)

# Source packages (mirrors .github/workflows/go.yaml)
PKGS    := ./database/ ./logging/ ./sse/
SRC_DIRS := database logging sse

ifeq ($(GO),)
$(error Go not found in PATH)
endif

.PHONY: build run test vet fmt fmt-check tidy-check check coverage clean

build:
	$(GO) build -o bin/$(BINARY) .

run:
	$(GO) run .

test:
	$(GO) test -count=1 ./database

vet:
	$(GO) vet $(PKGS)
	$(GO) vet *.go

fmt:
	gofmt -s -w $(SRC_DIRS) *.go

fmt-check:
	@out="$$(gofmt -s -l $(SRC_DIRS) *.go)"; \
	if [ -n "$$out" ]; then \
		echo "Files not gofmt -s formatted:"; \
		echo "$$out"; \
		exit 1; \
	fi

tidy-check:
	$(GO) mod tidy -diff

# Runs the same checks as CI
check: tidy-check fmt-check vet test

coverage:
	$(GO) test -coverprofile=coverage.out ./database
	$(GO) tool cover -html=coverage.out

clean:
	rm -rf bin/ coverage.out
