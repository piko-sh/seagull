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

# Test the fuzz tool's packages, then exercise runner failure handling without starting databases.
set -euo pipefail

repository="$(cd "$(dirname "$0")/../.." && pwd)"
(cd "$repository" && "${GO:-go}" test -count=1 ./cmd/internal/fuzztool/...)

runner="$(cd "$(dirname "$0")" && pwd)/fuzz.sh"
fixture=$(mktemp -d "${TMPDIR:-/tmp}/seagull-runner-test.XXXXXXXX")
trap 'rm -rf "$fixture"' EXIT
mkdir "$fixture/cache" "$fixture/modules" "$fixture/artefacts"
# The runner uses this build, since GO below is a stub.
(cd "$repository" && "${GO:-go}" build -o "$fixture/fuzztool" ./cmd/internal/fuzztool)
export SEAGULL_FUZZ_TOOL="$fixture/fuzztool"
export SEAGULL_RUNNER_FIXTURE="$fixture"
export SEAGULL_FUZZ_ROOT="$fixture/managed"
export TMPDIR="$fixture/artefacts"
export GO="$fixture/go"
cat > "$GO" <<'STUB'
#!/bin/bash
set -eu
case "$1" in
    version) printf 'go version fixture\n'; exit ;;
    env)
        case "$2" in
            -json) printf '{"GOOS": "fixture", "GOARCH": "fixture"}\n' ;;
            GOCACHE) printf '%s/cache\n' "$SEAGULL_RUNNER_FIXTURE" ;;
            GOMODCACHE) printf '%s/modules\n' "$SEAGULL_RUNNER_FIXTURE" ;;
            GOTMPDIR) printf '%s\n' "$TMPDIR" ;;
            CGO_ENABLED) printf '0\n' ;;
            CC) printf 'cc\n' ;;
        esac
        exit ;;
esac
printf '%s\n' "$*" >> "$SEAGULL_RUNNER_FIXTURE/commands"
if [[ "${SEAGULL_RUNNER_MODE:-}" == fail ]]; then exit 1; fi
if [[ "${SEAGULL_RUNNER_MODE:-}" == empty ]]; then printf 'no tests to run\n'; exit; fi
if [[ "$*" == *TestFuzzCorpusAudit* ]]; then printf 'structured coverage for sqlite\n'; exit; fi
if [[ "${SEAGULL_RUNNER_MODE:-}" == audit-resource && "$*" != *'-fuzz='* ]]; then
    dd if=/dev/zero of="$SEAGULL_FUZZ_ROOT/oversized" bs=1024 count=2048 2>/dev/null
    sleep 20
fi
if [[ "$*" == *TestMigrationKill* || "$*" == *TestConcurrentMigrators* ]]; then
    check=TestMigrationKill
    [[ "$*" != *TestConcurrentMigrators* ]] || check=TestConcurrentMigrators
    runs=$(sed -n 's/.*-count=\([0-9]*\).*/\1/p' <<< "$*")
    [[ "${SEAGULL_RUNNER_MODE:-}" != stress-short ]] || runs=$((runs - 1))
    for ((run=0; run<runs; run++)); do
        printf '=== RUN   %s\n' "$check"
        if [[ "${SEAGULL_RUNNER_MODE:-}" == stress-skip ]]; then printf -- '--- SKIP: %s (0.00s)\n' "$check"; continue; fi
        printf -- '--- PASS: %s (0.01s)\n' "$check"
    done
    printf 'PASS\n'
    exit
fi
if [[ "$*" == *TestGoldenNullSweep* || "$*" == *TestFunctionSpecAudit* ]]; then
    printf '=== RUN   %s\n' "$(sed -n 's/.*-run=^\([A-Za-z]*\)\$.*/\1/p' <<< "$*")"
    printf 'PASS\n'
    exit
fi
if [[ "$*" == *TestFuzzWorkflowSoak* ]]; then
    printf '=== RUN   TestFuzzWorkflowSoak\n'
    printf '{"Engine":"sqlite","Version":"fixture","Process":%s}\n' "$$" > "$SEAGULL_FUZZ_METADATA_DIR/sqlite-fixture.json"
    if [[ "${SEAGULL_RUNNER_MODE:-}" != incomplete-soak ]]; then
        printf 'workflow-soak completed %s rounds\n' "$SEAGULL_FUZZ_SOAK_ROUNDS"
    fi
    printf '%s\n' '--- PASS: TestFuzzWorkflowSoak (0.01s)' 'PASS'
    exit
fi
printf '=== RUN   FuzzSplit\n' 
if [[ "$*" == *'-fuzz='* ]]; then
    # Trap the interrupt before publishing the coordinator so the runner cannot race it.
    stopped=false
    trap 'stopped=true' INT
    [[ "$*" != *'-fuzztime='* ]]
    [[ "$*" == *'-exec=bash '* ]]
    printf '%s\n' "$$" > "$SEAGULL_FUZZ_COORDINATOR_PID"
    [[ "${SEAGULL_RUNNER_MODE:-}" != slow ]] || sleep 2
    printf 'fuzz: elapsed: 1s, gathering baseline coverage: 1/1 completed, now fuzzing with 1 workers\n'
    if [[ "${SEAGULL_RUNNER_MODE:-}" == resource ]]; then
        dd if=/dev/zero of="$SEAGULL_FUZZ_ROOT/oversized" bs=1024 count=2048 2>/dev/null
    fi
    if [[ "${SEAGULL_RUNNER_MODE:-}" == mutation-fail ]]; then
        printf '%s\n' '--- FAIL: FuzzSplit (1.00s)' 'FAIL'
        exit 1
    fi
    if [[ "${SEAGULL_RUNNER_MODE:-}" != early ]]; then
        [[ "${SEAGULL_RUNNER_MODE:-}" != partial ]] || printf 'partial output '
        # The wait builtin returns as soon as the trapped interrupt arrives.
        while [[ "$stopped" == false ]]; do sleep 0.1 & wait $! || true; done
        # Reap the last timer so no child outlives the stub and reads as a failed cleanup.
        wait || true
        [[ "${SEAGULL_RUNNER_MODE:-}" != partial ]] || printf 'survives the timer\n'
    fi
fi
printf '%s\n' '--- PASS: FuzzSplit (1.00s)' 'PASS'
if [[ "${SEAGULL_RUNNER_MODE:-}" == cleanup-fail ]]; then
    printf '%s\n' 'cleanup failed' 'FAIL'
    exit 1
fi
STUB
chmod +x "$GO"

# rejects requires the runner to fail the supplied invocation.
rejects() {
    if bash "$runner" "$@" > "$fixture/rejected.log" 2>&1; then
        printf 'Runner accepted an invalid or unsuccessful command: %s\n' "$*" >&2
        exit 1
    fi
}

bash "$runner" list > "$fixture/registry"
[[ $(wc -l < "$fixture/registry") == 246 ]]
rejects soak --rounds 0
rejects soak --rounds 1001
rejects soak --time 1s
rejects run --target sqlite/workflow-soak
rejects run --target sqllex/split --rounds 1
SEAGULL_RUNNER_MODE=incomplete-soak rejects soak --engine sqlite --rounds 3
bash "$runner" soak --engine sqlite --rounds 3 > "$fixture/soak.log"
soak_artefacts=$(sed -n 's/^Campaign logs: //p' "$fixture/soak.log")
grep -q 'passed' "$soak_artefacts/results.tsv"
grep -Fq 'workflow-soak completed 3 rounds' "$soak_artefacts/sqlite-workflow-soak.log"
rejects run
rejects seeds --time 1s
rejects list --workers 2
rejects run --target absent --time 1s
rejects run --target sqllex/split --time 0s
rejects run --target sqllex/split --time 999999h
rejects run --target sqllex/split --workers 0
rejects run --target sqlite/native-values --workers 2
rejects list --lane fast --lane fast
rejects seeds --target sqlite_cgo/query
SEAGULL_FUZZ_MIN_FREE_MB=9999999 rejects seeds --target sqllex/split
SEAGULL_RUNNER_MODE=empty rejects run --target sqllex/split --time 1s
SEAGULL_RUNNER_MODE=fail rejects seeds --target sqllex/split
# A resumed campaign reruns its failed target from fresh metadata.
failed_campaign=$(sed -n 's/^Campaign logs: //p' "$fixture/rejected.log")
[[ -d "$failed_campaign/sqllex-split-metadata" ]]
bash "$runner" seeds --target sqllex/split --resume "$failed_campaign" > "$fixture/resumed.log"
[[ $(tail -n 1 "$failed_campaign/results.tsv" | cut -f2) == passed ]]
# A failing seed gate stops a mutation run before it mutates.
SEAGULL_RUNNER_MODE=fail rejects run --target sqllex/split --time 1s
artefacts=$(sed -n 's/^Campaign logs: //p' "$fixture/rejected.log")
[[ $(cut -f2 "$artefacts/results.tsv") == seed-audit-failed && ! -e "$artefacts/sqllex-split.log" ]]

SEAGULL_RUNNER_MODE=early rejects run --target sqllex/split --time 1s
SEAGULL_RUNNER_MODE=mutation-fail rejects run --target sqllex/split --time 1s
SEAGULL_RUNNER_MODE=cleanup-fail rejects run --target sqllex/split --time 1s

SEAGULL_RUNNER_MODE=slow bash "$runner" run --target sqllex/split --time 1s > "$fixture/success.log"
artefacts=$(sed -n 's/^Campaign logs: //p' "$fixture/success.log")
[[ -s "$artefacts/sqllex-split-seed-audit.log" ]]
grep -q '^target_reached=true$' "$artefacts/sqllex-split-timing.txt"
grep -q '^setup_seconds=' "$artefacts/sqllex-split-timing.txt"
grep -q '^mutation_and_minimisation_seconds=' "$artefacts/sqllex-split-timing.txt"
grep -q '^mutation_budget_completed=true$' "$artefacts/sqllex-split-timing.txt"
setup=$(sed -n 's/^setup_seconds=//p' "$artefacts/sqllex-split-timing.txt")
mutation=$(sed -n 's/^mutation_and_minimisation_seconds=//p' "$artefacts/sqllex-split-timing.txt")
(( setup >= 2 && mutation >= 1 ))
grep -q 'passed' "$artefacts/results.tsv"
SEAGULL_RUNNER_MODE=partial bash "$runner" run --target sqllex/split --time 1s > "$fixture/partial.log"
artefacts=$(sed -n 's/^Campaign logs: //p' "$fixture/partial.log")
grep -q '^partial output survives the timer$' "$artefacts/sqllex-split.log"
# The storage limit runs start from empty managed roots, so the logs of the earlier runs
# do not count towards the limit.
resource_root="$fixture/resource-managed"
SEAGULL_FUZZ_ROOT="$resource_root" SEAGULL_RUNNER_MODE=resource rejects run --target sqllex/split --time 10s --max-disk-mib 2
artefacts=$(sed -n 's/^Campaign logs: //p' "$fixture/rejected.log")
grep -q 'resource-stopped' "$artefacts/results.tsv"
[[ ! -d "$resource_root/active" ]]
rm "$resource_root/oversized"
mkdir "$SEAGULL_FUZZ_ROOT/active"
rejects seeds --target sqllex/split
rmdir "$SEAGULL_FUZZ_ROOT/active"
ln -s "$fixture/managed" "$fixture/symlink"
SEAGULL_FUZZ_ROOT="$fixture/symlink" rejects seeds --target sqllex/split
audit_resource_root="$fixture/audit-resource-managed"
SEAGULL_FUZZ_ROOT="$audit_resource_root" SEAGULL_RUNNER_MODE=audit-resource rejects run --target sqllex/split --time 1s --max-disk-mib 2
artefacts=$(sed -n 's/^Campaign logs: //p' "$fixture/rejected.log")
grep -q 'resource-stopped' "$artefacts/results.tsv"
rm "$audit_resource_root/oversized"

bash "$runner" run --target sqllex/split --time 1h > "$fixture/interrupted.log" 2>&1 &
runner_pid=$!
coordinator=""
for ((attempt=0; attempt<200; attempt++)); do
    artefacts=$(sed -n 's/^Campaign logs: //p' "$fixture/interrupted.log")
    coordinator="$artefacts/sqllex-split-metadata/coordinator.pid"
    [[ ! -s "$coordinator" ]] || break
    sleep 0.05
done
[[ -s "$coordinator" ]]
read -r child_pid < "$coordinator"
kill -TERM "$runner_pid"
if wait "$runner_pid"; then printf 'Interrupted campaign passed.\n' >&2; exit 1; fi
[[ ! -d "$SEAGULL_FUZZ_ROOT/active" ]]
if kill -0 "$child_pid" 2>/dev/null; then printf 'Interrupted campaign left its command running.\n' >&2; exit 1; fi
grep -q '^interrupted$' "$artefacts/sqllex-split-metadata/outcome.txt"

touch "$SEAGULL_FUZZ_ROOT/programs/disposable" "$SEAGULL_FUZZ_ROOT/cache/disposable" "$SEAGULL_FUZZ_ROOT/tmp/disposable" "$SEAGULL_FUZZ_ROOT/runs/retained-failure"
bash "$runner" clean --scope all > "$fixture/preview.log"
[[ -f "$SEAGULL_FUZZ_ROOT/cache/disposable" && -f "$SEAGULL_FUZZ_ROOT/tmp/disposable" ]]
mkdir "$SEAGULL_FUZZ_ROOT/programs/lease"
rejects clean --scope cache --apply yes
rmdir "$SEAGULL_FUZZ_ROOT/programs/lease"
bash "$runner" clean --scope cache --apply yes > "$fixture/clean-cache.log"
[[ ! -f "$SEAGULL_FUZZ_ROOT/cache/disposable" && ! -f "$SEAGULL_FUZZ_ROOT/programs/disposable" && -f "$SEAGULL_FUZZ_ROOT/tmp/disposable" ]]
bash "$runner" clean --scope temporary --apply yes > "$fixture/clean-temporary.log"
[[ ! -f "$SEAGULL_FUZZ_ROOT/tmp/disposable" && -f "$SEAGULL_FUZZ_ROOT/runs/retained-failure" ]]
rejects clean --scope missing
rejects clean --target sqllex/split
rejects clean --apply maybe
rejects list --scope all
printf '{"name": "Bad Name"}\n' > "$fixture/manifest.json"
rejects intake --source "$fixture/cache" --manifest "$fixture/manifest.json"

# Stages print their plan without running anything.
for stage in A:228 B:201 C:36 D:9 E:17; do
    bash "$runner" campaign --stage "${stage%%:*}" --dry-run yes > "$fixture/stage.log"
    grep -q "^Targets: ${stage##*:}, run one at a time.$" "$fixture/stage.log"
done
grep -q '^mutation  1h         1        sqlite/native-typed$' <(bash "$runner" campaign --stage C --dry-run yes)
grep -q '^stress    200x       1        sqlite/migration-kill$' "$fixture/stage.log"
if grep -q 'clickhouse/concurrent-migrators' "$fixture/stage.log"; then printf 'Stage E runs an unsupported check.\n' >&2; exit 1; fi
rejects campaign --stage F --dry-run yes
rejects campaign --stage B --time 1s
rejects campaign --stage D --rounds 3
rejects seeds --stage A
rejects campaign --dry-run maybe
rejects list --dry-run yes
rejects run --target sqlite/golden-sweep --time 1s
rejects run --target sqlite/migration-kill --time 1s
rejects seeds --target sqlite/migration-kill
rejects campaign --rounds 3 --time 1s
bash "$runner" campaign --stage A --target sqllex/split > "$fixture/stage-a.log"
bash "$runner" campaign --stage D --engine sqlite > "$fixture/stage-d.log"
artefacts=$(sed -n 's/^Campaign logs: //p' "$fixture/stage-d.log")
grep -Fq 'workflow-soak completed 500 rounds' "$artefacts/sqlite-workflow-soak.log"
SEAGULL_RUNNER_MODE=incomplete-soak rejects campaign --stage D
grep -q '^Stage D stopped at sqlite/workflow-soak' "$fixture/rejected.log"
bash "$runner" stress --engine sqlite --rounds 3 > "$fixture/stress.log"
artefacts=$(sed -n 's/^Campaign logs: //p' "$fixture/stress.log")
[[ $(grep -c passed "$artefacts/results.tsv") == 2 ]]
SEAGULL_RUNNER_MODE=stress-skip rejects stress --target sqlite/migration-kill --rounds 2
SEAGULL_RUNNER_MODE=stress-short rejects stress --target sqlite/concurrent-migrators --rounds 2

printf 'Fuzz runner checks passed.\n' 
