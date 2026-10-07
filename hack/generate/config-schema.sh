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

# hack/generate/config-schema.sh - Generate the config JSON Schema from the config structs
#
# Usage:
#   ./hack/generate/config-schema.sh              Rewrite docs/reference/sgl.schema.json
#   ./hack/generate/config-schema.sh --validate   Fail when docs/reference/sgl.schema.json is stale

# shellcheck source=../lib/init.sh
source "$(dirname "$0")/../lib/init.sh"

# The generated schema.
readonly OUTPUT_FILE="${SEAGULL_ROOT}/docs/reference/sgl.schema.json"

# generate_schema renders the schema into a file.
# Arguments:
#   $1 - File to write
generate_schema() {
    "${GO}" run ./cmd/sgl config schema > "$1"
}

# validate_schema fails when the committed schema no longer matches the config structs.
# Globals:
#   OUTPUT_FILE - Read
# Arguments:
#   $1 - File holding the freshly rendered schema
validate_schema() {
    if ! diff -u "$OUTPUT_FILE" "$1" >&2; then
        seagull::log::error "docs/reference/sgl.schema.json is stale"
        seagull::log::info "Regenerate with: make generate-config-schema"
        exit 1
    fi
}

# main generates or validates the config JSON Schema.
# Arguments:
#   $1 - --validate to check the committed file instead of rewriting it
main() {
    local rendered
    rendered=$(mktemp)
    # shellcheck disable=SC2064
    trap "rm -f '${rendered}'" EXIT

    if [[ "${1:-}" == "--validate" ]]; then
        seagull::log::header "Validating the config JSON Schema"
        generate_schema "$rendered"
        validate_schema "$rendered"
        seagull::log::footer
        seagull::log::success "config JSON Schema is up to date"
        return 0
    fi

    seagull::log::header "Generating the config JSON Schema"

    generate_schema "$rendered"
    cp "$rendered" "$OUTPUT_FILE"

    seagull::log::footer
    seagull::log::success "config JSON Schema generation complete!"
}

main "$@"
