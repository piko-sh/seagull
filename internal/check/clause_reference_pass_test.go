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
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/resolve"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
	"seagull.sh/seagull/internal/testkit/scaling"
)

const referenceSQL = "SELECT id AS key FROM items WHERE nmae = 1"

type rowValueEngine struct {
	*fakedialect.Engine
}

func (rowValueEngine) RowValueType(relation dialect.RowRelation) sqlmodel.SQLType {
	return sqlmodel.NewSQLType(sqlmodel.TypeCategoryComposite, relation.Name)
}

func referenceCatalogue() *sqlmodel.Catalogue {
	catalogue := fakedialect.NewCatalogue("public")
	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")
	catalogue.Schemas["public"].Tables["items"] = fakedialect.NewTable("items",
		sqlmodel.NewColumn("id", sqlmodel.NewIntegerType("int4", 32, false), false),
		sqlmodel.NewColumn("name", text, false),
		sqlmodel.NewColumn("payload", sqlmodel.NewSQLType(sqlmodel.TypeCategoryStruct, "struct"), true),
	)
	catalogue.Schemas["public"].Tables["opaque"] = fakedialect.NewTable("opaque")
	catalogue.Schemas["public"].Tables["tags"] = fakedialect.NewTable("tags",
		sqlmodel.NewColumn("item_id", sqlmodel.NewIntegerType("int4", 32, false), false),
	)
	return catalogue
}

func referenceEngine(systemColumns []string) *fakedialect.Engine {
	engine := &fakedialect.Engine{}
	engine.BuiltinFunctionsFunc = func() *ast.FunctionCatalogue {
		lower := sqlmodel.NewFunction([]sqlmodel.FunctionArgument{{Name: "value", Type: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")}},
			sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"), sqlmodel.FunctionNullableReturnsNullOnNull, sqlmodel.WithFunctionName("lower"))
		return &ast.FunctionCatalogue{Functions: map[string][]*sqlmodel.Function{"lower": {lower}}}
	}
	info := (&fakedialect.Engine{}).Info()
	info.SystemColumns = systemColumns
	engine.InfoFunc = func() dialect.Info { return info }
	return engine
}

func column(qualifier, name string, start int) *ast.ColumnRefExpression {
	length := len(name)
	if qualifier != "" {
		length += len(qualifier) + 1
	}
	return &ast.ColumnRefExpression{TableAlias: qualifier, ColumnName: name, Span: ast.Span{Start: start, End: start + length}}
}

func call(name string, start int, arguments ...ast.Expression) *ast.FunctionCallExpression {
	return &ast.FunctionCallExpression{FunctionName: name, Arguments: arguments, Span: ast.Span{Start: start, End: start + len(name)}}
}

func clause(kind ast.Clause, expression ast.Expression) ast.ClauseExpression {
	return ast.ClauseExpression{Expression: expression, Clause: kind}
}

func runClauseReferencePass(t *testing.T, raw *ast.RawQueryAnalysis, systemColumns []string, prior []diag.SourceError) []diag.SourceError {
	t.Helper()

	return analyseClauseReferences(t.Context(), t, raw, systemColumns, prior)
}

func analyseClauseReferences(ctx context.Context, t *testing.T, raw *ast.RawQueryAnalysis, systemColumns []string, prior []diag.SourceError) []diag.SourceError {
	t.Helper()

	engine := referenceEngine(systemColumns)
	catalogue := referenceCatalogue()
	scope := resolve.NewRootScopeChain(resolve.ScopeKindQuery, resolve.ScopeRulesFor(engine))
	builder := resolve.NewCatalogueScopeBuilder(engine, catalogue)
	_ = builder.BuildQueryScope(ctx, raw, scope)
	pass := newClauseReferencePass(engine, newDialectFacts(engine), catalogue)
	return pass.Analyse(ctx, &DiagnosticContext{
		Query:               &sqlmodel.AnalysedQuery{Line: 3, SQL: referenceSQL},
		RawAnalysis:         raw,
		Scope:               scope,
		Filename:            "items.sql",
		ExistingDiagnostics: prior,
	})
}

func itemsQuery(clauses ...ast.ClauseExpression) *ast.RawQueryAnalysis {
	return &ast.RawQueryAnalysis{
		FromTables:        []ast.TableReference{{Schema: "", Name: "items", Alias: ""}},
		OutputColumns:     []ast.RawOutputColumn{{Name: "key", ColumnName: "id"}},
		ClauseExpressions: clauses,
	}
}

func describeDiagnostics(diagnostics []diag.SourceError) []string {
	described := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		described = append(described, diagnostic.Code+" "+diagnostic.Message)
	}
	return described
}

func TestClauseReferencePassReportsUnknownColumnsWhereTheyAreWritten(t *testing.T) {
	t.Parallel()

	diagnostics := runClauseReferencePass(t, itemsQuery(clause(ast.ClauseWhere, &ast.ComparisonExpression{
		Left: column("", "nmae", strings.Index(referenceSQL, "nmae")), Right: &ast.LiteralExpression{TypeName: "int"}, Operator: "=",
	})), nil, nil)

	require.Len(t, diagnostics, 1)
	require.Equal(t, diag.CodeUnknownColumn, diagnostics[0].Code)
	require.Equal(t, diag.SeverityWarning, diagnostics[0].Severity)
	require.Equal(t, "items.sql", diagnostics[0].Filename)
	require.Equal(t, 3, diagnostics[0].Line)
	require.Equal(t, strings.Index(referenceSQL, "nmae")+1, diagnostics[0].Column)
	require.Equal(t, strings.Index(referenceSQL, "nmae")+5, diagnostics[0].EndColumn)
	require.Equal(t, "name", diagnostics[0].Suggestion)
}

func TestClauseReferencePassResolvesTheNamesAQueryCanSee(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		raw           *ast.RawQueryAnalysis
		systemColumns []string
		expected      []string
	}{
		{
			name:     "a column of the query's table",
			raw:      itemsQuery(clause(ast.ClauseWhere, column("items", "name", 0))),
			expected: []string{},
		},
		{
			name:     "a select-list alias where the dialect makes it visible",
			raw:      itemsQuery(clause(ast.ClauseOrderBy, column("", "key", 0))),
			expected: []string{},
		},
		{
			name:     "a select-list alias where the dialect does not make it visible",
			raw:      itemsQuery(clause(ast.ClauseWhere, column("", "key", 0))),
			expected: []string{`Q001 unknown column "key"; the select list's alias "key" is not visible in WHERE`},
		},
		{
			name:          "a column every table has",
			raw:           itemsQuery(clause(ast.ClauseWhere, column("items", "xmin", 0))),
			systemColumns: []string{"xmin"},
			expected:      []string{},
		},
		{
			name: "a lambda parameter",
			raw: itemsQuery(clause(ast.ClauseWhere, call("lower", 0, &ast.LambdaExpression{
				Parameters: []string{"element"}, Body: column("element", "field", 0),
			}))),
			expected: []string{},
		},
		{
			name:     "a field of a struct column",
			raw:      itemsQuery(clause(ast.ClauseWhere, column("payload", "colour", 0))),
			expected: []string{},
		},
		{
			name: "a column of a relation whose columns are unknown",
			raw: &ast.RawQueryAnalysis{
				FromTables:        []ast.TableReference{{Schema: "", Name: "opaque", Alias: "o"}},
				ClauseExpressions: []ast.ClauseExpression{clause(ast.ClauseWhere, column("o", "anything", 0))},
			},
			expected: []string{},
		},
		{
			name: "a name read through a relation the analyser could not find",
			raw: &ast.RawQueryAnalysis{
				FromTables: []ast.TableReference{{Schema: "system", Name: "users", Alias: ""}},
				ClauseExpressions: []ast.ClauseExpression{
					clause(ast.ClauseWhere, column("", "name", 0)),
					clause(ast.ClauseWhere, column("users", "name", 10)),
				},
			},
			expected: []string{},
		},
		{
			name:     "an unknown function",
			raw:      itemsQuery(clause(ast.ClauseWhere, call("lowr", 0, column("", "name", 0)))),
			expected: []string{`Q005 unknown function "lowr"`},
		},
		{
			name:     "a known function with an unknown column argument",
			raw:      itemsQuery(clause(ast.ClauseHaving, call("lower", 0, column("", "nam", 0)))),
			expected: []string{`Q001 unknown column "nam"`},
		},
		{
			name: "an unknown column a struct constructor holds",
			raw: itemsQuery(clause(ast.ClauseWhere, &ast.StructExpression{
				Names:  []string{"known", "unknown"},
				Values: []ast.Expression{column("", "name", 0), column("", "nam", 10)},
			})),
			expected: []string{`Q001 unknown column "nam"`},
		},
		{
			name: "a correlated reference in a predicate subquery and an unknown one",
			raw: &ast.RawQueryAnalysis{
				FromTables: []ast.TableReference{{Schema: "", Name: "items", Alias: "i"}},
				PredicateSubqueries: []*ast.RawQueryAnalysis{{
					FromTables: []ast.TableReference{{Schema: "", Name: "tags", Alias: "t"}},
					ClauseExpressions: []ast.ClauseExpression{
						clause(ast.ClauseWhere, &ast.ComparisonExpression{Left: column("t", "item_id", 0), Right: column("i", "id", 10), Operator: "="}),
						clause(ast.ClauseWhere, column("t", "missing", 20)),
					},
				}},
			},
			expected: []string{`Q001 unknown column "missing" in table "t"`},
		},
		{
			name: "a CTE body sees its own relations and the CTEs before it",
			raw: &ast.RawQueryAnalysis{
				CTEDefinitions: []ast.RawCTEDefinition{{
					Name:          "recent",
					OutputColumns: []ast.RawOutputColumn{{Name: "id", ColumnName: "id"}},
					FromTables:    []ast.TableReference{{Schema: "", Name: "items", Alias: ""}},
					Query: &ast.RawQueryAnalysis{
						OutputColumns:     []ast.RawOutputColumn{{Name: "id", ColumnName: "id"}},
						FromTables:        []ast.TableReference{{Schema: "", Name: "items", Alias: ""}},
						ClauseExpressions: []ast.ClauseExpression{clause(ast.ClauseWhere, column("", "name", 0))},
					},
				}},
				FromTables: []ast.TableReference{{Schema: "", Name: "recent", Alias: "r"}},
				ClauseExpressions: []ast.ClauseExpression{
					clause(ast.ClauseWhere, column("r", "id", 0)),
					clause(ast.ClauseWhere, column("r", "name", 10)),
				},
			},
			expected: []string{`Q001 unknown column "name" in table "r"`},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			diagnostics := runClauseReferencePass(t, testCase.raw, testCase.systemColumns, nil)
			require.Equal(t, testCase.expected, describeDiagnostics(diagnostics))
		})
	}
}

func TestClauseReferencePassDoesNotRepeatAnEarlierReport(t *testing.T) {
	t.Parallel()

	raw := itemsQuery(
		clause(ast.ClauseWhere, column("", "nmae", 0)),
		clause(ast.ClauseWhere, column("", "nmae", 0)),
		clause(ast.ClauseWhere, column("", "other", 10)),
	)
	prior := []diag.SourceError{{Code: diag.CodeUnknownColumn, Message: `unknown column "other"`}}

	diagnostics := runClauseReferencePass(t, raw, nil, prior)

	require.Equal(t, []string{`Q001 unknown column "nmae"`}, describeDiagnostics(diagnostics))
}

func TestClauseReferencePassNeedsTheWholeContext(t *testing.T) {
	t.Parallel()

	engine := referenceEngine(nil)
	pass := newClauseReferencePass(engine, newDialectFacts(engine), referenceCatalogue())

	require.Empty(t, pass.Analyse(t.Context(), nil))
	require.Empty(t, pass.Analyse(t.Context(), &DiagnosticContext{RawAnalysis: itemsQuery(clause(ast.ClauseWhere, column("", "nmae", 0)))}))
}

func TestClauseReferencePassReadsARelationNamedAsAValueAsItsRow(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		engine resolve.TypeRules
		name   string
		want   []string
	}{
		{name: "where the dialect has row values", engine: rowValueEngine{Engine: referenceEngine(nil)}, want: []string{}},
		{name: "where the dialect has none", engine: referenceEngine(nil), want: []string{`Q001 unknown column "items"`}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			raw := itemsQuery(clause(ast.ClauseWhere, &ast.IsNullExpression{Inner: column("", "items", 30)}))
			catalogue := referenceCatalogue()
			scope := resolve.NewRootScopeChain(resolve.ScopeKindQuery, resolve.ScopeRulesFor(testCase.engine))
			_ = resolve.NewCatalogueScopeBuilder(referenceEngine(nil), catalogue).BuildQueryScope(t.Context(), raw, scope)
			engine := referenceEngine(nil)
			pass := newClauseReferencePass(engine, newDialectFacts(engine), catalogue)

			diagnostics := pass.Analyse(t.Context(), &DiagnosticContext{
				Query:       &sqlmodel.AnalysedQuery{Line: 3, SQL: referenceSQL},
				RawAnalysis: raw,
				Scope:       scope,
				Filename:    "items.sql",
			})

			require.Equal(t, testCase.want, describeDiagnostics(diagnostics))
		})
	}
}

func TestClauseReferencePassLeavesTheValuesOfAValuesRowAlone(t *testing.T) {
	t.Parallel()

	diagnostics := runClauseReferencePass(t, itemsQuery(
		clause(ast.ClauseInsertValue, column("", "DEFAULT", 0)),
		clause(ast.ClauseInsertValue, call("unknown_function", 0)),
	), nil, nil)

	require.Empty(t, diagnostics)
}

func TestClauseReferencePassChecksNoNestedQueryOnceTheContextIsDone(t *testing.T) {
	t.Parallel()

	nested := &ast.RawQueryAnalysis{
		FromTables:        []ast.TableReference{{Schema: "", Name: "tags", Alias: "t"}},
		ClauseExpressions: []ast.ClauseExpression{clause(ast.ClauseWhere, column("t", "missing", 20))},
	}
	raw := &ast.RawQueryAnalysis{
		FromTables:          []ast.TableReference{{Schema: "", Name: "items", Alias: "i"}},
		PredicateSubqueries: []*ast.RawQueryAnalysis{nested},
	}
	testCases := []struct {
		name      string
		cancelled bool
		expected  []string
	}{
		{name: "a live context checks the nested query", cancelled: false, expected: []string{`Q001 unknown column "missing" in table "t"`}},
		{name: "a cancelled context checks nothing more", cancelled: true, expected: []string{}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if testCase.cancelled {
				cancel()
			}
			engine := referenceEngine(nil)
			catalogue := referenceCatalogue()
			builder := resolve.NewCatalogueScopeBuilder(engine, catalogue)
			scope := resolve.NewRootScopeChain(resolve.ScopeKindQuery, resolve.ScopeRulesFor(engine))
			_ = builder.BuildQueryScope(t.Context(), raw, scope)
			checker := newReferenceChecker(builder, newDialectFacts(engine), &DiagnosticContext{
				Query:    &sqlmodel.AnalysedQuery{Line: 3, SQL: referenceSQL},
				Filename: "items.sql",
			})

			checker.checkQuery(ctx, raw, scope)

			require.Equal(t, testCase.expected, describeDiagnostics(checker.problems.finish()))
		})
	}
}

func nestedExistsQuery(levels int, innermost *ast.RawQueryAnalysis) *ast.RawQueryAnalysis {
	query := innermost
	for range levels {
		query = &ast.RawQueryAnalysis{
			FromTables:          []ast.TableReference{{Schema: "", Name: "items", Alias: ""}},
			OutputColumns:       []ast.RawOutputColumn{{Name: "key", ColumnName: "id"}},
			ClauseExpressions:   []ast.ClauseExpression{clause(ast.ClauseWhere, &ast.ExistsExpression{InnerQuery: query})},
			PredicateSubqueries: []*ast.RawQueryAnalysis{query},
		}
	}
	return query
}

func TestClauseReferencePassChecksEachNestedQueryOnce(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		want   []string
		levels int
	}{
		{name: "one EXISTS", levels: 1, want: []string{`Q001 unknown column "nmae"`}},
		{name: "forty nested EXISTS", levels: 40, want: []string{`Q001 unknown column "nmae"`}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			innermost := itemsQuery(clause(ast.ClauseWhere, column("", "nmae", strings.Index(referenceSQL, "nmae"))))
			raw := nestedExistsQuery(testCase.levels, innermost)

			var diagnostics []diag.SourceError
			scaling.Within(t, time.Second, func(ctx context.Context) {
				diagnostics = analyseClauseReferences(ctx, t, raw, nil, nil)
			})

			require.Equal(t, testCase.want, describeDiagnostics(diagnostics))
		})
	}
}

func TestClauseReferencePassChecksNestedQueriesUnderTheContextItIsGiven(t *testing.T) {
	t.Parallel()

	nested := &ast.RawQueryAnalysis{
		FromTables:        []ast.TableReference{{Schema: "", Name: "tags", Alias: "t"}},
		ClauseExpressions: []ast.ClauseExpression{clause(ast.ClauseWhere, column("t", "missing", 20))},
	}
	raw := &ast.RawQueryAnalysis{
		FromTables:          []ast.TableReference{{Schema: "", Name: "items", Alias: "i"}},
		PredicateSubqueries: []*ast.RawQueryAnalysis{nested},
	}
	testCases := []struct {
		name      string
		expected  []string
		cancelled bool
	}{
		{name: "a live context", cancelled: false, expected: []string{`Q001 unknown column "missing" in table "t"`}},
		{name: "a cancelled context", cancelled: true, expected: []string{}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			engine := referenceEngine(nil)
			catalogue := referenceCatalogue()
			scope := resolve.NewRootScopeChain(resolve.ScopeKindQuery, resolve.ScopeRulesFor(engine))
			_ = resolve.NewCatalogueScopeBuilder(engine, catalogue).BuildQueryScope(t.Context(), raw, scope)
			if testCase.cancelled {
				cancel()
			}
			pass := newClauseReferencePass(engine, newDialectFacts(engine), catalogue)

			diagnostics := pass.Analyse(ctx, &DiagnosticContext{
				Query:       &sqlmodel.AnalysedQuery{Line: 3, SQL: referenceSQL},
				RawAnalysis: raw,
				Scope:       scope,
				Filename:    "items.sql",
			})

			require.Equal(t, testCase.expected, describeDiagnostics(diagnostics))
		})
	}
}

func TestClauseReferencePassReportsAtMostTheQueryCapOfProblems(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		unknown     int
		wantSummary bool
	}{
		{name: "problems within the cap", unknown: 3, wantSummary: false},
		{name: "problems past the cap", unknown: MaxDiagnosticsPerQuery + 10, wantSummary: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			operands := make([]ast.Expression, 0, testCase.unknown)
			for index := range testCase.unknown {
				operands = append(operands, column("", fmt.Sprintf("missing_%d", index), 0))
			}
			raw := itemsQuery(clause(ast.ClauseWhere, &ast.LogicalOpExpression{Operator: "OR", Operands: operands}))

			diagnostics := runClauseReferencePass(t, raw, nil, nil)

			if !testCase.wantSummary {
				require.Len(t, diagnostics, testCase.unknown)
				return
			}
			require.Len(t, diagnostics, MaxDiagnosticsPerQuery+1)
			summary := diagnostics[len(diagnostics)-1]
			require.Equal(t, diag.CodeDiagnosticLimit, summary.Code)
			require.Equal(t, diag.SeverityWarning, summary.Severity)
			require.Equal(t, "and 10 more diagnostics were left out", summary.Message)
		})
	}
}

func TestClauseReferencePassChecksTheArgumentsOfFromCalls(t *testing.T) {
	t.Parallel()

	integer := &ast.LiteralExpression{TypeName: "int"}
	testCases := []struct {
		name      string
		reference ast.RawTableValuedFunctionReference
		want      []string
	}{
		{name: "an unknown column", want: []string{`Q001 unknown column "missing_col" in table "i"`},
			reference: ast.RawTableValuedFunctionReference{FunctionName: "series", Alias: "g",
				Arguments: []ast.Expression{integer, column("i", "missing_col", 0)}}},
		{name: "an unknown function", want: []string{`Q005 unknown function "no_such_fn"`},
			reference: ast.RawTableValuedFunctionReference{FunctionName: "series", Alias: "g",
				Arguments: []ast.Expression{integer, call("no_such_fn", 0, column("i", "id", 0))}}},
		{name: "an unknown column in one of several calls", want: []string{`Q001 unknown column "missing_col" in table "i"`},
			reference: ast.RawTableValuedFunctionReference{FunctionName: "series", Alias: "g", Functions: []ast.TableFunctionCall{
				{FunctionName: "series", Arguments: []ast.Expression{integer}},
				{FunctionName: "series", Arguments: []ast.Expression{column("i", "missing_col", 0)}},
			}}},
		{name: "a column of a relation the query reads", want: []string{},
			reference: ast.RawTableValuedFunctionReference{FunctionName: "series", Alias: "g",
				Arguments: []ast.Expression{integer, column("i", "id", 0)}}},
		{name: "a column of a derived table registered after the call", want: []string{},
			reference: ast.RawTableValuedFunctionReference{FunctionName: "series", Alias: "g",
				Arguments: []ast.Expression{integer, column("d", "id", 0)}}},
		{name: "a column of a relation the analyser could not find", want: []string{},
			reference: ast.RawTableValuedFunctionReference{FunctionName: "series", Alias: "g",
				Arguments: []ast.Expression{integer, column("u", "anything", 0)}}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			raw := &ast.RawQueryAnalysis{
				FromTables:              []ast.TableReference{{Schema: "", Name: "items", Alias: "i"}, {Schema: "", Name: "unknown_table", Alias: "u"}},
				RawTableValuedFunctions: []ast.RawTableValuedFunctionReference{testCase.reference},
				RawDerivedTables: []ast.RawDerivedTableReference{{Alias: "d", InnerQuery: &ast.RawQueryAnalysis{
					FromTables:    []ast.TableReference{{Schema: "", Name: "items", Alias: ""}},
					OutputColumns: []ast.RawOutputColumn{{Name: "id", ColumnName: "id"}},
				}}},
			}

			diagnostics := runClauseReferencePass(t, raw, nil, nil)

			require.Equal(t, testCase.want, describeDiagnostics(diagnostics))
		})
	}
}

func TestClauseReferencePassReportsTheArrayJoinProblemsOfNestedQueries(t *testing.T) {
	t.Parallel()

	const message = `unknown array join source column "tagz"`
	sourceAt := strings.Index(referenceSQL, "nmae")
	nested := func() *ast.RawQueryAnalysis {
		return &ast.RawQueryAnalysis{
			FromTables:       []ast.TableReference{{Name: "items", Alias: "inner_items"}},
			OutputColumns:    []ast.RawOutputColumn{{Name: "id", ColumnName: "id"}},
			ArrayJoinClauses: []ast.RawArrayJoinClause{{Source: column("", "tagz", sourceAt), Alias: "tag"}},
		}
	}
	testCases := []struct {
		raw   func() *ast.RawQueryAnalysis
		name  string
		prior []diag.SourceError
		want  []string
	}{
		{name: "an EXISTS subquery", prior: nil, want: []string{"Q001 " + message},
			raw: func() *ast.RawQueryAnalysis {
				return itemsQuery(clause(ast.ClauseWhere, &ast.ExistsExpression{InnerQuery: nested()}))
			}},
		{name: "a scalar subquery of the select list", prior: nil, want: []string{"Q001 " + message},
			raw: func() *ast.RawQueryAnalysis {
				raw := itemsQuery()
				raw.OutputColumns = append(raw.OutputColumns, ast.RawOutputColumn{Name: "first", Expression: &ast.ScalarSubqueryExpression{InnerQuery: nested()}})
				return raw
			}},
		{name: "a derived table the analyser reported on already", want: []string{},
			prior: []diag.SourceError{{Code: diag.CodeUnknownColumn, Message: message}},
			raw: func() *ast.RawQueryAnalysis {
				raw := itemsQuery()
				raw.RawDerivedTables = []ast.RawDerivedTableReference{{Alias: "derived", InnerQuery: nested()}}
				return raw
			}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			diagnostics := runClauseReferencePass(t, testCase.raw(), nil, testCase.prior)

			require.Equal(t, testCase.want, describeDiagnostics(diagnostics))
			if len(diagnostics) > 0 {
				require.Equal(t, diag.SeverityWarning, diagnostics[0].Severity)
				require.Equal(t, sourceAt+1, diagnostics[0].Column, "placed at the source")
			}
		})
	}
}

func TestClauseReferencePassChecksExpressionsOfAnyDepth(t *testing.T) {
	t.Parallel()

	deep := func(depth int, leaf ast.Expression) ast.Expression {
		expression := leaf
		for range depth {
			expression = &ast.UnaryOpExpression{Operator: "-", Operand: expression}
		}
		return expression
	}
	unknownAt := strings.Index(referenceSQL, "nmae")
	testCases := []struct {
		expression ast.Expression
		name       string
		want       []string
	}{
		{name: "an unknown column far deeper than the analyser types", expression: deep(50_000, column("", "nmae", unknownAt)),
			want: []string{`Q001 unknown column "nmae"`}},
		{name: "a lambda parameter far below its lambda", want: []string{},
			expression: &ast.FunctionCallExpression{FunctionName: "lower", Arguments: []ast.Expression{
				&ast.LambdaExpression{Parameters: []string{"nmae"}, Body: deep(50_000, column("", "nmae", unknownAt))},
			}}},
		{name: "a lambda binding, which stands for a value written in the call", want: []string{},
			expression: &ast.FunctionCallExpression{FunctionName: "lower", Arguments: []ast.Expression{
				&ast.LambdaExpression{Parameters: []string{"x"}, Body: column("", "x", 0),
					Bindings: []ast.Expression{column("", "nmae", unknownAt)}},
			}}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var diagnostics []diag.SourceError
			scaling.Within(t, time.Second, func(ctx context.Context) {
				diagnostics = analyseClauseReferences(ctx, t, itemsQuery(clause(ast.ClauseWhere, testCase.expression)), nil, nil)
			})

			require.Equal(t, testCase.want, describeDiagnostics(diagnostics))
		})
	}
}
