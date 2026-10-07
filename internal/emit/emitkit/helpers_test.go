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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/martian"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

var (
	dollarNumbered       = dialect.PlaceholderSigil{Prefix: "$", Kind: dialect.PlaceholderNumbered}
	postgresPlaceholders = dialect.Placeholders{
		Sigils:    []dialect.PlaceholderSigil{dollarNumbered, {Prefix: ":", Kind: dialect.PlaceholderNamed}},
		Execution: dollarNumbered,
	}
	sqlitePlaceholders = dialect.Placeholders{
		Sigils:    []dialect.PlaceholderSigil{{Prefix: "?", Kind: dialect.PlaceholderNumbered}, {Prefix: ":", Kind: dialect.PlaceholderNamed}},
		Execution: dialect.PlaceholderSigil{Prefix: "?", Kind: dialect.PlaceholderNumbered},
	}
	mysqlPlaceholders = dialect.Placeholders{
		Sigils:    []dialect.PlaceholderSigil{{Prefix: "?", Kind: dialect.PlaceholderAnonymous}, {Prefix: ":", Kind: dialect.PlaceholderNamed}},
		Execution: dialect.PlaceholderSigil{Prefix: "?", Kind: dialect.PlaceholderAnonymous},
	}
	clickhouseBraced       = dialect.PlaceholderSigil{Prefix: "{", Suffix: "}", TypeSeparator: ":", Kind: dialect.PlaceholderNamed}
	clickhousePlaceholders = dialect.Placeholders{
		Sigils:     []dialect.PlaceholderSigil{clickhouseBraced},
		Execution:  clickhouseBraced,
		Positional: dollarNumbered,
		NamePrefix: "p_",
	}
	martianPlaceholders = dialect.Wrap(martian.New()).Info().Placeholders
	arrayLists          = dialect.ListSyntax{
		EmptyMembership: "IN (SELECT NULL WHERE FALSE)",
		EmptyExclusion:  "NOT IN (SELECT NULL WHERE FALSE)",
		ArrayMembership: dialect.Affix{Before: "= ANY(", After: ")"},
		ArrayExclusion:  dialect.Affix{Before: "<> ALL(", After: ")"},
	}
	doubleQuote = dialect.IdentifierQuote{Open: `"`, Close: `"`}
	backtick    = dialect.IdentifierQuote{Open: "`", Close: "`"}
	toJSON      = emitkit.ColumnWrap{Affix: dialect.Affix{Before: "to_json(", After: ")"}, Kind: emitkit.ColumnWrapJSON}
	castToText  = emitkit.ColumnWrap{Affix: dialect.Affix{Before: "CAST(", After: " AS TEXT)"}, Kind: emitkit.ColumnWrapText}
)

type engineNameResolver map[string]sqlmodel.GoType

func (resolver engineNameResolver) Resolve(sqlType sqlmodel.SQLType, nullable bool) sqlmodel.GoType {
	if goType, found := resolver[sqlType.EngineName]; found {
		if nullable && goType.Composite == nil {
			goType.Name = "*" + goType.Name
		}
		return goType
	}
	if nullable {
		return sqlmodel.GoType{Name: "*string"}
	}
	return sqlmodel.GoType{Name: "string"}
}

func testQuery() *sqlmodel.AnalysedQuery {
	return &sqlmodel.AnalysedQuery{
		Parameters: []sqlmodel.QueryParameter{
			{Name: "id"},
			{Name: "name"},
			{Name: "ids", IsSlice: true},
			{Name: "kinds", IsSlice: true},
			{Name: "limit"},
		},
		OutputColumns: []sqlmodel.OutputColumn{{Name: "id"}, {Name: "tags"}, {Name: "total"}},
	}
}

func renderer(placeholders dialect.Placeholders, form dialect.PlaceholderSigil) emitkit.Renderer {
	return emitkit.Renderer{Query: testQuery(), Placeholders: placeholders, Form: form, Quote: doubleQuote}
}

func wrapping(wraps map[int]emitkit.ColumnWrap) func(column int) (emitkit.ColumnWrap, bool) {
	return func(column int) (emitkit.ColumnWrap, bool) {
		wrap, found := wraps[column]
		return wrap, found
	}
}

func text(value string) sqlmodel.SQLSegment {
	return sqlmodel.SQLSegment{Kind: sqlmodel.SegmentText, Text: value}
}

func parameter(index int) sqlmodel.SQLSegment {
	return sqlmodel.SQLSegment{Kind: sqlmodel.SegmentParameter, Parameter: index}
}

func typed(index int, typeText string) sqlmodel.SQLSegment {
	return sqlmodel.SQLSegment{Kind: sqlmodel.SegmentParameter, Parameter: index, TypeText: typeText}
}

func list(index int, operator string, negated bool) sqlmodel.SQLSegment {
	return sqlmodel.SQLSegment{Kind: sqlmodel.SegmentList, Parameter: index, Text: operator, Negated: negated}
}

func typedList(index int, typeText string, operator string, negated bool) sqlmodel.SQLSegment {
	segment := list(index, operator, negated)
	segment.TypeText = typeText
	return segment
}

func column(index int, aliased bool, children ...sqlmodel.SQLSegment) sqlmodel.SQLSegment {
	return sqlmodel.SQLSegment{Kind: sqlmodel.SegmentColumn, Column: index, Aliased: aliased, Children: children}
}

func star(written string, columns ...sqlmodel.StarColumn) sqlmodel.SQLSegment {
	return sqlmodel.SQLSegment{Kind: sqlmodel.SegmentStar, Text: written, Star: columns}
}

func clause(kind sqlmodel.SQLSegmentKind, present bool, children ...sqlmodel.SQLSegment) sqlmodel.SQLSegment {
	return sqlmodel.SQLSegment{Kind: kind, Present: present, Children: children}
}

func template(segments ...sqlmodel.SQLSegment) sqlmodel.SQLTemplate {
	return sqlmodel.SQLTemplate{Segments: segments}
}

func rangeOf(t *testing.T, sql string, part string) emitkit.Range {
	t.Helper()

	start := strings.Index(sql, part)
	require.GreaterOrEqual(t, start, 0, "the rendered SQL %q does not hold %q", sql, part)
	return emitkit.Range{Start: start, End: start + len(part)}
}

func placeBefore(t *testing.T, sql string, part string) emitkit.Range {
	t.Helper()

	covered := rangeOf(t, sql, part)
	return emitkit.Range{Start: covered.Start, End: covered.Start}
}

func placeAfter(t *testing.T, sql string, part string) emitkit.Range {
	t.Helper()

	covered := rangeOf(t, sql, part)
	return emitkit.Range{Start: covered.End, End: covered.End}
}

func sglStyle() *emit.Style {
	return &emit.Style{HelperPrefix: "sgl", ErrorPrefix: "sgl", SQLConstantSuffix: "SQL"}
}

func defaultReservation() emit.Reservation {
	return emitkit.DefaultReservation(&dialect.Facts{}, sglStyle())
}

func plannedQuery(t *testing.T, query *sqlmodel.AnalysedQuery, style emitkit.NameStyle) emit.QueryNaming {
	t.Helper()

	naming, problems := emitkit.PlanPackage(requestOf(nil, query), defaultReservation(), style)
	require.Empty(t, problems)
	return naming.Queries[0]
}

func textType() sqlmodel.SQLType {
	return sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")
}

func integerType() sqlmodel.SQLType {
	return sqlmodel.NewIntegerType("integer", 32, false)
}

func parameterNamed(name string) sqlmodel.QueryParameter {
	return sqlmodel.QueryParameter{Name: name, SQLType: integerType()}
}

func columnNamed(name string) sqlmodel.OutputColumn {
	return sqlmodel.OutputColumn{Name: name, SQLType: textType()}
}

func embeddedColumn(name, group string) sqlmodel.OutputColumn {
	column := columnNamed(name)
	column.IsEmbedded = true
	column.EmbedTable = group
	return column
}

func queryOf(name string, command sqlmodel.QueryCommand, parameters []sqlmodel.QueryParameter, columns []sqlmodel.OutputColumn) *sqlmodel.AnalysedQuery {
	return &sqlmodel.AnalysedQuery{
		Name:          name,
		Filename:      "queries.sql",
		Command:       command,
		Parameters:    parameters,
		OutputColumns: columns,
	}
}

func catalogueOf(tables ...*sqlmodel.Table) *sqlmodel.Catalogue {
	schema := &sqlmodel.Schema{Name: "public", Tables: map[string]*sqlmodel.Table{}, Enums: map[string]*sqlmodel.Enum{}}
	for _, table := range tables {
		schema.Tables[table.Name] = table
	}
	return &sqlmodel.Catalogue{
		Schemas:       map[string]*sqlmodel.Schema{"public": schema},
		DefaultSchema: "public",
		SearchPath:    []string{"public"},
	}
}

func tableOf(name string, columns ...sqlmodel.Column) *sqlmodel.Table {
	return &sqlmodel.Table{Name: name, Schema: "public", Columns: columns}
}

func namesEnumsAndStructs(sqlType sqlmodel.SQLType) bool {
	return sqlType.Category == sqlmodel.TypeCategoryEnum || sqlType.Category == sqlmodel.TypeCategoryStruct
}

func requestOf(catalogue *sqlmodel.Catalogue, queries ...*sqlmodel.AnalysedQuery) *emit.Request {
	return &emit.Request{
		Catalogue: catalogue,
		Types:     engineNameResolver{},
		Package:   "generated",
		Style:     *sglStyle(),
		Queries:   queries,
	}
}
