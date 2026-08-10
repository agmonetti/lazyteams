VERSION ?= dev
LDFLAGS := -X lazyteams/internal/version.Version=$(VERSION)

.PHONY: build release test

build: ## Build the TUI and auth-helper with the injected version
	go build -ldflags "$(LDFLAGS)" -o lazyteams .
	go build -ldflags "$(LDFLAGS)" -o lazyteams-auth ./cmd/auth-helper

release: ## Confirm, tag and push a release (make release VERSION=v1.2.3) — CI builds and publishes it
	@test "$(VERSION)" != "dev" || (echo "Usage: make release VERSION=v1.2.3" && exit 1)
	@echo "About to tag and push $(VERSION) — Ctrl-C to abort"
	@sleep 3
	@git tag -a $(VERSION) -m "$(VERSION)"
	@git push origin $(VERSION)
	@echo "Tag $(VERSION) pushed — the Release workflow will build and publish it."

test: ## Run the full test suite
	go test ./...
