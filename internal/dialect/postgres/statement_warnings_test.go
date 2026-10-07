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

package postgres

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func TestAStatementReportsAtMostItsCapOfWarnings(t *testing.T) {
	t.Parallel()

	numbered := func(count int) string {
		parameters := make([]string, count)
		for index := range parameters {
			parameters[index] = "-$" + strconv.Itoa(index+1)
		}
		return strings.Join(parameters, ", ")
	}
	testCases := []struct {
		name        string
		sql         string
		wantCount   int
		wantSummary string
	}{
		{
			name:        "thousands of @name operators",
			sql:         "SELECT id FROM users WHERE " + strings.TrimSuffix(strings.Repeat("@a + ", 9_000), " + ") + " > 0",
			wantCount:   maxStatementWarnings + 1,
			wantSummary: "and 8968 more warnings about this statement were left out",
		},
		{
			name:        "many untyped parameters",
			sql:         "SELECT " + numbered(100),
			wantCount:   maxStatementWarnings + 1,
			wantSummary: "and 68 more warnings about this statement were left out",
		},
		{
			name:        "@name operators and untyped parameters together",
			sql:         "SELECT " + numbered(20) + " FROM users WHERE " + strings.TrimSuffix(strings.Repeat("@a + ", 13), " + ") + " > 0",
			wantCount:   maxStatementWarnings + 1,
			wantSummary: "and 1 more warning about this statement was left out",
		},
		{
			name:      "exactly the cap",
			sql:       "SELECT " + numbered(maxStatementWarnings),
			wantCount: maxStatementWarnings,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newPostgresCatalogue(), testCase.sql)

			require.Len(t, analysis.Warnings, testCase.wantCount)
			last := analysis.Warnings[len(analysis.Warnings)-1]
			if testCase.wantSummary == "" {
				assert.NotContains(t, last.Message, "left out")
				return
			}
			assert.Equal(t, testCase.wantSummary, last.Message)
			assert.False(t, last.Span.Known())
		})
	}
}

func TestCappingWarningsKeepsTheirWorkLinear(t *testing.T) {
	t.Parallel()

	statement := func(count int) string {
		parameters := make([]string, count)
		for index := range parameters {
			parameters[index] = "-$" + strconv.Itoa(index+1)
		}
		return "SELECT " + strings.Join(parameters, ", ")
	}
	smaller, larger := statement(2_000), statement(8_000)
	engine := New()
	analyse := func(sql string) func() {
		return func() {
			statements, err := engine.ParseStatements(t.Context(), sql)
			require.NoError(t, err)
			analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])
			require.NoError(t, err)
			require.Len(t, analysis.Warnings, maxStatementWarnings+1)
		}
	}

	scaling.Linear(t, analyse(smaller), analyse(larger))
}

func TestFinishingWarningsSummarisesThoseLeftOut(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		found       int
		wantKept    int
		wantSummary string
	}{
		{name: "none", found: 0, wantKept: 0, wantSummary: ""},
		{name: "fewer than the cap", found: 3, wantKept: 3, wantSummary: ""},
		{name: "one past the cap", found: maxStatementWarnings + 1, wantKept: maxStatementWarnings,
			wantSummary: "and 1 more warning about this statement was left out"},
		{name: "many past the cap", found: maxStatementWarnings + 10, wantKept: maxStatementWarnings,
			wantSummary: "and 10 more warnings about this statement were left out"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var warnings statementWarnings
			for index := range testCase.found {
				if warnings.room() {
					warnings.add(ast.Warning{Message: strconv.Itoa(index), Span: ast.Span{Start: index, End: index + 1}})
				}
			}
			finished := warnings.finish()

			if testCase.wantSummary == "" {
				assert.Len(t, finished, testCase.wantKept)
				return
			}
			require.Len(t, finished, testCase.wantKept+1)
			assert.Equal(t, testCase.wantSummary, finished[testCase.wantKept].Message)
			assert.Equal(t, strconv.Itoa(testCase.wantKept-1), finished[testCase.wantKept-1].Message)
		})
	}
}
