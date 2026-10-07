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

# hack/lint/vet.sh - Run go vet over the workspace, on one or every build configuration
#
# Usage:
#   ./hack/lint/vet.sh            Vet the default build configuration
#   ./hack/lint/vet.sh tags       Vet every build tag and cross-compile target

# shellcheck source=../lib/init.sh
source "$(dirname "$0")/../lib/init.sh"

# Build tags that gate real code and so need their own vet pass.
readonly BUILD_TAGS=(formatcheck)

# GOOS/GOARCH pairs the project supports.
readonly CROSS_TARGETS=(linux/386 linux/arm linux/arm64 darwin/arm64 windows/amd64 js/wasm)

# vet_default vets the default build configuration of the root module, the command
# modules and the integration harness.
# Globals:
#   SEAGULL_ROOT - Read
vet_default() {
    seagull::log::info "vet the root module"
    "${GO}" vet ./...

    local dir
    for dir in cmd/sgl cmd/seagull tests/integration/internal/harness; do
        seagull::log::info "vet $dir"
        ( cd "${SEAGULL_ROOT}/${dir}" && "${GO}" vet ./... )
    done
}

# vet_tags type-checks every build configuration the project actually uses.
# Globals:
#   BUILD_TAGS - Read
#   CROSS_TARGETS - Read
#   SEAGULL_ROOT - Read
vet_tags() {
    for tag in "${BUILD_TAGS[@]}"; do
        seagull::log::info "vet -tags $tag"
        "${GO}" vet -tags "$tag" ./...
    done

    for target in "${CROSS_TARGETS[@]}"; do
        seagull::log::info "build ${target}"
        GOOS="${target%/*}" GOARCH="${target#*/}" "${GO}" build ./...
    done

    while IFS= read -r dir; do
        seagull::log::info "vet -tags integration ($dir)"
        ( cd "${SEAGULL_ROOT}/${dir}" && "${GO}" vet -tags integration ./... )
    done < <(seagull::go::integration_dirs)
}

# main dispatches to the requested vet mode.
# Arguments:
#   $1 - Mode to run: default (the default) or tags
main() {
    local command="${1:-default}"

    case "$command" in
        default)
            seagull::log::header "go vet (default build configuration)"
            vet_default
            seagull::log::footer
            seagull::log::success "go vet passed"
            ;;

        tags)
            seagull::log::header "go vet (every build tag and cross-compile target)"
            vet_tags
            seagull::log::footer
            seagull::log::success "every build configuration type-checks"
            ;;

        help|--help|-h)
            seagull::log::info "Usage: hack/lint/vet.sh [default|tags]"
            ;;

        *)
            seagull::log::fatal "Unknown command: $command (expected: default, tags)"
            ;;
    esac
}

main "$@"
