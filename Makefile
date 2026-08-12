.DEFAULT_GOAL := help

APP        := app
BIN        := bin/paripari
IMAGE      := ghcr.io/mattmezza/paripari
TAILWIND   := ./bin/tailwindcss
TAILWIND_V := v4.1.14
HTMX_V     := 2.0.4
ALPINE_V   := 3.14.8
CHARTJS_V  := 4.4.7
SANKEY_V   := 0.14.0

.PHONY: help dev test build docker-build release vendor css tools seed clean

help: ## List all targets
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

dev: ## Run locally with DEV=1 (uses air when installed)
	@cd $(APP) && DEV=1 $$(command -v air >/dev/null 2>&1 && echo air || echo "go run ./cmd/server")

test: ## Run all tests
	cd $(APP) && go vet ./... && go test ./...

build: ## Build a static binary into bin/paripari
	cd $(APP) && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o ../$(BIN) ./cmd/server

docker-build: ## Build the Docker image locally
	docker build -t $(IMAGE):dev -f $(APP)/Dockerfile $(APP)

release: ## Tag and publish a release: make release name=v0.1
	@test -n "$(name)" || { echo "usage: make release name=vX.Y"; exit 1; }
	@git diff-index --quiet HEAD -- || { echo "working tree is dirty — commit first"; exit 1; }
	@test -z "$$(git log @{u}.. --oneline 2>/dev/null)" || { echo "unpushed commits — run git push first"; exit 1; }
	@git rev-parse -q --verify refs/tags/$(name) >/dev/null && { echo "tag $(name) already exists"; exit 1; } || true
	$(MAKE) test
	git tag -a $(name) -m "$(name)" && git push origin $(name)
	gh release create $(name) --generate-notes
	@echo "released $(name) — GHA is building ghcr.io/mattmezza/paripari:$(name)"

seed: ## Insert demo data (refuses on a non-empty database)
	cd $(APP) && go run ./cmd/server -seed

tools: ## Download the Tailwind v4 standalone CLI into ./bin
	mkdir -p bin
	curl -sSL -o $(TAILWIND) \
		https://github.com/tailwindlabs/tailwindcss/releases/download/$(TAILWIND_V)/tailwindcss-linux-x64
	chmod +x $(TAILWIND)

css: ## Build app.min.css with the Tailwind CLI (needs make tools)
	@test -x $(TAILWIND) || (echo "run 'make tools' first" && exit 1)
	$(TAILWIND) -i $(APP)/static/css/app.css -o $(APP)/static/css/app.min.css --minify

vendor: ## Download pinned htmx / alpine / chart.js / sankey into app/static/js
	mkdir -p $(APP)/static/js
	curl -sSL -o $(APP)/static/js/htmx.min.js   https://unpkg.com/htmx.org@$(HTMX_V)/dist/htmx.min.js
	curl -sSL -o $(APP)/static/js/alpine.min.js https://cdn.jsdelivr.net/npm/alpinejs@$(ALPINE_V)/dist/cdn.min.js
	curl -sSL -o $(APP)/static/js/chart.umd.js  https://cdn.jsdelivr.net/npm/chart.js@$(CHARTJS_V)/dist/chart.umd.js
	curl -sSL -o $(APP)/static/js/chart-sankey.min.js https://cdn.jsdelivr.net/npm/chartjs-chart-sankey@$(SANKEY_V)/dist/chartjs-chart-sankey.min.js

clean: ## Remove build output
	rm -rf bin
