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

.PHONY: build build-all package test test-e2e clean install

build:
	@mkdir -p $(DIST_DIR)
	go build $(LDFLAGS) -o $(DIST_DIR)/$(BINARY) .
	@scripts/codesign-darwin.sh $(DIST_DIR)/$(BINARY) "$(CODESIGN_IDENTITY)"

build-all:
	@mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build $(LDFLAGS) -o $(DIST_DIR)/$(BINARY)-linux-amd64   .
	CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build $(LDFLAGS) -o $(DIST_DIR)/$(BINARY)-linux-arm64   .
	CGO_ENABLED=0 GOOS=darwin  GOARCH=amd64 go build $(LDFLAGS) -o $(DIST_DIR)/$(BINARY)-darwin-amd64  .
	CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build $(LDFLAGS) -o $(DIST_DIR)/$(BINARY)-darwin-arm64  .
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(DIST_DIR)/$(BINARY)-windows-amd64.exe .
	@scripts/codesign-darwin.sh $(DIST_DIR)/$(BINARY)-darwin-amd64 "$(CODESIGN_IDENTITY)"
	@scripts/codesign-darwin.sh $(DIST_DIR)/$(BINARY)-darwin-arm64 "$(CODESIGN_IDENTITY)"

## package: Build all platforms, zip with version suffix, and
## notarize darwin builds. Zip layout follows the canonical-binary
## convention (feedback_release_zip_canonical_binary): the binary
## inside the zip is named $(BINARY) (no arch suffix); the suffix
## lives only on the zip filename.
package: build-all
	@cd $(DIST_DIR) && for f in $(BINARY)-*; do \
		case "$$f" in *.zip) continue ;; esac; \
		suffix=$${f#$(BINARY)-}; \
		suffix=$${suffix%%.exe}; \
		ext=""; case "$$f" in *.exe) ext=".exe" ;; esac; \
		cp ../README.md .; \
		stage="$$(dirname "$$f")/_pkg"; rm -rf "$$stage"; mkdir -p "$$stage"; \
		cp "$$f" "$$stage/$(BINARY)$$ext"; \
		zip -j "$(BINARY)-$(VERSION)-$${suffix}.zip" "$$stage/$(BINARY)$$ext" README.md; \
		rm -rf "$$stage"; \
		rm -f README.md; \
	done
	@scripts/notarize-darwin.sh $(DIST_DIR)/$(BINARY)-$(VERSION)-darwin-amd64.zip "$(NOTARY_PROFILE)"
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
