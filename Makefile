VERSION ?= dev
LDFLAGS := -X lazyteams/internal/version.Version=$(VERSION)

PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

# Recipes use bash for `set -o pipefail` and the ERR trap.
SHELL := /bin/bash

.PHONY: build dist release test

build: ## Build the TUI and auth-helper with the injected version
	go build -ldflags "$(LDFLAGS)" -o lazyteams .
	go build -ldflags "$(LDFLAGS)" -o lazyteams-auth ./cmd/auth-helper

dist: ## Cross-compile all release binaries and write dist/SHA256SUMS
	@rm -rf dist && mkdir -p dist
	@set -e; for t in $(PLATFORMS); do \
	    os=$${t%/*}; arch=$${t#*/}; ext=""; \
	    [ "$$os" = "windows" ] && ext=".exe"; \
	    GOOS=$$os GOARCH=$$arch go build -ldflags "$(LDFLAGS)" -o "dist/lazyteams-$$os-$$arch$$ext" .; \
	    GOOS=$$os GOARCH=$$arch go build -ldflags "$(LDFLAGS)" -o "dist/lazyteams-auth-$$os-$$arch$$ext" ./cmd/auth-helper; \
	done
	@cd dist && sha256sum lazyteams-* > SHA256SUMS && cat SHA256SUMS

release: ## Validate, confirm and tag a release; CI builds and publishes it (make release VERSION=v1.2.3)
	@set -euo pipefail; \
	V="$(VERSION)"; \
	if ! echo "$$V" | grep -qE '^v[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.]+)?$$'; then \
	    echo "Error: La versión debe tener el formato vX.Y.Z (ej. v1.2.3, v1.2.3-beta.1)" >&2; exit 1; \
	fi; \
	if [ -n "$$(git status --porcelain)" ]; then \
	    echo "Error: Hay cambios sin commitear en el repositorio:" >&2; \
	    git status --porcelain >&2; \
	    echo "Por favor, haz commit o stash antes de continuar." >&2; \
	    exit 1; \
	fi; \
	if git tag -l "$$V" | grep -qx "$$V"; then \
	    echo "Error: El tag $$V ya existe localmente." >&2; exit 1; \
	fi; \
	if git ls-remote --exit-code --tags origin "refs/tags/$$V" >/dev/null 2>&1; then \
	    echo "Error: El tag $$V ya existe en origin." >&2; exit 1; \
	fi; \
	BR=$$(git branch --show-current | tr -d '[:space:]'); \
	if [ -z "$$BR" ]; then \
	    echo "Error: No se pudo determinar la rama actual (¿estás en estado HEAD separado?)." >&2; exit 1; \
	fi; \
	[ "$$BR" = "main" ] || echo "Aviso: liberando desde $$BR, no main."; \
	read -r -p "Taggear y empujar $$V desde $$BR (y/n)? " ans; \
	case "$$ans" in y|Y|yes|YES|si|Si|SI) ;; *) echo "Operación cancelada."; exit 0;; esac; \
	git tag -a "$$V" -m "$$V"; \
	trap 'echo "Error en git; borrando tag local $$V"; git tag -d "$$V" >/dev/null 2>&1 || true' ERR; \
	git push origin "$$BR"; \
	git push origin "$$V"; \
	trap - ERR; \
	echo "Tag $$V empujado — el workflow Release lo construirá y publicará."

test: ## Run the full test suite
	go test ./...