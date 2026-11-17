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

# seagull::fuzz::resource_check checks managed storage and affected filesystems.
seagull::fuzz::resource_check() {
    local used previous=0
    seagull::fuzz::space "${fuzz_min_free_mb:?}" "$fuzz_cache" "${fuzz_modules:?}" "$fuzz_temp" || return 1
    used=$("${fuzz_tool:?}" disk-usage "$fuzz_root") || return 1
    [[ "$used" =~ ^[0-9]+$ ]] || return 1
    if [[ -n "${artefacts:-}" ]]; then
        [[ ! -f "$artefacts/peak-disk-kib.txt" ]] || read -r previous < "$artefacts/peak-disk-kib.txt"
        if (( used > previous )); then printf '%s\n' "$used" > "$artefacts/peak-disk-kib.txt"; fi
    fi
    if (( used >= ${fuzz_max_disk_mb:?} * 1024 )); then
        printf 'Managed fuzz storage reached %s MiB. The stop threshold is %s MiB.\n' "$((used / 1024))" "$fuzz_max_disk_mb" >&2
        return 1
    fi
}

# seagull::fuzz::storage creates an exclusively owned campaign workspace.
seagull::fuzz::storage() {
    local requested="${SEAGULL_FUZZ_ROOT:-$SEAGULL_ROOT/.cache/seagull-fuzz}" parent child
    seagull::fuzz::tool || return 1
    if [[ "$requested" != /* ]]; then requested="$SEAGULL_ROOT/$requested"; fi
    parent="$requested"
    while [[ "$parent" != / ]]; do
        [[ ! -L "$parent" ]] || { printf 'Fuzz storage must not traverse a symlink: %s\n' "$parent" >&2; return 1; }
        parent=$(dirname "$parent")
    done
    mkdir -p "$requested"
    fuzz_root=$(cd "$requested" && pwd -P)
    if [[ ! -f "$fuzz_root/.seagull-fuzz-owner" ]]; then
        if [[ -n $(find "$fuzz_root" -mindepth 1 -maxdepth 1 -print -quit) ]]; then
            printf 'Refusing to claim a non-empty fuzz workspace: %s\n' "$fuzz_root" >&2
            return 1
        fi
        printf 'seagull-fuzz-v1\n' > "$fuzz_root/.seagull-fuzz-owner"
    fi
    [[ $(cat "$fuzz_root/.seagull-fuzz-owner") == seagull-fuzz-v1 ]] || return 1
    if ! mkdir "$fuzz_root/active" 2>/dev/null; then
        printf 'A campaign owns %s/active. Inspect its owner before removing a stale lock.\n' "$fuzz_root" >&2
        return 1
    fi
    printf '%s\n' "$$" > "$fuzz_root/active/pid"
    trap 'rm -f "$fuzz_root/active/pid"; rmdir "$fuzz_root/active"' EXIT
    trap 'seagull::fuzz::cancel; exit 130' INT
    trap 'seagull::fuzz::cancel; exit 143' TERM
    for child in cache tmp runs programs; do
        [[ ! -L "$fuzz_root/$child" ]] || { printf 'Refusing a symlink in managed storage: %s\n' "$child" >&2; return 1; }
    done
    mkdir -p "$fuzz_root/cache" "$fuzz_root/tmp" "$fuzz_root/runs" "$fuzz_root/programs"
    fuzz_cache="$fuzz_root/cache"
    fuzz_temp="$fuzz_root/tmp"
    export GOCACHE="$fuzz_cache" GOTMPDIR="$fuzz_temp" TMPDIR="$fuzz_temp"
    export GOPROXY=off
    export SEAGULL_FUZZ_MANAGED_ROOT="$fuzz_root" GOMODCACHE="$fuzz_modules"
    export SEAGULL_FUZZ_MIN_FREE_MB="$fuzz_min_free_mb" SEAGULL_FUZZ_MAX_DISK_MB="$fuzz_max_disk_mb"
}

# seagull::fuzz::interrupt stops only the active command owned by this campaign.
seagull::fuzz::interrupt() {
    local coordinator="$1" process launcher
    if [[ -s "$coordinator" ]]; then
        read -r process < "$coordinator"
        if [[ "$process" =~ ^[1-9][0-9]*$ ]]; then kill -INT "$process" 2>/dev/null || true; fi
    elif [[ -s "${coordinator%/*}/launcher.pid" ]]; then
        read -r launcher < "${coordinator%/*}/launcher.pid"
        if [[ "$launcher" =~ ^[1-9][0-9]*$ ]]; then kill -TERM "$launcher" 2>/dev/null || true; fi
    fi
}

# seagull::fuzz::clean removes only selected disposable data in an owned workspace.
seagull::fuzz::clean() {
    local scope="$1" apply="$2" child
    local -a directories=()
    case "$scope" in
        cache) directories=(cache programs) ;;
        temporary) directories=(tmp) ;;
        all) directories=(cache programs tmp) ;;
        *) printf 'Cleanup scope must be cache, temporary or all.\n' >&2; return 2 ;;
    esac
    if [[ "$scope" != temporary && -e "$fuzz_root/programs/lease" ]]; then
        printf 'Compiled programs are leased by a worker. Inspect the lease before cleanup.\n' >&2
        return 1
    fi
    for child in "${directories[@]}"; do
        du -sh "$fuzz_root/$child"
        if [[ "$apply" == yes ]]; then
            find "$fuzz_root/$child" -xdev -mindepth 1 -delete
        fi
    done
    if [[ "$apply" != yes ]]; then printf 'Preview only. Add --apply yes to remove this disposable data.\n'; fi
}

# seagull::fuzz::cancel waits for owned command cleanup before releasing the lock.
seagull::fuzz::cancel() {
    local process
    if [[ -n "${fuzz_active_metadata:-}" && -s "$fuzz_active_metadata/launcher.pid" ]]; then
        printf 'interrupted\n' > "$fuzz_active_metadata/outcome.txt"
        read -r process < "$fuzz_active_metadata/launcher.pid"
        if [[ "$process" =~ ^[1-9][0-9]*$ ]]; then kill -TERM "$process" 2>/dev/null || true; fi
    fi
    if [[ -n "${fuzz_pipeline_pid:-}" ]]; then wait "$fuzz_pipeline_pid" 2>/dev/null || true; fi
}
