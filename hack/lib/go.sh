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

# hack/lib/go.sh - Go toolchain helpers for seagull scripts
#
# This file should be sourced, not executed directly.
# All functions are namespaced with seagull::go::

# Prevent double-sourcing
if [[ -n "${_SEAGULL_GO_LOADED:-}" ]]; then
    return 0
fi
readonly _SEAGULL_GO_LOADED=1

# The Go command, overridable for toolchain experiments.
GO="${GO:-go}"

# The import path of this repository's root module.
readonly SEAGULL_MODULE="seagull.sh/seagull"

# seagull::go::integration_dirs lists every integration suite module directory, relative
# to SEAGULL_ROOT. The suites are one module each, and four scripts need the same list:
# discovering it here keeps a newly added suite from being linted and vetted but never
# actually tested.
# Globals:
#   SEAGULL_ROOT - Read
# Outputs:
#   Writes one module directory per line, such as tests/integration/postgres
seagull::go::integration_dirs() {
    local suite
    for suite in "${SEAGULL_ROOT}"/tests/integration/*/; do
        [[ -f "${suite}go.mod" ]] || continue
        printf '%s\n' "tests/integration/$(basename "${suite%/}")"
    done
}

# seagull::go::example_dirs lists every examples module directory, relative to
# SEAGULL_ROOT. The examples are one module each, and several scripts need the same list.
# Globals:
#   SEAGULL_ROOT - Read
# Outputs:
#   Writes one module directory per line, such as examples/postgres
seagull::go::example_dirs() {
    local example
    for example in "${SEAGULL_ROOT}"/examples/*/; do
        [[ -f "${example}go.mod" ]] || continue
        printf '%s\n' "examples/$(basename "${example%/}")"
    done
}

# seagull::go::with_local_replace runs a command in a module with temporary
# replace directives pointing at the working copy and at go.work's external
# replaces, then drops them again.
#
# Arguments:
#   $1 - Module directory, relative to SEAGULL_ROOT
#   $2 - Replacement target, relative to that module directory
#   $@ - Command to run inside the module
# Returns:
#   The command's exit status, after the replace has been dropped
seagull::go::with_local_replace() {
    local dir="$1"
    local target="$2"
    shift 2

    local required=() module
    while IFS= read -r module; do
        [[ -n "$module" ]] && required+=("$module")
    done < <(seagull::go::repository_modules)

    local self="${SEAGULL_MODULE}"
    [[ "$dir" != "." ]] && self="${SEAGULL_MODULE}/${dir}"

    local existing
    existing="$(seagull::go::existing_replaces "$dir")"

    local status=0
    (
        cd "${SEAGULL_ROOT}/${dir}" || exit 1
        local added=() module suffix
        for module in "${required[@]}"; do
            [[ "$module" == "$self" ]] && continue
            grep -qxF "$module" <<<"$existing" && continue
            suffix="${module#"${SEAGULL_MODULE}"}"
            "${GO}" mod edit -replace "${module}=${target}${suffix}"
            added+=("$module")
        done
        local location
        while IFS=$'\t' read -r module location; do
            [[ -z "$module" ]] && continue
            grep -qxF "$module" <<<"$existing" && continue
            "${GO}" mod edit -replace "${module}=${target}/${location}"
            added+=("$module")
        done < <(seagull::go::workspace_external_replaces)
        "$@"
        local inner=$?
        for module in "${added[@]+"${added[@]}"}"; do
            "${GO}" mod edit -dropreplace "${module}"
        done
        exit "${inner}"
    ) || status=$?

    return "${status}"
}

# seagull::go::workspace_external_replaces prints the go.work replaces that point at
# directories outside this repository.
# Globals:
#   SEAGULL_ROOT - Read
#   SEAGULL_MODULE - Read
# Outputs:
#   Writes one "module<TAB>directory" pair per line, the directory relative to
#   SEAGULL_ROOT
seagull::go::workspace_external_replaces() {
    local workspace="${SEAGULL_ROOT}/go.work"
    [[ -f "$workspace" ]] || return 0

    awk -v own="${SEAGULL_MODULE}" '
        /^replace[[:space:]]*\($/ { inside = 1; next }
        inside && /^\)/ { inside = 0; next }
        /^replace[[:space:]]+[^(]/ { sub(/^replace[[:space:]]+/, ""); line = $0 }
        inside { line = $0 }
        line != "" {
            split(line, fields, "=>")
            old = fields[1]; new = fields[2]; line = ""
            gsub(/^[[:space:]]+|[[:space:]]+$/, "", new)
            split(old, oldParts, " ")
            module = oldParts[1]
            if (index(module, own) == 1) next
            if (new !~ /^\.\.?\//) next
            printf "%s\t%s\n", module, new
        }
    ' "$workspace"
}

# seagull::go::existing_replaces prints the module paths a module already replaces,
# versioned or not, so a temporary replace never removes one the module committed.
# Globals:
#   SEAGULL_ROOT - Read
# Arguments:
#   $1 - Module directory, relative to SEAGULL_ROOT
# Outputs:
#   Writes one module path per line
seagull::go::existing_replaces() {
    local dir="$1"

    awk '
        /^replace[[:space:]]*\($/ { inside = 1; next }
        inside && /^\)/ { inside = 0; next }
        /^replace[[:space:]]+[^(]/ { print $2; next }
        inside && /=>/ { print $1 }
    ' "${SEAGULL_ROOT}/${dir}/go.mod" | sort -u
}

# seagull::go::repository_modules prints every module path this repository defines.
# Globals:
#   SEAGULL_MODULE - Read
# Outputs:
#   Writes one module path per line
seagull::go::repository_modules() {
    local directory

    while IFS= read -r directory; do
        if [[ "$directory" == "." ]]; then
            printf '%s\n' "${SEAGULL_MODULE}"
        else
            printf '%s/%s\n' "${SEAGULL_MODULE}" "$directory"
        fi
    done < <(seagull::go::module_dirs)
}

# seagull::go::module_dirs prints every module directory in the repository, relative to
# SEAGULL_ROOT, with the root module written as ".".
# Globals:
#   SEAGULL_ROOT - Read
# Outputs:
#   Writes one module directory per line, sorted
seagull::go::module_dirs() {
    local manifest directory

    while IFS= read -r manifest; do
        directory="${manifest%/go.mod}"
        directory="${directory#"${SEAGULL_ROOT}"}"
        directory="${directory#/}"
        printf '%s\n' "${directory:-.}"
    done < <(seagull::util::find_go_modules) | sort
}

# seagull::go::path_to_root returns the relative path from a module directory back to
# SEAGULL_ROOT.
# Arguments:
#   $1 - Module directory, relative to SEAGULL_ROOT
# Outputs:
#   The relative path, such as ../..
seagull::go::path_to_root() {
    local directory="$1" up="" segment

    if [[ "$directory" == "." ]]; then
        printf '%s\n' "."
        return 0
    fi

    while IFS= read -r segment; do
        [[ -n "$segment" ]] && up="../${up}"
    done < <(tr '/' '\n' <<<"$directory")
    printf '%s\n' "${up%/}"
}

# seagull::go::build compiles a package for one platform.
# Arguments:
#   $1 - Output file
#   $2 - Package to build
#   $3 - GOOS (defaults to the host)
#   $4 - GOARCH (defaults to the host)
#   $5 - ldflags (defaults to none)
# Outputs:
#   Logs the resulting binary size
seagull::go::build() {
    local output="$1"
    local package="$2"
    local goos="${3:-$(seagull::util::host_os)}"
    local goarch="${4:-$(seagull::util::host_arch)}"
    local ldflags="${5:-}"

    seagull::util::ensure_dir "$(dirname "$output")"

    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
        "${GO}" build -trimpath -ldflags "$ldflags" -o "$output" "$package"

    local size
    size=$(du -h "$output" | cut -f1)
    seagull::log::success "$(seagull::util::relative_path "$output") (${goos}/${goarch}, ${size})"
}
