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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	deepNestingLevels = 4_000
)

func newPostgresCatalogue() *sqlmodel.Catalogue {
	return &sqlmodel.Catalogue{
		DefaultSchema: "public",
		Schemas: map[string]*sqlmodel.Schema{
			"public": {
				Name: "public",
				Tables: map[string]*sqlmodel.Table{
					"users": {
						Name: "users",
						Columns: []sqlmodel.Column{
							{
								Name:    "id",
								SQLType: sqlmodel.NewIntegerType("int4", 32, false),
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
								SQLType: sqlmodel.NewIntegerType("int4", 32, false),
							},
							{
								Name:    "user_id",
								SQLType: sqlmodel.NewIntegerType("int4", 32, false),
							},
							{
								Name:    "title",
								SQLType: sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText},
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
		name string
		sql  string
	}{
		{name: "limit coalesce", sql: "SELECT id FROM users LIMIT COALESCE($1, 100)"},
		{name: "offset greatest", sql: "SELECT id FROM users OFFSET GREATEST($1, 0)"},
		{name: "fetch coalesce", sql: "SELECT id FROM users ORDER BY id FETCH FIRST COALESCE($1, 10) ROWS ONLY"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			analysis := analyseQuery(t, nil, testCase.sql)
			require.Len(t, analysis.ParameterReferences, 1,
				"a parameter nested in a compound LIMIT/OFFSET value must be registered")
			assert.Equal(t, 1, analysis.ParameterReferences[0].Number)
		})
	}
}

func TestAnalyseQuery_BareLimitOffsetKeepsContext(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, nil, "SELECT id FROM users LIMIT $1 OFFSET $2")
	require.Len(t, analysis.ParameterReferences, 2)
	assert.Equal(t, sqlmodel.ParameterContextLimit, analysis.ParameterReferences[0].Context)
	assert.Equal(t, sqlmodel.ParameterContextOffset, analysis.ParameterReferences[1].Context)
}

func TestAnalyseQuery_ExistsSubqueryExposesInnerQueryParameters(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, nil,
		`SELECT EXISTS(SELECT 1 FROM orchestrator_tasks WHERE workflow_id = $1 AND status = $2) AS has_incomplete`)

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
		`SELECT a.id FROM accounts a WHERE a.id = (SELECT MAX(av2.id) FROM account_versions av2 WHERE av2.id < $2)`)

	require.Len(t, analysis.PredicateSubqueries, 1)
	inner := analysis.PredicateSubqueries[0]
	require.NotNil(t, inner)
	require.Len(t, inner.FromTables, 1)
	assert.Equal(t, "account_versions", inner.FromTables[0].Name)
	assert.Equal(t, "av2", inner.FromTables[0].Alias)

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

	analysis := analyseQuery(t, nil, `SELECT a.id FROM accounts a WHERE (a.id + 1) = $1`)
	assert.Empty(t, analysis.PredicateSubqueries)
}

func TestAnalyseQuery_SetCaseComparisonParameterTypedFromOperand(t *testing.T) {
	t.Parallel()

	catalogue := newPostgresCatalogue()
	analysis := analyseQuery(t, catalogue,
		"UPDATE users SET name = CASE WHEN id >= $1 THEN 'flagged' ELSE name END WHERE email = $2")

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

	catalogue := newPostgresCatalogue()

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
			name: "WHERE with parameter reference",
			sql:  "SELECT id FROM users WHERE id = $1",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
				assert.Equal(t, "id", analysis.OutputColumns[0].Name)

				require.NotEmpty(t, analysis.ParameterReferences, "should have at least one parameter reference")
				assert.Equal(t, 1, analysis.ParameterReferences[0].Number)
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
			},
		},
		{
			name: "aggregate function",
			sql:  "SELECT COUNT(*) FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)

				assert.Equal(t, "count", analysis.OutputColumns[0].Name)
				assert.True(t, analysis.ReadOnly, "SELECT with aggregate should be read-only")
			},
		},
		{
			name: "CTE with WITH clause",
			sql:  "WITH active AS (SELECT id, name FROM users WHERE id > 0) SELECT id, name FROM active",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.CTEDefinitions, "should have CTE definitions")
				assert.Equal(t, "active", analysis.CTEDefinitions[0].Name)
				require.Len(t, analysis.OutputColumns, 2)
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "recursive CTE",
			sql:  "WITH RECURSIVE nums AS (SELECT 1 AS n UNION ALL SELECT n + 1 FROM nums WHERE n < 10) SELECT n FROM nums",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.CTEDefinitions)
				assert.Equal(t, "nums", analysis.CTEDefinitions[0].Name)
				assert.True(t, analysis.CTEDefinitions[0].IsRecursive)
			},
		},
		{
			name: "UNION of two selects",
			sql:  "SELECT id, name FROM users UNION SELECT id, title FROM posts",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.CompoundBranches)
				assert.Equal(t, ast.CompoundUnion, analysis.CompoundBranches[0].Operator)
			},
		},
		{
			name: "UNION ALL",
			sql:  "SELECT id FROM users UNION ALL SELECT id FROM posts",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.CompoundBranches)
				assert.Equal(t, ast.CompoundUnionAll, analysis.CompoundBranches[0].Operator)
			},
		},
		{
			name: "INTERSECT",
			sql:  "SELECT id FROM users INTERSECT SELECT user_id FROM posts",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.CompoundBranches)
				assert.Equal(t, ast.CompoundIntersect, analysis.CompoundBranches[0].Operator)
			},
		},
		{
			name: "EXCEPT",
			sql:  "SELECT id FROM users EXCEPT SELECT user_id FROM posts",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.CompoundBranches)
				assert.Equal(t, ast.CompoundExcept, analysis.CompoundBranches[0].Operator)
			},
		},
		{
			name: "subquery in FROM clause",
			sql:  "SELECT sub.cnt FROM (SELECT count(*) AS cnt FROM users) sub",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.RawDerivedTables, "should have derived tables")
				assert.Equal(t, "sub", analysis.RawDerivedTables[0].Alias)
			},
		},
		{
			name: "GROUP BY and HAVING",
			sql:  "SELECT user_id, count(*) FROM posts GROUP BY user_id HAVING count(*) > 5",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.GroupByColumns)
				assert.Equal(t, "user_id", analysis.GroupByColumns[0].ColumnName)
			},
		},
		{
			name: "DISTINCT",
			sql:  "SELECT DISTINCT name FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
				assert.Equal(t, "name", analysis.OutputColumns[0].Name)
			},
		},
		{
			name: "DISTINCT ON",
			sql:  "SELECT DISTINCT ON (user_id) user_id, title FROM posts ORDER BY user_id, id",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 2)
				assert.Equal(t, "user_id", analysis.OutputColumns[0].Name)
			},
		},
		{
			name: "window function with OVER clause",
			sql:  "SELECT id, name, row_number() OVER (ORDER BY id) FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 3)
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "window function with PARTITION BY",
			sql:  "SELECT user_id, title, rank() OVER (PARTITION BY user_id ORDER BY id) FROM posts",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 3)
			},
		},
		{
			name: "FOR UPDATE marks as not read-only",
			sql:  "SELECT id, name FROM users WHERE id = $1 FOR UPDATE",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.False(t, analysis.ReadOnly, "SELECT FOR UPDATE should not be read-only")
				require.NotEmpty(t, analysis.ParameterReferences)
			},
		},
		{
			name: "LEFT JOIN",
			sql:  "SELECT u.id, p.title FROM users u LEFT JOIN posts p ON p.user_id = u.id",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.JoinClauses)
				assert.Equal(t, ast.JoinLeft, analysis.JoinClauses[0].Kind)
				assert.Equal(t, "posts", analysis.JoinClauses[0].Table.Name)
			},
		},
		{
			name: "RIGHT JOIN",
			sql:  "SELECT u.id, p.title FROM users u RIGHT JOIN posts p ON p.user_id = u.id",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.JoinClauses)
				assert.Equal(t, ast.JoinRight, analysis.JoinClauses[0].Kind)
			},
		},
		{
			name: "FULL JOIN",
			sql:  "SELECT u.id, p.title FROM users u FULL JOIN posts p ON p.user_id = u.id",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.JoinClauses)
				assert.Equal(t, ast.JoinFull, analysis.JoinClauses[0].Kind)
			},
		},
		{
			name: "CROSS JOIN",
			sql:  "SELECT u.id, p.title FROM users u CROSS JOIN posts p",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.JoinClauses)
				assert.Equal(t, ast.JoinCross, analysis.JoinClauses[0].Kind)
			},
		},
		{
			name: "LEFT OUTER JOIN",
			sql:  "SELECT u.id FROM users u LEFT OUTER JOIN posts p ON p.user_id = u.id",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.JoinClauses)
				assert.Equal(t, ast.JoinLeft, analysis.JoinClauses[0].Kind)
			},
		},
		{
			name: "INNER JOIN",
			sql:  "SELECT u.id FROM users u INNER JOIN posts p ON p.user_id = u.id",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.JoinClauses)
				assert.Equal(t, ast.JoinInner, analysis.JoinClauses[0].Kind)
			},
		},
		{
			name: "ORDER BY with multiple columns and directions",
			sql:  "SELECT id, name FROM users ORDER BY name ASC, id DESC",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 2)
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "LIMIT and OFFSET with parameters",
			sql:  "SELECT id FROM users LIMIT $1 OFFSET $2",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 2)
				assert.Equal(t, 1, analysis.ParameterReferences[0].Number)
				assert.Equal(t, sqlmodel.ParameterContextLimit, analysis.ParameterReferences[0].Context)
				assert.Equal(t, 2, analysis.ParameterReferences[1].Number)
				assert.Equal(t, sqlmodel.ParameterContextOffset, analysis.ParameterReferences[1].Context)
			},
		},
		{
			name: "cast expression with ::type",
			sql:  "SELECT id FROM users WHERE id = $1::integer",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.ParameterReferences)
				assert.Equal(t, 1, analysis.ParameterReferences[0].Number)
			},
		},
		{
			name: "CAST function syntax",
			sql:  "SELECT CAST($1 AS integer) FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.ParameterReferences)
				assert.Equal(t, sqlmodel.ParameterContextCast, analysis.ParameterReferences[0].Context)
				require.NotNil(t, analysis.ParameterReferences[0].CastType)
			},
		},
		{
			name: "named parameter :name",
			sql:  "SELECT id FROM users WHERE name = :name",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.ParameterReferences)
				assert.Equal(t, "name", analysis.ParameterReferences[0].Name)
			},
		},
		{
			name: "multiple parameters in complex WHERE",
			sql:  "SELECT id FROM users WHERE id > $1 AND name = $2 AND email = $3",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 3)
				assert.Equal(t, 1, analysis.ParameterReferences[0].Number)
				assert.Equal(t, 2, analysis.ParameterReferences[1].Number)
				assert.Equal(t, 3, analysis.ParameterReferences[2].Number)
			},
		},
		{
			name: "column alias with AS",
			sql:  "SELECT id AS user_id, name AS full_name FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 2)
				assert.Equal(t, "user_id", analysis.OutputColumns[0].Name)
				assert.Equal(t, "full_name", analysis.OutputColumns[1].Name)
			},
		},
		{
			name: "table-qualified star",
			sql:  "SELECT u.* FROM users u",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
				assert.True(t, analysis.OutputColumns[0].IsStar)
				assert.Equal(t, "u", analysis.OutputColumns[0].TableAlias)
			},
		},
		{
			name: "comma-separated FROM tables",
			sql:  "SELECT u.id, p.title FROM users u, posts p WHERE p.user_id = u.id",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.FromTables, 2)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
				assert.Equal(t, "posts", analysis.FromTables[1].Name)
			},
		},
		{
			name: "COALESCE expression",
			sql:  "SELECT COALESCE(email, 'unknown') FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "CASE WHEN expression",
			sql:  "SELECT CASE WHEN id > 10 THEN 'large' ELSE 'small' END FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "EXISTS subquery",
			sql:  "SELECT id FROM users WHERE EXISTS (SELECT 1 FROM posts WHERE posts.user_id = users.id)",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "IN list subquery",
			sql:  "SELECT id FROM users WHERE id IN (SELECT user_id FROM posts)",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "IN list with literal values",
			sql:  "SELECT id FROM users WHERE id IN (1, 2, 3)",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "BETWEEN expression",
			sql:  "SELECT id FROM users WHERE id BETWEEN $1 AND $2",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)

				require.Len(t, analysis.ParameterReferences, 2)
				assert.Equal(t, 1, analysis.ParameterReferences[0].Number)
				assert.Equal(t, 2, analysis.ParameterReferences[1].Number)
			},
		},
		{
			name: "LIKE expression",
			sql:  "SELECT id FROM users WHERE name LIKE '%test%'",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "IS NULL expression",
			sql:  "SELECT id FROM users WHERE email IS NULL",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "IS NOT NULL expression",
			sql:  "SELECT id FROM users WHERE email IS NOT NULL",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "NOT IN expression",
			sql:  "SELECT id FROM users WHERE id NOT IN (1, 2, 3)",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "OFFSET before LIMIT",
			sql:  "SELECT id FROM users OFFSET $1 LIMIT $2",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 2)
			},
		},
		{
			name: "FETCH FIRST syntax",
			sql:  "SELECT id FROM users FETCH FIRST 10 ROWS ONLY",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "scalar subquery in SELECT list",
			sql:  "SELECT id, (SELECT count(*) FROM posts WHERE posts.user_id = users.id) AS post_count FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 2)
			},
		},
		{
			name: "boolean literal in WHERE",
			sql:  "SELECT id FROM users WHERE true",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "arithmetic in SELECT",
			sql:  "SELECT id, id + 1 AS next_id FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 2)
			},
		},
		{
			name: "string concatenation",
			sql:  "SELECT name || ' <' || email || '>' AS display FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "table-valued function in FROM",
			sql:  "SELECT val FROM generate_series(1, 10) AS val",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.RawTableValuedFunctions)
				assert.Equal(t, "generate_series", analysis.RawTableValuedFunctions[0].FunctionName)
			},
		},
		{
			name: "multiple CTEs",
			sql:  "WITH a AS (SELECT 1 AS x), b AS (SELECT 2 AS y) SELECT a.x, b.y FROM a, b",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.CTEDefinitions, 2)
				assert.Equal(t, "a", analysis.CTEDefinitions[0].Name)
				assert.Equal(t, "b", analysis.CTEDefinitions[1].Name)
			},
		},
		{
			name: "JOIN with USING clause",
			sql:  "SELECT u.id FROM users u JOIN posts p USING (id)",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.JoinClauses)
			},
		},
		{
			name: "expression with LIKE in SELECT list uses expression parser",
			sql:  "SELECT name LIKE '%test%' AS is_test FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
				assert.Equal(t, "is_test", analysis.OutputColumns[0].Name)
			},
		},
		{
			name: "expression with BETWEEN in SELECT list",
			sql:  "SELECT id BETWEEN 1 AND 100 AS in_range FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
				assert.Equal(t, "in_range", analysis.OutputColumns[0].Name)
			},
		},
		{
			name: "expression with IN list in SELECT list",
			sql:  "SELECT id IN (1, 2, 3) AS in_set FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
				assert.Equal(t, "in_set", analysis.OutputColumns[0].Name)
			},
		},
		{
			name: "IS NULL in SELECT list",
			sql:  "SELECT email IS NULL AS has_no_email FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
				assert.Equal(t, "has_no_email", analysis.OutputColumns[0].Name)
			},
		},
		{
			name: "IS NOT NULL in SELECT list",
			sql:  "SELECT email IS NOT NULL AS has_email FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "NOT expression in SELECT list",
			sql:  "SELECT NOT (id = 1) AS not_first FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "OR expression in SELECT list",
			sql:  "SELECT id = 1 OR id = 2 AS is_first_or_second FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "AND expression in SELECT list",
			sql:  "SELECT id > 0 AND name = 'test' AS both_true FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "multiplication and division in SELECT list",
			sql:  "SELECT id * 2 AS doubled, id / 2 AS halved FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 2)
			},
		},
		{
			name: "NOT LIKE in SELECT list",
			sql:  "SELECT name NOT LIKE '%admin%' AS is_regular FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "NOT BETWEEN in SELECT list",
			sql:  "SELECT id NOT BETWEEN 10 AND 20 AS outside_range FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "NOT IN in SELECT list",
			sql:  "SELECT id NOT IN (1, 2) AS not_first_two FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "INTERVAL expression in SELECT list",
			sql:  "SELECT INTERVAL '1 day' AS one_day FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "NULL literal in SELECT list",
			sql:  "SELECT NULL AS nothing FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
				assert.Equal(t, "nothing", analysis.OutputColumns[0].Name)
			},
		},
		{
			name: "boolean literal in SELECT list",
			sql:  "SELECT true AS always_true, false AS always_false FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 2)
			},
		},
		{
			name: "ROW constructor in SELECT list",
			sql:  "SELECT ROW(id, name) AS user_row FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "function with arguments in SELECT list",
			sql:  "SELECT upper(name) AS upper_name FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
				assert.Equal(t, "upper_name", analysis.OutputColumns[0].Name)
			},
		},
		{
			name: "JSON arrow operator in SELECT list",
			sql:  "SELECT '{}'::jsonb -> 'key' AS val FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "unary minus in SELECT list",
			sql:  "SELECT -id AS neg_id FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "ARRAY expression in SELECT list",
			sql:  "SELECT ARRAY[1, 2, 3] AS arr FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "window function with frame clause",
			sql:  "SELECT id, sum(id) OVER (ORDER BY id ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) AS running_sum FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 2)
			},
		},
		{
			name: "CASE WHEN with searched expression",
			sql:  "SELECT CASE id WHEN 1 THEN 'one' WHEN 2 THEN 'two' ELSE 'other' END AS label FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "EXISTS subquery in SELECT list",
			sql:  "SELECT EXISTS (SELECT 1 FROM posts WHERE posts.user_id = users.id) AS has_posts FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "IN subquery in SELECT list",
			sql:  "SELECT id IN (SELECT user_id FROM posts) AS has_posts FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "float literal in SELECT list",
			sql:  "SELECT 3.14 AS pi FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "CURRENT_TIMESTAMP in SELECT list",
			sql:  "SELECT CURRENT_TIMESTAMP AS now FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "schema-qualified table in FROM",
			sql:  "SELECT id FROM public.users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.FromTables)
				assert.Equal(t, "public", analysis.FromTables[0].Schema)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
			},
		},
		{
			name: "schema-qualified column reference in SELECT",
			sql:  "SELECT public.users.id FROM public.users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "named parameter used twice deduplicates",
			sql:  "SELECT id FROM users WHERE name = :name OR email = :name",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 2)

				assert.Equal(t, analysis.ParameterReferences[0].Number, analysis.ParameterReferences[1].Number)
				assert.Equal(t, "name", analysis.ParameterReferences[0].Name)
			},
		},
		{
			name: "multiple different named parameters",
			sql:  "SELECT id FROM users WHERE name = :name AND email = :email",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 2)
				assert.Equal(t, "name", analysis.ParameterReferences[0].Name)
				assert.Equal(t, "email", analysis.ParameterReferences[1].Name)

				assert.NotEqual(t, analysis.ParameterReferences[0].Number, analysis.ParameterReferences[1].Number)
			},
		},
		{
			name: "FOR UPDATE OF table",
			sql:  "SELECT id FROM users FOR UPDATE OF users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.False(t, analysis.ReadOnly)
			},
		},
		{
			name: "GROUP BY with table-qualified column",
			sql:  "SELECT u.name, count(*) FROM users u GROUP BY u.name",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.GroupByColumns)
				assert.Equal(t, "u", analysis.GroupByColumns[0].TableAlias)
				assert.Equal(t, "name", analysis.GroupByColumns[0].ColumnName)
			},
		},
		{
			name: "LIMIT ALL",
			sql:  "SELECT id FROM users LIMIT ALL",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "parameter in function call in SELECT",
			sql:  "SELECT upper($1) AS upper_val FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.ParameterReferences)
				assert.Equal(t, sqlmodel.ParameterContextFunctionArgument, analysis.ParameterReferences[0].Context)
			},
		},
		{
			name: "COALESCE with parameter gets comparison context",
			sql:  "SELECT COALESCE(email, $1) FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.ParameterReferences)

				assert.Equal(t, sqlmodel.ParameterContextComparison, analysis.ParameterReferences[0].Context)
				require.NotNil(t, analysis.ParameterReferences[0].ColumnReference)
				assert.Equal(t, "email", analysis.ParameterReferences[0].ColumnReference.ColumnName)
			},
		},
		{
			name: "inline cast ::type on parameter in SELECT",
			sql:  "SELECT $1::text AS casted FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.ParameterReferences)
				assert.Equal(t, sqlmodel.ParameterContextCast, analysis.ParameterReferences[0].Context)
				require.NotNil(t, analysis.ParameterReferences[0].CastType)
			},
		},
		{
			name: "IS DISTINCT FROM in SELECT list",
			sql:  "SELECT id IS DISTINCT FROM 1 AS is_different FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "IS NOT DISTINCT FROM in SELECT list",
			sql:  "SELECT id IS NOT DISTINCT FROM 1 AS is_same FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "IS TRUE in SELECT list",
			sql:  "SELECT (id > 0) IS TRUE AS is_positive FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "SIMILAR TO in SELECT list",
			sql:  "SELECT name SIMILAR TO '%(test|admin)%' AS is_special FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "ILIKE in SELECT list",
			sql:  "SELECT name ILIKE '%test%' AS matches_ci FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "array subscript in SELECT list",
			sql:  "SELECT ARRAY[10, 20, 30][1] AS first_element FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "comparison with ANY subquery",
			sql:  "SELECT id = ANY(ARRAY[1, 2, 3]) AS in_array FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "modulo operator in SELECT list",
			sql:  "SELECT id % 2 AS remainder FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "BETWEEN SYMMETRIC in SELECT list",
			sql:  "SELECT id BETWEEN SYMMETRIC 10 AND 1 AS in_range FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "parameter in IN list in SELECT list",
			sql:  "SELECT id IN ($1, $2) AS in_set FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.ParameterReferences)
				assert.Equal(t, sqlmodel.ParameterContextInList, analysis.ParameterReferences[0].Context)
			},
		},
		{
			name: "parameter in BETWEEN in SELECT list",
			sql:  "SELECT id BETWEEN $1 AND $2 AS in_range FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 2)
				assert.Equal(t, sqlmodel.ParameterContextBetween, analysis.ParameterReferences[0].Context)
				assert.Equal(t, sqlmodel.ParameterContextBetween, analysis.ParameterReferences[1].Context)
			},
		},
		{
			name: "LIKE with ESCAPE in SELECT list",
			sql:  "SELECT name LIKE '%\\%%' ESCAPE '\\' AS has_percent FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "double precision literal in SELECT",
			sql:  "SELECT 1e10 AS big_number FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "string literal in SELECT",
			sql:  "SELECT 'hello world' AS greeting FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "CTE with column names",
			sql:  "WITH named_cte (x, y) AS (SELECT id, name FROM users) SELECT x, y FROM named_cte",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.CTEDefinitions)
				assert.Equal(t, "named_cte", analysis.CTEDefinitions[0].Name)
			},
		},
		{
			name: "CTE with VALUES body",
			sql:  "WITH data AS (VALUES (1, 'a'), (2, 'b')) SELECT * FROM data",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.CTEDefinitions)
			},
		},
		{
			name: "window function with named window reference",
			sql:  "SELECT id, row_number() OVER w AS rn FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 2)
			},
		},
		{
			name: "bitwise AND operator in SELECT",
			sql:  "SELECT id & 1 AS bit_flag FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "aggregate with FILTER clause",
			sql:  "SELECT count(*) FILTER (WHERE id > 5) AS filtered_count FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "aggregate with WITHIN GROUP",
			sql:  "SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY id) AS median_id FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "CAST with array type",
			sql:  "SELECT CAST($1 AS integer[]) AS arr FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.ParameterReferences)
				assert.Equal(t, sqlmodel.ParameterContextCast, analysis.ParameterReferences[0].Context)
			},
		},
		{
			name: "inline cast with timestamp with time zone",
			sql:  "SELECT $1::timestamp with time zone AS ts FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.ParameterReferences)
				assert.Equal(t, sqlmodel.ParameterContextCast, analysis.ParameterReferences[0].Context)
			},
		},
		{
			name: "ARRAY subquery",
			sql:  "SELECT ARRAY(SELECT id FROM users) AS all_ids",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "parameter in WHERE with CAST via skip-tokens path",
			sql:  "SELECT id FROM users WHERE name = CAST($1 AS text)",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.ParameterReferences)
			},
		},
		{
			name: "schema.table.column function call",
			sql:  "SELECT pg_catalog.array_length(ARRAY[1,2,3], 1) AS len FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "FETCH FIRST with parameter",
			sql:  "SELECT id FROM users FETCH FIRST $1 ROWS ONLY",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.ParameterReferences)
				assert.Equal(t, sqlmodel.ParameterContextLimit, analysis.ParameterReferences[0].Context)
			},
		},
		{
			name: "OFFSET then FETCH",
			sql:  "SELECT id FROM users OFFSET 10 ROWS FETCH NEXT 5 ROWS ONLY",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "SELECT with implicit column alias (no AS) using expression",
			sql:  "SELECT id + 1 next_id FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
				assert.Equal(t, "next_id", analysis.OutputColumns[0].Name)
			},
		},
		{
			name: "table.star in SELECT list with dot prefix",
			sql:  "SELECT users.id FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
				assert.Equal(t, "id", analysis.OutputColumns[0].ColumnName)
			},
		},
		{
			name: "parameter with inline cast in WHERE via skip-tokens",
			sql:  "SELECT id FROM users WHERE id = $1::integer",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.ParameterReferences)
			},
		},
		{
			name: "NOT BETWEEN via expression parser",
			sql:  "SELECT NOT (id BETWEEN 1 AND 10) AS outside FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "parameter inside function in WHERE via skip-tokens",
			sql:  "SELECT id FROM users WHERE name = lower($1)",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.ParameterReferences)
			},
		},
		{
			name: "FETCH with OFFSET after it",
			sql:  "SELECT id FROM users FETCH FIRST 10 ROWS ONLY OFFSET 5",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "LIMIT with comma syntax for offset",
			sql:  "SELECT id FROM users LIMIT 10, 5",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "TVF with column definitions",
			sql:  "SELECT x, y FROM generate_series(1, 10) AS gs(x integer)",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.RawTableValuedFunctions)
			},
		},
		{
			name: "CTE with NOT MATERIALIZED hint",
			sql:  "WITH active AS NOT MATERIALIZED (SELECT id FROM users) SELECT id FROM active",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.CTEDefinitions)
			},
		},
		{
			name: "window function with RANGE frame",
			sql:  "SELECT id, sum(id) OVER (ORDER BY id RANGE BETWEEN CURRENT ROW AND UNBOUNDED FOLLOWING) AS cumsum FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 2)
			},
		},
		{
			name: "window function with ROWS frame and numeric bounds",
			sql:  "SELECT id, sum(id) OVER (ORDER BY id ROWS BETWEEN 1 PRECEDING AND 1 FOLLOWING) AS moving_sum FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 2)
			},
		},
		{
			name: "window function with EXCLUDE clause",
			sql:  "SELECT id, sum(id) OVER (ORDER BY id ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW EXCLUDE CURRENT ROW) FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 2)
			},
		},
		{
			name: "aggregate function with ORDER BY inside",
			sql:  "SELECT array_agg(name ORDER BY id) AS names FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "aggregate with DISTINCT",
			sql:  "SELECT count(DISTINCT name) AS unique_names FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "table-qualified dot.star in SELECT",
			sql:  "SELECT u.* FROM users u JOIN posts p ON p.user_id = u.id",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
				assert.True(t, analysis.OutputColumns[0].IsStar)
				assert.Equal(t, "u", analysis.OutputColumns[0].TableAlias)
			},
		},
		{
			name: "array slice expression",
			sql:  "SELECT (ARRAY[1,2,3,4])[2:3] AS slice FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "inline cast on schema-qualified type",
			sql:  "SELECT $1::pg_catalog.int4 AS val FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.ParameterReferences)
			},
		},
		{
			name: "WITH MATERIALIZED hint",
			sql:  "WITH cached AS MATERIALIZED (SELECT id FROM users) SELECT id FROM cached",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.CTEDefinitions)
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

func TestAnalyseQuery_LikeParameterContext(t *testing.T) {
	t.Parallel()

	catalogue := newPostgresCatalogue()

	tests := []struct {
		assertions func(t *testing.T, analysis *ast.RawQueryAnalysis)
		name       string
		sql        string
	}{
		{
			name: "LIKE pattern with direct column LHS",
			sql:  "SELECT id FROM users WHERE name LIKE $1",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextLike, analysis.ParameterReferences[0].Context)
				require.NotNil(t, analysis.ParameterReferences[0].ColumnReference)
				assert.Equal(t, "name", analysis.ParameterReferences[0].ColumnReference.ColumnName)
			},
		},
		{
			name: "ILIKE classifies as Like context",
			sql:  "SELECT id FROM users WHERE email ILIKE $1",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextLike, analysis.ParameterReferences[0].Context)
				require.NotNil(t, analysis.ParameterReferences[0].ColumnReference)
				assert.Equal(t, "email", analysis.ParameterReferences[0].ColumnReference.ColumnName)
			},
		},
		{
			name: "NOT LIKE binds to LHS column",
			sql:  "SELECT id FROM users WHERE name NOT LIKE $1",
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
			sql:  "SELECT id FROM users WHERE name LIKE ('%' || $1 || '%')",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextLike, analysis.ParameterReferences[0].Context)
				require.NotNil(t, analysis.ParameterReferences[0].ColumnReference)
				assert.Equal(t, "name", analysis.ParameterReferences[0].ColumnReference.ColumnName)
			},
		},
		{
			name: "LIKE with function-wrapped column picks the column",
			sql:  "SELECT id FROM users WHERE LOWER(name) LIKE $1",
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
			sql:  "SELECT id FROM users u WHERE u.email LIKE $1",
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
			sql:  "SELECT id FROM users WHERE name LIKE $1 OR email LIKE $2",
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

	catalogue := newPostgresCatalogue()

	tests := []struct {
		assertions func(t *testing.T, analysis *ast.RawQueryAnalysis)
		name       string
		sql        string
	}{
		{
			name: "simple insert with parameter",
			sql:  "INSERT INTO users (name) VALUES ($1)",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.Equal(t, "users", analysis.InsertTable)
				assert.Equal(t, []string{"name"}, analysis.InsertColumns)

				require.NotEmpty(t, analysis.ParameterReferences)
				assert.Equal(t, 1, analysis.ParameterReferences[0].Number)

				require.NotNil(t, analysis.ParameterReferences[0].ColumnReference)
				assert.Equal(t, "name", analysis.ParameterReferences[0].ColumnReference.ColumnName)
			},
		},
		{
			name: "insert with RETURNING clause",
			sql:  "INSERT INTO users (name) VALUES ($1) RETURNING id",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.Equal(t, "users", analysis.InsertTable)
				assert.True(t, analysis.HasReturning, "should detect RETURNING clause")

				require.NotEmpty(t, analysis.OutputColumns)
				assert.Equal(t, "id", analysis.OutputColumns[0].Name)
			},
		},
		{
			name: "INSERT with ON CONFLICT DO NOTHING",
			sql:  "INSERT INTO users (name, email) VALUES ($1, $2) ON CONFLICT (email) DO NOTHING",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.Equal(t, "users", analysis.InsertTable)
				assert.Equal(t, []string{"name", "email"}, analysis.InsertColumns)
				require.Len(t, analysis.ParameterReferences, 2)
			},
		},
		{
			name: "INSERT with ON CONFLICT DO UPDATE",
			sql:  "INSERT INTO users (name, email) VALUES ($1, $2) ON CONFLICT (email) DO UPDATE SET name = $3",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.Equal(t, "users", analysis.InsertTable)
				require.Len(t, analysis.ParameterReferences, 3)
			},
		},
		{
			name: "INSERT ... SELECT",
			sql:  "INSERT INTO posts (user_id, title) SELECT id, name FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.Equal(t, "posts", analysis.InsertTable)
				assert.Equal(t, []string{"user_id", "title"}, analysis.InsertColumns)

				require.NotNil(t, analysis.InsertSelect)
				require.NotEmpty(t, analysis.InsertSelect.FromTables)
				assert.Equal(t, "users", analysis.InsertSelect.FromTables[0].Name)
			},
		},
		{

			name: "INSERT ... SELECT with JOIN and WHERE parameters",
			sql: "INSERT INTO posts (user_id, title) " +
				"SELECT u.id, o.name FROM users u INNER JOIN orgs o ON o.id = u.org_id " +
				"WHERE u.id > $1 AND o.active = $2 " +
				"ON CONFLICT (user_id) DO NOTHING",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.Equal(t, "posts", analysis.InsertTable)
				require.NotNil(t, analysis.InsertSelect)
				require.NotEmpty(t, analysis.InsertSelect.FromTables)
				assert.Equal(t, "users", analysis.InsertSelect.FromTables[0].Name)
				require.NotEmpty(t, analysis.InsertSelect.JoinClauses)
				assert.Equal(t, "orgs", analysis.InsertSelect.JoinClauses[0].Table.Name)
				require.Len(t, analysis.InsertSelect.ParameterReferences, 2)
			},
		},
		{
			name: "INSERT with multiple rows",
			sql:  "INSERT INTO users (name) VALUES ($1), ($2), ($3)",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.Equal(t, "users", analysis.InsertTable)
				require.Len(t, analysis.ParameterReferences, 3)
			},
		},
		{
			name: "INSERT with multiple columns",
			sql:  "INSERT INTO users (name, email) VALUES ($1, $2)",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.Equal(t, "users", analysis.InsertTable)
				assert.Equal(t, []string{"name", "email"}, analysis.InsertColumns)
				require.Len(t, analysis.ParameterReferences, 2)

				require.NotNil(t, analysis.ParameterReferences[0].ColumnReference)
				assert.Equal(t, "name", analysis.ParameterReferences[0].ColumnReference.ColumnName)

				require.NotNil(t, analysis.ParameterReferences[1].ColumnReference)
				assert.Equal(t, "email", analysis.ParameterReferences[1].ColumnReference.ColumnName)
			},
		},
		{
			name: "INSERT with RETURNING star",
			sql:  "INSERT INTO users (name) VALUES ($1) RETURNING *",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.True(t, analysis.HasReturning)
				require.NotEmpty(t, analysis.OutputColumns)
				assert.True(t, analysis.OutputColumns[0].IsStar)
			},
		},
		{
			name: "INSERT with ON CONFLICT on constraint name",
			sql:  "INSERT INTO users (name) VALUES ($1) ON CONFLICT ON CONSTRAINT uq_name DO NOTHING",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.Equal(t, "users", analysis.InsertTable)
			},
		},
		{
			name: "INSERT DEFAULT VALUES",
			sql:  "INSERT INTO users DEFAULT VALUES RETURNING id",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.Equal(t, "users", analysis.InsertTable)
				assert.True(t, analysis.HasReturning)
			},
		},
		{
			name: "INSERT with OVERRIDING SYSTEM VALUE",
			sql:  "INSERT INTO users (id, name) OVERRIDING SYSTEM VALUE VALUES (1, 'test')",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.Equal(t, "users", analysis.InsertTable)
			},
		},
		{
			name: "INSERT without column list",
			sql:  "INSERT INTO users VALUES ($1, $2, $3)",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.Equal(t, "users", analysis.InsertTable)
				assert.Empty(t, analysis.InsertColumns)
			},
		},
		{
			name: "INSERT with ON CONFLICT DO UPDATE SET with WHERE",
			sql:  "INSERT INTO users (name, email) VALUES ($1, $2) ON CONFLICT (email) DO UPDATE SET name = $3 WHERE users.id > 0",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.Equal(t, "users", analysis.InsertTable)
				require.Len(t, analysis.ParameterReferences, 3)
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

func findParameterByNumber(references []ast.RawParameterReference, number int) *ast.RawParameterReference {
	for index := range references {
		if references[index].Number == number {
			return &references[index]
		}
	}
	return nil
}

func TestAnalyseQuery_FunctionArgumentMetadata(t *testing.T) {
	t.Parallel()

	t.Run("FROM-clause TVF argument ordinals follow the call-site slot", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, nil,
			"SELECT page_id FROM content.get_pages_with_latest_version($3, $1, $2)")

		require.NotEmpty(t, analysis.RawTableValuedFunctions)
		assert.Equal(t, "content.get_pages_with_latest_version",
			analysis.RawTableValuedFunctions[0].FunctionName)

		expectedOrdinals := map[int]int{3: 0, 1: 1, 2: 2}
		for number, expectedOrdinal := range expectedOrdinals {
			parameter := findParameterByNumber(analysis.ParameterReferences, number)
			require.NotNilf(t, parameter, "expected a reference for $%d", number)
			assert.Equal(t, sqlmodel.ParameterContextFunctionArgument, parameter.Context)
			assert.Equal(t, "content.get_pages_with_latest_version", parameter.EnclosingFunctionName)
			assert.Equal(t, expectedOrdinal, parameter.ArgumentOrdinal)
		}
	})

	t.Run("scalar builtin function argument records the bare lower-cased name", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, nil, "SELECT STRING_TO_ARRAY($2, '/')")

		require.NotEmpty(t, analysis.ParameterReferences)
		parameter := findParameterByNumber(analysis.ParameterReferences, 2)
		require.NotNil(t, parameter)
		assert.Equal(t, sqlmodel.ParameterContextFunctionArgument, parameter.Context)
		assert.Equal(t, "string_to_array", parameter.EnclosingFunctionName)
		assert.Equal(t, 0, parameter.ArgumentOrdinal)
	})

	t.Run("function argument in SELECT records name and ordinal", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, nil, "SELECT upper($1) AS upper_val FROM users")

		require.NotEmpty(t, analysis.ParameterReferences)
		assert.Equal(t, sqlmodel.ParameterContextFunctionArgument, analysis.ParameterReferences[0].Context)
		assert.Equal(t, "upper", analysis.ParameterReferences[0].EnclosingFunctionName)
		assert.Equal(t, 0, analysis.ParameterReferences[0].ArgumentOrdinal)
	})

	t.Run("nested call ordinals are isolated to their own call", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, nil, "SELECT outer_fn($1, inner_fn($2, $3))")

		outerParameter := findParameterByNumber(analysis.ParameterReferences, 1)
		require.NotNil(t, outerParameter)
		assert.Equal(t, "outer_fn", outerParameter.EnclosingFunctionName)
		assert.Equal(t, 0, outerParameter.ArgumentOrdinal)

		innerFirst := findParameterByNumber(analysis.ParameterReferences, 2)
		require.NotNil(t, innerFirst)
		assert.Equal(t, "inner_fn", innerFirst.EnclosingFunctionName)
		assert.Equal(t, 0, innerFirst.ArgumentOrdinal)

		innerSecond := findParameterByNumber(analysis.ParameterReferences, 3)
		require.NotNil(t, innerSecond)
		assert.Equal(t, "inner_fn", innerSecond.EnclosingFunctionName)
		assert.Equal(t, 1, innerSecond.ArgumentOrdinal)
	})

	t.Run("non-placeholder TVF argument still consumes its ordinal", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, nil,
			"SELECT page_id FROM content.get_pages_with_latest_version('latest', $1, $2)")

		first := findParameterByNumber(analysis.ParameterReferences, 1)
		require.NotNil(t, first)
		assert.Equal(t, 1, first.ArgumentOrdinal, "a literal argument burns slot 0")

		second := findParameterByNumber(analysis.ParameterReferences, 2)
		require.NotNil(t, second)
		assert.Equal(t, 2, second.ArgumentOrdinal)
	})
}

func TestAnalyseQuery_InsertSelectProjectionParameter(t *testing.T) {
	t.Parallel()

	t.Run("projection placeholder gets the target column", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, nil,
			"INSERT INTO posts (user_id, title) SELECT $1, name FROM users WHERE id > $2")

		require.NotNil(t, analysis.InsertSelect)

		projection := findParameterByNumber(analysis.ParameterReferences, 1)
		require.NotNil(t, projection)
		assert.Equal(t, sqlmodel.ParameterContextAssignment, projection.Context)
		require.NotNil(t, projection.ColumnReference)
		assert.Equal(t, "posts", projection.ColumnReference.TableAlias)
		assert.Equal(t, "user_id", projection.ColumnReference.ColumnName)

		whereParameter := findParameterByNumber(analysis.ParameterReferences, 2)
		require.NotNil(t, whereParameter)
		require.NotNil(t, whereParameter.ColumnReference)
		assert.Equal(t, "id", whereParameter.ColumnReference.ColumnName)
	})

	t.Run("literal projection items burn target-column slots", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, nil,
			"INSERT INTO session_versions (id, session_id, status, reason) "+
				"SELECT gen_random_uuid(), sv.session_id, 'deleted', $1 "+
				"FROM session_versions sv WHERE sv.id = $2")

		projection := findParameterByNumber(analysis.ParameterReferences, 1)
		require.NotNil(t, projection)
		assert.Equal(t, sqlmodel.ParameterContextAssignment, projection.Context)
		require.NotNil(t, projection.ColumnReference)
		assert.Equal(t, "session_versions", projection.ColumnReference.TableAlias)
		assert.Equal(t, "reason", projection.ColumnReference.ColumnName)

		whereParameter := findParameterByNumber(analysis.ParameterReferences, 2)
		require.NotNil(t, whereParameter)
		require.NotNil(t, whereParameter.ColumnReference)
		assert.Equal(t, "id", whereParameter.ColumnReference.ColumnName)
	})

	t.Run("data-modifying CTE INSERT ... SELECT projection placeholders", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, nil,
			"WITH inserted AS ("+
				"INSERT INTO media_transformations (id, source_algorithm, priority) "+
				"SELECT $5, $1, $6 RETURNING id) SELECT id FROM inserted")

		idParameter := findParameterByNumber(analysis.ParameterReferences, 5)
		require.NotNil(t, idParameter)
		assert.Equal(t, sqlmodel.ParameterContextAssignment, idParameter.Context)
		require.NotNil(t, idParameter.ColumnReference)
		assert.Equal(t, "media_transformations", idParameter.ColumnReference.TableAlias)
		assert.Equal(t, "id", idParameter.ColumnReference.ColumnName)

		priorityParameter := findParameterByNumber(analysis.ParameterReferences, 6)
		require.NotNil(t, priorityParameter)
		require.NotNil(t, priorityParameter.ColumnReference)
		assert.Equal(t, "priority", priorityParameter.ColumnReference.ColumnName)

		algorithmParameter := findParameterByNumber(analysis.ParameterReferences, 1)
		require.NotNil(t, algorithmParameter)
		require.NotNil(t, algorithmParameter.ColumnReference)
		assert.Equal(t, "source_algorithm", algorithmParameter.ColumnReference.ColumnName)
	})

	t.Run("placeholder nested in a projection subquery keeps its own scope", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, nil,
			"INSERT INTO posts (a, b) SELECT $1, (SELECT max(x) FROM t2 WHERE y = $2) FROM t1")

		outer := findParameterByNumber(analysis.ParameterReferences, 1)
		require.NotNil(t, outer)
		assert.Equal(t, sqlmodel.ParameterContextAssignment, outer.Context)
		require.NotNil(t, outer.ColumnReference)
		assert.Equal(t, "a", outer.ColumnReference.ColumnName)

		nested := findParameterByNumber(analysis.ParameterReferences, 2)
		require.NotNil(t, nested)
		if nested.ColumnReference != nil {
			assert.NotEqual(t, "b", nested.ColumnReference.ColumnName,
				"a placeholder inside a projection subquery must not bind the INSERT target column")
		}
	})
}

func TestAnalyseQuery_Update(t *testing.T) {
	t.Parallel()

	catalogue := newPostgresCatalogue()

	tests := []struct {
		assertions func(t *testing.T, analysis *ast.RawQueryAnalysis)
		name       string
		sql        string
	}{
		{
			name: "simple update with parameters",
			sql:  "UPDATE users SET name = $1 WHERE id = $2",
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
			name: "UPDATE with FROM clause (join-style update)",
			sql:  "UPDATE posts SET title = $1 FROM users WHERE posts.user_id = users.id AND users.name = $2",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)

				require.True(t, len(analysis.FromTables) >= 2)
				assert.Equal(t, "posts", analysis.FromTables[0].Name)
				assert.Equal(t, "users", analysis.FromTables[1].Name)
				require.Len(t, analysis.ParameterReferences, 2)
			},
		},
		{
			name: "UPDATE with RETURNING",
			sql:  "UPDATE users SET name = $1 WHERE id = $2 RETURNING id, name",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.True(t, analysis.HasReturning)
				require.Len(t, analysis.OutputColumns, 2)
				assert.Equal(t, "id", analysis.OutputColumns[0].Name)
				assert.Equal(t, "name", analysis.OutputColumns[1].Name)
			},
		},
		{
			name: "UPDATE with multiple SET columns",
			sql:  "UPDATE users SET name = $1, email = $2 WHERE id = $3",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 3)
				assert.Equal(t, sqlmodel.ParameterContextAssignment, analysis.ParameterReferences[0].Context)
				assert.Equal(t, sqlmodel.ParameterContextAssignment, analysis.ParameterReferences[1].Context)
				assert.Equal(t, sqlmodel.ParameterContextComparison, analysis.ParameterReferences[2].Context)
			},
		},
		{
			name: "UPDATE with alias",
			sql:  "UPDATE users u SET name = $1 WHERE u.id = $2",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.FromTables)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
				assert.Equal(t, "u", analysis.FromTables[0].Alias)
			},
		},
		{
			name: "UPDATE with multi-column SET",
			sql:  "UPDATE users SET (name, email) = ($1, $2) WHERE id = $3",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 3)
			},
		},
		{
			name: "UPDATE with AS alias",
			sql:  "UPDATE users AS u SET name = $1 WHERE u.id = $2",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.FromTables)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
				assert.Equal(t, "u", analysis.FromTables[0].Alias)
			},
		},
		{
			name: "UPDATE with expression in SET (not parameter)",
			sql:  "UPDATE users SET name = name || ' suffix' WHERE id = $1",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.ParameterReferences)
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

	catalogue := newPostgresCatalogue()

	tests := []struct {
		assertions func(t *testing.T, analysis *ast.RawQueryAnalysis)
		name       string
		sql        string
	}{
		{
			name: "simple delete with parameter",
			sql:  "DELETE FROM users WHERE id = $1",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)

				require.NotEmpty(t, analysis.FromTables)
				assert.Equal(t, "users", analysis.FromTables[0].Name)

				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, 1, analysis.ParameterReferences[0].Number)
			},
		},
		{
			name: "DELETE with USING clause",
			sql:  "DELETE FROM posts USING users WHERE posts.user_id = users.id AND users.name = $1",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)

				require.True(t, len(analysis.FromTables) >= 2)
				assert.Equal(t, "posts", analysis.FromTables[0].Name)
				assert.Equal(t, "users", analysis.FromTables[1].Name)
				require.Len(t, analysis.ParameterReferences, 1)
			},
		},
		{
			name: "DELETE with RETURNING clause",
			sql:  "DELETE FROM users WHERE id = $1 RETURNING id, name",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.True(t, analysis.HasReturning)
				require.Len(t, analysis.OutputColumns, 2)
				assert.Equal(t, "id", analysis.OutputColumns[0].Name)
				assert.Equal(t, "name", analysis.OutputColumns[1].Name)
			},
		},
		{
			name: "DELETE with alias",
			sql:  "DELETE FROM users u WHERE u.id = $1",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.FromTables)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
				assert.Equal(t, "u", analysis.FromTables[0].Alias)
			},
		},
		{
			name: "DELETE with multiple conditions",
			sql:  "DELETE FROM users WHERE id = $1 AND name = $2",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.Len(t, analysis.ParameterReferences, 2)
			},
		},
		{
			name: "DELETE with AS alias",
			sql:  "DELETE FROM users AS u WHERE u.id = $1",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				require.NotEmpty(t, analysis.FromTables)
				assert.Equal(t, "u", analysis.FromTables[0].Alias)
			},
		},
		{
			name: "DELETE with USING and JOIN in USING",
			sql:  "DELETE FROM posts USING users JOIN posts p2 ON p2.user_id = users.id WHERE posts.id = p2.id AND users.name = $1",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.Equal(t, "posts", analysis.FromTables[0].Name)
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

	catalogue := newPostgresCatalogue()

	tests := []struct {
		assertions func(t *testing.T, analysis *ast.RawQueryAnalysis)
		name       string
		sql        string
	}{
		{
			name: "simple VALUES statement",
			sql:  "VALUES (1, 'hello'), (2, 'world')",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.True(t, analysis.ReadOnly, "VALUES should be read-only")
			},
		},
		{
			name: "VALUES with parameters",
			sql:  "VALUES ($1, $2)",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
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

func TestAnalyseQuery_DataModifyingCTE(t *testing.T) {
	t.Parallel()

	catalogue := newPostgresCatalogue()

	tests := []struct {
		assertions func(t *testing.T, analysis *ast.RawQueryAnalysis)
		name       string
		sql        string
	}{
		{
			name: "WITH INSERT CTE makes query non-read-only",
			sql:  "WITH ins AS (INSERT INTO posts (user_id, title) VALUES (1, 'test') RETURNING id) SELECT id FROM ins",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.False(t, analysis.ReadOnly, "data-modifying CTE should not be read-only")
			},
		},
		{
			name: "WITH DELETE CTE",
			sql:  "WITH del AS (DELETE FROM users WHERE id = $1 RETURNING id) SELECT id FROM del",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.False(t, analysis.ReadOnly)
				require.NotEmpty(t, analysis.ParameterReferences)
			},
		},
		{
			name: "WITH UPDATE CTE",
			sql:  "WITH upd AS (UPDATE users SET name = 'updated' WHERE id = $1 RETURNING id, name) SELECT id, name FROM upd",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotNil(t, analysis)
				assert.False(t, analysis.ReadOnly)
				require.NotEmpty(t, analysis.CTEDefinitions)
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

func TestAnalyseQuery_HasWhereClause(t *testing.T) {
	t.Parallel()

	catalogue := newPostgresCatalogue()

	tests := []struct {
		name string
		sql  string
		want bool
	}{
		{name: "SELECT with WHERE", sql: "SELECT id FROM users WHERE id = $1", want: true},
		{name: "SELECT without WHERE", sql: "SELECT id FROM users", want: false},
		{name: "UPDATE with WHERE", sql: "UPDATE users SET name = $1 WHERE id = $2", want: true},
		{name: "UPDATE without WHERE", sql: "UPDATE users SET name = $1", want: false},
		{name: "DELETE with WHERE", sql: "DELETE FROM users WHERE id = $1", want: true},
		{name: "DELETE without WHERE", sql: "DELETE FROM users", want: false},
		{name: "UPDATE WHERE CURRENT OF", sql: "UPDATE users SET name = $1 WHERE CURRENT OF c1", want: true},
		{name: "DELETE WHERE CURRENT OF", sql: "DELETE FROM users WHERE CURRENT OF c1", want: true},
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

func TestAnalyseSelect_RejectsDeeplyNestedSubqueries(t *testing.T) {
	t.Parallel()

	catalogue := newPostgresCatalogue()

	const depth = dialectkit.DefaultMaxParseDepth + 4
	var builder strings.Builder
	builder.WriteString("SELECT * FROM users")
	for range depth {
		previous := builder.String()
		builder.Reset()
		builder.WriteString("SELECT * FROM (")
		builder.WriteString(previous)
		builder.WriteString(") sub")
	}

	engine := New()
	statements, parseErr := engine.ParseStatements(t.Context(), builder.String())
	require.NoError(t, parseErr)
	require.NotEmpty(t, statements)

	_, analyseErr := engine.AnalyseQuery(t.Context(), catalogue, statements[0])
	require.Error(t, analyseErr)
	assert.ErrorIs(t, analyseErr, dialectkit.ErrParseDepth)
}

func TestAnalyseQuery_NonDML(t *testing.T) {
	t.Parallel()

	catalogue := newPostgresCatalogue()
	engine := New()

	statements, err := engine.ParseStatements(t.Context(), "CREATE TABLE foo (id int)")
	require.NoError(t, err)
	require.NotEmpty(t, statements)

	analysis, err := engine.AnalyseQuery(t.Context(), catalogue, statements[0])
	require.NoError(t, err)

	require.NotNil(t, analysis, "non-DML should return a non-nil empty analysis")
	assert.Empty(t, analysis.OutputColumns, "non-DML should have no output columns")
	assert.Empty(t, analysis.FromTables, "non-DML should have no FROM tables")
	assert.Empty(t, analysis.ParameterReferences, "non-DML should have no parameter references")
}

func TestAnalyseQuery_InsertOnConflictTargetPredicateReturning(t *testing.T) {
	t.Parallel()

	catalogue := newPostgresCatalogue()

	tests := []struct {
		name               string
		sql                string
		expectedParameters int
	}{
		{
			name:               "DO NOTHING RETURNING",
			sql:                "INSERT INTO users (name, email) VALUES ($1, $2) ON CONFLICT (email) WHERE email IS NOT NULL DO NOTHING RETURNING id",
			expectedParameters: 2,
		},
		{
			name:               "DO SET RETURNING",
			sql:                "INSERT INTO users (name, email) VALUES ($1, $2) ON CONFLICT (email) WHERE email IS NOT NULL DO UPDATE SET name = $3 RETURNING id",
			expectedParameters: 3,
		},
		{
			name:               "predicate over a parenthesised expression still reaches RETURNING",
			sql:                "INSERT INTO users (name, email) VALUES ($1, $2) ON CONFLICT (email) WHERE (email IS NOT NULL AND name <> '') DO NOTHING RETURNING id",
			expectedParameters: 2,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, catalogue, testCase.sql)

			require.NotNil(t, analysis)

			assert.True(t, analysis.HasReturning, "RETURNING has failed to be properly defined")
			require.Len(t, analysis.OutputColumns, 1)
			assert.Equal(t, "id", analysis.OutputColumns[0].Name)
			require.Len(t, analysis.ParameterReferences, testCase.expectedParameters)
		})
	}
}

func TestAnalyseSelectParsesSetOperationArmsIntoAFlatList(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		sql           string
		wantOperators []ast.CompoundOperator
		wantReadOnly  bool
	}{
		{
			name:          "three arms are siblings",
			sql:           "SELECT 1 AS a UNION SELECT 2 INTERSECT SELECT 3",
			wantOperators: []ast.CompoundOperator{ast.CompoundUnion, ast.CompoundIntersect},
			wantReadOnly:  true,
		},
		{
			name:          "trailing clauses apply to the whole set operation",
			sql:           "SELECT id FROM users UNION ALL SELECT id FROM users EXCEPT SELECT id FROM users ORDER BY id LIMIT $1 FOR UPDATE",
			wantOperators: []ast.CompoundOperator{ast.CompoundUnionAll, ast.CompoundExcept},
			wantReadOnly:  false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newPostgresCatalogue(), testCase.sql)

			require.Len(t, analysis.CompoundBranches, len(testCase.wantOperators))
			for index, branch := range analysis.CompoundBranches {
				assert.Equal(t, testCase.wantOperators[index], branch.Operator)
				assert.Empty(t, branch.Query.CompoundBranches, "arms must not nest")
				assert.Equal(t, testCase.wantReadOnly, branch.Query.ReadOnly)
				assert.Empty(t, branch.Query.ParameterReferences, "an arm holds only the parameters written in it")
			}
			assert.Equal(t, testCase.wantReadOnly, analysis.ReadOnly)
		})
	}
}

func TestAnalyseSelectHandlesLongSetOperationChains(t *testing.T) {
	t.Parallel()

	const armCount = 10_000
	sql := "SELECT 1 AS a" + strings.Repeat(" UNION ALL SELECT 1", armCount-1)

	analysis := analyseQuery(t, newPostgresCatalogue(), sql)

	assert.Len(t, analysis.CompoundBranches, armCount-1)
}

func analyseQueryError(t *testing.T, sql string) error {
	t.Helper()

	engine := New()
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.NotEmpty(t, statements)

	_, err = engine.AnalyseQuery(t.Context(), newPostgresCatalogue(), statements[len(statements)-1])
	return err
}

func outputNames(analysis *ast.RawQueryAnalysis) []string {
	names := make([]string, 0, len(analysis.OutputColumns))
	for index := range analysis.OutputColumns {
		names = append(names, analysis.OutputColumns[index].Name)
	}
	return names
}

func parameterColumns(analysis *ast.RawQueryAnalysis) []string {
	columns := make([]string, 0, len(analysis.ParameterReferences))
	for index := range analysis.ParameterReferences {
		reference := analysis.ParameterReferences[index].ColumnReference
		if reference == nil {
			columns = append(columns, "")
			continue
		}
		if reference.TableAlias != "" {
			columns = append(columns, reference.TableAlias+"."+reference.ColumnName)
			continue
		}
		columns = append(columns, reference.ColumnName)
	}
	return columns
}

func TestAnalyseQueryTreatsQuotedReservedWordsAsNames(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		sql            string
		wantOutputs    []string
		wantTable      string
		wantParameters []string
	}{
		{
			name:           "select with quoted reserved words",
			sql:            `SELECT id, "group" FROM "order" WHERE "where" = $1`,
			wantOutputs:    []string{"id", "group"},
			wantTable:      "order",
			wantParameters: []string{"where"},
		},
		{
			name:           "update with quoted reserved words",
			sql:            `UPDATE "order" SET "from" = $1 WHERE "group" = $2`,
			wantTable:      "order",
			wantOutputs:    []string{},
			wantParameters: []string{"order.from", "group"},
		},
		{
			name:           "delete with quoted reserved words",
			sql:            `DELETE FROM "select" WHERE "limit" = $1 RETURNING "offset"`,
			wantTable:      "select",
			wantOutputs:    []string{"offset"},
			wantParameters: []string{"limit"},
		},
		{
			name:           "quoted alias is a name",
			sql:            `SELECT o.id AS "from" FROM "order" AS "where" JOIN "group" "on" ON "on".id = o.id WHERE o.id = $1`,
			wantTable:      "order",
			wantOutputs:    []string{"from"},
			wantParameters: []string{"o.id"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newPostgresCatalogue(), testCase.sql)

			assert.Equal(t, testCase.wantOutputs, outputNames(analysis))
			require.NotEmpty(t, analysis.FromTables)
			assert.Equal(t, testCase.wantTable, analysis.FromTables[0].Name)
			assert.Equal(t, testCase.wantParameters, parameterColumns(analysis))
		})
	}
}

func TestAnalyseQueryRejectsTokensAfterTheStatement(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
	}{
		{name: "select", sql: "SELECT id FROM users WHERE id = $1 ) )"},
		{name: "insert", sql: "INSERT INTO users (id) VALUES ($1) )"},
		{name: "update", sql: "UPDATE users SET name = $1 WHERE id = $2 )"},
		{name: "delete", sql: "DELETE FROM users WHERE id = $1 )"},
		{name: "values", sql: "VALUES (1, 2) )"},
		{name: "select list with a stray word", sql: "SELECT id id2 id3 FROM users"},
		{name: "cte body", sql: "WITH c AS (SELECT id FROM users junk junk) SELECT id FROM c"},
		{name: "derived table body", sql: "SELECT id FROM (SELECT id FROM users junk junk) AS d"},
		{name: "in subquery body", sql: "SELECT id FROM users WHERE id IN (SELECT user_id FROM posts junk junk)"},
		{name: "scalar subquery body", sql: "SELECT (SELECT user_id FROM posts junk junk) AS x FROM users"},
		{name: "exists subquery body", sql: "SELECT id FROM users WHERE EXISTS (SELECT 1 FROM posts junk junk)"},
		{name: "predicate subquery body", sql: "SELECT id FROM users WHERE id = (SELECT user_id FROM posts WHERE id = $1 FOR UPDATE junk)"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := analyseQueryError(t, testCase.sql)
			require.Error(t, err)
			require.ErrorIs(t, err, fault.ErrParse)

			require.ErrorAs(t, err, new(*dialectkit.ParseError))
		})
	}
}

func TestAnalyseQueryRejectsAnEmptyPredicate(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"SELECT id FROM authors WHERE;",
		"SELECT id FROM users WHERE ORDER BY id",
		"UPDATE users SET name = $1 WHERE",
		"DELETE FROM users WHERE RETURNING id",
		"SELECT id FROM users GROUP BY id HAVING",
		"SELECT u.id FROM users u JOIN posts p ON WHERE u.id = 1",
	} {
		t.Run(sql, func(t *testing.T) {
			t.Parallel()

			err := analyseQueryError(t, sql)
			require.ErrorIs(t, err, fault.ErrParse)
		})
	}
}

func TestAnalyseQueryReadsLeadingDotNumbers(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, newPostgresCatalogue(), "SELECT .5 AS half, id * .5 AS scaled FROM users WHERE id = $1 AND name = $2")

	assert.Equal(t, []string{"half", "scaled"}, outputNames(analysis))
	assert.Equal(t, []string{"id", "name"}, parameterColumns(analysis))
	require.Len(t, analysis.FromTables, 1)
}

func TestAnalyseQueryParsesTimeZoneAndCollationPostfixes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		wantOutputs []string
	}{
		{
			name:        "at time zone in the select list",
			sql:         "SELECT id, created_at AT TIME ZONE 'UTC' AS utc FROM users WHERE id = $1",
			wantOutputs: []string{"id", "utc"},
		},
		{
			name:        "at time zone with a parameter zone",
			sql:         "SELECT created_at AT TIME ZONE $2 AT TIME ZONE 'UTC' AS local FROM users WHERE id = $1",
			wantOutputs: []string{"local"},
		},
		{
			name:        "collate in the select list",
			sql:         `SELECT id, name COLLATE "C" FROM users WHERE id = $1`,
			wantOutputs: []string{"id", "name"},
		},
		{
			name:        "schema-qualified collation",
			sql:         `SELECT name COLLATE pg_catalog."default" AS n FROM users WHERE id = $1`,
			wantOutputs: []string{"n"},
		},
		{
			name:        "collate in order by",
			sql:         `SELECT id FROM users WHERE id = $1 ORDER BY name COLLATE "C" DESC`,
			wantOutputs: []string{"id"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newPostgresCatalogue(), testCase.sql)

			assert.Equal(t, testCase.wantOutputs, outputNames(analysis))
			require.Len(t, analysis.FromTables, 1)
			assert.Equal(t, "users", analysis.FromTables[0].Name)
			require.NotEmpty(t, analysis.ParameterReferences)
		})
	}
}

func TestAnalyseQueryAtTimeZoneIsTheTimezoneFunction(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, newPostgresCatalogue(), "SELECT created_at AT TIME ZONE 'UTC' AS utc FROM users")

	require.Len(t, analysis.OutputColumns, 1)
	call, ok := analysis.OutputColumns[0].Expression.(*ast.FunctionCallExpression)
	require.True(t, ok, "AT TIME ZONE should be the timezone function, got %T", analysis.OutputColumns[0].Expression)
	assert.Equal(t, "timezone", call.FunctionName)
	require.Len(t, call.Arguments, 2)
}

func TestAnalyseQueryNotInBindsTheColumn(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		sql            string
		wantParameters []string
	}{
		{name: "not in list", sql: "SELECT id FROM users WHERE id NOT IN ($1, $2)", wantParameters: []string{"id", "id"}},
		{name: "not in single", sql: "SELECT id FROM users WHERE name NOT IN ($1)", wantParameters: []string{"name"}},
		{name: "qualified column", sql: "SELECT u.id FROM users u WHERE u.id NOT IN ($1)", wantParameters: []string{"u.id"}},
		{name: "named parameter", sql: "SELECT id FROM users WHERE id NOT IN (:ids)", wantParameters: []string{"id"}},
		{name: "in list", sql: "SELECT id FROM users WHERE id IN ($1)", wantParameters: []string{"id"}},
		{name: "negated predicate", sql: "SELECT id FROM users WHERE NOT id IN ($1)", wantParameters: []string{"id"}},
		{name: "not in after and", sql: "SELECT id FROM users WHERE name = $1 AND id NOT IN ($2)", wantParameters: []string{"name", "id"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newPostgresCatalogue(), testCase.sql)

			assert.Equal(t, testCase.wantParameters, parameterColumns(analysis))
			last := analysis.ParameterReferences[len(analysis.ParameterReferences)-1]
			assert.Equal(t, sqlmodel.ParameterContextInList, last.Context)
		})
	}
}

func TestAnalyseQueryTypesNumberLiteralsByShape(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, newPostgresCatalogue(),
		"SELECT 1 AS i, 1.5 AS e, .5 AS d, 5. AS t, 1e3 AS a, 2.5E-2 AS b, 0x1E AS h, 0b101 AS bits")

	want := map[string]string{
		"i": "integer", "e": "numeric", "d": "numeric", "t": "numeric",
		"a": "double precision", "b": "double precision", "h": "integer", "bits": "integer",
	}
	require.Len(t, analysis.OutputColumns, len(want))
	for index := range analysis.OutputColumns {
		column := analysis.OutputColumns[index]
		literal, ok := column.Expression.(*ast.LiteralExpression)
		require.True(t, ok, "column %s should be a literal, got %T", column.Name, column.Expression)
		assert.Equal(t, want[column.Name], literal.TypeName, "column %s", column.Name)
	}
}

func TestAnalyseQueryParsesParenthesisedFromItems(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		sql            string
		wantTables     []string
		wantJoins      []string
		wantParameters []string
	}{
		{
			name:           "parenthesised join",
			sql:            "SELECT u.id FROM (users u JOIN posts p ON p.user_id = u.id) WHERE u.id = $1",
			wantTables:     []string{"users"},
			wantJoins:      []string{"posts"},
			wantParameters: []string{"u.id"},
		},
		{
			name:           "parenthesised table",
			sql:            "SELECT id FROM (users) WHERE id = $1",
			wantTables:     []string{"users"},
			wantJoins:      []string{},
			wantParameters: []string{"id"},
		},
		{
			name:           "nested parenthesised joins",
			sql:            "SELECT u.id FROM ((users u JOIN posts p ON p.user_id = u.id) LEFT JOIN posts q ON q.id = p.id) WHERE u.id = $1",
			wantTables:     []string{"users"},
			wantJoins:      []string{"posts", "posts"},
			wantParameters: []string{"u.id"},
		},
		{
			name:           "parenthesised join after a comma",
			sql:            "SELECT u.id FROM posts x, (users u JOIN posts p ON p.user_id = u.id) WHERE u.id = $1",
			wantTables:     []string{"posts", "users"},
			wantJoins:      []string{"posts"},
			wantParameters: []string{"u.id"},
		},
		{
			name:           "join onto a parenthesised join",
			sql:            "SELECT u.id FROM posts x JOIN (users u JOIN posts p ON p.user_id = u.id) ON x.id = p.id WHERE u.id = $1",
			wantTables:     []string{"posts"},
			wantJoins:      []string{"users", "posts"},
			wantParameters: []string{"u.id"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newPostgresCatalogue(), testCase.sql)

			tables := make([]string, 0, len(analysis.FromTables))
			for _, table := range analysis.FromTables {
				tables = append(tables, table.Name)
			}
			joins := make([]string, 0, len(analysis.JoinClauses))
			for _, join := range analysis.JoinClauses {
				joins = append(joins, join.Table.Name)
			}
			assert.Equal(t, testCase.wantTables, tables)
			assert.Equal(t, testCase.wantJoins, joins)
			assert.Equal(t, testCase.wantParameters, parameterColumns(analysis))
		})
	}
}

func TestAnalyseQueryAcceptsValidPostgres(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"SELECT date_trunc('month', created_at)::date AS month, count(*)::integer AS n FROM users GROUP BY date_trunc('month', created_at) ORDER BY month",
		"SELECT name, count(*) FROM users GROUP BY ROLLUP (name), CUBE (id)",
		"SELECT name FROM users GROUP BY GROUPING SETS ((name), (id), ())",
		"SELECT name FROM users GROUP BY DISTINCT name HAVING count(*) > $1",
		"SELECT row_number() OVER w AS n FROM users WINDOW w AS (PARTITION BY name ORDER BY id)",
		"SELECT sum(id) OVER (PARTITION BY name ORDER BY id ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) AS s FROM users",
		"SELECT count(*) FILTER (WHERE id > $1) AS c FROM users",
		"SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY id) AS median FROM users",
		"SELECT string_agg(name, ',' ORDER BY name DESC NULLS LAST) AS names FROM users",
		"SELECT tags @> ARRAY[1, 2] AS a, tags <@ $1 AS b, tags && $2 AS c FROM users",
		"SELECT name ~ '^a' AS a, name ~* 'b' AS b, name !~ 'c' AS c, name !~* 'd' AS d FROM users",
		"SELECT id ^ 2 AS squared FROM users",
		"SELECT id << 1 AS shifted, id & 3 AS masked, id # 1 AS flipped FROM users",
		"SELECT data -> 'a' AS a, data ->> 'b' AS b, data #> '{c}' AS c, data #>> '{d}' AS d FROM users",
		"SELECT id FROM users WHERE id = ANY($1) AND name <> ALL($2::text[])",
		"SELECT id FROM users WHERE id = ANY(ARRAY[1, 2, 3])",
		"SELECT tags[1] AS first, tags[1:2] AS pair, tags[:2] AS head FROM users",
		"SELECT name IS DISTINCT FROM $1 AS a, name IS NOT DISTINCT FROM $2 AS b FROM users",
		"SELECT id IS NOT TRUE AS a, id IS UNKNOWN AS b FROM users",
		"SELECT id FROM users WHERE id BETWEEN SYMMETRIC $1 AND $2",
		`SELECT id FROM users WHERE name LIKE $1 ESCAPE '\' AND name NOT ILIKE $2`,
		"SELECT id FROM users WHERE name SIMILAR TO $1",
		"SELECT EXTRACT(YEAR FROM created_at) AS y, EXTRACT(EPOCH FROM now()) AS e FROM users",
		"SELECT POSITION('a' IN name) AS p, SUBSTRING(name FROM 1 FOR 2) AS s, TRIM(BOTH 'x' FROM name) AS t FROM users",
		"SELECT OVERLAY(name PLACING 'x' FROM 1 FOR 1) AS o, TRIM(name) AS t2, TRIM(LEADING FROM name) AS t3 FROM users",
		"SELECT CAST(id AS bigint) AS b, id::int[] AS a, $1::timestamp with time zone AS ts FROM users",
		"SELECT interval '1 day' AS i, TIMESTAMP '2020-01-01' AS t, DATE '2020-01-01' AS d, now() - interval '1 hour' AS h",
		"SELECT ROW(1, 2) AS r, (id, name) = ($1, $2) AS same FROM users",
		"SELECT id FROM users WHERE (id, name) IN (SELECT id, title FROM posts)",
		"SELECT CASE WHEN id > $1 THEN 'a' WHEN id < 0 THEN 'b' ELSE 'c' END AS c, CASE id WHEN 1 THEN 'x' END AS d FROM users",
		"SELECT NULLIF(name, '') AS n, GREATEST(id, 1) AS g, LEAST(id, 2) AS l, COALESCE(name, $1) AS c FROM users",
		"SELECT ARRAY(SELECT id FROM posts) AS ids, ARRAY[]::integer[] AS empty FROM users",
		"SELECT id FROM users WHERE NOT EXISTS (SELECT 1 FROM posts WHERE posts.user_id = users.id)",
		"SELECT DISTINCT ON (name) name, id FROM users ORDER BY name, id DESC",
		"SELECT id FROM users UNION ALL SELECT id FROM posts ORDER BY id LIMIT $1 OFFSET $2",
		"(SELECT id FROM users) UNION (SELECT id FROM posts)",
		"SELECT id FROM users ORDER BY id OFFSET 5 ROWS FETCH NEXT 10 ROWS ONLY",
		"SELECT id FROM users ORDER BY id FETCH FIRST 5 ROWS WITH TIES",
		"SELECT id FROM users LIMIT ALL",
		"SELECT id FROM users FOR UPDATE OF users SKIP LOCKED",
		"SELECT id FROM users FOR NO KEY UPDATE NOWAIT",
		"SELECT id FROM users LIMIT 1 FOR UPDATE",
		"SELECT id FROM users FOR UPDATE LIMIT 1",
		"SELECT id FROM users FOR SHARE OF users FOR UPDATE OF posts",
		"SELECT u.id FROM users u JOIN posts p USING (id) NATURAL JOIN posts q",
		"SELECT u.id FROM users u CROSS JOIN LATERAL (SELECT id FROM posts WHERE posts.user_id = u.id) AS x",
		"SELECT u.id FROM users u LEFT JOIN LATERAL generate_series(1, u.id) AS g(n) ON true",
		"SELECT n FROM generate_series(1, 10) AS g(n)",
		"SELECT v, i FROM unnest($1::int[]) WITH ORDINALITY AS t(v, i)",
		"SELECT x FROM (VALUES (1), (2)) AS v(x)",
		"SELECT id FROM ONLY users",
		"SELECT id FROM users TABLESAMPLE SYSTEM (10)",
		"SELECT id FROM users AS u (a, b)",
		"SELECT a.* FROM users a",
		"SELECT * FROM users WHERE id = $1",
		"SELECT count(DISTINCT id) AS c, count(ALL id) AS d FROM users",
		"SELECT CURRENT_TIMESTAMP AS a, CURRENT_DATE AS b, LOCALTIMESTAMP(3) AS c, CURRENT_TIME(0) AS d",
		"SELECT id::text || '-' || name AS label, -id AS negative, +id AS positive, ~id AS inverted FROM users",
		"SELECT $1::text AS a, CAST($2 AS integer) AS b",
		"SELECT 'a' AS x WHERE true",
		"SELECT id FROM users WHERE name IS NULL OR name ISNULL OR name NOTNULL",
		"WITH RECURSIVE t(n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM t WHERE n < $1) SELECT n FROM t",
		"WITH a AS MATERIALIZED (SELECT id FROM users), b AS NOT MATERIALIZED (SELECT id FROM a) SELECT id FROM b",
		"WITH moved AS (DELETE FROM users WHERE id = $1 RETURNING id) INSERT INTO posts (id) SELECT id FROM moved",
		"INSERT INTO users (id, name) VALUES ($1, $2), ($3, $4) ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name WHERE users.name <> EXCLUDED.name RETURNING *",
		"INSERT INTO users (id) VALUES ($1) ON CONFLICT ON CONSTRAINT users_pkey DO NOTHING",
		"INSERT INTO users (id) VALUES ($1) ON CONFLICT (id) WHERE id > 0 DO NOTHING RETURNING id",
		"INSERT INTO users DEFAULT VALUES RETURNING id",
		"INSERT INTO users (id, name) SELECT id, title FROM posts WHERE id = $1",
		"INSERT INTO users AS u (id) OVERRIDING SYSTEM VALUE VALUES (DEFAULT)",
		"UPDATE users SET (name, email) = ($1, $2) WHERE id = $3",
		"UPDATE users SET name = DEFAULT, email = NULL WHERE id = $1 RETURNING id, name",
		"UPDATE users u SET name = p.title FROM posts p WHERE p.user_id = u.id AND p.id = $1",
		"UPDATE users SET name = $1 WHERE CURRENT OF users_cursor",
		"DELETE FROM users u USING posts p WHERE p.user_id = u.id AND p.id = $1 RETURNING u.id",
		"VALUES (1, 'a'), (2, 'b') ORDER BY 1 LIMIT 1",
		"SELECT id FROM users WHERE created_at > now() - $1::interval",
		"SELECT (SELECT max(id) FROM posts) AS m, id FROM users",
		"SELECT pg_typeof(id) AS t, id::regclass AS r FROM users",
		"SELECT jsonb_build_object('a', id) AS doc FROM users WHERE data @> '{\"a\": 1}'::jsonb",
		"SELECT id FROM users WHERE name ILIKE ANY ($1)",
		"SELECT id FROM users WHERE (created_at, updated_at) OVERLAPS ($1, $2)",
		"SELECT id FROM users WHERE id IN (1, 2, 3) AND NOT (id = 4 OR id = 5)",
		"SELECT id FROM users WHERE name COLLATE \"C\" < $1",
		"SELECT xmin, ctid FROM users",
		"SELECT users.id FROM public.users",
		"SELECT format('%s-%s', id, name) AS f, concat_ws(',', VARIADIC ARRAY['a', 'b']) AS c FROM users",
		"SELECT id FROM users WHERE id = $1;",
	} {
		t.Run(sql, func(t *testing.T) {
			t.Parallel()

			require.NoError(t, analyseQueryError(t, sql))
		})
	}
}

func TestAnalyseQuery_ANameWrittenInsideAndOutsideANestedQueryIsOneParameter(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		sql     string
		numbers map[int]bool
	}{
		{name: "a predicate subquery", sql: "SELECT id FROM users WHERE id = :id AND id IN (SELECT user_id FROM posts WHERE user_id = :id)",
			numbers: map[int]bool{1: true}},
		{name: "a scalar subquery", sql: "SELECT (SELECT count(*) FROM posts WHERE user_id = :id) AS total FROM users WHERE id = :id",
			numbers: map[int]bool{1: true}},
		{name: "a CTE body", sql: "WITH mine AS (SELECT id FROM posts WHERE user_id = :id) SELECT id FROM users WHERE id = :id",
			numbers: map[int]bool{1: true}},
		{name: "a derived table", sql: "SELECT d.id FROM (SELECT id FROM users WHERE id = :id) d WHERE d.id = :id",
			numbers: map[int]bool{1: true}},
		{name: "an EXISTS subquery", sql: "SELECT id FROM users WHERE name = :name AND EXISTS (SELECT 1 FROM posts WHERE title = :name)",
			numbers: map[int]bool{1: true}},
		{name: "two CTE bodies number their names apart",
			sql:     "WITH a AS (SELECT id FROM posts WHERE title = :title), b AS (SELECT id FROM users WHERE id = :id) SELECT a.id FROM a, b WHERE a.id = :id",
			numbers: map[int]bool{1: true, 2: true}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])
			require.NoError(t, err)
			numbers := map[int]bool{}
			for _, reference := range analysis.ParameterReferences {
				numbers[reference.Number] = true
			}
			require.Equal(t, testCase.numbers, numbers)
		})
	}
}

func TestColonAfterAnOperandIsNoNamedParameter(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		sql            string
		wantParameters int
	}{
		{name: "a slice between two names", sql: "SELECT tags[lo:hi] AS part FROM users", wantParameters: 0},
		{name: "a slice between a name and a number", sql: "SELECT tags[lo:2] AS part FROM users", wantParameters: 0},
		{name: "a slice with an open upper bound", sql: "SELECT tags[lo:] AS part FROM users", wantParameters: 0},
		{name: "a named parameter after an operator", sql: "SELECT id FROM users WHERE id=:id AND name = :name", wantParameters: 2},
		{name: "a named parameter in a list", sql: "SELECT id FROM users WHERE id IN (:first,:second)", wantParameters: 2},
		{name: "a named parameter cast", sql: "SELECT id FROM users WHERE id = :id::int", wantParameters: 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, nil, testCase.sql)
			require.Len(t, analysis.ParameterReferences, testCase.wantParameters)
		})
	}
}

func TestAParameterInAnExpressionCarriesItsNumber(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, nil, "SELECT id FROM crossings WHERE moods[1] = $2 AND moods[2] = :second AND moods[3] IN (:second, $1)")

	var numbers []int
	for _, clause := range analysis.ClauseExpressions {
		collectParameterNumbers(clause.Expression, &numbers)
	}
	require.Equal(t, []int{2, 3, 3, 1}, numbers)
}

func collectParameterNumbers(expression ast.Expression, numbers *[]int) {
	switch typed := expression.(type) {
	case *ast.ParameterExpression:
		*numbers = append(*numbers, typed.Number)
	case *ast.LogicalOpExpression:
		for _, operand := range typed.Operands {
			collectParameterNumbers(operand, numbers)
		}
	case *ast.ComparisonExpression:
		collectParameterNumbers(typed.Left, numbers)
		collectParameterNumbers(typed.Right, numbers)
	case *ast.InListExpression:
		collectParameterNumbers(typed.Inner, numbers)
		for _, value := range typed.Values {
			collectParameterNumbers(value, numbers)
		}
	}
}

func TestValuesListsTablesAndQueriesInParenthesesStandWhereverAQueryDoes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		sql        string
		columns    int
		branches   int
		parameters int
		derived    int
	}{
		{name: "a VALUES list before a set operator", sql: "VALUES (1, 'a') UNION ALL SELECT id, name FROM users", columns: 2, branches: 1},
		{name: "a VALUES list after a set operator", sql: "SELECT id, name FROM users UNION ALL VALUES (1, 'a'), (2, 'b')", columns: 2, branches: 1},
		{name: "a VALUES list ordered and limited", sql: "VALUES (3), (1), (2) ORDER BY 1 DESC LIMIT 2 OFFSET 1", columns: 1},
		{name: "a VALUES list in parentheses as an arm", sql: "(VALUES (1)) UNION SELECT 2", columns: 1, branches: 1},
		{name: "a VALUES list anchoring a recursive common table expression",
			sql: "WITH RECURSIVE c(n) AS (VALUES (1) UNION ALL SELECT n + 1 FROM c WHERE n < 3) SELECT n FROM c", columns: 1},
		{name: "two VALUES lists as a common table expression", sql: "WITH t(n) AS (VALUES (1) UNION ALL VALUES (2)) SELECT n FROM t", columns: 1},
		{name: "a VALUES list binding every row", sql: "WITH d(a, b) AS (VALUES ($1, $2), ($3, $4)) SELECT a, b FROM d", columns: 2, parameters: 4},
		{name: "a set operation of queries in parentheses as a derived table",
			sql: "SELECT x.id FROM ((SELECT id FROM users ORDER BY id LIMIT 1) UNION (SELECT id FROM posts)) AS x", columns: 1, derived: 1},
		{name: "a query in double parentheses as a derived table", sql: "SELECT * FROM ((SELECT 1 AS one)) AS y", columns: 1, derived: 1},
		{name: "a TABLE command as a derived table", sql: "SELECT * FROM (TABLE users) AS t", columns: 1, derived: 1},
		{name: "a TABLE command as a scalar subquery", sql: "SELECT id, (TABLE posts) AS first FROM users", columns: 2},
		{name: "a TABLE command tested by EXISTS and IN", sql: "SELECT id FROM users WHERE EXISTS (TABLE posts) AND id IN (TABLE posts)", columns: 1},
		{name: "a set operation of TABLE commands", sql: "TABLE users UNION ALL TABLE users", columns: 1, branches: 1},
		{name: "a set operation of queries in parentheses tested by IN",
			sql: "SELECT id FROM users WHERE id IN ((SELECT id FROM posts) UNION (SELECT 2))", columns: 1},
		{name: "an expression starting with a subquery", sql: "SELECT ((SELECT max(id) FROM users) + 1) AS next", columns: 1},
		{name: "a parenthesised join starting with a derived table",
			sql: "SELECT x.id FROM ((SELECT id FROM users) AS a JOIN posts USING (id)) AS x", columns: 1, derived: 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newPostgresCatalogue(), testCase.sql)
			require.Len(t, analysis.OutputColumns, testCase.columns)
			require.Len(t, analysis.CompoundBranches, testCase.branches)
			require.Len(t, analysis.ParameterReferences, testCase.parameters)
			require.Len(t, analysis.RawDerivedTables, testCase.derived)
			require.True(t, analysis.ReadOnly)
		})
	}
}

func TestAValuesListNamesItsColumnsAsPostgreSQLDoes(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, newPostgresCatalogue(), "SELECT 1, 'a' UNION ALL VALUES (2, 'b')")
	require.Len(t, analysis.CompoundBranches, 1)
	require.Equal(t, []string{"column1", "column2"}, outputNames(analysis.CompoundBranches[0].Query))
}

func TestAValuesListNeedsItsRowsInParentheses(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{"VALUES", "VALUES 1", "SELECT 1 UNION VALUES (2", "SELECT 1 UNION ALL VALUES (1), 2"} {
		t.Run(sql, func(t *testing.T) {
			t.Parallel()

			require.Error(t, analyseQueryError(t, sql))
		})
	}
}

func TestATableCommandInParenthesesSelectsEveryColumnOfItsTable(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, newPostgresCatalogue(), "SELECT * FROM (TABLE users) AS t")

	require.Len(t, analysis.RawDerivedTables, 1)
	inner := analysis.RawDerivedTables[0].InnerQuery
	require.NotNil(t, inner)
	require.Equal(t, []ast.TableReference{{Schema: "", Name: "users", Alias: ""}}, inner.FromTables)
	require.Len(t, inner.OutputColumns, 1)
	require.True(t, inner.OutputColumns[0].IsStar)
}

func TestAParenthesisedInsertQueryIsAnalysedAsTheBareOneIs(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		sql             string
		wantColumns     []string
		wantFromTables  []string
		wantReturning   bool
		wantInsertQuery bool
	}{
		{
			name:            "a query in parentheses after the column list",
			sql:             "INSERT INTO users (id, name) (SELECT $1, $2)",
			wantColumns:     []string{"users.id", "users.name"},
			wantInsertQuery: true,
		},
		{
			name:            "a query in doubled parentheses",
			sql:             "INSERT INTO users (id, name) ((SELECT $1, $2))",
			wantColumns:     []string{"users.id", "users.name"},
			wantInsertQuery: true,
		},
		{
			name:            "parenthesised arms of a UNION",
			sql:             "INSERT INTO users (id, name) (SELECT $1, $2) UNION ALL (SELECT $3, $4)",
			wantColumns:     []string{"users.id", "users.name", "users.id", "users.name"},
			wantInsertQuery: true,
		},
		{
			name:            "a parenthesised query reading a table, then RETURNING",
			sql:             "INSERT INTO posts (user_id, title) (SELECT id, $1 FROM users WHERE name = $2) RETURNING id",
			wantColumns:     []string{"posts.title", "name"},
			wantFromTables:  []string{"users"},
			wantReturning:   true,
			wantInsertQuery: true,
		},
		{
			name:            "a parenthesised query with no column list",
			sql:             "INSERT INTO users (SELECT id, name, email FROM users WHERE id = $1)",
			wantColumns:     []string{"id"},
			wantFromTables:  []string{"users"},
			wantInsertQuery: true,
		},
		{
			name:            "a TABLE command",
			sql:             "INSERT INTO users TABLE users",
			wantColumns:     []string{},
			wantFromTables:  []string{"users"},
			wantInsertQuery: true,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newPostgresCatalogue(), testCase.sql)

			require.Equal(t, testCase.wantInsertQuery, analysis.InsertSelect != nil)
			assert.Equal(t, testCase.wantColumns, parameterColumns(analysis))
			assert.Equal(t, testCase.wantReturning, analysis.HasReturning)
			if testCase.wantFromTables != nil {
				tables := make([]string, 0, len(analysis.InsertSelect.FromTables))
				for _, table := range analysis.InsertSelect.FromTables {
					tables = append(tables, table.Name)
				}
				assert.Equal(t, testCase.wantFromTables, tables)
			}
		})
	}
}

func TestParserDepthLimitPreventsStackOverflow(t *testing.T) {
	t.Parallel()

	const depth = 100_000
	engine := New()

	t.Run("nested expression parentheses", func(t *testing.T) {
		t.Parallel()
		sql := "SELECT " + strings.Repeat("(", depth) + "1" + strings.Repeat(")", depth) + " FROM t"
		statements, err := engine.ParseStatements(t.Context(), sql)
		require.NoError(t, err)
		require.NotEmpty(t, statements)

		_, _ = engine.AnalyseQuery(t.Context(), nil, statements[0])
	})

	t.Run("nested derived tables", func(t *testing.T) {
		t.Parallel()

		sql := buildNestedDerivedTables(deepNestingLevels)
		statements, err := engine.ParseStatements(t.Context(), sql)
		require.NoError(t, err)
		require.NotEmpty(t, statements)
		_, _ = engine.AnalyseQuery(t.Context(), nil, statements[0])
	})
}

func TestParserDepthLimitRejectsNestedSubquerySubtypes(t *testing.T) {
	t.Parallel()

	engine := New()

	t.Run("chained EXISTS subqueries", func(t *testing.T) {
		t.Parallel()

		var builder strings.Builder
		builder.WriteString("SELECT 1 WHERE 1 = 1")
		for range deepNestingLevels {
			previous := builder.String()
			builder.Reset()
			builder.WriteString("SELECT 1 WHERE EXISTS (")
			builder.WriteString(previous)
			builder.WriteString(")")
		}
		statements, err := engine.ParseStatements(t.Context(), builder.String())
		require.NoError(t, err)
		require.NotEmpty(t, statements)
		_, _ = engine.AnalyseQuery(t.Context(), nil, statements[0])
	})

	t.Run("chained scalar subqueries", func(t *testing.T) {
		t.Parallel()

		const depth = 100_000
		sql := "SELECT " + strings.Repeat("(SELECT ", depth) + "1" + strings.Repeat(")", depth)
		statements, err := engine.ParseStatements(t.Context(), sql)
		require.NoError(t, err)
		require.NotEmpty(t, statements)
		_, _ = engine.AnalyseQuery(t.Context(), nil, statements[0])
	})
}

func TestParserDepthLimitIsConfigurable(t *testing.T) {
	t.Parallel()

	engine := New(WithMaxParseDepth(8))

	t.Run("expression nesting", func(t *testing.T) {
		t.Parallel()
		sql := "SELECT " + strings.Repeat("(", 64) + "1" + strings.Repeat(")", 64) + " FROM t"
		statements, err := engine.ParseStatements(t.Context(), sql)
		require.NoError(t, err)
		require.NotEmpty(t, statements)
		_, _ = engine.AnalyseQuery(t.Context(), nil, statements[0])
	})

	t.Run("subquery nesting", func(t *testing.T) {
		t.Parallel()
		sql := buildNestedDerivedTables(64)
		statements, err := engine.ParseStatements(t.Context(), sql)
		require.NoError(t, err)
		require.NotEmpty(t, statements)
		_, _ = engine.AnalyseQuery(t.Context(), nil, statements[0])
	})
}

func buildNestedDerivedTables(depth int) string {
	var builder strings.Builder
	builder.WriteString("SELECT * FROM t")
	for range depth {
		previous := builder.String()
		builder.Reset()
		builder.WriteString("SELECT * FROM (")
		builder.WriteString(previous)
		builder.WriteString(") sub")
	}
	return builder.String()
}

func TestAnInsertRecordsWhatItsConflictClauseDoes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		sql        string
		wantAction ast.ConflictAction
	}{
		{name: "no conflict clause", sql: "INSERT INTO users (id) VALUES ($1) RETURNING id", wantAction: ast.ConflictActionNone},
		{name: "DO NOTHING on a target", sql: "INSERT INTO users (id) VALUES ($1) ON CONFLICT (id) DO NOTHING RETURNING id", wantAction: ast.ConflictActionDoNothing},
		{name: "DO NOTHING on a constraint", sql: "INSERT INTO users (id) VALUES ($1) ON CONFLICT ON CONSTRAINT users_pkey DO NOTHING", wantAction: ast.ConflictActionDoNothing},
		{name: "DO NOTHING with no target", sql: "INSERT INTO users (id) VALUES ($1) ON CONFLICT DO NOTHING", wantAction: ast.ConflictActionDoNothing},
		{name: "DO UPDATE", sql: "INSERT INTO users (id, name) VALUES ($1, $2) ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name WHERE users.id > 0", wantAction: ast.ConflictActionDoUpdate},
		{name: "a predicate on the target", sql: "INSERT INTO users (id) VALUES ($1) ON CONFLICT (id) WHERE id > 0 DO NOTHING", wantAction: ast.ConflictActionDoNothing},
		{name: "an INSERT whose WITH list changes data", sql: "WITH gone AS (DELETE FROM users WHERE id = $1 RETURNING id) INSERT INTO users (id) SELECT id FROM gone", wantAction: ast.ConflictActionUnrecorded},
		{name: "a query", sql: "SELECT id FROM users", wantAction: ast.ConflictActionUnrecorded},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newPostgresCatalogue(), testCase.sql)

			assert.Equal(t, testCase.wantAction, analysis.ConflictAction)
		})
	}
}

func TestAStatementAnInsertKeywordOpensRecordsThatItReplacesAConflictingRow(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		sql        string
		wantAction ast.ConflictAction
	}{
		{name: "rows", sql: "UPSERT INTO users (id, name) VALUES ($1, $2) RETURNING id", wantAction: ast.ConflictActionDoUpdate},
		{name: "a query", sql: "UPSERT INTO users (id, name) SELECT id, name FROM users WHERE id = $1", wantAction: ast.ConflictActionDoUpdate},
		{name: "after a WITH list that only reads", sql: "WITH chosen AS (SELECT $1::int4 AS id) UPSERT INTO users (id) SELECT id FROM chosen",
			wantAction: ast.ConflictActionDoUpdate},
		{name: "after a WITH list that changes data", sql: "WITH gone AS (DELETE FROM users WHERE id = $1 RETURNING id) UPSERT INTO users (id) SELECT id FROM gone",
			wantAction: ast.ConflictActionUnrecorded},
		{name: "an INSERT beside it", sql: "INSERT INTO users (id) VALUES ($1)", wantAction: ast.ConflictActionNone},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, err := analyseQueryWith(t, New(WithInsertKeywords("upsert")), testCase.sql)
			require.NoError(t, err)

			assert.Equal(t, testCase.wantAction, analysis.ConflictAction)
		})
	}
}

func TestAConflictClauseWithoutAnActionFailsTheStatement(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		wantMessage string
	}{
		{name: "no DO", sql: "INSERT INTO users (id) VALUES ($1) ON CONFLICT (id)", wantMessage: "expected keyword DO"},
		{name: "an action that is neither NOTHING nor UPDATE", sql: "INSERT INTO users (id) VALUES ($1) ON CONFLICT (id) DO DELETE", wantMessage: "expected keyword UPDATE"},
		{name: "ON with no CONFLICT", sql: "INSERT INTO users (id) VALUES ($1) ON DUPLICATE KEY UPDATE id = 1", wantMessage: "expected keyword CONFLICT"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := analyseWithContext(t.Context(), New(), testCase.sql)

			var parseError *dialectkit.ParseError
			require.ErrorAs(t, err, &parseError)
			assert.Equal(t, testCase.wantMessage, parseError.Message)
		})
	}
}

func TestACallOfAProcedureFindsItsResultByTheCaseOfItsName(t *testing.T) {
	t.Parallel()

	quoted := sqlmodel.NewFunction(nil, sqlmodel.NewIntegerType("int4", 32, false), sqlmodel.FunctionNullableCalledOnNull,
		sqlmodel.WithFunctionName("Archive"))
	unquoted := sqlmodel.NewFunction(nil, sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"), sqlmodel.FunctionNullableCalledOnNull,
		sqlmodel.WithFunctionName("archive"))
	catalogue := newPostgresCatalogue()
	catalogue.Identifiers = New().Info().Identifiers
	catalogue.Schemas["public"].Functions = map[string][]*sqlmodel.Function{"Archive": {quoted}, "archive": {unquoted}}

	testCases := []struct {
		name         string
		sql          string
		wantFunction string
	}{
		{name: "a quoted call of the quoted procedure", sql: `CALL "Archive"()`, wantFunction: "Archive"},
		{name: "an unquoted call of the unquoted procedure", sql: `CALL ARCHIVE()`, wantFunction: "archive"},
		{name: "a quoted call in another case finds no procedure", sql: `CALL "ARCHIVE"()`, wantFunction: ""},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, catalogue, testCase.sql)

			if testCase.wantFunction == "" {
				assert.Empty(t, analysis.RawTableValuedFunctions)
				return
			}
			require.Len(t, analysis.RawTableValuedFunctions, 1)
			assert.Equal(t, testCase.wantFunction, analysis.RawTableValuedFunctions[0].FunctionName)
		})
	}
}
