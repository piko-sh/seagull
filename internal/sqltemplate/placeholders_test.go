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

package sqltemplate_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/dialect/sqlite"
	"seagull.sh/seagull/internal/directive/syntax"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/sqltemplate"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func TestBuildReportsThePlaceholdersOfEachEarlierStatementOnce(t *testing.T) {
	t.Parallel()

	const advice = ", which the generated code binds no value to; bind parameters only in the query's statement"
	testCases := []struct {
		name     string
		sql      string
		messages []string
	}{
		{
			name:     "one placeholder",
			sql:      "SELECT set_config('app.tenant', $1, true);\nSELECT id FROM items WHERE owner = $1;",
			messages: []string{"the placeholder $1 is in a statement before the query's own" + advice},
		},
		{
			name:     "several placeholders in one statement",
			sql:      "SELECT set_config($1, $2, $3);\nSELECT id FROM items WHERE owner = $1;",
			messages: []string{"the placeholder $1 and 2 more are in a statement before the query's own" + advice},
		},
		{
			name: "placeholders in two statements",
			sql:  "SELECT set_config('a', $1, true);\nSELECT 1;\nSELECT set_config($1, $1, true);\nSELECT id FROM items WHERE owner = $1;",
			messages: []string{
				"the placeholder $1 is in a statement before the query's own" + advice,
				"the placeholder $1 and 1 more are in a statement before the query's own" + advice,
			},
		},
		{
			name:     "no placeholder before the query's statement",
			sql:      "SELECT 1;\nSELECT id FROM items WHERE owner = $1;",
			messages: nil,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, problems := buildTemplate(t, postgres.New(), testCase.sql, nil)

			var messages []string
			for _, problem := range problems {
				require.Equal(t, diag.CodeParametersBeforeStatement, problem.Code)
				require.Equal(t, "$1", testCase.sql[problem.Span.Start:problem.Span.End])
				messages = append(messages, problem.Message)
			}
			require.Equal(t, testCase.messages, messages)
		})
	}
}

func TestBuildMatchesEachPlaceholderToTheFirstParameterItNames(t *testing.T) {
	t.Parallel()

	runBuildCases(t, []buildCase{
		{
			name:     "a numbered placeholder is matched by number, whatever the parameters' order",
			engine:   postgres.New(),
			sql:      "SELECT id FROM items WHERE a = $2 AND b = $1",
			template: "SELECT id FROM items WHERE a = ⟨p0⟩ AND b = ⟨p1⟩⟨arguments p0 p1⟩",
			configure: func(input *sqltemplate.Input) {
				parameters := input.Query.Parameters
				parameters[0], parameters[1] = parameters[1], parameters[0]
			},
		},
		{
			name:     "a named placeholder is matched by its placeholder identifier before a parameter's name",
			engine:   sqlite.New(),
			sql:      "SELECT id FROM items WHERE a = :kind",
			template: "SELECT id FROM items WHERE a = ⟨p1⟩⟨arguments p1⟩",
			configure: func(input *sqltemplate.Input) {
				input.Query.Parameters = []sqlmodel.QueryParameter{
					{Name: "other", Placeholder: "", Number: 2},
					{Name: "renamed", Placeholder: "kind", Number: 1},
					{Name: "kind", Placeholder: "kind_too", Number: 3},
				}
			},
		},
	})
}

func TestBuildScalesWithTheNumberOfPlaceholders(t *testing.T) {
	t.Parallel()

	smaller := inListInput(t, 1_000)
	larger := inListInput(t, 4*1_000)

	built, problems := sqltemplate.Build(larger)
	require.Empty(t, problems)
	require.NotEmpty(t, built.Template.Segments)
	scaling.Linear(t, func() { sqltemplate.Build(smaller) }, func() { sqltemplate.Build(larger) })
}

func inListInput(t *testing.T, placeholders int) *sqltemplate.Input {
	t.Helper()

	written := make([]string, placeholders)
	for index := range written {
		written[index] = fmt.Sprintf("$%d", index+1)
	}
	sql := "SELECT id FROM items WHERE id IN (" + strings.Join(written, ", ") + ")"
	engine := postgres.New()
	info := engine.Info()
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[len(statements)-1])
	require.NoError(t, err)
	lexicon := syntax.Lexicon{LinePrefix: info.Comments.LinePrefix, Rules: info.Lexical}
	ranges := syntax.StripRanges(sql, lexicon, namespace.Default())
	kept := make([]ast.Span, 0, len(ranges))
	for _, keptRange := range ranges {
		kept = append(kept, ast.Span{Start: keptRange.Start, End: keptRange.End})
	}
	return &sqltemplate.Input{
		Info:       &info,
		Query:      &sqlmodel.AnalysedQuery{Parameters: parametersOf(analysis)},
		Analysis:   analysis,
		SQL:        sql,
		Statements: statements,
		Kept:       kept,
	}
}
