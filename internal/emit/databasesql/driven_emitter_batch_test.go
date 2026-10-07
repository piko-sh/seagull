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

package databasesql

import (
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/clickhouse"
	"seagull.sh/seagull/internal/dialect/mysql"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/dialect/sqlite"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

func templateText(value string) sqlmodel.SQLSegment {
	return sqlmodel.SQLSegment{Kind: sqlmodel.SegmentText, Text: value}
}

func templateParameter(index int, typeText string) sqlmodel.SQLSegment {
	return sqlmodel.SQLSegment{Kind: sqlmodel.SegmentParameter, Parameter: index, TypeText: typeText}
}

func templateRow(children ...sqlmodel.SQLSegment) sqlmodel.SQLSegment {
	return sqlmodel.SQLSegment{Kind: sqlmodel.SegmentValuesRow, Children: children}
}

func birdsParameters() []sqlmodel.QueryParameter {
	return []sqlmodel.QueryParameter{
		{Number: 1, Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}},
		{Number: 2, Name: "name", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}},
	}
}

func birdsBatch(t *testing.T, info *dialect.Facts, command sqlmodel.QueryCommand, segments ...sqlmodel.SQLSegment) *sqlmodel.AnalysedQuery {
	t.Helper()
	return batchOf(t, info, command, birdsParameters(), segments...)
}

func batchOf(t *testing.T, info *dialect.Facts, command sqlmodel.QueryCommand, parameters []sqlmodel.QueryParameter, segments ...sqlmodel.SQLSegment) *sqlmodel.AnalysedQuery {
	t.Helper()
	query := &sqlmodel.AnalysedQuery{
		Name:       "InsertBirds",
		Command:    command,
		Filename:   "birds.sql",
		Parameters: parameters,
		Template:   sqlmodel.SQLTemplate{Segments: segments},
	}
	rendered, err := emitkit.Render(query.Template, &emitkit.Renderer{
		Query: query, Form: info.Placeholders.Execution, Placeholders: info.Placeholders, Quote: info.IdentifierQuote, Lists: info.Lists,
	})
	require.NoError(t, err)
	query.SQL = rendered.SQL
	return query
}

func birdsUpsert(t *testing.T, info *dialect.Facts, idType, nameType string) *sqlmodel.AnalysedQuery {
	t.Helper()
	return birdsUpsertAs(t, info, sqlmodel.QueryCommandBatch, idType, nameType)
}

func birdsUpsertAs(t *testing.T, info *dialect.Facts, command sqlmodel.QueryCommand, idType, nameType string) *sqlmodel.AnalysedQuery {
	t.Helper()
	return birdsBatch(t, info, command,
		templateText("INSERT INTO birds (id, name, rank, note) VALUES "),
		templateRow(templateText("("), templateParameter(0, idType), templateText(", lower("), templateParameter(1, nameType),
			templateText("), "), templateParameter(0, idType), templateText(" + 1, '$9 ? {x:String}')")),
		templateText(" ON CONFLICT (id) DO UPDATE SET name = excluded.name"),
	)
}

func sqlConstant(t *testing.T, source string) string {
	t.Helper()
	match := regexp.MustCompile(`const (\w+) = `).FindStringSubmatch(source)
	require.NotNil(t, match, "no SQL constant in:\n%s", source)
	return match[1]
}

func emitBatch(t *testing.T, info dialect.Facts, query *sqlmodel.AnalysedQuery) string {
	t.Helper()
	files, err := emitQueriesForDialect(t.Context(), New(), info, "db", []*sqlmodel.AnalysedQuery{query}, defaultMappings())
	require.NoError(t, err)
	source := findGeneratedFile(t, files, "birds.sql.go")
	_, err = parser.ParseFile(token.NewFileSet(), "birds.sql.go", source, parser.AllErrors)
	require.NoError(t, err, "generated code must be valid Go:\n%s", source)
	require.NotContains(t, fileNames(files), "batch_helpers.go", "a batch needs no run-time SQL scanner")
	return source
}

func TestCopyFromRepeatsTheValuesRowInEachPlaceholderForm(t *testing.T) {
	t.Parallel()

	postgresInfo, sqliteInfo, mysqlInfo, clickHouseInfo := dialect.Wrap(postgres.New()).Info(), dialect.Wrap(sqlite.New()).Info(), dialect.Wrap(mysql.New()).Info(), dialect.Wrap(clickhouse.New()).Info()

	testCases := []struct {
		info  *dialect.Facts
		query func(t *testing.T) *sqlmodel.AnalysedQuery
		name  string
		wants []string
	}{
		{
			name: "a numbered form numbers each row's placeholders on",
			info: &postgresInfo,
			query: func(t *testing.T) *sqlmodel.AnalysedQuery {
				return birdsUpsertAs(t, &postgresInfo, sqlmodel.QueryCommandCopyFrom, "", "")
			},
			wants: []string{
				`values.Write(strconv.AppendInt(digits[:0], int64(i*2+1), 10))`,
				"args = append(args, item.ID, item.Name)",
				"for offset := 0; offset < len(params); offset += 32767 {",
				"queries.writer.ExecContext(ctx, values.String(), args...)",
			},
		},
		{
			name: "the numbered form of SQLite",
			info: &sqliteInfo,
			query: func(t *testing.T) *sqlmodel.AnalysedQuery {
				return birdsUpsertAs(t, &sqliteInfo, sqlmodel.QueryCommandCopyFrom, "", "")
			},
			wants: []string{
				`values.WriteString("(?")`,
				"for offset := 0; offset < len(params); offset += 499 {",
			},
		},
		{
			name: "an anonymous form writes the row as it is and binds every placeholder in turn",
			info: &mysqlInfo,
			query: func(t *testing.T) *sqlmodel.AnalysedQuery {
				return birdsUpsertAs(t, &mysqlInfo, sqlmodel.QueryCommandCopyFrom, "", "")
			},
			wants: []string{
				`values.WriteString("(?, lower(?), ? + 1, '$9 ? {x:String}')")`,
				"args = append(args, item.ID, item.Name, item.ID)",
				"for offset := 0; offset < len(params); offset += 21845 {",
			},
		},
		{
			name: "a named form names each row's placeholders and values apart",
			info: &clickHouseInfo,
			query: func(t *testing.T) *sqlmodel.AnalysedQuery {
				return birdsUpsertAs(t, &clickHouseInfo, sqlmodel.QueryCommandCopyFrom, "UInt64", "String")
			},
			wants: []string{
				"row := strconv.Itoa(i)",
				`values.WriteString(":String}), {p_id_")`,
				`args = append(args, clickhouse.Named("p_id_"+row, sglClickHouseShapedValue(item.ID, sglClickHouseShapes[0])), clickhouse.Named("p_name_"+row, sglClickHouseShapedValue(item.Name, sglClickHouseShapes[1])))`,
				"for offset := 0; offset < len(params); offset += 512 {",
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			source := emitBatch(t, *testCase.info, testCase.query(t))
			constant := sqlConstant(t, source)
			for _, want := range testCase.wants {
				require.Contains(t, source, strings.ReplaceAll(want, "CONSTANT", constant))
			}
			require.Contains(t, source, "if len(params) == 0 {")
			require.Less(t, strings.Index(source, "if i > 0 {"), strings.Index(source, `values.WriteString(", ")`),
				"the separator is written only between rows")
		})
	}
}

func TestBatchKeepsTheSQLAroundTheValuesRow(t *testing.T) {
	t.Parallel()

	info := dialect.Wrap(postgres.New()).Info()
	query := birdsUpsert(t, &info, "", "")
	start := strings.Index(query.SQL, "($1")
	end := strings.Index(query.SQL, " ON CONFLICT")

	source := emitBatch(t, info, query)
	constant := sqlConstant(t, source)

	require.Contains(t, source, "values.WriteString("+constant+"[:"+strconv.Itoa(start)+"])")
	require.Contains(t, source, "values.WriteString("+constant+"["+strconv.Itoa(end)+":])")
}

func TestBatchWithoutSQLAfterTheValuesRowEndsWithTheRows(t *testing.T) {
	t.Parallel()

	info := dialect.Wrap(postgres.New()).Info()
	query := birdsBatch(t, &info, sqlmodel.QueryCommandCopyFrom,
		templateText("INSERT INTO birds (id, name) VALUES "),
		templateRow(templateText("("), templateParameter(0, ""), templateText(", "), templateParameter(1, ""), templateText(")")),
	)

	source := emitBatch(t, info, query)

	require.Contains(t, source, "queries.writer.ExecContext(ctx, values.String(), args...)")
	require.Contains(t, source, "func (queries *Queries) InsertBirds(ctx context.Context, params []InsertBirdsParams) error {")
}

func TestBatchRunsAStatementWithAClauseAfterItsValuesRowOncePerRow(t *testing.T) {
	t.Parallel()

	info := dialect.Wrap(postgres.New()).Info()
	plain := func(command sqlmodel.QueryCommand, after string) *sqlmodel.AnalysedQuery {
		segments := []sqlmodel.SQLSegment{
			templateText("INSERT INTO birds (id, name) VALUES "),
			templateRow(templateText("("), templateParameter(0, ""), templateText(", "), templateParameter(1, ""), templateText(")")),
		}
		if after != "" {
			segments = append(segments, templateText(after))
		}
		return birdsBatch(t, &info, command, segments...)
	}
	testCases := []struct {
		name  string
		query *sqlmodel.AnalysedQuery
		step  string
	}{
		{name: "a batch with an upsert clause", query: birdsUpsert(t, &info, "", ""), step: "offset += 1"},
		{name: "a batch with a returning clause", query: plain(sqlmodel.QueryCommandBatch, " RETURNING id;"), step: "offset += 1"},
		{name: "a batch ending with a comment and the terminator", query: plain(sqlmodel.QueryCommandBatch, " -- rows\n;"), step: "offset += 32767"},
		{name: "a copyfrom with an upsert clause", query: plain(sqlmodel.QueryCommandCopyFrom, " ON CONFLICT DO NOTHING;"), step: "offset += 32767"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			source := emitBatch(t, info, testCase.query)

			require.Contains(t, source, testCase.step)
		})
	}
}

func TestBatchRefusesARowBindingMoreThanAStatementAllows(t *testing.T) {
	t.Parallel()

	info := dialect.Wrap(sqlite.New()).Info()
	count := info.MaxParameters + 1
	children := []sqlmodel.SQLSegment{templateText("(")}
	parameters := make([]sqlmodel.QueryParameter, count)
	for index := range count {
		if index > 0 {
			children = append(children, templateText(", "))
		}
		children = append(children, templateParameter(index, ""))
		parameters[index] = sqlmodel.QueryParameter{Number: index + 1, Name: "field" + strconv.Itoa(index), SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}}
	}
	children = append(children, templateText(")"))
	query := batchOf(t, &info, sqlmodel.QueryCommandBatch, parameters, templateText("INSERT INTO wide VALUES "), templateRow(children...))

	files, err := emitQueriesForDialect(t.Context(), New(), info, "db", []*sqlmodel.AnalysedQuery{query}, defaultMappings())

	require.ErrorContains(t, err, "query InsertBirds binds 1000 arguments in each row, more than the 999 one statement binds")
	require.Nil(t, files)
}
