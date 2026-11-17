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
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/fuzzcase"
	"seagull.sh/seagull/internal/testkit/fuzznative"
	"seagull.sh/seagull/internal/testkit/fuzzruntime"
)

// NativeFuzzSuite supplies isolated native connections and real generated dependencies.
type NativeFuzzSuite struct {
	// OpenNative returns the isolated connection string as well as its parent pool.
	OpenNative func(*testing.T) (*sql.DB, string)

	// BuildEnvironment selects required CGO and platform settings.
	BuildEnvironment []string

	// Driver identifies the suite's actual pinned native driver.
	Driver Driver

	// FuzzSuite supplies the engine, dialect and existing native metadata adapter.
	FuzzSuite
}

// nativeWorker owns one sequential helper, its cache and its metadata record.
type nativeWorker struct {
	// process runs the currently compiled native shape.
	process *fuzznative.Process

	// cache bounds retained compiled helpers across shape changes.
	cache *fuzzruntime.ProgramCache

	// temporary owns transient compiled programs outside managed campaigns.
	temporary string

	// shape identifies the currently running query catalogue.
	shape int

	// metadata records server details once for this worker.
	metadata sync.Once
}

// close stops the generated process before releasing its compiled cache lease.
//
// Returns error when helper shutdown or ownership release fails.
func (worker *nativeWorker) close() error {
	var failures []error
	if worker.process != nil {
		failures = append(failures, worker.process.Close())
		worker.process = nil
	}
	if worker.cache != nil {
		failures = append(failures, worker.cache.Close())
		worker.cache = nil
	}
	return errors.Join(failures...)
}

// prepare retains a compiled shape and starts its actual generated helper.
//
// Takes t (*testing.T) which reports generation, ownership and startup failures.
// Takes suite (NativeFuzzSuite) which selects the native driver profile.
// Takes shape (int) which selects the bounded query catalogue.
func (worker *nativeWorker) prepare(t *testing.T, suite NativeFuzzSuite, shape int) {
	t.Helper()
	if worker.process != nil && worker.shape == shape {
		return
	}
	if worker.process != nil {
		require.NoError(t, worker.process.Close())
		worker.process = nil
	}
	if worker.cache == nil {
		worker.cache = openProgramCache(t, worker.temporary)
	}
	binary := buildNativeProgram(t, suite, worker.cache, shape)
	var err error
	worker.process, err = fuzznative.Start(binary)
	require.NoError(t, err)
	worker.shape = shape
}

// FuzzGeneratedNativeValues runs actual generated methods of one query shape against
// independently modelled values.
//
// Takes f (*testing.F) which owns the worker and seed catalogue.
// Takes suite (NativeFuzzSuite) which supplies native connections and driver
// dependencies.
func FuzzGeneratedNativeValues(f *testing.F, suite NativeFuzzSuite) {
	f.Helper()
	fuzzGeneratedNative(f, suite, fuzzcase.RuntimeSeeds(suite.Engine), func(fuzzcase.RuntimeCase) int { return 0 })
}

// FuzzGeneratedNativeShapes runs actual generated methods of every bounded query shape
// against independently modelled values.
//
// Takes f (*testing.F) which owns the worker and seed catalogue.
// Takes suite (NativeFuzzSuite) which supplies native connections and driver
// dependencies.
func FuzzGeneratedNativeShapes(f *testing.F, suite NativeFuzzSuite) {
	f.Helper()
	fuzzGeneratedNative(f, suite, fuzzcase.RuntimeShapeSeeds(suite.Engine), func(model fuzzcase.RuntimeCase) int { return model.Shape })
}

// fuzzGeneratedNative runs actual generated methods against independently modelled
// values.
//
// Takes f (*testing.F) which owns the worker and seed catalogue.
// Takes suite (NativeFuzzSuite) which supplies native connections and driver
// dependencies.
// Takes seeds ([][]byte) which is the seed corpus.
// Takes baseShape (func(fuzzcase.RuntimeCase) int) which selects the compiled query
// shape.
func fuzzGeneratedNative(f *testing.F, suite NativeFuzzSuite, seeds [][]byte, baseShape func(fuzzcase.RuntimeCase) int) {
	f.Helper()
	require.True(f, suite.OpenNative != nil && suite.Dialect != nil && suite.Driver.Module != "" && len(fuzzcase.RuntimeSchema(suite.Engine)) > 0,
		"incomplete native generated-code suite")
	for _, seed := range seeds {
		f.Add(seed)
	}
	worker := &nativeWorker{process: nil, cache: nil, temporary: f.TempDir(), shape: -1, metadata: sync.Once{}}
	f.Cleanup(func() {
		assert.NoError(f, worker.close())
	})
	f.Fuzz(func(t *testing.T, data []byte) {
		model, ok := fuzzcase.DecodeRuntime(suite.Engine, data)
		if !ok {
			t.Skip("unsupported native runtime envelope")
		}
		shape := baseShape(model)
		if model.Prepared {
			shape += nativePreparedShape
		}
		if model.Native {
			shape += nativeDriverShape
		}
		worker.prepare(t, suite, shape)
		runGeneratedNative(t, suite, worker.process, model, &worker.metadata)
	})
}

// runGeneratedNative owns one isolated native state and checks every generated result.
//
// Takes t (*testing.T) which reports failed native oracles.
// Takes suite (NativeFuzzSuite) which supplies the actual driver and engine.
// Takes process (*fuzznative.Process) which owns the compiled generated helper.
// Takes model (fuzzcase.RuntimeCase) which supplies independent expected values.
// Takes metadata (*sync.Once) which records the server version once.
func runGeneratedNative(t *testing.T, suite NativeFuzzSuite, process *fuzznative.Process, model fuzzcase.RuntimeCase, metadata *sync.Once) {
	t.Helper()
	ctx, cancel := context.WithTimeoutCause(t.Context(), scaled(databaseInputDeadline), errFuzzInputDeadline)
	defer cancel()
	database, connection := suite.OpenNative(t)
	database.SetMaxOpenConns(1)
	metadata.Do(func() {
		writeFuzzEngineMetadata(ctx, t, database, suite.Engine)
		writeNativeCapabilities(t, suite.Engine)
	})
	tablePrefix := "CREATE TABLE native_" + model.Kind + " "
	applied := false
	for _, statement := range fuzzcase.RuntimeSchema(suite.Engine) {
		if model.Prepared || strings.HasPrefix(statement, tablePrefix) {
			fuzzApply(ctx, t, database, []string{statement})
			applied = true
		}
	}
	require.True(t, applied, "native input has no independent schema")
	require.NoError(t, database.Close())
	checkNativeResponse(ctx, t, suite, process, connection, model)
}

// checkNativeResponse compares generated values with the model and native storage.
//
// Takes t (*testing.T) which reports failed exact comparisons.
// Takes suite (NativeFuzzSuite) which selects the real driver and dialect.
// Takes process (*fuzznative.Process) which owns the actual generated helper.
// Takes connection (string) which identifies the isolated native state privately.
// Takes model (fuzzcase.RuntimeCase) which supplies independent expected values.
func checkNativeResponse(ctx context.Context, t *testing.T, suite NativeFuzzSuite, process *fuzznative.Process, connection string, model fuzzcase.RuntimeCase) {
	t.Helper()
	response, err := process.Exchange(ctx, connection, model.Data)
	require.NoError(t, err)
	reference, err := nativeReference(ctx, suite, connection, model)
	require.NoError(t, err, "native reference failed")
	observed := response.Plain
	if model.Transactions {
		observed = response.AfterCommit
	}
	require.NoError(t, fuzznative.CompareRows(reference, observed), "generated rows differ from native storage")
	require.Empty(t, fuzznative.Problems(model, &response), "native generated-code input %v (%s)", model.Data, model.Kind)
}

// nativeReference reads persisted rows without calling generated binding or scan helpers.
//
// Takes suite (NativeFuzzSuite) which identifies the actual linked driver.
// Takes connection (string) which reconnects to the same isolated database.
// Takes model (fuzzcase.RuntimeCase) which selects an independently declared table.
//
// Returns []fuzznative.Row which preserves exact native values and NULL masks.
// Returns error when native execution or lossless reference decoding fails.
func nativeReference(ctx context.Context, suite NativeFuzzSuite, connection string, model fuzzcase.RuntimeCase) ([]fuzznative.Row, error) {
	database, err := sql.Open(suite.Driver.Name, connection)
	if err != nil {
		return nil, fmt.Errorf("opening the reference pool: %w", err)
	}
	defer func() { _ = database.Close() }()
	database.SetMaxOpenConns(1)
	// #nosec G201 -- Both fragments come from the fixed native catalogue.
	query := fmt.Sprintf("SELECT id, %s, value IS NULL FROM native_%s ORDER BY id", nativeProjection(suite.Engine, model.Kind), model.Kind)
	rows, err := database.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("querying native_%s: %w", model.Kind, err)
	}
	defer func() { _ = rows.Close() }()
	var result []fuzznative.Row
	for rows.Next() {
		var row fuzznative.Row
		var raw any
		if err := rows.Scan(&row.ID, &raw, &row.Null); err != nil {
			return nil, fmt.Errorf("scanning native_%s: %w", model.Kind, err)
		}
		row.Value, err = fuzznative.NativeValue(model.Kind, raw, row.Null)
		if err != nil {
			return nil, fmt.Errorf("decoding native_%s: %w", model.Kind, err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading native_%s: %w", model.Kind, err)
	}
	return result, nil
}

// nativeProjection chooses native text encodings for exact non-floating values.
//
// Takes engine (string) which selects native cast and array serialisation syntax.
// Takes kind (string) which names the independent value family.
//
// Returns string which avoids generated decimal, JSON, UUID and array scan adapters.
func nativeProjection(engine, kind string) string {
	if kind == "array" {
		switch engine {
		case engineClickHouse:
			return "toJSONString(value)"
		case engineDuckDB:
			return "CAST(to_json(value) AS VARCHAR)"
		default:
			return "CAST(array_to_json(value) AS TEXT)"
		}
	}
	switch kind {
	case "unsigned", "decimal", "uuid", "json":
		switch engine {
		case engineClickHouse:
			return "toString(value)"
		case engineMySQL, engineMariaDB:
			return "CAST(value AS CHAR)"
		default:
			return "CAST(value AS VARCHAR)"
		}
	default:
		return "value"
	}
}
