BINARY  := vote
GO      := $(shell which go)
GOFMT  := $(shell which gofmt)

# Source packages (mirrors .github/workflows/go.yaml)
PKGS    := ./database/ ./logging/ ./sse/

ifeq ($(GO),)
$(error Go not found in PATH)
endif

.PHONY: build test vet lint tidy-check check coverage clean

build:
	$(GO) build -o bin/$(BINARY) .

run:
	docker-compose up

test:
	$(GO) test -count=1 ./database

vet:
	$(GO) vet $(PKGS)
	$(GO) vet *.go

lint:
	$(GOFMT) -w -s *.go logging sse database

tidy-check:
	$(GO) mod tidy -diff

# Runs the same checks as CI
check: tidy-check vet test

coverage:
	$(GO) test -coverprofile=coverage.out ./database
	$(GO) tool cover -html=coverage.out

clean:
	rm -rf bin/ coverage.out
