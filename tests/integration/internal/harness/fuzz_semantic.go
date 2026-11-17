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
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/fuzzsemantic"
)

// semanticCatalogue describes the version 19 semantic relation catalogue of an engine.
//
// Takes engine (string) which names the dialect.
//
// Returns sequenceCatalogue which compiles every relation query the engine admits.
func semanticCatalogue(engine string) sequenceCatalogue {
	return sequenceCatalogue{runner: "fuzznative.RunSemantic(" + strconv.Quote(engine) + ")", queries: fuzzsemantic.Queries(engine),
		sources: slices.Concat(sequenceModels, []string{"internal/testkit/fuzzsemantic"}), schema: fuzzsemantic.Schema(engine),
		methods: fuzzsemantic.QueryCount(engine)}
}

// FuzzSemanticRelations checks version 19 semantic relations through generated methods.
//
// Each input inserts its rows directly, then calls the catalogue's generated static
// methods and runtime builders, and checks the relations between their results. The
// relations cover the pivot row and the partitions of a predicate, aggregates over those
// partitions, window numbering and totals, join and semi-join formulations, compounds and
// pages. A call the engine refuses fails the input as a native error. A broken relation
// fails it as a semantic mismatch.
//
// Takes f (*testing.F) which owns the worker and seeds.
// Takes suite (NativeFuzzSuite) which supplies native connections and the driver.
func FuzzSemanticRelations(f *testing.F, suite NativeFuzzSuite) {
	f.Helper()
	if suite.OpenNative == nil || suite.Dialect == nil || suite.Driver.Module == "" || !fuzzsemantic.Supported(suite.Engine) {
		require.Fail(f, "incomplete native semantic relation suite")
	}
	for _, seed := range fuzzsemantic.Seeds(suite.Engine) {
		f.Add(seed)
	}
	worker := &nativeWorker{process: nil, cache: nil, temporary: f.TempDir(), shape: -1, metadata: sync.Once{}}
	f.Cleanup(func() {
		assert.NoError(f, worker.close())
	})
	catalogue := semanticCatalogue(suite.Engine)
	f.Fuzz(func(t *testing.T, data []byte) {
		model, ok := fuzzsemantic.Decode(suite.Engine, data)
		if !ok {
			t.Skip("unsupported semantic relation envelope")
		}
		worker.prepareSequence(t, suite, catalogue, sequenceShape(model.Mode))
		response := exchangeSequence(t, suite, worker, sequenceInput{lane: "semantic", capabilities: fuzzsemantic.Capabilities(suite.Engine),
			schema: catalogue.schema, inserts: model.Inserts(), data: data})
		var observations []fuzzsemantic.Observation
		require.NoError(t, json.Unmarshal(response.Observations, &observations), "semantic relation observations")
		if problems := model.Problems(observations); len(problems) != 0 {
			require.Failf(t, "semantic relation mismatch", "input %q\n%s\n%s", data, model, describeSemanticProblems(problems))
		}
	})
}

// describeSemanticProblems lists native errors before semantic mismatches.
//
// Takes problems ([]fuzzsemantic.Problem) which holds the findings of one input.
//
// Returns string which names each finding's kind so triage can tell them apart.
func describeSemanticProblems(problems []fuzzsemantic.Problem) string {
	lines := make([]string, 0, len(problems))
	for _, kind := range []string{fuzzsemantic.KindHarness, fuzzsemantic.KindNativeError, fuzzsemantic.KindMismatch} {
		for _, problem := range problems {
			if problem.Kind == kind {
				lines = append(lines, problem.String())
			}
		}
	}
	return strings.Join(lines, "\n")
}
