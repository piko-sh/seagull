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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
)

func TestCommandOutputPass(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		command       sqlmodel.QueryCommand
		outputColumns []sqlmodel.OutputColumn
		expectedCount int
	}{
		{
			name:    "one with output columns produces no diagnostic",
			command: sqlmodel.QueryCommandOne,
			outputColumns: []sqlmodel.OutputColumn{
				{Name: "id"},
			},
			expectedCount: 0,
		},
		{
			name:          "one without columns produces warning",
			command:       sqlmodel.QueryCommandOne,
			outputColumns: nil,
			expectedCount: 1,
		},
		{
			name:    "exec with columns produces warning",
			command: sqlmodel.QueryCommandExec,
			outputColumns: []sqlmodel.OutputColumn{
				{Name: "id"},
			},
			expectedCount: 1,
		},
		{
			name:          "exec without columns produces no diagnostic",
			command:       sqlmodel.QueryCommandExec,
			outputColumns: nil,
			expectedCount: 0,
		},
		{
			name:    "many with columns produces no diagnostic",
			command: sqlmodel.QueryCommandMany,
			outputColumns: []sqlmodel.OutputColumn{
				{Name: "id"},
				{Name: "email"},
			},
			expectedCount: 0,
		},
		{
			name:    "stream with columns produces no diagnostic",
			command: sqlmodel.QueryCommandStream,
			outputColumns: []sqlmodel.OutputColumn{
				{Name: "id"},
			},
			expectedCount: 0,
		},
		{
			name:          "many without columns produces warning",
			command:       sqlmodel.QueryCommandMany,
			outputColumns: nil,
			expectedCount: 1,
		},
		{
			name:    "execresult with columns produces warning",
			command: sqlmodel.QueryCommandExecResult,
			outputColumns: []sqlmodel.OutputColumn{
				{Name: "id"},
			},
			expectedCount: 1,
		},
		{
			name:    "execrows with columns produces warning",
			command: sqlmodel.QueryCommandExecRows,
			outputColumns: []sqlmodel.OutputColumn{
				{Name: "id"},
			},
			expectedCount: 1,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			pass := &commandOutputPass{}
			subject := &DiagnosticContext{
				Filename: "test.sql",
				Query: &sqlmodel.AnalysedQuery{
					Name:          "TestQuery",
					Command:       testCase.command,
					OutputColumns: testCase.outputColumns,
					Line:          3,
				},
				RawAnalysis: &ast.RawQueryAnalysis{},
			}

			diagnostics := pass.Analyse(t.Context(), subject)

			require.Len(t, diagnostics, testCase.expectedCount)

			if testCase.expectedCount > 0 {
				assert.Equal(t, "Q009", diagnostics[0].Code)
				assert.Equal(t, diag.SeverityWarning, diagnostics[0].Severity)
				assert.Contains(t, diagnostics[0].Message, `"TestQuery"`)
			}
		})
	}
}

func TestCommandOutputPassAcceptsAnExecOfAVoidResult(t *testing.T) {
	t.Parallel()

	void := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "void")
	withVoid := &fakedialect.Engine{CoreTypesFunc: func() dialect.CoreTypes {
		core := fakedialect.DefaultCoreTypes()
		core.Void = void
		return core
	}}
	tests := []struct {
		name          string
		engine        dialect.DialectPort
		outputColumns []sqlmodel.OutputColumn
		expectedCount int
	}{
		{
			name:          "an exec whose only column is void produces no diagnostic",
			engine:        withVoid,
			outputColumns: []sqlmodel.OutputColumn{{Name: "pg_advisory_lock", SQLType: void}},
			expectedCount: 0,
		},
		{
			name:   "an exec returning a value beside a void column produces a warning",
			engine: withVoid,
			outputColumns: []sqlmodel.OutputColumn{
				{Name: "pg_advisory_lock", SQLType: void},
				{Name: "id", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "int4")},
			},
			expectedCount: 1,
		},
		{
			name:          "a dialect with no void type warns about an untyped column",
			engine:        &fakedialect.Engine{},
			outputColumns: []sqlmodel.OutputColumn{{Name: "anything", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")}},
			expectedCount: 1,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			pass := &commandOutputPass{dialect: newDialectFacts(testCase.engine)}
			subject := &DiagnosticContext{
				Filename: "test.sql",
				Query: &sqlmodel.AnalysedQuery{
					Name:          "Lock",
					Command:       sqlmodel.QueryCommandExec,
					OutputColumns: testCase.outputColumns,
					Line:          1,
				},
				RawAnalysis: &ast.RawQueryAnalysis{},
			}

			require.Len(t, pass.Analyse(t.Context(), subject), testCase.expectedCount)
		})
	}
}

func TestCommandOutputPassWarnsWhenABatchDiscardsRows(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		command       sqlmodel.QueryCommand
		outputColumns []sqlmodel.OutputColumn
		want          []string
	}{
		{
			name:          "a batch over a select",
			command:       sqlmodel.QueryCommandBatch,
			outputColumns: []sqlmodel.OutputColumn{{Name: "bio"}, {Name: "published"}},
			want: []string{`query "Rows" uses command "batch", whose method returns no rows, but its statement returns 2 output ` +
				"columns, which are discarded; run a query that reads rows with one or many for each item, or leave RETURNING out"},
		},
		{
			name:          "a copyfrom with a returning clause",
			command:       sqlmodel.QueryCommandCopyFrom,
			outputColumns: []sqlmodel.OutputColumn{{Name: "uuid"}},
			want: []string{`query "Rows" uses command "copyfrom", whose method returns no rows, but its statement returns 1 output ` +
				"columns, which are discarded; run a query that reads rows with one or many for each item, or leave RETURNING out"},
		},
		{name: "a batch returning nothing", command: sqlmodel.QueryCommandBatch, outputColumns: nil, want: nil},
		{name: "a copyfrom returning nothing", command: sqlmodel.QueryCommandCopyFrom, outputColumns: nil, want: nil},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			pass := &commandOutputPass{}
			subject := &DiagnosticContext{
				Filename: "rows.sql",
				Query: &sqlmodel.AnalysedQuery{
					Name:          "Rows",
					Command:       testCase.command,
					OutputColumns: testCase.outputColumns,
					Line:          4,
				},
				RawAnalysis: &ast.RawQueryAnalysis{},
			}

			var messages []string
			for _, diagnostic := range pass.Analyse(t.Context(), subject) {
				require.Equal(t, diag.CodeCommandOutputMismatch, diagnostic.Code)
				require.Equal(t, diag.SeverityWarning, diagnostic.Severity)
				messages = append(messages, diagnostic.Message)
			}
			require.Equal(t, testCase.want, messages)
		})
	}
}

func TestCommandOutputPassConflictDoNothingReturning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		command      sqlmodel.QueryCommand
		sql          string
		hasReturning bool
		optional     bool
		wantWarning  bool
	}{
		{
			name:         "one with DO NOTHING and RETURNING warns",
			command:      sqlmodel.QueryCommandOne,
			sql:          "INSERT INTO t (id) VALUES ($1) ON CONFLICT (id) DO NOTHING RETURNING id",
			hasReturning: true,
			wantWarning:  true,
		},
		{
			name:         "optional suppresses the warning for one with DO NOTHING and RETURNING",
			command:      sqlmodel.QueryCommandOne,
			sql:          "INSERT INTO t (id) VALUES ($1) ON CONFLICT (id) DO NOTHING RETURNING id",
			hasReturning: true,
			optional:     true,
			wantWarning:  false,
		},
		{
			name:         "DO UPDATE does not warn",
			command:      sqlmodel.QueryCommandOne,
			sql:          "INSERT INTO t (id) VALUES ($1) ON CONFLICT (id) DO UPDATE SET id = $1 RETURNING id",
			hasReturning: true,
			wantWarning:  false,
		},
		{
			name:         "many with DO NOTHING does not warn",
			command:      sqlmodel.QueryCommandMany,
			sql:          "INSERT INTO t (id) VALUES ($1) ON CONFLICT (id) DO NOTHING RETURNING id",
			hasReturning: true,
			wantWarning:  false,
		},
		{
			name:         "one with DO NOTHING but no RETURNING does not warn",
			command:      sqlmodel.QueryCommandOne,
			sql:          "INSERT INTO t (id) VALUES ($1) ON CONFLICT (id) DO NOTHING",
			hasReturning: false,
			wantWarning:  false,
		},
		{
			name:         "lowercase do nothing across newlines and multiple spaces warns",
			command:      sqlmodel.QueryCommandOne,
			sql:          "insert into t (id) values ($1)\non conflict (id)\ndo   nothing\nreturning id",
			hasReturning: true,
			wantWarning:  true,
		},
		{
			name:         "DO NOTHING only inside a string literal without ON CONFLICT does not warn",
			command:      sqlmodel.QueryCommandOne,
			sql:          "INSERT INTO logs (action) VALUES ('DO NOTHING') RETURNING id",
			hasReturning: true,
			wantWarning:  false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			pass := &commandOutputPass{}
			subject := &DiagnosticContext{
				Filename: "test.sql",
				Query: &sqlmodel.AnalysedQuery{
					Name:          "TestQuery",
					Command:       testCase.command,
					SQL:           testCase.sql,
					Optional:      testCase.optional,
					OutputColumns: []sqlmodel.OutputColumn{{Name: "id"}},
					Line:          3,
				},
				RawAnalysis: &ast.RawQueryAnalysis{HasReturning: testCase.hasReturning},
			}

			var found bool
			for _, diagnostic := range pass.Analyse(t.Context(), subject) {
				if diagnostic.Code == diag.CodeConflictDoNothingReturning {
					found = true
					assert.Equal(t, diag.SeverityWarning, diagnostic.Severity)
					assert.Contains(t, diagnostic.Message, "sql.ErrNoRows")
				}
			}
			assert.Equal(t, testCase.wantWarning, found)
		})
	}
}

func TestCommandOutputPassOptionalMisuse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		command        sqlmodel.QueryCommand
		optional       bool
		isDynamic      bool
		dynamicRuntime bool
		wantError      bool
	}{
		{
			name:      "optional on command:many is rejected",
			command:   sqlmodel.QueryCommandMany,
			optional:  true,
			wantError: true,
		},
		{
			name:      "optional on a command:one with dynamic predicate parameters is rejected",
			command:   sqlmodel.QueryCommandOne,
			optional:  true,
			isDynamic: true,
			wantError: true,
		},
		{
			name:           "optional on a command:one with dynamic runtime builder is rejected",
			command:        sqlmodel.QueryCommandOne,
			optional:       true,
			dynamicRuntime: true,
			wantError:      true,
		},
		{
			name:      "optional on a static command:one is valid",
			command:   sqlmodel.QueryCommandOne,
			optional:  true,
			wantError: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			pass := &commandOutputPass{}
			subject := &DiagnosticContext{
				Filename: "test.sql",
				Query: &sqlmodel.AnalysedQuery{
					Name:           "TestQuery",
					Command:        testCase.command,
					Optional:       testCase.optional,
					IsDynamic:      testCase.isDynamic,
					DynamicRuntime: testCase.dynamicRuntime,
					OutputColumns:  []sqlmodel.OutputColumn{{Name: "id"}},
					Line:           3,
				},
				RawAnalysis: &ast.RawQueryAnalysis{},
			}

			var found bool
			for _, diagnostic := range pass.Analyse(t.Context(), subject) {
				if diagnostic.Code == diag.CodeOptionalNonOneCommand {
					found = true
					assert.Equal(t, diag.SeverityError, diagnostic.Severity)
					assert.Contains(t, diagnostic.Message, `"TestQuery"`)
				}
			}
			assert.Equal(t, testCase.wantError, found)
		})
	}
}

func TestCommandOutputPassReadsTheConflictActionNotCommentsOrStrings(t *testing.T) {
	t.Parallel()

	const upsert = "INSERT INTO t (id) VALUES ($1) ON CONFLICT (id) DO UPDATE SET id = $1 RETURNING id"
	const skip = "INSERT INTO t (id) VALUES ($1) ON CONFLICT (id) DO NOTHING RETURNING id"
	testCases := []struct {
		name        string
		sql         string
		action      ast.ConflictAction
		wantWarning bool
	}{
		{name: "an upsert whose header comment mentions DO NOTHING",
			sql: "-- sgl.query(name: Upsert, command: one)\n-- Unlike ON CONFLICT DO NOTHING, this updates the row.\n" + upsert},
		{name: "an upsert whose block comment mentions DO NOTHING",
			sql: "INSERT INTO t (id) VALUES ($1) /* not ON CONFLICT DO NOTHING */ ON CONFLICT (id) DO UPDATE SET id = $1 RETURNING id"},
		{name: "ON CONFLICT DO NOTHING inside a string literal",
			sql: "INSERT INTO notes (body) VALUES ('ON CONFLICT DO NOTHING') RETURNING id"},
		{name: "keywords written as quoted identifiers",
			sql: `INSERT INTO t (id) VALUES ($1) ON "conflict" (id) "do" NOTHING RETURNING id`},
		{name: "a statement that skips conflicting rows", sql: skip, wantWarning: true},
		{name: "a skip the dialect recorded", sql: "INSERT OR IGNORE INTO t (id) VALUES ($1) RETURNING id",
			action: ast.ConflictActionDoNothing, wantWarning: true},
		{name: "an update the dialect recorded over text that reads as a skip", sql: skip, action: ast.ConflictActionDoUpdate},
		{name: "no conflict clause the dialect recorded over text that reads as a skip", sql: skip, action: ast.ConflictActionNone},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			subject := &DiagnosticContext{
				Filename: "test.sql",
				Query: &sqlmodel.AnalysedQuery{Name: "Upsert", Command: sqlmodel.QueryCommandOne, SQL: testCase.sql,
					OutputColumns: []sqlmodel.OutputColumn{{Name: "id"}}, Line: 3},
				RawAnalysis: &ast.RawQueryAnalysis{HasReturning: true, ConflictAction: testCase.action},
			}

			found := false
			for _, diagnostic := range (&commandOutputPass{dialect: newDialectFacts(&fakedialect.Engine{})}).Analyse(t.Context(), subject) {
				found = found || diagnostic.Code == diag.CodeConflictDoNothingReturning
			}

			require.Equal(t, testCase.wantWarning, found)
		})
	}
}
