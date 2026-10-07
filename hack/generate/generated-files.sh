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

# hack/generate/generated-files.sh - Generate the generated-file tables in the docs
#
# The tables in docs/reference/generated-go.md and docs/explanation/architecture.md are
# rendered by a test in internal/emit/emitkit from the files the built-in emitters
# reserve.
#
# Usage:
#   ./hack/generate/generated-files.sh              Rewrite both tables
#   ./hack/generate/generated-files.sh --validate   Fail when either table is stale

# shellcheck source=../lib/init.sh
source "$(dirname "$0")/../lib/init.sh"

# The test that renders the tables and checks the committed ones.
readonly TABLE_TEST="TestTheGeneratedFileTablesListEveryFileTheEmittersWrite"

# The package that holds the test, beside the list of files the emitters reserve.
readonly TABLE_PACKAGE="./internal/emit/emitkit"

# generate_tables runs the table test, optionally with extra flags for it.
# Globals:
#   TABLE_TEST - Read
#   TABLE_PACKAGE - Read
# Arguments:
#   $@ - Extra flags for the test binary
generate_tables() {
    "${GO}" test -count=1 -run "$TABLE_TEST" "$TABLE_PACKAGE" "$@"
}

# main generates or validates the generated-file tables.
# Arguments:
#   $1 - --validate to check the committed files instead of rewriting them
main() {
    if [[ "${1:-}" == "--validate" ]]; then
        seagull::log::header "Validating the generated-file tables"
        generate_tables
        seagull::log::footer
        seagull::log::success "generated-file tables are up to date"
        return 0
    fi

    seagull::log::header "Generating the generated-file tables"

    generate_tables -args -update-generated-files

    seagull::log::footer
    seagull::log::success "generated-file table generation complete!"
}

main "$@"
