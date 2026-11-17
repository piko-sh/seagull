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

# Run local fuzz seeds and campaigns without changing CI or golden files.
#
# Usage:
#   make test-fuzz ARGS="list"
#   make test-fuzz ARGS="seeds --lane fast"
#   make test-fuzz ARGS="run --target postgres/generate --time 10m"
#   make test-fuzz ARGS="campaign --lane database --engine sqlite --time 30s"
#   make test-fuzz ARGS="campaign --stage B --dry-run yes"
#   make test-fuzz ARGS="stress --engine sqlite --rounds 2"

# shellcheck source=../lib/init.sh
source "$(dirname "$0")/../lib/init.sh"

# shellcheck source=../lib/fuzz.sh
source "$(dirname "$0")/../lib/fuzz.sh"

declare -a TARGETS=()

# register appends an explicit target to the local campaign registry.
register() {
    TARGETS+=("$1|$2|$3|$4|$5|$6|$7|$8|${9:-go}")
}

register "sqllex/split" . ./internal/sqllex FuzzSplit fast "" "" 2
register "directive/syntax" . ./internal/directive/syntax FuzzDirective fast "" "" 2
register "directive/format" . ./internal/directive/syntax FuzzFormatIsIdempotent fast "" "" 2
for engine in postgres sqlite mysql mariadb duckdb clickhouse cockroachdb timescaledb; do
    register "$engine/generate" . "./internal/dialect/$engine" FuzzGenerate fast "$engine" "" 2
    register "$engine/files" . "./internal/dialect/$engine" FuzzGenerateFiles fast "$engine" "" 2
    register "$engine/structured" . "./internal/dialect/$engine" FuzzStructuredQuery fast "$engine" "" 2
    register "$engine/lexical" . "./internal/dialect/$engine" FuzzLexicalProfile fast "$engine" "" 2
done
for emitter in databasesql pgx clickhouse; do
    register "$emitter/emit" . "./internal/emit/$emitter" FuzzEmit fast "" "" 2
done
for engine in sqlite postgres duckdb clickhouse mysql mariadb cockroachdb timescaledb sqlite_cgo; do
    case "$engine" in
        sqlite) prerequisites=go ;;
        postgres) prerequisites=go,embedded-postgres ;;
        duckdb|sqlite_cgo) prerequisites=go,cgo ;;
        *) prerequisites=go,docker ;;
    esac
    register "$engine/query" "tests/integration/$engine" . FuzzQueryEquivalence database "$engine" integration 1 "$prerequisites"
    register "$engine/count" "tests/integration/$engine" . FuzzCount database "$engine" integration 1 "$prerequisites"
    register "$engine/types" "tests/integration/$engine" . FuzzResultTypes database "$engine" integration 1 "$prerequisites"
    register "$engine/native-values" "tests/integration/$engine" . FuzzGeneratedNativeValues database "$engine" integration 1 "$prerequisites"
    register "$engine/native-shapes" "tests/integration/$engine" . FuzzGeneratedNativeShapes database "$engine" integration 1 "$prerequisites"
    register "$engine/dml-sequence" "tests/integration/$engine" . FuzzDMLSequence database "$engine" integration 1 "$prerequisites"
    register "$engine/schema-equivalence" "tests/integration/$engine" . FuzzSchemaEquivalence database "$engine" integration 1 "$prerequisites"
    register "$engine/workflow-soak" "tests/integration/$engine" . TestFuzzWorkflowSoak soak "$engine" integration 1 "$prerequisites"
    register "$engine/golden-sweep" "tests/integration/$engine" . TestGoldenNullSweep gate "$engine" integration 1 "$prerequisites"
    register "$engine/function-spec" "tests/integration/$engine" . TestFunctionSpecAudit gate "$engine" integration 1 "$prerequisites"
    register "$engine/migration-kill" "tests/integration/$engine" . TestMigrationKill stress "$engine" integration 1 "$prerequisites"
    # ClickHouse has no migration lock, so its suite skips the concurrent migrator check.
    [[ "$engine" == clickhouse ]] ||
        register "$engine/concurrent-migrators" "tests/integration/$engine" . TestConcurrentMigrators stress "$engine" integration 1 "$prerequisites"
    register "$engine/migration-execution" "tests/integration/$engine" . FuzzMigrationExecution database "$engine" integration 1 "$prerequisites"
    register "$engine/migrations" "tests/integration/$engine" . FuzzMigrationReplay database "$engine" integration 1 "$prerequisites"
    register "$engine/migration-lifecycle" "tests/integration/$engine" . FuzzMigrationLifecycle database "$engine" integration 1 "$prerequisites"
    register "$engine/native-builder" "tests/integration/$engine" . FuzzGeneratedBuilder database "$engine" integration 1 "$prerequisites"
    register "$engine/native-typed" "tests/integration/$engine" . FuzzGeneratedValues database "$engine" integration 1 "$prerequisites"
    register "$engine/native-modes" "tests/integration/$engine" . FuzzNativeModes database "$engine" integration 1 "$prerequisites"
    register "$engine/query-modes" "tests/integration/$engine" . FuzzQueryModes database "$engine" integration 1 "$prerequisites"
    register "$engine/dml-model" "tests/integration/$engine" . FuzzDMLModel database "$engine" integration 1 "$prerequisites"
    register "$engine/schema-history" "tests/integration/$engine" . FuzzSchemaHistory database "$engine" integration 1 "$prerequisites"
    register "$engine/semantic-relations" "tests/integration/$engine" . FuzzSemanticRelations database "$engine" integration 1 "$prerequisites"
    case "$engine" in
        sqlite|sqlite_cgo|duckdb) ;;
        *) register "$engine/cli-migrate" "tests/integration/$engine" . FuzzCLIMigrate database "$engine" integration 1 "$prerequisites" ;;
    esac
done

register "cli/workflow" cmd/sgl ./cli FuzzCLIWorkflow workflow "" "" 1 go
register "cli/migrate" cmd/sgl ./cli FuzzCLIMigrate workflow "" "" 1 go

register "generated/compile" tests/integration/compile . FuzzGeneratedCompile compile "" integration 1 go,cgo,cached-modules
register "generated/runtime" tests/integration/compile . FuzzGeneratedRuntime runtime "" integration 1 go,cgo,cached-modules

action="${1:-list}"
[[ $# -eq 0 ]] || shift
target_filter=""
lane_filter=""
engine_filter=""
duration=10m
worker_override=""
explicit_time=false
soak_rounds=500
cleanup_scope=cache
cleanup_apply=no
reduce_input=""
resume_dir=""
intake_source=""
intake_manifest=""
campaign_stage=""
stage_title=""
stage_targets=""
dry_run=no
fuzz_min_free_mb="${SEAGULL_FUZZ_MIN_FREE_MB:-5120}"
fuzz_max_disk_mb="${SEAGULL_FUZZ_MAX_DISK_MB:-8192}"
seen_options=" "
while [[ $# -gt 0 ]]; do
    [[ $# -ge 2 ]] || { printf 'Missing value for %s\n' "$1" >&2; exit 2; }
    if [[ "$seen_options" == *" $1 "* ]]; then printf 'Duplicate option %s\n' "$1" >&2; exit 2; fi
    seen_options+="$1 "
    case "$1" in
        --target) target_filter="$2" ;;
        --lane) lane_filter="$2" ;;
        --engine) engine_filter="$2" ;;
        --time) duration="$2"; explicit_time=true ;;
        --workers) worker_override="$2" ;;
        --rounds) soak_rounds="$2" ;;
        --min-free-mib) fuzz_min_free_mb="$2" ;;
        --max-disk-mib) fuzz_max_disk_mb="$2" ;;
        --scope) cleanup_scope="$2" ;;
        --apply) cleanup_apply="$2" ;;
        --input) reduce_input="$2" ;;
        --resume) resume_dir="$2" ;;
        --source) intake_source="$2" ;;
        --manifest) intake_manifest="$2" ;;
        --stage) campaign_stage="$2" ;;
        --dry-run) dry_run="$2" ;;
        *) printf 'Unknown option %s\n' "$1" >&2; exit 2 ;;
    esac
    shift 2
done
case "$action" in list|seeds|run|campaign|soak|stress|clean|reduce|intake) ;; *) printf 'Unknown action %s\n' "$action" >&2; exit 2 ;; esac
[[ "$dry_run" == yes || "$dry_run" == no ]] || { printf '%s\n' '--dry-run requires yes or no.' >&2; exit 2; }
case "$action" in seeds|run|campaign|soak|stress) ;; *)
    if [[ "$seen_options" == *" --dry-run "* ]]; then printf '%s does not accept --dry-run.\n' "$action" >&2; exit 2; fi ;;
esac
# A stage fixes the action, lanes and budgets of a validation campaign.
if [[ -n "$campaign_stage" ]]; then
    [[ "$action" == campaign ]] || { printf '%s\n' '--stage requires the campaign action.' >&2; exit 2; }
    for option in --time --rounds --workers --lane; do
        if [[ "$seen_options" == *" $option "* ]]; then printf 'A stage sets its own budgets, so it does not accept %s.\n' "$option" >&2; exit 2; fi
    done
    case "$campaign_stage" in
        A) action=seeds; stage_title='Stage A: seed gates, the golden sweep and the function-spec audit on every target' ;;
        B) duration=10m; stage_title='Stage B: 10-minute mutation for every mutation target, one target at a time' ;;
        C) duration=1h; stage_targets='/(native-values|native-typed|native-builder|dml-model)$'
            stage_title='Stage C: 1 hour of native values, typed values, version 16 builder and version 17 DML mutation per configuration' ;;
        D) action=soak; soak_rounds=500; stage_title='Stage D: 500 workflow soak rounds per configuration' ;;
        E) action=stress; soak_rounds=200; stage_title='Stage E: 200 runs of each concurrency and kill check per engine' ;;
        *) printf 'Stage must be A, B, C, D or E.\n' >&2; exit 2 ;;
    esac
elif [[ "$action" == stress && "$seen_options" != *" --rounds "* ]]; then
    soak_rounds=200
fi
if [[ "$action" == intake ]]; then
    [[ -d "$intake_source" && -f "$intake_manifest" ]] || { printf 'intake requires --source DIR and --manifest FILE.\n' >&2; exit 2; }
    [[ "$seen_options" =~ ^\ (\-\-(source|manifest)\ )+$ ]] || { printf 'intake accepts only --source and --manifest.\n' >&2; exit 2; }
    seagull::fuzz::tool || exit 1
    # The tool names the output directory after the manifest's suite.
    exec "${fuzz_tool:?}" intake --source "$intake_source" --manifest "$intake_manifest" --output-root "${SEAGULL_ROOT}/testdata/external"
fi
if [[ "$action" != intake && ( -n "$intake_source" || -n "$intake_manifest" ) ]]; then
    printf '%s\n' '--source and --manifest require the intake action.' >&2
    exit 2
fi
if [[ "$action" == reduce && ( -z "$target_filter" || -z "$reduce_input" ) ]]; then
    printf 'reduce requires --target and --input.\n' >&2
    exit 2
fi
if [[ -n "$resume_dir" && "$action" != campaign && "$action" != seeds && "$action" != run && -z "$campaign_stage" ]]; then
    printf '%s\n' '--resume requires the campaign, seeds or run action.' >&2
    exit 2
fi
if [[ -n "$resume_dir" && ! -f "$resume_dir/registry.txt" ]]; then
    printf 'No campaign to resume in %s\n' "$resume_dir" >&2
    exit 2
fi
if [[ "$action" != reduce && -n "$reduce_input" ]]; then
    printf '%s\n' '--input requires the reduce action.' >&2
    exit 2
fi
if [[ "$action" == run && -z "$target_filter" ]]; then
    printf 'run requires --target. Use list to inspect targets.\n' >&2
    exit 2
fi
if [[ "$action" == list || "$action" == seeds || "$action" == soak || "$action" == stress ]]; then
    if [[ "$explicit_time" == true || -n "$worker_override" ]]; then
        printf '%s does not accept --time or --workers.\n' "$action" >&2
        exit 2
    fi
fi
if [[ "$action" != soak && "$action" != stress && "$seen_options" == *" --rounds "* ]]; then
    printf '%s\n' '--rounds requires the soak or stress action.' >&2
    exit 2
fi
if [[ ! "$soak_rounds" =~ ^[1-9][0-9]{0,3}$ ]] || (( soak_rounds > 1000 )); then
    printf 'Soak rounds must be between 1 and 1000.\n' >&2
    exit 2
fi
if [[ -n "$worker_override" && ! "$worker_override" =~ ^[1-9][0-9]*$ ]]; then
    printf 'Workers must be a positive integer.\n' >&2
    exit 2
fi
if [[ ! "$duration" =~ ^([1-9][0-9]{0,5})(s|m|h)$ ]]; then
    printf 'Time must be a positive integer followed by s, m or h.\n' >&2
    exit 2
fi
seconds="${BASH_REMATCH[1]}"
case "${BASH_REMATCH[2]}" in m) seconds=$((seconds * 60)) ;; h) seconds=$((seconds * 3600)) ;; esac
if (( seconds > 86400 )); then printf 'A target may run for at most 24 hours.\n' >&2; exit 2; fi
timeout="$((seconds + 1200))s"
if [[ "$action" == soak ]]; then timeout="$((soak_rounds * 120 + 1200))s"; fi
# A kill run makes six killed migrations of up to two minutes each.
if [[ "$action" == stress ]]; then timeout="$((soak_rounds * 720 + 1200))s"; fi

if [[ "$action" == clean ]]; then
    [[ "$cleanup_apply" == yes || "$cleanup_apply" == no ]] || { printf '%s\n' '--apply requires yes or no.' >&2; exit 2; }
    [[ "$seen_options" == " " || "$seen_options" =~ ^\ (\-\-(scope|apply)\ )+$ ]] || { printf 'clean accepts only --scope and --apply.\n' >&2; exit 2; }
    fuzz_modules=$("$GO" env GOMODCACHE)
    seagull::fuzz::storage
    seagull::fuzz::clean "$cleanup_scope" "$cleanup_apply"
    exit
fi
if [[ "$seen_options" == *" --scope "* || "$seen_options" == *" --apply "* ]]; then
    printf 'Cleanup options require the clean action.\n' >&2
    exit 2
fi

selected=()
for entry in "${TARGETS[@]}"; do
    IFS='|' read -r name module package function lane engine tags workers prerequisites <<< "$entry"
    [[ -z "$target_filter" || "$name" == "$target_filter" ]] || continue
    [[ -z "$lane_filter" || "$lane" == "$lane_filter" ]] || continue
    [[ -z "$engine_filter" || "$engine" == "$engine_filter" ]] || continue
    [[ -z "$stage_targets" || "$name" =~ $stage_targets ]] || continue
    if [[ "$action" == soak && "$lane" != soak ]]; then continue; fi
    if [[ "$action" == stress && "$lane" != stress ]]; then continue; fi
    if [[ "$action" == seeds && "$lane" == stress ]]; then
        if [[ -n "$target_filter" ]]; then printf 'Concurrency and kill targets require the stress action.\n' >&2; exit 2; fi
        continue
    fi
    if [[ ( "$lane" == soak || "$lane" == gate || "$lane" == stress ) && ( "$action" == run || "$action" == campaign ) ]]; then
        if [[ -n "$target_filter" ]]; then
            case "$lane" in
                soak) printf 'Workflow targets require the soak action.\n' >&2 ;;
                gate) printf 'Gate targets run only as seeds.\n' >&2 ;;
                *) printf 'Concurrency and kill targets require the stress action.\n' >&2 ;;
            esac
            exit 2
        fi
        continue
    fi
    if [[ "$name" == */native-* && -n "$worker_override" && "$worker_override" != 1 ]]; then
        printf 'Native generated targets require one worker per managed program cache.\n' >&2
        exit 2
    fi
    selected+=("$entry")
done
[[ ${#selected[@]} -gt 0 ]] || { printf 'No registered targets match.\n' >&2; exit 2; }
if [[ "$action" == reduce ]]; then
    [[ ${#selected[@]} -eq 1 ]] || { printf 'reduce needs exactly one target.\n' >&2; exit 2; }
    [[ -f "$reduce_input" ]] || { printf 'No input file %s\n' "$reduce_input" >&2; exit 2; }
    IFS='|' read -r name module package function lane engine tags workers prerequisites <<< "${selected[0]}"
    fuzz_modules=$("$GO" env GOMODCACHE)
    seagull::fuzz::storage
    reduction="${fuzz_root:?}/reductions/${name//\//-}-$(date +%Y%m%d-%H%M%S)"
    mkdir -p "$(dirname "$reduction")"
    exec "$fuzz_tool" reduce --go "$GO" --module "${SEAGULL_ROOT}/$module" \
        --package "$package" --function "$function" --tags "$tags" --input "$(realpath "$reduce_input")" \
        --output "$reduction"
fi
if [[ "$dry_run" == yes ]]; then
    [[ -z "$stage_title" ]] || printf '%s.\n' "$stage_title"
    printf '%-9s %-10s %-8s %s\n' MODE BUDGET WORKERS TARGET
    mutation_total=0
    for entry in "${selected[@]}"; do
        IFS='|' read -r name module package function lane engine tags workers prerequisites <<< "$entry"
        case "$action" in
            seeds) mode=seeds; budget=- ;;
            soak) mode=soak; budget="${soak_rounds}r" ;;
            stress) mode=stress; budget="${soak_rounds}x" ;;
            *) mode=mutation; budget="$duration"; mutation_total=$((mutation_total + seconds)) ;;
        esac
        printf '%-9s %-10s %-8s %s\n' "$mode" "$budget" "${worker_override:-$workers}" "$name"
    done
    printf 'Targets: %s, run one at a time.\n' "${#selected[@]}"
    (( mutation_total == 0 )) || printf 'Mutation budget: %s hours, after each target'"'"'s seed gate.\n' "$((mutation_total / 3600))"
    printf 'Stops when managed storage reaches %s MiB or free space falls below %s MiB.\n' "$fuzz_max_disk_mb" "$fuzz_min_free_mb"
    exit 0
fi
if [[ "$action" == list ]]; then
    printf '%-28s %-10s %-15s %-24s %s\n' TARGET LANE ENGINE FUNCTION PREREQUISITES
    for entry in "${selected[@]}"; do
        IFS='|' read -r name module package function lane engine tags workers prerequisites <<< "$entry"
        printf '%-28s %-10s %-15s %-24s %s\n' "$name" "$lane" "$engine" "$function" "$prerequisites"
    done
    exit 0
fi

if [[ ! "$fuzz_min_free_mb" =~ ^[1-9][0-9]{0,6}$ || ! "$fuzz_max_disk_mb" =~ ^[1-9][0-9]{0,6}$ ]]; then
    printf 'Resource limits must be positive MiB counts.\n' >&2
    exit 2
fi
fuzz_cache=$("$GO" env GOCACHE)
fuzz_modules=$("$GO" env GOMODCACHE)
fuzz_temp=$("$GO" env GOTMPDIR)
fuzz_temp="${fuzz_temp:-${TMPDIR:-/tmp}}"
seagull::fuzz::space "$fuzz_min_free_mb" "$fuzz_cache" "$fuzz_modules" "$fuzz_temp"
seagull::fuzz::storage
seagull::fuzz::resource_check

if [[ -n "$resume_dir" ]]; then
    artefacts="$(cd "$resume_dir" && pwd -P)"
    printf 'Resuming campaign: %s\n' "$artefacts"
else
    artefacts="$(mktemp -d "${fuzz_root:?}/runs/campaign.XXXXXXXX")"
fi
printf 'Campaign logs: %s\n' "$artefacts"
"${GO}" version > "$artefacts/toolchain.txt"
# A tree without git still runs, and records that its revision is unknown.
git -C "$SEAGULL_ROOT" rev-parse HEAD > "$artefacts/revision.txt" 2>/dev/null || printf 'unknown\n' > "$artefacts/revision.txt"
git -C "$SEAGULL_ROOT" status --short > "$artefacts/working-tree.txt" 2>/dev/null || printf 'unknown\n' > "$artefacts/working-tree.txt"
printf '%s\n' "${selected[@]}" > "$artefacts/registry.txt"
printf 'minimum_free_mib=%s\nmanaged_stop_mib=%s\nmanaged_root=%s\n' "$fuzz_min_free_mb" "$fuzz_max_disk_mb" "$fuzz_root" > "$artefacts/resources.txt"
failures=0
for entry in "${selected[@]}"; do
    IFS='|' read -r name module package function lane engine tags workers prerequisites <<< "$entry"
    provenance="$artefacts/${name//\//-}-provenance.json"
    provenance_arguments=(--root "$SEAGULL_ROOT" --go "$GO" --target "$name" --module "$module" --package "$package"
        --function "$function" --command "$action" --budget "$duration/$soak_rounds" --workers "${worker_override:-$workers}"
        --file "$provenance")
    if [[ -n "$resume_dir" ]] && "$fuzz_tool" provenance matches "${provenance_arguments[@]}" 2>/dev/null; then
        printf 'Skipping %s: it passed whole with this provenance.\n' "$name"
        continue
    fi
    source_before=$("$fuzz_tool" provenance fingerprint --root "$SEAGULL_ROOT")
    if ! seagull::fuzz::resource_check; then
        printf '%s\tresource-stopped\n' "$name" >> "$artefacts/results.tsv"
        exit 1
    fi
    if ! seagull::fuzz::prerequisites "$prerequisites" "$module"; then
        printf '%s\tsetup-failed\n' "$name" >> "$artefacts/results.tsv"
        exit 1
    fi
    audit_started=$SECONDS
    if ! seagull::fuzz::audit "$engine" "$package" "$function" "$module" "$artefacts" "$action" "$name" "$tags" "$timeout"; then
        printf '%s\t%s\n' "$name" "${fuzz_audit_status:-seed-audit-failed}" >> "$artefacts/results.tsv"
        exit 1
    fi
    audit_seconds=$((SECONDS - audit_started))
    if ! seagull::fuzz::resource_check; then
        printf '%s\tresource-stopped\n' "$name" >> "$artefacts/results.tsv"
        exit 1
    fi
    mutation_budget=0
    count=1
    [[ "$action" != stress ]] || count="$soak_rounds"
    arguments=(test "-count=$count" -v "-timeout=$timeout")
    [[ -z "$tags" ]] || arguments+=(-tags "$tags")
    if [[ "$action" == seeds || "$action" == soak || "$action" == stress ]]; then
        arguments+=("-run=^${function}$")
    else
        mutation_budget=$seconds
        arguments+=('-run=^$' "-fuzz=^${function}$" "-exec=bash \"$SEAGULL_LIB_DIR/fuzz-exec.sh\"" "-parallel=${worker_override:-$workers}" -fuzzminimizetime=30s)
    fi
    arguments+=("$package")
    log="$artefacts/${name//\//-}.log"
    timing="$artefacts/${name//\//-}-timing.txt"
    metadata="$artefacts/${name//\//-}-metadata"
    # A resumed target starts from fresh metadata, so a stale outcome cannot decide its status.
    rm -rf -- "$metadata"
    mkdir "$metadata"
    case "$lane" in
        database|soak) ;;
        gate|stress) printf 'This check records no engine version.\n' > "$metadata/engine.txt" ;;
        *) printf 'No database server is used by this target.\n' > "$metadata/engine.txt" ;;
    esac
    printf 'Running %s (%s).\n' "$name" "$module"
    printf '%q ' "${GO}" "${arguments[@]}" > "$log"
    printf '\n' >> "$log"
    started=$SECONDS
    export fuzz_active_metadata="$metadata"
    export SEAGULL_FUZZ_SOAK_ROUNDS=1
    if [[ "$action" == soak ]]; then export SEAGULL_FUZZ_SOAK_ROUNDS="$soak_rounds"; fi
    (cd "${SEAGULL_ROOT}/$module" && printf '%s\n' "$BASHPID" > "$metadata/launcher.pid" &&
        export SEAGULL_FUZZ_METADATA_DIR="$metadata" SEAGULL_FUZZ_COORDINATOR_PID="$metadata/coordinator.pid" && exec "$fuzz_tool" command "${GO}" "${arguments[@]}") 2>&1 |
        seagull::fuzz::capture "$log" "$timing" "$started" "$function" "$mutation_budget" "$metadata/coordinator.pid" &
    fuzz_pipeline_pid=$!
    if wait "$fuzz_pipeline_pid"; then
        status=passed
    else
        status=failed
    fi
    if ! grep -q '^target_reached=true$' "$timing"; then status=failed; fi
    if [[ "$action" == run || "$action" == campaign ]] && ! grep -q 'now fuzzing with ' "$log"; then
        printf 'The target never reached mutation.\n' >> "$log"
        status=failed
    fi
    if [[ "$action" == run || "$action" == campaign ]] && ! grep -q '^mutation_budget_completed=true$' "$timing"; then
        printf 'The target did not complete its requested mutation budget.\n' >> "$log"
        status=failed
    fi
    if [[ "$lane" == soak ]] && ! grep -Fq "workflow-soak completed $SEAGULL_FUZZ_SOAK_ROUNDS rounds" "$log"; then
        printf 'The workflow did not complete every requested round.\n' >> "$log"
        status=failed
    fi
    if [[ "$lane" == stress ]]; then
        passes=$(grep -c "^--- PASS: $function " "$log" || true)
        if [[ "$passes" != "$soak_rounds" ]] || grep -q "^--- SKIP: $function " "$log"; then
            printf 'The check passed %s of %s requested runs.\n' "$passes" "$soak_rounds" >> "$log"
            status=failed
        fi
    fi
    engine_name="$engine"
    [[ "$engine_name" != sqlite_cgo ]] || engine_name=sqlite
    if [[ ( "$lane" == database || "$lane" == soak ) && -z $(find "$metadata" -name "$engine_name-*.json" -print -quit) ]]; then
        printf 'The target recorded no database engine version.\n' >> "$log"
        status=failed
    fi
    printf 'seed_audit_seconds=%s\nwall_seconds=%s\n' "$audit_seconds" "$((SECONDS - started))" >> "$timing"
    if [[ -f "$metadata/outcome.txt" ]]; then read -r status < "$metadata/outcome.txt"; fi
    budget_completed=false
    if [[ "$action" == seeds || "$action" == soak || "$action" == stress ]] || grep -q '^mutation_budget_completed=true$' "$timing"; then budget_completed=true; fi
    engine_identity=$(find "$metadata" -name "*.json" -exec cat {} + 2>/dev/null | sha256sum | cut -d' ' -f1)
    "$fuzz_tool" provenance record "${provenance_arguments[@]}" --status "$status" \
        --budget-completed "$budget_completed" --source-before "$source_before" --engine "$engine_identity"
    if ! "$fuzz_tool" provenance unchanged --file "$provenance" 2>/dev/null; then
        printf 'The source changed while the target ran, so its result is not trusted.\n' >> "$log"
        status=source-changed
    fi
    if [[ "$status" != passed ]]; then failures=$((failures + 1)); tail -n 25 "$log"; fi
    printf '%s\t%s\t%ss\t%s\n' "$name" "$status" "$((SECONDS - started))" "$log" | tee -a "$artefacts/results.tsv"
    [[ "$status" != resource-stopped ]] || exit 1
    if [[ -n "$campaign_stage" && "$status" != passed ]]; then
        printf 'Stage %s stopped at %s. Triage the finding, then rerun with --resume %s.\n' "$campaign_stage" "$name" "$artefacts" >&2
        exit 1
    fi
done
[[ "$failures" -eq 0 ]]
