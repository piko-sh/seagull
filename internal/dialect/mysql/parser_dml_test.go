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

package mysql

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

func analyseQuery(t *testing.T, sql string) *ast.RawQueryAnalysis {
	t.Helper()
	engine := New()
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.NotEmpty(t, statements)
	analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])
	require.NoError(t, err)
	require.NotNil(t, analysis)
	return analysis
}

func TestAnalyseQuery_Select(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		assertions func(t *testing.T, analysis *ast.RawQueryAnalysis)
	}{
		{
			name: "simple select with named columns",
			sql:  "SELECT id, name FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly, "SELECT should be read-only")
				require.Len(t, analysis.OutputColumns, 2)
				assert.Equal(t, "id", analysis.OutputColumns[0].ColumnName)
				assert.Equal(t, "name", analysis.OutputColumns[1].ColumnName)
				require.Len(t, analysis.FromTables, 1)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
			},
		},
		{
			name: "select star",
			sql:  "SELECT * FROM posts;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.NotEmpty(t, analysis.OutputColumns)
				assert.True(t, analysis.OutputColumns[0].IsStar, "star column should have IsStar set")
				require.Len(t, analysis.FromTables, 1)
				assert.Equal(t, "posts", analysis.FromTables[0].Name)
			},
		},
		{
			name: "select with table-qualified star",
			sql:  "SELECT u.* FROM users u;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.NotEmpty(t, analysis.OutputColumns)
				assert.True(t, analysis.OutputColumns[0].IsStar)
				assert.Equal(t, "u", analysis.OutputColumns[0].TableAlias)
			},
		},
		{
			name: "select with WHERE and question-mark parameter",
			sql:  "SELECT id, name FROM users WHERE id = ?;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.FromTables, 1)
				require.NotEmpty(t, analysis.ParameterReferences, "parameter reference should be recorded")
			},
		},
		{
			name: "inner join",
			sql:  "SELECT u.name, p.title FROM users u INNER JOIN posts p ON u.id = p.user_id;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.FromTables, 1)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
				assert.Equal(t, "u", analysis.FromTables[0].Alias)
				require.Len(t, analysis.JoinClauses, 1)
				assert.Equal(t, ast.JoinInner, analysis.JoinClauses[0].Kind)
				assert.Equal(t, "posts", analysis.JoinClauses[0].Table.Name)
				assert.Equal(t, "p", analysis.JoinClauses[0].Table.Alias)
			},
		},
		{
			name: "left join",
			sql:  "SELECT u.name, p.title FROM users u LEFT JOIN posts p ON u.id = p.user_id;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.JoinClauses, 1)
				assert.Equal(t, ast.JoinLeft, analysis.JoinClauses[0].Kind)
			},
		},
		{
			name: "right join",
			sql:  "SELECT u.name, p.title FROM users u RIGHT JOIN posts p ON u.id = p.user_id;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.JoinClauses, 1)
				assert.Equal(t, ast.JoinRight, analysis.JoinClauses[0].Kind)
			},
		},
		{
			name: "cross join",
			sql:  "SELECT a.id, b.id FROM users a CROSS JOIN posts b;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.JoinClauses, 1)
				assert.Equal(t, ast.JoinCross, analysis.JoinClauses[0].Kind)
			},
		},
		{
			name: "select with alias on output column",
			sql:  "SELECT name AS user_name FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
				assert.Equal(t, "user_name", analysis.OutputColumns[0].Name)
			},
		},
		{
			name: "select with GROUP BY",
			sql:  "SELECT user_id, COUNT(*) FROM posts GROUP BY user_id;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.NotEmpty(t, analysis.GroupByColumns)
				assert.Equal(t, "user_id", analysis.GroupByColumns[0].ColumnName)
			},
		},
		{
			name: "select with ORDER BY and LIMIT",
			sql:  "SELECT id FROM users ORDER BY id LIMIT 10;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.FromTables, 1)
			},
		},
		{
			name: "select with LIMIT and OFFSET parameters",
			sql:  "SELECT id FROM users LIMIT ? OFFSET ?;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.ParameterReferences, 2)

				assert.Equal(t, sqlmodel.ParameterContextLimit, analysis.ParameterReferences[0].Context)
				assert.Equal(t, sqlmodel.ParameterContextOffset, analysis.ParameterReferences[1].Context)
			},
		},
		{
			name: "for update marks query as non-read-only",
			sql:  "SELECT id FROM users FOR UPDATE;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.False(t, analysis.ReadOnly, "FOR UPDATE should mark query as non-read-only")
			},
		},
		{
			name: "select from multiple tables via comma join",
			sql:  "SELECT u.id, p.id FROM users u, posts p;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.FromTables, 2)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
				assert.Equal(t, "posts", analysis.FromTables[1].Name)
			},
		},
		{
			name: "union compound query",
			sql:  "SELECT id FROM users UNION SELECT id FROM posts;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.CompoundBranches, 1)
				assert.Equal(t, ast.CompoundUnion, analysis.CompoundBranches[0].Operator)
			},
		},
		{
			name: "union all compound query",
			sql:  "SELECT id FROM users UNION ALL SELECT id FROM posts;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.CompoundBranches, 1)
				assert.Equal(t, ast.CompoundUnionAll, analysis.CompoundBranches[0].Operator)
			},
		},
		{
			name: "schema-qualified table in FROM",
			sql:  "SELECT id FROM mydb.users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.FromTables, 1)
				assert.Equal(t, "mydb", analysis.FromTables[0].Schema)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
			},
		},

		{
			name: "select with HAVING clause",
			sql:  "SELECT user_id, COUNT(*) AS cnt FROM posts GROUP BY user_id HAVING COUNT(*) > 5;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.NotEmpty(t, analysis.GroupByColumns)
				assert.Equal(t, "user_id", analysis.GroupByColumns[0].ColumnName)
				require.Len(t, analysis.OutputColumns, 2)
			},
		},

		{
			name: "select with table-qualified GROUP BY column",
			sql:  "SELECT p.user_id, COUNT(*) FROM posts p GROUP BY p.user_id;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.NotEmpty(t, analysis.GroupByColumns)
				assert.Equal(t, "p", analysis.GroupByColumns[0].TableAlias)
				assert.Equal(t, "user_id", analysis.GroupByColumns[0].ColumnName)
			},
		},

		{
			name: "select from derived table",
			sql:  "SELECT sub.id FROM (SELECT id FROM users) AS sub;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.NotEmpty(t, analysis.RawDerivedTables)
				assert.Equal(t, "sub", analysis.RawDerivedTables[0].Alias)
			},
		},

		{
			name: "select with IN subquery",
			sql:  "SELECT name FROM users WHERE id IN (SELECT user_id FROM orders);",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.FromTables, 1)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
			},
		},

		{
			name: "select with EXISTS subquery",
			sql:  "SELECT name FROM users WHERE EXISTS (SELECT 1 FROM orders WHERE orders.user_id = users.id);",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.FromTables, 1)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
			},
		},

		{
			name: "select with scalar subquery in output",
			sql:  "SELECT id, (SELECT COUNT(*) FROM posts WHERE posts.user_id = users.id) AS post_count FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 2)
			},
		},

		{
			name: "select with AND/OR/NOT in WHERE",
			sql:  "SELECT id FROM users WHERE (active = 1 AND age > ?) OR NOT deleted;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.NotEmpty(t, analysis.ParameterReferences)
			},
		},
		{
			name: "select with BETWEEN in WHERE",
			sql:  "SELECT id FROM users WHERE age BETWEEN ? AND ?;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.ParameterReferences, 2)
			},
		},
		{
			name: "select with IN list parameters",
			sql:  "SELECT id FROM users WHERE status IN (?, ?, ?);",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.ParameterReferences, 3)
				for _, reference := range analysis.ParameterReferences {
					assert.Equal(t, sqlmodel.ParameterContextInList, reference.Context)
				}
			},
		},
		{
			name: "select with LIKE in WHERE",
			sql:  "SELECT id FROM users WHERE name LIKE 'A%';",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.FromTables, 1)
			},
		},
		{
			name: "select with IS NULL in WHERE",
			sql:  "SELECT id FROM users WHERE deleted_at IS NULL;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.FromTables, 1)
			},
		},
		{
			name: "select with IS NOT NULL in WHERE",
			sql:  "SELECT id FROM users WHERE email IS NOT NULL;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.FromTables, 1)
			},
		},

		{
			name: "select with LIMIT parameter only",
			sql:  "SELECT id FROM users LIMIT ?;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextLimit, analysis.ParameterReferences[0].Context)
			},
		},

		{
			name: "select distinct",
			sql:  "SELECT DISTINCT status FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},

		{
			name: "select with function call in output",
			sql:  "SELECT COUNT(*), MAX(age), MIN(age), AVG(age) FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 4)
			},
		},

		{
			name: "select with CASE expression",
			sql:  "SELECT id, CASE WHEN active = 1 THEN 'yes' ELSE 'no' END AS status FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 2)
			},
		},

		{
			name: "select with COALESCE",
			sql:  "SELECT COALESCE(nickname, name) AS display_name FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},

		{
			name: "select with CAST",
			sql:  "SELECT CAST(id AS CHAR) FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},

		{
			name: "select with CTE",
			sql:  "WITH active_users AS (SELECT id, name FROM users WHERE active = 1) SELECT * FROM active_users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.NotEmpty(t, analysis.CTEDefinitions)
				assert.Equal(t, "active_users", analysis.CTEDefinitions[0].Name)
			},
		},

		{
			name: "intersect compound query",
			sql:  "SELECT id FROM users INTERSECT SELECT id FROM admins;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.CompoundBranches, 1)
				assert.Equal(t, ast.CompoundIntersect, analysis.CompoundBranches[0].Operator)
			},
		},
		{
			name: "except compound query",
			sql:  "SELECT id FROM users EXCEPT SELECT id FROM banned;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.CompoundBranches, 1)
				assert.Equal(t, ast.CompoundExcept, analysis.CompoundBranches[0].Operator)
			},
		},

		{
			name: "lock in share mode locks rows, so the query is not read-only",
			sql:  "SELECT id FROM users LOCK IN SHARE MODE;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.False(t, analysis.ReadOnly)
			},
		},

		{
			name: "join with USING clause",
			sql:  "SELECT u.name, p.title FROM users u JOIN posts p USING (user_id);",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.JoinClauses, 1)
				assert.Equal(t, ast.JoinInner, analysis.JoinClauses[0].Kind)
			},
		},

		{
			name: "multiple union compound query",
			sql:  "SELECT id FROM users UNION SELECT id FROM admins UNION ALL SELECT id FROM guests;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)

				require.Len(t, analysis.CompoundBranches, 2)
				assert.Equal(t, ast.CompoundUnion, analysis.CompoundBranches[0].Operator)
				require.NotNil(t, analysis.CompoundBranches[0].Query)
				assert.Empty(t, analysis.CompoundBranches[0].Query.CompoundBranches)
				assert.Equal(t, ast.CompoundUnionAll, analysis.CompoundBranches[1].Operator)
				require.NotNil(t, analysis.CompoundBranches[1].Query)
			},
		},

		{
			name: "select with comma-style LIMIT offset,count",
			sql:  "SELECT id FROM users LIMIT 10, 20;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			analysis := analyseQuery(t, testCase.sql)
			testCase.assertions(t, analysis)
		})
	}
}

func TestAnalyseQuery_WherePredicateSubqueryCaptured(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t,
		"SELECT a.id FROM accounts a WHERE a.id = (SELECT MAX(av2.id) FROM account_versions av2 WHERE av2.id < ?)")

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

	analysis := analyseQuery(t, "SELECT a.id FROM accounts a WHERE (a.id + 1) = ?")
	assert.Empty(t, analysis.PredicateSubqueries)
}

func TestAnalyseQuery_Insert(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		assertions func(t *testing.T, analysis *ast.RawQueryAnalysis)
	}{
		{
			name: "simple insert with values",
			sql:  "INSERT INTO users (name, email) VALUES (?, ?);",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.Equal(t, "users", analysis.InsertTable)
				assert.Equal(t, []string{"name", "email"}, analysis.InsertColumns)
				require.Len(t, analysis.FromTables, 1)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
				require.Len(t, analysis.ParameterReferences, 2)
			},
		},
		{
			name: "insert without column list",
			sql:  "INSERT INTO users VALUES (?, ?, ?);",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.Equal(t, "users", analysis.InsertTable)
				assert.Empty(t, analysis.InsertColumns, "no explicit column list provided")
				require.Len(t, analysis.ParameterReferences, 3)
			},
		},
		{
			name: "insert with ON DUPLICATE KEY UPDATE",
			sql:  "INSERT INTO users (name, email) VALUES (?, ?) ON DUPLICATE KEY UPDATE email = VALUES(email);",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.Equal(t, "users", analysis.InsertTable)
				assert.Equal(t, []string{"name", "email"}, analysis.InsertColumns)
			},
		},
		{
			name: "insert ignore",
			sql:  "INSERT IGNORE INTO users (name) VALUES (?);",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.Equal(t, "users", analysis.InsertTable)
				assert.Equal(t, []string{"name"}, analysis.InsertColumns)
			},
		},
		{
			name: "replace into",
			sql:  "REPLACE INTO users (id, name) VALUES (?, ?);",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.Equal(t, "users", analysis.InsertTable)
				assert.Equal(t, []string{"id", "name"}, analysis.InsertColumns)
			},
		},
		{
			name: "insert into schema-qualified table",
			sql:  "INSERT INTO mydb.users (name) VALUES (?);",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.Equal(t, "users", analysis.InsertTable)
				require.Len(t, analysis.FromTables, 1)
				assert.Equal(t, "mydb", analysis.FromTables[0].Schema)
			},
		},
		{
			name: "insert multiple rows",
			sql:  "INSERT INTO users (name) VALUES (?), (?), (?);",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.Equal(t, "users", analysis.InsertTable)
				assert.Equal(t, []string{"name"}, analysis.InsertColumns)
				require.Len(t, analysis.ParameterReferences, 3)
			},
		},

		{
			name: "insert on duplicate key update with VALUES() expression",
			sql:  "INSERT INTO counters (id, count) VALUES (?, ?) ON DUPLICATE KEY UPDATE count = count + VALUES(count);",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.Equal(t, "counters", analysis.InsertTable)
				assert.Equal(t, []string{"id", "count"}, analysis.InsertColumns)
				require.Len(t, analysis.ParameterReferences, 2)
			},
		},

		{
			name: "insert with SET syntax",
			sql:  "INSERT INTO users SET name = ?, email = ?;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.Equal(t, "users", analysis.InsertTable)
				require.Len(t, analysis.ParameterReferences, 2)
			},
		},

		{
			name: "insert select",
			sql:  "INSERT INTO archive (id, name) SELECT id, name FROM users WHERE active = 0;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.Equal(t, "archive", analysis.InsertTable)
				assert.Equal(t, []string{"id", "name"}, analysis.InsertColumns)

				require.NotNil(t, analysis.InsertSelect)
				require.NotEmpty(t, analysis.InsertSelect.FromTables)
				assert.Equal(t, "users", analysis.InsertSelect.FromTables[0].Name)
			},
		},
		{

			name: "insert select with join and where params",
			sql: "INSERT INTO archive (id, name) " +
				"SELECT u.id, o.name FROM users u INNER JOIN orgs o ON o.id = u.org_id " +
				"WHERE u.id > ? AND o.active = ? " +
				"ON DUPLICATE KEY UPDATE name = VALUES(name);",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.Equal(t, "archive", analysis.InsertTable)
				require.NotNil(t, analysis.InsertSelect)
				require.NotEmpty(t, analysis.InsertSelect.FromTables)
				assert.Equal(t, "users", analysis.InsertSelect.FromTables[0].Name)
				require.NotEmpty(t, analysis.InsertSelect.JoinClauses)
				assert.Equal(t, "orgs", analysis.InsertSelect.JoinClauses[0].Table.Name)
				require.Len(t, analysis.InsertSelect.ParameterReferences, 2)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			analysis := analyseQuery(t, testCase.sql)
			testCase.assertions(t, analysis)
		})
	}
}

func TestAnalyseQuery_Update(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		assertions func(t *testing.T, analysis *ast.RawQueryAnalysis)
	}{
		{
			name: "simple update with WHERE",
			sql:  "UPDATE users SET name = ? WHERE id = ?;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.FromTables, 1)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
				require.Len(t, analysis.ParameterReferences, 2)
			},
		},
		{
			name: "update with join",
			sql:  "UPDATE users u INNER JOIN profiles p ON u.id = p.user_id SET u.name = p.display_name WHERE u.id = ?;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.FromTables, 1)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
				assert.Equal(t, "u", analysis.FromTables[0].Alias)
				require.Len(t, analysis.JoinClauses, 1)
				assert.Equal(t, ast.JoinInner, analysis.JoinClauses[0].Kind)
				assert.Equal(t, "profiles", analysis.JoinClauses[0].Table.Name)
			},
		},
		{
			name: "update multiple columns",
			sql:  "UPDATE users SET name = ?, email = ? WHERE id = ?;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.FromTables, 1)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
				require.Len(t, analysis.ParameterReferences, 3)
			},
		},
		{
			name: "update schema-qualified table",
			sql:  "UPDATE mydb.users SET name = ? WHERE id = ?;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.FromTables, 1)
				assert.Equal(t, "mydb", analysis.FromTables[0].Schema)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
			},
		},
		{
			name: "update with ORDER BY and LIMIT",
			sql:  "UPDATE users SET active = 0 ORDER BY last_login LIMIT 100;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.FromTables, 1)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
			},
		},
		{
			name: "update with left join",
			sql:  "UPDATE users u LEFT JOIN profiles p ON u.id = p.user_id SET u.bio = p.bio WHERE p.user_id IS NOT NULL;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.FromTables, 1)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
				require.Len(t, analysis.JoinClauses, 1)
				assert.Equal(t, ast.JoinLeft, analysis.JoinClauses[0].Kind)
			},
		},
		{
			name: "update with table-qualified SET column",
			sql:  "UPDATE users u INNER JOIN roles r ON u.role_id = r.id SET u.role_name = r.name WHERE u.id = ?;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.FromTables, 1)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
				require.NotEmpty(t, analysis.ParameterReferences)
			},
		},
		{
			name: "update multiple tables via comma join",
			sql:  "UPDATE users u, profiles p SET u.name = p.display_name WHERE u.id = p.user_id;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.FromTables, 2)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
				assert.Equal(t, "profiles", analysis.FromTables[1].Name)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			analysis := analyseQuery(t, testCase.sql)
			testCase.assertions(t, analysis)
		})
	}
}

func TestAnalyseQuery_Delete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		assertions func(t *testing.T, analysis *ast.RawQueryAnalysis)
	}{
		{
			name: "simple delete with WHERE",
			sql:  "DELETE FROM users WHERE id = ?;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.FromTables, 1)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
				require.NotEmpty(t, analysis.ParameterReferences)
			},
		},
		{
			name: "delete all rows",
			sql:  "DELETE FROM sessions;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.FromTables, 1)
				assert.Equal(t, "sessions", analysis.FromTables[0].Name)
			},
		},
		{
			name: "delete with ORDER BY and LIMIT",
			sql:  "DELETE FROM logs ORDER BY created_at LIMIT 1000;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.FromTables, 1)
				assert.Equal(t, "logs", analysis.FromTables[0].Name)
			},
		},
		{
			name: "delete from schema-qualified table",
			sql:  "DELETE FROM mydb.users WHERE id = ?;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.FromTables, 1)
				assert.Equal(t, "mydb", analysis.FromTables[0].Schema)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
			},
		},
		{
			name: "delete with alias",
			sql:  "DELETE FROM users AS u WHERE u.id = ?;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.FromTables, 1)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
				assert.Equal(t, "u", analysis.FromTables[0].Alias)
			},
		},
		{
			name: "delete with multiple parameters",
			sql:  "DELETE FROM users WHERE active = ? AND last_login < ?;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.FromTables, 1)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
				require.Len(t, analysis.ParameterReferences, 2)
			},
		},

		{
			name: "multi-table delete",
			sql:  "DELETE u FROM users u INNER JOIN banned b ON u.id = b.user_id;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotEmpty(t, analysis.FromTables)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
				require.Len(t, analysis.JoinClauses, 1)
				assert.Equal(t, ast.JoinInner, analysis.JoinClauses[0].Kind)
			},
		},
		{
			name: "multi-table delete with WHERE",
			sql:  "DELETE u FROM users u LEFT JOIN orders o ON u.id = o.user_id WHERE o.id IS NULL;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.NotEmpty(t, analysis.FromTables)
				require.Len(t, analysis.JoinClauses, 1)
				assert.Equal(t, ast.JoinLeft, analysis.JoinClauses[0].Kind)
			},
		},

		{
			name: "delete with implicit alias",
			sql:  "DELETE FROM users u WHERE u.id = ?;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.FromTables, 1)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
				assert.Equal(t, "u", analysis.FromTables[0].Alias)
				require.NotEmpty(t, analysis.ParameterReferences)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			analysis := analyseQuery(t, testCase.sql)
			testCase.assertions(t, analysis)
		})
	}
}

func TestAnalyseQuery_NonDML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sql  string
	}{
		{
			name: "CREATE TABLE returns empty analysis",
			sql:  "CREATE TABLE users (id INT);",
		},
		{
			name: "DROP TABLE returns empty analysis",
			sql:  "DROP TABLE users;",
		},
		{
			name: "ALTER TABLE returns empty analysis",
			sql:  "ALTER TABLE users ADD COLUMN age INT;",
		},
		{
			name: "CREATE INDEX returns empty analysis",
			sql:  "CREATE INDEX idx_name ON users (name);",
		},
		{
			name: "CREATE VIEW returns empty analysis",
			sql:  "CREATE VIEW v_active AS SELECT * FROM users WHERE active = 1;",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			analysis := analyseQuery(t, testCase.sql)

			assert.Empty(t, analysis.FromTables, "DDL should produce no FROM tables")
			assert.Empty(t, analysis.OutputColumns, "DDL should produce no output columns")
			assert.Empty(t, analysis.InsertTable, "DDL should produce no insert table")
		})
	}
}

func TestAnalyseQuery_Values(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		assertions func(t *testing.T, analysis *ast.RawQueryAnalysis)
	}{
		{
			name: "simple VALUES row",
			sql:  "VALUES (1, 'alice');",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 2)
			},
		},
		{
			name: "VALUES with parameter",
			sql:  "VALUES (?);",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.NotEmpty(t, analysis.ParameterReferences)
			},
		},
		{
			name: "VALUES with multiple rows",
			sql:  "VALUES (1, 'a'), (2, 'b');",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 2)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			analysis := analyseQuery(t, testCase.sql)
			testCase.assertions(t, analysis)
		})
	}
}

func TestAnalyseQuery_ExistsSubqueryExposesInnerQueryParameters(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t,
		`SELECT EXISTS(SELECT 1 FROM orchestrator_tasks WHERE workflow_id = ? AND status = ?) AS has_incomplete`)

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

func TestAnalyseQuery_SetCaseComparisonParameterTypedFromOperand(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t,
		"UPDATE users SET name = CASE WHEN id >= ? THEN 'flagged' ELSE name END WHERE email = ?")

	require.NotEmpty(t, analysis.ParameterReferences)
	caseParameter := analysis.ParameterReferences[0]
	assert.Equal(t, sqlmodel.ParameterContextComparison, caseParameter.Context,
		"a parameter compared against a column must use comparison context, not assignment")
	require.NotNil(t, caseParameter.ColumnReference)
	assert.Equal(t, "id", caseParameter.ColumnReference.ColumnName,
		"the parameter must be typed from the compared column (id), not the SET target (name)")
}

func TestAnalyseQuery_ParameterContexts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		assertions func(t *testing.T, analysis *ast.RawQueryAnalysis)
	}{
		{
			name: "parameter in comparison context",
			sql:  "SELECT id FROM users WHERE id = ?;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextComparison, analysis.ParameterReferences[0].Context)
				require.NotNil(t, analysis.ParameterReferences[0].ColumnReference)
				assert.Equal(t, "id", analysis.ParameterReferences[0].ColumnReference.ColumnName)
			},
		},
		{
			name: "parameter in assignment context via INSERT",
			sql:  "INSERT INTO users (name) VALUES (?);",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextAssignment, analysis.ParameterReferences[0].Context)
				require.NotNil(t, analysis.ParameterReferences[0].ColumnReference)
				assert.Equal(t, "name", analysis.ParameterReferences[0].ColumnReference.ColumnName)
			},
		},
		{
			name: "parameter in assignment context via UPDATE SET",
			sql:  "UPDATE users SET name = ? WHERE id = 1;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextAssignment, analysis.ParameterReferences[0].Context)
			},
		},
		{
			name: "parameter in function argument context",
			sql:  "SELECT CONCAT(?, name) FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextFunctionArgument, analysis.ParameterReferences[0].Context)
				assert.Equal(t, "concat", analysis.ParameterReferences[0].EnclosingFunctionName)
				assert.Equal(t, 0, analysis.ParameterReferences[0].ArgumentOrdinal)
			},
		},
		{
			name: "parameter in CAST context",
			sql:  "SELECT CAST(? AS UNSIGNED) FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextCast, analysis.ParameterReferences[0].Context)
				require.NotNil(t, analysis.ParameterReferences[0].CastType)
			},
		},
		{
			name: "parameter in BETWEEN context via WHERE",
			sql:  "SELECT id FROM users WHERE created_at BETWEEN ? AND ?;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {

				require.Len(t, analysis.ParameterReferences, 2)
			},
		},
		{
			name: "parameter in IN list context",
			sql:  "SELECT id FROM users WHERE id IN (?);",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextInList, analysis.ParameterReferences[0].Context)
			},
		},
		{
			name: "parameter in OFFSET context",
			sql:  "SELECT id FROM users OFFSET ?;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextOffset, analysis.ParameterReferences[0].Context)
			},
		},
		{
			name: "LIKE pattern with direct column LHS",
			sql:  "SELECT id FROM users WHERE name LIKE ?;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextLike, analysis.ParameterReferences[0].Context)
				require.NotNil(t, analysis.ParameterReferences[0].ColumnReference)
				assert.Equal(t, "name", analysis.ParameterReferences[0].ColumnReference.ColumnName)
			},
		},
		{
			name: "LIKE pattern wrapped in concat picks LHS column",
			sql:  "SELECT id FROM users WHERE name LIKE CONCAT('%', ?, '%');",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextLike, analysis.ParameterReferences[0].Context)
				require.NotNil(t, analysis.ParameterReferences[0].ColumnReference)
				assert.Equal(t, "name", analysis.ParameterReferences[0].ColumnReference.ColumnName)
			},
		},
		{
			name: "NOT LIKE binds to LHS column",
			sql:  "SELECT id FROM users WHERE name NOT LIKE ?;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextLike, analysis.ParameterReferences[0].Context)
				require.NotNil(t, analysis.ParameterReferences[0].ColumnReference)
				assert.Equal(t, "name", analysis.ParameterReferences[0].ColumnReference.ColumnName)
			},
		},
		{
			name: "MySQL RLIKE classifies as Like context",
			sql:  "SELECT id FROM users WHERE email RLIKE ?;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextLike, analysis.ParameterReferences[0].Context)
				require.NotNil(t, analysis.ParameterReferences[0].ColumnReference)
				assert.Equal(t, "email", analysis.ParameterReferences[0].ColumnReference.ColumnName)
			},
		},
		{
			name: "MySQL REGEXP classifies as Like context",
			sql:  "SELECT id FROM users WHERE email REGEXP ?;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextLike, analysis.ParameterReferences[0].Context)
				require.NotNil(t, analysis.ParameterReferences[0].ColumnReference)
				assert.Equal(t, "email", analysis.ParameterReferences[0].ColumnReference.ColumnName)
			},
		},
		{
			name: "LIKE with function-wrapped column picks the column",
			sql:  "SELECT id FROM users WHERE LOWER(name) LIKE ?;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextLike, analysis.ParameterReferences[0].Context)
				require.NotNil(t, analysis.ParameterReferences[0].ColumnReference)
				assert.Equal(t, "name", analysis.ParameterReferences[0].ColumnReference.ColumnName)
			},
		},
		{
			name: "LIKE with table-qualified LHS preserves the alias",
			sql:  "SELECT id FROM users u WHERE u.email LIKE ?;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextLike, analysis.ParameterReferences[0].Context)
				require.NotNil(t, analysis.ParameterReferences[0].ColumnReference)
				assert.Equal(t, "u", analysis.ParameterReferences[0].ColumnReference.TableAlias)
				assert.Equal(t, "email", analysis.ParameterReferences[0].ColumnReference.ColumnName)
			},
		},
		{
			name: "two LIKE patterns on different columns each bind to their own LHS",
			sql:  "SELECT id FROM users WHERE name LIKE ? OR email LIKE ?;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
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
			analysis := analyseQuery(t, testCase.sql)
			testCase.assertions(t, analysis)
		})
	}
}

func parameterByNumber(
	t *testing.T,
	analysis *ast.RawQueryAnalysis,
	number int,
) ast.RawParameterReference {
	t.Helper()
	for _, reference := range analysis.ParameterReferences {
		if reference.Number == number {
			return reference
		}
	}
	require.Failf(t, "no parameter reference", "no parameter reference with number %d", number)
	return ast.RawParameterReference{}
}

func TestAnalyseQuery_FunctionArgumentMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		assertions func(t *testing.T, analysis *ast.RawQueryAnalysis)
	}{
		{
			name: "scalar builtin argument records name and ordinal zero",
			sql:  "SELECT SUBSTRING_INDEX(?, '/', 1) FROM items;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.ParameterReferences, 1)
				reference := analysis.ParameterReferences[0]
				assert.Equal(t, sqlmodel.ParameterContextFunctionArgument, reference.Context)
				assert.Equal(t, "substring_index", reference.EnclosingFunctionName)
				assert.Equal(t, 0, reference.ArgumentOrdinal)
			},
		},
		{
			name: "placeholder ordinal counts a preceding non-placeholder argument",
			sql:  "SELECT SUBSTRING_INDEX(name, ?, 1) FROM items;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.ParameterReferences, 1)
				reference := analysis.ParameterReferences[0]
				assert.Equal(t, sqlmodel.ParameterContextFunctionArgument, reference.Context)
				assert.Equal(t, "substring_index", reference.EnclosingFunctionName)
				assert.Equal(t, 1, reference.ArgumentOrdinal)
			},
		},
		{
			name: "multiple placeholders take their own argument slots",
			sql:  "SELECT CONCAT(?, name, ?) FROM items;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.ParameterReferences, 2)
				first := parameterByNumber(t, analysis, 1)
				second := parameterByNumber(t, analysis, 2)
				assert.Equal(t, "concat", first.EnclosingFunctionName)
				assert.Equal(t, 0, first.ArgumentOrdinal)
				assert.Equal(t, "concat", second.EnclosingFunctionName)
				assert.Equal(t, 2, second.ArgumentOrdinal)
			},
		},
		{
			name: "function argument in WHERE uses the flat-scan path",
			sql:  "SELECT id FROM items WHERE quantity = LEAST(?, 5);",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.ParameterReferences, 1)
				reference := analysis.ParameterReferences[0]
				assert.Equal(t, sqlmodel.ParameterContextFunctionArgument, reference.Context)
				assert.Equal(t, "least", reference.EnclosingFunctionName)
				assert.Equal(t, 0, reference.ArgumentOrdinal)
			},
		},
		{
			name: "second argument in WHERE flat-scan path",
			sql:  "SELECT id FROM items WHERE quantity = LEAST(5, ?);",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.ParameterReferences, 1)
				reference := analysis.ParameterReferences[0]
				assert.Equal(t, sqlmodel.ParameterContextFunctionArgument, reference.Context)
				assert.Equal(t, "least", reference.EnclosingFunctionName)
				assert.Equal(t, 1, reference.ArgumentOrdinal)
			},
		},
		{
			name: "nested call ordinals are isolated per call",
			sql:  "SELECT outer_fn(?, inner_fn(?, ?)) FROM items;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.ParameterReferences, 3)
				outer := parameterByNumber(t, analysis, 1)
				assert.Equal(t, "outer_fn", outer.EnclosingFunctionName)
				assert.Equal(t, 0, outer.ArgumentOrdinal)
				innerFirst := parameterByNumber(t, analysis, 2)
				assert.Equal(t, "inner_fn", innerFirst.EnclosingFunctionName)
				assert.Equal(t, 0, innerFirst.ArgumentOrdinal)
				innerSecond := parameterByNumber(t, analysis, 3)
				assert.Equal(t, "inner_fn", innerSecond.EnclosingFunctionName)
				assert.Equal(t, 1, innerSecond.ArgumentOrdinal)
			},
		},
		{
			name: "schema-qualified function name is lower-cased and qualified",
			sql:  "SELECT MyApp.Compute(name, ?) FROM items;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.ParameterReferences, 1)
				reference := analysis.ParameterReferences[0]
				assert.Equal(t, sqlmodel.ParameterContextFunctionArgument, reference.Context)
				assert.Equal(t, "myapp.compute", reference.EnclosingFunctionName)
				assert.Equal(t, 1, reference.ArgumentOrdinal)
			},
		},
		{
			name: "from-clause table function argument records name and ordinal",
			sql:  "SELECT * FROM my_table_func(?, 7) AS t;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.ParameterReferences, 1)
				reference := analysis.ParameterReferences[0]
				assert.Equal(t, sqlmodel.ParameterContextFunctionArgument, reference.Context)
				assert.Equal(t, "my_table_func", reference.EnclosingFunctionName)
				assert.Equal(t, 0, reference.ArgumentOrdinal)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			analysis := analyseQuery(t, testCase.sql)
			testCase.assertions(t, analysis)
		})
	}
}

func TestAnalyseQuery_InsertSelectProjectionColumnReference(t *testing.T) {
	t.Parallel()

	t.Run("bare projection placeholder gets the target column", func(t *testing.T) {
		t.Parallel()
		analysis := analyseQuery(t,
			"INSERT INTO posts (user_id, title) SELECT ?, name FROM users WHERE id > ?;")
		require.NotNil(t, analysis.InsertSelect)
		require.Len(t, analysis.ParameterReferences, 2)

		projection := parameterByNumber(t, analysis, 1)
		assert.Equal(t, sqlmodel.ParameterContextAssignment, projection.Context)
		require.NotNil(t, projection.ColumnReference)
		assert.Equal(t, "posts", projection.ColumnReference.TableAlias)
		assert.Equal(t, "user_id", projection.ColumnReference.ColumnName)

		whereParameter := parameterByNumber(t, analysis, 2)
		require.NotNil(t, whereParameter.ColumnReference)
		assert.Equal(t, "id", whereParameter.ColumnReference.ColumnName)
		assert.NotEqual(t, "user_id", whereParameter.ColumnReference.ColumnName)
	})

	t.Run("non-placeholder projection items still consume target ordinals", func(t *testing.T) {
		t.Parallel()
		analysis := analyseQuery(t,
			"INSERT INTO sessions (id, account_id, status, reason) "+
				"SELECT UUID(), s.account_id, 'deleted', ? FROM sessions s WHERE s.id = ?;")
		require.NotNil(t, analysis.InsertSelect)

		projection := parameterByNumber(t, analysis, 1)
		assert.Equal(t, sqlmodel.ParameterContextAssignment, projection.Context)
		require.NotNil(t, projection.ColumnReference)
		assert.Equal(t, "sessions", projection.ColumnReference.TableAlias)
		assert.Equal(t, "reason", projection.ColumnReference.ColumnName)

		whereParameter := parameterByNumber(t, analysis, 2)
		require.NotNil(t, whereParameter.ColumnReference)
		assert.Equal(t, "id", whereParameter.ColumnReference.ColumnName)
	})

	t.Run("nested subquery placeholder does not take the target column", func(t *testing.T) {
		t.Parallel()
		analysis := analyseQuery(t,
			"INSERT INTO posts (a, b) SELECT ?, (SELECT MAX(x) FROM t2 WHERE y = ?) FROM t1;")
		require.NotNil(t, analysis.InsertSelect)

		projection := parameterByNumber(t, analysis, 1)
		assert.Equal(t, sqlmodel.ParameterContextAssignment, projection.Context)
		require.NotNil(t, projection.ColumnReference)
		assert.Equal(t, "a", projection.ColumnReference.ColumnName)

		nested := parameterByNumber(t, analysis, 2)
		if nested.ColumnReference != nil {
			assert.NotEqual(t, "b", nested.ColumnReference.ColumnName)
			assert.NotEqual(t, "a", nested.ColumnReference.ColumnName)
		}
	})
}

func TestAnalyseQuery_Expressions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		assertions func(t *testing.T, analysis *ast.RawQueryAnalysis)
	}{
		{
			name: "arithmetic expressions in output",
			sql:  "SELECT price * quantity AS total, price + tax AS with_tax FROM orders;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 2)
			},
		},
		{
			name: "boolean TRUE and FALSE literals",
			sql:  "SELECT id FROM users WHERE active = TRUE AND deleted = FALSE;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "NOT BETWEEN expression",
			sql:  "SELECT id FROM users WHERE age NOT BETWEEN 18 AND 65;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "NOT IN expression",
			sql:  "SELECT id FROM users WHERE status NOT IN ('banned', 'suspended');",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "NOT LIKE expression",
			sql:  "SELECT id FROM users WHERE name NOT LIKE '%test%';",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "CONVERT expression",
			sql:  "SELECT CONVERT(price, DECIMAL(10,2)) FROM products;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "IF expression",
			sql:  "SELECT IF(active = 1, 'yes', 'no') AS status FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "INTERVAL literal",
			sql:  "SELECT id FROM events WHERE created_at > NOW() - INTERVAL 30 DAY;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "GROUP_CONCAT function",
			sql:  "SELECT user_id, GROUP_CONCAT(DISTINCT tag ORDER BY tag SEPARATOR ', ') FROM tags GROUP BY user_id;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.NotEmpty(t, analysis.GroupByColumns)
			},
		},
		{
			name: "TRIM function",
			sql:  "SELECT TRIM(LEADING ' ' FROM name) FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "EXTRACT function",
			sql:  "SELECT EXTRACT(YEAR FROM created_at) FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "window function",
			sql:  "SELECT id, ROW_NUMBER() OVER (ORDER BY id) AS rn FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 2)
			},
		},
		{
			name: "window function with PARTITION BY",
			sql:  "SELECT dept_id, name, ROW_NUMBER() OVER (PARTITION BY dept_id ORDER BY name) AS rn FROM employees;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 3)
			},
		},
		{
			name: "JSON arrow operator",
			sql:  "SELECT data->'$.name' FROM documents;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "JSON double arrow operator",
			sql:  "SELECT data->>'$.name' FROM documents;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "unary negation in output",
			sql:  "SELECT -amount FROM transactions;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "hex string literal",
			sql:  "SELECT X'48454C4C4F' AS hex_val;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "string literal in output",
			sql:  "SELECT 'hello' AS greeting;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "numeric literals in output",
			sql:  "SELECT 42, 3.14, 1e10;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 3)
			},
		},
		{
			name: "COLLATE expression",
			sql:  "SELECT name COLLATE utf8mb4_bin FROM users ORDER BY name COLLATE utf8mb4_bin;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "implicit function identifiers",
			sql:  "SELECT CURRENT_TIMESTAMP, CURRENT_DATE, CURRENT_USER;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 3)
			},
		},

		{
			name: "IS NULL expression in output column",
			sql:  "SELECT (email IS NULL) AS missing_email FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "IS NOT NULL expression in output column",
			sql:  "SELECT (email IS NOT NULL) AS has_email FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "BETWEEN expression in output column",
			sql:  "SELECT (age BETWEEN 18 AND 65) AS working_age FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "IN list expression in output column",
			sql:  "SELECT (status IN ('active', 'pending')) AS is_valid FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "LIKE expression in output column",
			sql:  "SELECT (name LIKE 'A%') AS starts_with_a FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "NOT LIKE expression in output column",
			sql:  "SELECT (name NOT LIKE '%test%') AS not_test FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "NOT IN expression in output column",
			sql:  "SELECT (status NOT IN ('banned', 'deleted')) AS is_ok FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "NOT BETWEEN expression in output column",
			sql:  "SELECT (age NOT BETWEEN 0 AND 17) AS is_adult FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "OR expression in output column",
			sql:  "SELECT (active = 1 OR role = 'admin') AS has_access FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "AND expression in output column",
			sql:  "SELECT (active = 1 AND verified = 1) AS fully_active FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "NOT expression in output column",
			sql:  "SELECT NOT deleted AS is_active FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "NULL literal in output column",
			sql:  "SELECT NULL AS nothing;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "boolean TRUE/FALSE in output column",
			sql:  "SELECT TRUE AS yes, FALSE AS no;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 2)
			},
		},
		{
			name: "INTERVAL expression in output",
			sql:  "SELECT NOW() + INTERVAL 7 DAY AS next_week;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "REGEXP expression in output column",
			sql:  "SELECT (name REGEXP '^[A-Z]') AS starts_upper FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "comparison operators in output",
			sql:  "SELECT (a > b) AS gt, (a < b) AS lt, (a >= b) AS gte, (a <= b) AS lte, (a <> b) AS ne FROM t;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 5)
			},
		},
		{
			name: "bitwise operators in output",
			sql:  "SELECT (a & b) AS band, (a | b) AS bor, (a ^ b) AS bxor FROM t;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 3)
			},
		},
		{
			name: "parenthesised subquery in output column",
			sql:  "SELECT id, (SELECT MAX(score) FROM scores WHERE scores.user_id = users.id) AS max_score FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 2)
			},
		},
		{
			name: "schema-qualified function call",
			sql:  "SELECT mydb.my_func(1) AS result;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "LIKE pattern in output column",
			sql:  "SELECT (name LIKE '%test%') AS matches FROM users",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "IN subquery in output column",
			sql:  "SELECT (id IN (SELECT user_id FROM orders)) AS has_orders FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "EXISTS in output column",
			sql:  "SELECT EXISTS(SELECT 1 FROM orders WHERE orders.user_id = users.id) AS has_orders FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "CASE with searched WHEN clauses",
			sql:  "SELECT CASE status WHEN 'active' THEN 1 WHEN 'inactive' THEN 0 ELSE -1 END AS status_code FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "COALESCE with parameter",
			sql:  "SELECT COALESCE(nickname, ?) AS display FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.NotEmpty(t, analysis.ParameterReferences)
			},
		},
		{
			name: "CONVERT with USING",
			sql:  "SELECT CONVERT(name USING utf8mb4) FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "IS NULL expression in output column",
			sql:  "SELECT (email IS NULL) AS missing_email FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "IS NOT NULL expression in output column",
			sql:  "SELECT (email IS NOT NULL) AS has_email FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "BETWEEN expression in output column",
			sql:  "SELECT (age BETWEEN 18 AND 65) AS working_age FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "IN list expression in output column",
			sql:  "SELECT (status IN ('active', 'pending')) AS is_valid FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "LIKE expression in output column",
			sql:  "SELECT (name LIKE 'A%') AS starts_with_a FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "NOT IN expression in output column",
			sql:  "SELECT (status NOT IN ('banned', 'deleted')) AS is_ok FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "OR expression in output column",
			sql:  "SELECT (active = 1 OR role = 'admin') AS has_access FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "AND expression in output column",
			sql:  "SELECT (active = 1 AND verified = 1) AS fully_active FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "REGEXP expression in output column",
			sql:  "SELECT (name REGEXP '^[A-Z]') AS starts_upper FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "SOUNDS LIKE in output column",
			sql:  "SELECT (name SOUNDS LIKE 'john') AS matches FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "ROW constructor in output",
			sql:  "SELECT ROW(1, 2, 3);",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "BINARY prefix in output",
			sql:  "SELECT BINARY name FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "schema.table.column reference in output",
			sql:  "SELECT mydb.users.name FROM mydb.users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "IN subquery in output column",
			sql:  "SELECT (id IN (SELECT user_id FROM orders)) AS has_orders FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "EXISTS in output column",
			sql:  "SELECT EXISTS(SELECT 1 FROM orders WHERE orders.user_id = users.id) AS has_orders FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "BETWEEN with parameters in output column",
			sql:  "SELECT (price BETWEEN ? AND ?) AS in_range FROM products;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.ParameterReferences, 2)
				assert.Equal(t, sqlmodel.ParameterContextBetween, analysis.ParameterReferences[0].Context)
				assert.Equal(t, sqlmodel.ParameterContextBetween, analysis.ParameterReferences[1].Context)
			},
		},
		{
			name: "IN list with parameters in output column",
			sql:  "SELECT (status IN (?, ?)) AS matches FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.ParameterReferences, 2)
				assert.Equal(t, sqlmodel.ParameterContextInList, analysis.ParameterReferences[0].Context)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			analysis := analyseQuery(t, testCase.sql)
			testCase.assertions(t, analysis)
		})
	}
}

func TestAnalyseQuery_NamedParameters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		assertions func(t *testing.T, analysis *ast.RawQueryAnalysis)
	}{
		{
			name: "named parameter in WHERE",
			sql:  "SELECT id FROM users WHERE id = :user_id;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, "user_id", analysis.ParameterReferences[0].Name)
			},
		},
		{
			name: "named parameters in INSERT",
			sql:  "INSERT INTO users (name, email) VALUES (:name, :email);",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.Equal(t, "users", analysis.InsertTable)
				require.Len(t, analysis.ParameterReferences, 2)
				assert.Equal(t, "name", analysis.ParameterReferences[0].Name)
				assert.Equal(t, "email", analysis.ParameterReferences[1].Name)
			},
		},
		{
			name: "repeated named parameter gets same number",
			sql:  "SELECT id FROM users WHERE name = :search OR email = :search;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.ParameterReferences, 2)
				assert.Equal(t, analysis.ParameterReferences[0].Number, analysis.ParameterReferences[1].Number,
					"repeated named parameter should share the same number")
			},
		},
		{
			name: "named parameter in UPDATE SET",
			sql:  "UPDATE users SET name = :name WHERE id = :id;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				require.Len(t, analysis.ParameterReferences, 2)
				assert.Equal(t, "name", analysis.ParameterReferences[0].Name)
				assert.Equal(t, "id", analysis.ParameterReferences[1].Name)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			analysis := analyseQuery(t, testCase.sql)
			testCase.assertions(t, analysis)
		})
	}
}

func TestParseStatements_Delimiter(t *testing.T) {
	t.Parallel()

	engine := New()

	t.Run("custom delimiter separates statements", func(t *testing.T) {
		t.Parallel()
		sql := "DELIMITER //\nCREATE TABLE a (id INT)//\nCREATE TABLE b (id INT)//\nDELIMITER ;"
		statements, err := engine.ParseStatements(t.Context(), sql)
		require.NoError(t, err)
		require.Len(t, statements, 2)
	})

	t.Run("delimiter reset to semicolon", func(t *testing.T) {
		t.Parallel()
		sql := "DELIMITER //\nCREATE TABLE a (id INT)//\nDELIMITER ;\nCREATE TABLE b (id INT);"
		statements, err := engine.ParseStatements(t.Context(), sql)
		require.NoError(t, err)
		require.Len(t, statements, 2)
	})
}

func TestAnalyseQuery_Select_WindowsAndCTEs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		assertions func(t *testing.T, analysis *ast.RawQueryAnalysis)
	}{
		{
			name: "select with FOR UPDATE OF table",
			sql:  "SELECT id FROM users FOR UPDATE OF users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.False(t, analysis.ReadOnly, "FOR UPDATE should mark query as non-read-only")
			},
		},
		{
			name: "select with window ROWS BETWEEN frame",
			sql:  "SELECT id, SUM(amount) OVER (ORDER BY id ROWS BETWEEN 1 PRECEDING AND 1 FOLLOWING) AS running FROM orders;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 2)
			},
		},
		{
			name: "select with window RANGE UNBOUNDED",
			sql:  "SELECT id, SUM(amount) OVER (ORDER BY id RANGE BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) AS cumulative FROM orders;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "select with comma-style LIMIT using parameters",
			sql:  "SELECT id FROM users LIMIT ?, ?;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.ParameterReferences, 2)

				assert.Equal(t, sqlmodel.ParameterContextOffset, analysis.ParameterReferences[0].Context)
				assert.Equal(t, sqlmodel.ParameterContextLimit, analysis.ParameterReferences[1].Context)
			},
		},
		{
			name: "CTE with column names",
			sql:  "WITH numbered(id, rn) AS (SELECT id, ROW_NUMBER() OVER (ORDER BY id) FROM users) SELECT * FROM numbered;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.NotEmpty(t, analysis.CTEDefinitions)
				assert.Equal(t, "numbered", analysis.CTEDefinitions[0].Name)
				require.Len(t, analysis.CTEDefinitions[0].OutputColumns, 2)
				assert.Equal(t, []string{"id", "rn"}, analysis.CTEDefinitions[0].ColumnNames)
			},
		},
		{
			name: "recursive CTE",
			sql: `WITH RECURSIVE cte AS (
				SELECT 1 AS n
				UNION ALL
				SELECT n + 1 FROM cte WHERE n < 10
			) SELECT n FROM cte;`,
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.NotEmpty(t, analysis.CTEDefinitions)
				assert.True(t, analysis.CTEDefinitions[0].IsRecursive)
			},
		},
		{
			name: "select with expression alias without AS",
			sql:  "SELECT 1 + 2 total;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
				assert.Equal(t, "total", analysis.OutputColumns[0].Name)
			},
		},
		{
			name: "select with division and modulo operators",
			sql:  "SELECT (a / b) AS quotient, (a % b) AS remainder FROM t;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 2)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			analysis := analyseQuery(t, testCase.sql)
			testCase.assertions(t, analysis)
		})
	}
}

func TestAnalyseQuery_SelectExpressions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		assertions func(t *testing.T, analysis *ast.RawQueryAnalysis)
	}{
		{
			name: "bang (!) negation operator",
			sql:  "SELECT !active AS inactive FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "comparison with ANY subquery",
			sql:  "SELECT id FROM users WHERE salary > ANY (SELECT salary FROM employees);",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "comparison with ALL subquery",
			sql:  "SELECT id FROM users WHERE salary > ALL (SELECT salary FROM employees);",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "comparison with SOME subquery",
			sql:  "SELECT id FROM users WHERE salary = SOME (SELECT salary FROM employees);",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "IS TRUE expression",
			sql:  "SELECT (active IS TRUE) AS definitely_active FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "IS FALSE expression",
			sql:  "SELECT (active IS FALSE) AS definitely_inactive FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "IS NOT TRUE expression",
			sql:  "SELECT (active IS NOT TRUE) AS not_active FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "IS UNKNOWN expression",
			sql:  "SELECT (active IS UNKNOWN) AS mystery FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "LIKE with ESCAPE clause",
			sql:  "SELECT id FROM users WHERE name LIKE '%!_%' ESCAPE '!';",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "tilde bitwise NOT",
			sql:  "SELECT ~flags AS inverted FROM t;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 1)
			},
		},
		{
			name: "unary plus",
			sql:  "SELECT +amount FROM orders;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "CAST with DOUBLE PRECISION type",
			sql:  "SELECT CAST(value AS DOUBLE PRECISION) FROM t;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "CAST with UNSIGNED type",
			sql:  "SELECT CAST(x AS UNSIGNED) FROM t;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "shift operators",
			sql:  "SELECT (a << 2) AS lshift, (a >> 1) AS rshift FROM t;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.OutputColumns, 2)
			},
		},
		{
			name: "null-safe comparison operator",
			sql:  "SELECT (a <=> b) AS safe_eq FROM t;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "not-equal operator !=",
			sql:  "SELECT (a != b) AS neq FROM t;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "bit string literal",
			sql:  "SELECT b'10101010' AS bits;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "window function with named window reference",
			sql:  "SELECT id, ROW_NUMBER() OVER w AS rn FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "window function with empty OVER()",
			sql:  "SELECT id, ROW_NUMBER() OVER () AS rn FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "table with USE INDEX hint",
			sql:  "SELECT id FROM users USE INDEX (idx_name) WHERE name = 'alice';",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.FromTables, 1)
				assert.Equal(t, "users", analysis.FromTables[0].Name)
			},
		},
		{
			name: "table with FORCE INDEX hint",
			sql:  "SELECT id FROM users FORCE INDEX (PRIMARY) WHERE id > 100;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.FromTables, 1)
			},
		},
		{
			name: "table with IGNORE INDEX hint",
			sql:  "SELECT id FROM users IGNORE INDEX (idx_name) WHERE name = 'bob';",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.FromTables, 1)
			},
		},
		{
			name: "CAST parameter in expression context",
			sql:  "SELECT CAST(? AS CHAR) FROM users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.ParameterReferences, 1)
				assert.Equal(t, sqlmodel.ParameterContextCast, analysis.ParameterReferences[0].Context)
			},
		},
		{
			name: "schema-qualified column in schema.table.column form",
			sql:  "SELECT mydb.users.id FROM mydb.users;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "schema-qualified function call",
			sql:  "SELECT mydb.my_schema.custom_fn(1) AS result;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
			},
		},
		{
			name: "derived table without explicit AS keyword",
			sql:  "SELECT sub.id FROM (SELECT id FROM users) sub;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.NotEmpty(t, analysis.RawDerivedTables)
				assert.Equal(t, "sub", analysis.RawDerivedTables[0].Alias)
			},
		},
		{
			name: "NATURAL JOIN",
			sql:  "SELECT * FROM users NATURAL JOIN profiles;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.JoinClauses, 1)
			},
		},
		{
			name: "STRAIGHT_JOIN",
			sql:  "SELECT * FROM users STRAIGHT_JOIN posts ON users.id = posts.user_id;",
			assertions: func(t *testing.T, analysis *ast.RawQueryAnalysis) {
				assert.True(t, analysis.ReadOnly)
				require.Len(t, analysis.JoinClauses, 1)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			analysis := analyseQuery(t, testCase.sql)
			testCase.assertions(t, analysis)
		})
	}
}

func TestAnalyseQuery_CTEWithDML(t *testing.T) {
	t.Parallel()

	t.Run("CTE with INSERT body marks query non-read-only", func(t *testing.T) {
		t.Parallel()
		analysis := analyseQuery(t, "WITH inserted AS (INSERT INTO archive (id) VALUES (1)) SELECT * FROM inserted;")
		require.NotNil(t, analysis)
		assert.False(t, analysis.ReadOnly, "data-modifying CTE should mark query as non-read-only")
	})

	t.Run("CTE with UPDATE body marks query non-read-only", func(t *testing.T) {
		t.Parallel()
		analysis := analyseQuery(t, "WITH updated AS (UPDATE users SET active = 0 WHERE id = 1) SELECT * FROM updated;")
		require.NotNil(t, analysis)
		assert.False(t, analysis.ReadOnly, "data-modifying CTE should mark query as non-read-only")
	})

	t.Run("CTE with DELETE body marks query non-read-only", func(t *testing.T) {
		t.Parallel()
		analysis := analyseQuery(t, "WITH deleted AS (DELETE FROM users WHERE id = 1) SELECT * FROM deleted;")
		require.NotNil(t, analysis)
		assert.False(t, analysis.ReadOnly, "data-modifying CTE should mark query as non-read-only")
	})

	t.Run("CTE with VALUES body remains read-only", func(t *testing.T) {
		t.Parallel()
		analysis := analyseQuery(t, "WITH data AS (VALUES (1, 'a'), (2, 'b')) SELECT * FROM data;")
		require.NotNil(t, analysis)
		assert.True(t, analysis.ReadOnly, "CTE with VALUES should remain read-only")
	})
}

func TestAnalyseQuery_Values_OrderByAndLimit(t *testing.T) {
	t.Parallel()

	t.Run("VALUES with ORDER BY", func(t *testing.T) {
		t.Parallel()
		analysis := analyseQuery(t, "VALUES (3, 'c'), (1, 'a'), (2, 'b') ORDER BY 1;")
		require.NotNil(t, analysis)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("VALUES with LIMIT", func(t *testing.T) {
		t.Parallel()
		analysis := analyseQuery(t, "VALUES (1), (2), (3) LIMIT 2;")
		require.NotNil(t, analysis)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("VALUES with OFFSET", func(t *testing.T) {
		t.Parallel()
		analysis := analyseQuery(t, "VALUES (1), (2), (3) LIMIT 2 OFFSET 1;")
		require.NotNil(t, analysis)
		assert.True(t, analysis.ReadOnly)
	})
}

func TestAnalyseQuery_Delete_MultiTable(t *testing.T) {
	t.Parallel()

	t.Run("multi-table delete with schema-qualified target", func(t *testing.T) {
		t.Parallel()
		analysis := analyseQuery(t, "DELETE u FROM mydb.users u WHERE u.active = 0;")
		require.NotNil(t, analysis)
		require.NotEmpty(t, analysis.FromTables)
		assert.Equal(t, "users", analysis.FromTables[0].Name)
	})

	t.Run("multi-table delete with WHERE clause", func(t *testing.T) {
		t.Parallel()
		analysis := analyseQuery(t, "DELETE u FROM users u INNER JOIN banned b ON u.id = b.user_id WHERE b.reason = 'spam';")
		require.NotNil(t, analysis)
		require.NotEmpty(t, analysis.FromTables)
	})

	t.Run("multi-table delete with multiple target tables", func(t *testing.T) {
		t.Parallel()
		analysis := analyseQuery(t, "DELETE u, p FROM users u INNER JOIN profiles p ON u.id = p.user_id WHERE u.active = 0;")
		require.NotNil(t, analysis)
		require.NotEmpty(t, analysis.FromTables)
	})
}

func TestAnalyseQuery_InsertReturning(t *testing.T) {
	t.Parallel()

	t.Run("insert with RETURNING clause", func(t *testing.T) {
		t.Parallel()
		engine := New(WithReturning(ast.DataChangeInsert | ast.DataChangeUpdate | ast.DataChangeDelete))
		statements, err := engine.ParseStatements(t.Context(), "INSERT INTO users (name) VALUES (?) RETURNING id, name;")
		require.NoError(t, err)
		require.NotEmpty(t, statements)
		analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])
		require.NoError(t, err)
		require.NotNil(t, analysis)
		assert.Equal(t, "users", analysis.InsertTable)
	})
}

func TestAnalyseQuery_UpdateMultiColumnSet(t *testing.T) {
	t.Parallel()

	t.Run("update with tuple-style SET clause", func(t *testing.T) {
		t.Parallel()
		analysis := analyseQuery(t, "UPDATE users SET (name, email) = (?, ?) WHERE id = 1;")
		require.NotNil(t, analysis)
		require.NotEmpty(t, analysis.FromTables)
		assert.Equal(t, "users", analysis.FromTables[0].Name)
		require.Len(t, analysis.ParameterReferences, 2)
	})
}

func TestAnalyseQuery_ForUpdateVariants(t *testing.T) {
	t.Parallel()

	t.Run("FOR UPDATE SKIP LOCKED", func(t *testing.T) {
		t.Parallel()
		analysis := analyseQuery(t, "SELECT id FROM users FOR UPDATE SKIP LOCKED;")
		assert.False(t, analysis.ReadOnly)
	})

	t.Run("FOR UPDATE NOWAIT", func(t *testing.T) {
		t.Parallel()
		analysis := analyseQuery(t, "SELECT id FROM users FOR UPDATE NOWAIT;")
		assert.False(t, analysis.ReadOnly)
	})

	t.Run("FOR SHARE also marks non-read-only", func(t *testing.T) {
		t.Parallel()
		analysis := analyseQuery(t, "SELECT id FROM users FOR SHARE;")
		assert.False(t, analysis.ReadOnly, "FOR SHARE should mark query as non-read-only")
	})
}

func TestAnalyseQuery_WindowFrameVariants(t *testing.T) {
	t.Parallel()

	t.Run("ROWS UNBOUNDED PRECEDING", func(t *testing.T) {
		t.Parallel()
		analysis := analyseQuery(t, "SELECT id, SUM(amount) OVER (ORDER BY id ROWS UNBOUNDED PRECEDING) AS running FROM orders;")
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("ROWS CURRENT ROW", func(t *testing.T) {
		t.Parallel()
		analysis := analyseQuery(t, "SELECT id, SUM(amount) OVER (ORDER BY id ROWS CURRENT ROW) AS running FROM orders;")
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("window with ORDER BY DESC NULLS LAST", func(t *testing.T) {
		t.Parallel()
		analysis := analyseQuery(t, "SELECT id, ROW_NUMBER() OVER (ORDER BY created_at DESC NULLS LAST) AS rn FROM users;")
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("window with multiple ORDER BY columns", func(t *testing.T) {
		t.Parallel()
		analysis := analyseQuery(t, "SELECT id, RANK() OVER (ORDER BY dept ASC, salary DESC) AS ranked FROM employees;")
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("window with multiple PARTITION BY columns", func(t *testing.T) {
		t.Parallel()
		analysis := analyseQuery(t, "SELECT id, ROW_NUMBER() OVER (PARTITION BY dept, team ORDER BY id) AS rn FROM employees;")
		assert.True(t, analysis.ReadOnly)
	})
}

func TestAnalyseQuery_GroupByVariants(t *testing.T) {
	t.Parallel()

	t.Run("GROUP BY with positional reference", func(t *testing.T) {
		t.Parallel()
		analysis := analyseQuery(t, "SELECT status, COUNT(*) FROM users GROUP BY 1;")
		assert.True(t, analysis.ReadOnly)

		require.NotEmpty(t, analysis.OutputColumns)
	})

	t.Run("GROUP BY with function expression", func(t *testing.T) {
		t.Parallel()
		analysis := analyseQuery(t, "SELECT YEAR(created_at), COUNT(*) FROM users GROUP BY YEAR(created_at);")
		assert.True(t, analysis.ReadOnly)
		assert.Empty(t, analysis.GroupByColumns, "only plain grouped columns are recorded")
	})
}

func TestAnalyseQuery_ConvertUsing(t *testing.T) {
	t.Parallel()

	t.Run("CONVERT with USING charset", func(t *testing.T) {
		t.Parallel()
		analysis := analyseQuery(t, "SELECT CONVERT(name USING utf8) FROM users;")
		assert.True(t, analysis.ReadOnly)
		require.Len(t, analysis.OutputColumns, 1)
	})
}

func TestAnalyseQuery_JSONArrayElementPath(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, "SELECT data->'$.items[0]' AS first_item FROM documents;")
	assert.True(t, analysis.ReadOnly)
	require.Len(t, analysis.OutputColumns, 1)
}

func TestAnalyseQuery_HasWhereClause(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sql  string
		want bool
	}{
		{name: "SELECT with WHERE", sql: "SELECT id FROM users WHERE id = ?", want: true},
		{name: "SELECT without WHERE", sql: "SELECT id FROM users", want: false},
		{name: "UPDATE with WHERE", sql: "UPDATE users SET name = ? WHERE id = ?", want: true},
		{name: "UPDATE without WHERE", sql: "UPDATE users SET name = ?", want: false},
		{name: "DELETE simple with WHERE", sql: "DELETE FROM users WHERE id = ?", want: true},
		{name: "DELETE simple without WHERE", sql: "DELETE FROM users", want: false},
		{name: "DELETE multi-table with WHERE", sql: "DELETE u, p FROM users u JOIN posts p ON p.user_id = u.id WHERE u.id = ?", want: true},
		{name: "DELETE multi-table without WHERE", sql: "DELETE u, p FROM users u JOIN posts p ON p.user_id = u.id", want: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			analysis := analyseQuery(t, testCase.sql)
			require.NotNil(t, analysis)
			assert.Equal(t, testCase.want, analysis.HasWhereClause)
		})
	}
}

func TestAnalyseQuery_InsertPartitionClause(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sql  string
	}{
		{
			name: "single partition",
			sql:  "INSERT INTO users PARTITION (p0) (name, email) VALUES (?, ?)",
		},
		{
			name: "multiple partitions",
			sql:  "INSERT INTO users PARTITION (p0, p1) (name, email) VALUES (?, ?)",
		},
		{
			name: "replace with partition",
			sql:  "REPLACE INTO users PARTITION (p0) (name, email) VALUES (?, ?)",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, testCase.sql)

			assert.Equal(t, "users", analysis.InsertTable)
			assert.Equal(t, []string{"name", "email"}, analysis.InsertColumns,
				"a PARTITION clause must not strand the parser before the column list")
			require.Len(t, analysis.ParameterReferences, 2,
				"a PARTITION clause must not drop the VALUES parameters")
		})
	}
}

func TestAnalyseQuery_InsertPartitionClauseKeepsReturning(t *testing.T) {
	t.Parallel()

	engine := New(WithReturning(ast.DataChangeInsert | ast.DataChangeUpdate | ast.DataChangeDelete))
	statements, err := engine.ParseStatements(t.Context(),
		"INSERT INTO users PARTITION (p0) (name, email) VALUES (?, ?) RETURNING id")
	require.NoError(t, err)
	require.NotEmpty(t, statements)

	analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])
	require.NoError(t, err)
	require.NotNil(t, analysis)

	assert.True(t, analysis.HasReturning,
		"a PARTITION clause must not swallow a trailing RETURNING")
	require.Len(t, analysis.OutputColumns, 1)
	assert.Equal(t, "id", analysis.OutputColumns[0].Name)
	require.Len(t, analysis.ParameterReferences, 2)
}

func TestAnalyseQuery_RecordsTheReturningClauseWhateverTheSupport(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
	}{
		{name: "insert", sql: "-- sgl.query(CreateUser, one)\nINSERT INTO users (name) VALUES (?) RETURNING id"},
		{name: "update", sql: "UPDATE users\nSET name = ?\nWHERE id = ?\n  returning id, name"},
		{name: "delete", sql: "/* gone */ DELETE FROM users WHERE id = ? Returning id"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, testCase.sql)

			require.True(t, analysis.HasReturning)
			require.Equal(t, strings.Index(strings.ToUpper(testCase.sql), "RETURNING"), analysis.ReturningLocation)
		})
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
		{name: "an EXISTS subquery", sql: "SELECT id FROM users u WHERE EXISTS (SELECT 1 FROM posts WHERE user_id = :id) AND u.id = :id"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, testCase.sql)

			numbers := map[int]bool{}
			for _, reference := range analysis.ParameterReferences {
				numbers[reference.Number] = true
			}
			assert.Equal(t, map[int]bool{1: true}, numbers)
		})
	}
}

func TestAnalyseQuery_NamesStayDistinctAcrossANestedQuery(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, "SELECT id FROM users WHERE id = :id AND id IN (SELECT user_id FROM posts WHERE score > :score) AND name = :id")

	numbers := make([]int, 0, len(analysis.ParameterReferences))
	for _, reference := range analysis.ParameterReferences {
		numbers = append(numbers, reference.Number)
	}
	assert.ElementsMatch(t, []int{1, 2, 1}, numbers)
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
			name:     "a named placeholder written twice is one parameter",
			sql:      "SELECT :bound > id AS matched FROM items WHERE id < :bound AND name = :label",
			expected: []string{"select list 1", "clauses 1", "clauses 2"},
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
		{
			name:     "a HAVING and an ORDER BY number their placeholders",
			sql:      "SELECT kind, count(*) AS total FROM items GROUP BY kind HAVING count(*) > ? ORDER BY field(kind, ?)",
			expected: []string{"clauses 1", "clauses 2"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, testCase.sql)
			placed := placedParameterNumbers(analysis, "")
			require.Equal(t, testCase.expected, placed)
			registered := make(map[int]bool, len(analysis.ParameterReferences))
			for _, reference := range analysis.ParameterReferences {
				registered[reference.Number] = true
			}
			for _, each := range placed {
				fields := strings.Fields(each)
				require.Contains(t, registered, mustAtoi(t, fields[len(fields)-1]), "%s names no registered parameter", each)
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

func mustAtoi(t *testing.T, text string) int {
	t.Helper()

	number, err := strconv.Atoi(text)
	require.NoError(t, err)
	return number
}

func TestFunctionsWrittenWithoutParenthesesTakeAFractionalSecondsPrecision(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		function string
	}{
		{name: "UTC_TIMESTAMP with a precision", sql: "SELECT UTC_TIMESTAMP(6) AS moment FROM events", function: "utc_timestamp"},
		{name: "CURRENT_TIMESTAMP with a precision", sql: "SELECT CURRENT_TIMESTAMP(3) AS moment FROM events", function: "current_timestamp"},
		{name: "CURRENT_TIME with a precision", sql: "SELECT CURRENT_TIME(3) AS moment FROM events", function: "current_time"},
		{name: "LOCALTIMESTAMP with a precision", sql: "SELECT LOCALTIMESTAMP(2) AS moment FROM events", function: "localtimestamp"},
		{name: "UTC_TIME with empty parentheses", sql: "SELECT UTC_TIME() AS moment FROM events", function: "utc_time"},
		{name: "CURRENT_DATE without parentheses", sql: "SELECT CURRENT_DATE AS moment FROM events", function: "current_date"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, testCase.sql)
			require.Len(t, analysis.OutputColumns, 1)
			call, isCall := analysis.OutputColumns[0].Expression.(*ast.FunctionCallExpression)
			require.True(t, isCall)
			require.Equal(t, testCase.function, call.FunctionName)
			require.Empty(t, call.Arguments)
		})
	}
}

func TestNestedQueriesKeepTheirOwnParts(t *testing.T) {
	t.Parallel()

	t.Run("a subquery in a condition carries its query", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, "SELECT id FROM users WHERE (SELECT max(p.id) FROM posts p) = ?")

		comparison, isComparison := analysis.ClauseExpressions[0].Expression.(*ast.ComparisonExpression)
		require.True(t, isComparison)
		subquery, isSubquery := comparison.Left.(*ast.ScalarSubqueryExpression)
		require.True(t, isSubquery)
		require.Same(t, analysis.PredicateSubqueries[0], subquery.InnerQuery)
		require.Nil(t, analysis.ParameterReferences[0].ColumnReference)
	})

	t.Run("each arm of a set operation keeps its own derived tables and parameters", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, "SELECT id FROM users WHERE id = ? UNION ALL SELECT d.id FROM (SELECT id FROM posts WHERE id = ?) d")

		require.Empty(t, analysis.RawDerivedTables)
		require.Len(t, analysis.ParameterReferences, 2)
		arm := analysis.CompoundBranches[0].Query
		require.Len(t, arm.RawDerivedTables, 1)
		require.Len(t, arm.ParameterReferences, 1)
		require.Equal(t, 2, arm.ParameterReferences[0].Number)
	})

	t.Run("a derived table records its column list", func(t *testing.T) {
		t.Parallel()

		analysis := analyseQuery(t, "SELECT t.n, t.label FROM (VALUES ROW(1, 'a'), ROW(2, 'b')) AS t (n, label)")

		require.Len(t, analysis.RawDerivedTables, 1)
		require.Equal(t, []string{"n", "label"}, analysis.RawDerivedTables[0].ColumnNames)
	})
}

func TestAStringWrittenAfterASelectItemNamesIt(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, `SELECT 1 'one', id "ident", name AS 'label', price total FROM items`)

	require.Equal(t, []string{"one", "ident", "label", "total"}, outputColumnNames(analysis))
}

func TestSetOperationArmsMayBeValuesListsOrQueriesInParentheses(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		sql        string
		columns    []string
		branches   int
		parameters [][]int
	}{
		{name: "a VALUES list before a SELECT", sql: "VALUES ROW(1, 'a') UNION ALL SELECT id, name FROM items WHERE id = ?",
			columns: []string{"column1", "column2"}, branches: 1, parameters: [][]int{{1}}},
		{name: "a VALUES list after a SELECT", sql: "SELECT id FROM items WHERE id = ? UNION VALUES (2), (?)",
			columns: []string{"id"}, branches: 1, parameters: [][]int{{2}}},
		{name: "two queries in parentheses with their own limits", sql: "(SELECT id FROM items WHERE id > ? ORDER BY id LIMIT 1) UNION ALL (SELECT id FROM orders WHERE id < ? ORDER BY id DESC LIMIT 1) ORDER BY id",
			columns: []string{"id"}, branches: 1, parameters: [][]int{{2}}},
		{name: "a set operation in parentheses as a later arm", sql: "SELECT id FROM items UNION (SELECT id FROM orders UNION SELECT id FROM refunds WHERE id = ?)",
			columns: []string{"id"}, branches: 2, parameters: [][]int{nil, {1}}},
		{name: "a set operation in parentheses as the first arm", sql: "((SELECT id FROM items LIMIT 1) UNION (SELECT id FROM orders)) ORDER BY 1 DESC LIMIT 2",
			columns: []string{"id"}, branches: 1, parameters: [][]int{nil}},
		{name: "queries in parentheses ordered without a limit", sql: "(SELECT id FROM items ORDER BY id) UNION ALL (SELECT id FROM orders WHERE id = ? ORDER BY id)",
			columns: []string{"id"}, branches: 1, parameters: [][]int{{1}}},
		{name: "a query in parentheses alone", sql: "((SELECT id, name FROM items WHERE id = ?))",
			columns: []string{"id", "name"}, branches: 0, parameters: nil},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, testCase.sql)

			require.Equal(t, testCase.columns, outputColumnNames(analysis))
			require.Len(t, analysis.CompoundBranches, testCase.branches)
			for index, branch := range analysis.CompoundBranches {
				numbers := make([]int, 0, len(branch.Query.ParameterReferences))
				for _, reference := range branch.Query.ParameterReferences {
					numbers = append(numbers, reference.Number)
				}
				require.ElementsMatch(t, testCase.parameters[index], numbers, "branch %d", index)
			}
		})
	}
}

func TestSubqueriesMayBeSetOperationsOverQueriesInParentheses(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
	}{
		{name: "a derived table", sql: "SELECT x.id FROM ((SELECT id FROM items) UNION ALL (SELECT id FROM orders)) AS x WHERE x.id > ?"},
		{name: "a common table expression", sql: "WITH c AS ((SELECT id FROM items LIMIT 1) UNION (SELECT 9)) SELECT id FROM c"},
		{name: "an IN list", sql: "SELECT id FROM items WHERE id IN ((SELECT id FROM orders) UNION (SELECT id FROM refunds))"},
		{name: "a VALUES list as a common table expression", sql: "WITH RECURSIVE r (n) AS (VALUES ROW(1) UNION ALL SELECT n + 1 FROM r WHERE n < 3) SELECT n FROM r"},
		{name: "an expression that starts with a subquery", sql: "SELECT ((SELECT max(id) FROM orders) + 1) AS next_id FROM items"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, testCase.sql)

			require.NotEmpty(t, analysis.OutputColumns)
		})
	}
}

func TestAFullTextSearchPlaceholderIsText(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
		cast bool
	}{
		{name: "natural language", sql: "SELECT id FROM t WHERE MATCH (title, body) AGAINST (?)", cast: true},
		{name: "boolean mode", sql: "SELECT id FROM t WHERE MATCH (title) AGAINST (? IN BOOLEAN MODE) > 0", cast: true},
		{name: "an expression over the placeholder", sql: "SELECT id FROM t WHERE MATCH (title) AGAINST (concat(?, '*') IN BOOLEAN MODE)", cast: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, testCase.sql)

			require.Len(t, analysis.ParameterReferences, 1)
			castType := analysis.ParameterReferences[0].CastType
			if !testCase.cast {
				require.Nil(t, castType)
				return
			}
			require.NotNil(t, castType)
			require.Equal(t, sqlmodel.TypeCategoryText, castType.Category)
		})
	}
}

func TestAnIntervalAmountPlaceholderIsANumberOfItsUnit(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		wantCast bool
	}{
		{name: "a simple unit", sql: "SELECT id FROM t WHERE created_at > NOW() - INTERVAL ? HOUR", wantCast: true},
		{name: "in DATE_ADD", sql: "SELECT DATE_ADD(created_at, INTERVAL ? DAY) AS later FROM t WHERE id = 1", wantCast: true},
		{name: "a compound unit", sql: "SELECT id FROM t WHERE created_at > NOW() - INTERVAL ? DAY_HOUR", wantCast: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, testCase.sql)

			require.Len(t, analysis.ParameterReferences, 1)
			castType := analysis.ParameterReferences[0].CastType
			if !testCase.wantCast {
				require.True(t, castType == nil || castType.Category != sqlmodel.TypeCategoryInteger)
				return
			}
			require.NotNil(t, castType)
			require.Equal(t, sqlmodel.TypeCategoryInteger, castType.Category)
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
		{name: "a plain INSERT handles no conflict", sql: "INSERT INTO t (a) VALUES (?)", want: ast.ConflictActionNone},
		{name: "INSERT IGNORE skips the row", sql: "INSERT IGNORE INTO t (a) VALUES (?)", want: ast.ConflictActionDoNothing},
		{name: "IGNORE among other modifiers skips the row", sql: "INSERT LOW_PRIORITY IGNORE INTO t (a) VALUES (?)",
			want: ast.ConflictActionDoNothing},
		{name: "ON DUPLICATE KEY UPDATE updates the row", sql: "INSERT INTO t (a) VALUES (?) ON DUPLICATE KEY UPDATE a = VALUES(a)",
			want: ast.ConflictActionDoUpdate},
		{name: "ON DUPLICATE KEY UPDATE wins over IGNORE", sql: "INSERT IGNORE INTO t (a) VALUES (?) ON DUPLICATE KEY UPDATE a = 1",
			want: ast.ConflictActionDoUpdate},
		{name: "REPLACE writes the row in place of the existing one", sql: "REPLACE INTO t (a) VALUES (?)",
			want: ast.ConflictActionDoUpdate},
		{name: "an INSERT of a query handles no conflict", sql: "INSERT INTO t (a) SELECT b FROM u", want: ast.ConflictActionNone},
		{name: "a SELECT handles no conflict", sql: "SELECT a FROM t", want: ast.ConflictActionNone},
		{name: "UPDATE IGNORE is no INSERT conflict", sql: "UPDATE IGNORE t SET a = 1", want: ast.ConflictActionNone},
		{name: "a DELETE handles no conflict", sql: "DELETE FROM t WHERE a = ?", want: ast.ConflictActionNone},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, testCase.sql)

			assert.Equal(t, testCase.want, analysis.ConflictAction)
		})
	}
}

func TestAnInsertReadsAParenthesisedQueryAsItsRows(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		sql            string
		wantParameters []string
		wantArms       int
	}{
		{name: "a parenthesised select", sql: "INSERT INTO t (a) (SELECT ? FROM u)",
			wantParameters: []string{"1 assignment t.a -"}, wantArms: 0},
		{name: "a select in two pairs of parentheses", sql: "INSERT INTO t (a) ((SELECT ? FROM u))",
			wantParameters: []string{"1 assignment t.a -"}, wantArms: 0},
		{name: "a parenthesised union", sql: "INSERT INTO t (a, b) (SELECT ?, ? FROM u UNION SELECT ?, ? FROM v)",
			wantParameters: []string{"1 assignment t.a -", "2 assignment t.b -", "3 assignment t.a -", "4 assignment t.b -"},
			wantArms:       1},
		{name: "a condition of the parenthesised select", sql: "INSERT INTO t (a) (SELECT b FROM u WHERE c = ?)",
			wantParameters: []string{"1 comparison .c -"}, wantArms: 0},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, testCase.sql)

			require.NotNil(t, analysis.InsertSelect)
			assert.Len(t, analysis.InsertSelect.CompoundBranches, testCase.wantArms)
			assert.Equal(t, testCase.wantParameters, describeDeclaredParameters(analysis.ParameterReferences))
		})
	}
}
