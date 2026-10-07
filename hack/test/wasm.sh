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

# hack/test/wasm.sh - Test the WASM runtime natively and under Node, then self-test it
#
# Usage:
#   ./hack/test/wasm.sh

# shellcheck source=../lib/init.sh
source "$(dirname "$0")/../lib/init.sh"

# The runtime's module, relative to SEAGULL_ROOT.
readonly WASM_MODULE="cmd/wasm"

# The binary hack/wasm/build.sh writes.
readonly WASM_BINARY="${SEAGULL_ROOT}/bin/wasm/seagull.wasm"

# verify_node checks that node is installed.
# Returns:
#   Exits with code 1 if not found
verify_node() {
    if ! seagull::util::verify_binary "node" "https://nodejs.org/en/download OR apt install nodejs"; then
        exit 1
    fi
}

# test_runtime vets and tests the runtime natively, then for js/wasm under Node through
# Go's go_js_wasm_exec runner.
# Globals:
#   SEAGULL_ROOT - Read
#   WASM_MODULE - Read
test_runtime() {
    local tools
    tools="$("${GO}" env GOROOT)/lib/wasm"

    seagull::log::info "go vet and go test (${WASM_MODULE}, native)"
    ( cd "${SEAGULL_ROOT}/${WASM_MODULE}" && "${GO}" vet ./... && "${GO}" test ./... )

    seagull::log::info "go vet and go test (${WASM_MODULE}, js/wasm under $(node --version))"
    (
        cd "${SEAGULL_ROOT}/${WASM_MODULE}" &&
            export PATH="${tools}:${PATH}" GOOS=js GOARCH=wasm &&
            "${GO}" vet ./... &&
            "${GO}" test ./...
    )
}

# selftest_runtime runs the built binary's -selftest under Node, which generates the
# SQLite starter project and fails when the run reports an error.
# Globals:
#   WASM_BINARY - Read
selftest_runtime() {
    local output status=0

    seagull::log::info "seagull.wasm -selftest (under $(node --version))"
    output="$(node "$("${GO}" env GOROOT)/lib/wasm/wasm_exec_node.js" "${WASM_BINARY}" -selftest 2>&1)" || status=$?
    if [[ "$status" -ne 0 ]]; then
        printf '%s\n' "$output" >&2
        seagull::log::fatal "the runtime's self test exited ${status}"
    fi
}

# main builds the runtime, tests it natively and under Node, then self-tests the binary.
# Arguments:
#   $@ - Unused
main() {
    verify_node
    "${SEAGULL_ROOT}/hack/wasm/build.sh" build

    seagull::log::header "WASM runtime tests"
    test_runtime
    selftest_runtime
    seagull::log::footer
    seagull::log::success "WASM runtime tests passed"
}

main "$@"
