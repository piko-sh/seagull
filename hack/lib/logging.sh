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

# hack/lib/logging.sh - Standardised logging utilities for seagull scripts

# Prevent double-sourcing
if [[ -n "${_SEAGULL_LOGGING_LOADED:-}" ]]; then
    return 0
fi
readonly _SEAGULL_LOGGING_LOADED=1

# Colour codes (only if terminal supports it)
if [[ -t 2 ]]; then
    readonly SEAGULL_RED='\033[0;31m'
    readonly SEAGULL_GREEN='\033[0;32m'
    readonly SEAGULL_YELLOW='\033[1;33m'
    readonly SEAGULL_BLUE='\033[0;34m'
    readonly SEAGULL_CYAN='\033[0;36m'
    readonly SEAGULL_BOLD='\033[1m'
    readonly SEAGULL_NC='\033[0m'
else
    readonly SEAGULL_RED=''
    readonly SEAGULL_GREEN=''
    readonly SEAGULL_YELLOW=''
    readonly SEAGULL_BLUE=''
    readonly SEAGULL_CYAN=''
    readonly SEAGULL_BOLD=''
    readonly SEAGULL_NC=''
fi

# seagull::log::info prints an informational message
# Arguments:
#   $@ - Message to print
seagull::log::info() {
    echo -e "${SEAGULL_BLUE}[INFO]${SEAGULL_NC} $*" >&2
}

# seagull::log::success prints a success message
# Arguments:
#   $@ - Message to print
seagull::log::success() {
    echo -e "${SEAGULL_GREEN}[OK]${SEAGULL_NC} $*" >&2
}

# seagull::log::warn prints a warning message
# Arguments:
#   $@ - Message to print
seagull::log::warn() {
    echo -e "${SEAGULL_YELLOW}[WARN]${SEAGULL_NC} $*" >&2
}

# seagull::log::error prints an error message
# Arguments:
#   $@ - Message to print
seagull::log::error() {
    echo -e "${SEAGULL_RED}[ERROR]${SEAGULL_NC} $*" >&2
}

# seagull::log::fatal prints an error message and exits with code 1
# Arguments:
#   $@ - Message to print
seagull::log::fatal() {
    echo -e "${SEAGULL_RED}[FATAL]${SEAGULL_NC} $*" >&2
    exit 1
}

# seagull::log::header prints a prominent header
# Arguments:
#   $@ - Header text
seagull::log::header() {
    echo "========================================================================" >&2
    echo -e " ${SEAGULL_BOLD}$*${SEAGULL_NC}" >&2
    echo "========================================================================" >&2
}

# seagull::log::footer prints a footer separator
seagull::log::footer() {
    echo "------------------------------------------------------------------------" >&2
    echo >&2
}

# seagull::log::step prints a step indicator for multi-step processes
# Arguments:
#   $1 - Step number
#   $2 - Total steps
#   $3 - Step description
seagull::log::step() {
    local step="$1"
    local total="$2"
    shift 2
    echo -e "${SEAGULL_CYAN}[${step}/${total}]${SEAGULL_NC} $*" >&2
}

# seagull::log::detail prints supplementary detail without a prefix.
# Use for indented instructions or examples under a main log message.
# Arguments:
#   $@ - Detail text to print
seagull::log::detail() {
    echo -e "  $*" >&2
}

# seagull::log::blank prints a blank line for visual spacing.
seagull::log::blank() {
    echo >&2
}
