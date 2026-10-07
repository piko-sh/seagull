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

package sqlite

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/sqlmodel"
)

func newSQLiteCatalogue() *sqlmodel.Catalogue {
	return &sqlmodel.Catalogue{
		DefaultSchema: "main",
		Schemas: map[string]*sqlmodel.Schema{
			"main": {
				Name: "main",
				Tables: map[string]*sqlmodel.Table{
					"users": {
						Name: "users",
						Columns: []sqlmodel.Column{
							{
								Name:    "id",
								SQLType: sqlmodel.SQLType{EngineName: "integer", Category: sqlmodel.TypeCategoryInteger},
							},
							{
								Name:    "name",
								SQLType: sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText},
							},
							{
								Name:     "email",
								SQLType:  sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText},
								Nullable: true,
							},
						},
					},
					"posts": {
						Name: "posts",
						Columns: []sqlmodel.Column{
							{
								Name:    "id",
								SQLType: sqlmodel.SQLType{EngineName: "integer", Category: sqlmodel.TypeCategoryInteger},
							},
							{
								Name:    "user_id",
								SQLType: sqlmodel.SQLType{EngineName: "integer", Category: sqlmodel.TypeCategoryInteger},
							},
							{
								Name:    "title",
								SQLType: sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText},
							},
							{
								Name:     "body",
								SQLType:  sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText},
								Nullable: true,
							},
						},
					},
				},
				Views:          map[string]*sqlmodel.View{},
				Functions:      map[string][]*sqlmodel.Function{},
				Enums:          map[string]*sqlmodel.Enum{},
				CompositeTypes: map[string]*sqlmodel.CompositeType{},
				Sequences:      map[string]*sqlmodel.Sequence{},
			},
		},
		Extensions: map[string]struct{}{},
	}
}

func analyseQuery(t *testing.T, catalogue *sqlmodel.Catalogue, sql string) *ast.RawQueryAnalysis {
	t.Helper()

	engine := New()
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.NotEmpty(t, statements)

	analysis, err := engine.AnalyseQuery(t.Context(), catalogue, statements[0])
	require.NoError(t, err)

	return analysis
}

func TestAnalyseQuery_CompoundLimitOffsetRegistersParameter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		sql       string
		wantCount int
	}{
		{name: "limit coalesce", sql: "SELECT id FROM users LIMIT COALESCE(?1, 100)", wantCount: 1},
		{name: "limit compound comma offset", sql: "SELECT id FROM users LIMIT COALESCE(?1, 5), ?2", wantCount: 2},
		{name: "offset compound", sql: "SELECT id FROM users LIMIT ?1 OFFSET ABS(?2)", wantCount: 2},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			analysis := analyseQuery(t, nil, testCase.sql)
			require.Len(t, analysis.ParameterReferences, testCase.wantCount,
				"a parameter nested in a compound LIMIT/OFFSET value must be registered")
		})
	}
}

func TestAnalyseQueryCollectsCompoundArmsIntoOneFlatList(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		sql           string
		wantOperators []ast.CompoundOperator
	}{
		{
			name:          "three arms with mixed operators",
			sql:           "SELECT id FROM a UNION SELECT id FROM b INTERSECT SELECT id FROM c ORDER BY id LIMIT ?",
			wantOperators: []ast.CompoundOperator{ast.CompoundUnion, ast.CompoundIntersect},
		},
		{
			name:          "union all followed by except",
			sql:           "SELECT id FROM a UNION ALL SELECT id FROM b EXCEPT SELECT id FROM c",
			wantOperators: []ast.CompoundOperator{ast.CompoundUnionAll, ast.CompoundExcept},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, nil, testCase.sql)

			require.Len(t, analysis.CompoundBranches, len(testCase.wantOperators))
			for index, branch := range analysis.CompoundBranches {
				assert.Equal(t, testCase.wantOperators[index], branch.Operator)
				require.NotNil(t, branch.Query)
				assert.Empty(t, branch.Query.CompoundBranches, "arms must not nest inside one another")
			}
		})
	}
}

func TestAnalyseQueryHandlesLongCompoundChains(t *testing.T) {
	t.Parallel()

	const arms = 10_000
	sql := "SELECT 1" + strings.Repeat(" UNION SELECT 1", arms-1)

	analysis := analyseQuery(t, nil, sql)

	assert.Len(t, analysis.CompoundBranches, arms-1)
}

func TestAnalyseQuery_ExistsSubqueryExposesInnerQueryParameters(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, nil,
		`SELECT EXISTS(SELECT 1 FROM orchestrator_tasks WHERE workflow_id = ?1 AND status = ?2) AS has_incomplete`)

	require.Len(t, analysis.OutputColumns, 1)
	exists, ok := analysis.OutputColumns[0].Expression.(*ast.ExistsExpression)
	require.Truef(t, ok, "EXISTS output column should carry an ExistsExpression, got %T", analysis.OutputColumns[0].Expression)
	require.NotNil(t, exists.InnerQuery)

	require.Len(t, exists.InnerQuery.FromTables, 1)
	assert.Equal(t, "orchestrator_tasks", exists.InnerQuery.FromTables[0].Name)

	require.NotEmpty(t, exists.InnerQuery.ParameterReferences)
	first := exists.InnerQuery.ParameterReferences[0]
	require.NotNil(t, first.ColumnReference)
	assert.Equal(t, "workflow_id", first.ColumnReference.ColumnName)
}

func TestAnalyseQuery_WherePredicateSubqueryCaptured(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, nil,
		`SELECT a.id FROM accounts a WHERE a.id = (SELECT MAX(av2.id) FROM account_versions av2 WHERE av2.id < ?2)`)

	require.Len(t, analysis.PredicateSubqueries, 1)
	inner := analysis.PredicateSubqueries[0]
	require.NotNil(t, inner)
	require.Len(t, inner.FromTables, 1)
	assert.Equal(t, "account_versions", inner.FromTables[0].Name)
	assert.Equal(t, "av2", inner.FromTables[0].Alias)

	require.NotEmpty(t, inner.ParameterReferences)
	innerParameter := inner.ParameterReferences[len(inner.ParameterReferences)-1]
	require.NotNil(t, innerParameter.ColumnReference)
	assert.Equal(t, "av2", innerParameter.ColumnReference.TableAlias)
	assert.Equal(t, "id", innerParameter.ColumnReference.ColumnName)

	foundInFlat := false
	for _, parameter := range analysis.ParameterReferences {
		if parameter.ColumnReference != nil && parameter.ColumnReference.TableAlias == "av2" {
			foundInFlat = true
		}
	}
	assert.True(t, foundInFlat, "the subquery parameter must remain spliced into the flat list")
}

func TestAnalyseQuery_WhereParenthesisedExpressionNotCaptured(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, nil, `SELECT a.id FROM accounts a WHERE (a.id + 1) = ?1`)

	assert.Empty(t, analysis.PredicateSubqueries, "a parenthesised arithmetic expression is not a subquery")
}

func TestAnalyseQuery_SetCaseComparisonParameterTypedFromOperand(t *testing.T) {
	t.Parallel()

	catalogue := newSQLiteCatalogue()
	analysis := analyseQuery(t, catalogue,
		"UPDATE users SET name = CASE WHEN id >= ? THEN 'flagged' ELSE name END WHERE email = ?")

	require.NotEmpty(t, analysis.ParameterReferences)
	caseParameter := analysis.ParameterReferences[0]
	assert.Equal(t, sqlmodel.ParameterContextComparison, caseParameter.Context,
		"a parameter compared against a column must use comparison context, not assignment")
	require.NotNil(t, caseParameter.ColumnReference)
	assert.Equal(t, "id", caseParameter.ColumnReference.ColumnName,
		"the parameter must be typed from the compared column (id), not the SET target (name)")
}

func TestAnalyseQuery_Select(t *testing.T) {
	t.Parallel()

	catalogue := newSQLiteCatalogue()

	tests := []struct {
		assertions func(t *testing.T, analysis *ast.RawQueryAnalysis)
		name       string
		sql        string
	}{
		{
			name: "simple column list",
			sql:  "SELECT id, name FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 2)

				assert.Equal(t, "id", analysis.OutputColumns[0].Name)
				assert.Equal(t, "id", analysis.OutputColumns[0].ColumnName)
				assert.Equal(t, "name", analysis.OutputColumns[1].Name)
				assert.Equal(t, "name", analysis.OutputColumns[1].ColumnName)

				require.Len(t, analysis.FromTables, 1)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
				assert.True(t, analysis.ReadOnly, "SELECT should be read-only")
			},
		},
		{
			name: "star expansion",
			sql:  "SELECT * FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
				assert.True(t, analysis.OutputColumns[0].IsStar, "should be a star column")
			},
		},
		{
			name: "WHERE with numbered parameter ?1",
			sql:  "SELECT id FROM users WHERE id = ?1",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
				assert.Equal(t, "id", analysis.OutputColumns[0].Name)

				require.NotEmpty(t, analysis.ParameterReferences, "should have at least one parameter reference")
				assert.Equal(t, 1, analysis.ParameterReferences[0].Number)
			},
		},
		{
			name: "WHERE with named parameter :email",
			sql:  "SELECT id FROM users WHERE email = :email",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.ParameterReferences, "should have at least one parameter reference")
				assert.Equal(t, "email", analysis.ParameterReferences[0].Name)
			},
		},
		{
			name: "WHERE with LEFT() function call does not lose trailing parameter",
			sql:  "SELECT id FROM users WHERE name = LEFT(?, 3) AND email = ?",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 2,
					"LEFT() must not be treated as a JOIN terminator and swallow the second parameter")
			},
		},
		{
			name: "WHERE with RIGHT() function call does not lose trailing parameter",
			sql:  "SELECT id FROM users WHERE name = RIGHT(?, 3) AND email = ?",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 2)
			},
		},
		{
			name: "JOIN across two tables",
			sql:  "SELECT u.id, p.title FROM users u JOIN posts p ON p.user_id = u.id",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 2)

				require.NotEmpty(t, analysis.FromTables)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
				assert.Equal(t, "u", analysis.FromTables[0].Alias)

				require.NotEmpty(t, analysis.JoinClauses)
				assert.Equal(t, "posts", analysis.JoinClauses[0].Table.Name)
				assert.Equal(t, "p", analysis.JoinClauses[0].Table.Alias)
				assert.Equal(t, ast.JoinInner, analysis.JoinClauses[0].Kind)
			},
		},
		{
			name: "LEFT JOIN",
			sql:  "SELECT u.id, p.title FROM users u LEFT JOIN posts p ON p.user_id = u.id",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.JoinClauses)
				assert.Equal(t, ast.JoinLeft, analysis.JoinClauses[0].Kind)
			},
		},
		{
			name: "aggregate function COUNT",
			sql:  "SELECT COUNT(*) FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
				assert.Equal(t, "count", analysis.OutputColumns[0].Name)
				assert.True(t, analysis.ReadOnly, "SELECT with aggregate should be read-only")
			},
		},
		{
			name: "ORDER BY and LIMIT",
			sql:  "SELECT id, name FROM users ORDER BY name LIMIT 10",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 2)
				require.Len(t, analysis.FromTables, 1)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "qualified star expansion",
			sql:  "SELECT u.* FROM users u",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
				assert.True(t, analysis.OutputColumns[0].IsStar, "should be a star column")
				assert.Equal(t, "u", analysis.OutputColumns[0].TableAlias, "star should be qualified with table alias")
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			analysis := analyseQuery(t, catalogue, testCase.sql)
			testCase.assertions(t, analysis)
		})
	}
}

func TestAnalyseQuery_Select_CTEsAndCompoundQueries(t *testing.T) {
	t.Parallel()

	catalogue := newSQLiteCatalogue()

	tests := []struct {
		assertions func(t *testing.T, analysis *ast.RawQueryAnalysis)
		name       string
		sql        string
	}{
		{
			name: "CTE with SELECT",
			sql:  "WITH active AS (SELECT id, name FROM users WHERE name IS NOT NULL) SELECT id, name FROM active",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.CTEDefinitions, 1)
				assert.Equal(t, "active", analysis.CTEDefinitions[0].Name)
				require.Len(t, analysis.CTEDefinitions[0].OutputColumns, 2)
				assert.Equal(t, "id", analysis.CTEDefinitions[0].OutputColumns[0].Name)
				assert.Equal(t, "name", analysis.CTEDefinitions[0].OutputColumns[1].Name)
			},
		},
		{
			name: "CTE with explicit column names",
			sql:  "WITH cte (user_id, user_name) AS (SELECT id, name FROM users) SELECT user_id, user_name FROM cte",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.CTEDefinitions, 1)
				assert.Equal(t, "cte", analysis.CTEDefinitions[0].Name)
				require.Len(t, analysis.CTEDefinitions[0].OutputColumns, 2)
				assert.Equal(t, []string{"user_id", "user_name"}, analysis.CTEDefinitions[0].ColumnNames)
			},
		},
		{
			name: "multiple CTEs",
			sql:  "WITH a AS (SELECT id FROM users), b AS (SELECT user_id FROM posts) SELECT a.id FROM a JOIN b ON a.id = b.user_id",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.CTEDefinitions, 2)
				assert.Equal(t, "a", analysis.CTEDefinitions[0].Name)
				assert.Equal(t, "b", analysis.CTEDefinitions[1].Name)
			},
		},
		{

			name: "CTE body parameters recorded on the definition",
			sql:  "WITH latest AS (SELECT MAX(id) AS id FROM versions WHERE folder_id = ?2 AND id < ?1) SELECT id FROM latest",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.CTEDefinitions, 1)
				require.Len(t, analysis.CTEDefinitions[0].ParameterReferences, 2)
				require.NotNil(t, analysis.CTEDefinitions[0].ParameterReferences[0].ColumnReference)
				assert.Equal(t, "folder_id", analysis.CTEDefinitions[0].ParameterReferences[0].ColumnReference.ColumnName)
				require.NotNil(t, analysis.CTEDefinitions[0].ParameterReferences[1].ColumnReference)
				assert.Equal(t, "id", analysis.CTEDefinitions[0].ParameterReferences[1].ColumnReference.ColumnName)
			},
		},
		{

			name: "CTE body join clauses recorded on the definition",
			sql:  "WITH joined AS (SELECT u.id FROM users u INNER JOIN posts p ON p.user_id = u.id WHERE u.id > ?1) SELECT id FROM joined",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.CTEDefinitions, 1)
				require.NotEmpty(t, analysis.CTEDefinitions[0].JoinClauses)
				assert.Equal(t, "posts", analysis.CTEDefinitions[0].JoinClauses[0].Table.Name)
				require.Len(t, analysis.CTEDefinitions[0].ParameterReferences, 1)
			},
		},
		{
			name: "UNION compound query",
			sql:  "SELECT id, name FROM users WHERE id < 5 UNION SELECT id, name FROM users WHERE id >= 5",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.CompoundBranches, 1)
				assert.Equal(t, ast.CompoundUnion, analysis.CompoundBranches[0].Operator)
				require.NotNil(t, analysis.CompoundBranches[0].Query)
			},
		},
		{
			name: "INTERSECT compound query",
			sql:  "SELECT id FROM users INTERSECT SELECT user_id FROM posts",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.CompoundBranches, 1)
				assert.Equal(t, ast.CompoundIntersect, analysis.CompoundBranches[0].Operator)
			},
		},
		{
			name: "EXCEPT compound query",
			sql:  "SELECT id FROM users EXCEPT SELECT user_id FROM posts",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.CompoundBranches, 1)
				assert.Equal(t, ast.CompoundExcept, analysis.CompoundBranches[0].Operator)
			},
		},
		{
			name: "subquery in FROM",
			sql:  "SELECT s.id FROM (SELECT id FROM users) s",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.RawDerivedTables)
				assert.Equal(t, "s", analysis.RawDerivedTables[0].Alias)
			},
		},
		{
			name: "GROUP BY and HAVING",
			sql:  "SELECT user_id, COUNT(*) FROM posts GROUP BY user_id HAVING COUNT(*) > 1",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.GroupByColumns, 1)
				assert.Equal(t, "user_id", analysis.GroupByColumns[0].ColumnName)
			},
		},
		{
			name: "GROUP BY multiple columns",
			sql:  "SELECT user_id, name FROM users GROUP BY user_id, name",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.GroupByColumns, 2)
				assert.Equal(t, "user_id", analysis.GroupByColumns[0].ColumnName)
				assert.Equal(t, "name", analysis.GroupByColumns[1].ColumnName)
			},
		},
		{
			name: "GROUP BY expression key keeps following HAVING parameter",
			sql:  "SELECT user_id FROM posts GROUP BY DATE(created_at) HAVING COUNT(*) > ?1",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 1)
			},
		},
		{
			name: "GROUP BY expression key followed by ORDER BY parameter",
			sql:  "SELECT user_id FROM posts GROUP BY user_id, ABS(score) ORDER BY ?1",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.GroupByColumns, 1)
				assert.Equal(t, "user_id", analysis.GroupByColumns[0].ColumnName)
				require.Len(t, analysis.ParameterReferences, 1)
			},
		},
		{
			name: "DISTINCT modifier",
			sql:  "SELECT DISTINCT name FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
				assert.Equal(t, "name", analysis.OutputColumns[0].Name)
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "window function with OVER",
			sql:  "SELECT id, ROW_NUMBER() OVER (ORDER BY id) FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 2)
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "window function with PARTITION BY",
			sql:  "SELECT user_id, title, ROW_NUMBER() OVER (PARTITION BY user_id ORDER BY title) FROM posts",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 3)
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "CASE expression in SELECT",
			sql:  "SELECT id, CASE WHEN name IS NOT NULL THEN name ELSE 'unknown' END FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 2)
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "COALESCE expression",
			sql:  "SELECT COALESCE(email, 'none') FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "CAST expression",
			sql:  "SELECT CAST(id AS TEXT) FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "IS NULL condition",
			sql:  "SELECT id FROM users WHERE email IS NULL",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "IS NOT NULL condition",
			sql:  "SELECT id FROM users WHERE email IS NOT NULL",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "BETWEEN with parameters in WHERE",
			sql:  "SELECT id FROM users WHERE id BETWEEN ? AND ?",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 2)

				assert.Equal(t, 1, analysis.ParameterReferences[0].Number)
				assert.Equal(t, 2, analysis.ParameterReferences[1].Number)
			},
		},
		{
			name: "IN list with parameters",
			sql:  "SELECT id FROM users WHERE id IN (?1, ?2, ?3)",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 3)
				assert.Equal(t, sqlmodel.ParameterContextInList, analysis.ParameterReferences[0].Context)
			},
		},
		{
			name: "LIKE expression",
			sql:  "SELECT id, name FROM users WHERE name LIKE '%test%'",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 2)
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "ORDER BY with DESC",
			sql:  "SELECT id, name FROM users ORDER BY name DESC",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 2)
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "LIMIT with OFFSET parameter",
			sql:  "SELECT id FROM users LIMIT 10 OFFSET ?1",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextOffset, analysis.ParameterReferences[0].Context)
			},
		},
		{
			name: "LIMIT with parameter",
			sql:  "SELECT id FROM users LIMIT ?1",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextLimit, analysis.ParameterReferences[0].Context)
			},
		},
		{
			name: "multiple JOINs",
			sql:  "SELECT u.name, p.title FROM users u JOIN posts p ON p.user_id = u.id LEFT JOIN posts p2 ON p2.user_id = u.id",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.JoinClauses)
				assert.Equal(t, ast.JoinInner, analysis.JoinClauses[0].Kind)
				assert.Equal(t, "posts", analysis.JoinClauses[0].Table.Name)
			},
		},
		{
			name: "CROSS JOIN",
			sql:  "SELECT u.id, p.id FROM users u CROSS JOIN posts p",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.JoinClauses, 1)
				assert.Equal(t, ast.JoinCross, analysis.JoinClauses[0].Kind)
			},
		},
		{
			name: "correlated scalar subquery in SELECT",
			sql:  "SELECT id, (SELECT COUNT(*) FROM posts p WHERE p.user_id = u.id) FROM users u",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 2)
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "EXISTS subquery in WHERE",
			sql:  "SELECT id FROM users u WHERE EXISTS (SELECT 1 FROM posts p WHERE p.user_id = u.id)",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "comma-joined tables",
			sql:  "SELECT u.id, p.id FROM users u, posts p WHERE u.id = p.user_id",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.FromTables, 2)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
				assert.Equal(t, "posts", analysis.FromTables[1].Name)
			},
		},
		{
			name: "column alias with AS",
			sql:  "SELECT name AS user_name FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
				assert.Equal(t, "user_name", analysis.OutputColumns[0].Name)
			},
		},
		{
			name: "function call in output",
			sql:  "SELECT MAX(id) FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
				assert.Equal(t, "max", analysis.OutputColumns[0].Name)
			},
		},
		{
			name: "CAST parameter context detection",
			sql:  "SELECT CAST(? AS INTEGER) FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextCast, analysis.ParameterReferences[0].Context)
				require.NotNil(t, analysis.ParameterReferences[0].CastType)
			},
		},
		{
			name: "parameter in function argument",
			sql:  "SELECT UPPER(?) FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextFunctionArgument, analysis.ParameterReferences[0].Context)
			},
		},
		{
			name: "comparison context for parameter",
			sql:  "SELECT id FROM users WHERE id = ?",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextComparison, analysis.ParameterReferences[0].Context)
				require.NotNil(t, analysis.ParameterReferences[0].ColumnReference)
				assert.Equal(t, "id", analysis.ParameterReferences[0].ColumnReference.ColumnName)
			},
		},
		{
			name: "UNION ALL compound query",
			sql:  "SELECT id FROM users UNION ALL SELECT id FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.CompoundBranches, 1)
				assert.Equal(t, ast.CompoundUnionAll, analysis.CompoundBranches[0].Operator)
			},
		},
		{
			name: "RIGHT JOIN",
			sql:  "SELECT u.id, p.title FROM users u RIGHT JOIN posts p ON p.user_id = u.id",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.JoinClauses, 1)
				assert.Equal(t, ast.JoinRight, analysis.JoinClauses[0].Kind)
			},
		},
		{
			name: "FULL OUTER JOIN",
			sql:  "SELECT u.id, p.title FROM users u FULL OUTER JOIN posts p ON p.user_id = u.id",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.JoinClauses, 1)
				assert.Equal(t, ast.JoinFull, analysis.JoinClauses[0].Kind)
			},
		},
		{
			name: "LIMIT comma OFFSET syntax",
			sql:  "SELECT id FROM users LIMIT 10, 20",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "CASE with simple form",
			sql:  "SELECT CASE id WHEN 1 THEN 'one' WHEN 2 THEN 'two' ELSE 'other' END FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "boolean literals in expression",
			sql:  "SELECT id FROM users WHERE TRUE AND NOT FALSE",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "GROUP BY with table-qualified column",
			sql:  "SELECT u.name FROM users u GROUP BY u.name",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.GroupByColumns, 1)
				assert.Equal(t, "u", analysis.GroupByColumns[0].TableAlias)
				assert.Equal(t, "name", analysis.GroupByColumns[0].ColumnName)
			},
		},
		{
			name: "window function with ROWS BETWEEN frame",
			sql:  "SELECT id, SUM(id) OVER (ORDER BY id ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 2)
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "string concatenation in output",
			sql:  "SELECT name || ' <' || email || '>' FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "arithmetic operations in output",
			sql:  "SELECT id, id * 2, id + 1, id - 1, id / 2, id % 3 FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 6)
			},
		},
		{
			name: "unary minus in expression",
			sql:  "SELECT -id FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "nested function calls",
			sql:  "SELECT LOWER(UPPER(name)) FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "table-valued function in FROM",
			sql:  "SELECT * FROM json_each('[1,2,3]') AS j",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.RawTableValuedFunctions)
				assert.Equal(t, "json_each", analysis.RawTableValuedFunctions[0].FunctionName)
			},
		},
		{
			name: "OR logical expression in WHERE",
			sql:  "SELECT id FROM users WHERE name = 'alice' OR name = 'bob'",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "NOT expression in WHERE",
			sql:  "SELECT id FROM users WHERE NOT email IS NULL",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "parenthesised expression",
			sql:  "SELECT id FROM users WHERE (id > 5) AND (name IS NOT NULL)",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "number literal types",
			sql:  "SELECT 42, 3.14, 1e10 FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 3)
			},
		},
		{
			name: "GLOB expression",
			sql:  "SELECT id FROM users WHERE name GLOB 'A*'",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "IN list with static values",
			sql:  "SELECT id FROM users WHERE id IN (1, 2, 3)",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "BETWEEN with static values",
			sql:  "SELECT id FROM users WHERE id BETWEEN 1 AND 10",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "FILTER clause on aggregate",
			sql:  "SELECT COUNT(*) FILTER (WHERE name IS NOT NULL) FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "multiple aggregate functions",
			sql:  "SELECT COUNT(*), MAX(id), MIN(id), SUM(id) FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 4)
			},
		},
		{
			name: "subquery in WHERE with IN",
			sql:  "SELECT id FROM users WHERE id IN (SELECT user_id FROM posts)",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "column with table alias in output",
			sql:  "SELECT u.id, u.name FROM users u WHERE u.id > 1",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 2)
				assert.Equal(t, "u", analysis.OutputColumns[0].TableAlias)
			},
		},
		{
			name: "USING clause in JOIN",
			sql:  "SELECT u.name FROM users u JOIN posts p USING (id)",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.JoinClauses)
			},
		},
		{
			name: "NULL expression",
			sql:  "SELECT COALESCE(email, NULL, 'none') FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "repeated named parameter",
			sql:  "SELECT id FROM users WHERE name = :name OR email = :name",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 2)

				assert.Equal(t, analysis.ParameterReferences[0].Number, analysis.ParameterReferences[1].Number)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			analysis := analyseQuery(t, catalogue, testCase.sql)
			testCase.assertions(t, analysis)
		})
	}
}

func TestAnalyseQuery_Insert(t *testing.T) {
	t.Parallel()

	catalogue := newSQLiteCatalogue()

	tests := []struct {
		assertions func(t *testing.T, analysis *ast.RawQueryAnalysis)
		name       string
		sql        string
	}{
		{
			name: "simple insert with anonymous parameters",
			sql:  "INSERT INTO users (name, email) VALUES (?, ?)",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)

				require.NotEmpty(t, analysis.FromTables)
				assert.Equal(t, "users", analysis.FromTables[0].Name)

				require.Len(t, analysis.ParameterReferences, 2)
				assert.Equal(t, 1, analysis.ParameterReferences[0].Number)
				assert.Equal(t, 2, analysis.ParameterReferences[1].Number)
				assert.Equal(t, sqlmodel.ParameterContextAssignment, analysis.ParameterReferences[0].Context)
				assert.Equal(t, sqlmodel.ParameterContextAssignment, analysis.ParameterReferences[1].Context)
			},
		},
		{
			name: "insert with RETURNING clause",
			sql:  "INSERT INTO users (name) VALUES (?) RETURNING id",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.True(t, analysis.HasReturning, "should detect RETURNING clause")

				require.NotEmpty(t, analysis.OutputColumns)
				assert.Equal(t, "id", analysis.OutputColumns[0].Name)
			},
		},
		{
			name: "INSERT OR REPLACE",
			sql:  "INSERT OR REPLACE INTO users (id, name) VALUES (?1, ?2)",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)

				require.NotEmpty(t, analysis.FromTables)
				assert.Equal(t, "users", analysis.FromTables[0].Name)

				require.Len(t, analysis.ParameterReferences, 2)
				assert.Equal(t, 1, analysis.ParameterReferences[0].Number)
				assert.Equal(t, 2, analysis.ParameterReferences[1].Number)
			},
		},
		{
			name: "INSERT with SELECT source",
			sql:  "INSERT INTO posts (user_id, title) SELECT id, name FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.FromTables)
				assert.Equal(t, "posts", analysis.FromTables[0].Name)
			},
		},
		{
			name: "INSERT with ON CONFLICT DO NOTHING",
			sql:  "INSERT INTO users (id, name) VALUES (?, ?) ON CONFLICT (id) DO NOTHING",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 2)
				require.NotEmpty(t, analysis.FromTables)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
			},
		},
		{
			name: "INSERT with ON CONFLICT DO UPDATE",
			sql:  "INSERT INTO users (id, name) VALUES (?, ?) ON CONFLICT (id) DO UPDATE SET name = ?",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 3)
			},
		},
		{
			name: "INSERT with CTE",
			sql:  "WITH new_users AS (SELECT id, name FROM users WHERE id > ?) INSERT INTO posts (user_id, title) SELECT id, name FROM new_users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.CTEDefinitions, 1)
				assert.Equal(t, "new_users", analysis.CTEDefinitions[0].Name)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			analysis := analyseQuery(t, catalogue, testCase.sql)
			testCase.assertions(t, analysis)
		})
	}
}

func TestAnalyseQuery_Update(t *testing.T) {
	t.Parallel()

	catalogue := newSQLiteCatalogue()

	tests := []struct {
		assertions func(t *testing.T, analysis *ast.RawQueryAnalysis)
		name       string
		sql        string
	}{
		{
			name: "simple update with SET and WHERE",
			sql:  "UPDATE users SET name = ? WHERE id = ?",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)

				require.NotEmpty(t, analysis.FromTables)
				assert.Equal(t, "users", analysis.FromTables[0].Name)

				require.Len(t, analysis.ParameterReferences, 2)
				assert.Equal(t, 1, analysis.ParameterReferences[0].Number)
				assert.Equal(t, 2, analysis.ParameterReferences[1].Number)

				assert.Equal(t, sqlmodel.ParameterContextAssignment, analysis.ParameterReferences[0].Context)

				assert.Equal(t, sqlmodel.ParameterContextComparison, analysis.ParameterReferences[1].Context)
			},
		},
		{
			name: "update with named parameters",
			sql:  "UPDATE users SET email = :email WHERE id = :id",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)

				require.NotEmpty(t, analysis.FromTables)
				assert.Equal(t, "users", analysis.FromTables[0].Name)

				require.Len(t, analysis.ParameterReferences, 2)
				assert.Equal(t, "email", analysis.ParameterReferences[0].Name)
				assert.Equal(t, "id", analysis.ParameterReferences[1].Name)
			},
		},
		{
			name: "update with RETURNING",
			sql:  "UPDATE posts SET title = ? WHERE id = ? RETURNING id, title",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)

				assert.True(t, analysis.HasReturning, "should detect RETURNING clause")
				require.Len(t, analysis.OutputColumns, 2)
				assert.Equal(t, "id", analysis.OutputColumns[0].Name)
				assert.Equal(t, "title", analysis.OutputColumns[1].Name)
			},
		},
		{
			name: "update with complex SET expression",
			sql:  "UPDATE users SET name = UPPER(name) WHERE id = ?",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.FromTables)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
				require.Len(t, analysis.ParameterReferences, 1)
			},
		},
		{
			name: "update with FROM clause",
			sql:  "UPDATE posts SET title = ? FROM users WHERE posts.user_id = users.id AND users.name = ?",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.FromTables, 2)
				assert.Equal(t, "posts", analysis.FromTables[0].Name)
				assert.Equal(t, "users", analysis.FromTables[1].Name)
			},
		},
		{
			name: "update multiple columns with SET",
			sql:  "UPDATE users SET name = ?, email = ? WHERE id = ?",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 3)
				assert.Equal(t, sqlmodel.ParameterContextAssignment, analysis.ParameterReferences[0].Context)
				assert.Equal(t, sqlmodel.ParameterContextAssignment, analysis.ParameterReferences[1].Context)
			},
		},
		{
			name: "update with CTE",
			sql:  "WITH old AS (SELECT id FROM users WHERE name = ?) UPDATE users SET name = ? WHERE id IN (SELECT id FROM old)",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.CTEDefinitions, 1)
				assert.Equal(t, "old", analysis.CTEDefinitions[0].Name)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			analysis := analyseQuery(t, catalogue, testCase.sql)
			testCase.assertions(t, analysis)
		})
	}
}

func TestAnalyseQuery_Delete(t *testing.T) {
	t.Parallel()

	catalogue := newSQLiteCatalogue()

	tests := []struct {
		assertions func(t *testing.T, analysis *ast.RawQueryAnalysis)
		name       string
		sql        string
	}{
		{
			name: "simple delete with WHERE parameter",
			sql:  "DELETE FROM users WHERE id = ?",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)

				require.NotEmpty(t, analysis.FromTables)
				assert.Equal(t, "users", analysis.FromTables[0].Name)

				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, 1, analysis.ParameterReferences[0].Number)
			},
		},
		{
			name: "delete with RETURNING clause",
			sql:  "DELETE FROM posts WHERE user_id = ? RETURNING id, title",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)

				require.NotEmpty(t, analysis.FromTables)
				assert.Equal(t, "posts", analysis.FromTables[0].Name)

				assert.True(t, analysis.HasReturning, "should detect RETURNING clause")
				require.Len(t, analysis.OutputColumns, 2)
				assert.Equal(t, "id", analysis.OutputColumns[0].Name)
				assert.Equal(t, "title", analysis.OutputColumns[1].Name)

				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, 1, analysis.ParameterReferences[0].Number)
			},
		},
		{
			name: "delete with named parameter",
			sql:  "DELETE FROM users WHERE email = :email",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)

				require.NotEmpty(t, analysis.FromTables)
				assert.Equal(t, "users", analysis.FromTables[0].Name)

				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, "email", analysis.ParameterReferences[0].Name)
			},
		},
		{
			name: "delete with ORDER BY and LIMIT",
			sql:  "DELETE FROM posts WHERE user_id = ? ORDER BY id LIMIT 10",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.FromTables)
				assert.Equal(t, "posts", analysis.FromTables[0].Name)
				require.Len(t, analysis.ParameterReferences, 1)
			},
		},
		{
			name: "delete with CTE",
			sql:  "WITH old_posts AS (SELECT id FROM posts WHERE user_id = ?) DELETE FROM posts WHERE id IN (SELECT id FROM old_posts)",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.CTEDefinitions, 1)
				assert.Equal(t, "old_posts", analysis.CTEDefinitions[0].Name)
			},
		},
		{
			name: "delete with multiple conditions",
			sql:  "DELETE FROM users WHERE name = ? AND email IS NULL",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 1)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			analysis := analyseQuery(t, catalogue, testCase.sql)
			testCase.assertions(t, analysis)
		})
	}
}

func TestAnalyseQuery_Values(t *testing.T) {
	t.Parallel()

	catalogue := newSQLiteCatalogue()

	t.Run("simple VALUES statement", func(t *testing.T) {
		t.Parallel()
		analysis := analyseQuery(t, catalogue, "VALUES (1, 'hello'), (2, 'world')")
		require.NotNil(t, analysis)
		assert.True(t, analysis.ReadOnly, "VALUES should be read-only")
		require.Len(t, analysis.OutputColumns, 2)
	})

	t.Run("VALUES with parameters", func(t *testing.T) {
		t.Parallel()
		analysis := analyseQuery(t, catalogue, "VALUES (?1, ?2)")
		require.NotNil(t, analysis)
		require.Len(t, analysis.ParameterReferences, 2)
	})
}

func TestAnalyseQuery_SelectExpressions(t *testing.T) {
	t.Parallel()

	catalogue := newSQLiteCatalogue()

	tests := []struct {
		assertions func(t *testing.T, analysis *ast.RawQueryAnalysis)
		name       string
		sql        string
	}{
		{
			name: "IN list with column context propagated to parameters",
			sql:  "SELECT id FROM users WHERE id IN (:a, :b)",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 2)
				assert.Equal(t, sqlmodel.ParameterContextInList, analysis.ParameterReferences[0].Context)
				assert.Equal(t, sqlmodel.ParameterContextInList, analysis.ParameterReferences[1].Context)
				require.NotNil(t, analysis.ParameterReferences[0].ColumnReference)
				assert.Equal(t, "id", analysis.ParameterReferences[0].ColumnReference.ColumnName)
			},
		},
		{
			name: "BETWEEN expression in SELECT list with column context",
			sql:  "SELECT id BETWEEN 1 AND 10 FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "IN list expression in SELECT list",
			sql:  "SELECT id IN (1, 2, 3) FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "EXISTS subquery parsed as expression",
			sql:  "SELECT EXISTS (SELECT 1 FROM users WHERE id = 1)",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "comparison operators in expression tree",
			sql:  "SELECT id FROM users WHERE id > 1 AND id < 100 AND name <> 'test' AND id >= 5 AND id <= 50 AND id != 42",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "bitwise operators in expression",
			sql:  "SELECT id & 0xFF, id | 0x01, id << 2, id >> 1 FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 4)
			},
		},
		{
			name: "JSON arrow operators",
			sql:  "SELECT id, '{}' -> 'key', '{}' ->> 'nested' FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 3)
			},
		},
		{
			name: "NOT expression wrapping comparison",
			sql:  "SELECT id FROM users WHERE NOT id > 10",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "OR expression joining AND expressions",
			sql:  "SELECT id FROM users WHERE (id = 1 AND name = 'a') OR (id = 2 AND name = 'b')",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "COLLATE suffix on column",
			sql:  "SELECT name COLLATE NOCASE FROM users ORDER BY name COLLATE NOCASE",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "blob literal in expression",
			sql:  "SELECT X'DEADBEEF' FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "REGEXP expression",
			sql:  "SELECT id FROM users WHERE name REGEXP '^[A-Z]'",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "MATCH expression",
			sql:  "SELECT id FROM users WHERE name MATCH 'pattern'",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "window function with RANGE BETWEEN frame",
			sql:  "SELECT id, SUM(id) OVER (ORDER BY id RANGE BETWEEN 1 PRECEDING AND 1 FOLLOWING) FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 2)
			},
		},
		{
			name: "window function with GROUPS frame",
			sql:  "SELECT id, SUM(id) OVER (ORDER BY id GROUPS BETWEEN CURRENT ROW AND UNBOUNDED FOLLOWING) FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 2)
			},
		},
		{
			name: "window function without parenthesised spec",
			sql:  "SELECT id, ROW_NUMBER() OVER w FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 2)
			},
		},
		{
			name: "function with ORDER BY in arguments",
			sql:  "SELECT GROUP_CONCAT(name ORDER BY name ASC) FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "function with DISTINCT keyword",
			sql:  "SELECT COUNT(DISTINCT name) FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "unary tilde operator",
			sql:  "SELECT ~id FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "unary plus operator",
			sql:  "SELECT +id FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "qualified column with alias in qualified select item",
			sql:  "SELECT u.name AS user_name, u.id user_id FROM users u",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 2)
				assert.Equal(t, "user_name", analysis.OutputColumns[0].Name)
				assert.Equal(t, "user_id", analysis.OutputColumns[1].Name)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			analysis := analyseQuery(t, catalogue, testCase.sql)
			testCase.assertions(t, analysis)
		})
	}
}

func TestAnalyseQuery_OrderByLimitOffset(t *testing.T) {
	t.Parallel()

	catalogue := newSQLiteCatalogue()

	tests := []struct {
		assertions func(t *testing.T, analysis *ast.RawQueryAnalysis)
		name       string
		sql        string
	}{
		{
			name: "ORDER BY with parameters in expressions",
			sql:  "SELECT id FROM users ORDER BY id LIMIT ?",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextLimit, analysis.ParameterReferences[0].Context)
			},
		},
		{
			name: "LIMIT with comma-separated OFFSET parameter",
			sql:  "SELECT id FROM users LIMIT ?, ?",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 2)
				assert.Equal(t, sqlmodel.ParameterContextLimit, analysis.ParameterReferences[0].Context)
				assert.Equal(t, sqlmodel.ParameterContextOffset, analysis.ParameterReferences[1].Context)
			},
		},
		{
			name: "ORDER BY terminates on LIMIT keyword",
			sql:  "SELECT id FROM users ORDER BY name, id LIMIT 10",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "ORDER BY terminates on RETURNING keyword",
			sql:  "DELETE FROM users WHERE id = 1 ORDER BY id LIMIT 5",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.FromTables)
			},
		},
		{
			name: "ORDER BY with parameter in expression",
			sql:  "SELECT id FROM users ORDER BY id LIMIT ? OFFSET ?",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 2)
				assert.Equal(t, sqlmodel.ParameterContextLimit, analysis.ParameterReferences[0].Context)
				assert.Equal(t, sqlmodel.ParameterContextOffset, analysis.ParameterReferences[1].Context)
			},
		},
		{
			name: "ORDER BY with parenthesised sub-expression",
			sql:  "SELECT id FROM users ORDER BY (id + 1) DESC LIMIT 5",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.True(t, analysis.ReadOnly)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			analysis := analyseQuery(t, catalogue, testCase.sql)
			testCase.assertions(t, analysis)
		})
	}
}

func TestAnalyseQuery_Insert_ConflictAndSources(t *testing.T) {
	t.Parallel()

	catalogue := newSQLiteCatalogue()

	tests := []struct {
		assertions func(t *testing.T, analysis *ast.RawQueryAnalysis)
		name       string
		sql        string
	}{
		{
			name: "INSERT DEFAULT VALUES",
			sql:  "INSERT INTO users DEFAULT VALUES",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.FromTables)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
			},
		},
		{
			name: "INSERT with ON CONFLICT DO UPDATE SET with WHERE",
			sql:  "INSERT INTO users (id, name) VALUES (?, ?) ON CONFLICT (id) DO UPDATE SET name = ? WHERE name <> ?",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 4)
			},
		},
		{
			name: "INSERT with SELECT source containing parameter",
			sql:  "INSERT INTO posts (user_id, title) SELECT id, name FROM users WHERE id > ?",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.FromTables)
				assert.Equal(t, "posts", analysis.FromTables[0].Name)
				require.Len(t, analysis.ParameterReferences, 1)
			},
		},
		{

			name: "INSERT SELECT retains source FROM in InsertSelect",
			sql:  "INSERT INTO posts (user_id, title) SELECT id, name FROM users WHERE id > ?",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotNil(t, analysis.InsertSelect)
				require.NotEmpty(t, analysis.InsertSelect.FromTables)
				assert.Equal(t, "users", analysis.InsertSelect.FromTables[0].Name)
				require.Len(t, analysis.InsertSelect.ParameterReferences, 1)
			},
		},
		{

			name: "INSERT SELECT with JOIN retains joins and where params",
			sql: "INSERT INTO posts (user_id, title) " +
				"SELECT u.id, o.name FROM users u INNER JOIN orgs o ON o.id = u.org_id " +
				"WHERE u.id > ? AND o.active = ? " +
				"ON CONFLICT (user_id) DO NOTHING",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotNil(t, analysis.InsertSelect)
				require.NotEmpty(t, analysis.InsertSelect.FromTables)
				assert.Equal(t, "users", analysis.InsertSelect.FromTables[0].Name)
				require.NotEmpty(t, analysis.InsertSelect.JoinClauses)
				assert.Equal(t, "orgs", analysis.InsertSelect.JoinClauses[0].Table.Name)
				require.Len(t, analysis.InsertSelect.ParameterReferences, 2)
			},
		},
		{
			name: "INSERT with multiple value rows",
			sql:  "INSERT INTO users (name, email) VALUES (?, ?), (?, ?)",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 4)
			},
		},
		{
			name: "REPLACE INTO",
			sql:  "REPLACE INTO users (id, name) VALUES (?, ?)",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.FromTables)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
			},
		},
		{
			name: "INSERT with RETURNING star",
			sql:  "INSERT INTO users (name) VALUES (?) RETURNING *",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.True(t, analysis.HasReturning)
				require.Len(t, analysis.OutputColumns, 1)
				assert.True(t, analysis.OutputColumns[0].IsStar)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			analysis := analyseQuery(t, catalogue, testCase.sql)
			testCase.assertions(t, analysis)
		})
	}
}

func TestAnalyseQuery_Update_ConflictAndSubquery(t *testing.T) {
	t.Parallel()

	catalogue := newSQLiteCatalogue()

	tests := []struct {
		assertions func(t *testing.T, analysis *ast.RawQueryAnalysis)
		name       string
		sql        string
	}{
		{
			name: "UPDATE OR IGNORE",
			sql:  "UPDATE OR IGNORE users SET name = ? WHERE id = ?",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.FromTables)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
				require.Len(t, analysis.ParameterReferences, 2)
			},
		},
		{
			name: "UPDATE with subquery in SET expression",
			sql:  "UPDATE users SET name = (SELECT 'updated') WHERE id = ?",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 1)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			analysis := analyseQuery(t, catalogue, testCase.sql)
			testCase.assertions(t, analysis)
		})
	}
}

func TestAnalyseQuery_Delete_LimitAndReturning(t *testing.T) {
	t.Parallel()

	catalogue := newSQLiteCatalogue()

	tests := []struct {
		assertions func(t *testing.T, analysis *ast.RawQueryAnalysis)
		name       string
		sql        string
	}{
		{
			name: "DELETE with ORDER BY, LIMIT, and parameter",
			sql:  "DELETE FROM users ORDER BY id LIMIT ?",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextLimit, analysis.ParameterReferences[0].Context)
			},
		},
		{
			name: "DELETE with RETURNING star",
			sql:  "DELETE FROM users WHERE id = ? RETURNING *",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.True(t, analysis.HasReturning)
				require.Len(t, analysis.OutputColumns, 1)
				assert.True(t, analysis.OutputColumns[0].IsStar)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			analysis := analyseQuery(t, catalogue, testCase.sql)
			testCase.assertions(t, analysis)
		})
	}
}

func TestAnalyseQuery_ValuesTrailingRows(t *testing.T) {
	t.Parallel()

	catalogue := newSQLiteCatalogue()

	tests := []struct {
		assertions func(t *testing.T, analysis *ast.RawQueryAnalysis)
		name       string
		sql        string
	}{
		{
			name: "VALUES with trailing row parameters",
			sql:  "VALUES (1, 'first'), (?, ?)",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.ParameterReferences, 2)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			analysis := analyseQuery(t, catalogue, testCase.sql)
			testCase.assertions(t, analysis)
		})
	}
}

func TestAnalyseQuery_FromClauseVariants(t *testing.T) {
	t.Parallel()

	catalogue := newSQLiteCatalogue()

	tests := []struct {
		assertions func(t *testing.T, analysis *ast.RawQueryAnalysis)
		name       string
		sql        string
	}{
		{
			name: "derived table without explicit alias",
			sql:  "SELECT * FROM (SELECT id FROM users) sub",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.RawDerivedTables)
				assert.Equal(t, "sub", analysis.RawDerivedTables[0].Alias)
			},
		},
		{
			name: "table-valued function without explicit alias",
			sql:  "SELECT * FROM json_each('[1,2,3]')",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.RawTableValuedFunctions)
				assert.Equal(t, "json_each", analysis.RawTableValuedFunctions[0].FunctionName)
			},
		},
		{
			name: "schema-qualified table reference",
			sql:  "SELECT id FROM main.users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.FromTables, 1)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
			},
		},
		{
			name: "NATURAL JOIN",
			sql:  "SELECT * FROM users NATURAL JOIN posts",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.JoinClauses, 1)
				assert.Equal(t, ast.JoinInner, analysis.JoinClauses[0].Kind)
			},
		},
		{
			name: "INNER JOIN explicit keyword",
			sql:  "SELECT * FROM users INNER JOIN posts ON posts.user_id = users.id",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.JoinClauses, 1)
				assert.Equal(t, ast.JoinInner, analysis.JoinClauses[0].Kind)
			},
		},
		{
			name: "LEFT OUTER JOIN with OUTER keyword",
			sql:  "SELECT * FROM users LEFT OUTER JOIN posts ON posts.user_id = users.id",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.JoinClauses, 1)
				assert.Equal(t, ast.JoinLeft, analysis.JoinClauses[0].Kind)
			},
		},
		{
			name: "join with table-valued function target",
			sql:  "SELECT * FROM users JOIN json_each('[1]') ON 1=1",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.RawTableValuedFunctions)
			},
		},
		{
			name: "join with derived table target",
			sql:  "SELECT * FROM users JOIN (SELECT id FROM users) d ON d.id = users.id",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.RawDerivedTables)
			},
		},
		{
			name: "table alias without AS keyword",
			sql:  "SELECT t.id FROM users t WHERE t.id > 0",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.FromTables, 1)
				assert.Equal(t, "t", analysis.FromTables[0].Alias)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			analysis := analyseQuery(t, catalogue, testCase.sql)
			testCase.assertions(t, analysis)
		})
	}
}

func TestAnalyseQuery_ParameterContextDetection(t *testing.T) {
	t.Parallel()

	catalogue := newSQLiteCatalogue()

	tests := []struct {
		assertions func(t *testing.T, analysis *ast.RawQueryAnalysis)
		name       string
		sql        string
	}{
		{
			name: "parameter in CAST via detectParameterContext",
			sql:  "SELECT id FROM users WHERE id IN (CAST(? AS INTEGER))",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 1)

				assert.Equal(t, sqlmodel.ParameterContextCast, analysis.ParameterReferences[0].Context)
			},
		},
		{
			name: "parameter in function argument via detectParameterContext path",
			sql:  "SELECT id FROM users WHERE LENGTH(?) > 0",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextFunctionArgument, analysis.ParameterReferences[0].Context)
			},
		},
		{
			name: "parameter in IN list via detectParameterContext",
			sql:  "SELECT id FROM users WHERE id IN (?, ?, ?)",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 3)
				for _, reference := range analysis.ParameterReferences {
					assert.Equal(t, sqlmodel.ParameterContextInList, reference.Context)
				}
			},
		},
		{
			name: "comparison with table-qualified column",
			sql:  "SELECT id FROM users u WHERE u.id = ?",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextComparison, analysis.ParameterReferences[0].Context)
				require.NotNil(t, analysis.ParameterReferences[0].ColumnReference)
				assert.Equal(t, "u", analysis.ParameterReferences[0].ColumnReference.TableAlias)
				assert.Equal(t, "id", analysis.ParameterReferences[0].ColumnReference.ColumnName)
			},
		},
		{
			name: "LIKE pattern with direct column LHS",
			sql:  "SELECT id FROM users WHERE name LIKE ?",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextLike, analysis.ParameterReferences[0].Context)
				require.NotNil(t, analysis.ParameterReferences[0].ColumnReference)
				assert.Equal(t, "name", analysis.ParameterReferences[0].ColumnReference.ColumnName)
			},
		},
		{
			name: "LIKE pattern wrapped in concat picks LHS column",
			sql:  "SELECT id FROM users WHERE name LIKE ('%' || ? || '%')",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextLike, analysis.ParameterReferences[0].Context)
				require.NotNil(t, analysis.ParameterReferences[0].ColumnReference)
				assert.Equal(t, "name", analysis.ParameterReferences[0].ColumnReference.ColumnName)
			},
		},
		{
			name: "NOT LIKE pattern still binds to LHS column",
			sql:  "SELECT id FROM users WHERE name NOT LIKE ?",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextLike, analysis.ParameterReferences[0].Context)
				require.NotNil(t, analysis.ParameterReferences[0].ColumnReference)
				assert.Equal(t, "name", analysis.ParameterReferences[0].ColumnReference.ColumnName)
			},
		},
		{
			name: "GLOB and REGEXP and MATCH all classify as Like context",
			sql:  "SELECT id FROM files WHERE name GLOB ? AND path REGEXP ? AND content MATCH ?",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 3)
				expected := []string{"name", "path", "content"}
				for index, reference := range analysis.ParameterReferences {
					assert.Equal(t, sqlmodel.ParameterContextLike, reference.Context, "param %d context", index+1)
					require.NotNil(t, reference.ColumnReference, "param %d column ref", index+1)
					assert.Equal(t, expected[index], reference.ColumnReference.ColumnName, "param %d column name", index+1)
				}
			},
		},
		{
			name: "LIKE with concat-expression LHS picks first column",
			sql:  "SELECT id FROM users WHERE (name || ' ' || role) LIKE ?",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextLike, analysis.ParameterReferences[0].Context)
				require.NotNil(t, analysis.ParameterReferences[0].ColumnReference)
				assert.Equal(t, "name", analysis.ParameterReferences[0].ColumnReference.ColumnName)
			},
		},
		{
			name: "LIKE with CAST/COALESCE LHS picks the inner column",
			sql:  "SELECT id FROM customers WHERE CAST(COALESCE(json_extract(company_contacts, '$[0].name'), '') AS TEXT) LIKE ('%' || ? || '%')",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextLike, analysis.ParameterReferences[0].Context)
				require.NotNil(t, analysis.ParameterReferences[0].ColumnReference)
				assert.Equal(t, "company_contacts", analysis.ParameterReferences[0].ColumnReference.ColumnName)
			},
		},
		{
			name: "LIKE with function-wrapped column picks the column",
			sql:  "SELECT id FROM users WHERE LOWER(name) LIKE ?",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextLike, analysis.ParameterReferences[0].Context)
				require.NotNil(t, analysis.ParameterReferences[0].ColumnReference)
				assert.Equal(t, "name", analysis.ParameterReferences[0].ColumnReference.ColumnName)
			},
		},
		{
			name: "LIKE with table-qualified LHS preserves the alias",
			sql:  "SELECT id FROM users u WHERE u.email LIKE ?",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextLike, analysis.ParameterReferences[0].Context)
				require.NotNil(t, analysis.ParameterReferences[0].ColumnReference)
				assert.Equal(t, "u", analysis.ParameterReferences[0].ColumnReference.TableAlias)
				assert.Equal(t, "email", analysis.ParameterReferences[0].ColumnReference.ColumnName)
			},
		},
		{
			name: "two LIKE patterns on different columns each bind to their own LHS",
			sql:  "SELECT id FROM users WHERE name LIKE ? OR email LIKE ?",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 2)
				assert.Equal(t, sqlmodel.ParameterContextLike, analysis.ParameterReferences[0].Context)
				require.NotNil(t, analysis.ParameterReferences[0].ColumnReference)
				assert.Equal(t, "name", analysis.ParameterReferences[0].ColumnReference.ColumnName)
				assert.Equal(t, sqlmodel.ParameterContextLike, analysis.ParameterReferences[1].Context)
				require.NotNil(t, analysis.ParameterReferences[1].ColumnReference)
				assert.Equal(t, "email", analysis.ParameterReferences[1].ColumnReference.ColumnName)
			},
		},
		{
			name: "comparison and LIKE in same predicate stay distinct",
			sql:  "SELECT id FROM users WHERE id = ? AND name LIKE ?",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 2)
				assert.Equal(t, sqlmodel.ParameterContextComparison, analysis.ParameterReferences[0].Context)
				require.NotNil(t, analysis.ParameterReferences[0].ColumnReference)
				assert.Equal(t, "id", analysis.ParameterReferences[0].ColumnReference.ColumnName)
				assert.Equal(t, sqlmodel.ParameterContextLike, analysis.ParameterReferences[1].Context)
				require.NotNil(t, analysis.ParameterReferences[1].ColumnReference)
				assert.Equal(t, "name", analysis.ParameterReferences[1].ColumnReference.ColumnName)
			},
		},
		{
			name: "LIKE pattern with ESCAPE clause: pattern is Like, escape char is not",
			sql:  "SELECT id FROM users WHERE name LIKE ? ESCAPE ?",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 2)
				assert.Equal(t, sqlmodel.ParameterContextLike, analysis.ParameterReferences[0].Context,
					"first param (the pattern) should be Like")
				require.NotNil(t, analysis.ParameterReferences[0].ColumnReference)
				assert.Equal(t, "name", analysis.ParameterReferences[0].ColumnReference.ColumnName)
				assert.NotEqual(t, sqlmodel.ParameterContextLike, analysis.ParameterReferences[1].Context,
					"second param (the escape character) must not be classified as Like")
			},
		},
		{

			name: "table-qualified column IS parameter attaches column ref",
			sql:  "SELECT id FROM users u WHERE u.id IS ?",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextComparison, analysis.ParameterReferences[0].Context)
				require.NotNil(t, analysis.ParameterReferences[0].ColumnReference)
				assert.Equal(t, "u", analysis.ParameterReferences[0].ColumnReference.TableAlias)
				assert.Equal(t, "id", analysis.ParameterReferences[0].ColumnReference.ColumnName)
			},
		},
		{

			name: "bare column IS parameter attaches column ref",
			sql:  "SELECT id FROM users WHERE name IS ?",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextComparison, analysis.ParameterReferences[0].Context)
				require.NotNil(t, analysis.ParameterReferences[0].ColumnReference)
				assert.Equal(t, "", analysis.ParameterReferences[0].ColumnReference.TableAlias)
				assert.Equal(t, "name", analysis.ParameterReferences[0].ColumnReference.ColumnName)
			},
		},
		{

			name: "column IS NOT parameter attaches column ref",
			sql:  "SELECT id FROM users u WHERE u.email IS NOT ?",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextComparison, analysis.ParameterReferences[0].Context)
				require.NotNil(t, analysis.ParameterReferences[0].ColumnReference)
				assert.Equal(t, "email", analysis.ParameterReferences[0].ColumnReference.ColumnName)
			},
		},
		{

			name: "column IS NULL registers no parameter",
			sql:  "SELECT id FROM users WHERE email IS NULL",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.Empty(t, analysis.ParameterReferences)
			},
		},
		{

			name: "column IS NOT NULL registers no parameter",
			sql:  "SELECT id FROM users WHERE email IS NOT NULL",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.Empty(t, analysis.ParameterReferences)
			},
		},
		{

			name: "scalar builtin function argument records name and ordinal",
			sql:  "SELECT id FROM users WHERE name = substr(?, 1, ?)",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 2)

				assert.Equal(t, sqlmodel.ParameterContextFunctionArgument, analysis.ParameterReferences[0].Context)
				assert.Equal(t, "substr", analysis.ParameterReferences[0].EnclosingFunctionName)
				assert.Equal(t, 0, analysis.ParameterReferences[0].ArgumentOrdinal)

				assert.Equal(t, sqlmodel.ParameterContextFunctionArgument, analysis.ParameterReferences[1].Context)
				assert.Equal(t, "substr", analysis.ParameterReferences[1].EnclosingFunctionName)
				assert.Equal(t, 2, analysis.ParameterReferences[1].ArgumentOrdinal,
					"a non-placeholder argument (the literal 1) still consumes an ordinal slot")
			},
		},
		{

			name: "nested function calls isolate argument ordinals",
			sql:  "SELECT id FROM users WHERE name = replace(substr(?, ?), ?, name)",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 3)

				assert.Equal(t, "substr", analysis.ParameterReferences[0].EnclosingFunctionName)
				assert.Equal(t, 0, analysis.ParameterReferences[0].ArgumentOrdinal)
				assert.Equal(t, "substr", analysis.ParameterReferences[1].EnclosingFunctionName)
				assert.Equal(t, 1, analysis.ParameterReferences[1].ArgumentOrdinal)
				assert.Equal(t, "replace", analysis.ParameterReferences[2].EnclosingFunctionName)
				assert.Equal(t, 1, analysis.ParameterReferences[2].ArgumentOrdinal)
			},
		},
		{

			name: "flat-scan function argument records name and ordinal",
			sql:  "SELECT id FROM users WHERE length(?) > 0",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextFunctionArgument, analysis.ParameterReferences[0].Context)
				assert.Equal(t, "length", analysis.ParameterReferences[0].EnclosingFunctionName)
				assert.Equal(t, 0, analysis.ParameterReferences[0].ArgumentOrdinal)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			analysis := analyseQuery(t, catalogue, testCase.sql)
			testCase.assertions(t, analysis)
		})
	}
}

func TestAnalyseQuery_FromTVFArgumentMetadata(t *testing.T) {
	t.Parallel()

	catalogue := newSQLiteCatalogue()

	analysis := analyseQuery(t, catalogue, "SELECT value FROM get_pages(?3, ?1, ?2)")

	require.NotNil(t, analysis)
	require.NotEmpty(t, analysis.RawTableValuedFunctions)
	assert.Equal(t, "get_pages", analysis.RawTableValuedFunctions[0].FunctionName)
	require.Len(t, analysis.ParameterReferences, 3)

	bySlot := map[int]ast.RawParameterReference{}
	for _, reference := range analysis.ParameterReferences {
		assert.Equal(t, sqlmodel.ParameterContextFunctionArgument, reference.Context)
		assert.Equal(t, "get_pages", reference.EnclosingFunctionName)
		bySlot[reference.ArgumentOrdinal] = reference
	}

	require.Len(t, bySlot, 3, "the three placeholders must occupy three distinct ordinal slots")
	assert.Equal(t, 3, bySlot[0].Number, "slot 0 holds ?3")
	assert.Equal(t, 1, bySlot[1].Number, "slot 1 holds ?1")
	assert.Equal(t, 2, bySlot[2].Number, "slot 2 holds ?2")
}

func TestAnalyseQuery_InsertSelectProjectionBinding(t *testing.T) {
	t.Parallel()

	catalogue := newSQLiteCatalogue()

	analysis := analyseQuery(t, catalogue,
		"INSERT INTO posts (user_id, title) SELECT ?, ? FROM users")

	require.NotNil(t, analysis)
	require.Len(t, analysis.ParameterReferences, 2)

	assert.Equal(t, sqlmodel.ParameterContextAssignment, analysis.ParameterReferences[0].Context)
	require.NotNil(t, analysis.ParameterReferences[0].ColumnReference)
	assert.Equal(t, "posts", analysis.ParameterReferences[0].ColumnReference.TableAlias)
	assert.Equal(t, "user_id", analysis.ParameterReferences[0].ColumnReference.ColumnName)

	assert.Equal(t, sqlmodel.ParameterContextAssignment, analysis.ParameterReferences[1].Context)
	require.NotNil(t, analysis.ParameterReferences[1].ColumnReference)
	assert.Equal(t, "posts", analysis.ParameterReferences[1].ColumnReference.TableAlias)
	assert.Equal(t, "title", analysis.ParameterReferences[1].ColumnReference.ColumnName)
}

func TestAnalyseQuery_WithClassification(t *testing.T) {
	t.Parallel()

	catalogue := newSQLiteCatalogue()

	tests := []struct {
		assertions func(t *testing.T, analysis *ast.RawQueryAnalysis)
		name       string
		sql        string
	}{
		{
			name: "WITH INSERT classifies as insert",
			sql:  "WITH src AS (SELECT id, name FROM users) INSERT INTO posts (user_id, title) SELECT id, name FROM src",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.CTEDefinitions)
			},
		},
		{
			name: "WITH UPDATE classifies as update",
			sql:  "WITH src AS (SELECT id FROM users) UPDATE users SET name = 'x' WHERE id IN (SELECT id FROM src)",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.CTEDefinitions)
			},
		},
		{
			name: "WITH DELETE classifies as delete",
			sql:  "WITH old AS (SELECT id FROM users WHERE id < 5) DELETE FROM users WHERE id IN (SELECT id FROM old)",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.CTEDefinitions)
			},
		},
		{
			name: "RECURSIVE CTE",
			sql:  "WITH RECURSIVE cnt(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM cnt WHERE x < 10) SELECT x FROM cnt",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.CTEDefinitions, 1)
				assert.True(t, analysis.CTEDefinitions[0].IsRecursive)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			analysis := analyseQuery(t, catalogue, testCase.sql)
			testCase.assertions(t, analysis)
		})
	}
}

func TestParseStatements_Multiple(t *testing.T) {
	t.Parallel()

	engine := New()
	statements, err := engine.ParseStatements(t.Context(), "SELECT 1; SELECT 2")
	require.NoError(t, err)
	require.Len(t, statements, 2, "should split multiple statements on semicolons")
}

func TestIsParsedStatement(t *testing.T) {
	t.Parallel()

	statement := &parsedStatement{
		kind: statementKindSelect,
	}

	statement.IsParsedStatement()
}

func TestClassifyStatement(t *testing.T) {
	t.Parallel()

	engine := New()

	tests := []struct {
		name     string
		sql      string
		wantKind statementKind
	}{
		{
			name:     "CREATE TEMP TABLE classifies correctly",
			sql:      "CREATE TEMP TABLE t (id INTEGER)",
			wantKind: statementKindCreateTable,
		},
		{
			name:     "CREATE TEMPORARY VIEW classifies correctly",
			sql:      "CREATE TEMPORARY VIEW v AS SELECT 1",
			wantKind: statementKindCreateView,
		},
		{
			name:     "CREATE TEMP TRIGGER classifies correctly",
			sql:      "CREATE TEMP TRIGGER tr BEFORE INSERT ON t BEGIN SELECT 1; END",
			wantKind: statementKindCreateTrigger,
		},
		{
			name:     "CREATE UNIQUE INDEX classifies correctly",
			sql:      "CREATE UNIQUE INDEX idx ON t (c)",
			wantKind: statementKindCreateIndex,
		},
		{
			name:     "DROP VIEW classifies correctly",
			sql:      "DROP VIEW v",
			wantKind: statementKindDropView,
		},
		{
			name:     "DROP TRIGGER classifies correctly",
			sql:      "DROP TRIGGER tr",
			wantKind: statementKindDropTrigger,
		},
		{
			name:     "DROP INDEX classifies correctly",
			sql:      "DROP INDEX idx",
			wantKind: statementKindDropIndex,
		},
		{
			name:     "REPLACE classifies as insert",
			sql:      "REPLACE INTO t (id) VALUES (1)",
			wantKind: statementKindInsert,
		},
		{
			name:     "pragma statement",
			sql:      "PRAGMA table_info(users)",
			wantKind: statementKindPragma,
		},
		{
			name:     "unknown statement classifies as unknown",
			sql:      "VACUUM",
			wantKind: statementKindUnknown,
		},
		{
			name:     "CREATE with unknown second token classifies as unknown",
			sql:      "CREATE SOMETHING t",
			wantKind: statementKindUnknown,
		},
		{
			name:     "DROP with unknown second token classifies as unknown",
			sql:      "DROP SOMETHING t",
			wantKind: statementKindUnknown,
		},
		{
			name:     "WITH VALUES classifies as values",
			sql:      "WITH cte AS (SELECT 1) VALUES (1)",
			wantKind: statementKindValues,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			require.NotEmpty(t, statements)

			parsed, ok := statements[0].Raw.(*parsedStatement)
			require.True(t, ok)
			assert.Equal(t, testCase.wantKind, parsed.kind)
		})
	}
}

func TestAnalyseQuery_Select_NotExpressions(t *testing.T) {
	t.Parallel()

	catalogue := newSQLiteCatalogue()

	tests := []struct {
		assertions func(t *testing.T, analysis *ast.RawQueryAnalysis)
		name       string
		sql        string
	}{
		{
			name: "NOT IN in WHERE clause",
			sql:  "SELECT id, name FROM users WHERE id NOT IN (1, 2, 3)",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.OutputColumns, 2)
				assert.Equal(t, "id", analysis.OutputColumns[0].ColumnName)
				assert.Equal(t, "name", analysis.OutputColumns[1].ColumnName)
				require.Len(t, analysis.FromTables, 1)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "NOT IN in SELECT list",
			sql:  "SELECT id NOT IN (1, 2, 3) AS excluded FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.OutputColumns, 1)
				assert.Equal(t, "excluded", analysis.OutputColumns[0].Name)
				require.Len(t, analysis.FromTables, 1)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
			},
		},
		{
			name: "NOT BETWEEN in WHERE clause",
			sql:  "SELECT id FROM users WHERE id NOT BETWEEN 10 AND 20",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.OutputColumns, 1)
				require.Len(t, analysis.FromTables, 1)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "NOT BETWEEN in SELECT list",
			sql:  "SELECT id NOT BETWEEN 10 AND 20 AS outside_range FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.OutputColumns, 1)
				assert.Equal(t, "outside_range", analysis.OutputColumns[0].Name)
			},
		},
		{
			name: "NOT LIKE in WHERE clause",
			sql:  "SELECT id, name FROM users WHERE name NOT LIKE '%test%'",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.OutputColumns, 2)
				require.Len(t, analysis.FromTables, 1)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
			},
		},
		{
			name: "NOT LIKE in SELECT list",
			sql:  "SELECT name NOT LIKE '%admin%' AS is_regular FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.OutputColumns, 1)
				assert.Equal(t, "is_regular", analysis.OutputColumns[0].Name)
			},
		},
		{
			name: "NOT GLOB in WHERE clause",
			sql:  "SELECT id FROM users WHERE name NOT GLOB 'A*'",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.OutputColumns, 1)
				require.Len(t, analysis.FromTables, 1)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
			},
		},
		{
			name: "NOT GLOB in SELECT list",
			sql:  "SELECT name NOT GLOB 'A*' AS not_a_name FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.OutputColumns, 1)
				assert.Equal(t, "not_a_name", analysis.OutputColumns[0].Name)
			},
		},
		{
			name: "NOT IN inside CASE inside aggregate with GROUP BY",
			sql:  "SELECT user_id, COUNT(*) AS total, SUM(CASE WHEN title NOT IN ('draft', 'deleted') THEN 1 ELSE 0 END) AS active_count FROM posts GROUP BY user_id",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.OutputColumns, 3, "all three output columns must be preserved")
				assert.Equal(t, "user_id", analysis.OutputColumns[0].ColumnName)
				assert.Equal(t, "total", analysis.OutputColumns[1].Name)
				assert.Equal(t, "active_count", analysis.OutputColumns[2].Name)
				require.Len(t, analysis.FromTables, 1)
				assert.Equal(t, "posts", analysis.FromTables[0].Name)
				require.Len(t, analysis.GroupByColumns, 1)
				assert.Equal(t, "user_id", analysis.GroupByColumns[0].ColumnName)
			},
		},
		{
			name: "multiple NOT expressions combined",
			sql:  "SELECT id FROM users WHERE name NOT LIKE '%test%' AND id NOT IN (1, 2) AND id NOT BETWEEN 100 AND 200",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.OutputColumns, 1)
				require.Len(t, analysis.FromTables, 1)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, catalogue, testCase.sql)
			require.NotNil(t, analysis)
			testCase.assertions(t, analysis)
		})
	}
}

func TestAnalyseQuery_HasWhereClause(t *testing.T) {
	t.Parallel()

	catalogue := newSQLiteCatalogue()

	tests := []struct {
		name string
		sql  string
		want bool
	}{
		{name: "SELECT with WHERE", sql: "SELECT id FROM users WHERE id = ?", want: true},
		{name: "SELECT without WHERE", sql: "SELECT id FROM users", want: false},
		{name: "UPDATE with WHERE", sql: "UPDATE users SET name = ? WHERE id = ?", want: true},
		{name: "UPDATE without WHERE", sql: "UPDATE users SET name = ?", want: false},
		{name: "DELETE with WHERE", sql: "DELETE FROM users WHERE id = ?", want: true},
		{name: "DELETE without WHERE", sql: "DELETE FROM users", want: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			analysis := analyseQuery(t, catalogue, testCase.sql)
			require.NotNil(t, analysis)
			assert.Equal(t, testCase.want, analysis.HasWhereClause)
		})
	}
}

func TestAnalyseQuery_NumberedParameterOverflowFallsBackToSequential(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, nil,
		`SELECT id FROM users WHERE id = ?99999999999999999999999999`)

	require.Len(t, analysis.ParameterReferences, 1)
	assert.Equal(t, 1, analysis.ParameterReferences[0].Number,
		"an unparseable ?N number should fall back to a sequential index, not overflow")
}

func TestAnalyseQuery_InsertOnConflictTargetPredicateReturning(t *testing.T) {
	t.Parallel()

	catalogue := newSQLiteCatalogue()

	tests := []struct {
		name               string
		sql                string
		expectedParameters int
	}{
		{
			name:               "DO NOTHING with partial-index predicate keeps RETURNING",
			sql:                "INSERT INTO users (name, email) VALUES (?, ?) ON CONFLICT (email) WHERE email IS NOT NULL DO NOTHING RETURNING id",
			expectedParameters: 2,
		},
		{
			name:               "DO UPDATE with partial-index predicate keeps SET parameter and RETURNING",
			sql:                "INSERT INTO users (name, email) VALUES (?, ?) ON CONFLICT (email) WHERE email IS NOT NULL DO UPDATE SET name = ? RETURNING id",
			expectedParameters: 3,
		},
		{
			name:               "predicate over a parenthesised expression still reaches RETURNING",
			sql:                "INSERT INTO users (name, email) VALUES (?, ?) ON CONFLICT (email) WHERE (email IS NOT NULL AND name <> '') DO NOTHING RETURNING id",
			expectedParameters: 2,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, catalogue, testCase.sql)

			require.NotNil(t, analysis)
			assert.True(t, analysis.HasReturning,
				"RETURNING after a conflict-target WHERE predicate must be detected")
			require.Len(t, analysis.OutputColumns, 1)
			assert.Equal(t, "id", analysis.OutputColumns[0].Name)
			require.Len(t, analysis.ParameterReferences, testCase.expectedParameters)
		})
	}
}

func TestAnalyseQuery_InsertChainedOnConflictKeepsReturning(t *testing.T) {
	t.Parallel()

	catalogue := newSQLiteCatalogue()

	analysis := analyseQuery(t, catalogue,
		"INSERT INTO users (id, name) VALUES (?, ?) "+
			"ON CONFLICT (id) DO NOTHING ON CONFLICT (name) DO NOTHING RETURNING id")

	require.NotNil(t, analysis)
	assert.True(t, analysis.HasReturning,
		"RETURNING after a chained second ON CONFLICT clause must be detected")
	require.Len(t, analysis.OutputColumns, 1)
	assert.Equal(t, "id", analysis.OutputColumns[0].Name)
}

func newFullTextCatalogue() *sqlmodel.Catalogue {
	catalogue := newSQLiteCatalogue()
	catalogue.Schemas["main"].Tables["docs"] = &sqlmodel.Table{
		Name:              "docs",
		VirtualModuleName: moduleFTS5,
		IsVirtual:         true,
		Columns: []sqlmodel.Column{
			{Name: "title", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"), Nullable: true},
			{Name: "body", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"), Nullable: true},
			{Name: "rank", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryFloat, "real"), Nullable: true, IsGenerated: true},
		},
	}
	return catalogue
}

func analyseQueryError(t *testing.T, sql string) error {
	t.Helper()

	engine := New()
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.NotEmpty(t, statements)

	analysis, err := engine.AnalyseQuery(t.Context(), newSQLiteCatalogue(), statements[0])
	require.Error(t, err)
	assert.Nil(t, analysis)
	return err
}

func TestAnalyseQuery_DelimitedIdentifiersAreNeverKeywords(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		wantTable   string
		wantColumns []string
		wantCompare string
	}{
		{
			name:        "double-quoted reserved words",
			sql:         `SELECT id, "group" FROM "order" WHERE "where" = ?`,
			wantTable:   "order",
			wantColumns: []string{"id", "group"},
			wantCompare: "where",
		},
		{
			name:        "backtick and bracket reserved words",
			sql:         "SELECT [select], `from` FROM [table] WHERE `limit` = ?",
			wantTable:   "table",
			wantColumns: []string{"select", "from"},
			wantCompare: "limit",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue := newSQLiteCatalogue()
			catalogue.Schemas["main"].Tables["order"] = &sqlmodel.Table{
				Name: "order",
				Columns: []sqlmodel.Column{
					{Name: "id", SQLType: sqlmodel.SQLType{EngineName: "integer", Category: sqlmodel.TypeCategoryInteger}},
					{Name: "group", SQLType: sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}},
					{Name: "where", SQLType: sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}},
				},
			}
			analysis := analyseQuery(t, catalogue, testCase.sql)

			require.Len(t, analysis.FromTables, 1)
			assert.Equal(t, testCase.wantTable, analysis.FromTables[0].Name)
			names := make([]string, 0, len(analysis.OutputColumns))
			for _, column := range analysis.OutputColumns {
				names = append(names, column.Name)
			}
			assert.Equal(t, testCase.wantColumns, names)
			require.Len(t, analysis.ParameterReferences, 1)
			require.NotNil(t, analysis.ParameterReferences[0].ColumnReference)
			assert.Equal(t, testCase.wantCompare, analysis.ParameterReferences[0].ColumnReference.ColumnName)
		})
	}
}

func TestAnalyseQuery_RequiresTheEndOfTheStatement(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
	}{
		{name: "trailing words after a complete query", sql: "SELECT id FROM users u stray tokens"},
		{name: "trailing tokens after the select list", sql: "SELECT id AS i stray FROM users"},
		{name: "trailing tokens inside a derived table", sql: "SELECT id FROM (SELECT id FROM users u stray tokens) d"},
		{name: "trailing tokens inside a CTE body", sql: "WITH c AS (SELECT id FROM users u stray tokens) SELECT id FROM c"},
		{name: "unparsable predicate subquery holding a parameter", sql: "SELECT id FROM users WHERE id IN (SELECT user_id AS u stray FROM posts WHERE id = ?)"},
		{name: "unparsable scalar subquery holding a parameter", sql: "SELECT (SELECT title AS t stray FROM posts WHERE id = ?) AS title FROM users"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := analyseQueryError(t, testCase.sql)

			require.ErrorIs(t, err, fault.ErrParse)
			var parseError *dialectkit.ParseError
			require.ErrorAs(t, err, &parseError)
			assert.Equal(t, "stray", parseError.Token, "the error names the first unparsed token")
		})
	}
}

func TestAnalyseQuery_AcceptsWholeStatementsTheGrammarReads(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		sql             string
		wantOutputs     int
		wantParameters  int
		wantFromTables  int
		wantJoinClauses int
	}{
		{name: "leading-dot number", sql: "SELECT .5 AS half, id FROM users WHERE id = ? AND name = ?", wantOutputs: 2, wantParameters: 2, wantFromTables: 1},
		{name: "qualified column in an expression", sql: "SELECT u.id + 1 AS next FROM users u WHERE u.id = ?", wantOutputs: 1, wantParameters: 1, wantFromTables: 1},
		{name: "implicit alias on a column", sql: "SELECT id user_id FROM users WHERE id = ?", wantOutputs: 1, wantParameters: 1, wantFromTables: 1},
		{name: "implicit alias on a call", sql: "SELECT count(*) total FROM users", wantOutputs: 1, wantFromTables: 1},
		{name: "trailing semicolon", sql: "SELECT id FROM users;", wantOutputs: 1, wantFromTables: 1},
		{name: "postfix NOTNULL", sql: "SELECT id NOTNULL AS present FROM users", wantOutputs: 1, wantFromTables: 1},
		{name: "index hint", sql: "SELECT id FROM users INDEXED BY users_name WHERE name = ?", wantOutputs: 1, wantParameters: 1, wantFromTables: 1},
		{
			name:            "parenthesised join",
			sql:             "SELECT u.id FROM (users u JOIN posts p ON u.id = p.user_id) WHERE p.id = ?",
			wantOutputs:     1,
			wantParameters:  1,
			wantFromTables:  1,
			wantJoinClauses: 1,
		},
		{name: "parenthesised table", sql: "SELECT id FROM (users)", wantOutputs: 1, wantFromTables: 1},
		{
			name:            "parenthesised group joined in",
			sql:             "SELECT u.id FROM users u LEFT JOIN (posts p JOIN posts q ON p.id = q.id) ON u.id = p.user_id",
			wantOutputs:     1,
			wantFromTables:  1,
			wantJoinClauses: 2,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newSQLiteCatalogue(), testCase.sql)

			assert.Len(t, analysis.OutputColumns, testCase.wantOutputs)
			assert.Len(t, analysis.ParameterReferences, testCase.wantParameters)
			assert.Len(t, analysis.FromTables, testCase.wantFromTables)
			assert.Len(t, analysis.JoinClauses, testCase.wantJoinClauses)
		})
	}
}

func TestAnalyseQuery_ParenthesisedGroupJoinedInKeepsTheOuterJoinKind(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, newSQLiteCatalogue(),
		"SELECT u.id FROM users u LEFT JOIN (posts p JOIN posts q ON p.id = q.id) ON u.id = p.user_id")

	require.Len(t, analysis.JoinClauses, 2)
	for _, join := range analysis.JoinClauses {
		assert.Equal(t, ast.JoinLeft, join.Kind, "a table brought in by a LEFT JOIN stays nullable")
	}
}

func TestAnalyseQuery_TypesLiteralsFromTheirShape(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, newSQLiteCatalogue(),
		"SELECT 7 AS whole, 0x1E AS hex, 1.5 AS fraction, .5 AS leading, 1e3 AS exponent, 5000000000 AS wide, "+
			"99999999999999999999 AS overflow FROM users")

	want := []string{"integer", "integer", "real", "real", "real", "bigint", "real"}
	require.Len(t, analysis.OutputColumns, len(want))
	for index, column := range analysis.OutputColumns {
		literal, ok := column.Expression.(*ast.LiteralExpression)
		require.Truef(t, ok, "column %s should be a literal", column.Name)
		assert.Equalf(t, want[index], literal.TypeName, "column %s", column.Name)
	}
}

func TestAnalyseQuery_BindsParametersToTheirOperands(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		wantContext sqlmodel.ParameterContext
		wantColumn  string
		wantCast    string
		wantCount   int
	}{
		{name: "NOT IN binds the column", sql: "SELECT id FROM users WHERE id NOT IN (?)", wantContext: sqlmodel.ParameterContextInList, wantColumn: "id", wantCount: 1},
		{name: "qualified NOT IN binds the column", sql: "SELECT u.id FROM users u WHERE u.name NOT IN (?, ?)", wantContext: sqlmodel.ParameterContextInList, wantColumn: "name", wantCount: 2},
		{name: "BETWEEN binds both bounds", sql: "SELECT id FROM users WHERE name BETWEEN ? AND ?", wantContext: sqlmodel.ParameterContextBetween, wantColumn: "name", wantCount: 2},
		{name: "NOT BETWEEN binds both bounds", sql: "SELECT id FROM users WHERE id NOT BETWEEN ? AND ? AND name = 'x'", wantContext: sqlmodel.ParameterContextBetween, wantColumn: "id", wantCount: 2},
		{name: "comparison with a text function result", sql: "SELECT id FROM users WHERE lower(name) = ?", wantContext: sqlmodel.ParameterContextComparison, wantCast: "text", wantCount: 1},
		{name: "comparison with an integer function result", sql: "SELECT id FROM users WHERE length(name) > ?", wantContext: sqlmodel.ParameterContextComparison, wantCast: "integer", wantCount: 1},
		{name: "comparison in a select-list CASE binds the column", sql: "SELECT CASE WHEN id = ? THEN 1 ELSE 0 END AS flagged FROM users", wantContext: sqlmodel.ParameterContextComparison, wantColumn: "id", wantCount: 1},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newSQLiteCatalogue(), testCase.sql)

			require.Len(t, analysis.ParameterReferences, testCase.wantCount)
			for _, reference := range analysis.ParameterReferences {
				assert.Equal(t, testCase.wantContext, reference.Context)
				if testCase.wantColumn != "" {
					require.NotNil(t, reference.ColumnReference)
					assert.Equal(t, testCase.wantColumn, reference.ColumnReference.ColumnName)
				}
				if testCase.wantCast != "" {
					require.NotNil(t, reference.CastType)
					assert.Equal(t, testCase.wantCast, reference.CastType.EngineName)
				}
			}
		})
	}
}

func TestAnalyseQuery_LeavesPolymorphicComparisonsUntyped(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, newSQLiteCatalogue(), "SELECT id FROM users WHERE json_extract(name, '$.a') = ?")

	require.Len(t, analysis.ParameterReferences, 1)
	assert.Nil(t, analysis.ParameterReferences[0].CastType)
	assert.Nil(t, analysis.ParameterReferences[0].ColumnReference)
}

func TestAnalyseQuery_FullTextQueriesTakeText(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		sql        string
		wantColumn string
	}{
		{name: "MATCH on the table-named column", sql: "SELECT title FROM docs WHERE docs MATCH ?", wantColumn: "docs"},
		{name: "equality on the table-named column", sql: "SELECT title FROM docs WHERE docs = ?", wantColumn: "docs"},
		{name: "equality through an alias", sql: "SELECT d.title FROM docs AS d WHERE d = ?", wantColumn: "d"},
		{name: "table-valued form", sql: "SELECT title FROM docs(?)", wantColumn: ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newFullTextCatalogue(), testCase.sql)

			require.Len(t, analysis.ParameterReferences, 1)
			reference := analysis.ParameterReferences[0]
			assert.Equal(t, sqlmodel.ParameterContextLike, reference.Context)
			if testCase.wantColumn == "" {
				assert.Nil(t, reference.ColumnReference)
				return
			}
			require.NotNil(t, reference.ColumnReference)
			assert.Equal(t, testCase.wantColumn, reference.ColumnReference.ColumnName)
		})
	}
}

func TestAnalyseQuery_EqualityOnAnOrdinaryColumnStaysAComparison(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, newFullTextCatalogue(), "SELECT title FROM docs JOIN users ON users.id = docs.rowid WHERE name = ?")

	require.Len(t, analysis.ParameterReferences, 1)
	assert.Equal(t, sqlmodel.ParameterContextComparison, analysis.ParameterReferences[0].Context)
}

func TestAnalyseQuery_ReadsBuiltinRelations(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		sql          string
		wantFunction string
	}{
		{name: "sqlite_schema", sql: "SELECT name, type FROM sqlite_schema WHERE type = ?", wantFunction: "sqlite_schema"},
		{name: "qualified sqlite_master", sql: "SELECT name FROM main.sqlite_master", wantFunction: "sqlite_master"},
		{name: "pragma function call", sql: "SELECT name FROM pragma_table_info(?)", wantFunction: "pragma_table_info"},
		{name: "pragma function without arguments", sql: "SELECT name FROM pragma_table_list", wantFunction: "pragma_table_list"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newSQLiteCatalogue(), testCase.sql)

			assert.Empty(t, analysis.FromTables)
			require.Len(t, analysis.RawTableValuedFunctions, 1)
			assert.Equal(t, testCase.wantFunction, analysis.RawTableValuedFunctions[0].FunctionName)
			assert.NotEmpty(t, New().TableValuedFunctionColumns(testCase.wantFunction))
		})
	}
}

func TestAnalyseQuery_RejectsConditionsTheGrammarCannotRead(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		sql       string
		wantToken string
	}{
		{name: "WHERE without a condition", sql: "SELECT id FROM users WHERE", wantToken: ""},
		{name: "WHERE followed by the terminator", sql: "SELECT id FROM users WHERE;", wantToken: ""},
		{name: "HAVING without a condition", sql: "SELECT id FROM users GROUP BY id HAVING;", wantToken: ""},
		{name: "ON without a condition", sql: "SELECT u.id FROM users u JOIN posts p ON;", wantToken: ""},
		{name: "parenthesis opened before the end", sql: "SELECT id FROM users WHERE id = (;", wantToken: ""},
		{name: "parenthesis never closed", sql: "SELECT id FROM users WHERE (id = 1;", wantToken: ""},
		{name: "AND without a right operand", sql: "SELECT id FROM users WHERE id = 1 AND;", wantToken: ""},
		{name: "AND running into the next clause", sql: "SELECT id FROM users WHERE id = 1 AND GROUP BY id", wantToken: "GROUP"},
		{name: "OR without a right operand", sql: "SELECT id FROM users WHERE id = 1 OR", wantToken: ""},
		{name: "comparison without a right operand", sql: "SELECT id FROM users WHERE id =;", wantToken: ""},
		{name: "a word left over after the WHERE condition", sql: "SELECT id FROM users WHERE id = 1 name;", wantToken: "name"},
		{name: "a word left over after the HAVING condition", sql: "SELECT id FROM users GROUP BY id HAVING count(*) > 1 name", wantToken: "name"},
		{name: "a word left over after the ON condition", sql: "SELECT u.id FROM users u JOIN posts p ON u.id = p.user_id name", wantToken: "name"},
		{name: "a doubled operator", sql: "SELECT id FROM users WHERE id = = 1", wantToken: "="},
		{name: "a doubled logical operator", sql: "SELECT id FROM users WHERE id = 1 OR OR id = 2", wantToken: "OR"},
		{name: "a call left open", sql: "SELECT id FROM users WHERE lower(name = ?", wantToken: ""},
		{name: "a conflict action condition left dangling", sql: "INSERT INTO users (id, name) VALUES (?, ?) ON CONFLICT (id) DO UPDATE SET name = excluded.name WHERE id =", wantToken: ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := analyseQueryError(t, testCase.sql)

			require.ErrorIs(t, err, fault.ErrParse)
			var parseError *dialectkit.ParseError
			require.ErrorAs(t, err, &parseError)
			assert.Equal(t, testCase.wantToken, parseError.Token)
		})
	}
}

func TestAnalyseQuery_ConditionsReadTheWholeExpressionGrammar(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		condition      string
		wantParameters int
	}{
		{name: "comparisons joined by AND and OR", condition: "id = ? AND (name <> ? OR email != ?)", wantParameters: 3},
		{name: "double equals", condition: "id == ?", wantParameters: 1},
		{name: "IS DISTINCT FROM", condition: "email IS NOT DISTINCT FROM ?", wantParameters: 1},
		{name: "IS a value", condition: "email IS ?", wantParameters: 1},
		{name: "NULL tests", condition: "email IS NULL OR email NOTNULL OR email ISNULL OR email NOT NULL", wantParameters: 0},
		{name: "BETWEEN", condition: "id NOT BETWEEN ? AND ?", wantParameters: 2},
		{name: "IN a list", condition: "id NOT IN (?, ?, 3)", wantParameters: 2},
		{name: "IN a subquery", condition: "id IN (SELECT user_id FROM posts WHERE title = ?)", wantParameters: 1},
		{name: "EXISTS", condition: "NOT EXISTS (SELECT 1 FROM posts WHERE posts.user_id = users.id)", wantParameters: 0},
		{name: "LIKE with ESCAPE", condition: "name LIKE ? ESCAPE '\\'", wantParameters: 1},
		{name: "NOT GLOB, REGEXP and MATCH", condition: "name NOT GLOB ? AND name REGEXP ? AND name MATCH ?", wantParameters: 3},
		{name: "COLLATE", condition: "name = ? COLLATE NOCASE", wantParameters: 1},
		{name: "CASE", condition: "CASE WHEN id > ? THEN 1 ELSE 0 END = 1", wantParameters: 1},
		{name: "CAST", condition: "CAST(id AS TEXT) = ?", wantParameters: 1},
		{name: "arithmetic and concatenation", condition: "-id + 1 * 2 / 3 % 4 = ~? AND name || 'x' = ?", wantParameters: 2},
		{name: "bitwise operators", condition: "(id & ?) | (id << 1) >> 1 > 0", wantParameters: 1},
		{name: "JSON arrows", condition: "name ->> '$.a' = ? AND name -> '$.b' IS NOT NULL", wantParameters: 1},
		{name: "a call with a star and a filter", condition: "(SELECT count(*) FILTER (WHERE id > ?) FROM posts) > 0", wantParameters: 1},
		{name: "current time keywords", condition: "name < CURRENT_TIMESTAMP", wantParameters: 0},
		{name: "named placeholders", condition: "id = :id AND name = @name AND email = $email AND id = ?7", wantParameters: 4},
		{name: "a blob literal", condition: "name = x'00ff'", wantParameters: 0},
		{name: "TRUE and FALSE", condition: "TRUE AND NOT FALSE", wantParameters: 0},
		{name: "qualified columns", condition: "main.users.id = ? AND users.name = ?", wantParameters: 2},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newSQLiteCatalogue(), "SELECT id FROM users WHERE "+testCase.condition)

			assert.Len(t, analysis.ParameterReferences, testCase.wantParameters)
			require.Len(t, analysis.ClauseExpressions, 1)
			assert.Equal(t, ast.ClauseWhere, analysis.ClauseExpressions[0].Clause)
		})
	}
}

func TestAnalyseQuery_FromIsNeverASelectedColumn(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		sql       string
		wantToken string
	}{
		{name: "an empty select list before a table", sql: "SELECT FROM users", wantToken: "FROM"},
		{name: "an empty select list before the end", sql: "SELECT FROM;", wantToken: "FROM"},
		{name: "a trailing comma before FROM", sql: "SELECT id, FROM users", wantToken: "FROM"},
		{name: "a clause keyword in an expression", sql: "SELECT id + WHERE FROM users", wantToken: "WHERE"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := analyseQueryError(t, testCase.sql)

			var parseError *dialectkit.ParseError
			require.ErrorAs(t, err, &parseError)
			assert.Equal(t, testCase.wantToken, parseError.Token)
		})
	}
}

func TestAnalyseQuery_ReadsNamedWindows(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		wantOutputs int
	}{
		{name: "a call over a named window", sql: "SELECT id, row_number() OVER w AS n FROM users WINDOW w AS (ORDER BY id)", wantOutputs: 2},
		{
			name:        "a window built on a named one",
			sql:         "SELECT sum(id) OVER (w ROWS UNBOUNDED PRECEDING) AS total FROM users WINDOW w AS (PARTITION BY name ORDER BY id)",
			wantOutputs: 1,
		},
		{
			name:        "several named windows before ORDER BY",
			sql:         "SELECT rank() OVER a AS r, rank() OVER b AS s FROM users WINDOW a AS (ORDER BY id), b AS (ORDER BY name) ORDER BY id",
			wantOutputs: 2,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newSQLiteCatalogue(), testCase.sql)

			assert.Len(t, analysis.OutputColumns, testCase.wantOutputs)
		})
	}
}

func TestAnalyseQuery_RejectsMalformedWindows(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"SELECT row_number() OVER FROM users",
		"SELECT row_number() OVER (ORDER BY id FROM users",
		"SELECT row_number() OVER w FROM users WINDOW w (ORDER BY id)",
		"SELECT row_number() OVER w FROM users WINDOW AS (ORDER BY id)",
	} {
		err := analyseQueryError(t, sql)

		require.ErrorIs(t, err, fault.ErrParse, sql)
	}
}

func TestAnalyseQuery_ANameWrittenInsideAndOutsideANestedQueryIsOneParameter(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
	}{
		{name: "a predicate subquery", sql: "SELECT id FROM users WHERE id = :id AND id IN (SELECT user_id FROM posts WHERE user_id = :id)"},
		{name: "a scalar subquery", sql: "SELECT (SELECT count(*) FROM posts WHERE user_id = :id) AS total FROM users WHERE id = :id"},
		{name: "a CTE body", sql: "WITH mine AS (SELECT id FROM posts WHERE user_id = :id) SELECT id FROM users WHERE id = :id"},
		{name: "a derived table", sql: "SELECT d.id FROM (SELECT id FROM users WHERE id = :id) d WHERE d.id = :id"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newSQLiteCatalogue(), testCase.sql)

			numbers := map[int]bool{}
			for _, reference := range analysis.ParameterReferences {
				numbers[reference.Number] = true
			}
			assert.Equal(t, map[int]bool{1: true}, numbers)
		})
	}
}

func TestPlaceholdersInExpressionsCarryTheirNumbers(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		expected []string
	}{
		{
			name:     "anonymous placeholders are numbered in the order they are written",
			sql:      "SELECT id + 1 = ? AS matched, ? AS echoed FROM items WHERE id > ? AND name = ? ORDER BY id",
			expected: []string{"select list 1", "select list 2", "clauses 3", "clauses 4"},
		},
		{
			name:     "a numbered placeholder written twice is one parameter",
			sql:      "SELECT ?2 = id AS matched FROM items WHERE name = ?1 OR id > ?2",
			expected: []string{"select list 2", "clauses 1", "clauses 2"},
		},
		{
			name:     "a bare placeholder after a numbered one takes the next number",
			sql:      "SELECT ?5 = id AS matched FROM items WHERE id > ?",
			expected: []string{"select list 5", "clauses 6"},
		},
		{
			name:     "each named form written twice is one parameter",
			sql:      "SELECT :bound > id AS matched, @label = name AS labelled FROM items WHERE id < :bound AND name = @label AND kind = $kind",
			expected: []string{"select list 1", "select list 2", "clauses 1", "clauses 2", "clauses 3"},
		},
		{
			name:     "placeholders inside calls and CASE keep their numbers",
			sql:      "SELECT coalesce(name, ?) AS label, CASE WHEN id > ? THEN 1 ELSE 0 END AS flag FROM items WHERE id = ?",
			expected: []string{"select list 1", "select list 2", "clauses 3"},
		},
		{
			name:     "a placeholder in a subquery keeps the statement's numbering",
			sql:      "SELECT id FROM items WHERE id IN (SELECT item_id FROM tags WHERE label = ?) AND name = ?",
			expected: []string{"clauses 2", "nested clauses 1"},
		},
		{
			name:     "an UPDATE numbers its SET values and WHERE",
			sql:      "UPDATE items SET name = ?, rank = rank + ? WHERE id = ?",
			expected: []string{"clauses 1", "clauses 2", "clauses 3"},
		},
		{
			name:     "an INSERT ... SELECT numbers its projection and its WHERE",
			sql:      "INSERT INTO items (id, name) SELECT ?, label FROM tags WHERE label = ?",
			expected: []string{"nested select list 1", "nested clauses 2"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, nil, testCase.sql)
			placed := placedParameterNumbers(analysis, "")
			require.Equal(t, testCase.expected, placed)
			registered := make(map[int]bool, len(analysis.ParameterReferences))
			for _, reference := range analysis.ParameterReferences {
				registered[reference.Number] = true
			}
			for _, each := range placed {
				fields := strings.Fields(each)
				number, err := strconv.Atoi(fields[len(fields)-1])
				require.NoError(t, err)
				require.Contains(t, registered, number, "%s names no registered parameter", each)
			}
		})
	}
}

func placedParameterNumbers(analysis *ast.RawQueryAnalysis, prefix string) []string {
	if analysis == nil {
		return nil
	}
	var placed []string
	for _, column := range analysis.OutputColumns {
		for _, number := range expressionParameterNumbers(column.Expression) {
			placed = append(placed, prefix+"select list "+strconv.Itoa(number))
		}
	}
	for _, clause := range analysis.ClauseExpressions {
		for _, number := range expressionParameterNumbers(clause.Expression) {
			placed = append(placed, prefix+"clauses "+strconv.Itoa(number))
		}
	}
	for _, definition := range analysis.CTEDefinitions {
		placed = append(placed, placedParameterNumbers(definition.Query, "nested ")...)
	}
	for _, derived := range analysis.RawDerivedTables {
		placed = append(placed, placedParameterNumbers(derived.InnerQuery, "nested ")...)
	}
	for _, subquery := range analysis.PredicateSubqueries {
		placed = append(placed, placedParameterNumbers(subquery, "nested ")...)
	}
	return append(placed, placedParameterNumbers(analysis.InsertSelect, "nested ")...)
}

func expressionParameterNumbers(expression ast.Expression) []int {
	switch typed := expression.(type) {
	case *ast.ParameterExpression:
		return []int{typed.Number}
	case *ast.FunctionCallExpression:
		return expressionsParameterNumbers(typed.Arguments...)
	case *ast.BinaryOpExpression:
		return expressionsParameterNumbers(typed.Left, typed.Right)
	case *ast.ComparisonExpression:
		return expressionsParameterNumbers(typed.Left, typed.Right)
	case *ast.LogicalOpExpression:
		return expressionsParameterNumbers(typed.Operands...)
	case *ast.CoalesceExpression:
		return expressionsParameterNumbers(typed.Arguments...)
	case *ast.InListExpression:
		return expressionsParameterNumbers(append([]ast.Expression{typed.Inner}, typed.Values...)...)
	case *ast.CaseWhenExpression:
		var numbers []int
		for _, branch := range typed.Branches {
			numbers = append(numbers, expressionsParameterNumbers(branch.Condition, branch.Result)...)
		}
		return append(numbers, expressionParameterNumbers(typed.ElseResult)...)
	default:
		return nil
	}
}

func expressionsParameterNumbers(expressions ...ast.Expression) []int {
	numbers := make([]int, 0, len(expressions))
	for _, expression := range expressions {
		numbers = append(numbers, expressionParameterNumbers(expression)...)
	}
	return numbers
}

func TestNestedQueriesKeepTheirOwnParts(t *testing.T) {
	t.Parallel()

	t.Run("a subquery in a condition carries its query", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, newSQLiteCatalogue(), "SELECT id FROM users WHERE (SELECT max(p.id) FROM posts p) = ?")

		comparison, isComparison := analysis.ClauseExpressions[0].Expression.(*ast.ComparisonExpression)
		require.True(t, isComparison)
		subquery, isSubquery := comparison.Left.(*ast.ScalarSubqueryExpression)
		require.True(t, isSubquery)
		require.Same(t, analysis.PredicateSubqueries[0], subquery.InnerQuery)
	})

	t.Run("each arm of a set operation keeps its own derived tables and parameters", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, newSQLiteCatalogue(),
			"SELECT id FROM users WHERE id = ? UNION ALL SELECT d.id FROM (SELECT id FROM posts WHERE id = ?) d")

		require.Empty(t, analysis.RawDerivedTables)
		require.Len(t, analysis.ParameterReferences, 2)
		arm := analysis.CompoundBranches[0].Query
		require.Len(t, arm.RawDerivedTables, 1)
		require.Len(t, arm.ParameterReferences, 1)
		require.Equal(t, 2, arm.ParameterReferences[0].Number)
	})
}

func TestValuesListsStandWhereverAQueryDoes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		columns    int
		branches   int
		parameters int
	}{
		{name: "a VALUES list before a set operator", sql: "VALUES (1) UNION ALL SELECT 2", columns: 1, branches: 1},
		{name: "a VALUES list after a set operator", sql: "SELECT 2 UNION ALL VALUES (3), (4)", columns: 1, branches: 1},
		{name: "a VALUES list as a common table expression", sql: "WITH t(n) AS (VALUES (1), (2)) SELECT n FROM t", columns: 1},
		{
			name:    "a VALUES list anchoring a recursive common table expression",
			sql:     "WITH RECURSIVE c(n) AS (VALUES (1) UNION ALL SELECT n + 1 FROM c WHERE n < 3) SELECT n FROM c",
			columns: 1,
		},
		{name: "a VALUES list as a derived table", sql: "SELECT * FROM (VALUES (1, 'a'), (2, 'b'))", columns: 1},
		{name: "a VALUES list as a subquery", sql: "SELECT id FROM users WHERE id IN (VALUES (1), (2))", columns: 1},
		{name: "a VALUES list binding every row", sql: "WITH d(a, b) AS (VALUES (?, ?), (?, ?)) SELECT a, b FROM d", columns: 2, parameters: 4},
		{name: "a VALUES statement after a WITH clause", sql: "WITH d(a) AS (SELECT 1) VALUES (2)", columns: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newSQLiteCatalogue(), test.sql)
			require.Len(t, analysis.OutputColumns, test.columns)
			require.Len(t, analysis.CompoundBranches, test.branches)
			require.Len(t, analysis.ParameterReferences, test.parameters)
			require.True(t, analysis.ReadOnly)
		})
	}
}

func TestAValuesListNeedsItsRowsInParenthesesAndNoOrderingAfterIt(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"VALUES", "VALUES 1", "SELECT 1 UNION VALUES (2", "WITH t AS (VALUES) SELECT * FROM t",
		"VALUES (1, 'a'), (2, 'b') ORDER BY 1 LIMIT 1", "SELECT 1 UNION ALL VALUES (2) LIMIT 1",
		"SELECT * FROM (VALUES (3), (1) ORDER BY 1)",
	} {
		t.Run(sql, func(t *testing.T) {
			t.Parallel()

			require.Error(t, analyseQueryError(t, sql))
		})
	}
}

func TestAnalyseQueryRecordsWhatAStatementDoesWithAConflictingRow(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
		want ast.ConflictAction
	}{
		{name: "a plain INSERT handles no conflict", sql: "INSERT INTO users (name) VALUES (?)", want: ast.ConflictActionNone},
		{name: "DO NOTHING skips the row", sql: "INSERT INTO users (name) VALUES (?) ON CONFLICT DO NOTHING RETURNING id",
			want: ast.ConflictActionDoNothing},
		{name: "a targeted DO NOTHING skips the row", sql: "INSERT INTO users (id, name) VALUES (?, ?) ON CONFLICT (id) DO NOTHING",
			want: ast.ConflictActionDoNothing},
		{name: "DO UPDATE updates the row",
			sql:  "INSERT INTO users (id, name) VALUES (?, ?) ON CONFLICT (id) DO UPDATE SET name = excluded.name",
			want: ast.ConflictActionDoUpdate},
		{name: "a DO UPDATE with a condition updates the row",
			sql:  "INSERT INTO users (id, name) VALUES (?, ?) ON CONFLICT (id) DO UPDATE SET name = excluded.name WHERE name <> ?",
			want: ast.ConflictActionDoUpdate},
		{name: "any DO NOTHING among the clauses skips the row",
			sql:  "INSERT INTO users (id, name) VALUES (?, ?) ON CONFLICT (id) DO UPDATE SET name = excluded.name ON CONFLICT DO NOTHING",
			want: ast.ConflictActionDoNothing},
		{name: "a DO NOTHING after a conditional DO UPDATE skips the row",
			sql: "INSERT INTO users (id, name) VALUES (?, ?) ON CONFLICT (id) DO UPDATE SET name = excluded.name WHERE name <> ? " +
				"ON CONFLICT DO NOTHING",
			want: ast.ConflictActionDoNothing},
		{name: "OR IGNORE skips the row", sql: "INSERT OR IGNORE INTO users (name) VALUES (?)", want: ast.ConflictActionDoNothing},
		{name: "OR IGNORE skips a conflict a targeted DO UPDATE leaves",
			sql:  "INSERT OR IGNORE INTO users (id, name) VALUES (?, ?) ON CONFLICT (id) DO UPDATE SET name = excluded.name",
			want: ast.ConflictActionDoNothing},
		{name: "OR IGNORE skips a row failing another constraint after a DO UPDATE with no target",
			sql:  "INSERT OR IGNORE INTO users (id, name) VALUES (?, ?) ON CONFLICT DO UPDATE SET name = excluded.name",
			want: ast.ConflictActionDoNothing},
		{name: "OR REPLACE replaces the row", sql: "INSERT OR REPLACE INTO users (id, name) VALUES (?, ?)",
			want: ast.ConflictActionDoUpdate},
		{name: "REPLACE INTO replaces the row", sql: "REPLACE INTO users (id, name) VALUES (?, ?)", want: ast.ConflictActionDoUpdate},
		{name: "OR ABORT handles no conflict", sql: "INSERT OR ABORT INTO users (name) VALUES (?)", want: ast.ConflictActionNone},
		{name: "OR FAIL handles no conflict", sql: "INSERT OR FAIL INTO users (name) VALUES (?)", want: ast.ConflictActionNone},
		{name: "OR ROLLBACK handles no conflict", sql: "INSERT OR ROLLBACK INTO users (name) VALUES (?)", want: ast.ConflictActionNone},
		{name: "a SELECT handles no conflict", sql: "SELECT id FROM users", want: ast.ConflictActionNone},
		{name: "UPDATE OR IGNORE is no INSERT conflict", sql: "UPDATE OR IGNORE users SET name = ?", want: ast.ConflictActionNone},
		{name: "a DELETE handles no conflict", sql: "DELETE FROM users WHERE id = ?", want: ast.ConflictActionNone},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newSQLiteCatalogue(), testCase.sql)

			assert.Equal(t, testCase.want, analysis.ConflictAction)
		})
	}
}

func TestAnalyseQueryRefusesAnIncompleteConflictClause(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		wantMessage string
	}{
		{name: "ON with no CONFLICT", sql: "INSERT INTO users (name) VALUES (?) ON NOTHING", wantMessage: "expected keyword CONFLICT"},
		{name: "ON CONFLICT with no action", sql: "INSERT INTO users (name) VALUES (?) ON CONFLICT", wantMessage: "expected keyword DO"},
		{name: "DO with no action", sql: "INSERT INTO users (name) VALUES (?) ON CONFLICT DO", wantMessage: "expected one of NOTHING, UPDATE"},
		{name: "DO UPDATE with no SET", sql: "INSERT INTO users (id) VALUES (?) ON CONFLICT (id) DO UPDATE", wantMessage: "expected keyword SET"},
		{name: "OR with no resolution", sql: "INSERT OR INTO users (name) VALUES (?)", wantMessage: "expected"},
		{name: "OR with a word that resolves nothing", sql: "INSERT OR SKIP INTO users (name) VALUES (?)", wantMessage: "expected"},
		{name: "UPDATE OR with a word that resolves nothing", sql: "UPDATE OR SKIP users SET name = ?", wantMessage: "expected"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := analyseQueryError(t, testCase.sql)

			require.ErrorIs(t, err, fault.ErrParse)
			assert.Contains(t, err.Error(), testCase.wantMessage)
		})
	}
}
