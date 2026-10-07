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

package facade_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite"
	"seagull.sh/seagull/sdk/emit"
	"seagull.sh/seagull/sdk/observe"
)

type detailedChecker struct {
	checks, emissions int
	fail              error
}

func (e *detailedChecker) Check(request *emit.Request) []emit.Problem {
	e.checks++
	for _, query := range request.Queries {
		if query.Name == "CreateTask" {
			return []emit.Problem{{Query: query, Code: "TEST", Message: "refused", Severity: seagull.SeverityError}}
		}
	}
	return []emit.Problem{{Code: "AFTER", Message: "only accepted queries remain", Severity: seagull.SeverityWarning}}
}

func (e *detailedChecker) Emit(_ context.Context, request *emit.Request) ([]emit.GeneratedFile, error) {
	e.emissions++
	if e.fail != nil {
		return nil, e.fail
	}

	request.Queries[0].OutputColumns[0].Name = "changed by emitter"
	return []emit.GeneratedFile{{Name: "queries.go", Content: []byte("package store\n")}}, nil
}

func TestGenerateDetailedMatchesSeparatePasses(t *testing.T) {
	for _, scenario := range []struct {
		name, extra string
		skip        bool
	}{
		{name: "valid"}, {name: "partial", extra: "-- sgl.query(Bad, many)\nSELECT missing FROM tasks;", skip: true},
		{name: "stopped", extra: "-- sgl.query(Bad, many)\nSELECT missing FROM tasks;"},
		{name: "malformed", extra: "-- sgl.query(Broken, nope)\nSELECT id FROM tasks;", skip: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			project := tasksProject()
			if scenario.extra != "" {
				project["db/queries/bad.sql"] = &fstest.MapFile{Data: []byte(scenario.extra)}
			}
			input := tasksInput
			input.Migrations = project
			options := []seagull.Option{}
			if scenario.skip {
				options = append(options, seagull.WithSkipFailingQueries())
			}
			generator, err := seagull.New(dialect_engine_sqlite.New(), options...)
			require.NoError(t, err)
			details, err := generator.GenerateDetailed(t.Context(), input)
			require.NoError(t, err)
			result, err := generator.Generate(t.Context(), input)
			require.NoError(t, err)
			report, err := generator.Vet(t.Context(), input)
			require.NoError(t, err)
			catalogue, diagnostics, err := generator.Catalogue(t.Context(), input)
			require.NoError(t, err)
			require.Equal(t, result, details.Result)
			require.Equal(t, report, details.Report)
			require.Equal(t, catalogue, details.Catalogue)
			require.Equal(t, diagnostics, details.CatalogueDiagnostics)
		})
	}
}

func TestGenerateDetailedPreservesFirstCheckerPass(t *testing.T) {
	emitter := new(detailedChecker)
	generator, err := seagull.New(dialect_engine_sqlite.New(), seagull.WithEmitter(emitter), seagull.WithSkipFailingQueries())
	require.NoError(t, err)
	details, err := generator.GenerateDetailed(t.Context(), tasksInput)
	require.NoError(t, err)
	require.Equal(t, 2, emitter.checks)
	require.Equal(t, 1, emitter.emissions)
	require.True(t, details.Result.Partial)
	require.Len(t, details.Report.Diagnostics, 1)
	require.Len(t, details.Result.Diagnostics, 2)
	for _, query := range details.Report.Queries {
		require.NotEqual(t, "changed by emitter", query.OutputColumns[0].Name)
	}
	other, err := seagull.New(dialect_engine_sqlite.New(), seagull.WithEmitter(new(detailedChecker)), seagull.WithSkipFailingQueries())
	require.NoError(t, err)
	report, err := other.Vet(t.Context(), tasksInput)
	require.NoError(t, err)
	require.Equal(t, report, details.Report)
}

func TestGenerateDetailedOwnsReturnedDataAndUsesOneAnalysis(t *testing.T) {
	source, input := sharedSchemaInput(t)
	observer := new(recordingObserver)
	generator, err := seagull.New(dialect_engine_sqlite.New(), seagull.WithSchemaSource(source), seagull.WithObserver(observer),
		seagull.WithCustomFunctions(seagull.CustomFunction{Name: "similarity", ReturnType: "real", Arguments: []string{"text", "text"}}))
	require.NoError(t, err)
	details, err := generator.GenerateDetailed(t.Context(), input)
	require.NoError(t, err)
	for _, stage := range []observe.Stage{observe.StageCatalogue, observe.StageAnalyse, observe.StagePlan, observe.StageEmit} {
		count := 0
		for _, seen := range observer.stages {
			if seen == stage {
				count++
			}
		}
		require.Equal(t, 1, count, string(stage))
	}
	details.Catalogue.Schemas["main"].Tables["tasks"].Columns[0].Name = "changed catalogue"
	details.Report.Queries[0].OutputColumns[0].Name = "changed report"
	details.Result.Files[0].Content[0] = '!'
	again, err := generator.GenerateDetailed(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, "id", source.catalogue.Schemas["main"].Tables["tasks"].Columns[0].Name)
	require.Equal(t, "id", again.Catalogue.Schemas["main"].Tables["tasks"].Columns[0].Name)
	require.NotEqual(t, "changed report", again.Report.Queries[0].OutputColumns[0].Name)
	require.NotEqual(t, byte('!'), again.Result.Files[0].Content[0])
}

func TestGenerateDetailedFatalErrors(t *testing.T) {
	generator, err := seagull.New(dialect_engine_sqlite.New())
	require.NoError(t, err)
	invalid := tasksInput
	invalid.Package = "!invalid"
	result, err := generator.GenerateDetailed(t.Context(), invalid)
	require.ErrorIs(t, err, seagull.ErrInvalidConfig)
	require.Nil(t, result)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result, err = generator.GenerateDetailed(ctx, tasksInput)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, result)
	failure := errors.New("emitter failed")
	generator, err = seagull.New(dialect_engine_sqlite.New(), seagull.WithEmitter(&detailedChecker{fail: failure}), seagull.WithSkipFailingQueries())
	require.NoError(t, err)
	result, err = generator.GenerateDetailed(t.Context(), tasksInput)
	require.ErrorIs(t, err, failure)
	require.Nil(t, result)
}

func TestGenerateDetailedConcurrent(t *testing.T) {
	source, input := sharedSchemaInput(t)
	generator, err := seagull.New(dialect_engine_sqlite.New(), seagull.WithSchemaSource(source), seagull.WithCustomFunctions(seagull.CustomFunction{Name: "similarity", ReturnType: "real", Arguments: []string{"text", "text"}}))
	require.NoError(t, err)
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			details, err := generator.GenerateDetailed(t.Context(), input)
			if !assert.NoError(t, err) {
				return
			}
			assert.NoError(t, details.Result.Err())
			details.Catalogue.Schemas["main"].Tables["tasks"].Columns[0].Name = "owned"
		})
	}
	workers.Wait()
}
