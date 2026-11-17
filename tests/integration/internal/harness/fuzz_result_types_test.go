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

package harness

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/sdk/dialect"
)

func TestFuzzResultTypeNamePreservesUnsafeMismatches(t *testing.T) {
	engines := []string{enginePostgres, engineSQLite, engineMySQL, engineMariaDB, engineDuckDB, engineClickHouse, engineCockroachDB, engineTimescaleDB}
	for _, engine := range engines {
		for _, parameters := range []bool{false, true} {
			for _, inferred := range []dialect.SQLTypeCategory{dialect.TypeCategoryInteger, dialect.TypeCategoryDecimal, dialect.TypeCategoryFloat} {
				for _, observed := range []string{"BIGINT", "DECIMAL", "DOUBLE", "TEXT"} {
					suite := FuzzSuite{Engine: engine, ParseType: MySQLTypeName}
					want := observed
					if engine == engineMariaDB && parameters && inferred == dialect.TypeCategoryDecimal && observed == "BIGINT" {
						want = "DECIMAL"
					}
					got := fuzzResultTypeName(suite, dialect.SQLType{Category: inferred}, parameters, observed)
					require.Equal(t, want, got, "%s parameters %v inferred %v observed %s", engine, parameters, inferred, observed)
				}
			}
		}
	}
	problems := compareTypes(inferredType{sqlType: dialect.SQLType{Category: dialect.TypeCategoryInteger}}, "DECIMAL", ReportedType{Category: dialect.TypeCategoryDecimal}, false)
	require.NotEmpty(t, problems, "integer inference accepted a decimal result")
}

func TestNumericTypeAdaptersPreserveSignedness(t *testing.T) {
	for _, current := range []struct {
		name     string
		parse    func(string) (ReportedType, bool)
		signed   string
		unsigned string
	}{
		{name: engineMySQL, parse: MySQLTypeName, signed: "BIGINT", unsigned: "UNSIGNED BIGINT"},
		{name: engineDuckDB, parse: DuckDBTypeName, signed: "BIGINT", unsigned: "UBIGINT"},
		{name: engineClickHouse, parse: ClickHouseTypeName, signed: "Int64", unsigned: "UInt64"},
	} {
		t.Run(current.name, func(t *testing.T) {
			first, ok := current.parse(current.unsigned)
			require.True(t, ok, "unsigned metadata was not recognised")
			require.Equal(t, dialect.TypeCategoryInteger, first.Category, "unsigned metadata lost")
			require.Equal(t, 64, first.Width, "unsigned metadata lost")
			require.True(t, first.Unsigned, "unsigned metadata lost")
			second, ok := current.parse(current.signed)
			require.True(t, ok, "signed metadata was not recognised")
			require.Equal(t, 64, second.Width, "signed metadata changed")
			require.False(t, second.Unsigned, "signed metadata changed")
		})
	}
}

func TestScalarTypesRecogniseOnlyAdmittedNames(t *testing.T) {
	for _, current := range []struct {
		engine   string
		name     string
		category dialect.SQLTypeCategory
	}{
		{engine: enginePostgres, name: "TEXT", category: dialect.TypeCategoryText},
		{engine: engineCockroachDB, name: "character varying", category: dialect.TypeCategoryText},
		{engine: engineMariaDB, name: "longblob", category: dialect.TypeCategoryBinary},
		{engine: engineClickHouse, name: "Nullable(String)", category: dialect.TypeCategoryText},
		{engine: engineDuckDB, name: "TIMESTAMP WITH TIME ZONE", category: dialect.TypeCategoryTemporal},
		{engine: engineDuckDB, name: "INTEGER[]", category: dialect.TypeCategoryArray},
	} {
		reported, ok := fuzzScalarType(current.engine, current.name)
		require.True(t, ok, "%s %s", current.engine, current.name)
		require.Equal(t, current.category, reported.Category, "%s %s", current.engine, current.name)
	}
	_, ok := fuzzScalarType(engineSQLite, "uuid")
	require.False(t, ok, "SQLite has no shared scalar names")
	_, ok = fuzzScalarType(enginePostgres, "geometry")
	require.False(t, ok, "an unknown name was guessed")
}
