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
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
)

func TestAnUnnamedTupleIsUnreadableWhereNoDriverReadsIt(t *testing.T) {
	t.Parallel()

	integer := sqlmodel.SQLType{EngineName: "UInt8", Category: sqlmodel.TypeCategoryInteger, Width: 8, Unsigned: true}
	text := sqlmodel.SQLType{EngineName: "String", Category: sqlmodel.TypeCategoryText}
	unnamedOf := func(elements ...sqlmodel.SQLType) sqlmodel.SQLType {
		tuple := sqlmodel.SQLType{EngineName: "Tuple", Category: sqlmodel.TypeCategoryStruct}
		for index := range elements {
			tuple.StructFields = append(tuple.StructFields, sqlmodel.StructField{
				Name: "_" + strconv.Itoa(index+1), SQLType: elements[index], Positional: true,
			})
		}
		return tuple
	}
	namedOf := func(elements ...sqlmodel.SQLType) sqlmodel.SQLType {
		tuple := sqlmodel.SQLType{EngineName: "Tuple", Category: sqlmodel.TypeCategoryStruct}
		for index := range elements {
			tuple.StructFields = append(tuple.StructFields, sqlmodel.StructField{
				Name: string(rune('a' + index)), SQLType: elements[index],
			})
		}
		return tuple
	}
	arrayOf := func(element sqlmodel.SQLType) sqlmodel.SQLType {
		return sqlmodel.SQLType{EngineName: "Array", Category: sqlmodel.TypeCategoryArray, ElementType: &element}
	}
	mapOf := func(value sqlmodel.SQLType) sqlmodel.SQLType {
		return sqlmodel.SQLType{EngineName: "Map", Category: sqlmodel.TypeCategoryMap, KeyType: &text, ElementType: &value}
	}
	pair := unnamedOf(text, integer)
	fields := namedOf(text, integer)
	testCases := []struct {
		name       string
		sqlType    sqlmodel.SQLType
		unreadable bool
	}{
		{name: "an integer", sqlType: integer},
		{name: "an unnamed tuple", sqlType: pair},
		{name: "a named tuple", sqlType: fields},
		{name: "an unnamed tuple inside an unnamed tuple", sqlType: unnamedOf(integer, pair), unreadable: true},
		{name: "a named tuple inside an unnamed tuple", sqlType: unnamedOf(integer, fields)},
		{name: "an unnamed tuple inside a named tuple", sqlType: namedOf(integer, pair), unreadable: true},
		{name: "a named tuple inside a named tuple", sqlType: namedOf(integer, fields)},
		{name: "an array of unnamed tuples holding one", sqlType: arrayOf(unnamedOf(integer, pair)), unreadable: true},
		{name: "a map to unnamed tuples holding one", sqlType: mapOf(unnamedOf(integer, pair)), unreadable: true},
		{name: "an array of unnamed tuples inside an unnamed tuple", sqlType: unnamedOf(integer, arrayOf(pair)), unreadable: true},
		{name: "a map to unnamed tuples inside an unnamed tuple", sqlType: unnamedOf(integer, mapOf(pair))},
		{name: "an array of unnamed tuples inside a named tuple", sqlType: namedOf(integer, arrayOf(pair))},
		{name: "an array of integers inside an unnamed tuple", sqlType: unnamedOf(integer, arrayOf(integer))},
		{name: "a map to unnamed tuples inside a named tuple", sqlType: namedOf(integer, mapOf(pair))},
		{name: "an array of named tuples holding an unnamed one", sqlType: arrayOf(namedOf(integer, pair)), unreadable: true},
		{name: "an unnamed tuple inside a named one inside an unnamed one", sqlType: unnamedOf(integer, namedOf(text, pair)), unreadable: true},
		{name: "an unnamed tuple inside a named one inside a named one", sqlType: namedOf(integer, namedOf(integer, pair)), unreadable: true},
		{name: "an array of unnamed tuples inside a named one inside a named one", sqlType: namedOf(integer, namedOf(integer, arrayOf(pair)))},
		{name: "an array of arrays of unnamed tuples", sqlType: arrayOf(arrayOf(pair))},
		{name: "an array of named tuples inside an unnamed tuple", sqlType: unnamedOf(integer, arrayOf(fields))},
		{name: "a map to arrays of unnamed tuples", sqlType: mapOf(arrayOf(pair))},
		{name: "a map to arrays of unnamed tuples inside an unnamed tuple", sqlType: unnamedOf(integer, mapOf(arrayOf(pair)))},
		{name: "a map to named tuples holding an unnamed one", sqlType: mapOf(namedOf(integer, pair)), unreadable: true},
		{name: "a map to unnamed tuples holding one inside a named tuple", sqlType: namedOf(integer, mapOf(unnamedOf(integer, pair))), unreadable: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			problem, unreadable := New().UnreadableResult(testCase.sqlType)
			require.Equal(t, testCase.unreadable, unreadable)
			if testCase.unreadable {
				require.Contains(t, problem.Reason, "clickhouse-go")
				require.Contains(t, problem.Suggestion, "CAST")
				return
			}
			require.Empty(t, problem.Reason)
		})
	}
}

func TestWideIntegersAndAddressesAreUnreadableWhereClickHouseGoFailsOnThem(t *testing.T) {
	t.Parallel()

	text := sqlmodel.SQLType{EngineName: "String", Category: sqlmodel.TypeCategoryText}
	wide := sqlmodel.SQLType{EngineName: "Int128", Category: sqlmodel.TypeCategoryInteger, Width: 128}
	nullableWide := sqlmodel.SQLType{EngineName: "Int128", Category: sqlmodel.TypeCategoryInteger, Width: 128, Nullable: true}
	widest := sqlmodel.SQLType{EngineName: "UInt256", Category: sqlmodel.TypeCategoryInteger, Width: 256, Unsigned: true}
	address := sqlmodel.NewNetworkType("IPv4", sqlmodel.NetworkKindAddress)
	tupleOf := func(named bool, elements ...sqlmodel.SQLType) sqlmodel.SQLType {
		tuple := sqlmodel.SQLType{EngineName: "Tuple", Category: sqlmodel.TypeCategoryStruct}
		for index := range elements {
			field := sqlmodel.StructField{Name: "_" + strconv.Itoa(index+1), SQLType: elements[index], Positional: true}
			if named {
				field = sqlmodel.StructField{Name: string(rune('a' + index)), SQLType: elements[index]}
			}
			tuple.StructFields = append(tuple.StructFields, field)
		}
		return tuple
	}
	arrayOf := func(element sqlmodel.SQLType) sqlmodel.SQLType {
		return sqlmodel.SQLType{EngineName: "Array", Category: sqlmodel.TypeCategoryArray, ElementType: &element}
	}
	mapOf := func(key, value sqlmodel.SQLType) sqlmodel.SQLType {
		return sqlmodel.SQLType{EngineName: "Map", Category: sqlmodel.TypeCategoryMap, KeyType: &key, ElementType: &value}
	}
	testCases := []struct {
		name    string
		reason  string
		sqlType sqlmodel.SQLType
	}{
		{name: "a wide integer", sqlType: wide},
		{name: "an array of wide integers", sqlType: arrayOf(wide)},
		{name: "a map to wide integers", sqlType: mapOf(text, wide), reason: "panic"},
		{name: "a map to 256-bit integers", sqlType: mapOf(text, widest), reason: "panic"},
		{name: "a map keyed by wide integers", sqlType: mapOf(wide, text), reason: "panic"},
		{name: "a map to nullable wide integers", sqlType: mapOf(text, nullableWide)},
		{name: "a map to arrays of wide integers", sqlType: mapOf(text, arrayOf(wide))},
		{name: "a map to named tuples holding a wide integer", sqlType: mapOf(text, tupleOf(true, text, wide))},
		{name: "an array of maps to wide integers", sqlType: arrayOf(mapOf(text, wide)), reason: "panic"},
		{name: "a map to maps to wide integers", sqlType: mapOf(text, mapOf(text, widest)), reason: "panic"},
		{name: "a named tuple holding a map to wide integers", sqlType: tupleOf(true, text, mapOf(text, wide)), reason: "panic"},
		{name: "a map keyed by addresses", sqlType: mapOf(address, text), reason: "decode"},
		{name: "a map to addresses", sqlType: mapOf(text, address)},
		{name: "a named tuple holding a map keyed by addresses", sqlType: tupleOf(true, text, mapOf(address, text)), reason: "decode"},
		{name: "an unnamed tuple holding a wide integer", sqlType: tupleOf(false, text, wide), reason: "unnamed"},
		{name: "an array of unnamed tuples holding a 256-bit integer", sqlType: arrayOf(tupleOf(false, text, widest)), reason: "unnamed"},
		{name: "a map to unnamed tuples holding a wide integer", sqlType: mapOf(text, tupleOf(false, text, wide)), reason: "unnamed"},
		{name: "an unnamed tuple holding a nullable wide integer", sqlType: tupleOf(false, text, nullableWide)},
		{name: "an unnamed tuple holding an array of wide integers", sqlType: tupleOf(false, text, arrayOf(wide))},
		{name: "an unnamed tuple holding a named tuple holding a wide integer", sqlType: tupleOf(false, text, tupleOf(true, wide))},
		{name: "a named tuple holding a wide integer", sqlType: tupleOf(true, text, wide)},
		{name: "an unnamed tuple holding an address", sqlType: tupleOf(false, text, address)},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			problem, unreadable := New().UnreadableResult(testCase.sqlType)
			require.Equal(t, testCase.reason != "", unreadable, "%+v", problem)
			if testCase.reason == "" {
				require.Empty(t, problem.Reason)
				return
			}
			require.Contains(t, problem.Reason, "clickhouse-go")
			require.Contains(t, problem.Reason, testCase.reason)
			require.NotEmpty(t, problem.Suggestion)
		})
	}
}

func TestAMapKeyedByATypeClickHouseGoCannotDecodeIsUnreadable(t *testing.T) {
	t.Parallel()

	text := sqlmodel.SQLType{EngineName: "String", Category: sqlmodel.TypeCategoryText}
	integer := sqlmodel.SQLType{EngineName: "UInt8", Category: sqlmodel.TypeCategoryInteger, Width: 8, Unsigned: true}
	decimal := sqlmodel.SQLType{EngineName: "Decimal", Category: sqlmodel.TypeCategoryDecimal}
	uuid := sqlmodel.SQLType{EngineName: "UUID", Category: sqlmodel.TypeCategoryUUID}
	date := sqlmodel.NewTemporalType("Date", sqlmodel.TemporalKindDate, false)
	enum := sqlmodel.SQLType{EngineName: "Enum8", Category: sqlmodel.TypeCategoryEnum, EnumValues: []string{"a", "b"}}
	array := sqlmodel.SQLType{EngineName: "Array", Category: sqlmodel.TypeCategoryArray, ElementType: &integer}
	named := sqlmodel.SQLType{EngineName: "Tuple", Category: sqlmodel.TypeCategoryStruct,
		StructFields: []sqlmodel.StructField{{Name: "x", SQLType: integer}}}
	unnamed := sqlmodel.SQLType{EngineName: "Tuple", Category: sqlmodel.TypeCategoryStruct,
		StructFields: []sqlmodel.StructField{{Name: "_1", SQLType: integer, Positional: true}, {Name: "_2", SQLType: text, Positional: true}}}
	keyed := func(key sqlmodel.SQLType) sqlmodel.SQLType {
		return sqlmodel.SQLType{EngineName: "Map", Category: sqlmodel.TypeCategoryMap, KeyType: &key, ElementType: &text}
	}
	mapped := keyed(text)
	testCases := []struct {
		name    string
		key     string
		sqlType sqlmodel.SQLType
	}{
		{name: "a map keyed by arrays", sqlType: keyed(array), key: "an Array"},
		{name: "a map keyed by named tuples", sqlType: keyed(named), key: "a Tuple"},
		{name: "a map keyed by unnamed tuples", sqlType: keyed(unnamed), key: "a Tuple"},
		{name: "a map keyed by maps", sqlType: keyed(mapped), key: "a Map"},
		{name: "a map keyed by decimals", sqlType: keyed(decimal), key: "a Decimal"},
		{name: "an array of maps keyed by decimals", sqlType: sqlmodel.SQLType{EngineName: "Array", Category: sqlmodel.TypeCategoryArray,
			ElementType: new(keyed(decimal))}, key: "a Decimal"},
		{name: "a map keyed by text", sqlType: mapped},
		{name: "a map keyed by UUIDs", sqlType: keyed(uuid)},
		{name: "a map keyed by dates", sqlType: keyed(date)},
		{name: "a map keyed by enum values", sqlType: keyed(enum)},
		{name: "a map keyed by integers", sqlType: keyed(integer)},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			problem, unreadable := New().UnreadableResult(testCase.sqlType)
			require.Equal(t, testCase.key != "", unreadable, "%+v", problem)
			if testCase.key == "" {
				require.Empty(t, problem.Reason)
				return
			}
			require.Equal(t, "it holds a Map keyed by "+testCase.key+", which clickhouse-go cannot read: it fails to decode the "+
				"block, whatever the column is read into", problem.Reason)
			require.Equal(t, "select mapKeys(m) and mapValues(m), arrays clickhouse-go reads", problem.Suggestion)
		})
	}
}

func TestAggregateStatesAndVariantsAreUnreadableWhereClickHouseGoFailsOnThem(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		typeName string
		reason   string
	}{
		{name: "an aggregate state", typeName: "AggregateFunction(uniq, String)", reason: "AggregateFunction"},
		{name: "a state of an aggregate without arguments", typeName: "AggregateFunction(count)", reason: "AggregateFunction"},
		{name: "an array of aggregate states", typeName: "Array(AggregateFunction(sum, UInt64))", reason: "AggregateFunction"},
		{name: "a named tuple holding an aggregate state", typeName: "Tuple(a AggregateFunction(sum, UInt64), b UInt8)", reason: "AggregateFunction"},
		{name: "a map to aggregate states", typeName: "Map(String, AggregateFunction(max, UInt32))", reason: "AggregateFunction"},
		{name: "a variant", typeName: "Variant(Int32, String)", reason: "Variant"},
		{name: "an array of variants", typeName: "Array(Variant(Int32, String))", reason: "Variant"},
		{name: "a named tuple holding a variant", typeName: "Tuple(a Variant(Int32, String), b UInt8)", reason: "Variant"},
		{name: "a simple aggregate", typeName: "SimpleAggregateFunction(sum, UInt64)"},
		{name: "a simple aggregate of nullable text", typeName: "SimpleAggregateFunction(max, Nullable(String))"},
		{name: "a dynamic value", typeName: "Dynamic"},
		{name: "a plain integer", typeName: "UInt64"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			problem, unreadable := New().UnreadableResult(normaliseBuiltinTypeName(testCase.typeName))
			require.Equal(t, testCase.reason != "", unreadable, "%+v", problem)
			if testCase.reason == "" {
				require.Empty(t, problem.Reason)
				return
			}
			require.Contains(t, problem.Reason, testCase.reason)
			require.Contains(t, problem.Reason, "clickhouse-go")
			require.NotEmpty(t, problem.Suggestion)
		})
	}
}

func TestATypeNestedPastTheCheckedDepthIsUnreadable(t *testing.T) {
	t.Parallel()

	integer := sqlmodel.SQLType{EngineName: "UInt8", Category: sqlmodel.TypeCategoryInteger, Width: 8, Unsigned: true}
	text := sqlmodel.SQLType{EngineName: "String", Category: sqlmodel.TypeCategoryText}
	wide := sqlmodel.SQLType{EngineName: "Int128", Category: sqlmodel.TypeCategoryInteger, Width: 128}
	unnamedOf := func(elements ...sqlmodel.SQLType) sqlmodel.SQLType {
		tuple := sqlmodel.SQLType{EngineName: "Tuple", Category: sqlmodel.TypeCategoryStruct}
		for index := range elements {
			tuple.StructFields = append(tuple.StructFields, sqlmodel.StructField{
				Name: "_" + strconv.Itoa(index+1), SQLType: elements[index], Positional: true,
			})
		}
		return tuple
	}
	nestedInArrays := func(element sqlmodel.SQLType, levels int) sqlmodel.SQLType {
		for range levels {
			element = sqlmodel.SQLType{EngineName: "Array", Category: sqlmodel.TypeCategoryArray, ElementType: new(element)}
		}
		return element
	}
	wideMap := sqlmodel.SQLType{EngineName: "Map", Category: sqlmodel.TypeCategoryMap, KeyType: &text, ElementType: &wide}
	nestedPair := unnamedOf(integer, unnamedOf(text, integer))
	testCases := []struct {
		name       string
		wantReason string
		sqlType    sqlmodel.SQLType
	}{
		{name: "a readable type just inside the depth", sqlType: nestedInArrays(integer, sqlmodel.MaxTypeDepth-1)},
		{name: "a readable type reaching the depth", sqlType: nestedInArrays(integer, sqlmodel.MaxTypeDepth), wantReason: deepTypeReason},
		{name: "a map to wide integers past the depth", sqlType: nestedInArrays(wideMap, sqlmodel.MaxTypeDepth+10), wantReason: deepTypeReason},
		{name: "a nested unnamed tuple past the depth", sqlType: nestedInArrays(nestedPair, sqlmodel.MaxTypeDepth), wantReason: deepTypeReason},
		{name: "a map to wide integers just inside the depth", sqlType: nestedInArrays(wideMap, sqlmodel.MaxTypeDepth-2), wantReason: wideIntegerMapReason},
		{name: "a nested unnamed tuple just inside the depth", sqlType: nestedInArrays(nestedPair, sqlmodel.MaxTypeDepth-3), wantReason: nestedUnnamedTupleReason},
		{
			name: "a map keyed by arrays to values nesting past the depth",
			sqlType: sqlmodel.SQLType{EngineName: "Map", Category: sqlmodel.TypeCategoryMap,
				KeyType: new(nestedInArrays(integer, 1)), ElementType: new(nestedInArrays(wide, sqlmodel.MaxTypeDepth+10))},
			wantReason: fmt.Sprintf(undecodableKeyMapReason, "an Array"),
		},
		{
			name: "an aggregate state near the top of a type nesting past the depth",
			sqlType: unnamedOf(sqlmodel.SQLType{EngineName: "AggregateFunction", Category: sqlmodel.TypeCategoryAggregateState},
				nestedInArrays(integer, sqlmodel.MaxTypeDepth+10)),
			wantReason: aggregateStateReason,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			problem, unreadable := New().UnreadableResult(testCase.sqlType)
			require.Equal(t, testCase.wantReason != "", unreadable, "%+v", problem)
			require.Equal(t, testCase.wantReason, problem.Reason)
			if testCase.wantReason == deepTypeReason {
				require.Equal(t, deepTypeSuggestion, problem.Suggestion)
				require.Contains(t, problem.Reason, strconv.Itoa(sqlmodel.MaxTypeDepth))
			}
		})
	}
}

func TestAnArrayJoinUnfoldsAMapIntoTuplesOfItsKeysAndValues(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		typeName   string
		wantFields []string
		unfolds    bool
	}{
		{name: "a map of text to integers", typeName: "Map(String, UInt64)", wantFields: []string{"keys String", "values UInt64"}, unfolds: true},
		{name: "a map to arrays", typeName: "Map(UInt8, Array(String))", wantFields: []string{"keys UInt8", "values Array"}, unfolds: true},
		{name: "an array is left to the default", typeName: "Array(String)"},
		{name: "text is left to the default", typeName: "String"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			element, unfolds := New().ArrayJoinElement(normaliseBuiltinTypeName(testCase.typeName))
			require.Equal(t, testCase.unfolds, unfolds)
			if !unfolds {
				return
			}
			require.Equal(t, sqlmodel.TypeCategoryStruct, element.Category)
			fields := make([]string, len(element.StructFields))
			for index, field := range element.StructFields {
				fields[index] = field.Name + " " + field.SQLType.EngineName
			}
			require.Equal(t, testCase.wantFields, fields)
			require.False(t, element.Nullable)
		})
	}
}
