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

# hack/lib/init.sh - Main initialisation script for seagull hack/ scripts

# Prevent double-sourcing
if [[ -n "${_SEAGULL_INIT_LOADED:-}" ]]; then
    return 0
fi
readonly _SEAGULL_INIT_LOADED=1

set -o errexit
set -o nounset
set -o pipefail

if [[ -z "${SEAGULL_ROOT:-}" ]]; then
    if [[ -n "${BASH_SOURCE[0]:-}" ]]; then
        SEAGULL_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
    else
        SEAGULL_ROOT="$(pwd)"
        while [[ "$SEAGULL_ROOT" != "/" ]] && [[ ! -f "${SEAGULL_ROOT}/go.mod" ]]; do
            SEAGULL_ROOT="$(dirname "$SEAGULL_ROOT")"
        done
        if [[ "$SEAGULL_ROOT" == "/" ]]; then
            echo "Error: Could not determine SEAGULL_ROOT. Set it manually." >&2
            if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
                exit 1
            else
                return 1
            fi
        fi
    fi
fi
export SEAGULL_ROOT

SEAGULL_LIB_DIR="${SEAGULL_ROOT}/hack/lib"
export SEAGULL_LIB_DIR

# shellcheck source=./logging.sh
source "${SEAGULL_LIB_DIR}/logging.sh"
# shellcheck source=./util.sh
source "${SEAGULL_LIB_DIR}/util.sh"
# shellcheck source=./go.sh
source "${SEAGULL_LIB_DIR}/go.sh"

cd "${SEAGULL_ROOT}"
