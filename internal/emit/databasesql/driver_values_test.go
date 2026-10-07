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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/duckdb"
	"seagull.sh/seagull/internal/dialect/mysql"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/dialect/sqlite"
	"seagull.sh/seagull/internal/gotypes"
	"seagull.sh/seagull/internal/sqlmodel"
)

func mapParameterQuery() *sqlmodel.AnalysedQuery {
	integer := sqlmodel.NewIntegerType("int4", 32, false)
	fees := sqlmodel.NewSQLType(sqlmodel.TypeCategoryMap, "map")
	fees.KeyType = new(sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar"))
	fees.ElementType = &integer
	return &sqlmodel.AnalysedQuery{
		Name:     "SetFees",
		Command:  sqlmodel.QueryCommandExec,
		SQL:      "UPDATE fleets SET fees = $1 WHERE id = $2",
		Filename: "fleets.sql",
		Parameters: []sqlmodel.QueryParameter{
			{Number: 1, Name: "fees", SQLType: fees},
			{Number: 2, Name: "id", SQLType: integer},
		},
	}
}

func compositeParameterQuery() *sqlmodel.AnalysedQuery {
	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar")
	integer := sqlmodel.NewIntegerType("int4", 32, false)
	berth := sqlmodel.NewSQLType(sqlmodel.TypeCategoryStruct, "struct")
	berth.StructFields = []sqlmodel.StructField{{Name: "pontoon", SQLType: text}, {Name: "number", SQLType: integer}}
	route := sqlmodel.NewSQLType(sqlmodel.TypeCategoryArray, "struct[]")
	route.ElementType = &berth
	crew := sqlmodel.NewSQLType(sqlmodel.TypeCategoryArray, "varchar[]")
	crew.ElementType = &text
	return &sqlmodel.AnalysedQuery{
		Name:     "MoorBoat",
		Command:  sqlmodel.QueryCommandExec,
		SQL:      "INSERT INTO boats (berth, route, crew) VALUES ($1, $2, $3)",
		Filename: "boats.sql",
		Parameters: []sqlmodel.QueryParameter{
			{Number: 1, Name: "berth", SQLType: berth},
			{Number: 2, Name: "route", SQLType: route},
			{Number: 3, Name: "crew", SQLType: crew},
		},
	}
}

func TestEmitQueriesBindsDuckDBMapParametersThroughTheDriverComposite(t *testing.T) {
	t.Parallel()

	files, err := emitQueriesForDialect(t.Context(), New(), dialect.Wrap(duckdb.New()).Info(), "db", []*sqlmodel.AnalysedQuery{mapParameterQuery()}, defaultMappings())
	require.NoError(t, err)

	querySource := findGeneratedFile(t, files, "fleets.sql.go")
	helperSource := findGeneratedFile(t, files, driverCompositeHelperFile)
	for name, source := range map[string]string{"fleets.sql.go": querySource, driverCompositeHelperFile: helperSource} {
		_, parseErr := parser.ParseFile(token.NewFileSet(), name, source, parser.AllErrors)
		require.NoError(t, parseErr, "%s must be valid Go:\n%s", name, source)
	}
	require.Contains(t, querySource, "sglDriverComposite(params.Fees)")
	require.NotContains(t, querySource, "sglDriverComposite(params.ID)")
	require.Contains(t, helperSource, "package db\n")
	require.Contains(t, helperSource, `"github.com/duckdb/duckdb-go/v2"`)
	require.Contains(t, helperSource, "func sglDriverComposite(value any) any")
	require.Contains(t, helperSource, "converted := make(duckdb.Map, reflected.Len())")
}

func TestEmitQueriesBindsDuckDBStructAndStructListParametersThroughTheDriverComposite(t *testing.T) {
	t.Parallel()

	files, err := emitQueriesForDialect(t.Context(), New(), dialect.Wrap(duckdb.New()).Info(), "db", []*sqlmodel.AnalysedQuery{compositeParameterQuery()}, defaultMappings())
	require.NoError(t, err)

	querySource := findGeneratedFile(t, files, "boats.sql.go")
	require.Contains(t, querySource, "sglDriverComposite(params.Berth)")
	require.Contains(t, querySource, "sglDriverComposite(params.Route)")
	require.NotContains(t, querySource, "sglDriverComposite(params.Crew)")
	helperSource := findGeneratedFile(t, files, driverCompositeHelperFile)
	require.Contains(t, helperSource, `name, _, _ := strings.Cut(field.Tag.Get("json"), ",")`)
	require.Contains(t, helperSource, "converted := make([]any, reflected.Len())")
}

func TestEmitQueriesBindsMapParametersAsTheyAreOnOtherDrivers(t *testing.T) {
	t.Parallel()

	files, err := emitQueriesForDialect(t.Context(), New(), dialect.Wrap(postgres.New()).Info(), "db", []*sqlmodel.AnalysedQuery{mapParameterQuery()}, defaultMappings())
	require.NoError(t, err)

	require.NotContains(t, findGeneratedFile(t, files, "fleets.sql.go"), "sglDriverComposite")
	for _, file := range files {
		require.NotEqual(t, driverCompositeHelperFile, file.Name)
	}
}

func TestEmitQueriesBindsANullableDuckDBListThroughTheDriverComposite(t *testing.T) {
	t.Parallel()

	list := sqlmodel.NewSQLType(sqlmodel.TypeCategoryArray, "varchar[]")
	list.ElementType = new(sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar"))
	crewQuery := func() *sqlmodel.AnalysedQuery {
		return &sqlmodel.AnalysedQuery{
			Name:     "AddCrew",
			Command:  sqlmodel.QueryCommandExec,
			SQL:      "INSERT INTO crews (names, ranks) VALUES ($1, $2)",
			Filename: "crews.sql",
			Parameters: []sqlmodel.QueryParameter{
				{Number: 1, Name: "names", SQLType: list, Nullable: true},
				{Number: 2, Name: "ranks", SQLType: list},
			},
		}
	}

	files, err := emitQueriesForDialect(t.Context(), New(), dialect.Wrap(duckdb.New()).Info(), "db", []*sqlmodel.AnalysedQuery{crewQuery()}, defaultMappings())
	require.NoError(t, err)
	source := findGeneratedFile(t, files, "crews.sql.go")
	require.Contains(t, source, "sglDriverComposite(params.Names)", "the driver binds a nil slice as an empty list, so a nullable list binds through the conversion")
	require.NotContains(t, source, "sglDriverComposite(params.Ranks)")

	files, err = emitQueriesForDialect(t.Context(), New(), dialect.Wrap(postgres.New()).Info(), "db", []*sqlmodel.AnalysedQuery{crewQuery()}, defaultMappings())
	require.NoError(t, err)
	require.NotContains(t, findGeneratedFile(t, files, "crews.sql.go"), "sglDriverComposite")
}

func TestEmitQueriesBindsADuckDBListOfJSONThroughTheDriverComposite(t *testing.T) {
	t.Parallel()

	document := sqlmodel.NewSQLType(sqlmodel.TypeCategoryJSON, "json")
	list := sqlmodel.NewSQLType(sqlmodel.TypeCategoryArray, "json[]")
	list.ElementType = &document
	grid := sqlmodel.NewSQLType(sqlmodel.TypeCategoryArray, "json[][]")
	grid.ElementType = &list
	documentsQuery := func() *sqlmodel.AnalysedQuery {
		return &sqlmodel.AnalysedQuery{
			Name:     "FileDocuments",
			Command:  sqlmodel.QueryCommandExec,
			SQL:      "INSERT INTO folders (cover, documents, pages) VALUES ($1, $2, $3)",
			Filename: "folders.sql",
			Parameters: []sqlmodel.QueryParameter{
				{Number: 1, Name: "cover", SQLType: document},
				{Number: 2, Name: "documents", SQLType: list},
				{Number: 3, Name: "pages", SQLType: grid},
			},
		}
	}

	testCases := []struct {
		name     string
		contains []string
		lacks    []string
		duckDB   bool
	}{
		{
			name:     "DuckDB converts each element of a list of JSON through its driver.Valuer",
			duckDB:   true,
			contains: []string{"sglDriverValue{value: params.Cover}", "sglDriverComposite(params.Documents)", "sglDriverComposite(params.Pages)"},
		},
		{
			name:  "pgx binds a list of JSON itself",
			lacks: []string{"sglDriverComposite", "sglDriverValue"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			info := dialect.Wrap(postgres.New()).Info()
			if testCase.duckDB {
				info = dialect.Wrap(duckdb.New()).Info()
			}
			files, err := emitQueriesForDialect(t.Context(), New(), info, "db", []*sqlmodel.AnalysedQuery{documentsQuery()}, defaultMappings())
			require.NoError(t, err)
			source := findGeneratedFile(t, files, "folders.sql.go")
			for _, fragment := range testCase.contains {
				require.Contains(t, source, fragment)
			}
			for _, fragment := range testCase.lacks {
				require.NotContains(t, source, fragment)
			}
		})
	}
}

func priceParameters() []sqlmodel.QueryParameter {
	decimal := sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "numeric")
	return []sqlmodel.QueryParameter{
		{Number: 1, Name: "id", SQLType: sqlmodel.NewIntegerType("int4", 32, false)},
		{Number: 2, Name: "amount", SQLType: decimal},
		{Number: 3, Name: "discount", SQLType: decimal, Nullable: true},
	}
}

func priceQueries(t *testing.T, info *dialect.Facts, placeholders [3]string) []*sqlmodel.AnalysedQuery {
	t.Helper()
	batch := batchOf(t, info, sqlmodel.QueryCommandBatch, priceParameters(),
		templateText("INSERT INTO prices (id, amount, discount) VALUES "),
		templateRow(templateText("("), templateParameter(0, ""), templateText(", "), templateParameter(1, ""), templateText(", "),
			templateParameter(2, ""), templateText(")")),
	)
	batch.Name, batch.Filename = "AddPrices", "prices.sql"
	return []*sqlmodel.AnalysedQuery{
		{
			Name: "AddPrice", Command: sqlmodel.QueryCommandExec, Filename: "prices.sql", Parameters: priceParameters(),
			SQL: "INSERT INTO prices (id, amount, discount) VALUES (" + strings.Join(placeholders[:], ", ") + ")",
		},
		batch,
		{
			Name: "TrimPrices", Command: sqlmodel.QueryCommandExec, Filename: "prices.sql", IsDynamic: true,
			SQL: "DELETE FROM prices WHERE amount > " + placeholders[0],
			Parameters: []sqlmodel.QueryParameter{
				{Number: 1, Name: "ceiling", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "numeric"), IsOptional: true},
			},
		},
	}
}

func TestEmitQueriesBindsDecimalsHeldByPointerThroughTheDriverDecimal(t *testing.T) {
	t.Parallel()

	anonymous := [3]string{"?", "?", "?"}
	numbered := [3]string{"$1", "$2", "$3"}
	money := sqlmodel.TypeSlots{}
	money.Decimal = sqlmodel.TypeSlot{
		NotNull:  sqlmodel.GoType{Composite: nil, Package: "example.com/money", Name: "Decimal"},
		Nullable: sqlmodel.GoType{Composite: nil, Package: "example.com/money", Name: "*Decimal"},
	}

	testCases := []struct {
		info     dialect.Facts
		slots    sqlmodel.TypeSlots
		name     string
		contains []string
		lacks    []string
		sigils   [3]string
	}{
		{
			name:   "SQLite binds a nullable or optional apd decimal through the wrapper",
			info:   dialect.Wrap(sqlite.New()).Info(),
			sigils: anonymous,
			contains: []string{
				"sglDriverDecimalValue{value: params.Discount}", "sglDriverDecimalValue{value: item.Discount}",
				"sglDriverDecimalValue{value: params.Ceiling}", "func (wrapped sglDriverDecimalValue) Value() (driver.Value, error)",
			},
			lacks: []string{"sglDriverDecimalValue{value: params.Amount}", "sglDriverDecimalValue{value: item.Amount}", "sglDriverValue"},
		},
		{
			name:     "MySQL binds a nullable apd decimal through the wrapper",
			info:     dialect.Wrap(mysql.New()).Info(),
			sigils:   anonymous,
			contains: []string{"sglDriverDecimalValue{value: params.Discount}", "sglDriverDecimalValue{value: item.Discount}"},
			lacks:    []string{"sglDriverDecimalValue{value: params.Amount}"},
		},
		{
			name:     "DuckDB binds a nullable apd decimal through the wrapper",
			info:     dialect.Wrap(duckdb.New()).Info(),
			sigils:   numbered,
			contains: []string{"sglDriverDecimalValue{value: params.Discount}", "sglDriverDecimalValue{value: item.Discount}"},
			lacks:    []string{"sglDriverDecimalValue{value: params.Amount}"},
		},
		{
			name:   "pgx binds a nullable apd decimal itself",
			info:   dialect.Wrap(postgres.New()).Info(),
			sigils: numbered,
			lacks:  []string{"sglDriverDecimalValue"},
		},
		{
			name:   "a decimal type of the slot's choosing binds as it is",
			info:   dialect.Wrap(sqlite.New()).Info(),
			slots:  money,
			sigils: anonymous,
			lacks:  []string{"sglDriverDecimalValue"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mappings := gotypes.NewTypeMapper(nil).BuildMappingTable(nil, testCase.slots)
			files, err := emitQueriesForDialect(t.Context(), New(), testCase.info, "db", priceQueries(t, &testCase.info, testCase.sigils), mappings)
			require.NoError(t, err)
			var sources strings.Builder
			for _, file := range files {
				_, parseErr := parser.ParseFile(token.NewFileSet(), file.Name, file.Content, parser.AllErrors)
				require.NoError(t, parseErr, "%s must be valid Go:\n%s", file.Name, file.Content)
				sources.Write(file.Content)
			}
			for _, fragment := range testCase.contains {
				require.Contains(t, sources.String(), fragment)
			}
			for _, fragment := range testCase.lacks {
				require.NotContains(t, sources.String(), fragment)
			}
		})
	}
}
