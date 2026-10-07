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

package pipeline

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
)

type refusingEmitter struct {
	fakedialect.Emitter
	refuse func(query *sqlmodel.AnalysedQuery) (emit.Problem, bool)
}

func (e *refusingEmitter) Check(request *emit.Request) []emit.Problem {
	var problems []emit.Problem
	for _, query := range request.Queries {
		if problem, refused := e.refuse(query); refused {
			problems = append(problems, problem)
		}
	}
	return problems
}

type rawSQL string

func (rawSQL) IsParsedStatement() {}

func skippingInput(migration string) testRequest {
	return testRequest{
		packageName: "store",
		migrations: fstest.MapFS{
			"migrations/001_init.up.sql": &fstest.MapFile{Data: []byte(migration)},
			"queries/items.sql": &fstest.MapFile{Data: []byte(
				"-- sgl.query(ListItems, many)\nSELECT id FROM items;\n\n" +
					"-- sgl.query(MergeItems, exec)\nMERGE INTO items USING other ON true WHEN MATCHED THEN DELETE;\n\n" +
					"-- sgl.query(CountItems, one)\nSELECT count(*) FROM items;\n")},
		},
		migrationDirectory: "migrations",
		queryDirectory:     "queries",
	}
}

func refusingEngine() *fakedialect.Engine {
	return &fakedialect.Engine{
		ParseStatementsFunc: func(_ context.Context, sql string) ([]ast.ParsedStatement, error) {
			return []ast.ParsedStatement{{Raw: rawSQL(sql), Location: 0, Length: len(sql)}}, nil
		},
		ApplyDDLFunc: func(_ context.Context, statement ast.ParsedStatement) (*ast.CatalogueMutation, error) {
			if strings.Contains(string(statement.Raw.(rawSQL)), "BROKEN") {
				return nil, errors.New("the migration cannot be parsed")
			}
			return nil, nil
		},
		AnalyseQueryFunc: func(_ context.Context, _ *sqlmodel.Catalogue, statement ast.ParsedStatement) (*ast.RawQueryAnalysis, error) {
			if strings.Contains(string(statement.Raw.(rawSQL)), "MERGE") {
				return nil, errors.New("MERGE is not supported")
			}
			return &ast.RawQueryAnalysis{ReadOnly: true}, nil
		},
	}
}

func emittedNames(request *emit.Request) []string {
	names := make([]string, 0, len(request.Queries))
	for _, query := range request.Queries {
		names = append(names, query.Name)
	}
	return names
}

func generatedFiles(context.Context, *emit.Request) ([]sqlmodel.GeneratedFile, error) {
	return []sqlmodel.GeneratedFile{{Name: "querier.go"}}, nil
}

func TestGenerateSkipsAQueryWithErrorsOfItsOwn(t *testing.T) {
	t.Parallel()

	emitter := &fakedialect.Emitter{EmitFunc: generatedFiles}
	generator, err := New(Config{Dialect: refusingEngine(), Emitter: emitter, SkipFailingQueries: true})
	require.NoError(t, err)

	generation, err := generator.Generate(t.Context(), skippingInput("CREATE TABLE items (id int);"))
	require.NoError(t, err)

	require.True(t, generation.Partial)
	require.Len(t, generation.Files, 1)
	require.Len(t, emitter.Requests, 1)
	require.Equal(t, []string{"ListItems", "CountItems"}, emittedNames(emitter.Requests[0]))
	require.Len(t, generation.Queries, 2)
	errorCodes := map[string]int{}
	for _, diagnostic := range generation.Diagnostics {
		if diagnostic.Severity == diag.SeverityError {
			errorCodes[diagnostic.Code]++
		}
	}
	require.Equal(t, map[string]int{diag.CodeParseError: 1}, errorCodes)
}

func TestGenerateStopsWithoutSkipping(t *testing.T) {
	t.Parallel()

	emitter := &fakedialect.Emitter{EmitFunc: generatedFiles}
	generator, err := New(Config{Dialect: refusingEngine(), Emitter: emitter})
	require.NoError(t, err)

	generation, err := generator.Generate(t.Context(), skippingInput("CREATE TABLE items (id int);"))
	require.NoError(t, err)

	require.False(t, generation.Partial)
	require.Empty(t, generation.Files)
	require.Empty(t, emitter.Requests)
}

func TestGenerateStopsOnAnErrorThatIsNoQuerysOwn(t *testing.T) {
	t.Parallel()

	emitter := &fakedialect.Emitter{EmitFunc: generatedFiles}
	generator, err := New(Config{Dialect: refusingEngine(), Emitter: emitter, SkipFailingQueries: true})
	require.NoError(t, err)

	generation, err := generator.Generate(t.Context(), skippingInput("CREATE TABLE items (id int);\nBROKEN;"))
	require.NoError(t, err)

	require.False(t, generation.Partial)
	require.Empty(t, generation.Files)
	require.Empty(t, emitter.Requests)
}

func TestGenerateSkipsAQueryTheEmitterRefuses(t *testing.T) {
	t.Parallel()

	emitter := &refusingEmitter{
		EmitFunc: generatedFiles,
		refuse: func(query *sqlmodel.AnalysedQuery) (emit.Problem, bool) {
			return emit.Problem{Query: query, Code: diag.CodeEmitterUnsupported, Message: "unsupported", Severity: diag.SeverityError},
				query.Name == "CountItems"
		},
	}
	generator, err := New(Config{Dialect: refusingEngine(), Emitter: emitter, SkipFailingQueries: true})
	require.NoError(t, err)

	generation, err := generator.Generate(t.Context(), skippingInput("CREATE TABLE items (id int);"))
	require.NoError(t, err)

	require.True(t, generation.Partial)
	require.Len(t, generation.Files, 1)
	require.Len(t, emitter.Requests, 1)
	require.Equal(t, []string{"ListItems"}, emittedNames(emitter.Requests[0]))
	codes := map[string]bool{}
	for _, diagnostic := range generation.Diagnostics {
		codes[diagnostic.Code] = true
	}
	require.True(t, codes[diag.CodeEmitterUnsupported])
	require.True(t, codes[diag.CodeParseError])
}

func TestGenerateStopsOnAnEmitterProblemWithTheWholeRequest(t *testing.T) {
	t.Parallel()

	emitter := &refusingEmitter{
		EmitFunc: generatedFiles,
		refuse: func(query *sqlmodel.AnalysedQuery) (emit.Problem, bool) {
			return emit.Problem{Query: nil, Code: diag.CodeEmitterUnsupported, Message: "the driver cannot", Severity: diag.SeverityError},
				query.Name == "ListItems"
		},
	}
	generator, err := New(Config{Dialect: refusingEngine(), Emitter: emitter, SkipFailingQueries: true})
	require.NoError(t, err)

	generation, err := generator.Generate(t.Context(), skippingInput("CREATE TABLE items (id int);"))
	require.NoError(t, err)

	require.False(t, generation.Partial)
	require.Empty(t, generation.Files)
	require.Empty(t, emitter.Requests)
}

func TestWithoutQueriesLeavesOutOnlyTheQueriesItHolds(t *testing.T) {
	t.Parallel()

	first, second, third := &sqlmodel.AnalysedQuery{Name: "First"}, &sqlmodel.AnalysedQuery{Name: "Second"}, &sqlmodel.AnalysedQuery{Name: "Third"}
	copied := *second
	tests := []struct {
		name    string
		leftOut []*sqlmodel.AnalysedQuery
		want    []*sqlmodel.AnalysedQuery
	}{
		{name: "nothing left out", leftOut: nil, want: []*sqlmodel.AnalysedQuery{first, second, third}},
		{name: "one left out", leftOut: []*sqlmodel.AnalysedQuery{second}, want: []*sqlmodel.AnalysedQuery{first, third}},
		{name: "a copy of a query is not the query", leftOut: []*sqlmodel.AnalysedQuery{&copied}, want: []*sqlmodel.AnalysedQuery{first, second, third}},
		{name: "every query left out", leftOut: []*sqlmodel.AnalysedQuery{third, first, second}, want: []*sqlmodel.AnalysedQuery{}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			queries := []*sqlmodel.AnalysedQuery{first, second, third}
			require.Equal(t, testCase.want, withoutQueries(queries, testCase.leftOut))
			require.Equal(t, []*sqlmodel.AnalysedQuery{first, second, third}, queries)
		})
	}
}

func TestRefusedQueriesNamesEachRefusedQueryOnce(t *testing.T) {
	t.Parallel()

	first, second := &sqlmodel.AnalysedQuery{Name: "First"}, &sqlmodel.AnalysedQuery{Name: "Second"}
	refusal := func(query *sqlmodel.AnalysedQuery, severity diag.ErrorSeverity) emit.Problem {
		return emit.Problem{Query: query, Code: diag.CodeEmitterUnsupported, Message: "unsupported", Severity: severity}
	}
	tests := []struct {
		name     string
		problems []emit.Problem
		want     []*sqlmodel.AnalysedQuery
	}{
		{name: "no problems", problems: nil, want: nil},
		{name: "warnings refuse nothing", problems: []emit.Problem{refusal(first, diag.SeverityWarning)}, want: nil},
		{name: "repeated refusals name a query once", problems: []emit.Problem{refusal(first, diag.SeverityError), refusal(second, diag.SeverityError), refusal(first, diag.SeverityError)},
			want: []*sqlmodel.AnalysedQuery{first, second}},
		{name: "a refusal of the whole request names none", problems: []emit.Problem{refusal(first, diag.SeverityError), refusal(nil, diag.SeverityError)}, want: nil},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, refusedQueries(testCase.problems))
		})
	}
}
