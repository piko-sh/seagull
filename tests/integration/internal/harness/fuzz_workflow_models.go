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
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/fuzzbuilder"
	"seagull.sh/seagull/internal/testkit/fuzzcase"
	"seagull.sh/seagull/internal/testkit/fuzzruntime"
	"seagull.sh/seagull/internal/testkit/fuzztyped"
)

const (
	// workflowSelectionKey seeds the choice of input within a model for each round. It
	// spells "soak" in ASCII.
	workflowSelectionKey = 0x736f616b

	// workflowReportName names the per-round report in the campaign metadata directory.
	workflowReportName = "workflow-rounds.tsv"

	// workflowRuntime names the version 12 runtime workflow, which always runs first.
	workflowRuntime = "v12-runtime"

	// workflowGrammar names the version 14 query grammar run through every mode.
	workflowGrammar = "v14-grammar"

	// workflowTyped names the version 15 typed values.
	workflowTyped = "v15-typed"

	// workflowBuilder names the version 16 runtime builder sequences.
	workflowBuilder = "v16-builder"

	// workflowDML names the version 17 DML sequences.
	workflowDML = "v17-dml"

	// workflowLifecycle names the version 20 migration histories.
	workflowLifecycle = "v20-lifecycle"
)

// workflowModel is one model the soak rotates through.
type workflowModel struct {
	// run checks one input of the model through its fuzz target's own check.
	run func(soak *workflowSoak, t *testing.T, data []byte)

	// name identifies the model in subtest names and the report.
	name string

	// seeds holds the model's decodable inputs for the engine.
	seeds [][]byte
}

// workflowSoak owns the helpers and caches the rotation shares across rounds.
type workflowSoak struct {
	// cache is the one compiled program cache every helper uses, closed last.
	cache *fuzzruntime.ProgramCache

	// runtime serves version 12 workflows.
	runtime *nativeWorker

	// typed serves version 15 typed values.
	typed *nativeWorker

	// builder serves version 16 builder sequences.
	builder *nativeWorker

	// modes serves version 14 query modes and version 17 DML.
	modes *modeWorker

	// active names the model whose helper ran last.
	active string

	// suite supplies the native configuration.
	suite NativeFuzzSuite

	// dml holds the version 17 packages, generated on first use.
	dml []modePackage

	// typedCatalogue describes the version 15 package.
	typedCatalogue sequenceCatalogue

	// builderCatalogue describes the version 16 package.
	builderCatalogue sequenceCatalogue

	// dmlMetadata records the DML capabilities once.
	dmlMetadata sync.Once

	// runtimeCount counts the version 12 rounds run so far.
	runtimeCount int
}

// stopHelpers closes every running helper, which also closes their connections.
//
// Returns error when a helper fails to stop.
func (soak *workflowSoak) stopHelpers() error {
	var failures []error
	for _, worker := range []*nativeWorker{soak.runtime, soak.typed, soak.builder} {
		if worker.process != nil {
			failures = append(failures, worker.process.Close())
			worker.process, worker.shape = nil, -1
		}
	}
	if soak.modes.process != nil {
		failures = append(failures, soak.modes.process.Close())
		soak.modes.process, soak.modes.binary = nil, ""
	}
	return errors.Join(failures...)
}

// close stops the helpers before releasing the cache lease.
//
// Returns error when a helper or the lease cannot be released.
func (soak *workflowSoak) close() error {
	return errors.Join(soak.stopHelpers(), soak.cache.Close())
}

// round runs one round's model, stopping the previous model's helper first.
//
// Takes t (*testing.T) which owns the round.
// Takes round (int) which selects the model and its input.
// Takes models ([]workflowModel) which holds the rotation.
//
// Returns string which describes the round for the report.
func (soak *workflowSoak) round(t *testing.T, round int, models []workflowModel) string {
	t.Helper()
	model := models[round%len(models)]
	index := rand.New(rand.NewPCG(uint64(round), workflowSelectionKey)).IntN(len(model.seeds)) //nolint:gosec // a reproducible choice
	if model.name == workflowRuntime {

		index = soak.runtimeCount % len(model.seeds)
		soak.runtimeCount++
	}
	if soak.active != model.name {
		require.NoError(t, soak.stopHelpers())
		soak.active = model.name
	}
	started := time.Now()
	skipped := false
	passed := t.Run(fmt.Sprintf("round_%04d_%s", round, model.name), func(t *testing.T) {
		defer func() { skipped = t.Skipped() }()
		model.run(soak, t, model.seeds[index])
	})
	outcome := "passed"
	switch {
	case skipped:
		outcome = "skipped"
		assert.Failf(t, "skipped round", "round %d skipped its %s seed %d", round, model.name, index)
	case !passed:
		outcome = "failed"
	}
	return fmt.Sprintf("%d\t%s\t%d\t%.3f\t%s", round, model.name, index, time.Since(started).Seconds(), outcome)
}

// runRuntime runs one version 12 workflow.
//
// Takes t (*testing.T) which owns the round.
// Takes data ([]byte) which is the runtime seed.
func (soak *workflowSoak) runRuntime(t *testing.T, data []byte) {
	t.Helper()
	runWorkflowSeed(t, soak.suite, soak.runtime, data)
}

// runGrammar runs one version 14 query through every execution mode.
//
// Takes t (*testing.T) which owns the round.
// Takes data ([]byte) which is the grammar seed.
func (soak *workflowSoak) runGrammar(t *testing.T, data []byte) {
	t.Helper()
	scenario, ok := fuzzcase.DecodeScenario(soak.suite.Engine, data)
	require.True(t, ok, "workflow grammar seed rejected")
	runQueryModes(t, soak.suite, soak.modes, scenario)
}

// runTyped runs one version 15 typed value input.
//
// Takes t (*testing.T) which owns the round.
// Takes data ([]byte) which is the typed seed.
func (soak *workflowSoak) runTyped(t *testing.T, data []byte) {
	t.Helper()
	if soak.typedCatalogue.runner == "" {
		soak.typedCatalogue = typedCatalogue(soak.suite.Engine)
	}
	checkTypedValues(t, soak.suite, soak.typed, soak.typedCatalogue, data)
}

// runBuilder runs one version 16 builder sequence.
//
// Takes t (*testing.T) which owns the round.
// Takes data ([]byte) which is the builder seed.
func (soak *workflowSoak) runBuilder(t *testing.T, data []byte) {
	t.Helper()
	if soak.builderCatalogue.runner == "" {
		soak.builderCatalogue = builderCatalogue(soak.suite.Engine)
	}
	checkBuilderSequence(t, soak.suite, soak.builder, soak.builderCatalogue, data)
}

// runDML runs one version 17 statement sequence in every mode.
//
// Takes t (*testing.T) which owns the round.
// Takes data ([]byte) which is the DML seed.
func (soak *workflowSoak) runDML(t *testing.T, data []byte) {
	t.Helper()
	model, ok := fuzzcase.DecodeDML(soak.suite.Engine, data)
	require.True(t, ok, "workflow DML seed rejected")
	if soak.dml == nil {
		soak.dml = dmlPackages(t, soak.suite)
	}
	checkDMLModel(t, soak.suite, soak.modes, soak.dml, &model, &soak.dmlMetadata)
}

// runLifecycle runs one version 20 migration history.
//
// Takes t (*testing.T) which owns the round.
// Takes data ([]byte) which is the lifecycle seed.
func (soak *workflowSoak) runLifecycle(t *testing.T, data []byte) {
	t.Helper()
	model, ok := fuzzcase.DecodeLifecycle(soak.suite.Engine, data)
	require.True(t, ok, "workflow lifecycle seed rejected")
	runLifecycle(t, soak.suite, &model)
}

// workflowModels lists the models the engine supports, in rotation order, and the models
// it leaves out.
//
// Takes suite (NativeFuzzSuite) which names the engine.
//
// Returns []workflowModel which holds the supported models.
// Returns []string which names the models with no input for the engine.
func workflowModels(suite NativeFuzzSuite) ([]workflowModel, []string) {
	engine := suite.Engine
	candidates := []workflowModel{
		{name: workflowRuntime, seeds: workflowSeeds(engine), run: (*workflowSoak).runRuntime},
		{name: workflowGrammar, seeds: workflowGrammarSeeds(engine), run: (*workflowSoak).runGrammar},
		{name: workflowTyped, seeds: workflowDecodable(fuzztyped.Supported(engine), fuzztyped.Seeds(engine),
			func(data []byte) bool { _, ok := fuzztyped.Decode(engine, data); return ok }), run: (*workflowSoak).runTyped},
		{name: workflowBuilder, seeds: workflowDecodable(fuzzbuilder.Supported(engine), fuzzbuilder.Seeds(engine),
			func(data []byte) bool { _, ok := fuzzbuilder.Decode(engine, data); return ok }), run: (*workflowSoak).runBuilder},
		{name: workflowDML, seeds: workflowDecodable(true, fuzzcase.DMLSeeds(engine),
			func(data []byte) bool { _, ok := fuzzcase.DecodeDML(engine, data); return ok }), run: (*workflowSoak).runDML},
		{name: workflowLifecycle, seeds: workflowDecodable(true, fuzzcase.LifecycleSeeds(),
			func(data []byte) bool { _, ok := fuzzcase.DecodeLifecycle(engine, data); return ok }), run: (*workflowSoak).runLifecycle},
	}
	var models []workflowModel
	var excluded []string
	for _, model := range candidates {
		if len(model.seeds) == 0 {
			excluded = append(excluded, model.name)
			continue
		}
		models = append(models, model)
	}
	return models, excluded
}

// workflowDecodable keeps the seeds the model decodes.
//
// Takes supported (bool) which is false when the model has no catalogue for the engine.
// Takes seeds ([][]byte) which holds the model's seeds.
// Takes decodes (func([]byte) bool) which reports whether the model admits an input.
//
// Returns [][]byte which holds the admitted seeds.
func workflowDecodable(supported bool, seeds [][]byte, decodes func([]byte) bool) [][]byte {
	if !supported {
		return nil
	}
	return slices.DeleteFunc(slices.Clone(seeds), func(data []byte) bool { return !decodes(data) })
}

// workflowGrammarSeeds keeps the version 14 read queries the mode oracle runs.
//
// Takes engine (string) which selects the grammar.
//
// Returns [][]byte which holds the admitted seeds.
func workflowGrammarSeeds(engine string) [][]byte {
	return workflowDecodable(true, fuzzcase.QueryGrammarSeeds(engine), func(data []byte) bool {
		scenario, ok := fuzzcase.DecodeScenario(engine, data)
		return ok && !slices.ContainsFunc(scenario.Queries, func(query fuzzcase.ScenarioQuery) bool { return query.Writes })
	})
}

// newWorkflowSoak opens the shared program cache and the idle helpers.
//
// Takes t (*testing.T) which owns the soak's temporary programs.
// Takes suite (NativeFuzzSuite) which supplies the native configuration.
//
// Returns *workflowSoak which the caller closes.
func newWorkflowSoak(t *testing.T, suite NativeFuzzSuite) *workflowSoak {
	t.Helper()
	temporary := t.TempDir()
	cache := openProgramCache(t, temporary)
	worker := func() *nativeWorker {
		return &nativeWorker{process: nil, cache: cache, temporary: temporary, shape: -1, metadata: sync.Once{}}
	}
	return &workflowSoak{cache: cache, runtime: worker(), typed: worker(), builder: worker(),
		modes: &modeWorker{process: nil, cache: cache, temporary: temporary, binary: ""}, active: "", suite: suite, dml: nil,
		typedCatalogue:   sequenceCatalogue{runner: "", queries: "", sources: nil, schema: nil, methods: 0},
		builderCatalogue: sequenceCatalogue{runner: "", queries: "", sources: nil, schema: nil, methods: 0},
		dmlMetadata:      sync.Once{}, runtimeCount: 0}
}

// workflowReport appends lines to the per-round report of an explicit campaign.
//
// Takes t (*testing.T) which reports an unwritable report.
// Takes lines (...string) which are the tab-separated lines to append.
func workflowReport(t *testing.T, lines ...string) {
	t.Helper()
	directory := os.Getenv(fuzzMetadataDirectoryVariable)
	if directory == "" {
		return
	}
	root, err := os.OpenRoot(directory)
	require.NoError(t, err)
	defer func() { assert.NoError(t, root.Close()) }()
	file, err := root.OpenFile(workflowReportName, os.O_CREATE|os.O_APPEND|os.O_WRONLY, fuzzMetadataPermissions)
	require.NoError(t, err)
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
	}()
	for _, line := range lines {
		_, err := fmt.Fprintln(file, line)
		require.NoError(t, err)
	}
	closed = true
	require.NoError(t, file.Close())
}
