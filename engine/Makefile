# SCV engine — Go control plane over the Python perception tool.
# Delegated from the root Makefile as `make engine-build`,
# `make engine-test`, and `make engine-test-integration`.

GO ?= go
COVER_PROFILE := cover.out

.PHONY: build test test-integration cover

build: ## compile all engine packages
	$(GO) build ./...

test: ## unit tests: race detector + >=80% coverage gate on internal/ packages
	$(GO) test ./... -race -coverprofile=$(COVER_PROFILE) && $(GO) tool cover -func=$(COVER_PROFILE) | awk '$$3+0 < 80 && $$1 ~ /internal/ {print; f=1} END {exit f}'

test-integration: ## integration tests: real scv + perceive.py over perception fixtures (build tag `integration`; see tests/README.md)
	$(GO) test -tags=integration ./tests/ -v -timeout 20m

cover: ## run unit tests and write cover.out plus an HTML report
	$(GO) test ./... -race -coverprofile=$(COVER_PROFILE)
	$(GO) tool cover -html=$(COVER_PROFILE) -o cover.html
