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

package analyse

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"seagull.sh/seagull/internal/check"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/clickhouse"
	"seagull.sh/seagull/internal/directive"
	"seagull.sh/seagull/internal/logging"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/resolve"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlfile"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/sqltemplate"
	"seagull.sh/seagull/internal/testkit/fakedialect"
	"seagull.sh/seagull/internal/testkit/scaling"
)

type infoCountingEngine struct {
	*fakedialect.Engine
	reads *int
}

func (engine infoCountingEngine) Info() dialect.Info {
	*engine.reads++
	return engine.Engine.Info()
}

func TestNewQueryAnalyser(t *testing.T) {
	t.Parallel()

	engine := &fakedialect.Engine{}
	catalogue := fakedialect.NewCatalogue("public")

	analyser := NewQueryAnalyser(engine, catalogue, directive.PrefixPolicy{Prefixes: namespace.Default(), RequireCanonical: false})

	require.NotNil(t, analyser, "newQueryAnalyser must return a non-nil analyser")
	assert.Equal(t, engine, analyser.engine, "engine must be stored")
	assert.Equal(t, catalogue, analyser.catalogue, "catalogue must be stored")
	assert.NotNil(t, analyser.directiveParser, "directiveParser must be initialised")
	assert.NotNil(t, analyser.typeResolver, "typeResolver must be initialised")
	assert.NotNil(t, analyser.diagnosticAnalyser, "diagnosticAnalyser must be initialised")
}

func TestQueryAnalyser_AnalyseQuery(t *testing.T) {
	t.Parallel()

	buildCatalogue := func() *sqlmodel.Catalogue {
		catalogue := fakedialect.NewCatalogue("public")
		catalogue.Schemas["public"].Tables["users"] = fakedialect.NewTable("users",
			sqlmodel.Column{
				Name:    "id",
				SQLType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger},
			},
			sqlmodel.Column{
				Name:    "name",
				SQLType: sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText},
			},
		)
		return catalogue
	}

	buildEngine := func(
		parseFunction func(context.Context, string) ([]ast.ParsedStatement, error),
		analyseFunction func(context.Context, *sqlmodel.Catalogue, ast.ParsedStatement) (*ast.RawQueryAnalysis, error),
	) *fakedialect.Engine {
		return &fakedialect.Engine{
			ParseStatementsFunc: parseFunction,
			AnalyseQueryFunc:    analyseFunction,
			CommentStyleFunc: func() ast.CommentStyle {
				return ast.CommentStyle{LinePrefix: "--"}
			},
			PlaceholdersFunc: func() dialect.Placeholders {
				numbered := dialect.PlaceholderSigil{Prefix: "$", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNumbered}
				return dialect.Placeholders{Sigils: []dialect.PlaceholderSigil{numbered}, Execution: numbered, Positional: dialect.PlaceholderSigil{}, NamePrefix: ""}
			},
		}
	}

	standardParseFunction := func(_ context.Context, sql string) ([]ast.ParsedStatement, error) {
		return []ast.ParsedStatement{{Location: 0, Length: len(sql)}}, nil
	}

	standardAnalyseFunction := func(_ context.Context, _ *sqlmodel.Catalogue, _ ast.ParsedStatement) (*ast.RawQueryAnalysis, error) {
		return &ast.RawQueryAnalysis{
			FromTables: []ast.TableReference{
				{Name: "users", Schema: "public"},
			},
			OutputColumns: []ast.RawOutputColumn{
				{
					Name:       "id",
					TableAlias: "users",
					ColumnName: "id",
					Expression: &ast.ColumnRefExpression{TableAlias: "users", ColumnName: "id"},
				},
				{
					Name:       "name",
					TableAlias: "users",
					ColumnName: "name",
					Expression: &ast.ColumnRefExpression{TableAlias: "users", ColumnName: "name"},
				},
			},
			ParameterReferences: []ast.RawParameterReference{
				{
					Number:          1,
					ColumnReference: &ast.ColumnReference{TableAlias: "users", ColumnName: "id"},
					Context:         sqlmodel.ParameterContextComparison,
				},
			},
			ReadOnly: true,
		}, nil
	}

	tests := []struct {
		name string

		block sqlfile.QueryBlock

		filename string

		parseFunction   func(context.Context, string) ([]ast.ParsedStatement, error)
		analyseFunction func(context.Context, *sqlmodel.Catalogue, ast.ParsedStatement) (*ast.RawQueryAnalysis, error)

		wantNilQuery bool
		wantName     string
		wantCommand  sqlmodel.QueryCommand

		wantDiagCodes []string

		wantOutputCount    int
		wantParameterCount int
	}{
		{
			name: "simple SELECT with directives produces a valid AnalysedQuery",
			block: sqlfile.QueryBlock{
				SQL:       "-- sgl.query(name: GetUser, command: one)\n-- $1 as sgl.param(user_id)\nSELECT id, name FROM users WHERE id = $1",
				StartLine: 1,
			},
			filename:           "queries.sql",
			parseFunction:      standardParseFunction,
			analyseFunction:    standardAnalyseFunction,
			wantNilQuery:       false,
			wantName:           "GetUser",
			wantCommand:        sqlmodel.QueryCommandOne,
			wantOutputCount:    2,
			wantParameterCount: 1,
		},
		{
			name: "parse error returns diagnostic and nil query",
			block: sqlfile.QueryBlock{
				SQL:       "-- sgl.query(name: BadQuery, command: one)\nSELECT * FROM ???",
				StartLine: 5,
			},
			filename: "queries.sql",
			parseFunction: func(_ context.Context, _ string) ([]ast.ParsedStatement, error) {
				return nil, errors.New("syntax error at position 42")
			},
			analyseFunction: standardAnalyseFunction,
			wantNilQuery:    true,
			wantDiagCodes:   []string{"Q010"},
		},
		{
			name: "missing directives returns nil query with diagnostics",
			block: sqlfile.QueryBlock{
				SQL:       "SELECT 1",
				StartLine: 1,
			},
			filename:        "queries.sql",
			parseFunction:   standardParseFunction,
			analyseFunction: standardAnalyseFunction,
			wantNilQuery:    true,
			wantDiagCodes:   nil,
		},
		{
			name: "engine analysis error returns diagnostic and nil query",
			block: sqlfile.QueryBlock{
				SQL:       "-- sgl.query(name: FailAnalysis, command: many)\nSELECT id FROM users",
				StartLine: 10,
			},
			filename:      "queries.sql",
			parseFunction: standardParseFunction,
			analyseFunction: func(_ context.Context, _ *sqlmodel.Catalogue, _ ast.ParsedStatement) (*ast.RawQueryAnalysis, error) {
				return nil, errors.New("analysis failed: unsupported construct")
			},
			wantNilQuery:  true,
			wantDiagCodes: []string{"Q010"},
		},
		{
			name: "empty statement list returns diagnostic and nil query",
			block: sqlfile.QueryBlock{
				SQL:       "-- sgl.query(name: EmptyQuery, command: exec)\n",
				StartLine: 1,
			},
			filename: "queries.sql",
			parseFunction: func(_ context.Context, _ string) ([]ast.ParsedStatement, error) {
				return []ast.ParsedStatement{}, nil
			},
			analyseFunction: standardAnalyseFunction,
			wantNilQuery:    true,
			wantDiagCodes:   []string{"Q010"},
		},
		{
			name: "multiple statements produces hint diagnostic and analyses last",
			block: sqlfile.QueryBlock{
				SQL:       "-- sgl.query(name: MultiStmt, command: one)\nCREATE TEMP TABLE t (x int);\nSELECT id, name FROM users WHERE id = $1",
				StartLine: 1,
			},
			filename: "queries.sql",
			parseFunction: func(_ context.Context, sql string) ([]ast.ParsedStatement, error) {

				return []ast.ParsedStatement{
					{Location: 0, Length: 30},
					{Location: 31, Length: len(sql) - 31},
				}, nil
			},
			analyseFunction:    standardAnalyseFunction,
			wantNilQuery:       false,
			wantName:           "MultiStmt",
			wantCommand:        sqlmodel.QueryCommandOne,
			wantDiagCodes:      []string{"Q012"},
			wantOutputCount:    2,
			wantParameterCount: 1,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			parseFunction := testCase.parseFunction
			if parseFunction == nil {
				parseFunction = standardParseFunction
			}
			analyseFunction := testCase.analyseFunction
			if analyseFunction == nil {
				analyseFunction = standardAnalyseFunction
			}

			engine := buildEngine(parseFunction, analyseFunction)
			catalogue := buildCatalogue()
			analyser := NewQueryAnalyser(engine, catalogue, directive.PrefixPolicy{Prefixes: namespace.Default(), RequireCanonical: false})

			ctx := t.Context()
			query, diagnostics := analyser.AnalyseQuery(ctx, testCase.block, testCase.filename)

			if testCase.wantNilQuery {
				assert.Nil(t, query, "expected nil AnalysedQuery")
			} else {
				require.NotNil(t, query, "expected non-nil AnalysedQuery")
				assert.Equal(t, testCase.wantName, query.Name, "query name mismatch")
				assert.Equal(t, testCase.wantCommand, query.Command, "query command mismatch")
				assert.Equal(t, testCase.filename, query.Filename, "filename must be propagated")
				assert.Equal(t, testCase.block.SQL, query.SQL, "SQL text must be propagated")
				assert.Equal(t, testCase.block.StartLine, query.Line, "start line must be propagated")

				if testCase.wantOutputCount > 0 {
					assert.Len(t, query.OutputColumns, testCase.wantOutputCount, "output column count mismatch")
				}
				if testCase.wantParameterCount > 0 {
					assert.Len(t, query.Parameters, testCase.wantParameterCount, "parameter count mismatch")
				}
			}

			if len(testCase.wantDiagCodes) > 0 {
				require.NotEmpty(t, diagnostics, "expected diagnostics but got none")
				actualCodes := make([]string, len(diagnostics))
				for index, diagnostic := range diagnostics {
					actualCodes[index] = diagnostic.Code
				}
				for _, wantCode := range testCase.wantDiagCodes {
					assert.Contains(t, actualCodes, wantCode,
						"expected diagnostic code %q in %v", wantCode, actualCodes)
				}
			}
		})
	}
}

func TestQueryAnalyser_AnalyseQuery_OutputColumnDetails(t *testing.T) {
	t.Parallel()

	catalogue := fakedialect.NewCatalogue("public")
	catalogue.Schemas["public"].Tables["users"] = fakedialect.NewTable("users",
		sqlmodel.Column{
			Name:    "id",
			SQLType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger},
		},
		sqlmodel.Column{
			Name:     "email",
			SQLType:  sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText},
			Nullable: true,
		},
	)

	blockSQL := "-- sgl.query(name: GetUserEmail, command: one)\n-- $1 as sgl.param(user_id)\nSELECT id, email FROM users WHERE id = $1"
	engine := &fakedialect.Engine{
		ParseStatementsFunc: func(_ context.Context, sql string) ([]ast.ParsedStatement, error) {
			return []ast.ParsedStatement{{Location: 0, Length: len(sql)}}, nil
		},
		AnalyseQueryFunc: func(_ context.Context, _ *sqlmodel.Catalogue, _ ast.ParsedStatement) (*ast.RawQueryAnalysis, error) {
			return &ast.RawQueryAnalysis{
				FromTables: []ast.TableReference{
					{Name: "users", Schema: "public"},
				},
				OutputColumns: []ast.RawOutputColumn{
					{
						Name:       "id",
						TableAlias: "users",
						ColumnName: "id",
						Expression: &ast.ColumnRefExpression{TableAlias: "users", ColumnName: "id"},
					},
					{
						Name:       "email",
						TableAlias: "users",
						ColumnName: "email",
						Expression: &ast.ColumnRefExpression{TableAlias: "users", ColumnName: "email"},
					},
				},
				ParameterReferences: []ast.RawParameterReference{
					{
						Number:          1,
						ColumnReference: &ast.ColumnReference{TableAlias: "users", ColumnName: "id"},
						Context:         sqlmodel.ParameterContextComparison,
					},
				},
				Spans: ast.StatementSpans{Placeholders: []ast.PlaceholderSpan{{
					Span:   ast.Span{Start: strings.LastIndex(blockSQL, "$1"), End: strings.LastIndex(blockSQL, "$1") + 2},
					Number: 1,
				}}},
				ReadOnly: true,
			}, nil
		},
	}

	analyser := NewQueryAnalyser(engine, catalogue, directive.PrefixPolicy{Prefixes: namespace.Default(), RequireCanonical: false})
	ctx := t.Context()

	block := sqlfile.QueryBlock{
		SQL:       blockSQL,
		StartLine: 1,
	}

	query, diagnostics := analyser.AnalyseQuery(ctx, block, "test.sql")

	require.NotNil(t, query, "expected non-nil AnalysedQuery")
	assert.Equal(t, "GetUserEmail", query.Name)
	assert.Equal(t, sqlmodel.QueryCommandOne, query.Command)
	assert.True(t, query.ReadOnly, "a SELECT query should be read-only")

	require.Len(t, query.OutputColumns, 2, "expected two output columns")
	assert.Equal(t, "id", query.OutputColumns[0].Name)
	assert.Equal(t, sqlmodel.TypeCategoryInteger, query.OutputColumns[0].SQLType.Category)
	assert.Equal(t, "email", query.OutputColumns[1].Name)
	assert.Equal(t, sqlmodel.TypeCategoryText, query.OutputColumns[1].SQLType.Category)

	assert.True(t, query.OutputColumns[1].Nullable, "email column should be nullable")

	require.Len(t, query.Parameters, 1, "expected one parameter")
	assert.Equal(t, "user_id", query.Parameters[0].Name)
	assert.Equal(t, 1, query.Parameters[0].Number)
	assert.Equal(t, sqlmodel.TypeCategoryInteger, query.Parameters[0].SQLType.Category,
		"parameter type should be inferred from the compared column")

	for _, diagnostic := range diagnostics {
		assert.NotEqual(t, diag.SeverityError, diagnostic.Severity,
			"unexpected error-severity diagnostic: %s (%s)", diagnostic.Message, diagnostic.Code)
	}
}

func TestQueryAnalyser_AnalyseQuery_ReadOnlyOverride(t *testing.T) {
	t.Parallel()

	catalogue := fakedialect.NewCatalogue("public")
	catalogue.Schemas["public"].Tables["users"] = fakedialect.NewTable("users",
		sqlmodel.Column{
			Name:    "id",
			SQLType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger},
		},
	)

	engine := &fakedialect.Engine{
		ParseStatementsFunc: func(_ context.Context, sql string) ([]ast.ParsedStatement, error) {
			return []ast.ParsedStatement{{Location: 0, Length: len(sql)}}, nil
		},
		AnalyseQueryFunc: func(_ context.Context, _ *sqlmodel.Catalogue, _ ast.ParsedStatement) (*ast.RawQueryAnalysis, error) {
			return &ast.RawQueryAnalysis{
				FromTables: []ast.TableReference{
					{Name: "users", Schema: "public"},
				},
				OutputColumns: []ast.RawOutputColumn{
					{
						Name:       "id",
						TableAlias: "users",
						ColumnName: "id",
						Expression: &ast.ColumnRefExpression{TableAlias: "users", ColumnName: "id"},
					},
				},
				ReadOnly: false,
			}, nil
		},
	}

	analyser := NewQueryAnalyser(engine, catalogue, directive.PrefixPolicy{Prefixes: namespace.Default(), RequireCanonical: false})
	ctx := t.Context()

	block := sqlfile.QueryBlock{
		SQL:       "-- sgl.query(name: GetUsers, command: many, readonly: true)\nSELECT id FROM users",
		StartLine: 1,
	}

	query, _ := analyser.AnalyseQuery(ctx, block, "test.sql")

	require.NotNil(t, query, "expected non-nil AnalysedQuery")
	assert.True(t, query.ReadOnly, "sgl.readonly: true should override engine detection")
}

func TestQueryAnalyser_AnalyseQuery_NameOnlyBlock(t *testing.T) {
	t.Parallel()

	catalogue := fakedialect.NewCatalogue("public")
	catalogue.Schemas["public"].Tables["users"] = fakedialect.NewTable("users",
		sqlmodel.Column{
			Name:    "id",
			SQLType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger},
		},
	)

	engine := &fakedialect.Engine{
		ParseStatementsFunc: func(_ context.Context, sql string) ([]ast.ParsedStatement, error) {
			return []ast.ParsedStatement{{Location: 0, Length: len(sql)}}, nil
		},
		AnalyseQueryFunc: func(_ context.Context, _ *sqlmodel.Catalogue, _ ast.ParsedStatement) (*ast.RawQueryAnalysis, error) {
			return &ast.RawQueryAnalysis{
				FromTables: []ast.TableReference{
					{Name: "users", Schema: "public"},
				},
				OutputColumns: []ast.RawOutputColumn{
					{
						Name:       "id",
						TableAlias: "users",
						ColumnName: "id",
						Expression: &ast.ColumnRefExpression{TableAlias: "users", ColumnName: "id"},
					},
				},
				ReadOnly: true,
			}, nil
		},
	}

	analyser := NewQueryAnalyser(engine, catalogue, directive.PrefixPolicy{Prefixes: namespace.Default(), RequireCanonical: false})
	ctx := t.Context()

	block := sqlfile.QueryBlock{
		SQL:       "-- sgl.query(name: ListUsers)\nSELECT id FROM users",
		StartLine: 1,
	}

	query, _ := analyser.AnalyseQuery(ctx, block, "test.sql")

	require.NotNil(t, query, "a block with sgl.name should still produce a query")
	assert.Equal(t, "ListUsers", query.Name)
}

func TestQueryAnalyser_AnalyseQuery_UnknownTableWarning(t *testing.T) {
	t.Parallel()

	catalogue := fakedialect.NewCatalogue("public")

	engine := &fakedialect.Engine{
		ParseStatementsFunc: func(_ context.Context, sql string) ([]ast.ParsedStatement, error) {
			return []ast.ParsedStatement{{Location: 0, Length: len(sql)}}, nil
		},
		AnalyseQueryFunc: func(_ context.Context, _ *sqlmodel.Catalogue, _ ast.ParsedStatement) (*ast.RawQueryAnalysis, error) {
			return &ast.RawQueryAnalysis{
				FromTables: []ast.TableReference{
					{Name: "users", Schema: "public"},
				},
				OutputColumns: []ast.RawOutputColumn{
					{
						Name:       "id",
						TableAlias: "users",
						ColumnName: "id",
						Expression: &ast.ColumnRefExpression{TableAlias: "users", ColumnName: "id"},
					},
				},
				ReadOnly: true,
			}, nil
		},
	}

	analyser := NewQueryAnalyser(engine, catalogue, directive.PrefixPolicy{Prefixes: namespace.Default(), RequireCanonical: false})
	ctx := t.Context()

	block := sqlfile.QueryBlock{
		SQL:       "-- sgl.query(name: MissingTable, command: many)\nSELECT id FROM users",
		StartLine: 1,
	}

	query, diagnostics := analyser.AnalyseQuery(ctx, block, "test.sql")

	require.NotNil(t, query, "query should still be produced despite unresolved table")
	assert.Equal(t, "MissingTable", query.Name)

	var foundQ003 bool
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == "Q003" {
			foundQ003 = true
			assert.Equal(t, diag.SeverityWarning, diagnostic.Severity,
				"Q003 should be a warning, not an error")
			break
		}
	}
	assert.True(t, foundQ003, "expected Q003 warning for unresolved table reference")
}

func TestQueryAnalyser_AnalyseQuery_ReturningFollowsTheDialectCapability(t *testing.T) {
	t.Parallel()

	const returningSQL = "-- sgl.query(name: CreateUser, command: one)\nINSERT INTO users (name) VALUES ('x')\nRETURNING id"

	testCases := []struct {
		name      string
		want      []diag.SourceError
		returning ast.DataChange
	}{
		{
			name:      "dialect without RETURNING reports the clause",
			returning: 0,
			want: []diag.SourceError{{
				Filename:   "users.sql",
				Message:    `query "CreateUser" has a RETURNING clause, but the mock dialect does not support RETURNING`,
				Code:       diag.CodeReturningNotSupported,
				Suggestion: "",
				Line:       6,
				Column:     1,
				EndLine:    6,
				EndColumn:  10,
				Severity:   diag.SeverityError,
			}},
		},
		{
			name:      "dialect with RETURNING accepts the clause",
			returning: ast.DataChangeInsert,
			want:      nil,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue := fakedialect.NewCatalogue("public")
			catalogue.Schemas["public"].Tables["users"] = fakedialect.NewTable("users",
				sqlmodel.Column{Name: "id", SQLType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}},
				sqlmodel.Column{Name: "name", SQLType: sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}},
			)
			engine := &fakedialect.Engine{
				ParseStatementsFunc: func(_ context.Context, sql string) ([]ast.ParsedStatement, error) {
					return []ast.ParsedStatement{{Location: 0, Length: len(sql)}}, nil
				},
				AnalyseQueryFunc: func(_ context.Context, _ *sqlmodel.Catalogue, _ ast.ParsedStatement) (*ast.RawQueryAnalysis, error) {
					return &ast.RawQueryAnalysis{
						InsertTable:   "users",
						InsertColumns: []string{"name"},
						FromTables:    []ast.TableReference{{Name: "users", Schema: "public"}},
						OutputColumns: []ast.RawOutputColumn{{
							Name:       "id",
							TableAlias: "users",
							ColumnName: "id",
							Expression: &ast.ColumnRefExpression{TableAlias: "users", ColumnName: "id"},
						}},
						HasReturning:      true,
						ReturningLocation: strings.Index(returningSQL, "RETURNING"),
					}, nil
				},
				ReturningFunc: func() ast.DataChange { return testCase.returning },
			}
			analyser := NewQueryAnalyser(engine, catalogue, directive.PrefixPolicy{Prefixes: namespace.Default(), RequireCanonical: false})

			query, diagnostics := analyser.AnalyseQuery(t.Context(), sqlfile.QueryBlock{SQL: returningSQL, StartLine: 4}, "users.sql")

			require.NotNil(t, query)
			var reported []diag.SourceError
			for _, diagnostic := range diagnostics {
				if diagnostic.Code == diag.CodeReturningNotSupported {
					reported = append(reported, diagnostic)
				}
			}
			require.Equal(t, testCase.want, reported)
		})
	}
}

func typedErrorEngine(statementStart int) *fakedialect.Engine {
	return &fakedialect.Engine{
		ParseStatementsFunc: func(_ context.Context, sql string) ([]ast.ParsedStatement, error) {
			return []ast.ParsedStatement{{Location: statementStart, Length: len(sql) - statementStart}}, nil
		},
		AnalyseQueryFunc: func(_ context.Context, _ *sqlmodel.Catalogue, _ ast.ParsedStatement) (*ast.RawQueryAnalysis, error) {
			return &ast.RawQueryAnalysis{
				FromTables: []ast.TableReference{{Name: "users", Schema: "public"}},
				OutputColumns: []ast.RawOutputColumn{
					{Name: "nmae", ColumnName: "nmae", Expression: &ast.ColumnRefExpression{ColumnName: "nmae"}},
					{Name: "shout", Expression: &ast.FunctionCallExpression{FunctionName: "nonexistent_func"}},
				},
				ReadOnly: true,
			}, nil
		},
	}
}

func typedErrorCatalogue() *sqlmodel.Catalogue {
	catalogue := fakedialect.NewCatalogue("public")
	catalogue.Schemas["public"].Tables["users"] = &sqlmodel.Table{
		Name:   "users",
		Schema: "public",
		Columns: []sqlmodel.Column{
			{Name: "name", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "text"}},
		},
	}
	return catalogue
}

func TestQueryAnalyser_AnalyseQuery_ResolverProblemsKeepTheirCodes(t *testing.T) {
	t.Parallel()

	const header = "-- sgl.query(name: Shout, command: many)\n"
	block := sqlfile.QueryBlock{SQL: header + "SELECT nmae,\n  nonexistent_func() AS shout\nFROM users", StartLine: 4}
	analyser := NewQueryAnalyser(typedErrorEngine(len(header)), typedErrorCatalogue(),
		directive.PrefixPolicy{Prefixes: namespace.Default(), RequireCanonical: false})

	query, diagnostics := analyser.AnalyseQuery(t.Context(), block, "shout.sql")
	require.NotNil(t, query)
	require.Len(t, query.OutputColumns, 2, "every projected column keeps a row field")

	byCode := make(map[string]diag.SourceError)
	for _, diagnostic := range diagnostics {
		byCode[diagnostic.Code] = diagnostic
		require.NotContains(t, diagnostic.Message, diagnostic.Code, "the message must not repeat the code")
	}

	unknownColumn, found := byCode[diag.CodeUnknownColumn]
	require.True(t, found, "expected Q001, got %v", diagnostics)
	require.Equal(t, "name", unknownColumn.Suggestion)
	require.Equal(t, 5, unknownColumn.Line, "located on the statement, not the directive line")
	require.Equal(t, 7, unknownColumn.EndLine)

	unknownFunction, found := byCode[diag.CodeUnknownFunction]
	require.True(t, found, "expected Q005 under its own code, got %v", diagnostics)
	require.Equal(t, `unknown function "nonexistent_func"`, unknownFunction.Message)
	require.NotContains(t, byCode, diag.CodeExpressionTypeError)
}

func TestQueryAnalyser_AnalyseQuery_CancellationIsNotADiagnostic(t *testing.T) {
	t.Parallel()

	const header = "-- sgl.query(name: Shout, command: many)\n"
	block := sqlfile.QueryBlock{SQL: header + "SELECT nmae FROM users", StartLine: 1}
	analyser := NewQueryAnalyser(typedErrorEngine(len(header)), typedErrorCatalogue(),
		directive.PrefixPolicy{Prefixes: namespace.Default(), RequireCanonical: false})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	query, diagnostics := analyser.AnalyseQuery(ctx, block, "shout.sql")
	require.Nil(t, query)
	for _, diagnostic := range diagnostics {
		require.NotEqual(t, diag.CodeInternalNilGuard, diagnostic.Code)
	}
}

func TestQueryAnalyser_AnalyseQuery_GroupByKeepsOuterJoinNullability(t *testing.T) {
	t.Parallel()

	catalogue := fakedialect.NewCatalogue("public")
	integer := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: "integer"}
	catalogue.Schemas["public"].Tables["harbours"] = &sqlmodel.Table{
		Name: "harbours", Schema: "public", PrimaryKey: []string{"id"},
		Columns: []sqlmodel.Column{{Name: "id", SQLType: integer}},
	}
	catalogue.Schemas["public"].Tables["boats"] = &sqlmodel.Table{
		Name: "boats", Schema: "public", PrimaryKey: []string{"id"},
		Columns: []sqlmodel.Column{{Name: "id", SQLType: integer}, {Name: "harbour_id", SQLType: integer}},
	}
	engine := &fakedialect.Engine{
		ParseStatementsFunc: func(_ context.Context, sql string) ([]ast.ParsedStatement, error) {
			return []ast.ParsedStatement{{Location: 0, Length: len(sql)}}, nil
		},
		AnalyseQueryFunc: func(_ context.Context, _ *sqlmodel.Catalogue, _ ast.ParsedStatement) (*ast.RawQueryAnalysis, error) {
			return &ast.RawQueryAnalysis{
				FromTables:  []ast.TableReference{{Name: "harbours", Alias: "h"}},
				JoinClauses: []ast.JoinClause{{Table: ast.TableReference{Name: "boats", Alias: "b"}, Kind: ast.JoinLeft}},
				OutputColumns: []ast.RawOutputColumn{
					{Name: "id", TableAlias: "b", ColumnName: "id"},
				},
				GroupByColumns: []ast.ColumnReference{{TableAlias: "b", ColumnName: "id"}},
				ReadOnly:       true,
			}, nil
		},
	}
	analyser := NewQueryAnalyser(engine, catalogue, directive.PrefixPolicy{Prefixes: namespace.Default(), RequireCanonical: false})

	query, _ := analyser.AnalyseQuery(t.Context(), sqlfile.QueryBlock{
		SQL:       "-- sgl.query(name: BoatIDs, command: many)\nSELECT b.id FROM harbours h LEFT JOIN boats b ON b.harbour_id = h.id GROUP BY b.id",
		StartLine: 1,
	}, "boats.sql")

	require.NotNil(t, query)
	require.Len(t, query.OutputColumns, 1)
	require.True(t, query.OutputColumns[0].Nullable, "a harbour without boats groups under a NULL boat id")
}

func TestQueryAnalyserReportsTheDialectsWarningsWhereTheyAre(t *testing.T) {
	t.Parallel()

	catalogue := fakedialect.NewCatalogue("public")
	catalogue.Schemas["public"].Tables["users"] = fakedialect.NewTable("users",
		sqlmodel.Column{Name: "id", SQLType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}},
	)
	blockSQL := "-- sgl.query(name: ListUsers, command: many)\nSELECT id FROM users WHERE id = 1 || id = 2"
	pipes := strings.Index(blockSQL, "||")

	testCases := []struct {
		name       string
		span       ast.Span
		wantLine   int
		wantColumn int
	}{
		{name: "a located warning", span: ast.Span{Start: pipes, End: pipes + 2}, wantLine: 2, wantColumn: pipes - strings.Index(blockSQL, "\n")},
		{name: "a warning on the statement", span: ast.Span{}, wantLine: 1, wantColumn: 1},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := &fakedialect.Engine{
				ParseStatementsFunc: func(_ context.Context, sql string) ([]ast.ParsedStatement, error) {
					return []ast.ParsedStatement{{Location: 0, Length: len(sql)}}, nil
				},
				AnalyseQueryFunc: func(_ context.Context, _ *sqlmodel.Catalogue, _ ast.ParsedStatement) (*ast.RawQueryAnalysis, error) {
					return &ast.RawQueryAnalysis{
						FromTables: []ast.TableReference{{Name: "users", Schema: "public"}},
						OutputColumns: []ast.RawOutputColumn{{Name: "id", TableAlias: "users", ColumnName: "id",
							Expression: &ast.ColumnRefExpression{TableAlias: "users", ColumnName: "id"}}},
						Warnings: []ast.Warning{{Message: "|| is a logical OR", Span: testCase.span}},
						ReadOnly: true,
					}, nil
				},
			}
			analyser := NewQueryAnalyser(engine, catalogue, directive.PrefixPolicy{Prefixes: namespace.Default(), RequireCanonical: false})

			query, diagnostics := analyser.AnalyseQuery(t.Context(), sqlfile.QueryBlock{SQL: blockSQL, StartLine: 1}, "test.sql")

			require.NotNil(t, query)
			var warnings []diag.SourceError
			for _, diagnostic := range diagnostics {
				if diagnostic.Code == diag.CodeEngineReading {
					warnings = append(warnings, diagnostic)
				}
			}
			require.Len(t, warnings, 1)
			require.Equal(t, diag.SeverityWarning, warnings[0].Severity)
			require.Equal(t, "|| is a logical OR", warnings[0].Message)
			require.Equal(t, testCase.wantLine, warnings[0].Line)
			require.Equal(t, testCase.wantColumn, warnings[0].Column)
		})
	}
}

func nestedExistsAnalysis(levels int) *ast.RawQueryAnalysis {
	query := &ast.RawQueryAnalysis{
		FromTables:    []ast.TableReference{{Name: "users", Schema: "public"}},
		OutputColumns: []ast.RawOutputColumn{{Name: "id", ColumnName: "id", Expression: &ast.ColumnRefExpression{ColumnName: "id"}}},
		ClauseExpressions: []ast.ClauseExpression{{Clause: ast.ClauseWhere, Expression: &ast.ComparisonExpression{
			Left: &ast.ColumnRefExpression{ColumnName: "id"}, Operator: "=", Right: &ast.ParameterExpression{Number: 1},
		}}},
		ParameterReferences: []ast.RawParameterReference{{Number: 1, ColumnReference: &ast.ColumnReference{ColumnName: "id"},
			Context: sqlmodel.ParameterContextComparison}},
		ReadOnly: true,
	}
	for range levels {
		query = &ast.RawQueryAnalysis{
			FromTables:          []ast.TableReference{{Name: "users", Schema: "public"}},
			OutputColumns:       []ast.RawOutputColumn{{Name: "id", ColumnName: "id", Expression: &ast.ColumnRefExpression{ColumnName: "id"}}},
			ClauseExpressions:   []ast.ClauseExpression{{Clause: ast.ClauseWhere, Expression: &ast.ExistsExpression{InnerQuery: query}}},
			PredicateSubqueries: []*ast.RawQueryAnalysis{query},
			ParameterReferences: query.ParameterReferences,
			ReadOnly:            true,
		}
	}
	return query
}

func TestAnalyseQueryAnalysesEachNestedQueryOnce(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		levels int
	}{
		{name: "one EXISTS", levels: 1},
		{name: "forty nested EXISTS", levels: 40},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue := fakedialect.NewCatalogue("public")
			catalogue.Schemas["public"].Tables["users"] = fakedialect.NewTable("users",
				sqlmodel.Column{Name: "id", SQLType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}},
			)
			engine := &fakedialect.Engine{
				ParseStatementsFunc: func(_ context.Context, sql string) ([]ast.ParsedStatement, error) {
					return []ast.ParsedStatement{{Location: 0, Length: len(sql)}}, nil
				},
				AnalyseQueryFunc: func(_ context.Context, _ *sqlmodel.Catalogue, _ ast.ParsedStatement) (*ast.RawQueryAnalysis, error) {
					return nestedExistsAnalysis(testCase.levels), nil
				},
			}
			analyser := NewQueryAnalyser(engine, catalogue, directive.PrefixPolicy{Prefixes: namespace.Default(), RequireCanonical: false})
			block := sqlfile.QueryBlock{SQL: "-- sgl.query(name: Nested, command: many)\nSELECT id FROM users WHERE EXISTS (...)", StartLine: 1}

			var query *sqlmodel.AnalysedQuery
			var diagnostics []diag.SourceError
			scaling.Within(t, time.Second, func(ctx context.Context) {
				query, diagnostics = analyser.AnalyseQuery(ctx, block, "nested.sql")
			})

			require.NotNil(t, query)
			require.Empty(t, diagnostics)
			require.Len(t, query.Parameters, 1)
			require.Equal(t, "int4", query.Parameters[0].SQLType.EngineName)
		})
	}
}

func capturingLogs(t *testing.T) (context.Context, *bytes.Buffer) {
	t.Helper()

	logs := &bytes.Buffer{}
	return logging.ContextWithLogger(t.Context(), slog.New(slog.NewJSONHandler(logs, nil))), logs
}

func loggedRecords(t *testing.T, logs *bytes.Buffer) []map[string]any {
	t.Helper()

	var records []map[string]any
	decoder := json.NewDecoder(logs)
	for decoder.More() {
		record := map[string]any{}
		require.NoError(t, decoder.Decode(&record))
		records = append(records, record)
	}
	return records
}

func TestAnalyseQueryRefusesAnAnalysisHoldingATypedNilNode(t *testing.T) {
	t.Parallel()

	usersFrom := []ast.TableReference{{Name: "users", Schema: "public"}}
	idColumn := []ast.RawOutputColumn{{Name: "id", ColumnName: "id", Expression: &ast.ColumnRefExpression{ColumnName: "id"}}}
	testCases := []struct {
		analysis *ast.RawQueryAnalysis
		name     string
		wantNode string
	}{
		{name: "a nil binary operator in WHERE", wantNode: "*ast.BinaryOpExpression", analysis: &ast.RawQueryAnalysis{
			FromTables: usersFrom, OutputColumns: idColumn,
			ClauseExpressions: []ast.ClauseExpression{{Expression: (*ast.BinaryOpExpression)(nil), Clause: ast.ClauseWhere}},
		}},
		{name: "a nil call in the select list", wantNode: "*ast.FunctionCallExpression", analysis: &ast.RawQueryAnalysis{
			FromTables: usersFrom, OutputColumns: []ast.RawOutputColumn{{Name: "v", Expression: (*ast.FunctionCallExpression)(nil)}},
		}},
		{name: "a nil scalar subquery in a call", wantNode: "*ast.ScalarSubqueryExpression", analysis: &ast.RawQueryAnalysis{
			FromTables: usersFrom, OutputColumns: []ast.RawOutputColumn{{Name: "v", Expression: &ast.FunctionCallExpression{
				FunctionName: "coalesce_it", Arguments: []ast.Expression{(*ast.ScalarSubqueryExpression)(nil)}}}},
		}},
		{name: "a nil EXISTS in WHERE", wantNode: "*ast.ExistsExpression", analysis: &ast.RawQueryAnalysis{
			FromTables: usersFrom, OutputColumns: idColumn,
			ClauseExpressions: []ast.ClauseExpression{{Expression: (*ast.ExistsExpression)(nil), Clause: ast.ClauseWhere}},
		}},
		{name: "a nil window in the select list", wantNode: "*ast.WindowFunctionExpression", analysis: &ast.RawQueryAnalysis{
			FromTables: usersFrom, OutputColumns: []ast.RawOutputColumn{{Name: "v", Expression: (*ast.WindowFunctionExpression)(nil)}},
		}},
		{name: "a nil lambda in a call", wantNode: "*ast.LambdaExpression", analysis: &ast.RawQueryAnalysis{
			FromTables: usersFrom, OutputColumns: []ast.RawOutputColumn{{Name: "v", Expression: &ast.FunctionCallExpression{
				FunctionName: "list_filter", Arguments: []ast.Expression{(*ast.LambdaExpression)(nil)}}}},
		}},
		{name: "a nil named argument in a call", wantNode: "*ast.NamedArgumentExpression", analysis: &ast.RawQueryAnalysis{
			FromTables: usersFrom, OutputColumns: []ast.RawOutputColumn{{Name: "v", Expression: &ast.FunctionCallExpression{
				FunctionName: "make_it", Arguments: []ast.Expression{(*ast.NamedArgumentExpression)(nil)}}}},
		}},
		{name: "a nil column reference in WHERE", wantNode: "*ast.ColumnRefExpression", analysis: &ast.RawQueryAnalysis{
			FromTables: usersFrom, OutputColumns: idColumn,
			ClauseExpressions: []ast.ClauseExpression{{Clause: ast.ClauseWhere, Expression: &ast.ComparisonExpression{
				Left: (*ast.ColumnRefExpression)(nil), Operator: "=", Right: &ast.LiteralExpression{TypeName: "integer"}}}},
		}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue := fakedialect.NewCatalogue("public")
			catalogue.Schemas["public"].Tables["users"] = fakedialect.NewTable("users",
				sqlmodel.Column{Name: "id", SQLType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}},
			)
			engine := &fakedialect.Engine{
				ParseStatementsFunc: func(_ context.Context, sql string) ([]ast.ParsedStatement, error) {
					return []ast.ParsedStatement{{Location: 0, Length: len(sql)}}, nil
				},
				AnalyseQueryFunc: func(_ context.Context, _ *sqlmodel.Catalogue, _ ast.ParsedStatement) (*ast.RawQueryAnalysis, error) {
					return testCase.analysis, nil
				},
			}
			analyser := NewQueryAnalyser(engine, catalogue, directive.PrefixPolicy{Prefixes: namespace.Default(), RequireCanonical: false})
			block := sqlfile.QueryBlock{SQL: "-- sgl.query(name: Broken, command: many)\nSELECT id FROM users", StartLine: 1}

			var query *sqlmodel.AnalysedQuery
			var diagnostics []diag.SourceError
			require.NotPanics(t, func() { query, diagnostics = analyser.AnalyseQuery(t.Context(), block, "broken.sql") })

			require.Nil(t, query)
			require.Len(t, diagnostics, 1)
			require.Equal(t, diag.CodeInternalNilGuard, diagnostics[0].Code)
			require.Equal(t, diag.SeverityError, diagnostics[0].Severity)
			require.Contains(t, diagnostics[0].Message, "nil "+testCase.wantNode)
		})
	}
}

func TestAnalyseQueryReportsAPanicAsQ070AndLogsItOnce(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		resolve func(ctx context.Context, name string, argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error)
		name    string
		value   string
	}{
		{
			name:  "a dialect resolver that panics while the select list is typed",
			value: "resolver bug",
			resolve: func(context.Context, string, []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
				panic("resolver bug")
			},
		},
		{
			name:  "a dialect resolver that panics with an error",
			value: "resolver failed",
			resolve: func(context.Context, string, []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
				panic(errors.New("resolver failed"))
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue := fakedialect.NewCatalogue("public")
			catalogue.Schemas["public"].Tables["users"] = fakedialect.NewTable("users",
				sqlmodel.Column{Name: "id", SQLType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}},
			)
			engine := &fakedialect.FunctionResolverEngine{Engine: &fakedialect.Engine{
				ParseStatementsFunc: func(_ context.Context, sql string) ([]ast.ParsedStatement, error) {
					return []ast.ParsedStatement{{Location: 0, Length: len(sql)}}, nil
				},
				AnalyseQueryFunc: func(_ context.Context, _ *sqlmodel.Catalogue, _ ast.ParsedStatement) (*ast.RawQueryAnalysis, error) {
					return &ast.RawQueryAnalysis{
						FromTables:    []ast.TableReference{{Name: "users", Schema: "public"}},
						OutputColumns: []ast.RawOutputColumn{{Name: "v", Expression: &ast.FunctionCallExpression{FunctionName: "engine_only"}}},
						ReadOnly:      true,
					}, nil
				},
			}, ResolveFunc: testCase.resolve}
			analyser := NewQueryAnalyser(engine, catalogue, directive.PrefixPolicy{Prefixes: namespace.Default(), RequireCanonical: false})
			block := sqlfile.QueryBlock{SQL: "-- sgl.query(name: Panics, command: many)\nSELECT engine_only() AS v FROM users", StartLine: 3}
			ctx, logs := capturingLogs(t)

			var query *sqlmodel.AnalysedQuery
			var diagnostics []diag.SourceError
			require.NotPanics(t, func() { query, diagnostics = analyser.AnalyseQuery(ctx, block, "panics.sql") })

			require.Nil(t, query)
			require.Len(t, diagnostics, 1)
			require.Equal(t, diag.CodeRecoveredPanic, diagnostics[0].Code)
			require.Equal(t, diag.SeverityError, diagnostics[0].Severity)
			require.Equal(t, 3, diagnostics[0].Line)
			require.Contains(t, diagnostics[0].Message, "panic: "+testCase.value)
			require.NotContains(t, diagnostics[0].Message, "goroutine", "the stack is logged, not reported")
			records := loggedRecords(t, logs)
			require.Len(t, records, 1)
			require.Equal(t, "WARN", records[0]["level"])
			require.Equal(t, "analysing the query block at panics.sql:3", records[0]["operation"])
			require.Contains(t, records[0]["stack"], "seagull.sh/seagull/internal/resolve.")
		})
	}
}

func unknownColumnsEngine(columns int) (*fakedialect.Engine, string) {
	const header = "-- sgl.query(name: Unknown, command: many)\n"
	var sql strings.Builder
	sql.WriteString(header + "SELECT\n")
	outputs := make([]ast.RawOutputColumn, 0, columns)
	items := make([]ast.ItemSpan, 0, columns)
	for index := range columns {
		name := fmt.Sprintf("missing_%d", index)
		start := sql.Len() - len(header)
		sql.WriteString(name + ",\n")
		outputs = append(outputs, ast.RawOutputColumn{Name: name, ColumnName: name})
		span := ast.Span{Start: start, End: start + len(name)}
		items = append(items, ast.ItemSpan{Span: span, Expression: span})
	}
	sql.WriteString("1 FROM users")
	engine := &fakedialect.Engine{
		ParseStatementsFunc: func(_ context.Context, text string) ([]ast.ParsedStatement, error) {
			return []ast.ParsedStatement{{Location: len(header), Length: len(text) - len(header)}}, nil
		},
		AnalyseQueryFunc: func(_ context.Context, _ *sqlmodel.Catalogue, _ ast.ParsedStatement) (*ast.RawQueryAnalysis, error) {
			analysis := &ast.RawQueryAnalysis{
				FromTables:    []ast.TableReference{{Name: "users", Schema: "public"}},
				OutputColumns: outputs,
				ReadOnly:      true,
			}
			analysis.Spans.ProjectionItems = items
			return analysis, nil
		},
	}
	return engine, sql.String()
}

func unknownColumnsCatalogue() *sqlmodel.Catalogue {
	catalogue := fakedialect.NewCatalogue("public")
	catalogue.Schemas["public"].Tables["users"] = fakedialect.NewTable("users",
		sqlmodel.Column{Name: "id", SQLType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}},
	)
	return catalogue
}

func TestAnalyseQueryReportsAtMostTheQueryCapOfDiagnostics(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		columns     int
		wantSummary bool
	}{
		{name: "problems within the cap", columns: 5, wantSummary: false},
		{name: "problems past the cap", columns: check.MaxDiagnosticsPerQuery + 50, wantSummary: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine, sql := unknownColumnsEngine(testCase.columns)
			analyser := NewQueryAnalyser(engine, unknownColumnsCatalogue(), directive.PrefixPolicy{Prefixes: namespace.Default(), RequireCanonical: false})

			query, diagnostics := analyser.AnalyseQuery(t.Context(), sqlfile.QueryBlock{SQL: sql, StartLine: 10}, "unknown.sql")

			require.NotNil(t, query)
			require.Equal(t, diag.CodeUnknownColumn, diagnostics[0].Code)
			require.Equal(t, 12, diagnostics[0].Line, "each column is reported on its own line")
			require.Equal(t, 13, diagnostics[1].Line)
			if !testCase.wantSummary {
				require.Len(t, diagnostics, testCase.columns)
				return
			}
			require.Len(t, diagnostics, check.MaxDiagnosticsPerQuery+1)
			summary := diagnostics[len(diagnostics)-1]
			require.Equal(t, diag.CodeDiagnosticLimit, summary.Code)
			require.Equal(t, diag.SeverityWarning, summary.Severity)
			require.Equal(t, "and 50 more diagnostics were left out", summary.Message)
			require.Equal(t, 10, summary.Line)
		})
	}
}

func TestStatementLocationPlacesManyProblemsInLinearTime(t *testing.T) {
	t.Parallel()

	placeProblems := func(count int) func() {
		line := "SELECT missing_column" + strings.Repeat(" ", 1_000) + "\n"
		sql := strings.Repeat(line, count)
		problems := make([]*resolve.Error, 0, count)
		for index := range count {
			start := index*len(line) + len("SELECT ")
			problems = append(problems, &resolve.Error{Code: diag.CodeUnknownColumn, Message: "unknown column",
				Span: resolve.Span{Start: start, End: start + len("missing_column")}, Severity: diag.SeverityWarning})
		}
		return func() {
			location := statementLocation{filename: "unknown.sql", sql: sql, lines: check.NewSourceLines(sql, 1),
				statement: diagbuild.TextSpan{Line: 1, Column: 1}, startLine: 1}
			_ = location.diagnostics(problems)
		}
	}

	scaling.Linear(t, placeProblems(250), placeProblems(1_000))
}

func TestAnalyseQueryTakesAQueryDeeperThanItReadsToChangeData(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		terms        int
		wantReadOnly bool
		wantLimit    bool
	}{
		{name: "a short chain", terms: 10, wantReadOnly: true, wantLimit: false},
		{name: "a chain past the depth limit", terms: 300, wantReadOnly: false, wantLimit: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue := unknownColumnsCatalogue()
			reader := sqlmodel.NewFunction(nil, sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "int4"),
				sqlmodel.FunctionNullableReturnsNullOnNull, sqlmodel.WithFunctionName("measure"))
			reader.DataAccess = sqlmodel.DataAccessReadOnly
			catalogue.Schemas["public"].Functions["measure"] = []*sqlmodel.Function{reader}
			var condition ast.Expression = &ast.FunctionCallExpression{FunctionName: "measure"}
			for range testCase.terms {
				condition = &ast.BinaryOpExpression{Left: condition, Operator: "+", Right: &ast.LiteralExpression{TypeName: "integer"}}
			}
			engine := &fakedialect.Engine{
				ParseStatementsFunc: func(_ context.Context, sql string) ([]ast.ParsedStatement, error) {
					return []ast.ParsedStatement{{Location: 0, Length: len(sql)}}, nil
				},
				AnalyseQueryFunc: func(_ context.Context, _ *sqlmodel.Catalogue, _ ast.ParsedStatement) (*ast.RawQueryAnalysis, error) {
					return &ast.RawQueryAnalysis{
						FromTables:    []ast.TableReference{{Name: "users", Schema: "public"}},
						OutputColumns: []ast.RawOutputColumn{{Name: "id", ColumnName: "id", Expression: &ast.ColumnRefExpression{ColumnName: "id"}}},
						ClauseExpressions: []ast.ClauseExpression{{Clause: ast.ClauseWhere, Expression: &ast.ComparisonExpression{
							Left: condition, Operator: ">", Right: &ast.LiteralExpression{TypeName: "integer"}}}},
						ReadOnly: true,
					}, nil
				},
			}
			analyser := NewQueryAnalyser(engine, catalogue, directive.PrefixPolicy{Prefixes: namespace.Default(), RequireCanonical: false})
			block := sqlfile.QueryBlock{SQL: "-- sgl.query(name: Deep, command: many)\nSELECT id FROM users WHERE measure() + 0 > 0", StartLine: 1}

			query, diagnostics := analyser.AnalyseQuery(t.Context(), block, "deep.sql")

			require.NotNil(t, query)
			require.Equal(t, testCase.wantReadOnly, query.ReadOnly)
			limits := 0
			for _, diagnostic := range diagnostics {
				if diagnostic.Code == diag.CodeDepthLimit {
					limits++
					require.Equal(t, diag.SeverityWarning, diagnostic.Severity)
				}
			}
			require.Equal(t, testCase.wantLimit, limits > 0)
		})
	}
}

func TestAnalyseQueryTypesNestedLambdaFoldsInLinearTime(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		engine dialect.DialectPort
		name   string
		fold   func(inner string) string
		start  string
	}{
		{name: "ClickHouse arrayFold, whose accumulator stands for its last argument", engine: clickhouse.New(), start: "toUInt64(0)",
			fold: func(inner string) string { return "arrayFold((acc, x) -> acc + x, [1, 2], " + inner + ")" }},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			expression := testCase.start
			for range 20 {
				expression = testCase.fold(expression)
			}
			block := sqlfile.QueryBlock{SQL: "-- sgl.query(name: Folded, command: one)\nSELECT " + expression + " AS folded", StartLine: 1}
			analyser := NewQueryAnalyser(testCase.engine, fakedialect.NewCatalogue("main"),
				directive.PrefixPolicy{Prefixes: namespace.Default(), RequireCanonical: false})

			var query *sqlmodel.AnalysedQuery
			scaling.Within(t, 100*time.Millisecond, func(ctx context.Context) {
				query, _ = analyser.AnalyseQuery(ctx, block, "folded.sql")
			})

			require.NotNil(t, query)
			require.Len(t, query.OutputColumns, 1)
		})
	}
}

func TestQueryAnalyserReadsTheDialectInfoOnlyWhenItIsMade(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		queries int
	}{
		{name: "one query", queries: 1},
		{name: "many queries", queries: 20},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue := fakedialect.NewCatalogue("public")
			catalogue.Schemas["public"].Tables["users"] = fakedialect.NewTable("users",
				sqlmodel.Column{Name: "id", SQLType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}})
			reads := 0
			engine := infoCountingEngine{reads: &reads, Engine: &fakedialect.Engine{
				ParseStatementsFunc: func(_ context.Context, sql string) ([]ast.ParsedStatement, error) {
					return []ast.ParsedStatement{{Location: 0, Length: len(sql)}}, nil
				},
				AnalyseQueryFunc: func(context.Context, *sqlmodel.Catalogue, ast.ParsedStatement) (*ast.RawQueryAnalysis, error) {
					return &ast.RawQueryAnalysis{
						FromTables:    []ast.TableReference{{Name: "users"}},
						OutputColumns: []ast.RawOutputColumn{{Name: "id", ColumnName: "id", Expression: &ast.ColumnRefExpression{ColumnName: "id"}}},
						ReadOnly:      true,
					}, nil
				},
				CommentStyleFunc: func() ast.CommentStyle { return ast.CommentStyle{LinePrefix: "--"} },
			}}
			analyser := NewQueryAnalyser(engine, catalogue, directive.PrefixPolicy{Prefixes: namespace.Default(), RequireCanonical: false})
			readsWhenMade := reads

			for range testCase.queries {
				query, _ := analyser.AnalyseQuery(t.Context(), sqlfile.QueryBlock{SQL: "-- sgl.query(name: Users, command: many)\nSELECT id FROM users", StartLine: 1}, "users.sql")
				require.NotNil(t, query)
			}

			require.Equal(t, readsWhenMade, reads, "the analyser reads the dialect's Info once, when it is made")
		})
	}
}

func TestStatementLocationPlacesEachKindOfProblemAlike(t *testing.T) {
	t.Parallel()

	const sql = "SELECT id\nFROM users\nWHERE nmae = 1"
	at := strings.Index(sql, "nmae")
	statement := diagbuild.TextSpan{Line: 3, Column: 1, EndLine: 5, EndColumn: 15}
	testCases := []struct {
		span       ast.Span
		name       string
		wantLine   int
		wantColumn int
	}{
		{name: "a problem the dialect located", span: ast.Span{Start: at, End: at + 4}, wantLine: 5, wantColumn: 7},
		{name: "a problem the dialect did not locate", span: ast.Span{}, wantLine: statement.Line, wantColumn: statement.Column},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			location := statementLocation{filename: "users.sql", sql: sql, lines: check.NewSourceLines(sql, 3), statement: statement, startLine: 3}

			placed := slices.Concat(
				location.diagnostics([]*resolve.Error{{Code: diag.CodeUnknownColumn, Message: "a", Span: resolve.Span{Start: testCase.span.Start, End: testCase.span.End}}}),
				location.dialectWarnings([]ast.Warning{{Message: "b", Span: testCase.span}}),
				location.templateDiagnostics([]sqltemplate.Problem{{Code: diag.CodeInsertShape, Message: "c", Span: testCase.span}}),
			)

			require.Len(t, placed, 3)
			for _, diagnostic := range placed {
				require.Equal(t, "users.sql", diagnostic.Filename)
				require.Equal(t, testCase.wantLine, diagnostic.Line, diagnostic.Message)
				require.Equal(t, testCase.wantColumn, diagnostic.Column, diagnostic.Message)
			}
		})
	}
}
