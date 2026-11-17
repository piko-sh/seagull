// Copyright 2026 PolitePixels Limited
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This project stands against fascism, authoritarianism, and all forms of
// oppression. We built this to empower people, not to enable those who would
// strip others of their rights and dignity.

package harness

import (
	"context"
	"database/sql"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/fuzzcase"
	"seagull.sh/seagull/sdk/migrate"
)

const (
	// workflowRoundBound limits explicitly requested soak rounds.
	workflowRoundBound = 1000

	// workflowDeadline bounds one complete native workflow.
	workflowDeadline = 2 * time.Minute

	// workflowControlByte locates runtime driver and transaction choices.
	workflowControlByte = 4

	// workflowBinaryFamily selects the stable binary representation slot.
	workflowBinaryFamily = 6

	// workflowNullBoundary selects a nullable boundary in version 12.
	workflowNullBoundary = 128

	// workflowTransactionFlag identifies optional committed sentinel writes.
	workflowTransactionFlag = 16

	// workflowWatchdogDeadline stops a worker whose workflow ignores cancellation.
	workflowWatchdogDeadline = 2 * workflowDeadline

	// workflowRoundsVariable names the variable that asks for more soak rounds.
	workflowRoundsVariable = "SEAGULL_FUZZ_SOAK_ROUNDS"
)

// nativeWorkflow owns one complete migrated and generated native application state.
type nativeWorkflow struct {
	// database owns the current parent connection pool.
	database *sql.DB

	// worker owns the compiled generated helper and bounded cache.
	worker *nativeWorker

	// migrator drives the actual public migration service.
	migrator *migrate.Migrator

	// seeder drives the actual public seed service.
	seeder *migrate.Seeder

	// files contains independent migration and seed SQL.
	files fstest.MapFS

	// connection allows controlled handoff between parent and generated processes.
	connection string

	// suite selects the pinned native driver and dialect.
	suite NativeFuzzSuite

	// model predicts all generated values and operations.
	model fuzzcase.RuntimeCase
}

// services reconstructs public service objects after reconnecting to persisted state.
//
// Takes t (*testing.T) which reports rejected valid native configuration.
func (state *nativeWorkflow) services(t *testing.T) {
	t.Helper()
	state.database.SetMaxOpenConns(2)
	backend := executionBackend(state.model.Engine)
	var err error
	state.migrator, err = migrate.NewMigrator(state.database, backend, state.files, "migrations")
	require.NoError(t, err)
	state.seeder, err = migrate.NewSeeder(state.database, backend, state.files, "seeds")
	require.NoError(t, err)
}

// up requires a single applied migration followed by an idempotent repeated call.
//
// Takes t (*testing.T) which reports schema or history lifecycle failures.
func (state *nativeWorkflow) up(ctx context.Context, t *testing.T) {
	t.Helper()
	for _, expected := range []int{1, 0} {
		count, err := state.migrator.Up(ctx)
		require.NoError(t, err, "workflow up")
		require.Equal(t, expected, count, "workflow up applied the wrong number of migrations")
	}
	state.status(ctx, t, true)
}

// down requires complete rollback of the migration and its native tables.
//
// Takes t (*testing.T) which reports retained schema or stale migration history.
func (state *nativeWorkflow) down(ctx context.Context, t *testing.T) {
	t.Helper()
	count, err := state.migrator.DownTo(ctx, 0)
	require.NoError(t, err, "workflow down")
	require.Equal(t, 1, count, "workflow down reverted the wrong number of migrations")
	state.status(ctx, t, false)
	query := fuzzRelationsQuery(state.model.Engine)
	if state.model.Engine == engineSQLite {
		query = "SELECT name FROM sqlite_schema WHERE type = 'table'"
	}
	for _, row := range fuzzRows(ctx, t, state.database, query, nil) {
		require.False(t, strings.HasPrefix(fuzzValue(row[0]), "native_"), "workflow rollback retained the native table %v", row[0])
	}
}

// status checks the complete migrated history after applying or reverting the schema.
//
// Takes t (*testing.T) which reports incorrect history or checksums.
// Takes applied (bool) which supplies the independently expected migration state.
func (state *nativeWorkflow) status(ctx context.Context, t *testing.T, applied bool) {
	t.Helper()
	statuses, err := state.migrator.Status(ctx)
	require.NoError(t, err)
	require.Len(t, statuses, 1, "workflow migration history differs %+v", statuses)
	status := statuses[0]
	require.True(t, status.Applied == applied && !status.Dirty && (!applied || status.ChecksumMatch), "workflow migration history differs %+v", statuses)
	require.NoError(t, state.migrator.Validate(ctx))
}

// applySeeds requires an exact apply count and persisted seed history.
//
// Takes t (*testing.T) which reports lost seed history or unexpected execution.
// Takes expected (int) which supplies the independently expected applied count.
func (state *nativeWorkflow) applySeeds(ctx context.Context, t *testing.T, expected int) {
	t.Helper()
	count, err := state.seeder.Apply(ctx)
	state.checkSeeds(ctx, t, count, err, expected)
}

// reseed requires an exact reseed count and persisted seed history.
//
// Takes t (*testing.T) which reports lost seed history or unexpected execution.
// Takes expected (int) which supplies the independently expected applied count.
func (state *nativeWorkflow) reseed(ctx context.Context, t *testing.T, expected int) {
	t.Helper()
	count, err := state.seeder.Reseed(ctx)
	state.checkSeeds(ctx, t, count, err, expected)
}

// checkSeeds compares one seed run and the persisted seed history with the model.
//
// Takes t (*testing.T) which reports lost seed history or unexpected execution.
// Takes count (int) which is how many seeds the run applied.
// Takes err (error) which is the run's error.
// Takes expected (int) which supplies the independently expected applied count.
func (state *nativeWorkflow) checkSeeds(ctx context.Context, t *testing.T, count int, err error, expected int) {
	t.Helper()
	require.NoError(t, err, "workflow seed")
	require.Equal(t, expected, count, "workflow seed applied the wrong number of seeds")
	statuses, err := state.seeder.Status(ctx)
	require.NoError(t, err)
	require.Len(t, statuses, 1, "workflow seed history differs %+v", statuses)
	require.True(t, statuses[0].Applied && statuses[0].ChecksumMatch, "workflow seed history differs %+v", statuses)
}

// exchange hands native state to actual generated Go before reconnecting its services.
//
// Takes t (*testing.T) which reports generated value or connection failures.
// Takes model (fuzzcase.RuntimeCase) which independently predicts the current rows.
func (state *nativeWorkflow) exchange(ctx context.Context, t *testing.T, model fuzzcase.RuntimeCase) {
	t.Helper()
	require.NoError(t, state.database.Close())
	checkNativeResponse(ctx, t, state.suite, state.worker.process, state.connection, model)
	database, err := sql.Open(state.suite.Driver.Name, state.connection)
	require.NoError(t, err)
	state.database = database
	state.services(t)
}

// CheckFuzzWorkflowSoak combines real migrations, generated execution and seeds.
//
// Rounds rotate through the version 12 runtime workflow, version 14 query modes, version
// 15 typed values, version 16 builder sequences, version 17 DML and version 20 migration
// histories, each run by its fuzz target's own check. The round number alone selects the
// model and its input. A change of model stops the previous helper and its connections.
//
// Takes t (*testing.T) which owns the sequential soak and its compiled cache.
// Takes suite (NativeFuzzSuite) which supplies an isolated native configuration.
func CheckFuzzWorkflowSoak(t *testing.T, suite NativeFuzzSuite) {
	t.Helper()
	rounds := workflowRounds(t)
	models, excluded := workflowModels(suite)
	require.True(t, len(models) > 0 && models[0].name == workflowRuntime && suite.OpenNative != nil, "incomplete workflow soak suite")
	workflowReport(t, "round\tmodel\tseed\tseconds\toutcome")
	for _, name := range excluded {
		workflowReport(t, "-\t"+name+"\t-\t0\texcluded")
	}
	soak := newWorkflowSoak(t, suite)
	defer func() {
		assert.NoError(t, soak.close())
	}()
	for round := range rounds {
		line := soak.round(t, round, models)
		t.Log(line)
		workflowReport(t, line)
		if t.Failed() {
			return
		}
	}
	t.Logf("workflow-soak completed %d rounds", rounds)
}

// workflowRounds validates the explicit local soak budget.
//
// Takes t (*testing.T) which reports malformed or excessive budgets.
//
// Returns int which defaults to one round for ordinary test invocations.
func workflowRounds(t *testing.T) int {
	t.Helper()
	setting := os.Getenv(workflowRoundsVariable)
	if setting == "" {
		return 1
	}
	rounds, err := strconv.Atoi(setting)
	require.NoError(t, err, "%s must be a number", workflowRoundsVariable)
	require.True(t, rounds >= 1 && rounds <= workflowRoundBound, "%s must be between 1 and %d", workflowRoundsVariable, workflowRoundBound)
	return rounds
}

// runNativeWorkflow exercises one complete native lifecycle with generated value checks.
//
// Takes t (*testing.T) which owns the isolated database.
// Takes suite (NativeFuzzSuite) which supplies the native configuration.
// Takes worker (*nativeWorker) which owns the actual generated helper.
// Takes model (fuzzcase.RuntimeCase) which supplies independent row expectations.
//
// Panics in the watchdog callback if the workflow ignores cancellation.
func runNativeWorkflow(t *testing.T, suite NativeFuzzSuite, worker *nativeWorker, model fuzzcase.RuntimeCase) {
	t.Helper()
	ctx, cancel := context.WithTimeoutCause(t.Context(), scaled(workflowDeadline), errFuzzInputDeadline)
	defer cancel()
	watchdog := time.AfterFunc(scaled(workflowWatchdogDeadline), func() { panic("native workflow ignored cancellation: " + t.Name()) })
	defer watchdog.Stop()
	database, connection := suite.OpenNative(t)
	state := nativeWorkflow{database: database, connection: connection, worker: worker, suite: suite, model: model,
		files: workflowFiles(model), migrator: nil, seeder: nil}
	defer func() { _ = state.database.Close() }()
	worker.metadata.Do(func() {
		writeFuzzEngineMetadata(ctx, t, database, suite.Engine)
		writeNativeCapabilities(t, suite.Engine)
	})
	state.services(t)
	state.up(ctx, t)
	state.exchange(ctx, t, model)
	state.applySeeds(ctx, t, 1)
	state.applySeeds(ctx, t, 0)
	empty := workflowEmpty(t, model)
	state.exchange(ctx, t, empty)
	state.reseed(ctx, t, 1)
	state.down(ctx, t)
	state.up(ctx, t)
	state.applySeeds(ctx, t, 0)
	state.exchange(ctx, t, empty)
	state.down(ctx, t)
}

// workflowFiles combines independent native declarations with reversible migration files.
//
// Takes model (fuzzcase.RuntimeCase) which selects the required native tables.
//
// Returns fstest.MapFS which includes forward, rollback and clearing seed SQL.
func workflowFiles(model fuzzcase.RuntimeCase) fstest.MapFS {
	var up, down, seeds []string
	for _, statement := range fuzzcase.RuntimeSchema(model.Engine) {
		if !model.Prepared && !strings.HasPrefix(statement, "CREATE TABLE native_"+model.Kind+" ") {
			continue
		}
		table := strings.Fields(statement)[2]
		up = append(up, statement)
		down = append(down, "DROP TABLE "+table+";")
		clearing := "DELETE FROM " + table + ";"
		if model.Engine == engineClickHouse {
			clearing = "TRUNCATE TABLE " + table + ";"
		}
		seeds = append(seeds, clearing)
	}
	slices.Reverse(down)
	return fstest.MapFS{
		"migrations/0001_native.up.sql":   {Data: []byte(strings.Join(up, "\n"))},
		"migrations/0001_native.down.sql": {Data: []byte(strings.Join(down, "\n"))},
		"seeds/0001_clear.sql":            {Data: []byte(strings.Join(seeds, "\n"))},
	}
}

// workflowEmpty predicts empty results after clearing all model rows.
//
// Takes t (*testing.T) which reports an incompatible replay encoding.
// Takes model (fuzzcase.RuntimeCase) which selects the same native family and driver.
//
// Returns fuzzcase.RuntimeCase which requires empty generated reads and counts.
func workflowEmpty(t *testing.T, model fuzzcase.RuntimeCase) fuzzcase.RuntimeCase {
	t.Helper()
	data := make([]byte, max(len(model.Data), workflowControlByte+1))
	copy(data, model.Data)
	data[2] = 0
	data[workflowControlByte] &^= workflowTransactionFlag
	empty, ok := fuzzcase.DecodeRuntime(model.Engine, data)
	require.True(t, ok, "empty workflow model rejected")
	return empty
}

// runWorkflowSeed prepares one actual generated configuration and checks its lifecycle.
//
// Takes t (*testing.T) which owns the isolated workflow round.
// Takes suite (NativeFuzzSuite) which selects the pinned native configuration.
// Takes worker (*nativeWorker) which owns the bounded compiled helper cache.
// Takes data ([]byte) which selects an independent runtime envelope.
func runWorkflowSeed(t *testing.T, suite NativeFuzzSuite, worker *nativeWorker, data []byte) {
	t.Helper()
	model, ok := fuzzcase.DecodeRuntime(suite.Engine, data)
	require.True(t, ok, "workflow seed rejected")
	shape := model.Shape
	if model.Prepared {
		shape += nativePreparedShape
	}
	if model.Native {
		shape += nativeDriverShape
	}
	worker.prepare(t, suite, shape)
	runNativeWorkflow(t, suite, worker, model)
}

// workflowSeeds puts every driver mode before the complete representation corpus.
//
// Takes engine (string) which selects independently supported native operations.
//
// Returns [][]byte which reaches important lifecycle modes in short local soaks.
func workflowSeeds(engine string) [][]byte {
	var seeds [][]byte
	modes := []byte{0}
	if fuzzcase.RuntimePrepared(engine) {
		modes = append(modes, nativePreparedShape)
	}
	if fuzzcase.RuntimeNative(engine) {
		modes = append(modes, nativeDriverShape)
	}
	for _, mode := range modes {
		seeds = append(seeds, []byte{fuzzcase.RuntimeVersion, workflowBinaryFamily, 2, workflowNullBoundary, mode})
		if fuzzcase.RuntimeTransactions(engine) {
			seeds = append(seeds, []byte{fuzzcase.RuntimeVersion, workflowBinaryFamily, 2, workflowNullBoundary, mode | workflowTransactionFlag})
		}
	}
	return append(seeds, fuzzcase.RuntimeSeeds(engine)...)
}
