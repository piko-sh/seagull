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

package check

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
)

const returningQuerySQL = "-- sgl.query(CreateUser, one)\n" +
	"INSERT INTO users (name)\n" +
	"VALUES ($1) RETURNING id, name;"

func returningEngine(supported ast.DataChange) *fakedialect.Engine {
	return &fakedialect.Engine{
		DialectFunc:   func() string { return "nordic" },
		ReturningFunc: func() ast.DataChange { return supported },
	}
}

func returningContext(analysis *ast.RawQueryAnalysis) *DiagnosticContext {
	return &DiagnosticContext{
		Filename: "users.sql",
		Query: &sqlmodel.AnalysedQuery{
			Name:    "CreateUser",
			Command: sqlmodel.QueryCommandOne,
			SQL:     returningQuerySQL,
			Line:    7,
		},
		RawAnalysis: analysis,
	}
}

func TestReturningSupportPass_ReportsTheClauseOnADialectWithoutReturning(t *testing.T) {
	t.Parallel()

	location := strings.Index(returningQuerySQL, "RETURNING")
	analysis := &ast.RawQueryAnalysis{HasReturning: true, ReturningLocation: location}

	diagnostics := (&returningSupportPass{dialect: newDialectFacts(returningEngine(0))}).Analyse(t.Context(), returningContext(analysis))

	require.Equal(t, []diag.SourceError{{
		Filename:   "users.sql",
		Message:    `query "CreateUser" has a RETURNING clause, but the nordic dialect does not support RETURNING`,
		Code:       diag.CodeReturningNotSupported,
		Suggestion: "",
		Line:       9,
		Column:     13,
		EndLine:    9,
		EndColumn:  22,
		Severity:   diag.SeverityError,
	}}, diagnostics)
}

func TestReturningSupportPass_JudgesEachStatementKind(t *testing.T) {
	t.Parallel()

	supported := ast.DataChangeInsert | ast.DataChangeDelete
	testCases := []struct {
		name            string
		expectedMessage string
		change          ast.DataChange
	}{
		{name: "a supported INSERT", change: ast.DataChangeInsert, expectedMessage: ""},
		{name: "a supported DELETE", change: ast.DataChangeDelete, expectedMessage: ""},
		{
			name:            "an UPDATE the dialect does not support",
			change:          ast.DataChangeUpdate,
			expectedMessage: `query "CreateUser" has a RETURNING clause, but the nordic dialect does not support RETURNING on UPDATE`,
		},
		{
			name:            "a MERGE the dialect does not support",
			change:          ast.DataChangeMerge,
			expectedMessage: `query "CreateUser" has a RETURNING clause, but the nordic dialect does not support RETURNING on MERGE`,
		},
		{name: "a statement the dialect did not classify", change: 0, expectedMessage: ""},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := &ast.RawQueryAnalysis{HasReturning: true, DataChange: testCase.change}

			diagnostics := (&returningSupportPass{dialect: newDialectFacts(returningEngine(supported))}).Analyse(t.Context(), returningContext(analysis))

			if testCase.expectedMessage == "" {
				require.Empty(t, diagnostics)
				return
			}
			require.Len(t, diagnostics, 1)
			require.Equal(t, diag.CodeReturningNotSupported, diagnostics[0].Code)
			require.Equal(t, testCase.expectedMessage, diagnostics[0].Message)
		})
	}
}

func TestReturningSupportPass_NamesTheStatementOnADialectWithoutReturning(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		expectedMessage string
		change          ast.DataChange
	}{
		{
			name:   "an INSERT",
			change: ast.DataChangeInsert,
			expectedMessage: `query "CreateUser" has a RETURNING clause on its INSERT, but the nordic dialect does not support ` +
				`RETURNING on any statement`,
		},
		{
			name:   "a DELETE",
			change: ast.DataChangeDelete,
			expectedMessage: `query "CreateUser" has a RETURNING clause on its DELETE, but the nordic dialect does not support ` +
				`RETURNING on any statement`,
		},
		{
			name:            "a statement the dialect did not classify",
			change:          0,
			expectedMessage: `query "CreateUser" has a RETURNING clause, but the nordic dialect does not support RETURNING`,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := &ast.RawQueryAnalysis{HasReturning: true, DataChange: testCase.change}

			diagnostics := (&returningSupportPass{dialect: newDialectFacts(returningEngine(0))}).Analyse(t.Context(), returningContext(analysis))

			require.Len(t, diagnostics, 1)
			require.Equal(t, testCase.expectedMessage, diagnostics[0].Message)
		})
	}
}

func TestReturningSupportPass_FallsBackToTheQueryWithoutALocation(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		location int
	}{
		{name: "location not recorded", location: 0},
		{name: "location before the SQL", location: -4},
		{name: "location past the SQL", location: len(returningQuerySQL) + 10},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := &ast.RawQueryAnalysis{HasReturning: true, ReturningLocation: testCase.location}

			diagnostics := (&returningSupportPass{dialect: newDialectFacts(returningEngine(0))}).Analyse(t.Context(), returningContext(analysis))

			require.Len(t, diagnostics, 1)
			require.Equal(t, diag.CodeReturningNotSupported, diagnostics[0].Code)
			require.Equal(t, diag.SeverityError, diagnostics[0].Severity)
			require.Equal(t, 7, diagnostics[0].Line)
			require.Equal(t, 1, diagnostics[0].Column)
			require.Zero(t, diagnostics[0].EndLine)
			require.Zero(t, diagnostics[0].EndColumn)
		})
	}
}

func TestReturningSupportPass_StaysQuiet(t *testing.T) {
	t.Parallel()

	location := strings.Index(returningQuerySQL, "RETURNING")
	testCases := []struct {
		engine   dialect.DialectPort
		analysis *ast.RawQueryAnalysis
		name     string
	}{
		{
			name:     "dialect supports RETURNING",
			engine:   returningEngine(ast.DataChangeInsert | ast.DataChangeUpdate | ast.DataChangeDelete),
			analysis: &ast.RawQueryAnalysis{HasReturning: true, ReturningLocation: location},
		},
		{
			name:     "statement has no RETURNING clause",
			engine:   returningEngine(0),
			analysis: &ast.RawQueryAnalysis{HasReturning: false},
		},
		{
			name:     "no engine wired",
			engine:   nil,
			analysis: &ast.RawQueryAnalysis{HasReturning: true, ReturningLocation: location},
		},
		{
			name:     "no raw analysis",
			engine:   returningEngine(0),
			analysis: nil,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			diagnostics := (&returningSupportPass{dialect: newDialectFacts(testCase.engine)}).Analyse(t.Context(), returningContext(testCase.analysis))

			require.Empty(t, diagnostics)
		})
	}
}

func TestDiagnosticAnalyser_RunsTheReturningSupportPass(t *testing.T) {
	t.Parallel()

	analysis := &ast.RawQueryAnalysis{HasReturning: true, ReturningLocation: strings.Index(returningQuerySQL, "RETURNING")}
	subject := returningContext(analysis)
	subject.Query.OutputColumns = []sqlmodel.OutputColumn{{Name: "id"}, {Name: "name"}}

	diagnostics := NewDiagnosticAnalyser(returningEngine(0), fakedialect.NewCatalogue("main")).Analyse(t.Context(), subject)

	codes := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		codes = append(codes, diagnostic.Code)
	}
	require.Contains(t, codes, diag.CodeReturningNotSupported)
}
