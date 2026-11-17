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

# Helpers for manual fuzz campaigns and their reviewable artefacts.

# shellcheck source=./fuzz-resources.sh
source "$SEAGULL_LIB_DIR/fuzz-resources.sh"

# seagull::fuzz::tool builds the Go fuzz tool once into the checkout's bin directory.
# SEAGULL_FUZZ_TOOL names an already built tool instead.
seagull::fuzz::tool() {
    local checkout
    [[ -z "${fuzz_tool:-}" ]] || return 0
    if [[ -n "${SEAGULL_FUZZ_TOOL:-}" ]]; then
        [[ -x "$SEAGULL_FUZZ_TOOL" ]] || { printf 'SEAGULL_FUZZ_TOOL is not executable: %s\n' "$SEAGULL_FUZZ_TOOL" >&2; return 1; }
        fuzz_tool="$SEAGULL_FUZZ_TOOL"
        return 0
    fi
    # The tool builds from the checkout holding these scripts, whatever SEAGULL_ROOT names.
    checkout=$(cd -P "$SEAGULL_LIB_DIR/../.." && pwd)
    (cd "$checkout" && "$GO" build -o bin/fuzztool ./cmd/internal/fuzztool) || { printf 'The fuzz tool did not build.\n' >&2; return 1; }
    fuzz_tool="$checkout/bin/fuzztool"
}

# seagull::fuzz::space checks the filesystems that receive campaign data.
seagull::fuzz::space() {
    local minimum="$1" cache="$2" modules="$3" temporary="$4"
    local location available required
    for location in "$SEAGULL_ROOT" "${TMPDIR:-/tmp}" "$temporary" "$cache" "$modules"; do
        while [[ ! -d "$location" ]]; do location=$(dirname "$location"); done
        available=$(df -Pk "$location" | awk 'NR == 2 {print $4}')
        required=$((minimum * 1024))
        if [[ ! "$available" =~ ^[0-9]+$ ]] || (( available < required )); then
            printf 'Insufficient space at %s. Require %s MiB free. No cache was deleted.\n' "$location" "$minimum" >&2
            return 1
        fi
    done
}

# seagull::fuzz::prerequisites checks explicit target requirements.
seagull::fuzz::prerequisites() {
    local requirement enabled compiler platform module="$2"
    local -a requirements
    IFS=',' read -r -a requirements <<< "$1"
    for requirement in "${requirements[@]}"; do
        case "$requirement" in
            go) command -v "$GO" >/dev/null || { printf 'Go is required.\n' >&2; return 1; } ;;
            cgo)
                enabled=$("$GO" env CGO_ENABLED)
                compiler=$("$GO" env CC)
                [[ "$enabled" == 1 ]] || { printf 'This target requires CGO_ENABLED=1.\n' >&2; return 1; }
                command -v "${compiler%% *}" >/dev/null || { printf 'Missing C compiler %s.\n' "$compiler" >&2; return 1; }
                ;;
            docker)
                command -v docker >/dev/null || { printf 'This target requires Docker.\n' >&2; return 1; }
                docker info --format '{{.ServerVersion}}' >/dev/null || { printf 'Docker is unavailable.\n' >&2; return 1; }
                ;;
            embedded-postgres)
                platform=$("$GO" env GOOS)
                case "$platform" in linux|darwin) ;; *) printf 'Embedded PostgreSQL requires Linux or macOS.\n' >&2; return 1 ;; esac
                [[ $(id -u) != 0 ]] || { printf 'Embedded PostgreSQL cannot run as root.\n' >&2; return 1; }
                ;;
            cached-modules)
                (cd "$SEAGULL_ROOT/$module" && GOPROXY=off "$GO" list -m all >/dev/null) || {
                    printf 'Prepare pinned dependencies with the ordinary compile suite before fuzzing.\n' >&2
                    return 1
                }
                ;;
            *) printf 'Unknown registry prerequisite %s.\n' "$requirement" >&2; return 1 ;;
        esac
    done
}

# seagull::fuzz::capture retains output and times mutation after the baseline.
seagull::fuzz::capture() {
    local log="$1" timing="$2" began="$3" function="$4" budget="${5:-0}" coordinator="${6:-}"
    local line partial='' baseline_end='' mutation_end='' reached=false completed=false minimising=false
    local remaining read_status process checked=$SECONDS stopped=false
    local -a read_options
    while true; do
        read_options=(-r -t 5)
        if (( SECONDS - checked >= 5 )) && [[ "$stopped" == false ]]; then
            checked=$SECONDS
            if ! seagull::fuzz::resource_check >> "$log" 2>&1; then
                [[ -z "$coordinator" ]] || printf 'resource-stopped\n' > "${coordinator%/*}/outcome.txt"
                seagull::fuzz::interrupt "$coordinator"
                stopped=true
            fi
        fi
        if (( budget > 0 )) && [[ -n "$baseline_end" && -z "$mutation_end" && "$completed" == false && "$minimising" == false ]]; then
            # Round up so the shell clock cannot shorten the requested duration.
            remaining=$((baseline_end + budget + 1 - SECONDS))
            if (( remaining <= 0 )); then
                read -r process < "$coordinator" || return 1
                [[ "$process" =~ ^[1-9][0-9]*$ ]] || return 1
                # A coordinator that already exited needs no interrupt, and its output is still read.
                kill -INT "$process" 2>/dev/null || true
                completed=true
            else
                if (( remaining < 5 )); then read_options=(-r -t "$remaining"); fi
            fi
        fi
        if IFS= read -r "${read_options[@]}" line; then
            line="$partial$line"
            partial=''
            printf '%s\n' "$line" >> "$log"
            if [[ "$line" == "=== RUN   $function" ]]; then reached=true; fi
            if [[ "$line" == *'now fuzzing with '* && -z "$baseline_end" ]]; then baseline_end=$SECONDS; fi
            if [[ "$line" == 'fuzz: minimizing '* ]]; then minimising=true; fi
            if [[ "$line" == *'--- PASS: '* || "$line" == *'--- FAIL: '* ]]; then
                if [[ -n "$baseline_end" && -z "$mutation_end" ]]; then mutation_end=$SECONDS; fi
            fi
        else
            read_status=$?
            partial+="$line"
            # A timed read resumes the loop so the coordinator receives its interrupt.
            (( read_status > 128 )) || break
        fi
    done
    [[ -z "$partial" ]] || printf '%s' "$partial" >> "$log"
    if [[ -n "$baseline_end" ]]; then
        printf 'setup_seconds=%s\nmutation_and_minimisation_seconds=%s\ncleanup_seconds=%s\n' \
            "$((baseline_end - began))" "$((${mutation_end:-SECONDS} - baseline_end))" "$((SECONDS - ${mutation_end:-SECONDS}))" > "$timing"
    else
        printf 'setup_seconds=%s\nmutation_and_minimisation_seconds=0\ncleanup_seconds=0\n' "$((SECONDS - began))" > "$timing"
    fi
    printf 'target_reached=%s\nmutation_budget_completed=%s\n' "$reached" "$completed" >> "$timing"
    [[ "$stopped" == false ]]
}

# seagull::fuzz::audit checks seed reach before mutation begins.
# Mutation runs also gate on the target's own seeds.
seagull::fuzz::audit() {
    local engine="$1" package="$2" function="$3" module="$4" artefacts="$5"
    local action="$6" name="$7" tags="$8" timeout="$9" audit engine_name metadata
    local -a arguments
    export fuzz_audit_status=seed-audit-failed
    if [[ -n "$engine" ]]; then
        engine_name="$engine"
        [[ "$engine_name" != sqlite_cgo ]] || engine_name=sqlite
        audit="$artefacts/$engine_name-seed-audit.log"
    fi
    if [[ -n "$engine" && ! -f "$audit" ]]; then
        arguments=(test -count=1 -v -timeout=10m '-run=^TestFuzzCorpusAudit$' "./internal/dialect/$engine_name")
        metadata="${audit%.log}-metadata"
        mkdir -p "$metadata"
        export fuzz_active_metadata="$metadata"
        (cd "$SEAGULL_ROOT" && export SEAGULL_FUZZ_METADATA_DIR="$metadata" &&
            exec "$fuzz_tool" command "$GO" "${arguments[@]}") > "$audit" 2>&1 &
        fuzz_pipeline_pid=$!
        if ! wait "$fuzz_pipeline_pid"; then
            if [[ -f "$metadata/outcome.txt" ]]; then read -r fuzz_audit_status < "$metadata/outcome.txt"; fi
            tail -n 25 "$audit"
            return 1
        fi
        grep -q 'structured coverage for ' "$audit" || { printf 'Seed audit reached no structured feature oracle.\n' >&2; return 1; }
    fi
    if [[ "$action" == run || "$action" == campaign ]]; then
        seagull::fuzz::seed_gate "$package" "$function" "$module" "$artefacts" "$name" "$tags" "$timeout"
    fi
}

# seagull::fuzz::seed_gate runs a target's seeds before its mutation phase.
seagull::fuzz::seed_gate() {
    local package="$1" function="$2" module="$3" artefacts="$4" name="$5" tags="$6" timeout="$7"
    local audit="$artefacts/${name//\//-}-seed-audit.log" metadata outcome
    local -a arguments=(test -count=1 -v "-timeout=$timeout" "-run=^${function}$")
    [[ -z "$tags" ]] || arguments+=(-tags "$tags")
    arguments+=("$package")
    metadata="${audit%.log}-metadata"
    mkdir -p "$metadata"
    export fuzz_active_metadata="$metadata"
    (cd "$SEAGULL_ROOT/$module" && export SEAGULL_FUZZ_METADATA_DIR="$metadata" &&
        exec "$fuzz_tool" command "$GO" "${arguments[@]}") > "$audit" 2>&1 &
    fuzz_pipeline_pid=$!
    if wait "$fuzz_pipeline_pid"; then
        grep -Fq "=== RUN   $function" "$audit" || { printf 'Seed audit reached no registered target.\n' >&2; return 1; }
        return 0
    fi
    if [[ -f "$metadata/outcome.txt" ]]; then
        read -r outcome < "$metadata/outcome.txt"
        export fuzz_audit_status="$outcome"
    fi
    tail -n 25 "$audit"
    return 1
}
