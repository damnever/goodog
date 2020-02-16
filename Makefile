.PHONY: help build test deps clean

# Ref: https://gist.github.com/prwhite/8168133
help:  ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n\nTargets:\n"} \
		/^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
NOW = $(shell date '+%Y-%m-%d')
REV = $(shell git rev-parse --short HEAD || echo unknown)
LDFLAGS = -ldflags '-X github.com/damnever/goodog.GitRev=$(REV) \
		-X github.com/damnever/goodog.BuildDate=$(NOW)'


build:  ## Build executable files. (Args: GOOS=$(go env GOOS) GOARCH=$(go env GOARCH))
	env GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o 'bin/goodog-frontend' $(LDFLAGS) ./cmd/frontend/
	env GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o 'bin/goodog-backend-caddy' $(LDFLAGS) ./cmd/backend/caddy/


GOLANGCI_LINT_VERSION ?= "latest"

test:  ## Run test cases. (Args: GOLANGCI_LINT_VERSION=latest)
	GOLANGCI_LINT_CMD=golangci-lint; \
	if [[ ! -x $$(command -v golangci-lint) ]]; then \
		if [[ ! -e ./bin/golangci-lint ]]; then \
			curl -sfL https://install.goreleaser.com/github.com/golangci/golangci-lint.sh | sh -s $(GOLANGCI_LINT_VERSION); \
		fi; \
		GOLANGCI_LINT_CMD=./bin/golangci-lint; \
	fi; \
    	$${GOLANGCI_LINT_CMD} run .
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out  # -o coverage.html


deps: export GOPRIVATE=github.com/damnever/libext-go  # DO NOT use quotes

deps: ## Update dependencies.
	go mod verify
	go mod tidy -v
	# rm -rf vendor
	# go mod vendor -v
	go get ./...


clean:  ## Clean up useless files.
	rm -rf bin
	find . -type f -name '*.out' -exec rm -f {} +
	find . -type f -name '.DS_Store' -exec rm -f {} +
	find . -type f -name '*.test' -exec rm -f {} +
	find . -type f -name '*.prof' -exec rm -f {} +
