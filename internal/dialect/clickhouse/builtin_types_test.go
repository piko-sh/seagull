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
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
)

func TestParseType_Primitives(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		want sqlmodel.SQLTypeCategory
	}{
		{"UInt8", sqlmodel.TypeCategoryInteger},
		{"UInt16", sqlmodel.TypeCategoryInteger},
		{"UInt32", sqlmodel.TypeCategoryInteger},
		{"UInt64", sqlmodel.TypeCategoryInteger},
		{"UInt128", sqlmodel.TypeCategoryInteger},
		{"UInt256", sqlmodel.TypeCategoryInteger},
		{"Int8", sqlmodel.TypeCategoryInteger},
		{"Int64", sqlmodel.TypeCategoryInteger},
		{"Float32", sqlmodel.TypeCategoryFloat},
		{"Float64", sqlmodel.TypeCategoryFloat},
		{"BFloat16", sqlmodel.TypeCategoryFloat},
		{"String", sqlmodel.TypeCategoryText},
		{"Bool", sqlmodel.TypeCategoryBoolean},
		{"Date", sqlmodel.TypeCategoryTemporal},
		{"Date32", sqlmodel.TypeCategoryTemporal},
		{"DateTime", sqlmodel.TypeCategoryTemporal},
		{"UUID", sqlmodel.TypeCategoryUUID},
		{"IPv4", sqlmodel.TypeCategoryNetwork},
		{"IPv6", sqlmodel.TypeCategoryNetwork},
		{"JSON", sqlmodel.TypeCategoryJSON},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			result, err := parseClickHouseType(testCase.name, defaultMaxTypeParseDepth)
			require.NoError(t, err)
			assert.Equal(t, testCase.want, result.SQLType.Category)
			assert.False(t, result.Nullable)
		})
	}
}

func TestParseType_NullableWrapper(t *testing.T) {
	t.Parallel()

	result, err := parseClickHouseType("Nullable(UInt32)", defaultMaxTypeParseDepth)
	require.NoError(t, err)
	assert.True(t, result.Nullable, "outer Nullable should set the flag")
	assert.Equal(t, sqlmodel.TypeCategoryInteger, result.SQLType.Category)
}

func TestParseType_LowCardinalityWrapper(t *testing.T) {
	t.Parallel()

	result, err := parseClickHouseType("LowCardinality(String)", defaultMaxTypeParseDepth)
	require.NoError(t, err)
	assert.True(t, result.LowCardinality)
	assert.Equal(t, sqlmodel.TypeCategoryText, result.SQLType.Category)
}

func TestParseType_NestedNullableLowCardinality(t *testing.T) {
	t.Parallel()

	result, err := parseClickHouseType("LowCardinality(Nullable(String))", defaultMaxTypeParseDepth)
	require.NoError(t, err)
	assert.True(t, result.LowCardinality)
	assert.True(t, result.Nullable)
	assert.Equal(t, sqlmodel.TypeCategoryText, result.SQLType.Category)
}

func TestParseType_AddressesRecordTheVersionTheyHold(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		width int
	}{
		{name: "IPv4", width: 32},
		{name: "ipv4", width: 32},
		{name: "Nullable(IPv4)", width: 32},
		{name: "LowCardinality(IPv4)", width: 32},
		{name: "IPv6", width: 128},
		{name: "Nullable(IPv6)", width: 128},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			result, err := parseClickHouseType(testCase.name, defaultMaxTypeParseDepth)
			require.NoError(t, err)
			require.Equal(t, sqlmodel.NetworkKindAddress, result.SQLType.Network)
			require.Equal(t, testCase.width, result.SQLType.Width)
		})
	}

	nested, err := parseClickHouseType("Map(String, Array(Tuple(a IPv4, b Nullable(IPv6))))", defaultMaxTypeParseDepth)
	require.NoError(t, err)
	require.NotNil(t, nested.SQLType.ElementType)
	require.NotNil(t, nested.SQLType.ElementType.ElementType)
	fields := nested.SQLType.ElementType.ElementType.StructFields
	require.Len(t, fields, 2)
	require.Equal(t, 32, fields[0].SQLType.Width)
	require.Equal(t, 128, fields[1].SQLType.Width)
}

func TestParseType_Array(t *testing.T) {
	t.Parallel()

	result, err := parseClickHouseType("Array(String)", defaultMaxTypeParseDepth)
	require.NoError(t, err)
	assert.Equal(t, sqlmodel.TypeCategoryArray, result.SQLType.Category)
	require.NotNil(t, result.SQLType.ElementType)
	assert.Equal(t, sqlmodel.TypeCategoryText, result.SQLType.ElementType.Category)
}

func TestParseType_ArrayOfNullable(t *testing.T) {
	t.Parallel()

	result, err := parseClickHouseType("Array(Nullable(Int32))", defaultMaxTypeParseDepth)
	require.NoError(t, err)
	assert.Equal(t, sqlmodel.TypeCategoryArray, result.SQLType.Category)
	require.NotNil(t, result.SQLType.ElementType)
	assert.Equal(t, sqlmodel.TypeCategoryInteger, result.SQLType.ElementType.Category)
	assert.True(t, result.SQLType.ElementType.Nullable, "Array element should preserve inner Nullable flag")
	assert.False(t, result.Nullable, "outer Array, not Nullable")
}

func TestParseType_MapNullableValue(t *testing.T) {
	t.Parallel()

	result, err := parseClickHouseType("Map(String, Nullable(Int32))", defaultMaxTypeParseDepth)
	require.NoError(t, err)
	assert.Equal(t, sqlmodel.TypeCategoryMap, result.SQLType.Category)
	require.NotNil(t, result.SQLType.KeyType)
	require.NotNil(t, result.SQLType.ElementType)
	assert.False(t, result.SQLType.KeyType.Nullable)
	assert.True(t, result.SQLType.ElementType.Nullable, "Map value should preserve inner Nullable flag")
}

func TestParseType_TupleWithNullableField(t *testing.T) {
	t.Parallel()

	result, err := parseClickHouseType("Tuple(name String, count Nullable(UInt64))", defaultMaxTypeParseDepth)
	require.NoError(t, err)
	require.Len(t, result.SQLType.StructFields, 2)
	assert.False(t, result.SQLType.StructFields[0].SQLType.Nullable)
	assert.True(t, result.SQLType.StructFields[1].SQLType.Nullable, "Tuple field should preserve inner Nullable flag")
}

func TestParseType_NestedWithNullableField(t *testing.T) {
	t.Parallel()

	result, err := parseClickHouseType("Nested(name String, salary Nullable(Float64))", defaultMaxTypeParseDepth)
	require.NoError(t, err)

	require.NotNil(t, result.SQLType.ElementType)
	require.Len(t, result.SQLType.ElementType.StructFields, 2)
	assert.True(t, result.SQLType.ElementType.StructFields[1].SQLType.Nullable)
}

func TestParseType_TupleAnonymous(t *testing.T) {
	t.Parallel()

	result, err := parseClickHouseType("Tuple(String, UInt64)", defaultMaxTypeParseDepth)
	require.NoError(t, err)
	assert.Equal(t, sqlmodel.TypeCategoryStruct, result.SQLType.Category)
	require.Len(t, result.SQLType.StructFields, 2)
	assert.Equal(t, "_1", result.SQLType.StructFields[0].Name)
	assert.Equal(t, sqlmodel.TypeCategoryText, result.SQLType.StructFields[0].SQLType.Category)
	assert.Equal(t, "_2", result.SQLType.StructFields[1].Name)
}

func TestParseType_TupleNamed(t *testing.T) {
	t.Parallel()

	result, err := parseClickHouseType("Tuple(name String, age UInt64)", defaultMaxTypeParseDepth)
	require.NoError(t, err)
	require.Len(t, result.SQLType.StructFields, 2)
	assert.Equal(t, "name", result.SQLType.StructFields[0].Name)
	assert.Equal(t, "age", result.SQLType.StructFields[1].Name)
}

func TestParseType_Map(t *testing.T) {
	t.Parallel()

	result, err := parseClickHouseType("Map(String, UInt32)", defaultMaxTypeParseDepth)
	require.NoError(t, err)
	assert.Equal(t, sqlmodel.TypeCategoryMap, result.SQLType.Category)
	require.NotNil(t, result.SQLType.KeyType)
	require.NotNil(t, result.SQLType.ElementType)
	assert.Equal(t, sqlmodel.TypeCategoryText, result.SQLType.KeyType.Category)
	assert.Equal(t, sqlmodel.TypeCategoryInteger, result.SQLType.ElementType.Category)
}

func TestParseType_NestedDesugarsToArrayOfTuple(t *testing.T) {
	t.Parallel()

	result, err := parseClickHouseType("Nested(name String, age UInt64)", defaultMaxTypeParseDepth)
	require.NoError(t, err)
	assert.Equal(t, sqlmodel.TypeCategoryArray, result.SQLType.Category)
	require.NotNil(t, result.SQLType.ElementType)
	assert.Equal(t, sqlmodel.TypeCategoryStruct, result.SQLType.ElementType.Category)
	require.Len(t, result.SQLType.ElementType.StructFields, 2)
	assert.Equal(t, "name", result.SQLType.ElementType.StructFields[0].Name)
}

func TestParseType_Enum(t *testing.T) {
	t.Parallel()

	result, err := parseClickHouseType("Enum8('red' = 1, 'green' = 2, 'blue' = 3)", defaultMaxTypeParseDepth)
	require.NoError(t, err)
	assert.Equal(t, sqlmodel.TypeCategoryEnum, result.SQLType.Category)
	assert.Equal(t, []string{"red", "green", "blue"}, result.SQLType.EnumValues)
}

func TestParseType_Enum16WithNegativeTag(t *testing.T) {
	t.Parallel()

	result, err := parseClickHouseType("Enum16('low' = -1, 'high' = 1)", defaultMaxTypeParseDepth)
	require.NoError(t, err)
	assert.Equal(t, []string{"low", "high"}, result.SQLType.EnumValues)
}

func TestParseType_FixedString(t *testing.T) {
	t.Parallel()

	result, err := parseClickHouseType("FixedString(16)", defaultMaxTypeParseDepth)
	require.NoError(t, err)
	assert.Equal(t, sqlmodel.TypeCategoryText, result.SQLType.Category)
	require.NotNil(t, result.SQLType.Length)
	assert.Equal(t, 16, *result.SQLType.Length)
}

func TestParseType_Decimal(t *testing.T) {
	t.Parallel()

	result, err := parseClickHouseType("Decimal(18, 4)", defaultMaxTypeParseDepth)
	require.NoError(t, err)
	assert.Equal(t, sqlmodel.TypeCategoryDecimal, result.SQLType.Category)
	require.NotNil(t, result.SQLType.Precision)
	require.NotNil(t, result.SQLType.Scale)
	assert.Equal(t, 18, *result.SQLType.Precision)
	assert.Equal(t, 4, *result.SQLType.Scale)
}

func TestParseType_DecimalShortForms(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input             string
		expectedPrecision int
		expectedScale     int
	}{
		{"Decimal32(2)", 9, 2},
		{"Decimal64(4)", 18, 4},
		{"Decimal128(8)", 38, 8},
		{"Decimal256(16)", 76, 16},
	}
	for _, testCase := range cases {
		t.Run(testCase.input, func(t *testing.T) {
			t.Parallel()
			result, err := parseClickHouseType(testCase.input, defaultMaxTypeParseDepth)
			require.NoError(t, err)
			require.NotNil(t, result.SQLType.Precision)
			require.NotNil(t, result.SQLType.Scale)
			assert.Equal(t, testCase.expectedPrecision, *result.SQLType.Precision)
			assert.Equal(t, testCase.expectedScale, *result.SQLType.Scale)
		})
	}
}

func TestParseType_DateTime64(t *testing.T) {
	t.Parallel()

	result, err := parseClickHouseType("DateTime64(3)", defaultMaxTypeParseDepth)
	require.NoError(t, err)
	assert.Equal(t, sqlmodel.TypeCategoryTemporal, result.SQLType.Category)
	require.NotNil(t, result.SQLType.Precision)
	assert.Equal(t, 3, *result.SQLType.Precision)
}

func TestParseType_DateTime64WithTimezone(t *testing.T) {
	t.Parallel()

	result, err := parseClickHouseType("DateTime64(6, 'UTC')", defaultMaxTypeParseDepth)
	require.NoError(t, err)
	require.NotNil(t, result.SQLType.Precision)
	assert.Equal(t, 6, *result.SQLType.Precision)
}

func TestParseType_DateTimeWithTimezone(t *testing.T) {
	t.Parallel()

	result, err := parseClickHouseType("DateTime('UTC')", defaultMaxTypeParseDepth)
	require.NoError(t, err)
	assert.Equal(t, sqlmodel.TypeCategoryTemporal, result.SQLType.Category)
	assert.Equal(t, "DateTime", result.SQLType.EngineName)
}

func TestParseType_Variant(t *testing.T) {
	t.Parallel()

	result, err := parseClickHouseType("Variant(String, UInt32, Float64)", defaultMaxTypeParseDepth)
	require.NoError(t, err)
	assert.Equal(t, sqlmodel.TypeCategoryUnion, result.SQLType.Category)
	require.Len(t, result.SQLType.UnionMembers, 3)
}

func TestParseType_AggregateFunction(t *testing.T) {
	t.Parallel()

	result, err := parseClickHouseType("AggregateFunction(sum, UInt64)", defaultMaxTypeParseDepth)
	require.NoError(t, err)

	assert.Equal(t, "AggregateFunction(sum, UInt64)", result.SQLType.EngineName)
	assert.Equal(t, sqlmodel.TypeCategoryAggregateState, result.SQLType.Category)
	require.NotNil(t, result.SQLType.ElementType)
	assert.Equal(t, sqlmodel.TypeCategoryInteger, result.SQLType.ElementType.Category)
}

func TestParseType_DeeplyNested(t *testing.T) {
	t.Parallel()

	result, err := parseClickHouseType("Array(Tuple(name String, scores Array(Nullable(Float64))))", defaultMaxTypeParseDepth)
	require.NoError(t, err)
	assert.Equal(t, sqlmodel.TypeCategoryArray, result.SQLType.Category)
	require.NotNil(t, result.SQLType.ElementType)
	tuple := result.SQLType.ElementType
	assert.Equal(t, sqlmodel.TypeCategoryStruct, tuple.Category)
	require.Len(t, tuple.StructFields, 2)
	scoresField := tuple.StructFields[1]
	assert.Equal(t, "scores", scoresField.Name)
	assert.Equal(t, sqlmodel.TypeCategoryArray, scoresField.SQLType.Category)
}

func TestParseType_UnknownTypeReturnsUnknown(t *testing.T) {
	t.Parallel()

	result, err := parseClickHouseType("MagicalType", defaultMaxTypeParseDepth)
	require.NoError(t, err)
	assert.Equal(t, sqlmodel.TypeCategoryUnknown, result.SQLType.Category)
	assert.Equal(t, "MagicalType", result.SQLType.EngineName)
}

func TestParseType_MalformedReturnsError(t *testing.T) {
	t.Parallel()

	cases := []string{
		"Nullable(",
		"Nullable(UInt32",
		"Array",
		"Tuple(",
		"Map(",
		"Decimal(",
		"DateTime64",
		"DateTime64(",
		"Decimal(18 4)",
		"UInt32 trailing",
	}
	for _, testCase := range cases {
		t.Run(testCase, func(t *testing.T) {
			t.Parallel()
			_, err := parseClickHouseType(testCase, defaultMaxTypeParseDepth)
			assert.Error(t, err)
		})
	}
}

func TestEngine_NormaliseTypeNameStripsNullable(t *testing.T) {
	t.Parallel()

	engine := New()
	result := engine.NormaliseTypeName("Nullable(UInt32)")
	assert.Equal(t, sqlmodel.TypeCategoryInteger, result.Category)
}

func TestEngine_NormaliseTypeNameHandlesArray(t *testing.T) {
	t.Parallel()

	engine := New()
	result := engine.NormaliseTypeName("Array(String)")
	assert.Equal(t, sqlmodel.TypeCategoryArray, result.Category)
	require.NotNil(t, result.ElementType)
	assert.Equal(t, sqlmodel.TypeCategoryText, result.ElementType.Category)
}

func TestBuildTypeCatalogue_IncludesExtras(t *testing.T) {
	t.Parallel()

	extras := map[string]sqlmodel.SQLType{
		"MyCustomType": {Category: sqlmodel.TypeCategoryComposite, EngineName: "MyCustomType"},
	}
	catalogue := buildTypeCatalogue(extras)
	require.NotNil(t, catalogue)
	_, hasCustom := catalogue.Types["mycustomtype"]
	assert.True(t, hasCustom)
	_, hasBuiltin := catalogue.Types["uint32"]
	assert.True(t, hasBuiltin)
}

func TestParseType_NewGeoTypes(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"LineString", "MultiLineString", "Geometry"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result, err := parseClickHouseType(name, defaultMaxTypeParseDepth)
			require.NoError(t, err)
			assert.Equal(t, sqlmodel.TypeCategoryGeometric, result.SQLType.Category)
		})
	}
}

func TestParseType_IdentifierType(t *testing.T) {
	t.Parallel()

	result, err := parseClickHouseType("Identifier", defaultMaxTypeParseDepth)
	require.NoError(t, err)
	assert.Equal(t, "Identifier", result.SQLType.EngineName)
}

func TestParseType_EnumAutoTagShorthand(t *testing.T) {
	t.Parallel()

	result, err := parseClickHouseType("Enum('a', 'b', 'c')", defaultMaxTypeParseDepth)
	require.NoError(t, err)
	assert.Equal(t, sqlmodel.TypeCategoryEnum, result.SQLType.Category)
	assert.Equal(t, []string{"a", "b", "c"}, result.SQLType.EnumValues)
	assert.Equal(t, "Enum8", result.SQLType.EngineName, "bare Enum should fold to Enum8")
}

func TestParseType_JSONWithParameters(t *testing.T) {
	t.Parallel()

	result, err := parseClickHouseType("JSON(max_dynamic_paths=100, path.to.field UInt32, SKIP other.path)", defaultMaxTypeParseDepth)
	require.NoError(t, err)
	assert.Equal(t, sqlmodel.TypeCategoryJSON, result.SQLType.Category)
	assert.Equal(t, "JSON", result.SQLType.EngineName)
}

func TestParseType_DynamicWithParameters(t *testing.T) {
	t.Parallel()

	result, err := parseClickHouseType("Dynamic(max_types=10)", defaultMaxTypeParseDepth)
	require.NoError(t, err)
	assert.Equal(t, "Dynamic", result.SQLType.EngineName)
}

func TestTimeTypesHoldATimeOfDay(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		typeName      string
		wantEngine    string
		wantPrecision int
		wantNullable  bool
	}{
		{name: "seconds", typeName: "Time", wantEngine: "Time", wantPrecision: -1},
		{name: "milliseconds", typeName: "Time64(3)", wantEngine: "Time64", wantPrecision: 3},
		{name: "microseconds written with spaces", typeName: "Time64( 6 )", wantEngine: "Time64", wantPrecision: 6},
		{name: "a nullable time", typeName: "Nullable(Time64(9))", wantEngine: "Time64", wantPrecision: 9, wantNullable: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result, err := parseClickHouseType(testCase.typeName, defaultMaxTypeParseDepth)
			require.NoError(t, err)
			sqlType := result.SQLType
			sqlType.Nullable = sqlType.Nullable || result.Nullable
			require.Equal(t, testCase.wantEngine, sqlType.EngineName)
			require.Equal(t, sqlmodel.TypeCategoryTemporal, sqlType.Category)
			require.Equal(t, sqlmodel.TemporalKindTime, sqlType.Temporal)
			require.False(t, sqlType.TimeZone)
			require.Equal(t, testCase.wantNullable, sqlType.Nullable)
			if testCase.wantPrecision < 0 {
				require.Nil(t, sqlType.Precision)
				return
			}
			require.NotNil(t, sqlType.Precision)
			require.Equal(t, testCase.wantPrecision, *sqlType.Precision)
		})
	}
}

func TestASimpleAggregateFunctionColumnHoldsItsValueType(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		typeName     string
		wantEngine   string
		wantCategory sqlmodel.SQLTypeCategory
		wantNullable bool
	}{
		{name: "an integer", typeName: "SimpleAggregateFunction(any, UInt64)", wantEngine: "UInt64", wantCategory: sqlmodel.TypeCategoryInteger},
		{name: "nullable text", typeName: "SimpleAggregateFunction(max, Nullable(String))", wantEngine: "String",
			wantCategory: sqlmodel.TypeCategoryText, wantNullable: true},
		{name: "an array", typeName: "SimpleAggregateFunction(groupArrayArray, Array(String))", wantEngine: "Array",
			wantCategory: sqlmodel.TypeCategoryArray},
		{name: "a parametrised aggregate", typeName: "SimpleAggregateFunction(anyLast, LowCardinality(String))", wantEngine: "String",
			wantCategory: sqlmodel.TypeCategoryText},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result, err := parseClickHouseType(testCase.typeName, defaultMaxTypeParseDepth)
			require.NoError(t, err)
			sqlType := result.SQLType
			sqlType.Nullable = sqlType.Nullable || result.Nullable
			require.Equal(t, testCase.wantEngine, sqlType.EngineName)
			require.Equal(t, testCase.wantCategory, sqlType.Category)
			require.Equal(t, testCase.wantNullable, sqlType.Nullable)
		})
	}
}

func TestParseType_ObjectJSONAliasesToJSON(t *testing.T) {
	t.Parallel()

	result, err := parseClickHouseType("Object('json')", defaultMaxTypeParseDepth)
	require.NoError(t, err)
	assert.Equal(t, sqlmodel.TypeCategoryJSON, result.SQLType.Category)
	assert.Equal(t, "JSON", result.SQLType.EngineName)
}

func TestTypeNameParseErrorQuotesAtMostALimitedNumberOfRunes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		input         string
		wantQuoted    string
		wantTruncated bool
	}{
		{name: "short name is quoted whole", input: "Array(", wantQuoted: `"Array("`, wantTruncated: false},
		{name: "name at the limit is quoted whole", input: strings.Repeat("é", maxTypeNameErrorRunes), wantQuoted: `"` + strings.Repeat("é", maxTypeNameErrorRunes) + `"`, wantTruncated: false},
		{name: "long multibyte name is cut on a rune boundary", input: "Array(" + strings.Repeat("日本", 100), wantQuoted: `"Array(` + strings.Repeat("日本", 29) + `"`, wantTruncated: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			parseError := &typeNameParseError{input: testCase.input, message: "expected type identifier", position: 3}
			message := parseError.Error()

			assert.True(t, utf8.ValidString(message))
			assert.Contains(t, message, testCase.wantQuoted)
			assert.Contains(t, message, "at position 3")
			if testCase.wantTruncated {
				assert.Contains(t, message, "truncated, 206 runes in total")
				assert.Less(t, len(message), len(testCase.input))
				return
			}
			assert.NotContains(t, message, "truncated")
		})
	}
}

func TestParseTypeReportsOversizedMalformedNamesBriefly(t *testing.T) {
	t.Parallel()

	input := "Array(" + strings.Repeat("x", 10_000) + " junk"
	_, err := parseClickHouseType(input, defaultMaxTypeParseDepth)
	require.Error(t, err)
	assert.Less(t, len(err.Error()), 300)
}

func TestEnumLabelsAreDecodedAsTheServerReturnsThem(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		input string
		want  []string
	}{
		{name: "a doubled quote", input: "Enum8('it''s' = 1, 'b' = 2)", want: []string{"it's", "b"}},
		{name: "a backslash-escaped quote", input: `Enum8('x\'y' = 1)`, want: []string{"x'y"}},
		{name: "an escaped backslash", input: `Enum8('a\\b' = 1)`, want: []string{`a\b`}},
		{name: "hex and unicode escapes", input: `Enum16('\x41' = 1, '\u00e9' = 2)`, want: []string{"A", "é"}},
		{name: "plain labels", input: "Enum8('red' = 1, 'green' = 2)", want: []string{"red", "green"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result, err := parseClickHouseType(testCase.input, defaultMaxTypeParseDepth)
			require.NoError(t, err)
			assert.Equal(t, testCase.want, result.SQLType.EnumValues)
		})
	}
}

func TestEnumLabelsWithQuotesSurviveDDLAndPlaceholders(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
		want []string
	}{
		{name: "a doubled quote in a column type", sql: "CREATE TABLE t (e Enum8('it''s' = 1, 'b' = 2)) ENGINE = Memory", want: []string{"it's", "b"}},
		{name: "a backslash-escaped quote in a column type", sql: `CREATE TABLE t (e Enum8('x\'y' = 1)) ENGINE = Memory`, want: []string{"x'y"}},
		{name: "a backslash in a column type", sql: `CREATE TABLE t (e Enum8('a\\b' = 1)) ENGINE = Memory`, want: []string{`a\b`}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyDDL(t, testCase.sql)
			require.NoError(t, err)
			require.Len(t, mutation.Columns, 1)
			assert.Equal(t, testCase.want, mutation.Columns[0].SQLType.EnumValues)
		})
	}

	analysis := analyse(t, `SELECT id FROM t WHERE e = {e:Enum8('it\'s' = 1)}`)
	require.Len(t, analysis.ParameterReferences, 1)
	require.NotNil(t, analysis.ParameterReferences[0].CastType)
	assert.Equal(t, []string{"it's"}, analysis.ParameterReferences[0].CastType.EnumValues)
}

func TestAggregateFunctionStatesWithoutArgumentsParse(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "CREATE TABLE s (k String, d AggregateFunction(count), q AggregateFunction(quantiles(0.5, 0.9), UInt64)) "+
		"ENGINE = AggregatingMergeTree() ORDER BY k")
	require.NoError(t, err)
	require.Len(t, mutation.Columns, 3)
	assert.Equal(t, sqlmodel.TypeCategoryAggregateState, mutation.Columns[1].SQLType.Category)
	assert.Equal(t, "AggregateFunction(count)", mutation.Columns[1].SQLType.EngineName)
	assert.Equal(t, "AggregateFunction(quantiles, UInt64)", mutation.Columns[2].SQLType.EngineName)
}
