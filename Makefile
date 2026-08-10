VERSION ?= dev
LDFLAGS := -X lazyteams/internal/version.Version=$(VERSION)

.PHONY: build release test

build: ## Build the TUI and auth-helper with the injected version
	go build -ldflags "$(LDFLAGS)" -o lazyteams .
	go build -ldflags "$(LDFLAGS)" -o lazyteams-auth ./cmd/auth-helper

release: ## Tag and cross-compile binaries (make release VERSION=v1.2.3)
	@test "$(VERSION)" != "dev" || (echo "Usage: make release VERSION=v1.2.3" && exit 1)
	@git tag $(VERSION) && git push origin $(VERSION)
	@mkdir -p dist
	@for t in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64; do \
		os=$${t%/*}; arch=$${t#*/}; \
		ext=""; [ "$$os" = "windows" ] && ext=".exe"; \
		GOOS=$$os GOARCH=$$arch go build -ldflags "-X lazyteams/internal/version.Version=$(VERSION)" -o dist/lazyteams-$$os-$$arch$$ext .; \
		GOOS=$$os GOARCH=$$arch go build -ldflags "-X lazyteams/internal/version.Version=$(VERSION)" -o dist/lazyteams-auth-$$os-$$arch$$ext ./cmd/auth-helper; \
	done
	@echo "Release $(VERSION) built in dist/"

test: ## Run the full test suite
	go test ./...
