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
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/internal/testkit/fuzzbuilder"
	"seagull.sh/seagull/internal/testkit/fuzzcase"
	"seagull.sh/seagull/internal/testkit/fuzznative"
	"seagull.sh/seagull/internal/testkit/fuzzruntime"
	"seagull.sh/seagull/internal/testkit/fuzztyped"
	"seagull.sh/seagull/sdk/emit"
)

// sequenceModels names the model packages every sequence helper links.
var sequenceModels = []string{"internal/testkit/fuzzbuilder", "internal/testkit/fuzztyped"}

// sequenceCatalogue describes the generated package a sequence helper compiles.
type sequenceCatalogue struct {
	// runner is the Go expression of the helper's fuzznative.SequenceRunner.
	runner string

	// queries holds the catalogue's query file.
	queries string

	// sources names the model packages the helper links besides fuzznative and fuzzcase.
	sources []string

	// schema holds the catalogue's migration statements.
	schema []string

	// methods is the number of queries generation must report.
	methods int
}

// sequenceInput holds what one sequence input needs in its isolated database.
type sequenceInput struct {
	// lane names the capability record written once per worker.
	lane string

	// capabilities records the model's exclusions and their reasons.
	capabilities []fuzzcase.Capability

	// schema creates the catalogue's tables.
	schema []string

	// inserts write the modelled rows.
	inserts []string

	// data is the input the helper decodes.
	data []byte
}

// prepareSequence keeps or starts the helper compiled for a catalogue and shape.
//
// Takes t (*testing.T) which reports generation, build and startup failures.
// Takes suite (NativeFuzzSuite) which selects the driver.
// Takes catalogue (sequenceCatalogue) which describes the generated package.
// Takes shape (int) which selects preparation or the dedicated driver.
func (worker *nativeWorker) prepareSequence(t *testing.T, suite NativeFuzzSuite, catalogue sequenceCatalogue, shape int) {
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
	binary := buildSequenceProgram(t, suite, worker.cache, catalogue, shape)
	process, err := fuzznative.Start(binary)
	require.NoError(t, err)
	worker.process, worker.shape = process, shape
}

// builderCatalogue describes the version 16 runtime builder catalogue of an engine.
//
// Takes engine (string) which names the dialect.
//
// Returns sequenceCatalogue which compiles every builder query.
func builderCatalogue(engine string) sequenceCatalogue {
	return sequenceCatalogue{runner: "fuzznative.RunBuilder(" + strconv.Quote(engine) + ")", queries: fuzzbuilder.Queries(engine),
		sources: sequenceModels, schema: fuzzbuilder.Schema(engine), methods: fuzzbuilder.QueryCount()}
}

// typedCatalogue describes the version 15 typed value and expression catalogue.
//
// Takes engine (string) which names the dialect.
//
// Returns sequenceCatalogue which compiles every family's and expression's methods.
func typedCatalogue(engine string) sequenceCatalogue {
	return sequenceCatalogue{runner: "fuzznative.RunTyped(" + strconv.Quote(engine) + ")", queries: fuzztyped.Queries(engine),
		sources: sequenceModels, schema: fuzztyped.Schema(engine), methods: fuzztyped.QueryCount(engine)}
}

// FuzzGeneratedValues round-trips version 15 typed values and expressions through
// generated methods.
//
// Each input writes its expression rows directly, then calls the generated insert, read,
// filter, join, update and expression methods, and compares every value, kind by kind,
// with the independent storage, equality and expression rules.
//
// Takes f (*testing.F) which owns the worker and seeds.
// Takes suite (NativeFuzzSuite) which supplies native connections and the driver.
func FuzzGeneratedValues(f *testing.F, suite NativeFuzzSuite) {
	f.Helper()
	fuzzSequenceModel(f, suite, fuzztyped.Supported(suite.Engine), fuzztyped.Seeds(suite.Engine), typedCatalogue, checkTypedValues)
}

// fuzzSequenceModel adds a sequence model's seeds and runs each input through check with
// one helper worker, closed with the target.
//
// Takes f (*testing.F) which owns the worker and seeds.
// Takes suite (NativeFuzzSuite) which supplies native connections and the driver.
// Takes supported (bool) which is false when the model has no catalogue for the engine.
// Takes seeds ([][]byte) which holds the model's seeds.
// Takes catalogue (func(string) sequenceCatalogue) which describes the engine's package.
// Takes check (func(...)) which runs and checks one input.
func fuzzSequenceModel(f *testing.F, suite NativeFuzzSuite, supported bool, seeds [][]byte, catalogue func(string) sequenceCatalogue,
	check func(*testing.T, NativeFuzzSuite, *nativeWorker, sequenceCatalogue, []byte)) {
	f.Helper()
	if suite.OpenNative == nil || suite.Dialect == nil || suite.Driver.Module == "" || !supported {
		require.Fail(f, "incomplete native sequence suite")
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	worker := &nativeWorker{process: nil, cache: nil, temporary: f.TempDir(), shape: -1, metadata: sync.Once{}}
	f.Cleanup(func() {
		assert.NoError(f, worker.close())
	})
	described := catalogue(suite.Engine)
	f.Fuzz(func(t *testing.T, data []byte) { check(t, suite, worker, described, data) })
}

// checkTypedValues runs one version 15 input in the helper and checks it, skipping an
// input the model does not decode.
//
// Takes t (*testing.T) which reports differences from the model.
// Takes suite (NativeFuzzSuite) which supplies native connections and the driver.
// Takes worker (*nativeWorker) which owns the helper compiled for the catalogue.
// Takes catalogue (sequenceCatalogue) which describes the typed value package.
// Takes data ([]byte) which is the input.
func checkTypedValues(t *testing.T, suite NativeFuzzSuite, worker *nativeWorker, catalogue sequenceCatalogue, data []byte) {
	t.Helper()
	model, ok := fuzztyped.Decode(suite.Engine, data)
	if !ok {
		t.Skip("unsupported typed value envelope")
	}
	worker.prepareSequence(t, suite, catalogue, sequenceShape(fuzzbuilder.Mode(model.Mode)))
	response := exchangeSequence(t, suite, worker, sequenceInput{lane: "typed", capabilities: fuzztyped.Capabilities(suite.Engine),
		schema: catalogue.schema, inserts: model.Inserts, data: data})
	var observations []fuzztyped.Observation
	require.NoError(t, json.Unmarshal(response.Observations, &observations), "typed value observations")
	if problems := model.Problems(observations); len(problems) != 0 {
		require.Failf(t, "typed value mismatch", "input %v\n%s\n%s", data, model, strings.Join(problems, "\n"))
	}
}

// FuzzGeneratedBuilder runs version 16 call sequences through generated runtime builders.
//
// Each input inserts its rows directly, then calls Where, OrderBy, Limit, Offset and the
// All, One and Count terminals of the actual generated builders, and compares every
// result with the independent row model.
//
// Takes f (*testing.F) which owns the worker and seeds.
// Takes suite (NativeFuzzSuite) which supplies native connections and the driver.
func FuzzGeneratedBuilder(f *testing.F, suite NativeFuzzSuite) {
	f.Helper()
	fuzzSequenceModel(f, suite, fuzzbuilder.Supported(suite.Engine), fuzzbuilder.Seeds(suite.Engine), builderCatalogue, checkBuilderSequence)
}

// checkBuilderSequence runs one version 16 input in the helper and checks it, skipping an
// input the model does not decode.
//
// Takes t (*testing.T) which reports differences from the row model.
// Takes suite (NativeFuzzSuite) which supplies native connections and the driver.
// Takes worker (*nativeWorker) which owns the helper compiled for the catalogue.
// Takes catalogue (sequenceCatalogue) which describes the builder package.
// Takes data ([]byte) which is the input.
func checkBuilderSequence(t *testing.T, suite NativeFuzzSuite, worker *nativeWorker, catalogue sequenceCatalogue, data []byte) {
	t.Helper()
	model, ok := fuzzbuilder.Decode(suite.Engine, data)
	if !ok {
		t.Skip("unsupported runtime builder envelope")
	}
	worker.prepareSequence(t, suite, catalogue, sequenceShape(model.Mode))
	response := exchangeSequence(t, suite, worker, sequenceInput{lane: "builder", capabilities: fuzzbuilder.Capabilities(suite.Engine),
		schema: catalogue.schema, inserts: model.Inserts(), data: data})
	var observations []fuzzbuilder.Observation
	require.NoError(t, json.Unmarshal(response.Observations, &observations), "runtime builder observations")
	if problems := model.Problems(observations); len(problems) != 0 {
		require.Failf(t, "runtime builder mismatch", "sequence %v\n%s\n%s", data, model, strings.Join(problems, "\n"))
	}
}

// sequenceShape maps an emitter mode to the native helper's compiled shape.
//
// Takes mode (fuzzbuilder.Mode) which selects the emitter.
//
// Returns int which selects preparation or the dedicated driver as nativeMain does.
func sequenceShape(mode fuzzbuilder.Mode) int {
	switch mode {
	case fuzzbuilder.ModePrepared:
		return nativePreparedShape
	case fuzzbuilder.ModeNative:
		return nativeDriverShape
	default:
		return 0
	}
}

// exchangeSequence prepares an isolated database and runs one input in the helper.
//
// Takes t (*testing.T) which reports setup and helper failures.
// Takes suite (NativeFuzzSuite) which opens the isolated database.
// Takes worker (*nativeWorker) which owns the running helper and its metadata record.
// Takes input (sequenceInput) which holds the schema, rows and input.
//
// Returns fuzznative.SequenceResponse which holds the helper's observations.
func exchangeSequence(t *testing.T, suite NativeFuzzSuite, worker *nativeWorker, input sequenceInput) fuzznative.SequenceResponse {
	t.Helper()
	ctx, cancel := context.WithTimeoutCause(t.Context(), scaled(databaseInputDeadline), errFuzzInputDeadline)
	defer cancel()
	database, connection := suite.OpenNative(t)
	database.SetMaxOpenConns(1)
	worker.metadata.Do(func() {
		writeFuzzEngineMetadata(ctx, t, database, suite.Engine)
		writeFuzzCapabilities(t, input.lane, suite.Engine, input.capabilities)
	})
	fuzzApply(ctx, t, database, input.schema)
	fuzzApply(ctx, t, database, input.inserts)
	require.NoError(t, database.Close())
	response, err := worker.process.ExchangeSequence(ctx, connection, input.data)
	require.NoError(t, err)
	require.True(t, response.Error == "" && response.Panic == "", "generated sequence failed at %s: %s %s", response.Stage, response.Error, response.Panic)
	return response
}

// buildSequenceProgram compiles a sequence helper over actual generated Go.
//
// Takes t (*testing.T) which reports generation and build failures.
// Takes suite (NativeFuzzSuite) which supplies the dialect and driver.
// Takes cache (*fuzzruntime.ProgramCache) which keeps compiled helpers.
// Takes catalogue (sequenceCatalogue) which describes the generated package.
// Takes shape (int) which selects preparation or the dedicated driver.
//
// Returns string which names the cached executable.
func buildSequenceProgram(t *testing.T, suite NativeFuzzSuite, cache *fuzzruntime.ProgramCache, catalogue sequenceCatalogue, shape int) string {
	t.Helper()
	ctx, cancel := context.WithTimeoutCause(t.Context(), nativeCompileBound, errNativeCompileDeadline)
	defer cancel()
	files := sequenceGeneratedFiles(ctx, t, suite, catalogue, shape)
	directory := t.TempDir()
	for _, file := range files {
		writeFile(t, filepath.Join(directory, "generated", file.Name), file.Content)
	}
	serve := "fuzznative.Serve(" + strconv.Quote(suite.Engine) + ", factory,"
	main := nativeMain(suite.Engine, suite.Driver.Name, shape)
	require.True(t, strings.Contains(main, serve), "native helper entry point no longer calls fuzznative.Serve")
	main = strings.Replace(main, serve, "fuzznative.ServeSequences(factory, "+catalogue.runner+",", 1)
	return compileHelper(ctx, t, suite, cache, &helperProgram{directory: directory, name: "sequence-program", main: main, files: files,
		sources: moduleSources(t, nativeRepositoryRoot(t), catalogue.sources)})
}

// sequenceGeneratedFiles generates a catalogue with the shape's emitter.
//
// Takes t (*testing.T) which reports failures.
// Takes suite (NativeFuzzSuite) which supplies the dialect and driver module.
// Takes catalogue (sequenceCatalogue) which holds the migrations and queries.
// Takes shape (int) which selects preparation or the dedicated driver.
//
// Returns []emit.GeneratedFile which holds the generated package.
func sequenceGeneratedFiles(ctx context.Context, t *testing.T, suite NativeFuzzSuite, catalogue sequenceCatalogue, shape int) []emit.GeneratedFile {
	t.Helper()
	generator, err := seagull.New(suite.Dialect(), seagull.WithEmitter(modeEmitter(suite, shapeMode(shape))))
	require.NoError(t, err)
	input := seagull.Input{Package: "generated", PackagePath: "", Queries: nil, MigrationDir: "migrations", QueryDir: "queries",
		Migrations: fstest.MapFS{
			"migrations/001_sequence.up.sql": &fstest.MapFile{Data: []byte(strings.Join(catalogue.schema, "\n"))},
			"queries/sequence.sql":           &fstest.MapFile{Data: []byte(catalogue.queries)},
		}}
	details, err := generator.GenerateDetailed(ctx, input)
	require.NoError(t, err)
	require.NoError(t, details.Result.Err(), "sequence catalogue shape %d", shape)
	require.Len(t, details.Report.Queries, catalogue.methods, "sequence catalogue did not generate every query")
	require.NotEmpty(t, details.Result.Files, "sequence catalogue generated no files")
	return details.Result.Files
}
