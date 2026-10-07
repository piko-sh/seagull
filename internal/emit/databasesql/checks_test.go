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
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/clickhouse"
	"seagull.sh/seagull/internal/dialect/duckdb"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/sqlmodel"
)

func checkRequest(info dialect.Facts, queries ...*sqlmodel.AnalysedQuery) *emit.Request {
	request := testRequest("db", queries, defaultMappings())
	request.Dialect = info
	return request
}

func TestCheckRefusesPreparingForADriverThatCannot(t *testing.T) {
	t.Parallel()

	refusal := emit.Problem{
		Code: diag.CodeEmitterUnsupported,
		Message: "WithPrepared(true) asks for prepared.go, but the github.com/ClickHouse/clickhouse-go/v2 driver behind " +
			"database/sql prepares only INSERT statements, so it cannot prepare the package's queries; leave WithPrepared out or pass false",
		Severity: diag.SeverityError,
	}
	testCases := []struct {
		emitter *Emitter
		name    string
		want    []emit.Problem
		info    dialect.Facts
	}{
		{name: "asked on a driver that prepares only inserts", emitter: New(WithPrepared(true)), info: dialect.Wrap(clickhouse.New()).Info(), want: []emit.Problem{refusal}},
		{name: "asked on that driver named", emitter: New(WithDriver(driverClickHouse), WithPrepared(true)), info: dialect.Wrap(postgres.New()).Info(), want: []emit.Problem{refusal}},
		{name: "left to the default on that driver", emitter: New(), info: dialect.Wrap(clickhouse.New()).Info(), want: nil},
		{name: "turned off on that driver", emitter: New(WithPrepared(false)), info: dialect.Wrap(clickhouse.New()).Info(), want: nil},
		{name: "asked on a driver that prepares", emitter: New(WithPrepared(true)), info: dialect.Wrap(postgres.New()).Info(), want: nil},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, testCase.emitter.Check(checkRequest(testCase.info)))
		})
	}
}

func TestCheckRefusesCommandsWhoseResultTheDriverDoesNotReport(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		want    string
		info    dialect.Facts
		command sqlmodel.QueryCommand
	}{
		{name: "execrows on clickhouse-go", info: dialect.Wrap(clickhouse.New()).Info(), command: sqlmodel.QueryCommandExecRows,
			want: "command: execrows returns the number of rows the statement changed, which the github.com/ClickHouse/clickhouse-go/v2 driver behind database/sql does not report; use command: exec"},
		{name: "execresult on clickhouse-go", info: dialect.Wrap(clickhouse.New()).Info(), command: sqlmodel.QueryCommandExecResult,
			want: "command: execresult returns the sql.Result counting the rows the statement changed, which the github.com/ClickHouse/clickhouse-go/v2 driver behind database/sql does not report; use command: exec"},
		{name: "exec on clickhouse-go", info: dialect.Wrap(clickhouse.New()).Info(), command: sqlmodel.QueryCommandExec},
		{name: "execrows on pgx", info: dialect.Wrap(postgres.New()).Info(), command: sqlmodel.QueryCommandExecRows},
		{name: "execresult on pgx", info: dialect.Wrap(postgres.New()).Info(), command: sqlmodel.QueryCommandExecResult},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			query := &sqlmodel.AnalysedQuery{Name: "Purge", Command: testCase.command, SQL: "ALTER TABLE events DELETE WHERE 1", Filename: "events.sql"}
			problems := New().Check(checkRequest(testCase.info, query))

			if testCase.want == "" {
				require.Empty(t, problems)
				return
			}
			require.Equal(t, []emit.Problem{{Query: query, Code: diag.CodeEmitterUnsupported, Message: testCase.want, Severity: diag.SeverityError}}, problems)
		})
	}
}

func TestCheckRefusesABatchItCannotRepeatPerRow(t *testing.T) {
	t.Parallel()

	info := dialect.Wrap(postgres.New()).Info()
	testCases := []struct {
		query *sqlmodel.AnalysedQuery
		name  string
		want  string
	}{
		{
			name: "a statement without a VALUES row",
			query: birdsBatch(t, &info, sqlmodel.QueryCommandBatch,
				templateText("INSERT INTO birds (id, name) SELECT "), templateParameter(0, ""), templateText(", "), templateParameter(1, "")),
			want: "command: batch repeats the statement's VALUES row for each row, so it needs an INSERT with exactly one VALUES row",
		},
		{
			name: "a parameter after the VALUES row",
			query: birdsBatch(t, &info, sqlmodel.QueryCommandCopyFrom,
				templateText("INSERT INTO birds (id) VALUES "), templateRow(templateText("("), templateParameter(0, ""), templateText(")")),
				templateText(" ON CONFLICT (id) DO UPDATE SET name = "), templateParameter(1, "")),
			want: "command: copyfrom binds parameter name outside its VALUES row",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			problems := New().Check(checkRequest(info, testCase.query))

			require.Len(t, problems, 1)
			require.Same(t, testCase.query, problems[0].Query)
			require.Equal(t, diag.CodeBatchShape, problems[0].Code)
			require.Equal(t, diag.SeverityError, problems[0].Severity)
			require.Contains(t, problems[0].Message, testCase.want)
		})
	}
}

func TestCheckRefusesABatchRowWiderThanOneStatement(t *testing.T) {
	t.Parallel()

	narrow := dialect.Wrap(postgres.New()).Info()
	narrow.MaxParameters = 1
	wide := dialect.Wrap(postgres.New()).Info()
	row := templateRow(templateText("("), templateParameter(0, ""), templateText(", "), templateParameter(1, ""), templateText(")"))

	testCases := []struct {
		info    dialect.Facts
		name    string
		command sqlmodel.QueryCommand
		want    string
	}{
		{name: "a batch within the limit", info: wide, command: sqlmodel.QueryCommandBatch},
		{name: "a batch past the limit", info: narrow, command: sqlmodel.QueryCommandBatch,
			want: "command: batch binds 2 variables in each VALUES row, more than the 1 one statement of the driver binds"},
		{name: "a copyfrom past the limit", info: narrow, command: sqlmodel.QueryCommandCopyFrom,
			want: "command: copyfrom binds 2 variables in each VALUES row, more than the 1 one statement of the driver binds"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			query := birdsBatch(t, &testCase.info, testCase.command, templateText("INSERT INTO birds (id, name) VALUES "), row)
			request := checkRequest(testCase.info, query)
			problems := New().Check(request)

			if testCase.want == "" {
				require.Empty(t, problems)
				return
			}
			require.Len(t, problems, 1)
			require.Same(t, query, problems[0].Query)
			require.Equal(t, diag.CodeBatchShape, problems[0].Code)
			require.Equal(t, diag.SeverityError, problems[0].Severity)
			require.Contains(t, problems[0].Message, testCase.want)

			files, err := New().Emit(t.Context(), request)
			require.ErrorContains(t, err, "query InsertBirds: Q060 "+testCase.want)
			require.Nil(t, files)
		})
	}
}

func TestEmitRefusesWhatCheckRefuses(t *testing.T) {
	t.Parallel()

	query := &sqlmodel.AnalysedQuery{Name: "Purge", Command: sqlmodel.QueryCommandExecRows, SQL: "ALTER TABLE events DELETE WHERE 1", Filename: "events.sql"}

	files, err := New().Emit(t.Context(), checkRequest(dialect.Wrap(clickhouse.New()).Info(), query))

	require.ErrorContains(t, err, "emitting queries for db")
	require.ErrorContains(t, err, "query Purge: Q059 command: execrows")
	require.ErrorIs(t, err, emit.ErrRefused)
	require.Nil(t, files)
}

func TestPreparesStatementsSaysWhereWithPreparedWorks(t *testing.T) {
	t.Parallel()

	clickHouse := dialect.Wrap(clickhouse.New()).Info()
	postgresInfo := dialect.Wrap(postgres.New()).Info()

	testCases := []struct {
		info   *dialect.Facts
		name   string
		driver string
		want   bool
	}{
		{name: "the dialect's driver prepares", info: &postgresInfo, want: true},
		{name: "the dialect's driver prepares only inserts", info: &clickHouse, want: false},
		{name: "a named driver that prepares", info: &clickHouse, driver: driverPgx, want: true},
		{name: "a named driver that prepares only inserts", info: &postgresInfo, driver: driverClickHouse, want: false},
		{name: "a driver the emitter does not know", info: &clickHouse, driver: "example.com/driver", want: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, PreparesStatements(testCase.info, testCase.driver))
		})
	}
}

func TestCheckRefusesTuplesTheDriverReadsWithTheirElementsLost(t *testing.T) {
	t.Parallel()

	integer := sqlmodel.SQLType{EngineName: "UInt8", Category: sqlmodel.TypeCategoryInteger, Width: 8, Unsigned: true}
	text := sqlmodel.SQLType{EngineName: "String", Category: sqlmodel.TypeCategoryText}
	pair := sqlmodel.SQLType{EngineName: "Tuple", Category: sqlmodel.TypeCategoryStruct, StructFields: []sqlmodel.StructField{
		{Name: "_1", SQLType: text, Positional: true}, {Name: "_2", SQLType: integer, Positional: true},
	}}
	namedOf := func(field sqlmodel.SQLType) sqlmodel.SQLType {
		return sqlmodel.SQLType{EngineName: "Tuple", Category: sqlmodel.TypeCategoryStruct, StructFields: []sqlmodel.StructField{
			{Name: "a", SQLType: integer}, {Name: "b", SQLType: field},
		}}
	}
	arrayOf := func(element sqlmodel.SQLType) sqlmodel.SQLType {
		return sqlmodel.SQLType{EngineName: "Array", Category: sqlmodel.TypeCategoryArray, ElementType: &element}
	}
	mapOf := func(value sqlmodel.SQLType) sqlmodel.SQLType {
		return sqlmodel.SQLType{EngineName: "Map", Category: sqlmodel.TypeCategoryMap, KeyType: &text, ElementType: &value}
	}
	testCases := []struct {
		name    string
		column  sqlmodel.SQLType
		info    dialect.Facts
		refused bool
	}{
		{name: "an array of unnamed tuples inside a named tuple", column: namedOf(arrayOf(pair)), info: dialect.Wrap(clickhouse.New()).Info(), refused: true},
		{name: "an unnamed tuple inside a named tuple", column: namedOf(pair), info: dialect.Wrap(clickhouse.New()).Info(), refused: true},
		{name: "an array of named tuples holding arrays of unnamed ones", column: arrayOf(namedOf(arrayOf(pair))), info: dialect.Wrap(clickhouse.New()).Info(), refused: true},
		{name: "a map to unnamed tuples inside a named tuple", column: namedOf(mapOf(pair)), info: dialect.Wrap(clickhouse.New()).Info()},
		{name: "an unnamed tuple", column: pair, info: dialect.Wrap(clickhouse.New()).Info()},
		{name: "an array of unnamed tuples", column: arrayOf(pair), info: dialect.Wrap(clickhouse.New()).Info()},
		{name: "a named tuple holding plain values", column: namedOf(text), info: dialect.Wrap(clickhouse.New()).Info()},
		{name: "an array of unnamed tuples inside a named tuple on another driver", column: namedOf(arrayOf(pair)), info: dialect.Wrap(postgres.New()).Info()},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			query := &sqlmodel.AnalysedQuery{Name: "Nests", Command: sqlmodel.QueryCommandMany, SQL: "SELECT nest FROM nests", Filename: "nests.sql",
				OutputColumns: []sqlmodel.OutputColumn{{Name: "nest", SQLType: testCase.column}}}
			problems := New().Check(checkRequest(testCase.info, query))

			if !testCase.refused {
				require.Empty(t, problems)
				return
			}
			require.Len(t, problems, 1)
			require.Same(t, query, problems[0].Query)
			require.Equal(t, diag.CodeEmitterUnsupported, problems[0].Code)
			require.Equal(t, diag.SeverityError, problems[0].Severity)
			require.Contains(t, problems[0].Message, `column "nest" holds a tuple whose elements have no names inside another tuple`)
		})
	}
}

func TestCheckRefusesAnArrayParameterOfACompositeTypeTheDriverCannotBind(t *testing.T) {
	t.Parallel()

	pair := sqlmodel.SQLType{EngineName: "pair", Category: sqlmodel.TypeCategoryComposite}
	solutions := sqlmodel.SQLType{EngineName: "solutions", Category: sqlmodel.TypeCategoryUnknown}
	arrayOf := func(element sqlmodel.SQLType) sqlmodel.SQLType {
		return sqlmodel.SQLType{EngineName: element.EngineName + "[]", Category: sqlmodel.TypeCategoryArray, ElementType: &element}
	}
	catalogue := &sqlmodel.Catalogue{DefaultSchema: "public", Schemas: map[string]*sqlmodel.Schema{"public": {
		Name:           "public",
		Tables:         map[string]*sqlmodel.Table{"solutions": {Name: "solutions", Schema: "public"}},
		CompositeTypes: map[string]*sqlmodel.CompositeType{"pair": {Name: "pair", Schema: "public"}},
	}}}
	testCases := []struct {
		name      string
		parameter sqlmodel.SQLType
		info      dialect.Facts
		want      string
	}{
		{name: "an array of a composite type", parameter: arrayOf(pair), info: dialect.Wrap(postgres.New()).Info(),
			want: "parameter pairs is an array of the composite type pair, which the github.com/jackc/pgx/v5 driver behind " +
				"database/sql cannot bind; pass each field as an array of its own and build the rows with unnest(...), or " +
				"override the parameter's Go type with a type whose value the driver can encode"},
		{name: "an array of a table's row type", parameter: arrayOf(arrayOf(solutions)), info: dialect.Wrap(postgres.New()).Info(),
			want: "parameter pairs is an array of the composite type solutions"},
		{name: "a composite value that is no array", parameter: pair, info: dialect.Wrap(postgres.New()).Info()},
		{name: "an array of a composite type on another driver", parameter: arrayOf(pair), info: dialect.Wrap(clickhouse.New()).Info()},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			query := &sqlmodel.AnalysedQuery{Name: "Pairs", Command: sqlmodel.QueryCommandExec, SQL: "UPDATE solutions SET pairs = $1",
				Filename: "pairs.sql", Parameters: []sqlmodel.QueryParameter{{Name: "pairs", Number: 1, SQLType: testCase.parameter}}}
			request := checkRequest(testCase.info, query)
			request.Catalogue = catalogue
			problems := New().Check(request)

			if testCase.want == "" {
				require.Empty(t, problems)
				return
			}
			require.Len(t, problems, 1)
			require.Same(t, query, problems[0].Query)
			require.Equal(t, diag.CodeEmitterUnsupported, problems[0].Code)
			require.Equal(t, diag.SeverityError, problems[0].Severity)
			require.Contains(t, problems[0].Message, testCase.want)
		})
	}
}

func TestCheckRefusesAParameterOfNullableElementsTheDriverCannotBind(t *testing.T) {
	t.Parallel()

	integer := sqlmodel.NewIntegerType("INTEGER", 32, false)
	nullableInteger := integer
	nullableInteger.Nullable = true
	listOf := func(element sqlmodel.SQLType) sqlmodel.SQLType {
		return sqlmodel.SQLType{EngineName: element.EngineName + "[]", Category: sqlmodel.TypeCategoryArray, ElementType: &element}
	}
	mapOf := func(value sqlmodel.SQLType) sqlmodel.SQLType {
		return sqlmodel.SQLType{EngineName: "MAP", Category: sqlmodel.TypeCategoryMap, KeyType: new(sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "VARCHAR")), ElementType: &value}
	}
	testCases := []struct {
		name      string
		parameter sqlmodel.SQLType
		slice     bool
		info      dialect.Facts
		refused   bool
	}{
		{name: "a list of elements that may be NULL on DuckDB", parameter: listOf(nullableInteger), info: dialect.Wrap(duckdb.New()).Info(), refused: true},
		{name: "a map of values that may be NULL on DuckDB", parameter: mapOf(nullableInteger), info: dialect.Wrap(duckdb.New()).Info(), refused: true},
		{name: "a list of lists of elements that may be NULL on DuckDB", parameter: listOf(listOf(nullableInteger)), info: dialect.Wrap(duckdb.New()).Info(), refused: true},
		{name: "a list of plain elements on DuckDB", parameter: listOf(integer), info: dialect.Wrap(duckdb.New()).Info()},
		{name: "a slice parameter on DuckDB", parameter: nullableInteger, slice: true, info: dialect.Wrap(duckdb.New()).Info()},
		{name: "an array of elements that may be NULL on PostgreSQL", parameter: listOf(nullableInteger), info: dialect.Wrap(postgres.New()).Info()},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			query := &sqlmodel.AnalysedQuery{Name: "SetScores", Command: sqlmodel.QueryCommandExec, SQL: "UPDATE readings SET scores = $1",
				Filename: "readings.sql", Parameters: []sqlmodel.QueryParameter{{Name: "scores", Number: 1, SQLType: testCase.parameter, IsSlice: testCase.slice}}}
			problems := New().Check(checkRequest(testCase.info, query))

			if !testCase.refused {
				require.Empty(t, problems)
				return
			}
			require.Len(t, problems, 1)
			require.Same(t, query, problems[0].Query)
			require.Equal(t, diag.CodeEmitterUnsupported, problems[0].Code)
			require.Equal(t, "parameter scores declares elements that may be NULL, which the github.com/duckdb/duckdb-go/v2 driver behind "+
				"database/sql cannot bind; declare them non-null with non_null_elements: true, or write the NULL elements in the SQL", problems[0].Message)
		})
	}
}

func TestBindsNullElementsSaysWhetherTheDriverBindsANullElement(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		info dialect.Facts
		want bool
	}{
		{name: "pgx behind database/sql", info: dialect.Wrap(postgres.New()).Info(), want: true},
		{name: "clickhouse-go behind database/sql", info: dialect.Wrap(clickhouse.New()).Info(), want: true},
		{name: "duckdb-go, which panics on one", info: dialect.Wrap(duckdb.New()).Info(), want: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, New().BindsNullElements(&testCase.info))
		})
	}
}

func TestCheckRefusesAValueWhoseTypeNestsDeeperThanGeneratedCodeFollows(t *testing.T) {
	t.Parallel()

	nested := func(levels int) sqlmodel.SQLType {
		sqlType := sqlmodel.SQLType{EngineName: "BIGINT", Category: sqlmodel.TypeCategoryInteger}
		for range levels {
			sqlType = sqlmodel.SQLType{EngineName: "BIGINT[]", Category: sqlmodel.TypeCategoryArray, ElementType: new(sqlType)}
		}
		return sqlType
	}

	testCases := []struct {
		name    string
		sqlType sqlmodel.SQLType
		want    []emit.Problem
	}{
		{name: "a column as deep as generated code follows", sqlType: nested(sqlmodel.MaxTypeDepth - 1)},
		{name: "a column deeper than generated code follows", sqlType: nested(sqlmodel.MaxTypeDepth), want: []emit.Problem{{
			Code: diag.CodeEmitterUnsupported,
			Message: "column grid has a type nesting 64 or more levels of elements, keys, fields or members, deeper than " +
				"generated code follows; flatten the type",
			Severity: diag.SeverityError,
		}}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			query := &sqlmodel.AnalysedQuery{
				Name: "ReadGrid", Command: sqlmodel.QueryCommandMany, Filename: "grids.sql",
				OutputColumns: []sqlmodel.OutputColumn{{Name: "grid", SQLType: testCase.sqlType}},
			}
			problems := New().Check(checkRequest(dialect.Wrap(duckdb.New()).Info(), query))

			for index := range testCase.want {
				testCase.want[index].Query = query
			}
			require.Equal(t, testCase.want, problems)
		})
	}
}
