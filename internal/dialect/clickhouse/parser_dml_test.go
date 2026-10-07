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

package clickhouse

import (
	"cmp"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func analyse(t *testing.T, sql string) *ast.RawQueryAnalysis {
	t.Helper()
	engine := New()
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.Len(t, statements, 1)
	analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])
	require.NoError(t, err)
	require.NotNil(t, analysis)
	return analysis
}

func analyseError(t *testing.T, sql string) error {
	t.Helper()
	engine := New()
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.Len(t, statements, 1)
	analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])
	if err == nil {
		assert.NotNil(t, analysis)
	}
	return err
}

func TestAnalyseQuery_AlterAsyncMutationSetsAsyncBodyMarker(t *testing.T) {
	t.Parallel()

	deleteAnalysis := analyse(t, "ALTER TABLE events DELETE WHERE ts < {cutoff:DateTime}")
	assert.True(t, deleteAnalysis.AsyncMutation, "ALTER ... DELETE must be marked as an asynchronous mutation")
	assert.NotEmpty(t, detailOf(deleteAnalysis).asyncBody)

	updateAnalysis := analyse(t, "ALTER TABLE events UPDATE payload = {p:String} WHERE id = {id:UInt64}")
	assert.True(t, updateAnalysis.AsyncMutation, "ALTER ... UPDATE must be marked as an asynchronous mutation")

	assert.Len(t, updateAnalysis.ParameterReferences, 2)

	addColumn := analyse(t, "ALTER TABLE events ON CLUSTER c ADD COLUMN flag UInt8")
	assert.False(t, addColumn.AsyncMutation, "ALTER ... ADD COLUMN must not be marked async")
}

func TestDML_SelectBasic(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT id, name FROM users")
	require.Len(t, analysis.OutputColumns, 2)
	assert.Equal(t, "id", analysis.OutputColumns[0].ColumnName)
	assert.Equal(t, "name", analysis.OutputColumns[1].ColumnName)
	require.Len(t, analysis.FromTables, 1)
	assert.Equal(t, "users", analysis.FromTables[0].Name)
}

func TestDML_SelectStarExpansion(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT * FROM events")
	require.Len(t, analysis.OutputColumns, 1)
	assert.True(t, analysis.OutputColumns[0].IsStar)
}

func TestDML_SelectQualifiedStar(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT users.* FROM users")
	require.Len(t, analysis.OutputColumns, 1)
	assert.True(t, analysis.OutputColumns[0].IsStar)
	assert.Equal(t, "users", analysis.OutputColumns[0].TableAlias)
}

func TestDML_SelectWithSchemaQualifiedTable(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT id FROM analytics.events")
	require.Len(t, analysis.FromTables, 1)
	assert.Equal(t, "analytics", analysis.FromTables[0].Schema)
	assert.Equal(t, "events", analysis.FromTables[0].Name)
}

func TestDML_SelectWithAlias(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT e.id FROM events AS e")
	require.Len(t, analysis.FromTables, 1)
	assert.Equal(t, "events", analysis.FromTables[0].Name)
	assert.Equal(t, "e", analysis.FromTables[0].Alias)
}

func TestDML_SelectInnerJoin(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT u.id FROM users u INNER JOIN accounts a ON a.user_id = u.id")
	require.Len(t, analysis.FromTables, 2)
	assert.Equal(t, "users", analysis.FromTables[0].Name)
	assert.Equal(t, "accounts", analysis.FromTables[1].Name)
}

func TestDML_SelectAsofJoinRecordsKind(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT q.symbol FROM quotes q ASOF JOIN trades t ON q.symbol = t.symbol AND q.ts <= t.ts")
	require.Len(t, analysis.JoinClauses, 1)
	assert.Equal(t, joinAsof, analysis.JoinClauses[0].Kind)
}

func TestDML_SelectSemiJoinRecordsKind(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT u.id FROM users u SEMI JOIN accounts a ON a.user_id = u.id")
	require.Len(t, analysis.JoinClauses, 1)
	assert.Equal(t, joinSemi, analysis.JoinClauses[0].Kind)
}

func TestDML_SelectAntiJoinRecordsKind(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT u.id FROM users u ANTI JOIN accounts a ON a.user_id = u.id")
	require.Len(t, analysis.JoinClauses, 1)
	assert.Equal(t, joinAnti, analysis.JoinClauses[0].Kind)
}

func TestDML_SelectLeftSemiJoinRecordsKind(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT u.id FROM users u LEFT SEMI JOIN accounts a ON a.user_id = u.id")
	require.Len(t, analysis.JoinClauses, 1)
	assert.Equal(t, joinLeftSemi, analysis.JoinClauses[0].Kind)
}

func TestDML_SelectLeftAntiJoinRecordsKind(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT u.id FROM users u LEFT ANTI JOIN accounts a ON a.user_id = u.id")
	require.Len(t, analysis.JoinClauses, 1)
	assert.Equal(t, joinLeftAnti, analysis.JoinClauses[0].Kind)
}

func TestDML_SelectRightSemiJoinRecordsKind(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT u.id FROM users u RIGHT SEMI JOIN accounts a ON a.user_id = u.id")
	require.Len(t, analysis.JoinClauses, 1)
	assert.Equal(t, joinRightSemi, analysis.JoinClauses[0].Kind)
}

func TestDML_SelectRightAntiJoinRecordsKind(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT u.id FROM users u RIGHT ANTI JOIN accounts a ON a.user_id = u.id")
	require.Len(t, analysis.JoinClauses, 1)
	assert.Equal(t, joinRightAnti, analysis.JoinClauses[0].Kind)
}

func TestDML_SelectInfixProjectionCorrupt(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT a + b AS c, d FROM t")
	require.Len(t, analysis.OutputColumns, 2)
	require.Len(t, analysis.FromTables, 1)
	assert.Equal(t, "t", analysis.FromTables[0].Name)
}

func TestDML_SelectWindowClauseAfterFrom(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT id, row_number() OVER w FROM users WINDOW w AS (ORDER BY id)")
	require.Len(t, analysis.FromTables, 1)
	assert.Equal(t, "users", analysis.FromTables[0].Name)
}

func TestDML_SelectInSubqueryDoesNotMisparse(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT id FROM users WHERE id IN (SELECT user_id FROM admins)")
	require.Len(t, analysis.FromTables, 1)
	assert.Equal(t, "users", analysis.FromTables[0].Name)
}

func TestDML_SelectInWrappedSubqueryDoesNotMisparse(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT id FROM users WHERE id IN ((SELECT user_id FROM admins))")
	require.Len(t, analysis.FromTables, 1)
	assert.Equal(t, "users", analysis.FromTables[0].Name)
}

func TestDML_SelectPrewhereSetsHasWhereClause(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT id FROM users PREWHERE active")
	assert.True(t, analysis.HasWhereClause, "PREWHERE-only queries should still set HasWhereClause for count rewriting")
}

func TestDML_SelectRecursiveCTEFlag(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "WITH RECURSIVE r AS (SELECT 1 AS n) SELECT * FROM r")
	require.Len(t, analysis.CTEDefinitions, 1)
	assert.True(t, analysis.CTEDefinitions[0].IsRecursive)
}

func TestDML_SelectLimitCommaForm(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT id FROM users LIMIT 10, 5")
	require.Len(t, analysis.FromTables, 1)
	assert.Equal(t, "users", analysis.FromTables[0].Name)
}

func TestDML_SelectWhereWithCurlyParameter(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT id FROM users WHERE id = {uid:UInt32}")
	require.Len(t, analysis.ParameterReferences, 1)
	reference := analysis.ParameterReferences[0]
	assert.Equal(t, "uid", reference.Name)
	assert.Equal(t, 1, reference.Number)
	require.NotNil(t, reference.CastType)
	assert.Equal(t, sqlmodel.TypeCategoryInteger, reference.CastType.Category)
	assert.True(t, analysis.HasWhereClause)
}

func TestDML_UnrecognisedParameterTypeTagRecordsDiagnostic(t *testing.T) {
	t.Parallel()

	engine := New()
	statements, parseErr := engine.ParseStatements(t.Context(), "SELECT id FROM users WHERE id = {uid:Strign}")
	require.NoError(t, parseErr)
	require.Len(t, statements, 1)

	analysis, analyseErr := engine.AnalyseQuery(t.Context(), nil, statements[0])
	require.Error(t, analyseErr, "an unrecognised parameter type tag must no longer be silently accepted")
	assert.Contains(t, analyseErr.Error(), "unrecognised type tag")

	require.NotNil(t, analysis)
	require.Len(t, analysis.ParameterReferences, 1)
	reference := analysis.ParameterReferences[0]
	assert.Equal(t, "uid", reference.Name)
	require.NotNil(t, reference.CastType, "the binding is retained with the Unknown cast, not dropped")
	assert.Equal(t, sqlmodel.TypeCategoryUnknown, reference.CastType.Category)
}

func TestDML_FunctionArgumentParameterIsSelfTyped(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT toString({uid:UInt32}) FROM users")
	require.Len(t, analysis.ParameterReferences, 1)
	reference := analysis.ParameterReferences[0]
	assert.Equal(t, "uid", reference.Name)

	require.NotNil(t, reference.CastType, "function-arg placeholder must carry its explicit type tag")
	assert.Equal(t, sqlmodel.TypeCategoryInteger, reference.CastType.Category)

	assert.Empty(t, reference.EnclosingFunctionName, "clickhouse does not record the enclosing function name")
	assert.Zero(t, reference.ArgumentOrdinal, "clickhouse does not record an argument ordinal")
}

func TestTableFunctionArgumentPlaceholdersAreRegisteredAsFunctionArguments(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		sql        string
		function   string
		parameters []string
		contexts   []sqlmodel.ParameterContext
	}{
		{
			name:       "an unqualified table function",
			sql:        "SELECT number FROM numbers({n:UInt64})",
			function:   "numbers",
			parameters: []string{"n"},
			contexts:   []sqlmodel.ParameterContext{sqlmodel.ParameterContextFunctionArgument},
		},
		{
			name:       "a qualified table function",
			sql:        "SELECT * FROM db.f({a:String}, {b:UInt8}) AS rows",
			function:   "db.f",
			parameters: []string{"a", "b"},
			contexts:   []sqlmodel.ParameterContext{sqlmodel.ParameterContextFunctionArgument, sqlmodel.ParameterContextFunctionArgument},
		},
		{
			name:       "a placeholder nested in an argument",
			sql:        "SELECT number FROM numbers(plus({n:UInt64}, 1))",
			function:   "numbers",
			parameters: []string{"n"},
			contexts:   []sqlmodel.ParameterContext{sqlmodel.ParameterContextFunctionArgument},
		},
		{
			name:       "a placeholder used again in WHERE",
			sql:        "SELECT number FROM numbers({n:UInt64}) WHERE number < {n:UInt64}",
			function:   "numbers",
			parameters: []string{"n", "n"},
			contexts:   []sqlmodel.ParameterContext{sqlmodel.ParameterContextFunctionArgument, sqlmodel.ParameterContextComparison},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyse(t, testCase.sql)
			require.Len(t, analysis.RawTableValuedFunctions, 1)
			assert.Equal(t, testCase.function, analysis.RawTableValuedFunctions[0].FunctionName)
			require.Len(t, analysis.ParameterReferences, len(testCase.parameters))
			for index, reference := range analysis.ParameterReferences {
				assert.Equal(t, testCase.parameters[index], reference.Name)
				assert.Equal(t, testCase.contexts[index], reference.Context)
				require.NotNil(t, reference.CastType, "the placeholder is typed by its own tag")
			}
			assert.Len(t, analysis.Spans.Placeholders, len(testCase.parameters), "every placeholder the spans list has a parameter")
		})
	}
}

func TestMalformedStatementsAreLocatedSyntaxErrors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		token    string
		position int
	}{
		{name: "an unclosed table function", sql: "SELECT * FROM numbers(10", token: "(", position: 21},
		{name: "an unclosed qualified table function", sql: "SELECT * FROM db.f(1, 2", token: "(", position: 18},
		{name: "an unclosed DISTINCT ON list", sql: "SELECT DISTINCT ON (a, b FROM t", token: "(", position: 19},
		{name: "an unclosed call", sql: "SELECT f(", token: "(", position: 8},
		{name: "an unclosed tuple", sql: "SELECT (1, 2", token: "(", position: 7},
		{name: "an unclosed array", sql: "SELECT [1, 2", token: "[", position: 7},
		{name: "an empty first select item", sql: "SELECT , 1", token: ",", position: 7},
		{name: "an empty item after a comma", sql: "SELECT 1,,", token: ",", position: 9},
		{name: "a comma ending the statement", sql: "SELECT 1,", token: "", position: 9},
		{name: "an AS with no alias", sql: "SELECT x AS", token: "", position: 11},
		{name: "a table alias AS with no alias", sql: "SELECT x FROM t AS", token: "", position: 18},
		{name: "an empty select item in a scalar subquery", sql: "SELECT (SELECT , 1)", token: ",", position: 15},
		{name: "a word left over in a scalar subquery", sql: "SELECT (SELECT 1 FROM t AS a b)", token: "b", position: 29},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := analyseError(t, testCase.sql)
			var parseError *dialectkit.ParseError
			require.ErrorAs(t, err, &parseError)
			assert.Equal(t, testCase.token, parseError.Token)
			assert.Equal(t, testCase.position, parseError.Position)
			assert.ErrorIs(t, err, fault.ErrParse)
		})
	}
}

func TestDML_ParameterReuseDedupes(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT id FROM users WHERE id = {uid:UInt32} OR parent_id = {uid:UInt32}")

	require.Len(t, analysis.ParameterReferences, 2)
	assert.Equal(t, analysis.ParameterReferences[0].Number, analysis.ParameterReferences[1].Number)
}

func TestDML_GroupByCapturesColumns(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT count(*), country FROM users GROUP BY country")
	require.Len(t, analysis.GroupByColumns, 1)
	assert.Equal(t, "country", analysis.GroupByColumns[0].ColumnName)
}

func TestDML_GroupByWithRollup(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT country, region, count(*) FROM users GROUP BY country, region WITH ROLLUP")
	require.Len(t, analysis.GroupByColumns, 2)
}

func TestDML_OrderByLimit(t *testing.T) {
	t.Parallel()

	analyse(t, "SELECT id FROM users ORDER BY created_at DESC LIMIT 10 OFFSET 20")
}

func TestDML_PrewhereAccepted(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT id FROM users PREWHERE active = 1 WHERE id > 0")
	require.Len(t, analysis.FromTables, 1)
	assert.Equal(t, "users", analysis.FromTables[0].Name)
	assert.True(t, analysis.HasWhereClause, "PREWHERE + WHERE should set HasWhereClause")
}

func TestDML_FinalModifierAccepted(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT id FROM users FINAL WHERE id = 1")
	require.Len(t, analysis.FromTables, 1)
	assert.Equal(t, "users", analysis.FromTables[0].Name)
	assert.True(t, analysis.HasWhereClause)
}

func TestDML_SampleAccepted(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT id FROM users SAMPLE 0.1 WHERE id = 1")
	require.Len(t, analysis.FromTables, 1)
	assert.Equal(t, "users", analysis.FromTables[0].Name)
	assert.True(t, analysis.HasWhereClause)
}

func TestDML_ArrayJoinAccepted(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT tag FROM events ARRAY JOIN tags AS tag WHERE id = 1")
	require.Len(t, analysis.FromTables, 1)
	assert.Equal(t, "events", analysis.FromTables[0].Name)
	require.Len(t, analysis.OutputColumns, 1)
	assert.Equal(t, "tag", analysis.OutputColumns[0].Name)
}

func TestDML_LeftArrayJoinAccepted(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT tag FROM events LEFT ARRAY JOIN tags AS tag WHERE id = 1")
	require.Len(t, analysis.FromTables, 1)
	assert.Equal(t, "events", analysis.FromTables[0].Name)
}

func TestDML_UnionAllRecordsCompoundBranches(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT id FROM a UNION ALL SELECT id FROM b")
	require.Len(t, analysis.CompoundBranches, 1)
	assert.Equal(t, ast.CompoundUnionAll, analysis.CompoundBranches[0].Operator)
}

func TestDML_IntersectExcept(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT id FROM a INTERSECT SELECT id FROM b EXCEPT SELECT id FROM c")

	require.Len(t, analysis.CompoundBranches, 2)
	assert.Equal(t, ast.CompoundIntersect, analysis.CompoundBranches[0].Operator)
	assert.Equal(t, ast.CompoundExcept, analysis.CompoundBranches[1].Operator)
	assert.Empty(t, analysis.CompoundBranches[0].Query.CompoundBranches)
	require.Len(t, analysis.CompoundBranches[1].Query.FromTables, 1)
	assert.Equal(t, "c", analysis.CompoundBranches[1].Query.FromTables[0].Name)
}

func TestDML_CTERecordsDefinitions(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "WITH active AS (SELECT id FROM users WHERE active = 1) SELECT id FROM active")
	require.Len(t, analysis.CTEDefinitions, 1)
	assert.Equal(t, "active", analysis.CTEDefinitions[0].Name)
}

func TestDML_DerivedTableSubquery(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT t.id FROM (SELECT id FROM users WHERE active = 1) t")
	require.Len(t, analysis.RawDerivedTables, 1)
	assert.Equal(t, "t", analysis.RawDerivedTables[0].Alias)
}

func TestDML_TableValuedFunctionCall(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT number FROM numbers(100)")
	require.Len(t, analysis.RawTableValuedFunctions, 1)
	assert.Equal(t, "numbers", analysis.RawTableValuedFunctions[0].FunctionName)
}

func TestDML_InsertValues(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "INSERT INTO users (id, name) VALUES (1, 'alice'), (2, 'bob')")
	assert.Equal(t, "users", analysis.InsertTable)
	assert.Equal(t, []string{"id", "name"}, analysis.InsertColumns)
	assert.False(t, analysis.ReadOnly)
}

func TestDML_InsertSelect(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "INSERT INTO dest (id, name) SELECT id, name FROM source WHERE active = 1")
	assert.Equal(t, "dest", analysis.InsertTable)
	assert.Equal(t, []string{"id", "name"}, analysis.InsertColumns)

	require.NotNil(t, analysis.InsertSelect)
	require.NotEmpty(t, analysis.InsertSelect.FromTables)
	assert.Equal(t, "source", analysis.InsertSelect.FromTables[0].Name)
}

func TestDML_InsertSelectWithJoinAndParams(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "INSERT INTO dest (id, name) "+
		"SELECT u.id, o.name FROM users u INNER JOIN orgs o ON o.id = u.org_id "+
		"WHERE u.id > {min_id:UInt32} AND o.active = {active:UInt8}")
	assert.Equal(t, "dest", analysis.InsertTable)

	require.NotNil(t, analysis.InsertSelect)
	require.Len(t, analysis.InsertSelect.FromTables, 2)
	assert.Equal(t, "users", analysis.InsertSelect.FromTables[0].Name)
	assert.Equal(t, "orgs", analysis.InsertSelect.FromTables[1].Name)
	require.Len(t, analysis.InsertSelect.ParameterReferences, 2)
	require.Len(t, analysis.ParameterReferences, 2)
}

func TestDML_InsertFormat(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "INSERT INTO logs (data) FORMAT JSONEachRow")
	assert.Equal(t, "logs", analysis.InsertTable)
}

func TestDML_SchemaQualifiedInsert(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "INSERT INTO analytics.events (id) VALUES (1)")
	assert.Equal(t, "events", analysis.InsertTable)
	require.Len(t, analysis.FromTables, 1)
	assert.Equal(t, "analytics", analysis.FromTables[0].Schema)
}

func TestDML_DeeplyNestedSubquery(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, `
		WITH agg AS (
			SELECT id, count() AS c FROM events GROUP BY id
		)
		SELECT a.id, a.c FROM agg a
		WHERE a.c > {threshold:UInt32}
		ORDER BY a.c DESC
		LIMIT {page_size:UInt32}
	`)
	require.Len(t, analysis.CTEDefinitions, 1)
	assert.Equal(t, "agg", analysis.CTEDefinitions[0].Name)
	require.GreaterOrEqual(t, len(analysis.ParameterReferences), 2, "should capture both threshold and page_size parameters")
	parameterNames := map[string]bool{}
	for _, reference := range analysis.ParameterReferences {
		parameterNames[reference.Name] = true
	}
	assert.True(t, parameterNames["threshold"], "threshold parameter must be captured")
	assert.True(t, parameterNames["page_size"], "page_size parameter must be captured")
}

func TestDML_GroupByAllRecordsModifier(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT region, count(*) FROM users GROUP BY ALL")
	require.NotNil(t, analysis.Detail)
	assert.True(t, detailOf(analysis).groupByAll)
	assert.Empty(t, analysis.GroupByColumns, "GROUP BY ALL should not populate the column list")
}

func TestDML_GroupByGroupingSets(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT country, city, sum(val) FROM sales GROUP BY GROUPING SETS ((country, city), (country), ())")
	require.NotNil(t, analysis.Detail)
	captured := detailOf(analysis).groupingSets
	require.NotEmpty(t, captured, "GROUPING SETS body should be captured")
	assert.Contains(t, captured, "country")
	assert.Contains(t, captured, "city")
}

func TestDML_QualifyClauseRecorded(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT id, row_number() OVER (PARTITION BY user_id ORDER BY ts) AS rn FROM events QUALIFY rn = 1")
	require.NotNil(t, analysis.Detail)
	assert.True(t, detailOf(analysis).qualify)
	require.Len(t, analysis.FromTables, 1)
	assert.Equal(t, "events", analysis.FromTables[0].Name)
}

func TestDML_AllJoinRecordsKind(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT u.id FROM users u ALL INNER JOIN accounts a ON a.user_id = u.id")
	require.Len(t, analysis.FromTables, 2)
	assert.Equal(t, "accounts", analysis.FromTables[1].Name)
}

func TestDML_PlainAnyJoinResolvesAny(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT u.id FROM users u ANY JOIN accounts a ON a.user_id = u.id")
	require.Len(t, analysis.JoinClauses, 1)
	assert.Equal(t, joinAny, analysis.JoinClauses[0].Kind)
}

func TestDML_PlainGlobalJoinResolvesGlobal(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT u.id FROM users u GLOBAL JOIN accounts a ON a.user_id = u.id")
	require.Len(t, analysis.JoinClauses, 1)
	assert.Equal(t, joinGlobal, analysis.JoinClauses[0].Kind)
}

func TestDML_CTEMaterializedQualifierCaptured(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "WITH active AS (SELECT id FROM users) MATERIALIZED SELECT * FROM active")
	require.Len(t, analysis.CTEDefinitions, 1)
	require.Equal(t, &cteDetail{materialized: true}, analysis.CTEDefinitions[0].Detail)
}

func TestDML_CTENotMaterializedQualifierCaptured(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "WITH active AS (SELECT id FROM users) NOT MATERIALIZED SELECT * FROM active")
	require.Len(t, analysis.CTEDefinitions, 1)
	require.Equal(t, &cteDetail{materialized: false}, analysis.CTEDefinitions[0].Detail)
}

func TestDML_CTEWithoutMaterializedHasNoDetail(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "WITH active AS (SELECT id FROM users) SELECT * FROM active")
	require.Len(t, analysis.CTEDefinitions, 1)
	assert.Nil(t, analysis.CTEDefinitions[0].Detail, "plain CTE should record no detail")
}

func TestDML_LimitWithFillBoundariesCaptured(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT id FROM events ORDER BY id LIMIT 10 WITH FILL FROM 1 TO 100 STEP 2")
	require.NotNil(t, analysis.Detail)
	assert.Equal(t, "1", detailOf(analysis).limitFillFrom)
	assert.Equal(t, "100", detailOf(analysis).limitFillTo)
	assert.Equal(t, "2", detailOf(analysis).limitFillStep)
}

func TestDML_OrderByCapturesDirectionAndNulls(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT id FROM users ORDER BY created_at DESC NULLS LAST, name ASC NULLS FIRST")
	require.Len(t, analysis.OrderByColumns, 2)
	assert.Equal(t, "created_at", analysis.OrderByColumns[0].Expression)
	assert.Equal(t, ast.OrderDirectionDesc, analysis.OrderByColumns[0].Direction)
	assert.Equal(t, ast.OrderNullsLast, analysis.OrderByColumns[0].Nulls)
	assert.Equal(t, "name", analysis.OrderByColumns[1].Expression)
	assert.Equal(t, ast.OrderDirectionAsc, analysis.OrderByColumns[1].Direction)
	assert.Equal(t, ast.OrderNullsFirst, analysis.OrderByColumns[1].Nulls)
}

func TestDML_OrderByCapturesFillSettings(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT id FROM events ORDER BY ts WITH FILL FROM 0 TO 10 STEP 1")
	require.Len(t, analysis.OrderByColumns, 1)
	column := analysis.OrderByColumns[0]
	assert.Equal(t, "ts", column.Expression)
	assert.True(t, column.HasFill)
	assert.Equal(t, "0", column.FillFrom)
	assert.Equal(t, "10", column.FillTo)
	assert.Equal(t, "1", column.FillStep)
}

func TestDML_OrderByEmptyDefaults(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT id FROM events ORDER BY ts")
	require.Len(t, analysis.OrderByColumns, 1)
	column := analysis.OrderByColumns[0]
	assert.Equal(t, ast.OrderDirectionUnspecified, column.Direction)
	assert.Equal(t, ast.OrderNullsUnspecified, column.Nulls)
	assert.False(t, column.HasFill)
}

func TestDML_ArrayJoinAcceptsLiteralArraySource(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT x FROM events ARRAY JOIN [1, 2, 3] AS x WHERE id = 1")
	require.Len(t, analysis.ArrayJoinClauses, 1)
	assert.Empty(t, analysis.ArrayJoinClauses[0].SourceColumn)
	assert.NotEmpty(t, analysis.ArrayJoinClauses[0].SourceExpression)
	assert.Equal(t, "x", analysis.ArrayJoinClauses[0].Alias)
}

func TestDML_ArrayJoinAcceptsFunctionCallSource(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT y FROM events ARRAY JOIN arrayMap(v -> v * 2, src) AS y")
	require.Len(t, analysis.ArrayJoinClauses, 1)
	assert.Empty(t, analysis.ArrayJoinClauses[0].SourceColumn)
	assert.NotEmpty(t, analysis.ArrayJoinClauses[0].SourceExpression)
	assert.Equal(t, "y", analysis.ArrayJoinClauses[0].Alias)
}

func TestDML_ArrayJoinBareColumnPreservesSourceColumn(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT tag FROM events ARRAY JOIN tags AS tag WHERE id = 1")
	require.Len(t, analysis.ArrayJoinClauses, 1)
	assert.Equal(t, "tags", analysis.ArrayJoinClauses[0].SourceColumn)
	assert.Empty(t, analysis.ArrayJoinClauses[0].SourceExpression, "bare column form must not populate SourceExpression")
	assert.Equal(t, "tag", analysis.ArrayJoinClauses[0].Alias)
}

func TestDML_ExistsSubqueryRegistersBodyParameter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sql  string
	}{
		{
			name: "exists in select list",
			sql:  "SELECT EXISTS(SELECT 1 FROM orchestrator_tasks WHERE workflow_id = {workflow_id:String}) AS has_incomplete",
		},
		{
			name: "in subquery in where",
			sql:  "SELECT id FROM workflows WHERE id IN (SELECT workflow_id FROM orchestrator_tasks WHERE workflow_id = {workflow_id:String})",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyse(t, test.sql)
			require.Len(t, analysis.ParameterReferences, 1)
			reference := analysis.ParameterReferences[0]
			assert.Equal(t, "workflow_id", reference.Name)
			require.NotNil(t, reference.CastType, "self-typed placeholder must keep its cast type")
			assert.Equal(t, "String", reference.CastType.EngineName)
		})
	}
}

func TestAnalyse_ParametersInProjectionAndGroupBy(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		sql  string
		want int
	}{
		{name: "projection arithmetic", sql: `SELECT id + {bonus:UInt32} AS total FROM users`, want: 1},
		{name: "projection function arg", sql: `SELECT greatest(x, {y:UInt32}) FROM t`, want: 1},
		{name: "group by expression", sql: `SELECT count() FROM t GROUP BY toStartOfInterval(ts, {step:UInt32})`, want: 1},
		{name: "where still works", sql: `SELECT id FROM users WHERE id = {uid:UInt32}`, want: 1},
		{name: "projection and where", sql: `SELECT id + {bonus:UInt32} FROM users WHERE id = {uid:UInt32}`, want: 2},
	}
	for index := range cases {
		testCase := cases[index]
		t.Run(testCase.name, func(testRunner *testing.T) {
			testRunner.Parallel()
			analysis := analyse(testRunner, testCase.sql)
			require.Len(testRunner, analysis.ParameterReferences, testCase.want,
				"parameters in projection/GROUP BY expressions must be registered")
			for _, reference := range analysis.ParameterReferences {
				assert.NotEmpty(testRunner, reference.Name)
			}
		})
	}
}

func TestFlatSetOperationChainsParseIteratively(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		operator string
		want     ast.CompoundOperator
	}{
		{name: "UNION ALL", operator: " UNION ALL ", want: ast.CompoundUnionAll},
		{name: "UNION", operator: " UNION ", want: ast.CompoundUnion},
		{name: "INTERSECT", operator: " INTERSECT ", want: ast.CompoundIntersect},
		{name: "EXCEPT", operator: " EXCEPT ", want: ast.CompoundExcept},
	}
	const arms = 10_000
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			sql := "SELECT id FROM a" + strings.Repeat(testCase.operator+"SELECT id FROM a", arms-1)
			var analysis *ast.RawQueryAnalysis
			scaling.Within(t, 10*time.Second, func(context.Context) {
				analysis = analyse(t, sql)
			})
			require.Len(t, analysis.CompoundBranches, arms-1)
			for _, branch := range analysis.CompoundBranches {
				require.Equal(t, testCase.want, branch.Operator)
				require.Empty(t, branch.Query.CompoundBranches)
			}
		})
	}
}

func TestNestedSubqueriesStillCountTowardsTheDepthCap(t *testing.T) {
	t.Parallel()

	const nesting = 20
	engine := New(WithMaxParseDepth(nesting / 2))
	sql := strings.Repeat("SELECT * FROM (", nesting) + "SELECT 1" + strings.Repeat(") AS t", nesting)
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.Len(t, statements, 1)

	_, err = engine.AnalyseQuery(t.Context(), nil, statements[0])
	require.ErrorIs(t, err, dialectkit.ErrParseDepth)
}

func TestOverDeepProjectionExpressionsAreReported(t *testing.T) {
	t.Parallel()

	const nesting = 40
	testCases := []struct {
		name string
		sql  string
	}{
		{name: "nested parentheses", sql: "SELECT " + strings.Repeat("(", nesting) + "1" + strings.Repeat(")", nesting) + " AS x FROM t"},
		{name: "nested function calls", sql: "SELECT " + strings.Repeat("abs(", nesting) + "1" + strings.Repeat(")", nesting) + " AS x FROM t"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New(WithMaxParseDepth(nesting / 2))
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			require.Len(t, statements, 1)

			analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])
			require.ErrorIs(t, err, dialectkit.ErrParseDepth)
			assert.Nil(t, analysis)
			assert.NotContains(t, err.Error(), "panic")
		})
	}
}

func TestShallowExpressionsWithinTheCapAreAccepted(t *testing.T) {
	t.Parallel()

	engine := New(WithMaxParseDepth(64))
	statements, err := engine.ParseStatements(t.Context(), "SELECT ((((1)))) AS x, NOT NOT 1 AS y, - - 1 AS z FROM t")
	require.NoError(t, err)
	require.Len(t, statements, 1)

	analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])
	require.NoError(t, err)
	require.Len(t, analysis.OutputColumns, 3)
}

func TestProjectionStarAfterColumnIsMultiplication(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		sql        string
		wantNames  []string
		wantColumn []string
	}{
		{name: "multiplication with alias", sql: "SELECT id, val * 2 AS doubled FROM src", wantNames: []string{"id", "doubled"}, wantColumn: []string{"id", ""}},
		{name: "qualified star is still an expansion", sql: "SELECT s.* FROM src AS s", wantNames: []string{""}, wantColumn: []string{""}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyse(t, testCase.sql)
			require.Len(t, analysis.OutputColumns, len(testCase.wantNames))
			for index, column := range analysis.OutputColumns {
				assert.Equal(t, testCase.wantNames[index], column.Name)
				assert.Equal(t, testCase.wantColumn[index], column.ColumnName)
			}
		})
	}
}

func TestAnalyseQuery_ColumnsMatcherIsRejected(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"SELECT id, COLUMNS('^w') FROM boats",
		"SELECT id FROM boats WHERE COLUMNS('^w') > 1",
	} {
		engine := New()
		statements, err := engine.ParseStatements(t.Context(), sql)
		require.NoError(t, err)
		_, err = engine.AnalyseQuery(t.Context(), nil, statements[0])
		var parseError *dialectkit.ParseError
		require.ErrorAs(t, err, &parseError, sql)
		assert.Contains(t, parseError.Message, "COLUMNS(...) is not supported", sql)
		assert.Equal(t, strings.Index(sql, "COLUMNS"), parseError.Position, sql)
	}
}

func TestArrayLiteralsStayOneExpression(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		sql     string
		outputs []string
	}{
		{name: "an aliased literal", sql: "SELECT [1, 2] AS a FROM t", outputs: []string{"a"}},
		{name: "a literal beside a column", sql: "SELECT id, [1, 2] AS a, name FROM t", outputs: []string{"id", "a", "name"}},
		{name: "a literal inside a call", sql: "SELECT has([1, 2], id) AS found FROM t", outputs: []string{"found"}},
		{name: "a nested literal", sql: "SELECT [[1, 2], [3]] AS grid FROM t", outputs: []string{"grid"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyse(t, testCase.sql)
			names := make([]string, 0, len(analysis.OutputColumns))
			for _, column := range analysis.OutputColumns {
				names = append(names, column.Name)
			}
			require.Equal(t, testCase.outputs, names)
		})
	}
}

func TestArrayLiteralsInOrderByStayOneItem(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT id FROM t ORDER BY [id, 1] DESC, id")
	require.Len(t, analysis.OrderByColumns, 2)
	require.Equal(t, ast.OrderDirectionDesc, analysis.OrderByColumns[0].Direction)
}

func TestHeredocIsAStringHoldingNoParameters(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT $tag$it's {note:String} $1 $tag$ AS label, $$;$$ AS terminator FROM gulls WHERE id = {id:UInt64}")
	require.Len(t, analysis.OutputColumns, 2)
	require.Len(t, analysis.ParameterReferences, 1)
	require.Equal(t, "id", analysis.ParameterReferences[0].Name)
}

func TestGroupByReportsTheKeysItsModifierLeavesOut(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		sql           string
		engine        *Dialect
		keys          []string
		fillsDefaults bool
		noModifier    bool
	}{
		{name: "plain GROUP BY", sql: "SELECT s, sum(n) FROM t GROUP BY s", noModifier: true},
		{name: "WITH TOTALS", sql: "SELECT s, sum(n) FROM t GROUP BY s WITH TOTALS", noModifier: true},
		{name: "WITH ROLLUP fills defaults", sql: "SELECT s, c, sum(n) FROM t GROUP BY s, c WITH ROLLUP", keys: []string{"s", "c"}, fillsDefaults: true},
		{name: "WITH CUBE", sql: "SELECT s, sum(n) FROM t GROUP BY s WITH CUBE", keys: []string{"s"}, fillsDefaults: true},
		{name: "the query's own group_by_use_nulls", sql: "SELECT s, sum(n) FROM t GROUP BY s WITH ROLLUP SETTINGS group_by_use_nulls = 1", keys: []string{"s"}},
		{name: "the assumed group_by_use_nulls", sql: "SELECT s, sum(n) FROM t GROUP BY s WITH ROLLUP", engine: New(WithGroupByUseNulls(true)), keys: []string{"s"}},
		{name: "ROLLUP as a grouping element", sql: "SELECT s, c, sum(n) FROM t GROUP BY ROLLUP(s, c)", keys: []string{"s", "c"}, fillsDefaults: true},
		{name: "a key beside ROLLUP is never left out", sql: "SELECT s, c, sum(n) FROM t GROUP BY c, ROLLUP(s)", keys: []string{"s"}, fillsDefaults: true},
		{name: "GROUPING SETS", sql: "SELECT s, c, sum(n) FROM t GROUP BY GROUPING SETS ((s, c), (s), ())", keys: []string{"s", "c", "s"}, fillsDefaults: true},
		{name: "a key by position names the select item", sql: "SELECT lower(s) AS l, sum(n) FROM t GROUP BY 1 WITH ROLLUP", keys: []string{"lower"}, fillsDefaults: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := testCase.engine
			if engine == nil {
				engine = New()
			}
			analysis := analyseWith(t, engine, testCase.sql)
			if testCase.noModifier {
				require.Nil(t, analysis.GroupingSets)
				return
			}
			require.NotNil(t, analysis.GroupingSets)
			require.Equal(t, testCase.fillsDefaults, analysis.GroupingSets.FillsDefaults)
			var keys []string
			for _, key := range analysis.GroupingSets.Keys {
				switch typed := key.(type) {
				case *ast.ColumnRefExpression:
					keys = append(keys, typed.ColumnName)
				case *ast.FunctionCallExpression:
					keys = append(keys, typed.FunctionName)
				default:
					keys = append(keys, "?")
				}
			}
			require.Equal(t, testCase.keys, keys)
		})
	}
}

func TestGroupByRollupIsNoFunctionCall(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT s, sum(n) FROM t GROUP BY ROLLUP(s)")
	for _, clause := range analysis.ClauseExpressions {
		if call, isCall := clause.Expression.(*ast.FunctionCallExpression); isCall {
			require.NotEqual(t, "ROLLUP", call.FunctionName)
		}
	}
}

func TestAColumnOfACTEBodysDerivedTableIsNoElement(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "WITH a AS (SELECT d.id FROM (SELECT id FROM events) d) SELECT id FROM a")

	column := analysis.CTEDefinitions[0].OutputColumns[0]
	require.Nil(t, column.Expression)
	require.Equal(t, "d", column.TableAlias)
	require.Equal(t, "id", column.ColumnName)
}

func TestUntupleInASelectListIsRefused(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"SELECT untuple(point) FROM events",
		"SELECT id, untuple(point) AS p FROM events",
		"SELECT untuple(CAST((1, 'a'), 'Tuple(x UInt8, y String)'))",
	} {
		t.Run(sql, func(t *testing.T) {
			t.Parallel()

			require.ErrorContains(t, analyseError(t, sql), "untuple")
		})
	}
}

func TestUntupleSpreadingAFunctionsArgumentsIsAccepted(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT concat(untuple(('a', 'b'))) AS joined FROM events")
	require.Len(t, analysis.OutputColumns, 1)
}

func TestJoinQualifierReadsOnAndUsing(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		sql           string
		wantUsing     []string
		wantCondition bool
		wantErr       string
	}{
		{name: "an ON condition", sql: "SELECT a.id FROM a LEFT JOIN b ON a.id = b.id", wantCondition: true},
		{name: "a USING column", sql: "SELECT id FROM a JOIN b USING id", wantUsing: []string{"id"}},
		{name: "a parenthesised USING list", sql: "SELECT id FROM a LEFT JOIN b USING (id, kind)", wantUsing: []string{"id", "kind"}},
		{name: "no qualifier", sql: "SELECT id FROM a CROSS JOIN b"},
		{name: "a USING list that is not closed after a name", sql: "SELECT id FROM a JOIN b USING (id kind)", wantErr: "expected ')' to close the USING column list"},
		{name: "a USING list without a name", sql: "SELECT id FROM a JOIN b USING ()", wantErr: "expected an identifier"},
		{name: "a USING list ending in a comma", sql: "SELECT id FROM a JOIN b USING id,", wantErr: "expected an identifier"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if testCase.wantErr != "" {
				err := analyseError(t, testCase.sql)
				require.ErrorIs(t, err, fault.ErrParse)
				assert.Contains(t, err.Error(), testCase.wantErr)
				return
			}
			analysis := analyse(t, testCase.sql)
			require.Len(t, analysis.JoinClauses, 1)
			using := analysis.JoinClauses[0].Using
			if testCase.wantUsing == nil {
				assert.Nil(t, using)
			} else {
				require.NotNil(t, using)
				assert.Equal(t, testCase.wantUsing, using.Columns)
				assert.Equal(t, ast.JoinColumnsLead, using.Placement)
			}
			conditions := 0
			for _, clause := range analysis.ClauseExpressions {
				if clause.Clause == ast.ClauseJoinCondition {
					conditions++
				}
			}
			assert.Equal(t, testCase.wantCondition, conditions == 1)
		})
	}
}

func TestLimitAndOffsetPlaceholdersArePaginationBounds(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		sql          string
		wantContexts []sqlmodel.ParameterContext
	}{
		{
			name:         "LIMIT and OFFSET",
			sql:          "SELECT id FROM t LIMIT {size:UInt32} OFFSET {skip:UInt32}",
			wantContexts: []sqlmodel.ParameterContext{sqlmodel.ParameterContextLimit, sqlmodel.ParameterContextOffset},
		},
		{
			name:         "LIMIT offset, count",
			sql:          "SELECT id FROM t LIMIT {skip:UInt32}, {size:UInt32}",
			wantContexts: []sqlmodel.ParameterContext{sqlmodel.ParameterContextOffset, sqlmodel.ParameterContextLimit},
		},
		{
			name:         "an expression is not a bound",
			sql:          "SELECT id FROM t LIMIT {size:UInt32} * 2",
			wantContexts: []sqlmodel.ParameterContext{sqlmodel.ParameterContextComparison},
		},
		{
			name:         "LIMIT BY",
			sql:          "SELECT id FROM t LIMIT {size:UInt32} BY id",
			wantContexts: []sqlmodel.ParameterContext{sqlmodel.ParameterContextLimit},
		},
		{
			name:         "OFFSET and FETCH",
			sql:          "SELECT id FROM t ORDER BY id OFFSET {skip:UInt32} ROWS FETCH FIRST {size:UInt32} ROWS ONLY",
			wantContexts: []sqlmodel.ParameterContext{sqlmodel.ParameterContextOffset, sqlmodel.ParameterContextLimit},
		},
		{
			name:         "OFFSET and FETCH WITH TIES",
			sql:          "SELECT id FROM t ORDER BY id OFFSET 1 ROW FETCH NEXT {size:UInt32} ROW WITH TIES",
			wantContexts: []sqlmodel.ParameterContext{sqlmodel.ParameterContextLimit},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyse(t, testCase.sql)
			contexts := make([]sqlmodel.ParameterContext, 0, len(analysis.ParameterReferences))
			for _, reference := range analysis.ParameterReferences {
				contexts = append(contexts, reference.Context)
			}
			assert.Equal(t, testCase.wantContexts, contexts)
		})
	}
}

func TestKeywordsAreAliasesAfterAs(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT inner.id FROM (SELECT id FROM t) AS inner")
	require.Len(t, analysis.RawDerivedTables, 1)
	assert.Equal(t, "inner", analysis.RawDerivedTables[0].Alias)
	require.Len(t, analysis.OutputColumns, 1)
	assert.Equal(t, "inner", analysis.OutputColumns[0].TableAlias)
}

func TestQuotedKeywordsAreNames(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT `from`, `where` FROM `select` WHERE `group` = {group:String}")
	require.Len(t, analysis.OutputColumns, 2)
	assert.Equal(t, "from", analysis.OutputColumns[0].ColumnName)
	assert.Equal(t, "where", analysis.OutputColumns[1].ColumnName)
	require.Len(t, analysis.FromTables, 1)
	assert.Equal(t, "select", analysis.FromTables[0].Name)
	require.Len(t, analysis.ParameterReferences, 1)
}

func TestATrailingCommaMayEndTheSelectListBeforeFrom(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		sql            string
		wantProjection string
		wantColumns    []string
		wantTables     []string
	}{
		{
			name: "before a table", sql: "SELECT a, b, FROM t",
			wantColumns: []string{"a", "b"}, wantTables: []string{"t"}, wantProjection: "a, b",
		},
		{
			name: "before a qualified table", sql: "SELECT a, FROM db.t WHERE a = {a:UInt8}",
			wantColumns: []string{"a"}, wantTables: []string{"t"}, wantProjection: "a",
		},
		{
			name: "before a table function", sql: "SELECT number, FROM numbers(10)",
			wantColumns: []string{"number"}, wantTables: nil, wantProjection: "number",
		},
		{
			name: "before a subquery", sql: "SELECT a, FROM (SELECT 1 AS a)",
			wantColumns: []string{"a"}, wantTables: nil, wantProjection: "a",
		},
		{
			name: "a column named from after a comma", sql: "SELECT a, from FROM t",
			wantColumns: []string{"a", "from"}, wantTables: []string{"t"}, wantProjection: "a, from",
		},
		{
			name: "a column named from with an alias", sql: "SELECT a, from AS f FROM t",
			wantColumns: []string{"a", "f"}, wantTables: []string{"t"}, wantProjection: "a, from AS f",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, statement := analyseStatement(t, testCase.sql)
			columns := make([]string, 0, len(analysis.OutputColumns))
			for _, column := range analysis.OutputColumns {
				columns = append(columns, cmp.Or(column.Name, column.ColumnName))
			}
			var tables []string
			for _, table := range analysis.FromTables {
				tables = append(tables, table.Name)
			}
			assert.Equal(t, testCase.wantColumns, columns)
			assert.Equal(t, testCase.wantTables, tables)
			assert.Equal(t, testCase.wantProjection, spanTextOf(testCase.sql, statement, analysis.Spans.Projection))
		})
	}
}

func TestAnEmptySelectItemIsStillRefused(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{"SELECT a, , b FROM t", "SELECT 1,", "SELECT a,, FROM t", "SELECT , a FROM t"} {
		err := analyseError(t, sql)
		require.ErrorIs(t, err, fault.ErrParse, sql)
	}
}
