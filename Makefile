BINARY  := ask-llm-mcp
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.version=$(VERSION)"
DIST_DIR := dist

# macOS Developer ID signing / notarization (see nlink-jp/.github
# CONVENTIONS.md §Code Signing). Defaults match any Developer ID
# Application cert in the keychain and the org-standard notary
# profile. Builds without these fall back to ad-hoc / un-notarized
# with a one-line warning — see scripts/codesign-darwin.sh.
CODESIGN_IDENTITY ?= Developer ID Application
NOTARY_PROFILE    ?= nlink-jp-notary

# darwin ships arm64 only (no amd64, no universal). linux/windows keep their matrix.
PLATFORMS := darwin/arm64 linux/amd64 linux/arm64 windows/amd64

.PHONY: build build-all package test test-e2e clean install

build:
	@mkdir -p $(DIST_DIR)
	go build $(LDFLAGS) -o $(DIST_DIR)/$(BINARY) .
	@scripts/codesign-darwin.sh $(DIST_DIR)/$(BINARY) "$(CODESIGN_IDENTITY)"

build-all:
	@mkdir -p $(DIST_DIR)
	@for p in $(PLATFORMS); do os=$${p%/*}; arch=$${p#*/}; \
		ext=""; [ "$$os" = windows ] && ext=".exe"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build $(LDFLAGS) -o $(DIST_DIR)/$(BINARY)-$$os-$$arch$$ext . ; \
	done
	@scripts/codesign-darwin.sh $(DIST_DIR)/$(BINARY)-darwin-arm64 "$(CODESIGN_IDENTITY)" "$(BINARY)"

## package: archive each platform as <name>-v<version>-<os>-<arch>.<ext>
## (darwin/windows=zip, linux=tar.gz); canonical binary + README + LICENSE
## inside; notarize the darwin arm64 zip.
package: build-all
	@cd $(DIST_DIR) && for p in $(PLATFORMS); do os=$${p%/*}; arch=$${p#*/}; \
		ext=""; [ "$$os" = windows ] && ext=".exe"; \
		stage=_pkg; rm -rf $$stage; mkdir -p $$stage; \
		cp "$(BINARY)-$$os-$$arch$$ext" "$$stage/$(BINARY)$$ext"; \
		cp ../README.md ../LICENSE $$stage/; \
		base="$(BINARY)-$(VERSION)-$$os-$$arch"; \
		if [ "$$os" = linux ]; then ( cd $$stage && tar -czf "../$$base.tar.gz" * ); \
		else ( cd $$stage && zip -q "../$$base.zip" * ); fi; \
		rm -rf $$stage; \
	done
	@scripts/notarize-darwin.sh $(DIST_DIR)/$(BINARY)-$(VERSION)-darwin-arm64.zip "$(NOTARY_PROFILE)"

test:
	go test ./...

# E2E tests spawn the built binary and drive it over stdio. They are
# hermetic: each test stands up an in-process OpenAI-compatible dummy
# server, so no network, credentials, or running LM Studio are required.
# The harness reads ASK_LLM_TEST_BINARY (set here to the built binary).
test-e2e: build
	ASK_LLM_TEST_BINARY=$$(pwd)/$(DIST_DIR)/$(BINARY) go test -tags e2e ./e2e/...

clean:
	rm -rf $(DIST_DIR)

install: build
	install -m 0755 $(DIST_DIR)/$(BINARY) $(HOME)/bin/$(BINARY)
