GO ?= go
# The boilerplate's template sources are not packages that compile; its own
# tests cover them.
PKGS = $(shell $(GO) list -e ./... | grep -v /internal/boilerplate/files)
DIRS = $(shell $(GO) list -e -f '{{.Dir}}' ./... | grep -v /internal/boilerplate/files | sed 's|^$(CURDIR)|.|')

.PHONY: check example test lint vuln secrets e2e tools

check: test lint vuln secrets e2e

test: example
	$(GO) test -race $(PKGS)

lint: example
	golangci-lint run $(DIRS)

vuln:
	govulncheck $(PKGS)

secrets:
	@set -e; \
	if [ -e .gitleaksignore ] && grep -Eqv '^(#.*)?$$|^[0-9a-f]{40}:[^:]+:[^:]+:[0-9]+$$' .gitleaksignore; then \
		echo ".gitleaksignore may list only commit fingerprints: <commit>:<file>:<rule>:<line>"; \
		exit 1; \
	fi; \
	tmp=$$(mktemp -d); \
	trap 'rm -rf "$$tmp"' EXIT; \
	printf '[extend]\nuseDefault = true\n' >"$$tmp/gitleaks.toml"; \
	gitleaks git --no-banner --no-color --redact --verbose --ignore-gitleaks-allow --config "$$tmp/gitleaks.toml" \
		--log-opts="--full-history --diff-filter=tuxdb HEAD" .; \
	git ls-files -z --cached --others --exclude-standard >"$$tmp/listed"; \
	xargs -0 sh -c 'for f; do if [ -f "$$f" ]; then printf "%s\0" "$$f"; fi; done' sh <"$$tmp/listed" >"$$tmp/files"; \
	if grep -qz : "$$tmp/files"; then \
		echo "file paths must not contain a colon: rename the file"; \
		exit 1; \
	fi; \
	tar -cf "$$tmp/tree.tar" --null -T "$$tmp/files"; \
	mkdir "$$tmp/tree"; \
	tar -xf "$$tmp/tree.tar" -C "$$tmp/tree"; \
	cd "$$tmp/tree"; \
	gitleaks dir --no-banner --no-color --redact --verbose --ignore-gitleaks-allow --config "$$tmp/gitleaks.toml" .

# The example's embedded assets are generated, not committed. GOSSR_RUNTIME_TGZ,
# when set, is a local gossr-runtime package installed over the one in the
# lockfile.
example:
	cd example/internal/web && npm ci && { [ -z "$$GOSSR_RUNTIME_TGZ" ] || npm install --no-save "$$GOSSR_RUNTIME_TGZ"; }
	cd example && $(GO) run ..

e2e: example
	cd example/tests && npm ci && npx playwright test
	git diff --exit-code -- 'example/**/*_gen.go' 'example/**/__ssr_gen__.ts'

tools:
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
	$(GO) install golang.org/x/vuln/cmd/govulncheck@v1.8.0
	$(GO) install github.com/zricethezav/gitleaks/v8@v8.30.1
