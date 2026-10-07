#!/bin/bash
# Copyright 2026 PolitePixels Limited
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# This project stands against fascism, authoritarianism, and all forms of
# oppression. We built this to empower people, not to enable those who would
# strip others of their rights and dignity.

# hack/lint/go.sh - Lint every Go module in the workspace with golangci-lint
#
# Every module reads the same .golangci.yml at the repository root, so the
# layering rules under depguard are enforced here too; there is no separate
# layering target.
#
# Usage:
#   ./hack/lint/go.sh
#   ./hack/lint/go.sh --fix

# shellcheck source=../lib/init.sh
source "$(dirname "$0")/../lib/init.sh"

# Module directories to lint, relative to SEAGULL_ROOT.
LINT_DIRS=(. cmd/sgl cmd/seagull cmd/wasm tests/facade tests/integration/internal/harness)

# The integration suites are one module each and sit behind a build tag, so
# golangci-lint sees nothing in them without it.
while IFS= read -r suite; do
    LINT_DIRS+=("$suite")
done < <(seagull::go::integration_dirs)

# Extra arguments forwarded to golangci-lint.
LINT_ARGS=()

# Count of modules that reported a finding.
FAILED=0

# verify_golangci_lint checks that golangci-lint is installed.
# Returns:
#   Exits with code 1 if not found
verify_golangci_lint() {
    if ! seagull::util::verify_binary "golangci-lint" "https://golangci-lint.run/welcome/install/"; then
        exit 1
    fi
}

# parse_args reads the command line.
# Globals:
#   LINT_ARGS - Set to the forwarded arguments
parse_args() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            -h|--help)
                seagull::log::info "Usage: hack/lint/go.sh [golangci-lint arguments]"
                seagull::log::detail "--fix   Apply the fixes golangci-lint can make itself"
                exit 0
                ;;

            *)
                LINT_ARGS+=("$1")
                shift
                ;;
        esac
    done
}

# lint_modules runs golangci-lint in each module directory.
# Globals:
#   LINT_DIRS - Read
#   LINT_ARGS - Read
#   FAILED - Incremented for each failing module
lint_modules() {
    local total=${#LINT_DIRS[@]}
    local current=0

    for dir in "${LINT_DIRS[@]}"; do
        current=$((current + 1))
        seagull::log::step "$current" "$total" "golangci-lint $dir"

        local tag_args=()
        if [[ "$dir" == tests/integration/* ]]; then
            tag_args=(--build-tags integration)
        fi

        if ( cd "${SEAGULL_ROOT}/${dir}" && golangci-lint run "${tag_args[@]+"${tag_args[@]}"}" "${LINT_ARGS[@]+"${LINT_ARGS[@]}"}" ./... ); then
            seagull::log::success "$dir"
        else
            seagull::log::error "$dir"
            FAILED=$((FAILED + 1))
        fi
    done
}

# print_summary displays the lint results.
# Globals:
#   LINT_DIRS - Read
#   FAILED - Read
print_summary() {
    seagull::log::blank
    seagull::log::header "Summary"
    seagull::log::info "Total modules: ${#LINT_DIRS[@]}"
    seagull::log::info "Passed: $((${#LINT_DIRS[@]} - FAILED))"
    seagull::log::info "Failed: $FAILED"

    if [[ $FAILED -gt 0 ]]; then
        seagull::log::blank
        seagull::log::error "Some modules have lint findings."
        exit 1
    fi

    seagull::log::success "All modules passed golangci-lint!"
}

# main lints every module in the workspace.
# Arguments:
#   $@ - Arguments forwarded to golangci-lint
main() {
    parse_args "$@"
    verify_golangci_lint

    seagull::log::header "Linting Go modules with golangci-lint"

    lint_modules
    print_summary
}

main "$@"
