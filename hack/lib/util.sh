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

# hack/lib/util.sh - Common utility functions for seagull scripts

# Prevent double-sourcing
if [[ -n "${_SEAGULL_UTIL_LOADED:-}" ]]; then
    return 0
fi
readonly _SEAGULL_UTIL_LOADED=1

# seagull::util::verify_binary checks if a binary is available in PATH
# Arguments:
#   $1 - Binary name
#   $2 - Optional install command hint
# Returns:
#   0 if binary exists, 1 otherwise
seagull::util::verify_binary() {
    local binary="$1"
    local install_cmd="${2:-}"

    if ! command -v "$binary" &>/dev/null; then
        seagull::log::error "Required binary not found: $binary"
        if [[ -n "$install_cmd" ]]; then
            seagull::log::info "Install with: $install_cmd"
        fi
        return 1
    fi
    return 0
}

# seagull::util::host_os returns the current operating system
# Returns: linux, darwin, or windows
seagull::util::host_os() {
    local os
    os="$(uname -s)"
    case "$os" in
        Linux*)  echo "linux" ;;
        Darwin*) echo "darwin" ;;
        MINGW*|MSYS*|CYGWIN*) echo "windows" ;;
        *)       seagull::log::fatal "Unsupported OS: $os" ;;
    esac
}

# seagull::util::host_arch returns the current architecture
# Returns: amd64, arm64, etc.
seagull::util::host_arch() {
    local arch
    arch="$(uname -m)"
    case "$arch" in
        x86_64)  echo "amd64" ;;
        aarch64) echo "arm64" ;;
        arm64)   echo "arm64" ;;
        *)       seagull::log::fatal "Unsupported architecture: $arch" ;;
    esac
}

# seagull::util::ensure_dir creates a directory if it doesn't exist
# Arguments:
#   $1 - Directory path
seagull::util::ensure_dir() {
    local dir="$1"
    if [[ ! -d "$dir" ]]; then
        mkdir -p "$dir"
    fi
}

# seagull::util::relative_path converts an absolute path to a path relative to SEAGULL_ROOT
# Arguments:
#   $1 - Absolute path
seagull::util::relative_path() {
    local path="$1"
    echo "${path#"${SEAGULL_ROOT}/"}"
}

# seagull::util::find_go_modules finds all go.mod files under a directory
# Arguments:
#   $1 - Directory to search (defaults to SEAGULL_ROOT)
#   $2 - If "--all", include testdata directories
seagull::util::find_go_modules() {
    local dir="${1:-${SEAGULL_ROOT}}"
    local include_all="${2:-}"

    if [[ "$include_all" == "--all" ]]; then
        find "$dir" -name "go.mod" -type f -not -path "*/vendor/*" -not -path "*/.git/*" -not -path "*/node_modules/*" | seagull::util::reject_ignored_paths
    else
        find "$dir" -name "go.mod" -type f -not -path "*/vendor/*" -not -path "*/.git/*" -not -path "*/node_modules/*" -not -path "*/testdata/*" | seagull::util::reject_ignored_paths
    fi
}

# seagull::util::reject_ignored_paths drops paths that git ignores, so that
# scratch checkouts and build output are never treated as modules belonging
# to this repository.
# Arguments:
#   Reads newline-separated paths on stdin
seagull::util::reject_ignored_paths() {
    local paths
    paths=$(cat)

    if [[ -z "$paths" ]]; then
        return 0
    fi

    local ignored
    ignored=$(git -C "${SEAGULL_ROOT}" check-ignore --stdin <<<"$paths" 2>/dev/null || true)

    if [[ -z "$ignored" ]]; then
        printf '%s\n' "$paths"
        return 0
    fi

    grep -vxF -f <(printf '%s\n' "$ignored") <<<"$paths" || true
}
