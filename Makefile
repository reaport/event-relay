LOCAL_BIN:=$(CURDIR)/bin
PATH:=$(PATH):$(LOCAL_BIN)

LINT_VERSION := v1.64.5

download-golangci-lint:
	GOBIN=$(LOCAL_BIN) go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(LINT_VERSION)

lint: download-golangci-lint
	$(LOCAL_BIN)/golangci-lint run --fix

.PHONY: download-golangci-lint lint