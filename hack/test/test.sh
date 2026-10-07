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

# hack/test/test.sh - Run seagull's test suites in any of their lanes
#
# Usage:
#   ./hack/test/test.sh quick                Library and CLI, default lane (the default)
#   ./hack/test/test.sh short                Library and CLI, -short
#   ./hack/test/test.sh race                 The race detector
#   ./hack/test/test.sh golden               Golden snapshots
#   ./hack/test/test.sh compile              The compile suite
#   ./hack/test/test.sh integration [suite]  The database integration suites, or one
#   ./hack/test/test.sh examples             Every examples module
#   ./hack/test/test.sh tutorials            Every tutorial checkpoint
#   ./hack/test/test.sh formatcheck          The import formatter cross-check
#   ./hack/test/test.sh wasm                 The WASM runtime, natively and under Node
#   ./hack/test/test.sh all                  quick and race

# shellcheck source=../lib/init.sh
source "$(dirname "$0")/../lib/init.sh"

# Module directories that carry their own test suite in the default lane.
readonly TEST_MODULE_DIRS=(. cmd/sgl cmd/seagull tests/facade tests/integration/internal/harness)

# The database integration suites, one module each, all behind the `integration` tag.
INTEGRATION_DIRS=()
while IFS= read -r suite; do
    [[ "$suite" == "tests/integration/compile" ]] && continue
    INTEGRATION_DIRS+=("$suite")
done < <(seagull::go::integration_dirs)
readonly INTEGRATION_DIRS

# Array of failed suite names.
FAILED=()

# run_modules runs go test in every module directory.
# Globals:
#   TEST_MODULE_DIRS - Read
#   SEAGULL_ROOT - Read
# Arguments:
#   $@ - Flags for go test
run_modules() {
    for dir in "${TEST_MODULE_DIRS[@]}"; do
        seagull::log::info "go test $* ($dir)"
        ( cd "${SEAGULL_ROOT}/${dir}" && "${GO}" test "$@" ./... ) || return 1
    done
}

# run_suites runs go test in every integration suite module, or in the one named.
# Globals:
#   INTEGRATION_DIRS - Read
#   SEAGULL_ROOT - Read
# Arguments:
#   $1 - Suite to run, such as postgres, or empty for every suite
#   $@ - Further flags for go test
run_suites() {
    local only="$1"
    shift

    for dir in "${INTEGRATION_DIRS[@]}"; do
        if [[ -n "$only" && "$dir" != "tests/integration/${only}" ]]; then
            continue
        fi
        seagull::log::info "go test -tags integration $* ($dir)"
        ( cd "${SEAGULL_ROOT}/${dir}" && "${GO}" test -tags integration "$@" ./... ) || return 1
    done
}

# run_suite_dir runs go test in one integration suite module.
# Globals:
#   SEAGULL_ROOT - Read
# Arguments:
#   $1 - Suite directory, relative to SEAGULL_ROOT
#   $@ - Further flags for go test
run_suite_dir() {
    local dir="$1"
    shift

    ( cd "${SEAGULL_ROOT}/${dir}" && "${GO}" test -tags integration "$@" ./... )
}

# golden_packages lists every root-module package that defines the -update flag.
# Globals:
#   SEAGULL_ROOT - Read
# Outputs:
#   Writes one package directory per line, relative to SEAGULL_ROOT
golden_packages() {
    grep -rlE --include='*_test.go' 'flag\.Bool\("update"' "${SEAGULL_ROOT}" \
        | grep -v '/tests/integration/' \
        | xargs -r -n1 dirname \
        | sed "s#^${SEAGULL_ROOT}#.#" \
        | sort -u
}

# test_quick runs the full suite of the library and CLI in the default lane.
test_quick() {
    run_modules
}

# test_short runs the short suite of the library and CLI.
test_short() {
    run_modules -short
}

# test_race runs the library and CLI suites under the race detector.
test_race() {
    run_modules -race -count=1 -timeout=40m
}

# test_golden compares the golden snapshots.
test_golden() {
    local dirs=() dir
    while IFS= read -r dir; do
        dirs+=("$dir")
    done < <(golden_packages)
    "${GO}" test -count=1 "${dirs[@]}"
}

# test_golden_update re-records the golden snapshots.
test_golden_update() {
    local dir
    while IFS= read -r dir; do
        seagull::log::info "go test ${dir} -update"
        "${GO}" test -count=1 "$dir" -update
    done < <(golden_packages)
}

# test_compile runs the compile suite, which vets and builds every case for every emitter.
test_compile() {
    run_suite_dir tests/integration/compile -count=1
}

# test_integration runs the database integration suites, or the one named.
# Arguments:
#   $1 - Suite to run, such as postgres (defaults to every suite)
test_integration() {
    run_suites "${1:-}" -count=1 -timeout=60m
}

# test_examples vets, builds and tests every examples module, then fails if any did.
# Globals:
#   SEAGULL_ROOT - Read
test_examples() {
    local modules=() dir
    while IFS= read -r dir; do
        modules+=("$dir")
    done < <(seagull::go::example_dirs)

    local total=${#modules[@]} current=0 failing=() cgo
    for dir in "${modules[@]}"; do
        current=$((current + 1))
        seagull::log::step "$current" "$total" "$dir"

        # DuckDB's driver needs cgo.
        cgo="$("${GO}" env CGO_ENABLED)"
        if [[ "$dir" == "examples/duckdb" ]]; then
            cgo=1
        fi

        # The server variants skip without a DSN; the integration suites set one.
        if (
            cd "${SEAGULL_ROOT}/${dir}" &&
                export CGO_ENABLED="$cgo" &&
                unset SGL_EXAMPLE_DSN SGL_EXAMPLE_ENGINE SGL_EXAMPLE_REQUIRE_DSN &&
                status=0 &&
                { "${GO}" vet ./... || status=1; } &&
                { "${GO}" build ./... || status=1; } &&
                { "${GO}" test -count=1 ./... || status=1; } &&
                exit "$status"
        ); then
            seagull::log::success "$dir"
        else
            seagull::log::error "$dir"
            failing+=("$dir")
        fi
        seagull::log::blank
    done

    if [[ ${#failing[@]} -gt 0 ]]; then
        seagull::log::fatal "failing examples modules: ${failing[*]}"
    fi
}

# test_tutorials runs the tests of every tutorial checkpoint.
test_tutorials() {
    ( cd "${SEAGULL_ROOT}/tests/tutorials" && "${GO}" test -count=1 ./... )
}

# test_formatcheck runs the Go generator's tests with the formatcheck tag, which checks
# every file it formats against goimports.
test_formatcheck() {
    "${GO}" test -tags formatcheck -count=1 ./internal/gogen/...
}

# test_wasm tests the WASM runtime natively and under Node, then self-tests it.
test_wasm() {
    "${SEAGULL_ROOT}/hack/test/wasm.sh"
}

# run_suite executes one suite and tracks the result.
# Globals:
#   FAILED - Modified with failed suite names
# Arguments:
#   $1 - Display name for the suite
#   $2 - Function to execute
run_suite() {
    local name="$1"
    local suite="$2"

    seagull::log::header "$name"

    if "$suite"; then
        seagull::log::success "$name passed"
    else
        seagull::log::error "$name failed"
        FAILED+=("$name")
    fi

    seagull::log::footer
}

# test_all runs the suites that make up the "is everything OK" check.
# Globals:
#   FAILED - Read
test_all() {
    run_suite "Default lane" test_quick
    run_suite "Race detector" test_race

    seagull::log::header "Summary"
    seagull::log::info "Failed: ${#FAILED[@]}"

    if [[ ${#FAILED[@]} -gt 0 ]]; then
        for name in "${FAILED[@]}"; do
            seagull::log::detail "- $name"
        done
        exit 1
    fi

    seagull::log::success "All suites passed"
}

# print_usage displays help information.
print_usage() {
    cat <<EOF
Usage: $(basename "$0") <command>

Commands:
    quick                Run the library and CLI tests in the default lane (default)
    short                Run the library and CLI tests with -short
    race                 Run the library and CLI tests under the race detector
    golden               Compare the golden snapshots
    golden-update        Re-record the golden snapshots
    compile              Run the compile suite over every case and emitter
    integration [suite]  Run the database integration suites, or the one named
    examples             Vet, build and test every examples module
    tutorials            Test every tutorial checkpoint
    formatcheck          Cross-check the import formatter against goimports
    wasm                 Test the WASM runtime natively and under Node, then self-test it
    all                  Run quick and race

Examples:
    $(basename "$0") quick
    $(basename "$0") integration postgres
EOF
}

# main handles command dispatch.
# Arguments:
#   $1 - Command to execute
#   $2 - Suite to run, for integration
main() {
    local command="${1:-quick}"

    case "$command" in
        quick)
            test_quick
            ;;

        short)
            test_short
            ;;

        race)
            test_race
            ;;

        golden)
            test_golden
            ;;

        golden-update)
            test_golden_update
            ;;

        compile)
            test_compile
            ;;

        integration)
            test_integration "${2:-}"
            ;;

        examples)
            test_examples
            ;;

        tutorials)
            test_tutorials
            ;;

        formatcheck)
            test_formatcheck
            ;;

        wasm)
            test_wasm
            ;;

        all)
            test_all
            ;;

        help|--help|-h)
            print_usage
            ;;

        *)
            seagull::log::error "Unknown command: $command"
            seagull::log::blank
            print_usage
            exit 1
            ;;
    esac
}

main "$@"
