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

# hack/generate/tutorials.sh - Regenerate, or check, the code of every tutorial checkpoint
#
# Each directory under tests/tutorials with an sgl.yaml is the state a lesson ends in.
# --validate also requires every checkpoint to vet cleanly under --strict.
#
# Usage:
#   ./hack/generate/tutorials.sh              Regenerate every checkpoint
#   ./hack/generate/tutorials.sh --validate   Fail when any checkpoint is stale

# shellcheck source=../lib/init.sh
source "$(dirname "$0")/../lib/init.sh"

# The directory holding the tutorial checkpoints.
readonly TUTORIALS_DIR="${SEAGULL_ROOT}/tests/tutorials"

# Array of checkpoints that failed or were stale.
FAILED=()

# Set to 1 by --validate.
VALIDATE=0

# generate_checkpoint generates, or checks, one checkpoint's committed code.
# Globals:
#   VALIDATE - Read
# Arguments:
#   $1 - Path to the checkpoint's sgl.yaml
#   $2 - Path to the sgl binary
# Returns:
#   0 when the checkpoint is current (or was regenerated), 1 otherwise
generate_checkpoint() {
    local config="$1" sgl="$2"

    if [[ "$VALIDATE" -eq 1 ]]; then
        "$sgl" -c "$config" vet --strict || return 1
        "$sgl" -c "$config" generate --check || return 1
        return 0
    fi

    "$sgl" -c "$config" generate || return 1
}

# main builds sgl once and generates, or checks, every checkpoint.
# Globals:
#   FAILED - Modified with failed checkpoints
#   SEAGULL_ROOT - Read
#   TUTORIALS_DIR - Read
#   VALIDATE - Set by --validate
# Arguments:
#   $1 - --validate to check the committed files instead of rewriting them
main() {
    if [[ "${1:-}" == "--validate" ]]; then
        VALIDATE=1
        seagull::log::header "Validating the tutorial checkpoints"
    else
        seagull::log::header "Generating the tutorial checkpoints"
    fi

    local workdir
    workdir=$(mktemp -d)
    # shellcheck disable=SC2064
    trap "rm -rf '${workdir}'" EXIT

    local sgl="${workdir}/sgl"
    "${GO}" build -o "$sgl" ./cmd/sgl

    local config relative count=0
    for config in "${TUTORIALS_DIR}"/*/sgl.yaml; do
        [[ -f "$config" ]] || continue
        count=$((count + 1))
        relative="${config#"${SEAGULL_ROOT}"/}"
        relative="${relative%/sgl.yaml}"
        if generate_checkpoint "$config" "$sgl"; then
            seagull::log::success "$relative"
        else
            seagull::log::error "$relative"
            FAILED+=("$relative")
        fi
    done

    if [[ "$count" -eq 0 ]]; then
        seagull::log::fatal "no tutorial checkpoints under tests/tutorials"
    fi
    if [[ ${#FAILED[@]} -gt 0 ]]; then
        seagull::log::fatal "stale or failing: ${FAILED[*]}; run make generate-tutorials, or see the messages above"
    fi

    seagull::log::footer

    if [[ "$VALIDATE" -eq 1 ]]; then
        seagull::log::success "tutorial checkpoints are up to date"
    else
        seagull::log::success "tutorial checkpoint generation complete!"
    fi
}

main "$@"
