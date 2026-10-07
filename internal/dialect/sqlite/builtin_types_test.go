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

package sqlite

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestNormaliseByAffinityRetainsTextLengthModifier(t *testing.T) {
	t.Parallel()

	result := normaliseTypeName("VARCHAR2", 50)
	assert.Equal(t, sqlmodel.TypeCategoryText, result.Category)
	require.NotNil(t, result.Length, "affinity-resolved text type dropped its length modifier")
	assert.Equal(t, 50, *result.Length)
}

func TestNormaliseByAffinityDecimalKeepsPrecisionAndScale(t *testing.T) {
	t.Parallel()

	result := normaliseTypeName("DECIMAL_LIKE", 10, 2)
	assert.Equal(t, sqlmodel.TypeCategoryDecimal, result.Category)
	require.NotNil(t, result.Precision)
	assert.Equal(t, 10, *result.Precision)
	require.NotNil(t, result.Scale)
	assert.Equal(t, 2, *result.Scale)
}

func TestSpellingsWhoseValuesAreNoNumbersKeepTheirOwnType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		spelling string
		category sqlmodel.SQLTypeCategory
		engine   string
	}{
		{name: "a JSONB column holds a document", spelling: "JSONB", category: sqlmodel.TypeCategoryJSON, engine: "jsonb"},
		{name: "a lower-case jsonb column holds a document", spelling: "jsonb", category: sqlmodel.TypeCategoryJSON, engine: "jsonb"},
		{name: "a JSON column holds a document", spelling: "JSON", category: sqlmodel.TypeCategoryJSON, engine: "json"},
		{name: "a STRING column holds text", spelling: "STRING", category: sqlmodel.TypeCategoryText, engine: "text"},
		{name: "a NUMERIC column holds a number", spelling: "NUMERIC", category: sqlmodel.TypeCategoryDecimal, engine: "numeric"},
		{name: "a MONEY column follows the numeric affinity", spelling: "MONEY", category: sqlmodel.TypeCategoryDecimal, engine: "numeric"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result := normaliseTypeName(test.spelling)
			require.Equal(t, test.category, result.Category)
			require.Equal(t, test.engine, result.EngineName)
		})
	}
}

func TestDeclaredSpellingsWhoseValuesReadAsNoNumberTakeTheirOwnType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		spelling  string
		category  sqlmodel.SQLTypeCategory
		engine    string
		modifiers []int
	}{
		{name: "a UUID column holds UUIDs", spelling: "UUID", category: sqlmodel.TypeCategoryUUID, engine: "uuid"},
		{name: "a lower-case uuid column holds UUIDs", spelling: "uuid", category: sqlmodel.TypeCategoryUUID, engine: "uuid"},
		{name: "a TIME column holds the text of a time", spelling: "TIME", category: sqlmodel.TypeCategoryText, engine: "time"},
		{name: "a TIME column with a precision holds text", spelling: "TIME", category: sqlmodel.TypeCategoryText, engine: "time", modifiers: []int{6}},
		{name: "a TIMETZ column holds text", spelling: "TIMETZ", category: sqlmodel.TypeCategoryText, engine: "timetz"},
		{name: "a TIMESTAMPTZ column holds the text of a moment", spelling: "TIMESTAMPTZ", category: sqlmodel.TypeCategoryText, engine: "timestamptz"},
		{name: "a zoned timestamp spelt in words holds text", spelling: "TIMESTAMP WITH TIME ZONE", category: sqlmodel.TypeCategoryText, engine: "timestamp with time zone"},
		{name: "a timestamp without a zone spelt in words holds text", spelling: "timestamp without time zone", category: sqlmodel.TypeCategoryText, engine: "timestamp without time zone"},
		{name: "a zoned time spelt in words holds text", spelling: "TIME WITH TIME ZONE", category: sqlmodel.TypeCategoryText, engine: "time with time zone"},
		{name: "an INTERVAL column holds the text of an interval", spelling: "INTERVAL", category: sqlmodel.TypeCategoryText, engine: "interval"},
		{name: "a BYTEA column holds bytes", spelling: "BYTEA", category: sqlmodel.TypeCategoryBinary, engine: "bytea"},
		{name: "a VARBINARY column holds bytes", spelling: "VARBINARY", category: sqlmodel.TypeCategoryBinary, engine: "varbinary", modifiers: []int{16}},
		{name: "a BINARY column holds bytes", spelling: "BINARY", category: sqlmodel.TypeCategoryBinary, engine: "binary", modifiers: []int{16}},
		{name: "an XML column holds a document's text", spelling: "XML", category: sqlmodel.TypeCategoryText, engine: "xml"},
		{name: "an INET column holds an address's text", spelling: "INET", category: sqlmodel.TypeCategoryText, engine: "inet"},
		{name: "a CIDR column holds a network's text", spelling: "CIDR", category: sqlmodel.TypeCategoryText, engine: "cidr"},
		{name: "a MACADDR column holds an address's text", spelling: "MACADDR", category: sqlmodel.TypeCategoryText, engine: "macaddr"},
		{name: "a MONEY column holds a number", spelling: "MONEY", category: sqlmodel.TypeCategoryDecimal, engine: "numeric"},
		{name: "a spelling of no known kind follows the numeric affinity", spelling: "GEOMETRY", category: sqlmodel.TypeCategoryDecimal, engine: "numeric"},
		{name: "a DATETIME column is a time the drivers read", spelling: "DATETIME", category: sqlmodel.TypeCategoryTemporal, engine: "datetime"},
		{name: "a DATE column is a date the drivers read", spelling: "date", category: sqlmodel.TypeCategoryTemporal, engine: "date"},
		{name: "a TIMESTAMP column is a time the drivers read", spelling: "TIMESTAMP", category: sqlmodel.TypeCategoryTemporal, engine: "timestamp"},
		{name: "a TIMESTAMP column with a precision holds text", spelling: "TIMESTAMP", category: sqlmodel.TypeCategoryText, engine: "timestamp", modifiers: []int{3}},
		{name: "a DATETIME column with a precision holds text", spelling: "DATETIME", category: sqlmodel.TypeCategoryText, engine: "datetime", modifiers: []int{6}},
		{name: "a DATE column with a length holds text", spelling: "Date", category: sqlmodel.TypeCategoryText, engine: "date", modifiers: []int{10}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result := normaliseTypeName(test.spelling, test.modifiers...)
			require.Equal(t, test.category, result.Category)
			require.Equal(t, test.engine, result.EngineName)
		})
	}
}

func TestTheTypeCatalogueNamesTheSpellingsWhoseValuesReadAsNoNumber(t *testing.T) {
	t.Parallel()

	catalogue := New().BuiltinTypes()
	for _, spelling := range []string{"uuid", "time", "timestamptz", "interval", "bytea", "xml"} {
		sqlType, known := catalogue.Types[spelling]
		require.True(t, known, spelling)
		require.Equal(t, spelling, sqlType.EngineName)
	}
}

func TestACastConvertsByTheAffinityOfItsTypeName(t *testing.T) {
	t.Parallel()

	hooked := newTypeResolver([]func(string, []int) *sqlmodel.SQLType{func(name string, _ []int) *sqlmodel.SQLType {
		if name == "uuid" {
			return new(sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "uuid"))
		}
		return nil
	}}, nil)
	extra := newTypeResolver(nil, map[string]sqlmodel.SQLType{"time": sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "time")})
	tests := []struct {
		name     string
		resolver *typeResolver
		spelling string
		want     string
	}{
		{name: "a cast to UUID gives a number", spelling: "UUID", want: "numeric"},
		{name: "a cast to TIME gives a number", spelling: "TIME", want: "numeric"},
		{name: "a cast to a zoned timestamp in words gives a number", spelling: "TIMESTAMP WITH TIME ZONE", want: "numeric"},
		{name: "a cast to INTERVAL gives an integer", spelling: "INTERVAL", want: "integer"},
		{name: "a cast to BYTEA gives a number", spelling: "BYTEA", want: "numeric"},
		{name: "a cast to TEXT keeps its name", spelling: "TEXT", want: "text"},
		{name: "a cast to VARCHAR keeps its name", spelling: "VARCHAR", want: "varchar"},
		{name: "a cast to DATETIME gives a number", spelling: "DATETIME", want: "numeric"},
		{name: "a cast to DATE gives a number", spelling: "date", want: "numeric"},
		{name: "a cast to TIMESTAMP gives a number", spelling: "TIMESTAMP", want: "numeric"},
		{name: "a cast to NUMERIC keeps its name", spelling: "NUMERIC", want: "numeric"},
		{name: "a cast to INTEGER keeps its name", spelling: "INTEGER", want: "integer"},
		{name: "a cast to BOOLEAN gives a number", spelling: "BOOLEAN", want: "numeric"},
		{name: "a cast to BOOL gives a number", spelling: "bool", want: "numeric"},
		{name: "a cast to JSON gives a number", spelling: "JSON", want: "numeric"},
		{name: "a cast to JSONB gives a number", spelling: "JSONB", want: "numeric"},
		{name: "a cast to STRING gives a number", spelling: "STRING", want: "numeric"},
		{name: "a cast to ANY gives a number", spelling: "ANY", want: "numeric"},
		{name: "a cast to BIGINT keeps its name", spelling: "BIGINT", want: "bigint"},
		{name: "a cast to DOUBLE keeps its name", spelling: "DOUBLE", want: "double"},
		{name: "a cast to BLOB keeps its name", spelling: "BLOB", want: "blob"},
		{name: "a cast to a spelling of no known kind keeps its name", spelling: "GEOMETRY", want: "geometry"},
		{name: "a cast to a spelling a hook resolves keeps its name", resolver: hooked, spelling: "UUID", want: "uuid"},
		{name: "a cast to an extra type keeps its name", resolver: extra, spelling: "TIME", want: "time"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, test.want, test.resolver.castTypeName(test.spelling))
		})
	}
}

func TestSplitTypeModifiersSeparatesASignedNumericListFromTheName(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		input         string
		wantBase      string
		wantModifiers []int
		wantFound     bool
	}{
		{name: "a precision and a scale", input: "decimal(10, 2)", wantBase: "decimal", wantModifiers: []int{10, 2},
			wantFound: true},
		{name: "spaces around every part", input: "  UNSIGNED BIG INT ( 8 )  ", wantBase: "UNSIGNED BIG INT",
			wantModifiers: []int{8}, wantFound: true},
		{name: "a negative scale", input: "decimal(10,-2)", wantBase: "decimal", wantModifiers: []int{10, -2},
			wantFound: true},
		{name: "a sign apart from its number", input: "decimal(+ 5, - 1)", wantBase: "decimal",
			wantModifiers: []int{5, -1}, wantFound: true},
		{name: "a modifier past the largest one", input: "varchar(99999999999999999999)", wantBase: "varchar",
			wantModifiers: []int{dialectkit.MaxTypeModifier}, wantFound: true},
		{name: "a negative modifier past the largest one", input: "decimal(4, -99999999999999999999)",
			wantBase: "decimal", wantModifiers: []int{4, -dialectkit.MaxTypeModifier}, wantFound: true},
		{name: "a fraction is truncated", input: "varchar(1.5e3)", wantBase: "varchar", wantModifiers: []int{1500},
			wantFound: true},
		{name: "a hexadecimal modifier", input: "varchar(0x10)", wantBase: "varchar", wantModifiers: []int{16},
			wantFound: true},
		{name: "a name with no list", input: "decimal"},
		{name: "a list with no name", input: " (10)"},
		{name: "a list left open", input: "decimal(10"},
		{name: "three modifiers", input: "decimal(1,2,3)"},
		{name: "an empty list", input: "decimal()"},
		{name: "an empty modifier", input: "decimal(,2)"},
		{name: "a sign with no number", input: "decimal(-)"},
		{name: "two signs", input: "decimal(--5)"},
		{name: "a word", input: "decimal(x)"},
		{name: "a nested list", input: "decimal((10))"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			base, modifiers, found := splitTypeModifiers(testCase.input)

			assert.Equal(t, testCase.wantFound, found)
			assert.Equal(t, testCase.wantBase, base)
			assert.Equal(t, testCase.wantModifiers, modifiers)
		})
	}
}

func TestFormatTypeModifiersWritesTheListSplitTypeModifiersReads(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		modifiers []int
		want      string
		wantBase  string
		wantFound bool
	}{
		{name: "no modifiers", modifiers: nil, want: ""},
		{name: "a length", modifiers: []int{20}, want: "(20)", wantBase: "decimal", wantFound: true},
		{name: "a precision and a negative scale", modifiers: []int{10, -2}, want: "(10,-2)", wantBase: "decimal",
			wantFound: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			written := formatTypeModifiers(testCase.modifiers)
			base, modifiers, found := splitTypeModifiers("decimal" + written)

			assert.Equal(t, testCase.want, written)
			assert.Equal(t, testCase.wantFound, found)
			assert.Equal(t, testCase.wantBase, base)
			assert.Equal(t, testCase.modifiers, modifiers)
		})
	}
}

func TestACastKeepsItsModifiersOnlyWhenTypedByItsOwnSpelling(t *testing.T) {
	t.Parallel()

	ownDate := newTypeResolver(nil, map[string]sqlmodel.SQLType{
		"date": sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "date"),
	})

	testCases := []struct {
		name                   string
		resolver               *typeResolver
		spelling               string
		modifiers              []int
		wantCastName           string
		wantParameterEngine    string
		wantParameterPrecision *int
		wantParameterScale     *int
		wantParameterLength    *int
	}{
		{name: "a spelling typed by itself keeps a length", resolver: newTypeResolver(nil, nil), spelling: "VARCHAR",
			modifiers: []int{10}, wantCastName: "varchar(10)", wantParameterEngine: "text", wantParameterLength: new(10)},
		{name: "a spelling typed by itself keeps a negative scale", resolver: newTypeResolver(nil, nil),
			spelling: "DECIMAL", modifiers: []int{10, -2}, wantCastName: "decimal(10,-2)", wantParameterEngine: "numeric",
			wantParameterPrecision: new(10), wantParameterScale: new(-2)},
		{name: "a spelling written without modifiers", resolver: newTypeResolver(nil, nil), spelling: "DECIMAL",
			wantCastName: "decimal", wantParameterEngine: "numeric"},
		{name: "a spelling typed by its affinity takes none", resolver: newTypeResolver(nil, nil), spelling: "TIMESTAMP",
			modifiers: []int{3}, wantCastName: "numeric", wantParameterEngine: "numeric"},
		{name: "a truth value keeps its type for a placeholder and takes none", resolver: newTypeResolver(nil, nil),
			spelling: "BOOLEAN", modifiers: []int{1}, wantCastName: "numeric", wantParameterEngine: "boolean"},
		{name: "an extra type resolves itself and keeps its modifiers", resolver: ownDate, spelling: "DATE",
			modifiers: []int{3}, wantCastName: "date(3)", wantParameterEngine: "date", wantParameterLength: new(3)},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			castName := testCase.resolver.castTargetName(testCase.spelling, testCase.modifiers)
			parameterType := testCase.resolver.parameterCastType(testCase.spelling, testCase.modifiers)

			assert.Equal(t, testCase.wantCastName, castName)
			assert.Equal(t, testCase.wantParameterEngine, parameterType.EngineName)
			assert.Equal(t, testCase.wantParameterPrecision, parameterType.Precision)
			assert.Equal(t, testCase.wantParameterScale, parameterType.Scale)
			assert.Equal(t, testCase.wantParameterLength, parameterType.Length)
		})
	}
}
