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

package sqltemplate_test

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/clickhouse"
	"seagull.sh/seagull/internal/dialect/duckdb"
	"seagull.sh/seagull/internal/dialect/martian"
	"seagull.sh/seagull/internal/dialect/mysql"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/dialect/sqlite"
	"seagull.sh/seagull/internal/directive/syntax"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/sqltemplate"
	"seagull.sh/seagull/internal/testkit/goldenjson"
)

type buildCase struct {
	engine    dialect.DialectPort
	configure func(input *sqltemplate.Input)
	name      string
	sql       string
	template  string
	count     string
	problems  []string
	wrapped   bool
}

func buildTemplate(t *testing.T, engine dialect.DialectPort, sql string, configure func(input *sqltemplate.Input)) (sqltemplate.Built, []sqltemplate.Problem) {
	t.Helper()

	info := engine.Info()
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.NotEmpty(t, statements)
	analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[len(statements)-1])
	require.NoError(t, err)
	lexicon := syntax.Lexicon{LinePrefix: info.Comments.LinePrefix, Rules: info.Lexical}
	ranges := syntax.StripRanges(sql, lexicon, namespace.Default())
	kept := make([]ast.Span, 0, len(ranges))
	for _, keptRange := range ranges {
		kept = append(kept, ast.Span{Start: keptRange.Start, End: keptRange.End})
	}
	input := &sqltemplate.Input{
		Info:       &info,
		Query:      &sqlmodel.AnalysedQuery{Parameters: parametersOf(analysis)},
		Analysis:   analysis,
		SQL:        sql,
		Statements: statements,
		Kept:       kept,
	}
	if configure != nil {
		configure(input)
	}
	return sqltemplate.Build(input)
}

func parametersOf(analysis *ast.RawQueryAnalysis) []sqlmodel.QueryParameter {
	byNumber := make(map[int]sqlmodel.QueryParameter)
	for _, reference := range analysis.ParameterReferences {
		if _, seen := byNumber[reference.Number]; seen {
			continue
		}
		name := reference.Name
		if name == "" {
			name = fmt.Sprintf("p%d", reference.Number)
		}
		byNumber[reference.Number] = sqlmodel.QueryParameter{Name: name, Number: reference.Number}
	}
	return slices.SortedFunc(maps.Values(byNumber), func(left, right sqlmodel.QueryParameter) int {
		return cmp.Compare(left.Number, right.Number)
	})
}

func compact(t *testing.T, template *sqlmodel.SQLTemplate) string {
	t.Helper()

	if template == nil {
		return ""
	}
	return goldenjson.Template(*template)
}

func codesOf(problems []sqltemplate.Problem) []string {
	codes := make([]string, 0, len(problems))
	for _, problem := range problems {
		codes = append(codes, problem.Code)
	}
	return codes
}

func runBuildCases(t *testing.T, testCases []buildCase) {
	t.Helper()

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			built, problems := buildTemplate(t, testCase.engine, testCase.sql, testCase.configure)

			require.Equal(t, testCase.problems, nilIfEmpty(codesOf(problems)), "%v", problems)
			require.Equal(t, testCase.template, compact(t, &built.Template))
			require.Equal(t, testCase.count, compact(t, built.CountTemplate))
			require.Equal(t, testCase.wrapped, built.CountWrapped)
		})
	}
}

func nilIfEmpty(codes []string) []string {
	if len(codes) == 0 {
		return nil
	}
	return codes
}

func slice(index int) func(input *sqltemplate.Input) {
	return func(input *sqltemplate.Input) { input.Query.Parameters[index].IsSlice = true }
}

func runtimeBuilder(input *sqltemplate.Input) {
	input.Query.DynamicRuntime = true
}

func TestBuildMarksPlaceholdersAndKeepsTheTextAsWritten(t *testing.T) {
	t.Parallel()

	runBuildCases(t, []buildCase{
		{
			name:     "a numbered placeholder becomes its parameter and the header is left out",
			engine:   postgres.New(),
			sql:      "-- sgl.query(name: GetUser, command: one)\nSELECT id, name FROM users WHERE id = $1;",
			template: "SELECT id, name FROM users WHERE id = ⟨p0⟩;⟨arguments p0⟩",
		},
		{
			name:     "a named placeholder is matched by name, and casts and literals that spell it are text",
			engine:   postgres.New(),
			sql:      "SELECT id FROM events WHERE created_at::date = :date AND note <> ':date' AND day = :date",
			template: "SELECT id FROM events WHERE created_at::date = ⟨p0⟩ AND note <> ':date' AND day = ⟨p0⟩⟨arguments p0⟩",
		},
		{
			name:     "a binding line inside the statement is left out",
			engine:   postgres.New(),
			sql:      "-- sgl.query(name: GetUser, command: one)\nSELECT id FROM users\n-- $1 as sgl.param(user_id)\nWHERE id = $1",
			template: `SELECT id FROM users\nWHERE id = ⟨p0⟩⟨arguments p0⟩`,
		},
		{
			name:      "a slice list is numbered after the other parameters",
			engine:    postgres.New(),
			sql:       "SELECT id FROM items WHERE kind IN ($1) AND id > $2",
			template:  "SELECT id FROM items WHERE kind ⟨in p0:IN ⟩ AND id > ⟨p1⟩⟨arguments p1 p0⟩",
			configure: slice(0),
		},
		{
			name:     "anonymous placeholders bind in order around a named one used twice",
			engine:   mysql.New(),
			sql:      "SELECT id FROM t WHERE a = ? AND b = :tag AND c = ? AND d = :tag",
			template: "SELECT id FROM t WHERE a = ⟨p0⟩ AND b = ⟨p1⟩ AND c = ⟨p2⟩ AND d = ⟨p1⟩⟨arguments p0 p1 p2 p1⟩",
		},
		{
			name:      "a plain question mark is a slice list too",
			engine:    sqlite.New(),
			sql:       "SELECT id FROM items WHERE id NOT IN (?)",
			template:  "SELECT id FROM items WHERE id ⟨not in p0:NOT IN ⟩⟨arguments p0⟩",
			configure: slice(0),
		},
		{
			name:     "a typed placeholder keeps its type",
			engine:   clickhouse.New(),
			sql:      "SELECT id FROM items WHERE id = {id:UInt64} AND kind IN ({kinds:Array(String)})",
			template: "SELECT id FROM items WHERE id = ⟨p0 UInt64⟩ AND kind ⟨in p1 Array(String):IN ⟩⟨arguments p0 p1⟩",
			configure: func(input *sqltemplate.Input) {
				input.Query.Parameters[1].IsSlice = true
			},
		},
		{
			name:      "the martian form marks its lists",
			engine:    martian.New(),
			sql:       "SELECT id FROM items WHERE id IN (@p1) AND name = @p2",
			template:  "SELECT id FROM items WHERE id ⟨in p0:IN ⟩ AND name = ⟨p1⟩⟨arguments p1 p0⟩",
			configure: slice(0),
		},
	})
}

func sortable(index int) func(input *sqltemplate.Input) {
	return func(input *sqltemplate.Input) {
		input.Query.IsDynamic = true
		input.Query.Parameters[index].Kind = sqlmodel.ParameterDirectiveSortable
	}
}

func TestBuildMarksTheClausesADynamicQueryEdits(t *testing.T) {
	t.Parallel()

	runBuildCases(t, []buildCase{
		{
			name:      "a sortable query marks its condition, its ORDER BY and the place of its pagination",
			engine:    postgres.New(),
			sql:       "SELECT id, name FROM users WHERE active = true ORDER BY $1",
			template:  "SELECT id, name FROM users WHERE ⟨where:active = true⟩ ⟨order:ORDER BY ⟨p0⟩⟩⟨pagination⟩⟨arguments p0⟩",
			configure: sortable(0),
		},
		{
			name:     "absent clauses are marked where they go, before the terminator",
			engine:   postgres.New(),
			sql:      "SELECT id FROM users;",
			template: "SELECT id FROM users⟨where⟩⟨order⟩⟨pagination⟩;",
			configure: func(input *sqltemplate.Input) {
				input.Query.IsDynamic = true
			},
		},
		{
			name:     "an absent ORDER BY goes before the pagination and locking written",
			engine:   postgres.New(),
			sql:      "SELECT id FROM users WHERE id > $1 LIMIT 10 FOR UPDATE",
			template: "SELECT id FROM users WHERE ⟨where:id > ⟨p0⟩⟩⟨order⟩ ⟨pagination:LIMIT 10⟩ FOR UPDATE⟨arguments p0⟩",
			configure: func(input *sqltemplate.Input) {
				input.Query.IsDynamic = true
			},
		},
		{
			name:     "an absent ORDER BY goes where the dialect records it, before a LIMIT BY that orders after it",
			engine:   clickhouse.New(),
			sql:      "SELECT id, kind FROM items LIMIT 2 BY kind LIMIT 10",
			template: "SELECT id, kind FROM items⟨where⟩⟨order⟩ LIMIT 2 BY kind ⟨pagination:LIMIT 10⟩",
			configure: func(input *sqltemplate.Input) {
				input.Query.IsDynamic = true
			},
		},
		{
			name:     "a static query marks no clause",
			engine:   postgres.New(),
			sql:      "SELECT id FROM users WHERE id = $1 ORDER BY id",
			template: "SELECT id FROM users WHERE id = ⟨p0⟩ ORDER BY id⟨arguments p0⟩",
		},
		{
			name:     "a compound query marks no condition",
			engine:   postgres.New(),
			sql:      "SELECT id FROM a UNION SELECT id FROM b ORDER BY id",
			template: "SELECT id FROM a UNION SELECT id FROM b ⟨order:ORDER BY id⟩⟨pagination⟩",
			configure: func(input *sqltemplate.Input) {
				input.Query.IsDynamic = true
			},
		},
	})
}

func TestBuildCountsTheRowsOfARuntimeBuilderQuery(t *testing.T) {
	t.Parallel()

	runBuildCases(t, []buildCase{
		{
			name:      "a query whose rows are the rows it reads counts them in place",
			engine:    postgres.New(),
			sql:       "-- sgl.query(name: Users, command: many, dynamic: runtime)\nSELECT id, name FROM users WHERE tenant = $1 ORDER BY name LIMIT 10;",
			template:  "SELECT id, name FROM users WHERE ⟨where:tenant = ⟨p0⟩⟩ ⟨order:ORDER BY name⟩ ⟨pagination:LIMIT 10⟩;⟨arguments p0⟩",
			count:     "SELECT COUNT(*) FROM users WHERE ⟨where:tenant = ⟨p0⟩⟩⟨arguments p0⟩",
			configure: runtimeBuilder,
		},
		{
			name:      "a grouped query is counted in a subquery",
			engine:    postgres.New(),
			sql:       "SELECT kind, count(*) AS n FROM items WHERE owner = $1 GROUP BY kind ORDER BY kind",
			template:  "SELECT kind, count(*) AS n FROM items WHERE ⟨where:owner = ⟨p0⟩⟩ GROUP BY kind ⟨order:ORDER BY kind⟩⟨pagination⟩⟨arguments p0⟩",
			count:     "SELECT COUNT(*) FROM (SELECT kind, count(*) AS n FROM items WHERE ⟨where:owner = ⟨p0⟩⟩ GROUP BY kind) sub⟨arguments p0⟩",
			wrapped:   true,
			configure: runtimeBuilder,
		},
		{
			name:     "a query that aggregates without grouping is counted in a subquery",
			engine:   postgres.New(),
			sql:      "SELECT count(*) AS n FROM items",
			template: "SELECT count(*) AS n FROM items⟨where⟩⟨order⟩⟨pagination⟩",
			count:    "SELECT COUNT(*) FROM (SELECT count(*) AS n FROM items⟨where⟩) sub",
			wrapped:  true,
			configure: func(input *sqltemplate.Input) {
				input.Query.DynamicRuntime = true
				input.Aggregated = true
			},
		},
		{
			name:      "a parameter of the select list is not bound by the count",
			engine:    postgres.New(),
			sql:       "SELECT id, $1::text AS label FROM items WHERE owner = $2",
			template:  "SELECT id, ⟨p0⟩::text AS label FROM items WHERE ⟨where:owner = ⟨p1⟩⟩⟨order⟩⟨pagination⟩⟨arguments p0 p1⟩",
			count:     "SELECT COUNT(*) FROM items WHERE ⟨where:owner = ⟨p1⟩⟩⟨arguments p1⟩",
			configure: runtimeBuilder,
		},
		{
			name:      "the clauses that stay last follow the count",
			engine:    clickhouse.New(),
			sql:       "SELECT id FROM items WHERE kind = {kind:String} ORDER BY id LIMIT 5 SETTINGS max_threads = 1",
			template:  "SELECT id FROM items WHERE ⟨where:kind = ⟨p0 String⟩⟩ ⟨order:ORDER BY id⟩ ⟨pagination:LIMIT 5⟩ SETTINGS max_threads = 1⟨arguments p0⟩",
			count:     "SELECT COUNT(*) FROM items WHERE ⟨where:kind = ⟨p0 String⟩⟩ SETTINGS max_threads = 1⟨arguments p0⟩",
			configure: runtimeBuilder,
		},
		{
			name:      "a row-shaping clause after the ORDER BY keeps it in the wrapped count",
			engine:    clickhouse.New(),
			sql:       "SELECT id, kind FROM items ORDER BY id LIMIT 2 BY kind LIMIT 10",
			template:  "SELECT id, kind FROM items⟨where⟩ ⟨order:ORDER BY id⟩ LIMIT 2 BY kind ⟨pagination:LIMIT 10⟩",
			count:     "SELECT COUNT(*) FROM (SELECT id, kind FROM items⟨where⟩ ORDER BY id LIMIT 2 BY kind) sub",
			wrapped:   true,
			configure: runtimeBuilder,
		},
		{
			name:      "a statement that does not read rows has no count",
			engine:    postgres.New(),
			sql:       "DELETE FROM items WHERE id = $1",
			template:  "DELETE FROM items WHERE id = ⟨p0⟩⟨arguments p0⟩",
			configure: runtimeBuilder,
		},
	})
}

func withColumns(origins []int, columns ...sqlmodel.OutputColumn) func(input *sqltemplate.Input) {
	return func(input *sqltemplate.Input) {
		input.Query.OutputColumns = columns
		input.Origins = origins
	}
}

func withReferableColumns(origins []int, referable []bool, columns ...sqlmodel.OutputColumn) func(input *sqltemplate.Input) {
	return func(input *sqltemplate.Input) {
		input.Query.OutputColumns = columns
		input.Origins = origins
		input.Referable = referable
	}
}

func TestBuildMarksTheOutputItems(t *testing.T) {
	t.Parallel()

	runBuildCases(t, []buildCase{
		{
			name:     "a star lists its columns, an expression is a column and an alias stays text",
			engine:   postgres.New(),
			sql:      "SELECT u.*, lower(name) AS shouted, id FROM users u",
			template: "SELECT ⟨star 0=u.id 1=u.name:u.*⟩, ⟨c2 aliased:lower(name)⟩ AS shouted, ⟨c3:id⟩ FROM users u",
			configure: withReferableColumns([]int{0, 0, 1, 2}, []bool{true, true, false, false},
				sqlmodel.OutputColumn{Name: "id", SourceQualifier: "u", SourceColumn: "id"},
				sqlmodel.OutputColumn{Name: "name", SourceQualifier: "u", SourceColumn: "name"},
				sqlmodel.OutputColumn{Name: "shouted"},
				sqlmodel.OutputColumn{Name: "id", SourceColumn: "id"},
			),
		},
		{
			name:     "a star over a relation the catalogue does not hold is written as it is",
			engine:   postgres.New(),
			sql:      "SELECT * FROM (SELECT count(*) FROM users) sub",
			template: "SELECT ⟨star written 0=sub.count:*⟩ FROM (SELECT count(*) FROM users) sub",
			configure: withReferableColumns([]int{0}, []bool{false},
				sqlmodel.OutputColumn{Name: "count", SourceQualifier: "sub", SourceColumn: "count"},
			),
		},
		{
			name:     "a column a FULL join merges is named alone",
			engine:   postgres.New(),
			sql:      "SELECT * FROM users FULL JOIN posts USING (id)",
			template: "SELECT ⟨star 0=id 1=users.name 2=posts.body:*⟩ FROM users FULL JOIN posts USING (id)",
			configure: withReferableColumns([]int{0, 0, 0}, []bool{true, true, true},
				sqlmodel.OutputColumn{Name: "id"},
				sqlmodel.OutputColumn{Name: "name", SourceQualifier: "users", SourceColumn: "name"},
				sqlmodel.OutputColumn{Name: "body", SourceQualifier: "posts", SourceColumn: "body"},
			),
		},
		{
			name:     "a RETURNING star names a column alone in place of an alias unless another of its columns has the name",
			engine:   postgres.New(),
			sql:      "UPDATE posts AS p SET body = $1 FROM users u WHERE u.id = p.user_id RETURNING *",
			template: "UPDATE posts AS p SET body = ⟨p0⟩ FROM users u WHERE u.id = p.user_id RETURNING ⟨star 0=p.id 1=body 2=u.id 3=name:*⟩⟨arguments p0⟩",
			configure: withReferableColumns([]int{0, 0, 0, 0}, []bool{true, true, true, true},
				sqlmodel.OutputColumn{Name: "id", SourceTable: "posts", SourceQualifier: "p", SourceColumn: "id"},
				sqlmodel.OutputColumn{Name: "body", SourceTable: "posts", SourceQualifier: "p", SourceColumn: "body"},
				sqlmodel.OutputColumn{Name: "id", SourceTable: "users", SourceQualifier: "u", SourceColumn: "id"},
				sqlmodel.OutputColumn{Name: "name", SourceTable: "users", SourceQualifier: "u", SourceColumn: "name"},
			),
		},
		{
			name:     "a RETURNING star keeps the name of the table a statement changes",
			engine:   postgres.New(),
			sql:      "DELETE FROM posts RETURNING *",
			template: "DELETE FROM posts RETURNING ⟨star 0=posts.id 1=posts.body:*⟩",
			configure: withReferableColumns([]int{0, 0}, []bool{true, true},
				sqlmodel.OutputColumn{Name: "id", SourceTable: "posts", SourceQualifier: "posts", SourceColumn: "id"},
				sqlmodel.OutputColumn{Name: "body", SourceTable: "posts", SourceQualifier: "posts", SourceColumn: "body"},
			),
		},
		{
			name:     "a qualified RETURNING star keeps its qualifier",
			engine:   postgres.New(),
			sql:      "UPDATE posts AS p SET body = $1 FROM users u WHERE u.id = p.user_id RETURNING p.*",
			template: "UPDATE posts AS p SET body = ⟨p0⟩ FROM users u WHERE u.id = p.user_id RETURNING ⟨star 0=p.id 1=p.body:p.*⟩⟨arguments p0⟩",
			configure: withReferableColumns([]int{0, 0}, []bool{true, true},
				sqlmodel.OutputColumn{Name: "id", SourceQualifier: "p", SourceColumn: "id"},
				sqlmodel.OutputColumn{Name: "body", SourceQualifier: "p", SourceColumn: "body"},
			),
		},
		{
			name:     "the RETURNING items are the output items and the one VALUES row is marked",
			engine:   postgres.New(),
			sql:      "INSERT INTO app.tags (id, name) VALUES ($1, lower($2)) RETURNING id, name",
			template: "INSERT INTO app.tags (id, name) VALUES ⟨values:(⟨p0⟩, lower(⟨p1⟩))⟩ RETURNING ⟨c0:id⟩, ⟨c1:name⟩⟨arguments p0 p1⟩",
			configure: withColumns([]int{0, 1},
				sqlmodel.OutputColumn{Name: "id", SourceColumn: "id"},
				sqlmodel.OutputColumn{Name: "name", SourceColumn: "name"},
			),
		},
		{
			name:     "an item a later item names by its alias is written as it is, so no wrap changes what that item reads",
			engine:   duckdb.New(),
			sql:      "SELECT price AS p, p * 2 AS d FROM items",
			template: "SELECT price AS p, ⟨c1 aliased:p * 2⟩ AS d FROM items",
			configure: func(input *sqltemplate.Input) {
				withColumns([]int{0, 1}, sqlmodel.OutputColumn{Name: "p", SourceColumn: "price"}, sqlmodel.OutputColumn{Name: "d"})(input)
				input.NamedItems = []int{0}
			},
		},
		{
			name:     "items are not marked when the origins of the columns are unknown",
			engine:   postgres.New(),
			sql:      "SELECT id FROM users",
			template: "SELECT id FROM users",
		},
	})
}

func TestBuildReportsTheInsertTargetAsWritten(t *testing.T) {
	t.Parallel()

	built, problems := buildTemplate(t, postgres.New(), "INSERT INTO app.tags (id) VALUES ($1), ($2)", nil)

	require.Empty(t, problems)
	require.Equal(t, sqlmodel.TableName{Schema: "app", Name: "tags"}, built.InsertTarget)
	require.Equal(t, "INSERT INTO app.tags (id) VALUES (⟨p0⟩), (⟨p1⟩)⟨arguments p0 p1⟩", compact(t, &built.Template))
}

func TestBuildReportsParametersWrittenWhereTheyCannotWork(t *testing.T) {
	t.Parallel()

	runBuildCases(t, []buildCase{
		{
			name:      "a slice parameter outside an IN list",
			engine:    postgres.New(),
			sql:       "SELECT id FROM items WHERE id = ANY($1)",
			template:  "SELECT id FROM items WHERE id = ANY(⟨p0⟩)⟨arguments p0⟩",
			problems:  []string{diag.CodeSliceParameterShape},
			configure: slice(0),
		},
		{
			name:     "a slice parameter whose type is an array already",
			engine:   clickhouse.New(),
			sql:      "SELECT id FROM items WHERE id IN ({ids:Array(UInt64)})",
			template: "SELECT id FROM items WHERE id ⟨in p0 Array(UInt64):IN ⟩⟨arguments p0⟩",
			problems: []string{diag.CodeSliceParameterShape},
			configure: func(input *sqltemplate.Input) {
				input.Query.Parameters[0].IsSlice = true
				input.Query.Parameters[0].SQLType = sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray, EngineName: "Array(UInt64)"}
			},
		},
		{
			name:     "an optional parameter whose placeholder cannot hold NULL",
			engine:   clickhouse.New(),
			sql:      "SELECT id FROM items WHERE ({kind:String} IS NULL OR kind = {kind:String})",
			template: "SELECT id FROM items WHERE (⟨p0 String⟩ IS NULL OR kind = ⟨p0 String⟩)⟨arguments p0⟩",
			problems: []string{diag.CodeOptionalNotNullable},
			configure: func(input *sqltemplate.Input) {
				input.Query.Parameters[0].IsOptional = true
				input.Query.Parameters[0].SQLType = sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "String"}
			},
		},
		{
			name:     "an optional parameter whose placeholder holds NULL",
			engine:   clickhouse.New(),
			sql:      "SELECT id FROM items WHERE ({kind:Nullable(String)} IS NULL OR kind = {kind:Nullable(String)})",
			template: "SELECT id FROM items WHERE (⟨p0 Nullable(String)⟩ IS NULL OR kind = ⟨p0 Nullable(String)⟩)⟨arguments p0⟩",
			configure: func(input *sqltemplate.Input) {
				input.Query.Parameters[0].IsOptional = true
				input.Query.Parameters[0].SQLType = sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "String", Nullable: true}
			},
		},
		{
			name:      "a parameter in the ORDER BY of a sortable query",
			engine:    postgres.New(),
			sql:       "SELECT id FROM points ORDER BY $1, distance(position, $2)",
			template:  "SELECT id FROM points⟨where⟩ ⟨order:ORDER BY ⟨p0⟩, distance(position, ⟨p1⟩)⟩⟨pagination⟩⟨arguments p0 p1⟩",
			problems:  []string{diag.CodeSortableOrderByParameter},
			configure: sortable(0),
		},
		{
			name:     "a placeholder in a statement before the query's own",
			engine:   postgres.New(),
			sql:      "SELECT set_config('app.tenant', $1, true);\nSELECT id FROM items WHERE owner = $1;",
			template: "SELECT set_config('app.tenant', $1, true);\\nSELECT id FROM items WHERE owner = ⟨p0⟩;⟨arguments p0⟩",
			problems: []string{diag.CodeParametersBeforeStatement},
		},
		{
			name:     "a placeholder that matches no parameter breaks the template",
			engine:   postgres.New(),
			sql:      "SELECT id FROM items WHERE id = $1",
			template: "",
			problems: []string{diag.CodeInternalNilGuard},
			configure: func(input *sqltemplate.Input) {
				input.Query.Parameters = nil
			},
		},
	})
}
