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

package duckdb

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/sqlmodel"
)

func buildTestCatalogue() *sqlmodel.Catalogue {
	return &sqlmodel.Catalogue{
		DefaultSchema: "main",
		Schemas: map[string]*sqlmodel.Schema{
			"main": {
				Name: "main",
				Tables: map[string]*sqlmodel.Table{
					"users": {
						Name:   "users",
						Schema: "main",
						Columns: []sqlmodel.Column{
							{Name: "id", SQLType: sqlmodel.NewIntegerType("int4", 32, false), Nullable: false},
							{Name: "name", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "varchar"}, Nullable: false},
							{Name: "email", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "varchar"}, Nullable: true},
							{Name: "age", SQLType: sqlmodel.NewIntegerType("int4", 32, false), Nullable: true},
							{Name: "active", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryBoolean, EngineName: "bool"}, Nullable: false},
						},
						PrimaryKey: []string{"id"},
					},
					"orders": {
						Name:   "orders",
						Schema: "main",
						Columns: []sqlmodel.Column{
							{Name: "id", SQLType: sqlmodel.NewIntegerType("int4", 32, false), Nullable: false},
							{Name: "user_id", SQLType: sqlmodel.NewIntegerType("int4", 32, false), Nullable: false},
							{Name: "total", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryDecimal, EngineName: "numeric"}, Nullable: true},
							{Name: "created_at", SQLType: sqlmodel.NewTemporalType("timestamp", sqlmodel.TemporalKindTimestamp, false), Nullable: false},
						},
						PrimaryKey: []string{"id"},
					},
				},
				Views:          map[string]*sqlmodel.View{},
				Enums:          map[string]*sqlmodel.Enum{},
				Functions:      map[string][]*sqlmodel.Function{},
				CompositeTypes: map[string]*sqlmodel.CompositeType{},
				Sequences:      map[string]*sqlmodel.Sequence{},
			},
		},
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
	require.NotNil(t, analysis)
	return analysis
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
		`SELECT a.id FROM accounts a WHERE a.id = (SELECT MAX(av2.id) FROM account_versions av2 WHERE av2.id < $1)`)

	require.Len(t, analysis.PredicateSubqueries, 1)
	inner := analysis.PredicateSubqueries[0]
	require.NotNil(t, inner)
	require.Len(t, inner.FromTables, 1)
	assert.Equal(t, "account_versions", inner.FromTables[0].Name)
	assert.Equal(t, "av2", inner.FromTables[0].Alias)
	require.NotEmpty(t, analysis.ParameterReferences)
}

func TestAnalyseQuery_WhereParenthesisedExpressionNotCaptured(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, nil, `SELECT a.id FROM accounts a WHERE (a.id + 1) = $1`)
	assert.Empty(t, analysis.PredicateSubqueries)
}

func TestAnalyseQuery_SetCaseComparisonParameterTypedFromOperand(t *testing.T) {
	t.Parallel()

	catalogue := buildTestCatalogue()
	analysis := analyseQuery(t, catalogue,
		`UPDATE users SET name = CASE WHEN id >= $1 THEN 'flagged' ELSE name END WHERE email = $2`)

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

	catalogue := buildTestCatalogue()

	t.Run("simple column selection", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id, name FROM users`)

		require.Len(t, analysis.OutputColumns, 2)
		assert.Equal(t, "id", analysis.OutputColumns[0].Name)
		assert.Equal(t, "name", analysis.OutputColumns[1].Name)
		require.Len(t, analysis.FromTables, 1)
		assert.Equal(t, "users", analysis.FromTables[0].Name)
		assert.True(t, analysis.ReadOnly, "SELECT should be read-only")
	})

	t.Run("star expansion", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT * FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.OutputColumns[0].IsStar, "should detect star expansion")
		require.Len(t, analysis.FromTables, 1)
		assert.Equal(t, "users", analysis.FromTables[0].Name)
	})

	t.Run("table-qualified star", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT u.* FROM users u`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.OutputColumns[0].IsStar)
		assert.Equal(t, "u", analysis.OutputColumns[0].TableAlias)
	})

	t.Run("WHERE with dollar parameter", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id, name FROM users WHERE id = $1`)

		require.Len(t, analysis.OutputColumns, 2)
		require.Len(t, analysis.ParameterReferences, 1)
		assert.Equal(t, 1, analysis.ParameterReferences[0].Number)
	})

	t.Run("multiple parameters", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users WHERE name = $1 AND age > $2`)

		require.Len(t, analysis.ParameterReferences, 2)
		assert.Equal(t, 1, analysis.ParameterReferences[0].Number)
		assert.Equal(t, 2, analysis.ParameterReferences[1].Number)
	})

	t.Run("INNER JOIN", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT u.name, o.total FROM users u JOIN orders o ON u.id = o.user_id`)

		require.Len(t, analysis.FromTables, 1)
		assert.Equal(t, "users", analysis.FromTables[0].Name)
		assert.Equal(t, "u", analysis.FromTables[0].Alias)
		require.Len(t, analysis.JoinClauses, 1)
		assert.Equal(t, ast.JoinInner, analysis.JoinClauses[0].Kind)
		assert.Equal(t, "orders", analysis.JoinClauses[0].Table.Name)
		assert.Equal(t, "o", analysis.JoinClauses[0].Table.Alias)
	})

	t.Run("LEFT JOIN", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT u.name, o.total FROM users u LEFT JOIN orders o ON u.id = o.user_id`)

		require.Len(t, analysis.JoinClauses, 1)
		assert.Equal(t, ast.JoinLeft, analysis.JoinClauses[0].Kind)
	})

	t.Run("CROSS JOIN", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT u.name, o.total FROM users u CROSS JOIN orders o`)

		require.Len(t, analysis.JoinClauses, 1)
		assert.Equal(t, ast.JoinCross, analysis.JoinClauses[0].Kind)
		assert.Equal(t, "orders", analysis.JoinClauses[0].Table.Name)
	})

	t.Run("aggregate function in output", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT count(*) FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.Equal(t, "count", analysis.OutputColumns[0].Name)
	})

	t.Run("column alias", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT name AS user_name FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.Equal(t, "user_name", analysis.OutputColumns[0].Name)
	})

	t.Run("CTE with SELECT", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `WITH active AS (
			SELECT id, name FROM users WHERE active = true
		)
		SELECT id, name FROM active`)

		require.Len(t, analysis.CTEDefinitions, 1)
		assert.Equal(t, "active", analysis.CTEDefinitions[0].Name)
		require.Len(t, analysis.CTEDefinitions[0].OutputColumns, 2)
		assert.Equal(t, "id", analysis.CTEDefinitions[0].OutputColumns[0].Name)
		assert.Equal(t, "name", analysis.CTEDefinitions[0].OutputColumns[1].Name)
	})

	t.Run("ORDER BY and LIMIT", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id, name FROM users ORDER BY name ASC LIMIT 10`)

		require.Len(t, analysis.OutputColumns, 2)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("GROUP BY", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT active, count(*) FROM users GROUP BY active`)

		require.Len(t, analysis.GroupByColumns, 1)
		assert.Equal(t, "active", analysis.GroupByColumns[0].ColumnName)
	})

	t.Run("comma-joined tables", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT u.name, o.total FROM users u, orders o WHERE u.id = o.user_id`)

		require.Len(t, analysis.FromTables, 2)
		assert.Equal(t, "users", analysis.FromTables[0].Name)
		assert.Equal(t, "orders", analysis.FromTables[1].Name)
	})

	t.Run("UNION ALL compound query", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id, name FROM users WHERE active = true
			UNION ALL
			SELECT id, name FROM users WHERE active = false`)

		require.Len(t, analysis.CompoundBranches, 1)
		assert.Equal(t, ast.CompoundUnionAll, analysis.CompoundBranches[0].Operator)
		require.NotNil(t, analysis.CompoundBranches[0].Query)
	})

	t.Run("LIMIT with parameter", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users LIMIT $1`)

		require.Len(t, analysis.ParameterReferences, 1)
		assert.Equal(t, sqlmodel.ParameterContextLimit, analysis.ParameterReferences[0].Context)
	})

	t.Run("OFFSET with parameter", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users LIMIT 10 OFFSET $1`)

		require.Len(t, analysis.ParameterReferences, 1)
		assert.Equal(t, sqlmodel.ParameterContextOffset, analysis.ParameterReferences[0].Context)
	})

	t.Run("schema-qualified table in FROM", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM main.users`)

		require.Len(t, analysis.FromTables, 1)
		assert.Equal(t, "main", analysis.FromTables[0].Schema)
		assert.Equal(t, "users", analysis.FromTables[0].Name)
	})
}

func TestAnalyseQuery_Select_ClausesAndExpressions(t *testing.T) {
	t.Parallel()

	catalogue := buildTestCatalogue()

	t.Run("DISTINCT modifier", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT DISTINCT name FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.Equal(t, "name", analysis.OutputColumns[0].Name)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("HAVING clause", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT user_id, COUNT(*) FROM orders GROUP BY user_id HAVING COUNT(*) > 1`)

		require.Len(t, analysis.GroupByColumns, 1)
		assert.Equal(t, "user_id", analysis.GroupByColumns[0].ColumnName)
	})

	t.Run("GROUP BY multiple columns", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT active, name, COUNT(*) FROM users GROUP BY active, name`)

		require.Len(t, analysis.GroupByColumns, 2)
		assert.Equal(t, "active", analysis.GroupByColumns[0].ColumnName)
		assert.Equal(t, "name", analysis.GroupByColumns[1].ColumnName)
	})

	t.Run("window function with OVER", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id, ROW_NUMBER() OVER (ORDER BY id) FROM users`)

		require.Len(t, analysis.OutputColumns, 2)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("window function with PARTITION BY", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT user_id, total, ROW_NUMBER() OVER (PARTITION BY user_id ORDER BY total DESC) FROM orders`)

		require.Len(t, analysis.OutputColumns, 3)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("CASE expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id, CASE WHEN active = true THEN 'yes' ELSE 'no' END FROM users`)

		require.Len(t, analysis.OutputColumns, 2)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("COALESCE expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT COALESCE(email, 'none') FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("CAST expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT CAST(id AS VARCHAR) FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("IS NULL condition", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users WHERE email IS NULL`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("IS NOT NULL condition", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users WHERE email IS NOT NULL`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("BETWEEN with parameters in WHERE", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users WHERE id BETWEEN $1 AND $2`)

		require.Len(t, analysis.ParameterReferences, 2)

		assert.Equal(t, 1, analysis.ParameterReferences[0].Number)
		assert.Equal(t, 2, analysis.ParameterReferences[1].Number)
	})

	t.Run("IN list with parameters", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users WHERE id IN ($1, $2, $3)`)

		require.Len(t, analysis.ParameterReferences, 3)
		assert.Equal(t, sqlmodel.ParameterContextInList, analysis.ParameterReferences[0].Context)
	})

	t.Run("LIKE expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id, name FROM users WHERE name LIKE '%test%'`)

		require.Len(t, analysis.OutputColumns, 2)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("ILIKE expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users WHERE name ILIKE '%test%'`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("UNION compound query", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id, name FROM users WHERE active = true
			UNION
			SELECT id, name FROM users WHERE active = false`)

		require.Len(t, analysis.CompoundBranches, 1)
		assert.Equal(t, ast.CompoundUnion, analysis.CompoundBranches[0].Operator)
	})

	t.Run("INTERSECT compound query", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users INTERSECT SELECT user_id FROM orders`)

		require.Len(t, analysis.CompoundBranches, 1)
		assert.Equal(t, ast.CompoundIntersect, analysis.CompoundBranches[0].Operator)
	})

	t.Run("EXCEPT compound query", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users EXCEPT SELECT user_id FROM orders`)

		require.Len(t, analysis.CompoundBranches, 1)
		assert.Equal(t, ast.CompoundExcept, analysis.CompoundBranches[0].Operator)
	})

	t.Run("subquery in FROM clause", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT s.id FROM (SELECT id FROM users) s`)

		require.NotEmpty(t, analysis.RawDerivedTables)
		assert.Equal(t, "s", analysis.RawDerivedTables[0].Alias)
	})

	t.Run("correlated scalar subquery", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id, (SELECT COUNT(*) FROM orders o WHERE o.user_id = u.id) FROM users u`)

		require.Len(t, analysis.OutputColumns, 2)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("EXISTS subquery in WHERE", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users u WHERE EXISTS (SELECT 1 FROM orders o WHERE o.user_id = u.id)`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("RIGHT JOIN", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT u.id, o.total FROM users u RIGHT JOIN orders o ON u.id = o.user_id`)

		require.Len(t, analysis.JoinClauses, 1)
		assert.Equal(t, ast.JoinRight, analysis.JoinClauses[0].Kind)
	})

	t.Run("FULL OUTER JOIN", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT u.id, o.total FROM users u FULL OUTER JOIN orders o ON u.id = o.user_id`)

		require.Len(t, analysis.JoinClauses, 1)
		assert.Equal(t, ast.JoinFull, analysis.JoinClauses[0].Kind)
	})

	t.Run("multiple JOINs", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT u.name, o.total FROM users u JOIN orders o ON u.id = o.user_id LEFT JOIN orders o2 ON u.id = o2.user_id`)

		require.NotEmpty(t, analysis.JoinClauses)
		assert.Equal(t, ast.JoinInner, analysis.JoinClauses[0].Kind)
		assert.Equal(t, "orders", analysis.JoinClauses[0].Table.Name)
	})

	t.Run("ORDER BY with NULLS FIRST", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id, email FROM users ORDER BY email ASC NULLS FIRST`)

		require.Len(t, analysis.OutputColumns, 2)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("ORDER BY with NULLS LAST", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id, email FROM users ORDER BY email DESC NULLS LAST`)

		require.Len(t, analysis.OutputColumns, 2)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("CAST parameter context detection", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT CAST($1 AS INTEGER) FROM users`)

		require.Len(t, analysis.ParameterReferences, 1)
		assert.Equal(t, sqlmodel.ParameterContextCast, analysis.ParameterReferences[0].Context)
		require.NotNil(t, analysis.ParameterReferences[0].CastType)
	})

	t.Run("parameter in function argument", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT UPPER($1) FROM users`)

		require.Len(t, analysis.ParameterReferences, 1)
		assert.Equal(t, sqlmodel.ParameterContextFunctionArgument, analysis.ParameterReferences[0].Context)
	})

	t.Run("comparison context for parameter", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users WHERE id = $1`)

		require.Len(t, analysis.ParameterReferences, 1)
		assert.Equal(t, sqlmodel.ParameterContextComparison, analysis.ParameterReferences[0].Context)
		require.NotNil(t, analysis.ParameterReferences[0].ColumnReference)
		assert.Equal(t, "id", analysis.ParameterReferences[0].ColumnReference.ColumnName)
	})

	t.Run("boolean literal in expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users WHERE active = true`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("GROUP BY with table-qualified column", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT u.name FROM users u GROUP BY u.name`)

		require.Len(t, analysis.GroupByColumns, 1)
		assert.Equal(t, "u", analysis.GroupByColumns[0].TableAlias)
		assert.Equal(t, "name", analysis.GroupByColumns[0].ColumnName)
	})

	t.Run("CASE with simple form", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT CASE id WHEN 1 THEN 'one' WHEN 2 THEN 'two' ELSE 'other' END FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("window function with ROWS BETWEEN frame", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id, SUM(id) OVER (ORDER BY id ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) FROM users`)

		require.Len(t, analysis.OutputColumns, 2)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("OFFSET before LIMIT", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users OFFSET $1 ROWS LIMIT $2`)

		require.Len(t, analysis.ParameterReferences, 2)
		assert.Equal(t, sqlmodel.ParameterContextOffset, analysis.ParameterReferences[0].Context)
		assert.Equal(t, sqlmodel.ParameterContextLimit, analysis.ParameterReferences[1].Context)
	})

	t.Run("FETCH FIRST syntax", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users FETCH FIRST 5 ROWS ONLY`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("COALESCE with parameter inherits column reference", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT COALESCE(email, $1) FROM users`)

		require.Len(t, analysis.ParameterReferences, 1)
		require.NotNil(t, analysis.ParameterReferences[0].ColumnReference)
		assert.Equal(t, "email", analysis.ParameterReferences[0].ColumnReference.ColumnName)
	})

	t.Run("multiple CTEs", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `
			WITH active AS (SELECT id, name FROM users WHERE active = true),
			     totals AS (SELECT user_id, SUM(total) AS total FROM orders GROUP BY user_id)
			SELECT a.name, t.total FROM active a JOIN totals t ON a.id = t.user_id`)

		require.Len(t, analysis.CTEDefinitions, 2)
		assert.Equal(t, "active", analysis.CTEDefinitions[0].Name)
		assert.Equal(t, "totals", analysis.CTEDefinitions[1].Name)
	})

	t.Run("NOT IN subquery", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users WHERE id NOT IN (SELECT user_id FROM orders)`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("DISTINCT ON clause", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT DISTINCT ON (user_id) user_id, total FROM orders ORDER BY user_id, total DESC`)

		require.Len(t, analysis.OutputColumns, 2)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("interval literal in expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM orders WHERE created_at > created_at - INTERVAL '30' DAY`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("ARRAY expression in SELECT", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT ARRAY[1, 2, 3]`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("NOT LIKE expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users WHERE name NOT LIKE '%test%'`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("NOT BETWEEN expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users WHERE age NOT BETWEEN 10 AND 20`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("IS DISTINCT FROM", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users WHERE email IS DISTINCT FROM 'test@example.com'`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("concatenation operator", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT name || ' <' || email || '>' FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("nested function calls", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT LOWER(TRIM(name)) FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("arithmetic in output", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id, total * 1.1 AS with_tax FROM orders`)

		require.Len(t, analysis.OutputColumns, 2)
		assert.Equal(t, "with_tax", analysis.OutputColumns[1].Name)
	})

	t.Run("unary minus in expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT -total FROM orders`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("subquery in WHERE with IN", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users WHERE id IN (SELECT user_id FROM orders WHERE total > 100)`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("multiple aggregate functions", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT COUNT(*), SUM(total), AVG(total), MIN(total), MAX(total) FROM orders`)

		require.Len(t, analysis.OutputColumns, 5)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("table-valued function in FROM", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT * FROM generate_series(1, 10) AS gs`)

		require.NotEmpty(t, analysis.RawTableValuedFunctions)
		assert.Equal(t, "generate_series", analysis.RawTableValuedFunctions[0].FunctionName)
	})

	t.Run("USING clause in JOIN", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT u.name FROM users u JOIN orders o USING (id)`)

		require.NotEmpty(t, analysis.JoinClauses)
	})

	t.Run("NULL literal in expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT COALESCE(email, NULL, 'none') FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("CURRENT_TIMESTAMP implicit function", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT CURRENT_TIMESTAMP`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("inline cast with :: syntax", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id::VARCHAR FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
	})
}

func TestAnalyseQuery_LikeParameterContext(t *testing.T) {
	t.Parallel()

	catalogue := buildTestCatalogue()

	tests := []struct {
		assertions func(t *testing.T, analysis *ast.RawQueryAnalysis)
		name       string
		sql        string
	}{
		{
			name: "LIKE pattern with direct column LHS",
			sql:  "SELECT id FROM users WHERE name LIKE $1",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				t.Helper()
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
				t.Helper()
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextLike, analysis.ParameterReferences[0].Context)
				require.NotNil(t, analysis.ParameterReferences[0].ColumnReference)
				assert.Equal(t, "email", analysis.ParameterReferences[0].ColumnReference.ColumnName)
			},
		},
		{
			name: "GLOB classifies as Like context",
			sql:  "SELECT id FROM users WHERE email GLOB $1",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				t.Helper()
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
				t.Helper()
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
				t.Helper()
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
				t.Helper()
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
				t.Helper()
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
				t.Helper()
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

	catalogue := buildTestCatalogue()

	t.Run("simple INSERT with VALUES", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `INSERT INTO users (name, email) VALUES ($1, $2)`)

		assert.Equal(t, "users", analysis.InsertTable)
		require.Len(t, analysis.InsertColumns, 2)
		assert.Equal(t, "name", analysis.InsertColumns[0])
		assert.Equal(t, "email", analysis.InsertColumns[1])
		require.Len(t, analysis.ParameterReferences, 2)
		assert.Equal(t, sqlmodel.ParameterContextAssignment, analysis.ParameterReferences[0].Context)
	})

	t.Run("INSERT without column list", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `INSERT INTO users VALUES ($1, $2, $3, $4, $5)`)

		assert.Equal(t, "users", analysis.InsertTable)
		assert.Empty(t, analysis.InsertColumns, "no explicit column list should be empty")
	})

	t.Run("INSERT with RETURNING", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `INSERT INTO users (name, email) VALUES ($1, $2) RETURNING id, name`)

		assert.True(t, analysis.HasReturning)
		require.Len(t, analysis.OutputColumns, 2)
		assert.Equal(t, "id", analysis.OutputColumns[0].Name)
		assert.Equal(t, "name", analysis.OutputColumns[1].Name)
	})

	t.Run("INSERT with RETURNING star", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `INSERT INTO users (name) VALUES ($1) RETURNING *`)

		assert.True(t, analysis.HasReturning)
		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.OutputColumns[0].IsStar)
	})

	t.Run("INSERT INTO schema-qualified table", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `INSERT INTO main.users (name) VALUES ($1)`)

		assert.Equal(t, "users", analysis.InsertTable)
		require.Len(t, analysis.FromTables, 1)
		assert.Equal(t, "main", analysis.FromTables[0].Schema)
	})

	t.Run("INSERT with ON CONFLICT DO NOTHING", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `INSERT INTO users (name, email) VALUES ($1, $2) ON CONFLICT (email) DO NOTHING`)

		assert.Equal(t, "users", analysis.InsertTable)
		require.Len(t, analysis.ParameterReferences, 2)
	})

	t.Run("INSERT with ON CONFLICT DO UPDATE", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `INSERT INTO users (name, email) VALUES ($1, $2) ON CONFLICT (email) DO UPDATE SET name = $3`)

		assert.Equal(t, "users", analysis.InsertTable)
		require.Len(t, analysis.ParameterReferences, 3)
	})

	t.Run("INSERT with SELECT source", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `INSERT INTO orders (user_id, total) SELECT id, 0 FROM users WHERE active = true`)

		assert.Equal(t, "orders", analysis.InsertTable)
		require.Len(t, analysis.InsertColumns, 2)
		assert.Equal(t, "user_id", analysis.InsertColumns[0])
		assert.Equal(t, "total", analysis.InsertColumns[1])

		require.NotNil(t, analysis.InsertSelect)
		require.NotEmpty(t, analysis.InsertSelect.FromTables)
		assert.Equal(t, "users", analysis.InsertSelect.FromTables[0].Name)
	})

	t.Run("INSERT with joined SELECT source retains joins and where params", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `INSERT INTO orders (user_id, total) `+
			`SELECT u.id, o.amount FROM users u INNER JOIN orgs o ON o.id = u.org_id `+
			`WHERE u.id > $1 AND o.amount > $2 `+
			`ON CONFLICT (user_id) DO NOTHING`)

		assert.Equal(t, "orders", analysis.InsertTable)
		require.NotNil(t, analysis.InsertSelect)
		require.NotEmpty(t, analysis.InsertSelect.FromTables)
		assert.Equal(t, "users", analysis.InsertSelect.FromTables[0].Name)
		require.NotEmpty(t, analysis.InsertSelect.JoinClauses)
		assert.Equal(t, "orgs", analysis.InsertSelect.JoinClauses[0].Table.Name)
		require.Len(t, analysis.InsertSelect.ParameterReferences, 2)
	})

	t.Run("INSERT with CTE", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `WITH new_data AS (SELECT id FROM users WHERE active = true) INSERT INTO orders (user_id) SELECT id FROM new_data`)

		assert.Equal(t, "orders", analysis.InsertTable)
		require.Len(t, analysis.CTEDefinitions, 1)
		assert.Equal(t, "new_data", analysis.CTEDefinitions[0].Name)
	})

	t.Run("INSERT with DEFAULT VALUES", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `INSERT INTO users DEFAULT VALUES RETURNING id`)

		assert.Equal(t, "users", analysis.InsertTable)
		assert.True(t, analysis.HasReturning)
	})
}

func TestAnalyseQuery_Update(t *testing.T) {
	t.Parallel()

	catalogue := buildTestCatalogue()

	t.Run("simple UPDATE SET WHERE", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `UPDATE users SET name = $1 WHERE id = $2`)

		require.Len(t, analysis.FromTables, 1)
		assert.Equal(t, "users", analysis.FromTables[0].Name)
		require.Len(t, analysis.ParameterReferences, 2)
		assert.Equal(t, sqlmodel.ParameterContextAssignment, analysis.ParameterReferences[0].Context)
	})

	t.Run("UPDATE multiple columns", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `UPDATE users SET name = $1, email = $2 WHERE id = $3`)

		require.Len(t, analysis.ParameterReferences, 3)
	})

	t.Run("UPDATE with RETURNING", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `UPDATE users SET name = $1 WHERE id = $2 RETURNING id, name`)

		assert.True(t, analysis.HasReturning)
		require.Len(t, analysis.OutputColumns, 2)
		assert.Equal(t, "id", analysis.OutputColumns[0].Name)
		assert.Equal(t, "name", analysis.OutputColumns[1].Name)
	})

	t.Run("UPDATE with alias", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `UPDATE users AS u SET name = $1 WHERE u.id = $2`)

		require.Len(t, analysis.FromTables, 1)
		assert.Equal(t, "users", analysis.FromTables[0].Name)
		assert.Equal(t, "u", analysis.FromTables[0].Alias)
	})

	t.Run("UPDATE with FROM clause", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `UPDATE orders SET total = $1 FROM users WHERE orders.user_id = users.id AND users.name = $2`)

		require.Len(t, analysis.FromTables, 2)
		assert.Equal(t, "orders", analysis.FromTables[0].Name)
		assert.Equal(t, "users", analysis.FromTables[1].Name)
	})

	t.Run("UPDATE schema-qualified table", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `UPDATE main.users SET active = true WHERE id = $1`)

		require.Len(t, analysis.FromTables, 1)
		assert.Equal(t, "main", analysis.FromTables[0].Schema)
		assert.Equal(t, "users", analysis.FromTables[0].Name)
	})

	t.Run("UPDATE with complex SET expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `UPDATE users SET name = UPPER(name) WHERE id = $1`)

		require.Len(t, analysis.FromTables, 1)
		assert.Equal(t, "users", analysis.FromTables[0].Name)
		require.Len(t, analysis.ParameterReferences, 1)
	})

	t.Run("UPDATE with CTE", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `WITH old AS (SELECT id FROM users WHERE name = $1) UPDATE users SET name = $2 WHERE id IN (SELECT id FROM old)`)

		require.Len(t, analysis.CTEDefinitions, 1)
		assert.Equal(t, "old", analysis.CTEDefinitions[0].Name)
	})
}

func TestAnalyseQuery_Delete(t *testing.T) {
	t.Parallel()

	catalogue := buildTestCatalogue()

	t.Run("simple DELETE WHERE", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `DELETE FROM users WHERE id = $1`)

		require.Len(t, analysis.FromTables, 1)
		assert.Equal(t, "users", analysis.FromTables[0].Name)
		require.Len(t, analysis.ParameterReferences, 1)
		assert.Equal(t, 1, analysis.ParameterReferences[0].Number)
	})

	t.Run("DELETE with RETURNING", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `DELETE FROM users WHERE id = $1 RETURNING id, name`)

		assert.True(t, analysis.HasReturning)
		require.Len(t, analysis.OutputColumns, 2)
		assert.Equal(t, "id", analysis.OutputColumns[0].Name)
		assert.Equal(t, "name", analysis.OutputColumns[1].Name)
	})

	t.Run("DELETE with RETURNING star", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `DELETE FROM users WHERE id = $1 RETURNING *`)

		assert.True(t, analysis.HasReturning)
		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.OutputColumns[0].IsStar)
	})

	t.Run("DELETE with alias", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `DELETE FROM users AS u WHERE u.id = $1`)

		require.Len(t, analysis.FromTables, 1)
		assert.Equal(t, "users", analysis.FromTables[0].Name)
		assert.Equal(t, "u", analysis.FromTables[0].Alias)
	})

	t.Run("DELETE with USING clause", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `DELETE FROM orders USING users WHERE orders.user_id = users.id AND users.active = false`)

		require.Len(t, analysis.FromTables, 2)
		assert.Equal(t, "orders", analysis.FromTables[0].Name)
		assert.Equal(t, "users", analysis.FromTables[1].Name)
	})

	t.Run("DELETE from schema-qualified table", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `DELETE FROM main.users WHERE id = $1`)

		require.Len(t, analysis.FromTables, 1)
		assert.Equal(t, "main", analysis.FromTables[0].Schema)
		assert.Equal(t, "users", analysis.FromTables[0].Name)
	})

	t.Run("DELETE with multiple parameters", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `DELETE FROM users WHERE name = $1 AND age < $2`)

		require.Len(t, analysis.ParameterReferences, 2)
		assert.Equal(t, 1, analysis.ParameterReferences[0].Number)
		assert.Equal(t, 2, analysis.ParameterReferences[1].Number)
	})

	t.Run("DELETE with CTE", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `WITH inactive AS (SELECT id FROM users WHERE active = false) DELETE FROM orders WHERE user_id IN (SELECT id FROM inactive)`)

		require.Len(t, analysis.CTEDefinitions, 1)
		assert.Equal(t, "inactive", analysis.CTEDefinitions[0].Name)
	})

	t.Run("DELETE with complex WHERE", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `DELETE FROM users WHERE email IS NULL AND age < $1`)

		require.Len(t, analysis.FromTables, 1)
		assert.Equal(t, "users", analysis.FromTables[0].Name)
		require.Len(t, analysis.ParameterReferences, 1)
	})
}

func TestAnalyseQuery_Values(t *testing.T) {
	t.Parallel()

	catalogue := buildTestCatalogue()

	t.Run("simple VALUES statement", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `VALUES (1, 'hello'), (2, 'world')`)

		assert.True(t, analysis.ReadOnly, "VALUES should be read-only")
		require.Len(t, analysis.OutputColumns, 2)
	})

	t.Run("VALUES with parameters", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `VALUES ($1, $2)`)

		require.Len(t, analysis.ParameterReferences, 2)
	})
}

func TestAnalyseQuery_SelectExpressions(t *testing.T) {
	t.Parallel()

	catalogue := buildTestCatalogue()

	t.Run("SIMILAR TO expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users WHERE name SIMILAR TO '%pattern%'`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("SIMILAR TO with ESCAPE clause", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users WHERE name SIMILAR TO '%\%%' ESCAPE '\'`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("LIKE with ESCAPE clause", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users WHERE name LIKE '%\%%' ESCAPE '\'`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("bitwise operators", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id & 0xFF, id | 0x01, id << 2, id >> 1 FROM users`)

		require.Len(t, analysis.OutputColumns, 4)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("JSON arrow operators", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT '{"a": 1}'::json -> 'a', '{"b": 2}'::json ->> 'b'`)

		require.Len(t, analysis.OutputColumns, 2)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("ROW constructor expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT ROW(1, 'hello', true)`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("subscript expression with array access", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT ARRAY[1, 2, 3][1]`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("subscript slice expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT ARRAY[1, 2, 3][1:2]`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("comparison with ANY subquery", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users WHERE id = ANY (SELECT user_id FROM orders)`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("OR expression in WHERE", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users WHERE name = 'alice' OR name = 'bob'`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("NOT expression in WHERE", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users WHERE NOT active`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("inline cast with parameter", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users WHERE id = $1::int`)

		require.Len(t, analysis.ParameterReferences, 1)

		require.NotNil(t, analysis.ParameterReferences[0].CastType)
	})

	t.Run("IS TRUE and IS FALSE", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users WHERE active IS TRUE`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("IS NOT DISTINCT FROM", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users WHERE email IS NOT DISTINCT FROM 'test@example.com'`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("schema-qualified function call", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT main.my_func(id) FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("schema-qualified column reference", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT main.users.id FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("NOT EXISTS subquery", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users u WHERE NOT EXISTS (SELECT 1 FROM orders o WHERE o.user_id = u.id)`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("ARRAY subquery expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT ARRAY(SELECT id FROM users)`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("BETWEEN with parameters and column reference", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users WHERE age BETWEEN $1 AND $2`)

		require.Len(t, analysis.ParameterReferences, 2)
		assert.Equal(t, 1, analysis.ParameterReferences[0].Number)
		assert.Equal(t, 2, analysis.ParameterReferences[1].Number)
	})

	t.Run("IN list with literal values", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users WHERE id IN (1, 2, 3)`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("QUALIFY clause", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id, ROW_NUMBER() OVER (ORDER BY id) AS rn FROM users QUALIFY rn = 1`)

		require.Len(t, analysis.OutputColumns, 2)
		assert.True(t, analysis.ReadOnly)
	})
}

func TestAnalyseQuery_SelectFunctions(t *testing.T) {
	t.Parallel()

	catalogue := buildTestCatalogue()

	t.Run("lambda expression single parameter", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT list_transform([1, 2, 3], x -> x + 1)`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("multi-parameter lambda expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT list_reduce([1, 2, 3], (x, y) -> x + y)`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("WITHIN GROUP aggregate", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT mode() WITHIN GROUP (ORDER BY age) FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("FILTER clause on aggregate", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT COUNT(*) FILTER (WHERE active) FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("function with ORDER BY in arguments", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT string_agg(name, ', ' ORDER BY name ASC) FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("window function with named window reference", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id, SUM(id) OVER w FROM users WINDOW w AS (ORDER BY id)`)

		require.Len(t, analysis.OutputColumns, 2)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("window function with RANGE frame", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id, AVG(age) OVER (ORDER BY id RANGE BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) FROM users`)

		require.Len(t, analysis.OutputColumns, 2)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("window function with GROUPS frame", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id, SUM(id) OVER (ORDER BY id GROUPS BETWEEN 1 PRECEDING AND 1 FOLLOWING) FROM users`)

		require.Len(t, analysis.OutputColumns, 2)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("CAST with array type", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT CAST($1 AS INTEGER[]) FROM users`)

		require.Len(t, analysis.ParameterReferences, 1)
		assert.Equal(t, sqlmodel.ParameterContextCast, analysis.ParameterReferences[0].Context)
	})
}

func TestAnalyseQuery_SelectClauses(t *testing.T) {
	t.Parallel()

	catalogue := buildTestCatalogue()

	t.Run("row-locking clauses are rejected", func(t *testing.T) {
		t.Parallel()

		for _, sql := range []string{
			`SELECT id FROM users FOR UPDATE`,
			`SELECT id FROM users FOR UPDATE OF users NOWAIT`,
			`SELECT id FROM users FOR NO KEY UPDATE SKIP LOCKED`,
			`SELECT id FROM users FOR SHARE`,
			`SELECT * FROM (SELECT id FROM users FOR SHARE) s`,
			`SELECT id FROM users UNION ALL SELECT 1 FOR UPDATE`,
		} {
			err := analyseQueryError(t, sql)

			require.ErrorIs(t, err, fault.ErrParse, sql)
			assert.Contains(t, err.Error(), "locking clause is not supported", sql)
		}
	})

	t.Run("FETCH NEXT syntax", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users FETCH NEXT 5 ROWS ONLY`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("FETCH FIRST with TIES", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users ORDER BY id FETCH FIRST 5 ROWS WITH TIES`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("OFFSET after FETCH", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users OFFSET 5 ROWS FETCH FIRST 10 ROWS ONLY`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("GROUP BY with expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT LOWER(name), count(*) FROM users GROUP BY LOWER(name), id`)

		require.Equal(t, []ast.ColumnReference{{TableAlias: "", ColumnName: "id"}}, analysis.GroupByColumns)
	})

	t.Run("CTE with column names", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `WITH active (uid, username) AS (
			SELECT id, name FROM users WHERE active = true
		)
		SELECT uid, username FROM active`)

		require.Len(t, analysis.CTEDefinitions, 1)
		assert.Equal(t, "active", analysis.CTEDefinitions[0].Name)
		require.Len(t, analysis.CTEDefinitions[0].OutputColumns, 2)
		assert.Equal(t, []string{"uid", "username"}, analysis.CTEDefinitions[0].ColumnNames)
	})

	t.Run("RECURSIVE CTE", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `WITH RECURSIVE nums AS (
			SELECT 1 AS n
			UNION ALL
			SELECT n + 1 FROM nums WHERE n < 10
		)
		SELECT n FROM nums`)

		require.Len(t, analysis.CTEDefinitions, 1)
		assert.True(t, analysis.CTEDefinitions[0].IsRecursive)
	})

	t.Run("MATERIALIZED CTE hint", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `WITH active AS MATERIALIZED (
			SELECT id FROM users WHERE active = true
		)
		SELECT id FROM active`)

		require.Len(t, analysis.CTEDefinitions, 1)
	})

	t.Run("NOT MATERIALIZED CTE hint", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `WITH active AS NOT MATERIALIZED (
			SELECT id FROM users WHERE active = true
		)
		SELECT id FROM active`)

		require.Len(t, analysis.CTEDefinitions, 1)
	})

	t.Run("TVF with column definitions", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT * FROM read_csv('data.csv') AS t(id INTEGER, name VARCHAR)`)

		require.NotEmpty(t, analysis.RawTableValuedFunctions)
		assert.Equal(t, "read_csv", analysis.RawTableValuedFunctions[0].FunctionName)
		assert.Equal(t, "t", analysis.RawTableValuedFunctions[0].Alias)
		require.NotEmpty(t, analysis.RawTableValuedFunctions[0].ColumnDefinitions)
	})

	t.Run("PIVOT clause", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT p.* FROM (SELECT id, active FROM users) PIVOT (COUNT(id) FOR active IN (true, false)) AS p`)

		require.NotEmpty(t, analysis.RawDerivedTables)
	})

	t.Run("LATERAL join", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT u.name, gs.val FROM users u, LATERAL generate_series(1, u.id) AS gs(val)`)

		require.NotEmpty(t, analysis.RawTableValuedFunctions)
	})
}

func TestAnalyseQuery_Update_AliasAndMultiColumnSet(t *testing.T) {
	t.Parallel()

	catalogue := buildTestCatalogue()

	t.Run("UPDATE with multi-column SET", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `UPDATE users SET (name, email) = ($1, $2) WHERE id = $3`)

		require.Len(t, analysis.ParameterReferences, 3)
	})

	t.Run("UPDATE with alias without AS keyword", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `UPDATE users u SET name = $1 WHERE u.id = $2`)

		require.Len(t, analysis.FromTables, 1)
		assert.Equal(t, "u", analysis.FromTables[0].Alias)
	})

	t.Run("UPDATE with WHERE CURRENT OF", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `UPDATE users SET name = 'test' WHERE CURRENT OF cursor_name`)

		require.Len(t, analysis.FromTables, 1)
		assert.Equal(t, "users", analysis.FromTables[0].Name)
	})

	t.Run("DELETE with alias without AS keyword", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `DELETE FROM users u WHERE u.id = $1`)

		require.Len(t, analysis.FromTables, 1)
		assert.Equal(t, "u", analysis.FromTables[0].Alias)
	})
}

func TestAnalyseQuery_Insert_ConflictAndOverriding(t *testing.T) {
	t.Parallel()

	catalogue := buildTestCatalogue()

	t.Run("INSERT with OVERRIDING SYSTEM VALUE", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `INSERT INTO users (id, name) OVERRIDING SYSTEM VALUE VALUES ($1, $2)`)

		assert.Equal(t, "users", analysis.InsertTable)
		require.Len(t, analysis.ParameterReferences, 2)
	})

	t.Run("INSERT with alias AS", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `INSERT INTO users AS u (name) VALUES ($1) ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name`)

		assert.Equal(t, "users", analysis.InsertTable)
	})

	t.Run("INSERT with ON CONFLICT ON CONSTRAINT", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `INSERT INTO users (name) VALUES ($1) ON CONFLICT ON CONSTRAINT users_pkey DO NOTHING`)

		assert.Equal(t, "users", analysis.InsertTable)
	})

	t.Run("INSERT VALUES with non-parameter literal expressions", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `INSERT INTO users (name, email) VALUES ('alice', 'alice@example.com')`)

		assert.Equal(t, "users", analysis.InsertTable)
		require.Len(t, analysis.InsertColumns, 2)
	})

	t.Run("INSERT VALUES with subexpression in parentheses", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `INSERT INTO users (name) VALUES ((SELECT 'alice'))`)

		assert.Equal(t, "users", analysis.InsertTable)
	})

	t.Run("INSERT with multiple value rows", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `INSERT INTO users (name, email) VALUES ($1, $2), ($3, $4)`)

		assert.Equal(t, "users", analysis.InsertTable)
		require.Len(t, analysis.ParameterReferences, 4)
	})
}

func TestAnalyseQuery_Values_OrderByAndLimit(t *testing.T) {
	t.Parallel()

	catalogue := buildTestCatalogue()

	t.Run("VALUES with ORDER BY and LIMIT", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `VALUES (1, 'a'), (2, 'b') ORDER BY 1 LIMIT 1`)

		assert.True(t, analysis.ReadOnly)
		require.Len(t, analysis.OutputColumns, 2)
	})
}

func TestAnalyseQuery_NamedParameters(t *testing.T) {
	t.Parallel()

	catalogue := buildTestCatalogue()

	t.Run("named parameter with colon prefix", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users WHERE name = :name`)

		require.Len(t, analysis.ParameterReferences, 1)
		assert.Equal(t, "name", analysis.ParameterReferences[0].Name)
		assert.Equal(t, 1, analysis.ParameterReferences[0].Number)
	})

	t.Run("named parameter with dollar prefix", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users WHERE name = $name AND email = $email_address OR name = :name`)

		require.Len(t, analysis.ParameterReferences, 3)
		require.Equal(t, "name", analysis.ParameterReferences[0].Name)
		require.Equal(t, "email_address", analysis.ParameterReferences[1].Name)
		require.Equal(t, []int{1, 2, 1}, []int{
			analysis.ParameterReferences[0].Number, analysis.ParameterReferences[1].Number, analysis.ParameterReferences[2].Number,
		})
	})

	t.Run("dollar-quoted string beside a dollar parameter", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT $tag$ $name $tag$ AS label FROM users WHERE name = $name`)

		require.Len(t, analysis.ParameterReferences, 1)
		require.Equal(t, "name", analysis.ParameterReferences[0].Name)
	})

	t.Run("repeated named parameter", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users WHERE name = :name OR email = :name`)

		require.Len(t, analysis.ParameterReferences, 2)
		assert.Equal(t, analysis.ParameterReferences[0].Number, analysis.ParameterReferences[1].Number,
			"repeated named parameter should share the same number")
	})
}

func TestAnalyseQuery_CTEWithDML(t *testing.T) {
	t.Parallel()

	catalogue := buildTestCatalogue()

	t.Run("CTE with INSERT body", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `WITH inserted AS (
			INSERT INTO orders (user_id, total) VALUES ($1, $2) RETURNING id
		)
		SELECT id FROM inserted`)

		require.Len(t, analysis.CTEDefinitions, 1)
		assert.False(t, analysis.ReadOnly, "data-modifying CTE should not be read-only")
	})

	t.Run("CTE with UPDATE body", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `WITH updated AS (
			UPDATE users SET name = 'new' WHERE id = $1 RETURNING id
		)
		SELECT id FROM updated`)

		require.Len(t, analysis.CTEDefinitions, 1)
		assert.False(t, analysis.ReadOnly)
	})

	t.Run("CTE with DELETE body", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `WITH deleted AS (
			DELETE FROM users WHERE active = false RETURNING id
		)
		SELECT id FROM deleted`)

		require.Len(t, analysis.CTEDefinitions, 1)
		assert.False(t, analysis.ReadOnly)
	})
}

func TestAnalyseQuery_Select_OutputExpressionOperators(t *testing.T) {
	t.Parallel()

	catalogue := buildTestCatalogue()

	t.Run("LIKE in output expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT name LIKE '%test%' AS is_test FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.Equal(t, "is_test", analysis.OutputColumns[0].Name)
	})

	t.Run("ILIKE in output expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT name ILIKE '%test%' AS matches FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("SIMILAR TO in output expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT name SIMILAR TO '%(test|demo)%' AS matches FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("NOT LIKE in output expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT name NOT LIKE '%test%' AS not_test FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("NOT BETWEEN in output expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT age NOT BETWEEN 10 AND 20 AS outside_range FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("BETWEEN in output expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT age BETWEEN 10 AND 20 AS in_range FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("IN list in output expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id IN (1, 2, 3) AS in_list FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("IS NULL in output expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT email IS NULL AS missing_email FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("IS NOT NULL in output expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT email IS NOT NULL AS has_email FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("IS DISTINCT FROM in output expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT email IS DISTINCT FROM 'test@example.com' AS different FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("comparison operators in output expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id > 5 AS big, id <= 10 AS small, id <> 0 AS nonzero FROM users`)

		require.Len(t, analysis.OutputColumns, 3)
	})

	t.Run("OR and AND in output expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT (active AND age > 18) OR (name = 'admin') AS eligible FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("NOT in output expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT NOT active AS inactive FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("INTERVAL literal in output expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT INTERVAL '30' DAY AS one_month`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("INTERVAL with YEAR TO MONTH", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT INTERVAL '1' YEAR TO MONTH AS period`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("EXISTS subquery in output expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT EXISTS (SELECT 1 FROM orders WHERE orders.user_id = users.id) AS has_orders FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("parenthesised expression in output", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT (id + 1) * 2 AS doubled FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("NOT unary in cast expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT NOT true AS result`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("tilde unary operator", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT ~id AS inverted FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("inline cast in output expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id::varchar AS id_text FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("cast with type modifiers", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT CAST(name AS VARCHAR(100)) FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("schema-qualified function call in output", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT main.lower(name) FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("three-part schema.table.column reference", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT main.users.name FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("schema-qualified function with three parts", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT main.pg_catalog.now()`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("BETWEEN SYMMETRIC in output expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT age BETWEEN SYMMETRIC 20 AND 10 AS in_range FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("IN list with subquery in output", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id IN (SELECT user_id FROM orders) AS has_orders FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("NOT IN list in output expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id NOT IN (1, 2, 3) AS not_in_list FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("LIKE with ESCAPE in output", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT name LIKE '%\_%' ESCAPE '\' AS has_underscore FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("SIMILAR TO with ESCAPE in output", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT name SIMILAR TO '%\_%' ESCAPE '\' AS matches FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("IS TRUE in output expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT active IS TRUE AS is_active FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("IS NOT TRUE in output expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT active IS NOT TRUE AS not_active FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("IS UNKNOWN in output expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT active IS UNKNOWN AS is_unknown FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("comparison with ALL subquery", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id > ALL (SELECT user_id FROM orders) AS bigger_than_all FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("CAST target type with multi-word type", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT CAST(id AS DOUBLE PRECISION) FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("CAST target type with array brackets", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT CAST(ARRAY[1,2] AS INTEGER[])`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("LIMIT ALL syntax", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users LIMIT ALL`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("OFFSET ROWS FETCH", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id FROM users OFFSET 10 ROWS FETCH FIRST 5 ROWS ONLY`)

		require.Len(t, analysis.OutputColumns, 1)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("window function with EXCLUDE CURRENT ROW", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id, SUM(age) OVER (ORDER BY id ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING EXCLUDE CURRENT ROW) FROM users`)

		require.Len(t, analysis.OutputColumns, 2)
	})

	t.Run("window function with numeric frame bound", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT id, AVG(age) OVER (ORDER BY id ROWS BETWEEN 2 PRECEDING AND 2 FOLLOWING) FROM users`)

		require.Len(t, analysis.OutputColumns, 2)
	})

	t.Run("function with DISTINCT keyword", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT COUNT(DISTINCT name) FROM users`)

		require.Len(t, analysis.OutputColumns, 1)
	})

	t.Run("parameter in CAST in output expression", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT CAST($1 AS VARCHAR) AS value`)

		require.Len(t, analysis.ParameterReferences, 1)
		assert.Equal(t, sqlmodel.ParameterContextCast, analysis.ParameterReferences[0].Context)
	})

	t.Run("parameter in inline cast in output", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, catalogue, `SELECT $1::INTEGER AS value`)

		require.Len(t, analysis.ParameterReferences, 1)
		assert.Equal(t, sqlmodel.ParameterContextCast, analysis.ParameterReferences[0].Context)
	})
}

func TestParseStatements_Multiple(t *testing.T) {
	t.Parallel()

	engine := New()
	statements, err := engine.ParseStatements(t.Context(), `SELECT 1; SELECT 2`)
	require.NoError(t, err)
	require.Len(t, statements, 2, "should split multiple statements on semicolons")
}

func TestAnalyseQuery_HasWhereClause(t *testing.T) {
	t.Parallel()

	catalogue := buildTestCatalogue()

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

func TestAnalyseQuery_FromTVFArgumentMetadata(t *testing.T) {
	t.Parallel()

	catalogue := buildTestCatalogue()
	analysis := analyseQuery(t, catalogue,
		`SELECT * FROM content.get_pages_with_latest_version($3, $1, $2)`)

	require.Len(t, analysis.RawTableValuedFunctions, 1)
	assert.Equal(t, "content.get_pages_with_latest_version", analysis.RawTableValuedFunctions[0].FunctionName)

	require.Len(t, analysis.ParameterReferences, 3)
	byNumber := map[int]ast.RawParameterReference{}
	for _, parameter := range analysis.ParameterReferences {
		byNumber[parameter.Number] = parameter
	}

	for number, wantOrdinal := range map[int]int{3: 0, 1: 1, 2: 2} {
		parameter := byNumber[number]
		assert.Equalf(t, sqlmodel.ParameterContextFunctionArgument, parameter.Context,
			"$%d should be a function argument", number)
		assert.Equalf(t, "content.get_pages_with_latest_version", parameter.EnclosingFunctionName,
			"$%d enclosing function name", number)
		assert.Equalf(t, wantOrdinal, parameter.ArgumentOrdinal, "$%d argument ordinal", number)
	}
}

func TestAnalyseQuery_ScalarFunctionArgumentMetadata(t *testing.T) {
	t.Parallel()

	catalogue := buildTestCatalogue()
	analysis := analyseQuery(t, catalogue, `SELECT STRING_TO_ARRAY($2, '/') FROM users`)

	require.Len(t, analysis.ParameterReferences, 1)
	parameter := analysis.ParameterReferences[0]
	assert.Equal(t, sqlmodel.ParameterContextFunctionArgument, parameter.Context)
	assert.Equal(t, "string_to_array", parameter.EnclosingFunctionName)
	assert.Equal(t, 0, parameter.ArgumentOrdinal)
}

func TestAnalyseQuery_NestedFunctionArgumentOrdinalIsolation(t *testing.T) {
	t.Parallel()

	catalogue := buildTestCatalogue()
	analysis := analyseQuery(t, catalogue, `SELECT outer_fn($1, inner_fn($2, $3)) FROM users`)

	require.Len(t, analysis.ParameterReferences, 3)
	byNumber := map[int]ast.RawParameterReference{}
	for _, parameter := range analysis.ParameterReferences {
		byNumber[parameter.Number] = parameter
	}

	assert.Equal(t, "outer_fn", byNumber[1].EnclosingFunctionName)
	assert.Equal(t, 0, byNumber[1].ArgumentOrdinal)

	assert.Equal(t, "inner_fn", byNumber[2].EnclosingFunctionName)
	assert.Equal(t, 0, byNumber[2].ArgumentOrdinal)

	assert.Equal(t, "inner_fn", byNumber[3].EnclosingFunctionName)
	assert.Equal(t, 1, byNumber[3].ArgumentOrdinal)
}

func TestAnalyseQuery_InsertSelectProjectionParameterMapping(t *testing.T) {
	t.Parallel()

	catalogue := buildTestCatalogue()
	analysis := analyseQuery(t, catalogue,
		`INSERT INTO users (id, name, email) SELECT $1, COALESCE($2, 'x'), $3 FROM users WHERE id = $4`)

	require.Len(t, analysis.ParameterReferences, 4)
	byNumber := map[int]ast.RawParameterReference{}
	for _, parameter := range analysis.ParameterReferences {
		byNumber[parameter.Number] = parameter
	}

	wantColumns := map[int]string{1: "id", 2: "name", 3: "email"}
	for number, wantColumn := range wantColumns {
		parameter := byNumber[number]
		assert.Equalf(t, sqlmodel.ParameterContextAssignment, parameter.Context,
			"$%d should be an assignment", number)
		require.NotNilf(t, parameter.ColumnReference, "$%d should carry a column reference", number)
		assert.Equalf(t, "users", parameter.ColumnReference.TableAlias, "$%d target table", number)
		assert.Equalf(t, wantColumn, parameter.ColumnReference.ColumnName, "$%d target column", number)
	}

	where := byNumber[4]
	assert.Equal(t, sqlmodel.ParameterContextComparison, where.Context)
	require.NotNil(t, where.ColumnReference)
	assert.Equal(t, "id", where.ColumnReference.ColumnName)
}

func TestAnalyseQuery_IsComparisonParameterTypedFromColumn(t *testing.T) {
	t.Parallel()

	catalogue := buildTestCatalogue()

	tests := []struct {
		name string
		sql  string
	}{
		{name: "IS in WHERE", sql: `SELECT id FROM users WHERE name IS $1`},
		{name: "IS NOT in WHERE", sql: `SELECT id FROM users WHERE name IS NOT $1`},
		{name: "IS nested in CASE", sql: `SELECT CASE WHEN name IS $1 THEN 1 ELSE 0 END FROM users`},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			analysis := analyseQuery(t, catalogue, testCase.sql)
			require.Len(t, analysis.ParameterReferences, 1)
			parameter := analysis.ParameterReferences[0]
			assert.Equal(t, sqlmodel.ParameterContextComparison, parameter.Context)
			require.NotNil(t, parameter.ColumnReference)
			assert.Equal(t, "name", parameter.ColumnReference.ColumnName)
		})
	}
}

func TestAnalyseQuery_IsNullRegistersNoParameter(t *testing.T) {
	t.Parallel()

	catalogue := buildTestCatalogue()

	for _, sql := range []string{
		`SELECT id FROM users WHERE name IS NULL`,
		`SELECT id FROM users WHERE name IS NOT NULL`,
	} {
		analysis := analyseQuery(t, catalogue, sql)
		assert.Emptyf(t, analysis.ParameterReferences, "%q should register no parameter", sql)
	}
}

func TestParseStatements_RejectsOutOfRangeDollarParameters(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
	}{
		{name: "number that would overflow an int", sql: `SELECT id FROM users WHERE id = $99999999999999999999999999`},
		{name: "number above the protocol maximum", sql: `SELECT id FROM users WHERE id = $2147483647`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := New().ParseStatements(t.Context(), testCase.sql)

			require.ErrorContains(t, err, "exceeds maximum of 65535")
		})
	}
}

func TestAnalyseQuery_InsertOnConflictTargetPredicateReturning(t *testing.T) {
	t.Parallel()

	catalogue := buildTestCatalogue()

	tests := []struct {
		name               string
		sql                string
		expectedParameters int
	}{
		{
			name:               "DO NOTHING with partial-index predicate keeps RETURNING",
			sql:                "INSERT INTO users (name, email) VALUES ($1, $2) ON CONFLICT (email) WHERE email IS NOT NULL DO NOTHING RETURNING id",
			expectedParameters: 2,
		},
		{
			name:               "DO UPDATE with partial-index predicate keeps SET parameter and RETURNING",
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
			assert.True(t, analysis.HasReturning,
				"RETURNING after a conflict-target WHERE predicate must be detected")
			require.Len(t, analysis.OutputColumns, 1)
			assert.Equal(t, "id", analysis.OutputColumns[0].Name)
			require.Len(t, analysis.ParameterReferences, testCase.expectedParameters)
		})
	}
}

func TestAnalyseQuery_InsertOrReplaceIgnorePrefix(t *testing.T) {
	t.Parallel()

	catalogue := buildTestCatalogue()

	for _, sql := range []string{
		"INSERT OR REPLACE INTO users (name, email) VALUES ($1, $2) RETURNING id",
		"INSERT OR IGNORE INTO users (name, email) VALUES ($1, $2) RETURNING id",
	} {
		t.Run(sql, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, catalogue, sql)

			assert.Equal(t, "users", analysis.InsertTable,
				"the OR REPLACE/IGNORE prefix must not be mistaken for the table name")
			assert.Equal(t, []string{"name", "email"}, analysis.InsertColumns)
			require.Len(t, analysis.ParameterReferences, 2)
			assert.True(t, analysis.HasReturning)
			require.Len(t, analysis.OutputColumns, 1)
			assert.Equal(t, "id", analysis.OutputColumns[0].Name)
		})
	}
}

func TestAnalyseQuery_FlatSetOperationChain(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		operator      string
		armCount      int
		wantOperator  ast.CompoundOperator
		wantParameter bool
	}{
		{name: "long UNION ALL chain", operator: " UNION ALL ", wantOperator: ast.CompoundUnionAll, armCount: 10_000},
		{name: "long UNION chain", operator: " UNION ", wantOperator: ast.CompoundUnion, armCount: 1_000},
		{name: "INTERSECT chain", operator: " INTERSECT ", wantOperator: ast.CompoundIntersect, armCount: 300},
		{name: "EXCEPT chain with trailing LIMIT", operator: " EXCEPT ", wantOperator: ast.CompoundExcept, armCount: 300, wantParameter: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			arms := make([]string, testCase.armCount)
			for index := range arms {
				arms[index] = "SELECT id FROM users"
			}
			sql := strings.Join(arms, testCase.operator)
			if testCase.wantParameter {
				sql += " ORDER BY id LIMIT $1"
			}

			analysis := analyseQuery(t, buildTestCatalogue(), sql)

			require.Len(t, analysis.CompoundBranches, testCase.armCount-1)
			for _, branch := range analysis.CompoundBranches {
				assert.Equal(t, testCase.wantOperator, branch.Operator)
				require.NotNil(t, branch.Query)
				assert.Empty(t, branch.Query.CompoundBranches)
				assert.Empty(t, branch.Query.ParameterReferences, "an arm holds only the parameters written in it")
			}
			if testCase.wantParameter {
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, 1, analysis.ParameterReferences[0].Number)
			}
		})
	}
}

func TestAnalyseQuery_SetOperationArmsKeepTheirOwnStructure(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, buildTestCatalogue(),
		`SELECT id FROM users WHERE id = $1 UNION SELECT id FROM users GROUP BY id UNION ALL SELECT 1 ORDER BY 1`)

	require.Len(t, analysis.CompoundBranches, 2)
	assert.True(t, analysis.HasWhereClause)
	assert.False(t, analysis.CompoundBranches[0].Query.HasWhereClause)
	assert.NotEmpty(t, analysis.CompoundBranches[0].Query.GroupByColumns)
	assert.True(t, analysis.ReadOnly)
	assert.True(t, analysis.CompoundBranches[1].Query.ReadOnly)
}

func TestAnalyseQuery_ManyPlaceholderArgumentsInOneCall(t *testing.T) {
	t.Parallel()

	const argumentCount = 20_000

	placeholders := make([]string, argumentCount)
	for index := range placeholders {
		placeholders[index] = "$" + strconv.Itoa(index+1)
	}
	sql := "SELECT concat(" + strings.Join(placeholders, ", ") + ") FROM users"

	analysis := analyseQuery(t, buildTestCatalogue(), sql)

	require.Len(t, analysis.ParameterReferences, argumentCount)
	for index, parameter := range analysis.ParameterReferences {
		assert.Equal(t, "concat", parameter.EnclosingFunctionName)
		assert.Equal(t, index, parameter.ArgumentOrdinal)
	}
}

func TestAnalyseQuery_ArgumentOrdinalsAcrossInterleavedCalls(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, buildTestCatalogue(),
		`SELECT outer_fn(inner_fn($1, $2), $3, inner_fn($4), $5) FROM users`)

	byNumber := map[int]ast.RawParameterReference{}
	for _, parameter := range analysis.ParameterReferences {
		byNumber[parameter.Number] = parameter
	}

	testCases := []struct {
		wantName    string
		number      int
		wantOrdinal int
	}{
		{number: 1, wantName: "inner_fn", wantOrdinal: 0},
		{number: 2, wantName: "inner_fn", wantOrdinal: 1},
		{number: 3, wantName: "outer_fn", wantOrdinal: 1},
		{number: 4, wantName: "inner_fn", wantOrdinal: 0},
		{number: 5, wantName: "outer_fn", wantOrdinal: 3},
	}

	for _, testCase := range testCases {
		parameter, found := byNumber[testCase.number]
		require.Truef(t, found, "$%d registered", testCase.number)
		assert.Equalf(t, testCase.wantName, parameter.EnclosingFunctionName, "$%d enclosing function", testCase.number)
		assert.Equalf(t, testCase.wantOrdinal, parameter.ArgumentOrdinal, "$%d argument ordinal", testCase.number)
	}
}

func TestAnalyseQuery_ManyProjectedPlaceholders(t *testing.T) {
	t.Parallel()

	const placeholderCount = 50_000

	placeholders := make([]string, placeholderCount)
	for index := range placeholders {
		placeholders[index] = "$" + strconv.Itoa(index+1)
	}

	analysis := analyseQuery(t, nil, "SELECT "+strings.Join(placeholders, ", "))

	require.Len(t, analysis.ParameterReferences, placeholderCount)
	require.Len(t, analysis.OutputColumns, placeholderCount)
}

func TestAnalyseQuery_DuckDBGrammarConsumesTheWholeStatement(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		sql            string
		wantColumns    []string
		wantParameters int
	}{
		{name: "leading-dot number keeps later parameters", sql: "SELECT .5 AS half, id FROM users WHERE id = $1 AND name = $2", wantColumns: []string{"half", "id"}, wantParameters: 2},
		{name: "struct field access", sql: "SELECT name, berth.pontoon AS pontoon FROM boats", wantColumns: []string{"name", "pontoon"}},
		{name: "unaliased struct field access", sql: "SELECT berth.pontoon FROM boats", wantColumns: []string{"pontoon"}},
		{name: "table-qualified struct field access", sql: "SELECT b.berth.number AS n FROM boats b", wantColumns: []string{"n"}},
		{name: "subscript then field", sql: "SELECT name, route[1].lat AS start_lat FROM boats", wantColumns: []string{"name", "start_lat"}},
		{name: "list slice", sql: "SELECT crew[1:2] AS first_two FROM boats", wantColumns: []string{"first_two"}},
		{name: "named function arguments", sql: "SELECT struct_pack(a := name, b := id) AS packed FROM boats", wantColumns: []string{"packed"}},
		{name: "arrow named function arguments", sql: "SELECT struct_pack(a => name) AS packed FROM boats", wantColumns: []string{"packed"}},
		{name: "struct literal", sql: "SELECT {'a': name, 'b': $1} AS literal FROM boats", wantColumns: []string{"literal"}, wantParameters: 1},
		{name: "bare-key struct literal", sql: "SELECT {a: name} AS literal FROM boats", wantColumns: []string{"literal"}},
		{name: "list literal", sql: "SELECT [1, 2, $1] AS xs FROM boats", wantColumns: []string{"xs"}, wantParameters: 1},
		{name: "map literal", sql: "SELECT MAP {'k': $1} AS m FROM boats", wantColumns: []string{"m"}, wantParameters: 1},
		{name: "list comprehension", sql: "SELECT [x * 2 FOR x IN crew IF x <> $1] AS doubled FROM boats", wantColumns: []string{"doubled"}, wantParameters: 1},
		{name: "lambda with parameter", sql: "SELECT list_filter(crew, x -> x <> $1) AS kept FROM boats", wantColumns: []string{"kept"}, wantParameters: 1},
		{name: "from first with select", sql: "FROM boats SELECT id, name WHERE id = $1", wantColumns: []string{"id", "name"}, wantParameters: 1},
		{name: "group by all", sql: "SELECT harbour, count(*) AS n FROM landings GROUP BY ALL ORDER BY ALL", wantColumns: []string{"harbour", "n"}},
		{name: "qualify after table", sql: "SELECT id FROM landings QUALIFY row_number() OVER (PARTITION BY harbour ORDER BY kg DESC) = $1", wantColumns: []string{"id"}, wantParameters: 1},
		{name: "named window", sql: "SELECT id, rank() OVER w AS r FROM landings WINDOW w AS (PARTITION BY harbour ORDER BY kg)", wantColumns: []string{"id", "r"}},
		{name: "asof join", sql: "SELECT t.sym FROM ticks t ASOF JOIN quotes q ON t.sym = q.sym AND t.ts >= q.ts", wantColumns: []string{"sym"}},
		{name: "anti join", sql: "SELECT l.id FROM landings l ANTI JOIN boats b ON l.boat_id = b.id WHERE l.kg > $1", wantColumns: []string{"id"}, wantParameters: 1},
		{name: "semi join", sql: "SELECT l.id FROM landings l SEMI JOIN boats b ON l.boat_id = b.id", wantColumns: []string{"id"}},
		{name: "parenthesised join", sql: "SELECT l.id FROM (landings l JOIN boats b ON l.boat_id = b.id) WHERE b.id = $1", wantColumns: []string{"id"}, wantParameters: 1},
		{name: "using sample", sql: "SELECT id FROM landings USING SAMPLE 10%", wantColumns: []string{"id"}},
		{name: "tablesample", sql: "SELECT id FROM landings TABLESAMPLE 10 ROWS", wantColumns: []string{"id"}},
		{name: "union by name", sql: "SELECT name FROM boats UNION ALL BY NAME SELECT name FROM boats WHERE id = $1", wantColumns: []string{"name"}, wantParameters: 1},
		{name: "parenthesised compound arms", sql: "(SELECT id FROM boats WHERE id = $1) UNION (SELECT boat_id FROM landings)", wantColumns: []string{"id"}, wantParameters: 1},
		{name: "is not distinct from in where", sql: "SELECT id FROM landings WHERE price IS NOT DISTINCT FROM $1", wantColumns: []string{"id"}, wantParameters: 1},
		{name: "try cast", sql: "SELECT TRY_CAST(species AS INTEGER) AS t FROM landings", wantColumns: []string{"t"}},
		{name: "extract from", sql: "SELECT extract(year FROM landed_on) AS y FROM landings WHERE kg > $1", wantColumns: []string{"y"}, wantParameters: 1},
		{name: "exponent and integer division", sql: "SELECT kg // 2 AS halved, kg ** 2 AS squared FROM landings", wantColumns: []string{"halved", "squared"}},
		{name: "prefix operator", sql: "SELECT species ^@ $1 AS starts FROM landings", wantColumns: []string{"starts"}, wantParameters: 1},
		{name: "interval with unit", sql: "SELECT landed_on + INTERVAL 1 DAY AS next, now() - INTERVAL (3) HOUR AS earlier FROM landings", wantColumns: []string{"next", "earlier"}},
		{name: "escape string", sql: "SELECT E'a\\nb' AS esc FROM boats", wantColumns: []string{"esc"}},
		{name: "row literal", sql: "SELECT (1, 'a') AS r FROM boats", wantColumns: []string{"r"}},
		{name: "table function column aliases", sql: "SELECT i FROM range(10) r(i)", wantColumns: []string{"i"}},
		{name: "values derived table", sql: "SELECT n FROM (VALUES (1, 'a'), (2, $1)) AS v(n, s)", wantColumns: []string{"n"}, wantParameters: 1},
		{name: "pivot subquery", sql: "SELECT harbour, cod FROM (PIVOT landings ON species IN ('cod', 'hake') USING sum(kg) GROUP BY harbour)", wantColumns: []string{"harbour", "cod"}},
		{name: "standard pivot", sql: "SELECT * FROM landings PIVOT (sum(kg) FOR species IN ('cod', 'hake') GROUP BY harbour)"},
		{name: "insert by name", sql: "INSERT INTO landings BY NAME SELECT $1 AS id, 'a' AS harbour", wantParameters: 1},
		{name: "named macro argument", sql: "SELECT add_default(1, b := $1) AS b FROM boats", wantColumns: []string{"b"}, wantParameters: 1},
		{name: "unnest with named argument", sql: "SELECT unnest(route, recursive := true) FROM boats"},
		{name: "positional column reference", sql: "SELECT #1 FROM boats"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, nil, testCase.sql)

			numbers := map[int]bool{}
			for _, reference := range analysis.ParameterReferences {
				numbers[reference.Number] = true
			}
			assert.Len(t, numbers, testCase.wantParameters, "distinct parameters")
			if testCase.wantColumns != nil {
				names := make([]string, 0, len(analysis.OutputColumns))
				for _, column := range analysis.OutputColumns {
					names = append(names, column.Name)
				}
				assert.Equal(t, testCase.wantColumns, names)
			}
		})
	}
}

func TestAnalyseQuery_RejectsUnsupportedStarModifiers(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"SELECT * EXCLUDE (meta) FROM boats",
		"SELECT * REPLACE (upper(name) AS name) FROM boats",
		"SELECT boats.* EXCLUDE (meta) FROM boats",
		"SELECT * RENAME (name AS label) FROM boats",
	} {
		engine := New()
		statements, err := engine.ParseStatements(t.Context(), sql)
		require.NoError(t, err)
		_, err = engine.AnalyseQuery(t.Context(), nil, statements[0])
		require.ErrorIs(t, err, fault.ErrParse, sql)
		assert.Contains(t, err.Error(), "list the columns", sql)
	}
}

func analyseQueryError(t *testing.T, sql string) error {
	t.Helper()
	engine := New()
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.Len(t, statements, 1)
	_, err = engine.AnalyseQuery(t.Context(), nil, statements[0])
	return err
}

func TestAnalyseQuery_QuotedKeywordsAreNames(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, nil, `SELECT "from", "select" AS s FROM "order" "where" WHERE "where"."group" = $1 AND "limit" = $2 ORDER BY "order"`)

	require.Len(t, analysis.OutputColumns, 2)
	assert.Equal(t, "from", analysis.OutputColumns[0].Name)
	assert.Equal(t, "s", analysis.OutputColumns[1].Name)
	require.Len(t, analysis.FromTables, 1)
	assert.Equal(t, ast.TableReference{Schema: "", Name: "order", Alias: "where"}, analysis.FromTables[0])
	require.Len(t, analysis.ParameterReferences, 2)
	assert.Equal(t, &ast.ColumnReference{TableAlias: "where", ColumnName: "group"}, analysis.ParameterReferences[0].ColumnReference)
	assert.Equal(t, &ast.ColumnReference{TableAlias: "", ColumnName: "limit"}, analysis.ParameterReferences[1].ColumnReference)
}

func TestAnalyseQuery_RejectsTokensAfterTheStatement(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"SELECT id FROM users garbage here WHERE id = $1",
		"SELECT id FROM users LIMIT 1 2",
		"SELECT id FROM (SELECT id FROM users trailing words WHERE id = $1) t",
		"SELECT id FROM users WHERE id IN (SELECT id FROM users nonsense words)",
		"WITH u AS (SELECT id FROM users extra tokens) SELECT id FROM u",
		"SELECT (SELECT max(id) FROM users broken here) AS m",
		"UPDATE users SET name = $1 RETURNING id junk words",
		"DELETE FROM users WHERE id = $1 RETURNING id junk words",
		"INSERT INTO users (id) VALUES ($1) junk",
	} {
		require.ErrorIs(t, analyseQueryError(t, sql), fault.ErrParse, sql)
	}
}

func TestAnalyseQuery_NumberLiteralTypes(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, nil, "SELECT .5 AS a, 1.5 AS b, 2e3 AS c, 7 AS d, 2147483648 AS e, 9223372036854775808 AS f, 5. AS g")

	want := []string{"decimal", "decimal", "double", "integer", "bigint", "hugeint", "decimal"}
	require.Len(t, analysis.OutputColumns, len(want))
	for index, typeName := range want {
		literal, isLiteral := analysis.OutputColumns[index].Expression.(*ast.LiteralExpression)
		require.True(t, isLiteral, analysis.OutputColumns[index].Name)
		assert.Equal(t, typeName, literal.TypeName, analysis.OutputColumns[index].Name)
	}
}

func TestAnalyseQuery_HexPrefixReadsAsZeroAndAnAlias(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, nil, "SELECT 0x1F FROM users")

	require.Len(t, analysis.OutputColumns, 1)
	assert.Equal(t, "x1F", analysis.OutputColumns[0].Name)
	assert.Equal(t, &ast.LiteralExpression{TypeName: "integer"}, analysis.OutputColumns[0].Expression)
}

func nestedTypeCatalogue() *sqlmodel.Catalogue {
	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar")
	integer := sqlmodel.NewIntegerType("int4", 32, false)
	berth := sqlmodel.NewSQLType(sqlmodel.TypeCategoryStruct, "struct")
	berth.StructFields = []sqlmodel.StructField{{Name: "pontoon", SQLType: text}, {Name: "number", SQLType: integer}}
	fees := sqlmodel.NewSQLType(sqlmodel.TypeCategoryMap, "map")
	fees.KeyType = &text
	fees.ElementType = &integer
	catalogue := buildTestCatalogue()
	catalogue.Schemas["main"].Tables["boats"] = &sqlmodel.Table{
		Name:   "boats",
		Schema: "main",
		Columns: []sqlmodel.Column{
			{Name: "id", SQLType: integer},
			{Name: "berth", SQLType: berth},
			{Name: "fees", SQLType: fees},
		},
	}
	return catalogue
}

func TestAnalyseQuery_StructFieldAccess(t *testing.T) {
	t.Parallel()

	berthField := func(field string) ast.Expression {
		return &ast.StructFieldAccessExpression{Struct: &ast.ColumnRefExpression{TableAlias: "", ColumnName: "berth"}, FieldName: field}
	}
	testCases := []struct {
		want     ast.Expression
		name     string
		sql      string
		wantName string
	}{
		{name: "dotted field", sql: "SELECT berth.pontoon FROM boats", wantName: "pontoon", want: berthField("pontoon")},
		{name: "relation-qualified column stays a column", sql: "SELECT boats.berth FROM boats", wantName: "berth", want: nil},
		{
			name: "relation then field", sql: "SELECT b.berth.number AS n FROM boats b", wantName: "n",
			want: &ast.StructFieldAccessExpression{Struct: &ast.ColumnRefExpression{TableAlias: "b", ColumnName: "berth"}, FieldName: "number"},
		},
		{name: "struct_extract with a literal field", sql: "SELECT struct_extract(berth, 'number') AS n FROM boats", wantName: "n", want: berthField("number")},
		{name: "string subscript of a struct column", sql: "SELECT berth['pontoon'] AS p FROM boats", wantName: "p", want: berthField("pontoon")},
		{
			name: "string subscript of a map column stays a subscript keeping its key", sql: "SELECT fees['July'] AS july FROM boats", wantName: "july",
			want: &ast.ArraySubscriptExpression{Array: &ast.ColumnRefExpression{TableAlias: "", ColumnName: "fees"}, Index: &ast.LiteralExpression{TypeName: "text"}, Key: "July"},
		},
		{
			name: "subscript then field", sql: "SELECT route[1].lat AS lat FROM boats", wantName: "lat",
			want: &ast.StructFieldAccessExpression{
				Struct:    &ast.ArraySubscriptExpression{Array: &ast.ColumnRefExpression{TableAlias: "", ColumnName: "route"}, Index: &ast.LiteralExpression{TypeName: "integer"}},
				FieldName: "lat",
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, nestedTypeCatalogue(), testCase.sql)

			require.Len(t, analysis.OutputColumns, 1)
			assert.Equal(t, testCase.wantName, analysis.OutputColumns[0].Name)
			assert.Equal(t, testCase.want, withoutSpans(analysis.OutputColumns[0].Expression))
		})
	}
}

func TestAnalyseQuery_StructFieldAccessInsideLambdas(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
	}{
		{name: "a lambda in the select list", sql: "SELECT list_transform([1, 2], x -> x + berth.number) AS l FROM boats"},
		{name: "a list comprehension in the select list", sql: "SELECT [x + berth.number FOR x IN [1, 2]] AS l FROM boats"},
		{name: "a lambda in a WHERE clause", sql: "SELECT id FROM boats WHERE len(list_filter([1, 2], x -> x = berth.number)) > 0"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, nestedTypeCatalogue(), testCase.sql)

			var fields []string
			var relationColumns []string
			visit := func(root ast.Expression) {
				for node := range ast.Preorder(root) {
					switch typed := node.(type) {
					case *ast.StructFieldAccessExpression:
						fields = append(fields, typed.FieldName)
					case *ast.ColumnRefExpression:
						if typed.TableAlias != "" {
							relationColumns = append(relationColumns, typed.TableAlias+"."+typed.ColumnName)
						}
					}
				}
			}
			for _, column := range analysis.OutputColumns {
				visit(column.Expression)
			}
			for _, clause := range analysis.ClauseExpressions {
				visit(clause.Expression)
			}
			assert.Contains(t, fields, "number")
			assert.Empty(t, relationColumns)
		})
	}
}

func withoutSpans(expression ast.Expression) ast.Expression {
	return rewriteExpression(expression, func(node ast.Expression) ast.Expression {
		switch typed := node.(type) {
		case *ast.ColumnRefExpression:
			unlocated := *typed
			unlocated.Span = ast.Span{}
			return &unlocated
		case *ast.FunctionCallExpression:
			unlocated := *typed
			unlocated.Span = ast.Span{}
			return &unlocated
		default:
			return node
		}
	})
}

func TestAnalyseQuery_PivotColumns(t *testing.T) {
	t.Parallel()

	catalogue := buildTestCatalogue()
	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar")
	integer := sqlmodel.NewIntegerType("int4", 32, false)
	catalogue.Schemas["main"].Tables["landings"] = &sqlmodel.Table{
		Name:    "landings",
		Schema:  "main",
		Columns: []sqlmodel.Column{{Name: "harbour", SQLType: text}, {Name: "species", SQLType: text}, {Name: "kg", SQLType: integer}},
	}
	testCases := []struct {
		name        string
		sql         string
		wantColumns []string
	}{
		{name: "explicit group", sql: "PIVOT landings ON species IN ('cod', 'hake') USING sum(kg) GROUP BY harbour", wantColumns: []string{"harbour", "cod", "hake"}},
		{name: "implicit group", sql: "PIVOT landings ON species IN ('cod') USING sum(kg)", wantColumns: []string{"harbour", "cod"}},
		{name: "aliases and two aggregates", sql: "PIVOT landings ON species IN ('cod' AS c) USING sum(kg) AS total, max(kg) GROUP BY harbour", wantColumns: []string{"harbour", "c_total", "c_max(kg)"}},
		{name: "count by default", sql: "PIVOT_WIDER landings ON species IN ('cod', 'hake') GROUP BY harbour", wantColumns: []string{"harbour", "cod", "hake"}},
		{name: "two pivoted columns", sql: "PIVOT landings ON species IN ('cod'), harbour IN ('a', 'b') USING sum(kg)", wantColumns: []string{"cod_a", "cod_b"}},
		{name: "unpivot", sql: "UNPIVOT landings ON kg INTO NAME measure VALUE amount", wantColumns: []string{"harbour", "species", "measure", "amount"}},
		{name: "unpivot default names", sql: "PIVOT_LONGER landings ON kg", wantColumns: []string{"harbour", "species", "name", "value"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, catalogue, testCase.sql)

			names := make([]string, 0, len(analysis.OutputColumns))
			for _, column := range analysis.OutputColumns {
				names = append(names, column.Name)
			}
			assert.Equal(t, testCase.wantColumns, names)
			assert.Equal(t, []ast.TableReference{{Schema: "", Name: "landings", Alias: ""}}, analysis.FromTables)
		})
	}
}

func TestAnalyseQuery_PivotColumnsFromTheDataAreReported(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"PIVOT landings ON species USING sum(kg) GROUP BY harbour",
		"PIVOT landings ON species, harbour IN ('a') USING sum(kg)",
	} {
		err := analyseQueryError(t, sql)
		require.Error(t, err, sql)
		assert.Contains(t, err.Error(), "list the values to pivot on", sql)
	}
	err := analyseQueryError(t, "PIVOT (SELECT * FROM landings) ON species IN ('cod') USING sum(kg)")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "give PIVOT a GROUP BY")
}

func TestAnalyseQuery_NamedParametersShareNumbersAcrossSubqueries(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, nil, "SELECT id FROM users WHERE id = :id AND age IN (SELECT age FROM users WHERE id = :id)")

	numbers := map[int]bool{}
	for _, reference := range analysis.ParameterReferences {
		numbers[reference.Number] = true
	}
	assert.Len(t, numbers, 1)
}

func TestAnalyseQuery_ColumnsMatcher(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"SELECT id, COLUMNS('^w') FROM boats",
		"SELECT min(COLUMNS(*)) FROM boats",
		"WITH widths AS (SELECT COLUMNS('^w') FROM boats) SELECT * FROM widths",
	} {
		err := analyseQueryError(t, sql)
		require.ErrorIs(t, err, fault.ErrParse, sql)
		assert.ErrorContains(t, err, "COLUMNS(...) in a select list is not supported", sql)
	}

	analysis := analyseQuery(t, nil, "SELECT id FROM boats WHERE COLUMNS('^w') > 1")
	require.Len(t, analysis.OutputColumns, 1)
	_, known := New().BuiltinFunctions().Functions["columns"]
	assert.True(t, known, "COLUMNS(...) in a clause resolves as a known built-in")
}

func TestAnalyseQuery_SelectListIsNeverEmpty(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{"SELECT FROM items", "SELECT FROM", "SELECT DISTINCT FROM items", "SELECT order FROM items"} {
		t.Run(sql, func(t *testing.T) {
			t.Parallel()

			require.ErrorIs(t, analyseQueryError(t, sql), fault.ErrParse)
		})
	}
}

func TestAnalyseQuery_SelectListMayEndInAComma(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		sql        string
		projection string
		columns    []string
	}{
		{sql: "SELECT id, FROM users WHERE id = 1", projection: "id", columns: []string{"id"}},
		{sql: "SELECT id, name, ORDER BY 1", projection: "id, name", columns: []string{"id", "name"}},
		{sql: "SELECT 1 AS a, 2 AS b,", projection: "1 AS a, 2 AS b", columns: []string{"a", "b"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.sql, func(t *testing.T) {
			t.Parallel()

			analysis, statement := analyseStatement(t, testCase.sql)

			names := make([]string, 0, len(analysis.OutputColumns))
			for _, column := range analysis.OutputColumns {
				names = append(names, column.Name)
			}
			require.Equal(t, testCase.columns, names)
			require.Equal(t, testCase.projection, spanTextOf(testCase.sql, statement, analysis.Spans.Projection))
		})
	}
}

func TestAnalyseQuery_SelectListRejectsADoubledComma(t *testing.T) {
	t.Parallel()

	require.ErrorIs(t, analyseQueryError(t, "SELECT id, , name FROM users"), fault.ErrParse)
}

func TestAnalyseQuery_UpdateRejectsQualifiedSetTargets(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		`UPDATE users SET users.name = $1 WHERE id = $2`,
		`UPDATE users AS u SET u.name = $1`,
		`INSERT INTO users (id, name) VALUES ($1, $2) ON CONFLICT (id) DO UPDATE SET users.name = excluded.name`,
	} {
		err := analyseQueryError(t, sql)

		require.ErrorIs(t, err, fault.ErrParse, sql)
		assert.Contains(t, err.Error(), "qualified column names in UPDATE .. SET", sql)
	}
}

func TestAnalyseQuery_NumberLiteralsWithDigitSeparators(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, nil, "SELECT 1_000 AS a, 3_000_000_000 AS b, 1_000.5_5 AS c, 1_000e2 AS d")

	want := []string{"integer", "bigint", "decimal", "double"}
	require.Len(t, analysis.OutputColumns, len(want))
	for index, typeName := range want {
		assert.Equal(t, &ast.LiteralExpression{TypeName: typeName}, analysis.OutputColumns[index].Expression,
			analysis.OutputColumns[index].Name)
	}
}

func TestAnalyseQuery_QuestionMarkPlaceholders(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		sql     string
		numbers []int
	}{
		{sql: "SELECT id FROM users WHERE id = ? AND name = ?", numbers: []int{1, 2}},
		{sql: "SELECT id FROM users WHERE id = $1 AND name = ?", numbers: []int{1, 2}},
		{sql: "SELECT id FROM users WHERE id = ? AND age = $1", numbers: []int{1, 1}},
		{sql: "SELECT id FROM users WHERE id = ?2 AND name = ?1", numbers: []int{2, 1}},
		{sql: "SELECT id FROM users WHERE id = :id AND name = ?", numbers: []int{1, 2}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.sql, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, buildTestCatalogue(), testCase.sql)

			numbers := make([]int, 0, len(analysis.ParameterReferences))
			for _, reference := range analysis.ParameterReferences {
				numbers = append(numbers, reference.Number)
			}
			require.Equal(t, testCase.numbers, numbers)
		})
	}
}

func TestAnalyseQuery_LambdaKeywordForm(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, nil, "SELECT list_transform(tags, lambda tag: upper(tag)) AS a, list_reduce(tags, lambda x, y: x || y) AS b FROM users")

	require.Len(t, analysis.OutputColumns, 2)
	transform, isCall := analysis.OutputColumns[0].Expression.(*ast.FunctionCallExpression)
	require.True(t, isCall)
	require.Len(t, transform.Arguments, 2)
	lambda, isLambda := transform.Arguments[1].(*ast.LambdaExpression)
	require.True(t, isLambda)
	assert.Equal(t, []string{"tag"}, lambda.Parameters)
	reduce, isCall := analysis.OutputColumns[1].Expression.(*ast.FunctionCallExpression)
	require.True(t, isCall)
	pair, isLambda := reduce.Arguments[1].(*ast.LambdaExpression)
	require.True(t, isLambda)
	assert.Equal(t, []string{"x", "y"}, pair.Parameters)
}

func TestAnalyseQuery_TableQuerySelectsEveryColumn(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, buildTestCatalogue(), "TABLE users ORDER BY id LIMIT 5")

	require.Len(t, analysis.OutputColumns, 1)
	assert.True(t, analysis.OutputColumns[0].IsStar)
	require.Len(t, analysis.FromTables, 1)
	assert.Equal(t, "users", analysis.FromTables[0].Name)
	require.ErrorIs(t, analyseQueryError(t, "TABLE users, orders"), fault.ErrParse)
}

func TestStructLiteralsAndStructPackAreStructConstructorsNamingTheirFields(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		wantSpan string
	}{
		{name: "a struct literal", sql: "SELECT {'name': name, key: id, \"quoted\": 1} AS packed FROM boats",
			wantSpan: "{'name': name, key: id, \"quoted\": 1}"},
		{name: "struct_pack", sql: "SELECT struct_pack(name := name, key => id, quoted := 1) AS packed FROM boats"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, nil, testCase.sql)
			require.Len(t, analysis.OutputColumns, 1)
			structure, isStruct := analysis.OutputColumns[0].Expression.(*ast.StructExpression)
			require.True(t, isStruct)
			require.Equal(t, []string{"name", "key", "quoted"}, structure.Names)
			require.Len(t, structure.Values, 3)
			name, isColumn := structure.Values[0].(*ast.ColumnRefExpression)
			require.True(t, isColumn)
			require.Equal(t, "name", name.ColumnName)
			_, isLiteral := structure.Values[2].(*ast.LiteralExpression)
			require.True(t, isLiteral)
			if testCase.wantSpan != "" {
				require.True(t, structure.Span.Known())
				require.Equal(t, testCase.wantSpan, testCase.sql[structure.Span.Start:structure.Span.End])
			}
		})
	}
}

func TestEmptyStructLiteralIsRejected(t *testing.T) {
	t.Parallel()

	require.Error(t, analyseQueryError(t, "SELECT {} AS empty FROM boats"))
}

func TestColonAfterAnOperandIsNoNamedParameter(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		sql            string
		wantParameters int
	}{
		{name: "a lambda without a space", sql: "SELECT list_transform(tags, lambda tag:upper(tag)) AS a FROM users", wantParameters: 0},
		{name: "a lambda of two parameters without a space", sql: "SELECT list_reduce(tags, lambda x, y:x || y) AS b FROM users", wantParameters: 0},
		{name: "a struct literal without a space", sql: "SELECT {'name':name, \"id\":id} AS packed FROM users", wantParameters: 0},
		{name: "a slice without spaces", sql: "SELECT tags[id:id] AS part FROM users", wantParameters: 0},
		{name: "a named parameter after an operator", sql: "SELECT id FROM users WHERE id=:id AND name = :name", wantParameters: 2},
		{name: "a named parameter in a list", sql: "SELECT id FROM users WHERE id IN (:first,:second)", wantParameters: 2},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, nil, testCase.sql)
			require.Len(t, analysis.ParameterReferences, testCase.wantParameters)
		})
	}
}

func TestLambdaWithoutASpaceReadsItsBody(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, nil, "SELECT list_transform(tags, lambda tag:upper(tag)) AS a FROM users")

	transform, isCall := analysis.OutputColumns[0].Expression.(*ast.FunctionCallExpression)
	require.True(t, isCall)
	lambda, isLambda := transform.Arguments[1].(*ast.LambdaExpression)
	require.True(t, isLambda)
	require.Equal(t, []string{"tag"}, lambda.Parameters)
	body, isCall := lambda.Body.(*ast.FunctionCallExpression)
	require.True(t, isCall)
	require.Equal(t, "upper", body.FunctionName)
}

func TestStructInsertGivesItsNamedArgumentsAsOneStructConstructor(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, nil, "SELECT struct_insert(berth, depth := 4, crane => true) AS extended FROM boats")

	require.Len(t, analysis.OutputColumns, 1)
	call, isCall := analysis.OutputColumns[0].Expression.(*ast.FunctionCallExpression)
	require.True(t, isCall)
	require.Equal(t, functionStructInsert, call.FunctionName)
	require.Len(t, call.Arguments, 2)
	column, isColumn := call.Arguments[0].(*ast.ColumnRefExpression)
	require.True(t, isColumn)
	require.Equal(t, "berth", column.ColumnName)
	added, isStruct := call.Arguments[1].(*ast.StructExpression)
	require.True(t, isStruct)
	require.Equal(t, []string{"depth", "crane"}, added.Names)
	require.Len(t, added.Values, 2)
}

func TestStructInsertWithAPositionalValueStaysACall(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, nil, "SELECT struct_insert(berth, 4) AS extended FROM boats")

	call, isCall := analysis.OutputColumns[0].Expression.(*ast.FunctionCallExpression)
	require.True(t, isCall)
	require.Equal(t, "struct_insert", call.FunctionName)
}

func TestRowConstructorsAreRowCalls(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
		want int
	}{
		{name: "ROW with its keyword", sql: "SELECT ROW(id, name, 1) AS packed FROM boats", want: 3},
		{name: "a parenthesised list of values", sql: "SELECT (id, name) AS packed FROM boats", want: 2},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, nil, testCase.sql)

			call, isCall := analysis.OutputColumns[0].Expression.(*ast.FunctionCallExpression)
			require.True(t, isCall)
			require.Equal(t, functionRow, call.FunctionName)
			require.Len(t, call.Arguments, testCase.want)
		})
	}
}

func TestAParenthesisedValueIsNoRow(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, nil, "SELECT (id) AS key, (id + 1) AS next FROM boats")

	require.Equal(t, "id", analysis.OutputColumns[0].ColumnName)
	_, isSum := analysis.OutputColumns[1].Expression.(*ast.BinaryOpExpression)
	require.True(t, isSum)
}

func TestAKeyOfAStructTheQueryBuildsReadsTheField(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
	}{
		{name: "a struct literal", sql: "SELECT {'k': 1, 'j': 'x'}['k'] AS value"},
		{name: "struct_pack", sql: "SELECT struct_pack(k := 1)['k'] AS value"},
		{name: "struct_insert", sql: "SELECT struct_insert({'j': 1}, k := 2)['k'] AS value"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, nil, testCase.sql)

			field, isField := analysis.OutputColumns[0].Expression.(*ast.StructFieldAccessExpression)
			require.True(t, isField)
			require.Equal(t, "k", field.FieldName)
		})
	}
}

func TestParseDML_ColumnNamedLikeJoinKeywordKeepsParameters(t *testing.T) {
	t.Parallel()

	catalogue := buildTestCatalogue()
	analysis := analyseQuery(t, catalogue, `SELECT id FROM users WHERE left = $1 AND id = $2`)
	require.Len(t, analysis.ParameterReferences, 2,
		"a column named like a JOIN keyword must not drop following parameters")
}

func TestNestedQueriesKeepTheirOwnParts(t *testing.T) {
	t.Parallel()

	t.Run("a subquery in a condition carries its query", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, buildTestCatalogue(), "SELECT id FROM users WHERE (SELECT max(p.id) FROM users p) = ?")

		comparison, isComparison := analysis.ClauseExpressions[0].Expression.(*ast.ComparisonExpression)
		require.True(t, isComparison)
		subquery, isSubquery := comparison.Left.(*ast.ScalarSubqueryExpression)
		require.True(t, isSubquery)
		require.Same(t, analysis.PredicateSubqueries[0], subquery.InnerQuery)
		require.Nil(t, analysis.ParameterReferences[0].ColumnReference)
	})

	t.Run("each arm of a set operation keeps its own derived tables and parameters", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, buildTestCatalogue(),
			"SELECT id FROM users WHERE id = ? UNION ALL SELECT d.id FROM (SELECT id FROM users WHERE id = ?) d")

		require.Empty(t, analysis.RawDerivedTables)
		require.Len(t, analysis.ParameterReferences, 2)
		arm := analysis.CompoundBranches[0].Query
		require.Len(t, arm.RawDerivedTables, 1)
		require.Len(t, arm.ParameterReferences, 1)
		require.Equal(t, 2, arm.ParameterReferences[0].Number)
	})

	t.Run("a derived table records its column list", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, buildTestCatalogue(), "SELECT d.a FROM (SELECT id, name FROM users) AS d (a)")

		require.Equal(t, []string{"a"}, analysis.RawDerivedTables[0].ColumnNames)
		require.Equal(t, "id", analysis.RawDerivedTables[0].InnerQuery.OutputColumns[0].Name)
	})

	t.Run("a column of a CTE body's derived table is no struct field", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, buildTestCatalogue(), "WITH a AS (SELECT d.id FROM (SELECT id FROM users) d) SELECT id FROM a")

		column := analysis.CTEDefinitions[0].OutputColumns[0]
		require.Nil(t, column.Expression)
		require.Equal(t, "d", column.TableAlias)
		require.Equal(t, "id", column.ColumnName)
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
		{name: "a VALUES list ordered and limited", sql: "SELECT 1 UNION ALL VALUES (2) ORDER BY 1 DESC LIMIT 1", columns: 1, branches: 1},
		{
			name:    "a VALUES list anchoring a recursive common table expression",
			sql:     "WITH RECURSIVE c(n) AS (VALUES (1) UNION ALL SELECT n + 1 FROM c WHERE n < 3) SELECT n FROM c",
			columns: 1,
		},
		{name: "a VALUES list in a subquery's set operation", sql: "SELECT 1 WHERE 1 IN (VALUES (1) UNION SELECT 2)", columns: 1},
		{name: "a VALUES list binding every row", sql: "WITH d(a, b) AS (VALUES ($1, $2), ($3, $4)) SELECT a, b FROM d", columns: 2, parameters: 4},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, buildTestCatalogue(), test.sql)
			require.Len(t, analysis.OutputColumns, test.columns)
			require.Len(t, analysis.CompoundBranches, test.branches)
			require.Len(t, analysis.ParameterReferences, test.parameters)
			require.True(t, analysis.ReadOnly)
		})
	}
}

func TestAValuesListNeedsItsRowsInParentheses(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{"VALUES", "VALUES 1", "SELECT 1 UNION VALUES (2"} {
		t.Run(sql, func(t *testing.T) {
			t.Parallel()

			require.Error(t, analyseQueryError(t, sql))
		})
	}
}

func TestAValuesListNamesItsColumnsAsDuckDBDoes(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, buildTestCatalogue(), "VALUES (1, 'a'), (2, 'b')")
	names := make([]string, 0, len(analysis.OutputColumns))
	for _, column := range analysis.OutputColumns {
		names = append(names, column.Name)
	}
	require.Equal(t, []string{"col0", "col1"}, names)
}

func TestAnalyseQueryRecordsWhatAnInsertDoesWithAConflictingRow(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
		want ast.ConflictAction
	}{
		{name: "an insert without a conflict clause", sql: "INSERT INTO users (name, email) VALUES ($1, $2)",
			want: ast.ConflictActionNone},
		{name: "ON CONFLICT DO NOTHING", sql: "INSERT INTO users (name, email) VALUES ($1, $2) ON CONFLICT DO NOTHING",
			want: ast.ConflictActionDoNothing},
		{name: "a conflict target and DO NOTHING",
			sql:  "INSERT INTO users (name, email) VALUES ($1, $2) ON CONFLICT (email) WHERE email IS NOT NULL DO NOTHING RETURNING id",
			want: ast.ConflictActionDoNothing},
		{name: "a named constraint and DO NOTHING",
			sql:  "INSERT INTO users (name) VALUES ($1) ON CONFLICT ON CONSTRAINT users_pkey DO NOTHING",
			want: ast.ConflictActionDoNothing},
		{name: "ON CONFLICT DO UPDATE",
			sql:  "INSERT INTO users (name, email) VALUES ($1, $2) ON CONFLICT (email) DO UPDATE SET name = $3 RETURNING id",
			want: ast.ConflictActionDoUpdate},
		{name: "INSERT OR REPLACE", sql: "INSERT OR REPLACE INTO users (name, email) VALUES ($1, $2) RETURNING id",
			want: ast.ConflictActionDoUpdate},
		{name: "INSERT OR IGNORE", sql: "INSERT OR IGNORE INTO users (name, email) VALUES ($1, $2) RETURNING id",
			want: ast.ConflictActionDoNothing},
		{name: "an insert after a WITH clause",
			sql:  "WITH named AS (SELECT 'a' AS name) INSERT INTO users (name) SELECT name FROM named ON CONFLICT DO NOTHING",
			want: ast.ConflictActionDoNothing},
		{name: "a query", sql: "SELECT id FROM users", want: ast.ConflictActionNone},
		{name: "an update", sql: "UPDATE users SET name = $1 WHERE id = $2", want: ast.ConflictActionNone},
		{name: "a delete", sql: "DELETE FROM users WHERE id = $1", want: ast.ConflictActionNone},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, buildTestCatalogue(), testCase.sql)

			require.Equal(t, testCase.want, analysis.ConflictAction)
		})
	}
}
