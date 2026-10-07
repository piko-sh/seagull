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
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestTableFunctionOfABaseTypeNamesItsColumnAfterTheAlias(t *testing.T) {
	t.Parallel()

	integer := sqlmodel.NewIntegerType("int4", widthInteger, false)
	catalogue := &sqlmodel.Catalogue{
		Schemas: map[string]*sqlmodel.Schema{
			"public": {
				Name: "public",
				Functions: map[string][]*sqlmodel.Function{
					"numbers": {{Name: "numbers", Schema: "public", ReturnType: integer, ReturnsSet: true}},
				},
			},
		},
		DefaultSchema: "public",
	}
	aliasOnly := func(name string) []ast.TVFColumnDefinition {
		return []ast.TVFColumnDefinition{{Name: name, TypeName: ""}}
	}
	testCases := []struct {
		name string
		sql  string
		want []ast.TVFColumnDefinition
	}{
		{name: "unnest of one array", sql: "SELECT u FROM items, unnest(tags) AS u", want: aliasOnly("u")},
		{name: "unnest without AS", sql: "SELECT u FROM items, unnest(tags) u", want: aliasOnly("u")},
		{name: "unnest with ordinality", sql: "SELECT u FROM items, unnest(tags) WITH ORDINALITY AS u", want: aliasOnly("u")},
		{name: "unnest of several arrays", sql: "SELECT * FROM unnest(ARRAY[1], ARRAY['a']) AS z", want: nil},
		{name: "generate_series", sql: "SELECT g FROM generate_series(1, 3) AS g", want: aliasOnly("g")},
		{name: "regexp_split_to_table", sql: "SELECT r FROM regexp_split_to_table('a,b', ',') AS r", want: aliasOnly("r")},
		{name: "a function naming its column", sql: "SELECT value FROM jsonb_array_elements('[1]') AS e", want: nil},
		{name: "a function of several columns", sql: "SELECT key FROM jsonb_each('{}') AS e", want: nil},
		{name: "a user function returning a set of a base type", sql: "SELECT s FROM numbers() AS s", want: aliasOnly("s")},
		{name: "a column definition list wins", sql: "SELECT v FROM unnest(ARRAY[1]) AS u(v)", want: aliasOnly("v")},
		{name: "a column list after an alias without AS", sql: "SELECT v FROM generate_series(1, 3) g(v)", want: aliasOnly("v")},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, catalogue, testCase.sql)
			require.Len(t, analysis.RawTableValuedFunctions, 1)
			require.Equal(t, testCase.want, analysis.RawTableValuedFunctions[0].ColumnDefinitions)
		})
	}
}

func TestAColumnDefinitionListKeepsTheModifiersAndQuotedLettersOfItsTypes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
		want []ast.TVFColumnDefinition
	}{
		{
			name: "modifiers, arrays and a time zone",
			sql: "SELECT * FROM json_to_record($1) AS x (price numeric(10, 2), codes varchar(8)[], seen timestamp(3) with time zone, " +
				"label text)",
			want: []ast.TVFColumnDefinition{
				{Name: "price", TypeName: "numeric(10,2)"},
				{Name: "codes", TypeName: "varchar(8)[]"},
				{Name: "seen", TypeName: "timestamp(3) with time zone"},
				{Name: "label", TypeName: "text"},
			},
		},
		{
			name: "a modifier read as its magnitude",
			sql:  "SELECT * FROM json_to_record($1) AS x (price numeric(10.5, 0x2))",
			want: []ast.TVFColumnDefinition{{Name: "price", TypeName: "numeric(10,2)"}},
		},
		{
			name: "a quoted type beside an unquoted one",
			sql:  `SELECT * FROM json_to_record($1) AS x (quoted "Shapes"."Mood", plain MOOD)`,
			want: []ast.TVFColumnDefinition{{Name: "quoted", TypeName: `"Shapes"."Mood"`}, {Name: "plain", TypeName: "mood"}},
		},
		{
			name: "an empty modifier list",
			sql:  "SELECT * FROM json_to_record($1) AS x (price numeric(), label text)",
			want: []ast.TVFColumnDefinition{{Name: "price", TypeName: "numeric"}, {Name: "label", TypeName: "text"}},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, nil, testCase.sql)
			require.Len(t, analysis.RawTableValuedFunctions, 1)
			require.Equal(t, testCase.want, analysis.RawTableValuedFunctions[0].ColumnDefinitions)
		})
	}
}

func TestTableFunctionWithOrdinalityRecordsItsColumn(t *testing.T) {
	t.Parallel()

	ordinality := &ast.ScopedColumn{Name: "ordinality", SQLType: New().NormaliseTypeName("bigint"), Nullable: false}
	testCases := []struct {
		name string
		sql  string
		want *ast.ScopedColumn
	}{
		{name: "unnest with ordinality", sql: "SELECT u FROM items, unnest(tags) WITH ORDINALITY AS u", want: ordinality},
		{name: "with ordinality and an alias list", sql: "SELECT x, n FROM items, unnest(tags) WITH ORDINALITY AS u(x, n)", want: ordinality},
		{name: "with ordinality and no alias", sql: "SELECT ordinality FROM jsonb_each('{}') WITH ORDINALITY", want: ordinality},
		{name: "lateral with ordinality", sql: "SELECT p FROM items, LATERAL generate_series(1, 3) WITH ORDINALITY g(v, p)", want: ordinality},
		{name: "without ordinality", sql: "SELECT u FROM items, unnest(tags) AS u", want: nil},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, nil, testCase.sql)
			require.Len(t, analysis.RawTableValuedFunctions, 1)
			require.Equal(t, testCase.want, analysis.RawTableValuedFunctions[0].Ordinality)
		})
	}
}

func TestAliasNamesTheOneColumnOfAnyFunctionUnderTheFlavourOption(t *testing.T) {
	t.Parallel()

	integer := sqlmodel.NewIntegerType("int4", widthInteger, false)
	catalogue := &sqlmodel.Catalogue{
		Schemas: map[string]*sqlmodel.Schema{
			"public": {
				Name: "public",
				Functions: map[string][]*sqlmodel.Function{
					"numbers": {{
						Name: "numbers", Schema: "public", ReturnsSet: true,
						ReturnType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryComposite, EngineName: "numbered"},
					}},
				},
				CompositeTypes: map[string]*sqlmodel.CompositeType{
					"numbered": {Name: "numbered", Schema: "public", Fields: []sqlmodel.Column{{Name: "x", SQLType: integer}}},
				},
			},
		},
		DefaultSchema: "public",
	}
	aliasOnly := func(name string) []ast.TVFColumnDefinition {
		return []ast.TVFColumnDefinition{{Name: name, TypeName: ""}}
	}
	testCases := []struct {
		name       string
		sql        string
		postgres   []ast.TVFColumnDefinition
		flavour    []ast.TVFColumnDefinition
		ordinality bool
	}{
		{name: "a function naming its one column", sql: "SELECT e FROM jsonb_array_elements('[1]') AS e", postgres: nil, flavour: aliasOnly("e")},
		{name: "a function naming its one column, unaliased", sql: "SELECT value FROM jsonb_array_elements('[1]')", postgres: nil, flavour: nil},
		{name: "a user function returning one named column", sql: "SELECT n FROM numbers() AS n", postgres: nil, flavour: aliasOnly("n")},
		{name: "a function of a base type", sql: "SELECT g FROM generate_series(1, 3) AS g", postgres: aliasOnly("g"), flavour: aliasOnly("g")},
		{name: "a function of several columns", sql: "SELECT key FROM jsonb_each('{}') AS e", postgres: nil, flavour: nil},
		{name: "unnest of several arrays", sql: "SELECT * FROM unnest(ARRAY[1], ARRAY['a']) AS z", postgres: nil, flavour: nil},
		{name: "a column list wins", sql: "SELECT x FROM jsonb_array_elements('[1]') AS e(x)", postgres: aliasOnly("x"), flavour: aliasOnly("x")},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			for _, engine := range []struct {
				dialect *Dialect
				want    []ast.TVFColumnDefinition
			}{
				{dialect: New(), want: testCase.postgres},
				{dialect: New(WithAliasNamesSingleColumnFunctions()), want: testCase.flavour},
			} {
				statements, err := engine.dialect.ParseStatements(t.Context(), testCase.sql)
				require.NoError(t, err)
				analysis, err := engine.dialect.AnalyseQuery(t.Context(), catalogue, statements[0])
				require.NoError(t, err)
				require.Len(t, analysis.RawTableValuedFunctions, 1)
				require.Equal(t, engine.want, analysis.RawTableValuedFunctions[0].ColumnDefinitions)
			}
		})
	}
}

func TestAnAliasedParenthesisedJoinIsADerivedTableOverTheJoin(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, newPostgresCatalogue(),
		"SELECT r.id FROM (users u JOIN posts p ON p.user_id = u.id AND p.title = $1) AS r (id, name) JOIN orders o ON o.user_id = r.id")

	require.Equal(t, []ast.JoinClause{{Using: nil, Table: ast.TableReference{Schema: "", Name: "orders", Alias: "o"}, Kind: ast.JoinInner}},
		analysis.JoinClauses)
	require.Empty(t, analysis.FromTables)
	require.Len(t, analysis.RawDerivedTables, 1)
	derived := analysis.RawDerivedTables[0]
	require.Equal(t, "r", derived.Alias)
	require.Equal(t, []string{"id", "name"}, derived.ColumnNames)
	inner := derived.InnerQuery
	require.Equal(t, []ast.TableReference{{Schema: "", Name: "users", Alias: "u"}}, inner.FromTables)
	require.Len(t, inner.JoinClauses, 1)
	require.Equal(t, "posts", inner.JoinClauses[0].Table.Name)
	require.True(t, inner.OutputColumns[0].IsStar)
	require.Len(t, inner.ClauseExpressions, 1)
	require.Len(t, inner.ParameterReferences, 1)
	require.Len(t, analysis.ParameterReferences, 1)
	require.Equal(t, []string{"u", "p"}, inner.FromOrder)
	require.Equal(t, []string{"r", "o"}, analysis.FromOrder)
	require.Len(t, analysis.ClauseExpressions, 1, "only the ON condition of the join onto orders belongs to the query")
}

func TestAnUnaliasedParenthesisedJoinStaysInTheFromClause(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, newPostgresCatalogue(), "SELECT u.id FROM (users u JOIN posts p ON p.user_id = u.id)")

	require.Equal(t, []ast.TableReference{{Schema: "", Name: "users", Alias: "u"}}, analysis.FromTables)
	require.Len(t, analysis.JoinClauses, 1)
	require.Empty(t, analysis.RawDerivedTables)
}

func TestADerivedTableRecordsItsColumnList(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, newPostgresCatalogue(), "SELECT d.a FROM (SELECT id, name FROM users) AS d (a)")

	require.Len(t, analysis.RawDerivedTables, 1)
	require.Equal(t, []string{"a"}, analysis.RawDerivedTables[0].ColumnNames)
	require.Len(t, analysis.RawDerivedTables[0].InnerQuery.OutputColumns, 2)
}

func TestEachArmOfASetOperationKeepsWhatItReads(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, newPostgresCatalogue(),
		"SELECT id FROM users WHERE id = $1 UNION ALL SELECT d.id FROM (SELECT id FROM posts WHERE title = $2) d ORDER BY 1 LIMIT $3")

	require.Empty(t, analysis.RawDerivedTables)
	require.Len(t, analysis.ParameterReferences, 3)
	require.Len(t, analysis.CompoundBranches, 1)
	arm := analysis.CompoundBranches[0].Query
	require.Len(t, arm.RawDerivedTables, 1)
	require.Equal(t, "d", arm.RawDerivedTables[0].Alias)
	require.Len(t, arm.ParameterReferences, 1)
	require.Equal(t, 2, arm.ParameterReferences[0].Number)
}

func TestASubqueryInAConditionCarriesItsQuery(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, newPostgresCatalogue(), "SELECT id FROM users WHERE (SELECT max(p.id) FROM posts p) = $1")

	require.Len(t, analysis.ClauseExpressions, 1)
	comparison, isComparison := analysis.ClauseExpressions[0].Expression.(*ast.ComparisonExpression)
	require.True(t, isComparison)
	subquery, isSubquery := comparison.Left.(*ast.ScalarSubqueryExpression)
	require.True(t, isSubquery)
	require.Len(t, analysis.PredicateSubqueries, 1)
	require.Same(t, analysis.PredicateSubqueries[0], subquery.InnerQuery)
	require.Len(t, analysis.ParameterReferences, 1)
	require.Nil(t, analysis.ParameterReferences[0].ColumnReference, "no column of the subquery is the compared column")
}

func TestAQualifiedStarNamesTheTableItsQualifiersEndWith(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
		want string
	}{
		{name: "a table star", sql: "SELECT users.* FROM users", want: "users"},
		{name: "a schema-qualified star", sql: "SELECT public.users.* FROM public.users", want: "users"},
		{name: "a database-qualified star", sql: "SELECT app.public.users.* FROM public.users", want: "users"},
		{name: "a schema-qualified star after a column", sql: "SELECT p.id, public.users.* FROM posts p JOIN public.users ON public.users.id = p.user_id", want: "users"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newPostgresCatalogue(), testCase.sql)

			star := analysis.OutputColumns[len(analysis.OutputColumns)-1]
			require.True(t, star.IsStar)
			require.Equal(t, testCase.want, star.TableAlias)
		})
	}
}

func TestASchemaQualifiedStarInsideACallIsAnExpression(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, newPostgresCatalogue(), "SELECT count(public.users.*) AS total FROM public.users")

	require.Equal(t, []string{"total"}, outputNames(analysis))
}

func TestATableWithAColumnAliasListBecomesARelationOfTheRenamedColumns(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		sql       string
		alias     string
		columns   []string
		joinKind  ast.JoinKind
		fromTable string
	}{
		{name: "every column renamed", sql: "SELECT r.a, r.b FROM users AS r (a, b)", alias: "r", columns: []string{"a", "b"},
			joinKind: ast.JoinInner, fromTable: "users"},
		{name: "the leading column renamed without AS", sql: "SELECT * FROM public.users r (a)", alias: "r", columns: []string{"a"},
			joinKind: ast.JoinInner, fromTable: "users"},
		{name: "a joined table renamed", sql: "SELECT u.a FROM posts p LEFT JOIN users u (a) ON u.a = p.user_id", alias: "u",
			columns: []string{"a"}, joinKind: ast.JoinLeft, fromTable: "users"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newPostgresCatalogue(), testCase.sql)

			require.Len(t, analysis.RawDerivedTables, 1)
			derived := analysis.RawDerivedTables[0]
			require.Equal(t, testCase.alias, derived.Alias)
			require.Equal(t, testCase.columns, derived.ColumnNames)
			require.Equal(t, testCase.joinKind, derived.JoinKind)
			require.NotNil(t, derived.InnerQuery)
			require.Len(t, derived.InnerQuery.FromTables, 1)
			require.Equal(t, testCase.fromTable, derived.InnerQuery.FromTables[0].Name)
			require.Contains(t, analysis.FromOrder, testCase.alias)
		})
	}
}

func analyseQueryWith(t *testing.T, engine *Dialect, sql string) (*ast.RawQueryAnalysis, error) {
	t.Helper()

	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.NotEmpty(t, statements)

	return engine.AnalyseQuery(t.Context(), newPostgresCatalogue(), statements[0])
}

func TestAsOfSystemTimeReadsTheFromClauseAtAnEarlierTimeWhereTheDialectTakesIt(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		sql    string
		tables []string
	}{
		{name: "an interval before WHERE", sql: "SELECT id, name FROM users AS OF SYSTEM TIME '-10s' WHERE id > $1", tables: []string{"users"}},
		{name: "a follower read", sql: "SELECT id FROM users AS OF SYSTEM TIME follower_read_timestamp()", tables: []string{"users"}},
		{name: "after a join", sql: "SELECT u.id FROM users AS u JOIN posts AS p ON p.user_id = u.id AS OF SYSTEM TIME '-1s'", tables: []string{"users"}},
		{name: "after a comma list", sql: "SELECT u.id FROM users u, posts p AS OF SYSTEM TIME '-1s' WHERE p.user_id = u.id", tables: []string{"users", "posts"}},
		{name: "after a join merging columns", sql: "SELECT id FROM users JOIN posts USING (id) AS OF SYSTEM TIME '-1s' ORDER BY id LIMIT 1", tables: []string{"users"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, err := analyseQueryWith(t, New(WithAsOfSystemTime()), testCase.sql)
			require.NoError(t, err)

			names := make([]string, 0, len(analysis.FromTables))
			for _, table := range analysis.FromTables {
				names = append(names, table.Name)
			}
			require.Equal(t, testCase.tables, names)
			require.True(t, analysis.ReadOnly)
		})
	}
}

func TestAsOfSystemTimeIsRefusedWhereTheEngineRefusesIt(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		engine  *Dialect
		sql     string
		message string
	}{
		{name: "on PostgreSQL", engine: New(), sql: "SELECT id FROM users AS OF SYSTEM TIME '-10s'", message: "SYSTEM"},
		{name: "in a subquery", engine: New(WithAsOfSystemTime()),
			sql: "SELECT id FROM users WHERE id IN (SELECT user_id FROM posts AS OF SYSTEM TIME '-1s')", message: "top-level"},
		{name: "with a parameter", engine: New(WithAsOfSystemTime()), sql: "SELECT id FROM users AS OF SYSTEM TIME $1", message: "constant"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := analyseQueryWith(t, testCase.engine, testCase.sql)
			require.ErrorContains(t, err, testCase.message)
		})
	}
}

func TestANamedParameterInACommonTableExpressionIsNumberedAfterEveryWrittenNumber(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
		want []string
	}{
		{
			name: "a named parameter in the body and a numbered one after it",
			sql:  "WITH c AS (SELECT id, name FROM users WHERE name = :name) SELECT id FROM c WHERE id = $1",
			want: []string{"name=2", "$1"},
		},
		{
			name: "the same name in two bodies and the main query",
			sql: "WITH a AS (SELECT id FROM users WHERE name = :name), b AS (SELECT id FROM users WHERE name = :name) " +
				"SELECT id FROM a WHERE id = $2 AND name = :name",
			want: []string{"name=3", "name=3", "$2", "name=3"},
		},
		{
			name: "a named parameter in a body changing rows",
			sql:  "WITH d AS (DELETE FROM users WHERE name = :name RETURNING id) SELECT id FROM d WHERE id = $1",
			want: []string{"name=2", "$1"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newPostgresCatalogue(), testCase.sql)

			require.Equal(t, testCase.want, describeParameterNumbers(analysis.ParameterReferences))
		})
	}
}

func describeParameterNumbers(references []ast.RawParameterReference) []string {
	described := make([]string, 0, len(references))
	for _, reference := range references {
		if reference.Name == "" {
			described = append(described, "$"+strconv.Itoa(reference.Number))
			continue
		}
		described = append(described, reference.Name+"="+strconv.Itoa(reference.Number))
	}
	return described
}

func TestAParameterPassedByNameInACommonTableExpressionKeepsItsName(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
		want map[int]string
	}{
		{name: "in a query's body", sql: "WITH c AS (SELECT concat_ws(sep => $1, 'a') AS v) SELECT v FROM c", want: map[int]string{1: "sep"}},
		{name: "in a body changing rows", sql: "WITH d AS (DELETE FROM users WHERE name = concat_ws(sep => $2, 'a') RETURNING id) SELECT id FROM d WHERE id = $1",
			want: map[int]string{2: "sep"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newPostgresCatalogue(), testCase.sql)

			require.Equal(t, testCase.want, analysis.ArgumentNames)
		})
	}
}

func TestATableFunctionKeepsTheNameItsTokenGives(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		sql           string
		wantFunction  string
		wantEnclosing string
	}{
		{name: "a quoted mixed-case name keeps its case", sql: `SELECT * FROM "Series"($1)`, wantFunction: "Series", wantEnclosing: "Series"},
		{name: "an unquoted name folds to lower case", sql: `SELECT * FROM SERIES($1)`, wantFunction: "series", wantEnclosing: "series"},
		{name: "a qualified quoted name keeps its case", sql: `SELECT * FROM reports."Series"($1)`, wantFunction: "reports.Series",
			wantEnclosing: "reports.Series"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newPostgresCatalogue(), testCase.sql)

			require.Len(t, analysis.RawTableValuedFunctions, 1)
			require.Equal(t, testCase.wantFunction, analysis.RawTableValuedFunctions[0].FunctionName)
			require.Len(t, analysis.ParameterReferences, 1)
			require.Equal(t, testCase.wantEnclosing, analysis.ParameterReferences[0].EnclosingFunctionName)
		})
	}
}
