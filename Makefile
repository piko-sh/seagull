# seagull Makefile
#
# This Makefile provides a unified interface for common development tasks.
# Every recipe delegates to a script under hack/; the rationale for what each
# one does lives in that script's header, not here.
#
# Run `make help` to see available targets.

.DEFAULT_GOAL := help

# Directories
HACK_DIR := ./hack
BIN_DIR := ./bin

# Go settings
GO ?= go

##@ General

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} \
		/^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-27s\033[0m %s\n", $$1, $$2 } \
		/^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)

##@ Build

.PHONY: build-cli
build-cli: ## Build the CLI binary for the current platform
	@$(HACK_DIR)/build/build.sh current $(BIN_DIR)/sgl

.PHONY: build-cli-all
build-cli-all: ## Build CLI binaries for every release platform
	@$(HACK_DIR)/build/build.sh all $(BIN_DIR)/cli

.PHONY: install-cli
install-cli: ## Build and install the CLI binary into GOBIN
	@$(HACK_DIR)/build/build.sh install

.PHONY: build-wasm
build-wasm: ## Build, optimise, and compress the WASM playground binary
	@$(HACK_DIR)/wasm/build.sh build

.PHONY: build-wasm-clean
build-wasm-clean: ## Clean WASM build artefacts
	@$(HACK_DIR)/wasm/build.sh clean

##@ Test

.PHONY: test
test: ## Run the library and CLI tests in the default lane
	@$(HACK_DIR)/test/test.sh quick

.PHONY: test-short
test-short: ## Run the library and CLI tests with -short
	@$(HACK_DIR)/test/test.sh short

.PHONY: test-race
test-race: ## Run the library and CLI tests under the race detector
	@$(HACK_DIR)/test/test.sh race

.PHONY: test-golden
test-golden: ## Compare the golden snapshots
	@$(HACK_DIR)/test/test.sh golden

.PHONY: test-golden-update
test-golden-update: ## Re-record the golden snapshots
	@$(HACK_DIR)/test/test.sh golden-update

.PHONY: test-compile
test-compile: ## Run the compile suite over every case and emitter
	@$(HACK_DIR)/test/test.sh compile

.PHONY: test-integration
test-integration: ## Run the database integration suites. ARGS=postgres narrows it to one
	@$(HACK_DIR)/test/test.sh integration $(ARGS)

.PHONY: test-examples
test-examples: ## Vet, build and test every examples module
	@$(HACK_DIR)/test/test.sh examples

.PHONY: test-tutorials
test-tutorials: ## Test every tutorial checkpoint
	@$(HACK_DIR)/test/test.sh tutorials

.PHONY: test-formatcheck
test-formatcheck: ## Cross-check the import formatter against goimports
	@$(HACK_DIR)/test/test.sh formatcheck

.PHONY: test-wasm
test-wasm: ## Test the WASM runtime natively and under Node, then self-test it
	@$(HACK_DIR)/test/test.sh wasm

.PHONY: test-all
test-all: ## Run the default and race lanes
	@$(HACK_DIR)/test/test.sh all

.PHONY: test-coverage-total
test-coverage-total: ## Report library statement coverage
	@$(HACK_DIR)/test/coverage.sh total

.PHONY: test-coverage-cli-total
test-coverage-cli-total: ## Report CLI statement coverage
	@$(HACK_DIR)/test/coverage.sh cli-total

.PHONY: test-update-badges
test-update-badges: ## Re-measure coverage and rewrite the README shields
	@$(HACK_DIR)/test/coverage.sh update-badges

##@ Lint

.PHONY: lint
lint: lint-go lint-scripts ## Run all linters

.PHONY: lint-go
lint-go: ## Lint every Go module with golangci-lint (includes the layering rules)
	@$(HACK_DIR)/lint/go.sh $(ARGS)

.PHONY: lint-scripts
lint-scripts: ## Lint every hack/ script with shellcheck
	@$(HACK_DIR)/lint/scripts.sh

.PHONY: lint-vet
lint-vet: ## Run go vet over the default build configuration
	@$(HACK_DIR)/lint/vet.sh default

.PHONY: lint-vet-tags
lint-vet-tags: ## Vet every build tag and cross-compile target
	@$(HACK_DIR)/lint/vet.sh tags

##@ Go

.PHONY: go-mod-tidy
go-mod-tidy: ## Tidy the go.mod files that can be tidied
	@$(HACK_DIR)/go/mod-tidy.sh

.PHONY: go-mod-update
go-mod-update: ## Update every module's direct dependencies. ARGS=cmd/sgl narrows it to one
	@$(HACK_DIR)/go/mod-update.sh $(ARGS)

.PHONY: go-mod-verify
go-mod-verify: ## Verify module checksums and tidiness
	@$(HACK_DIR)/go/mod-verify.sh

##@ Generate

.PHONY: generate-all
generate-all: ## Run every code generator, in dependency order
	@$(HACK_DIR)/generate/all.sh

.PHONY: generate-all-validate
generate-all-validate: ## Check every generated file is current
	@$(HACK_DIR)/generate/all.sh --validate

.PHONY: generate-diagnostics
generate-diagnostics: ## Regenerate the diagnostic reference
	@$(HACK_DIR)/generate/diagnostics.sh

.PHONY: generate-diagnostics-validate
generate-diagnostics-validate: ## Check the committed diagnostic reference is current
	@$(HACK_DIR)/generate/diagnostics.sh --validate

.PHONY: generate-config-schema
generate-config-schema: ## Regenerate the config JSON Schema
	@$(HACK_DIR)/generate/config-schema.sh

.PHONY: generate-config-schema-validate
generate-config-schema-validate: ## Check the committed config JSON Schema is current
	@$(HACK_DIR)/generate/config-schema.sh --validate

.PHONY: generate-generated-files
generate-generated-files: ## Regenerate the generated-file tables in the docs
	@$(HACK_DIR)/generate/generated-files.sh

.PHONY: generate-generated-files-validate
generate-generated-files-validate: ## Check the committed generated-file tables are current
	@$(HACK_DIR)/generate/generated-files.sh --validate

.PHONY: generate-examples
generate-examples: ## Regenerate the example projects' generated code
	@$(HACK_DIR)/generate/examples.sh

.PHONY: generate-examples-validate
generate-examples-validate: ## Check the committed example projects are current
	@$(HACK_DIR)/generate/examples.sh --validate

.PHONY: generate-tutorials
generate-tutorials: ## Regenerate the tutorial checkpoints' generated code
	@$(HACK_DIR)/generate/tutorials.sh

.PHONY: generate-tutorials-validate
generate-tutorials-validate: ## Check the committed tutorial checkpoints are current
	@$(HACK_DIR)/generate/tutorials.sh --validate

##@ Pre-submit

.PHONY: check
check: lint-vet lint-vet-tags lint-go lint-scripts go-mod-verify generate-all-validate ## Every gate that does not run a test suite

.PHONY: check-all
check-all: check test test-compile ## check plus the test suites and compile suite

##@ Clean

.PHONY: clean
clean: ## Clean build artefacts
	rm -rf $(BIN_DIR)
	rm -f $(TMPDIR)/seagull-coverage-*.out
