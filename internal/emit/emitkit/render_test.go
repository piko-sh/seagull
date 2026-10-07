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

package emitkit_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

func repeatedParameters() sqlmodel.SQLTemplate {
	return template(
		text("SELECT id FROM t WHERE id = "), parameter(0),
		text(" AND name = "), parameter(1),
		text(" OR id = "), parameter(0),
	)
}

func typedParameters() sqlmodel.SQLTemplate {
	return template(
		text("SELECT id FROM t WHERE id = "), typed(0, "UInt64"),
		text(" AND name = "), typed(1, "String"),
		text(" OR id = "), typed(0, "UInt64"),
	)
}

func TestRenderPlaceholderForms(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		template  sqlmodel.SQLTemplate
		renderer  emitkit.Renderer
		sql       string
		arguments []emitkit.Argument
	}{
		{
			name:      "a numbered form binds each parameter once, in number order",
			template:  repeatedParameters(),
			renderer:  renderer(postgresPlaceholders, postgresPlaceholders.Execution),
			sql:       "SELECT id FROM t WHERE id = $1 AND name = $2 OR id = $1",
			arguments: []emitkit.Argument{{Parameter: 0}, {Parameter: 1}},
		},
		{
			name:      "a numbered form with another prefix writes it",
			template:  repeatedParameters(),
			renderer:  renderer(sqlitePlaceholders, sqlitePlaceholders.Execution),
			sql:       "SELECT id FROM t WHERE id = ?1 AND name = ?2 OR id = ?1",
			arguments: []emitkit.Argument{{Parameter: 0}, {Parameter: 1}},
		},
		{
			name:      "the martian form numbers after a two-character prefix",
			template:  repeatedParameters(),
			renderer:  renderer(martianPlaceholders, martianPlaceholders.Execution),
			sql:       "SELECT id FROM t WHERE id = @p1 AND name = @p2 OR id = @p1",
			arguments: []emitkit.Argument{{Parameter: 0}, {Parameter: 1}},
		},
		{
			name:      "an anonymous form binds every occurrence in the order written",
			template:  repeatedParameters(),
			renderer:  renderer(mysqlPlaceholders, mysqlPlaceholders.Execution),
			sql:       "SELECT id FROM t WHERE id = ? AND name = ? OR id = ?",
			arguments: []emitkit.Argument{{Parameter: 0}, {Parameter: 1}, {Parameter: 0}},
		},
		{
			name:     "a typed named form binds each parameter once under its prefixed name",
			template: typedParameters(),
			renderer: renderer(clickhousePlaceholders, clickhousePlaceholders.Execution),
			sql:      "SELECT id FROM t WHERE id = {p_id:UInt64} AND name = {p_name:String} OR id = {p_id:UInt64}",
			arguments: []emitkit.Argument{
				{Name: "p_id", Parameter: 0},
				{Name: "p_name", Parameter: 1},
			},
		},
		{
			name:      "the positional form of a named dialect drops the types and names",
			template:  typedParameters(),
			renderer:  renderer(clickhousePlaceholders, clickhousePlaceholders.PositionalForm()),
			sql:       "SELECT id FROM t WHERE id = $1 AND name = $2 OR id = $1",
			arguments: []emitkit.Argument{{Parameter: 0}, {Parameter: 1}},
		},
		{
			name:     "the zero template renders nothing",
			template: sqlmodel.SQLTemplate{},
			renderer: renderer(postgresPlaceholders, postgresPlaceholders.Execution),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			rendered, err := emitkit.Render(testCase.template, &testCase.renderer)
			require.NoError(t, err)
			require.Equal(t, emitkit.Rendered{SQL: testCase.sql, Arguments: testCase.arguments}, rendered)
		})
	}
}

func TestRenderLists(t *testing.T) {
	t.Parallel()

	slicesLast := template(
		text("SELECT id FROM t WHERE name = "), parameter(1),
		text(" AND id "), list(2, "IN ", false),
		text(" AND id > "), parameter(0),
	)
	membership := template(
		text("SELECT id FROM t WHERE id "), list(2, "IN ", false),
		text(" AND kind "), list(3, "not in ", true),
	)
	typedMembership := template(
		text("SELECT id FROM t WHERE id "), typedList(2, "Array(UInt64)", "IN ", false),
		text(" AND kind "), typedList(3, "Array(String)", "NOT IN ", true),
	)
	anonymousLists := template(
		text("SELECT id FROM t WHERE a = "), parameter(0),
		text(" AND id "), list(2, "IN ", false),
		text(" AND b = "), parameter(1),
	)
	withMode := func(placeholders dialect.Placeholders, form dialect.PlaceholderSigil, mode emitkit.ListMode) emitkit.Renderer {
		configured := renderer(placeholders, form)
		configured.ListMode = mode
		configured.Lists = arrayLists
		return configured
	}

	testCases := []struct {
		name      string
		template  sqlmodel.SQLTemplate
		renderer  emitkit.Renderer
		sql       string
		arguments []emitkit.Argument
		lists     func(t *testing.T, sql string) []emitkit.ListMark
	}{
		{
			name:      "slice parameters are numbered after the others and expand in place",
			template:  slicesLast,
			renderer:  withMode(postgresPlaceholders, postgresPlaceholders.Execution, emitkit.ListExpand),
			sql:       "SELECT id FROM t WHERE name = $2 AND id IN ($3) AND id > $1",
			arguments: []emitkit.Argument{{Parameter: 0}, {Parameter: 1}, {Parameter: 2, Expand: true}},
			lists: func(t *testing.T, sql string) []emitkit.ListMark {
				return []emitkit.ListMark{{Range: rangeOf(t, sql, "($3)"), Operator: rangeOf(t, sql, "IN "), Parameter: 2, First: 3}}
			},
		},
		{
			name:      "an anonymous form flattens a slice where its list is written",
			template:  anonymousLists,
			renderer:  withMode(mysqlPlaceholders, mysqlPlaceholders.Execution, emitkit.ListExpand),
			sql:       "SELECT id FROM t WHERE a = ? AND id IN (?) AND b = ?",
			arguments: []emitkit.Argument{{Parameter: 0}, {Parameter: 2, Expand: true}, {Parameter: 1}},
			lists: func(t *testing.T, sql string) []emitkit.ListMark {
				return []emitkit.ListMark{{Range: rangeOf(t, sql, "(?)"), Operator: rangeOf(t, sql, "IN "), Parameter: 2, First: 3}}
			},
		},
		{
			name:      "array forms replace the operator and list, the exclusion form for NOT IN",
			template:  membership,
			renderer:  withMode(postgresPlaceholders, postgresPlaceholders.Execution, emitkit.ListArray),
			sql:       "SELECT id FROM t WHERE id = ANY($1) AND kind <> ALL($2)",
			arguments: []emitkit.Argument{{Parameter: 2}, {Parameter: 3}},
			lists: func(t *testing.T, sql string) []emitkit.ListMark {
				return []emitkit.ListMark{
					{Range: rangeOf(t, sql, "= ANY($1)"), Operator: placeBefore(t, sql, "= ANY($1)"), Parameter: 2, First: 1},
					{Range: rangeOf(t, sql, "<> ALL($2)"), Operator: placeBefore(t, sql, "<> ALL($2)"), Parameter: 3, First: 2, Negated: true},
				}
			},
		},
		{
			name:     "lists as written bind the whole slice as one value",
			template: typedMembership,
			renderer: withMode(clickhousePlaceholders, clickhousePlaceholders.Execution, emitkit.ListAsWritten),
			sql:      "SELECT id FROM t WHERE id IN ({p_ids:Array(UInt64)}) AND kind NOT IN ({p_kinds:Array(String)})",
			arguments: []emitkit.Argument{
				{Name: "p_ids", Parameter: 2},
				{Name: "p_kinds", Parameter: 3},
			},
			lists: func(t *testing.T, sql string) []emitkit.ListMark {
				return []emitkit.ListMark{
					{Range: rangeOf(t, sql, "({p_ids:Array(UInt64)})"), Operator: rangeOf(t, sql, "IN "), Parameter: 2, First: 1},
					{Range: rangeOf(t, sql, "({p_kinds:Array(String)})"), Operator: rangeOf(t, sql, "NOT IN "), Parameter: 3, First: 2, Negated: true},
				}
			},
		},
		{
			name:      "the martian form expands its lists like any numbered form",
			template:  slicesLast,
			renderer:  withMode(martianPlaceholders, martianPlaceholders.Execution, emitkit.ListExpand),
			sql:       "SELECT id FROM t WHERE name = @p2 AND id IN (@p3) AND id > @p1",
			arguments: []emitkit.Argument{{Parameter: 0}, {Parameter: 1}, {Parameter: 2, Expand: true}},
			lists: func(t *testing.T, sql string) []emitkit.ListMark {
				return []emitkit.ListMark{{Range: rangeOf(t, sql, "(@p3)"), Operator: rangeOf(t, sql, "IN "), Parameter: 2, First: 3}}
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			rendered, err := emitkit.Render(testCase.template, &testCase.renderer)
			require.NoError(t, err)
			require.Equal(t, emitkit.Rendered{
				SQL:       testCase.sql,
				Arguments: testCase.arguments,
				Marks:     emitkit.Marks{Lists: testCase.lists(t, testCase.sql)},
			}, rendered)
		})
	}
}

func TestRenderColumns(t *testing.T) {
	t.Parallel()

	qualifiedStar := template(
		text("SELECT "),
		star("u.*", sqlmodel.StarColumn{Column: 0, Qualifier: "u", Name: "id"}, sqlmodel.StarColumn{Column: 1, Qualifier: "u", Name: "tags"}),
		text(" FROM users u"),
	)
	withWraps := func(quote dialect.IdentifierQuote, wraps map[int]emitkit.ColumnWrap) emitkit.Renderer {
		configured := renderer(postgresPlaceholders, postgresPlaceholders.Execution)
		configured.Quote = quote
		configured.WrapColumn = wrapping(wraps)
		return configured
	}
	keepingStars := func(configured emitkit.Renderer) emitkit.Renderer {
		configured.KeepStars = true
		return configured
	}

	testCases := []struct {
		name      string
		template  sqlmodel.SQLTemplate
		renderer  emitkit.Renderer
		sql       string
		arguments []emitkit.Argument
		wrapped   map[int]emitkit.ColumnWrapKind
	}{
		{
			name:     "a star covering a wrapped column expands into quoted references",
			template: qualifiedStar,
			renderer: withWraps(doubleQuote, map[int]emitkit.ColumnWrap{1: toJSON}),
			sql:      `SELECT "u"."id", to_json("u"."tags") AS "tags" FROM users u`,
			wrapped:  map[int]emitkit.ColumnWrapKind{1: emitkit.ColumnWrapJSON},
		},
		{
			name:     "a star covering no wrapped column expands by default",
			template: qualifiedStar,
			renderer: withWraps(doubleQuote, map[int]emitkit.ColumnWrap{2: toJSON}),
			sql:      `SELECT "u"."id", "u"."tags" FROM users u`,
		},
		{
			name:     "a renderer keeping stars writes one covering no wrapped column as it is",
			template: qualifiedStar,
			renderer: keepingStars(withWraps(doubleQuote, map[int]emitkit.ColumnWrap{2: toJSON})),
			sql:      "SELECT u.* FROM users u",
		},
		{
			name:     "a renderer keeping stars still expands one covering a wrapped column",
			template: qualifiedStar,
			renderer: keepingStars(withWraps(doubleQuote, map[int]emitkit.ColumnWrap{1: toJSON})),
			sql:      `SELECT "u"."id", to_json("u"."tags") AS "tags" FROM users u`,
			wrapped:  map[int]emitkit.ColumnWrapKind{1: emitkit.ColumnWrapJSON},
		},
		{
			name: "a star written as it is stays so unless a column of it is wrapped",
			template: template(
				text("SELECT "),
				sqlmodel.SQLSegment{Kind: sqlmodel.SegmentStar, Text: "*", AsWritten: true, Star: []sqlmodel.StarColumn{{Column: 0, Qualifier: "sub", Name: "count"}}},
				text(" FROM (SELECT count(*) FROM t) sub"),
			),
			renderer: withWraps(doubleQuote, nil),
			sql:      "SELECT * FROM (SELECT count(*) FROM t) sub",
		},
		{
			name: "a star with a column of no name stays as written",
			template: template(
				text("SELECT "),
				star("*", sqlmodel.StarColumn{Column: 0, Name: "id"}, sqlmodel.StarColumn{Column: 1, Name: ""}),
				text(" FROM t"),
			),
			renderer: withWraps(doubleQuote, nil),
			sql:      "SELECT * FROM t",
		},
		{
			name: "an unqualified star expands with the dialect's quote",
			template: template(
				text("SELECT "),
				star("*", sqlmodel.StarColumn{Column: 0, Name: "id"}, sqlmodel.StarColumn{Column: 1, Name: "ta`gs"}),
				text(" FROM t"),
			),
			renderer: withWraps(backtick, map[int]emitkit.ColumnWrap{1: castToText}),
			sql:      "SELECT `id`, CAST(`ta``gs` AS TEXT) AS `tags` FROM t",
			wrapped:  map[int]emitkit.ColumnWrapKind{1: emitkit.ColumnWrapText},
		},
		{
			name: "a wrapped column without an alias keeps its output name",
			template: template(
				text("SELECT "), column(0, false, text("id")),
				text(", "), column(1, false, text("t.tags")),
				text(" FROM t"),
			),
			renderer: withWraps(doubleQuote, map[int]emitkit.ColumnWrap{1: toJSON}),
			sql:      `SELECT id, to_json(t.tags) AS "tags" FROM t`,
			wrapped:  map[int]emitkit.ColumnWrapKind{1: emitkit.ColumnWrapJSON},
		},
		{
			name:     "a wrapped column with an alias keeps the alias it is written with",
			template: template(text("SELECT "), column(1, true, text("t.labels")), text(" AS tags FROM t")),
			renderer: withWraps(doubleQuote, map[int]emitkit.ColumnWrap{1: toJSON}),
			sql:      "SELECT to_json(t.labels) AS tags FROM t",
			wrapped:  map[int]emitkit.ColumnWrapKind{1: emitkit.ColumnWrapJSON},
		},
		{
			name:      "a wrapped column's parameters are bound",
			template:  template(text("SELECT "), column(2, false, text("count(*) + "), parameter(0)), text(" FROM t")),
			renderer:  withWraps(doubleQuote, map[int]emitkit.ColumnWrap{2: castToText}),
			sql:       `SELECT CAST(count(*) + $1 AS TEXT) AS "total" FROM t`,
			arguments: []emitkit.Argument{{Parameter: 0}},
			wrapped:   map[int]emitkit.ColumnWrapKind{2: emitkit.ColumnWrapText},
		},
		{
			name:     "columns are written as they are without a wrap function",
			template: template(text("SELECT "), column(0, false, text("id")), text(", "), star("t.*")),
			renderer: renderer(postgresPlaceholders, postgresPlaceholders.Execution),
			sql:      "SELECT id, t.*",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			rendered, err := emitkit.Render(testCase.template, &testCase.renderer)
			require.NoError(t, err)
			require.Equal(t, emitkit.Rendered{
				SQL:       testCase.sql,
				Arguments: testCase.arguments,
				Marks:     emitkit.Marks{Wrapped: testCase.wrapped},
			}, rendered)
		})
	}
}

func TestRenderClauseMarks(t *testing.T) {
	t.Parallel()

	sortable := template(
		text("SELECT id FROM t WHERE a = "), parameter(1), text(" "),
		clause(sqlmodel.SegmentOrderBy, true, text("ORDER BY "), parameter(0)),
		text(" LIMIT 5"),
	)
	omitting := func(kinds ...sqlmodel.SQLSegmentKind) emitkit.Renderer {
		configured := renderer(postgresPlaceholders, postgresPlaceholders.Execution)
		configured.Omit = emitkit.NewSegmentSet(kinds...)
		return configured
	}

	testCases := []struct {
		name      string
		template  sqlmodel.SQLTemplate
		renderer  emitkit.Renderer
		sql       string
		arguments []emitkit.Argument
		marks     func(t *testing.T, sql string) emitkit.Marks
	}{
		{
			name: "a present filter, an ORDER BY place and pagination are marked before the terminator",
			template: template(
				text("SELECT id FROM t WHERE "),
				clause(sqlmodel.SegmentFilter, true, text("a = "), parameter(0), text(" OR b = "), parameter(1)),
				clause(sqlmodel.SegmentOrderBy, false),
				text(" "),
				clause(sqlmodel.SegmentPagination, true, text("LIMIT "), parameter(4)),
				text(";"),
			),
			renderer:  renderer(postgresPlaceholders, postgresPlaceholders.Execution),
			sql:       "SELECT id FROM t WHERE a = $1 OR b = $2 LIMIT $3;",
			arguments: []emitkit.Argument{{Parameter: 0}, {Parameter: 1}, {Parameter: 4}},
			marks: func(t *testing.T, sql string) emitkit.Marks {
				condition := rangeOf(t, sql, "a = $1 OR b = $2")
				return emitkit.Marks{
					Filter:     &emitkit.FilterMark{Condition: condition, InsertAt: condition.End, Present: true},
					OrderBy:    new(placeAfter(t, sql, "b = $2")),
					Pagination: new(rangeOf(t, sql, "LIMIT $3")),
				}
			},
		},
		{
			name: "a prefix is written first and the marks count it",
			template: template(
				text("SELECT id FROM t WHERE "),
				clause(sqlmodel.SegmentFilter, true, text("a = "), parameter(0)),
				clause(sqlmodel.SegmentOrderBy, false),
				text(";"),
			),
			renderer: func() emitkit.Renderer {
				configured := renderer(postgresPlaceholders, postgresPlaceholders.Execution)
				configured.Prefix = "-- ListB\n"
				return configured
			}(),
			sql:       "-- ListB\nSELECT id FROM t WHERE a = $1;",
			arguments: []emitkit.Argument{{Parameter: 0}},
			marks: func(t *testing.T, sql string) emitkit.Marks {
				condition := rangeOf(t, sql, "a = $1")
				return emitkit.Marks{
					Filter:  &emitkit.FilterMark{Condition: condition, InsertAt: condition.End, Present: true},
					OrderBy: new(placeAfter(t, sql, "a = $1")),
				}
			},
		},
		{
			name: "an absent filter marks where a WHERE clause goes",
			template: template(
				text("SELECT id FROM t"),
				clause(sqlmodel.SegmentFilter, false),
				text(" GROUP BY id"),
			),
			renderer: renderer(postgresPlaceholders, postgresPlaceholders.Execution),
			sql:      "SELECT id FROM t GROUP BY id",
			marks: func(t *testing.T, sql string) emitkit.Marks {
				place := placeAfter(t, sql, "FROM t")
				return emitkit.Marks{Filter: &emitkit.FilterMark{Condition: place, InsertAt: place.End}}
			},
		},
		{
			name:      "a written ORDER BY is covered and binds its parameters",
			template:  sortable,
			renderer:  renderer(postgresPlaceholders, postgresPlaceholders.Execution),
			sql:       "SELECT id FROM t WHERE a = $2 ORDER BY $1 LIMIT 5",
			arguments: []emitkit.Argument{{Parameter: 0}, {Parameter: 1}},
			marks: func(t *testing.T, sql string) emitkit.Marks {
				return emitkit.Marks{OrderBy: new(rangeOf(t, sql, "ORDER BY $1"))}
			},
		},
		{
			name:      "an omitted ORDER BY leaves its place and the rest numbered densely",
			template:  sortable,
			renderer:  omitting(sqlmodel.SegmentOrderBy),
			sql:       "SELECT id FROM t WHERE a = $1  LIMIT 5",
			arguments: []emitkit.Argument{{Parameter: 1}},
			marks: func(t *testing.T, sql string) emitkit.Marks {
				return emitkit.Marks{OrderBy: new(placeAfter(t, sql, "$1 "))}
			},
		},
		{
			name: "an omitted pagination leaves its place",
			template: template(
				text("SELECT id FROM t "),
				clause(sqlmodel.SegmentPagination, true, text("LIMIT "), parameter(4), text(" OFFSET "), parameter(0)),
			),
			renderer: omitting(sqlmodel.SegmentPagination),
			sql:      "SELECT id FROM t ",
			marks: func(t *testing.T, sql string) emitkit.Marks {
				return emitkit.Marks{Pagination: new(placeAfter(t, sql, "FROM t "))}
			},
		},
		{
			name: "a VALUES row is covered with its expressions kept",
			template: template(
				text("INSERT INTO t (a, b) VALUES "),
				clause(sqlmodel.SegmentValuesRow, false, text("("), parameter(0), text(", lower("), parameter(1), text("))")),
				text(" ON CONFLICT DO NOTHING"),
			),
			renderer:  renderer(mysqlPlaceholders, mysqlPlaceholders.Execution),
			sql:       "INSERT INTO t (a, b) VALUES (?, lower(?)) ON CONFLICT DO NOTHING",
			arguments: []emitkit.Argument{{Parameter: 0}, {Parameter: 1}},
			marks: func(t *testing.T, sql string) emitkit.Marks {
				return emitkit.Marks{Values: new(rangeOf(t, sql, "(?, lower(?))"))}
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			rendered, err := emitkit.Render(testCase.template, &testCase.renderer)
			require.NoError(t, err)
			require.Equal(t, emitkit.Rendered{
				SQL:       testCase.sql,
				Arguments: testCase.arguments,
				Marks:     testCase.marks(t, testCase.sql),
			}, rendered)
		})
	}
}

func TestCommentLineNamesAQueryInTheDialectsCommentSyntax(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		prefix   string
		expected string
	}{
		{name: "the dialect's line comment", prefix: "--", expected: "-- ListB\n"},
		{name: "another line comment", prefix: "#", expected: "# ListB\n"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			info := dialect.Facts{}
			info.CommentPrefix = testCase.prefix

			require.Equal(t, testCase.expected, emitkit.CommentLine(&info, "ListB"))
		})
	}
}

func TestRenderRefusesWhatItCannotWrite(t *testing.T) {
	t.Parallel()

	numbered := renderer(postgresPlaceholders, postgresPlaceholders.Execution)
	withoutQuery := numbered
	withoutQuery.Query = nil
	withoutForm := numbered
	withoutForm.Form = dialect.PlaceholderSigil{}
	unknownMode := numbered
	unknownMode.ListMode = emitkit.ListMode(7)
	omittingFilter := numbered
	omittingFilter.Omit = emitkit.NewSegmentSet(sqlmodel.SegmentFilter)
	arrayWithoutForms := numbered
	arrayWithoutForms.ListMode = emitkit.ListArray
	wrappingAll := numbered
	wrappingAll.WrapColumn = func(int) (emitkit.ColumnWrap, bool) { return toJSON, true }
	wrappingUnnamed := wrappingAll
	wrappingUnnamed.Query = &sqlmodel.AnalysedQuery{OutputColumns: []sqlmodel.OutputColumn{{}}}
	unnamedQuery := renderer(clickhousePlaceholders, clickhousePlaceholders.Execution)
	unnamedQuery.Query = &sqlmodel.AnalysedQuery{Parameters: []sqlmodel.QueryParameter{{}}}

	testCases := []struct {
		renderer *emitkit.Renderer
		name     string
		message  string
		template sqlmodel.SQLTemplate
	}{
		{name: "no renderer", message: "there is no renderer", template: repeatedParameters()},
		{name: "no query", renderer: &withoutQuery, message: "names no query", template: repeatedParameters()},
		{name: "no form", renderer: &withoutForm, message: "no placeholder form", template: repeatedParameters()},
		{name: "an unknown list mode", renderer: &unknownMode, message: "list mode 7 is unknown", template: repeatedParameters()},
		{name: "omitting a filter", renderer: &omittingFilter, message: "only ORDER BY and pagination", template: repeatedParameters()},
		{name: "a parameter out of range", renderer: &numbered, message: "parameter 9 is out of range; the query has 5", template: template(parameter(9))},
		{name: "a column out of range", renderer: &numbered, message: "output column 5 is out of range; the query has 3", template: template(column(5, false))},
		{
			name:     "a star column out of range",
			renderer: &wrappingAll,
			message:  "output column 8 is out of range",
			template: template(star("*", sqlmodel.StarColumn{Column: 8, Name: "x"})),
		},
		{
			name:     "a star column without a name to expand to",
			renderer: &wrappingAll,
			message:  "the star column for output column 1 has no name",
			template: template(star("*", sqlmodel.StarColumn{Column: 0, Name: "id"}, sqlmodel.StarColumn{Column: 1})),
		},
		{
			name:     "a parameter written alone and as a list",
			renderer: &numbered,
			message:  "parameter 2 is written both as a list and alone",
			template: template(list(2, "IN ", false), text(" OR id = "), parameter(2)),
		},
		{
			name:     "a typed form for a placeholder without a type",
			renderer: new(renderer(clickhousePlaceholders, clickhousePlaceholders.Execution)),
			message:  `parameter "id" is written in a typed form but its placeholder carries no type`,
			template: template(parameter(0)),
		},
		{name: "a named form for a parameter without a name", renderer: &unnamedQuery, message: "parameter 0 has no name", template: template(typed(0, "String"))},
		{name: "an array list without the dialect's form", renderer: &arrayWithoutForms, message: "no array form to write NOT IN with", template: template(list(2, "NOT IN ", true))},
		{
			name:     "two filters",
			renderer: &numbered,
			message:  "more than one filter segment",
			template: template(clause(sqlmodel.SegmentFilter, false), clause(sqlmodel.SegmentFilter, true, text("a"))),
		},
		{
			name:     "an absent ORDER BY with children",
			renderer: &numbered,
			message:  "an absent order-by segment has children",
			template: template(clause(sqlmodel.SegmentOrderBy, false, text("ORDER BY id"))),
		},
		{
			name:     "text with children",
			renderer: &numbered,
			message:  "a text segment cannot hold children",
			template: template(sqlmodel.SQLSegment{Kind: sqlmodel.SegmentText, Children: []sqlmodel.SQLSegment{text("x")}}),
		},
		{
			name:     "a wrapped column without a name to keep",
			renderer: &wrappingUnnamed,
			message:  "output column 0 is wrapped but has no name to keep",
			template: template(column(0, false, text("x"))),
		},
		{
			name:     "a wrapped star column without a name to keep",
			renderer: &wrappingUnnamed,
			message:  "output column 0 is wrapped but has no name to keep",
			template: template(star("*", sqlmodel.StarColumn{Column: 0, Name: "x"})),
		},
		{name: "an unknown kind", renderer: &numbered, message: "a segment of kind SQLSegmentKind(42) cannot be rendered", template: template(sqlmodel.SQLSegment{Kind: 42})},
		{
			name:     "a failure inside a clause",
			renderer: &numbered,
			message:  "parameter 6 is out of range",
			template: template(clause(sqlmodel.SegmentFilter, true, parameter(6))),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			rendered, err := emitkit.Render(testCase.template, testCase.renderer)
			require.ErrorContains(t, err, "emitkit: cannot render the SQL template: ")
			require.ErrorContains(t, err, testCase.message)
			require.Equal(t, emitkit.Rendered{}, rendered)
		})
	}
}

func TestRenderBindsInTheTemplatesArgumentOrder(t *testing.T) {
	t.Parallel()

	templates := []sqlmodel.SQLTemplate{
		repeatedParameters(),
		template(text("WHERE b = "), parameter(1), text(" AND a "), list(2, "IN ", false), text(" AND c = "), parameter(0), text(" AND d "), list(3, "IN ", false)),
		template(
			clause(sqlmodel.SegmentFilter, true, parameter(4), text(" = "), parameter(1)),
			clause(sqlmodel.SegmentOrderBy, true, parameter(0)),
			clause(sqlmodel.SegmentPagination, true, parameter(1)),
		),
	}
	forms := []dialect.PlaceholderSigil{
		postgresPlaceholders.Execution,
		sqlitePlaceholders.Execution,
		mysqlPlaceholders.Execution,
		martianPlaceholders.Execution,
	}

	for _, sqlTemplate := range templates {
		for _, form := range forms {
			rendered, err := emitkit.Render(sqlTemplate, new(renderer(postgresPlaceholders, form)))
			require.NoError(t, err)
			bound := make([]int, 0, len(rendered.Arguments))
			for _, argument := range rendered.Arguments {
				bound = append(bound, argument.Parameter)
			}
			require.Equal(t, emitkit.ArgumentOrder(sqlTemplate, form), bound, "form %q", form.Prefix)
		}
	}
}

func TestSegmentSet(t *testing.T) {
	t.Parallel()

	set := emitkit.NewSegmentSet(sqlmodel.SegmentOrderBy, sqlmodel.SegmentPagination)

	require.True(t, set.Has(sqlmodel.SegmentOrderBy))
	require.True(t, set.Has(sqlmodel.SegmentPagination))
	require.False(t, set.Has(sqlmodel.SegmentFilter))
	require.False(t, emitkit.NewSegmentSet().Has(sqlmodel.SegmentText))
}
