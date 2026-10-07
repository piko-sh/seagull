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

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// nestedUnnamedTupleReason says why clickhouse-go cannot read a value holding a Tuple
	// with unnamed elements inside another Tuple.
	nestedUnnamedTupleReason = "it holds a Tuple with unnamed elements inside another Tuple, such as " +
		"Tuple(UInt8, Tuple(String, UInt16)) or (a, (b, c)), which clickhouse-go cannot read: the native driver " +
		"fails and database/sql returns NULL or loses the inner tuple's elements"

	// nestedUnnamedTupleSuggestion says how to write the query so clickhouse-go reads the
	// value.
	nestedUnnamedTupleSuggestion = "name the inner tuple's elements, as CAST(x AS Tuple(UInt8, Tuple(s String, n UInt16))) " +
		"does, or select the inner tuple's elements as columns of their own, such as x.2.1 and x.2.2"

	// wideIntegerMapReason says why clickhouse-go cannot read a value holding a Map keyed
	// by, or mapping to, a 128- or 256-bit integer that cannot be NULL.
	wideIntegerMapReason = "it holds a Map whose keys or values are 128- or 256-bit integers that cannot be NULL, which " +
		"clickhouse-go cannot read: both the native driver and database/sql panic reading such a Map"

	// wideIntegerMapSuggestion says how to write the query so clickhouse-go reads the value.
	wideIntegerMapSuggestion = "make the Map's values Nullable, as CAST(m AS Map(String, Nullable(Int128))) does, or select " +
		"mapKeys(m) and mapValues(m), arrays clickhouse-go reads"

	// addressKeyedMapReason says why clickhouse-go cannot read a value holding a Map keyed
	// by an IP address.
	addressKeyedMapReason = "it holds a Map keyed by an IPv4 or IPv6 address, which clickhouse-go cannot read: it fails to " +
		"decode the block, whatever the column is read into"

	// addressKeyedMapSuggestion says how to write the query so clickhouse-go reads the
	// value.
	addressKeyedMapSuggestion = "key the Map by the address's text, as CAST(m AS Map(String, String)) does, or select " +
		"mapKeys(m) and mapValues(m), arrays clickhouse-go reads"

	// undecodableKeyMapReason says why clickhouse-go cannot read a value holding a Map keyed
	// by a type it cannot decode as a Map's key; it is completed with the key, such as "an
	// Array".
	undecodableKeyMapReason = "it holds a Map keyed by %s, which clickhouse-go cannot read: it fails to decode the block, " +
		"whatever the column is read into"

	// undecodableKeyMapSuggestion says how to write the query so clickhouse-go reads the
	// value.
	undecodableKeyMapSuggestion = "select mapKeys(m) and mapValues(m), arrays clickhouse-go reads"

	// unnamedWideIntegerReason says why clickhouse-go cannot read a value holding an unnamed
	// Tuple with a 128- or 256-bit integer element that cannot be NULL.
	unnamedWideIntegerReason = "it holds a Tuple with unnamed elements, one of them a 128- or 256-bit integer that cannot be " +
		"NULL, which clickhouse-go cannot read: the native driver fails, and database/sql fails or returns NULL"

	// unnamedWideIntegerSuggestion says how to write the query so clickhouse-go reads the
	// value.
	unnamedWideIntegerSuggestion = "name the tuple's elements, as CAST(x AS Tuple(a UInt8, b Int128)) does, or make the " +
		"integer Nullable"

	// aggregateStateReason says why clickhouse-go cannot read a value holding an
	// AggregateFunction state.
	aggregateStateReason = "it holds an AggregateFunction state, which clickhouse-go cannot read: both the native driver " +
		"and database/sql fail to decode the block with an unsupported column type"

	// aggregateStateSuggestion says how to write the query so clickhouse-go reads the
	// state's value.
	aggregateStateSuggestion = "finalise the state in the query, as finalizeAggregation(x) or the aggregate's -Merge " +
		"combinator, such as uniqMerge(x), does"

	// variantReason says why clickhouse-go cannot read a value holding a Variant into the Go
	// type Seagull gives it.
	variantReason = "it holds a Variant, which clickhouse-go reads only into its own chcol.Variant type: both the " +
		"native driver and database/sql fail to scan it into the Go type Seagull generates"

	// variantSuggestion says how to write the query so clickhouse-go reads the value.
	variantSuggestion = "select the value as text, as toString(x) does, and the name of its type, as variantType(x) does"

	// deepTypeSuggestion says how to read a value whose type nests too deeply to check.
	deepTypeSuggestion = "select the deeply nested part as text, as toJSONString(x) does, or select its parts as columns " +
		"of their own"

	// bitsInLong is the width of the widest integer clickhouse-go reads as a Go integer.
	bitsInLong = 64
)

const (
	// positionTyped is a place the drivers read into the Go type the generated code gives
	// it: the column itself, an element of an array read so, and a map's value, which the
	// drivers always read into the map's own Go type. An unnamed Tuple reads there as a
	// []any.
	positionTyped tuplePosition = iota

	// positionNamedField is a field of a named Tuple, which the native driver reads into a
	// field of a Go struct by name and cannot read an unnamed Tuple into, and database/sql
	// reads into a map[string]any that keeps none of an unnamed Tuple's elements but the
	// last.
	positionNamedField

	// positionDynamic is anywhere inside an unnamed Tuple, which the drivers read into a
	// []any of interface values: there an unnamed Tuple fails or reads as NULL, and an array
	// of them loses their elements.
	positionDynamic
)

// deepTypeReason says why a value whose type nests too deeply to check is refused.
var deepTypeReason = fmt.Sprintf("its type nests %d or more levels deep, deeper than Seagull checks that clickhouse-go "+
	"can read it", sqlmodel.MaxTypeDepth)

// tuplePosition is where a value sits within a result, which decides whether
// clickhouse-go can read an unnamed Tuple there.
type tuplePosition int

// UnreadableResult identifies ClickHouse results that the driver cannot read reliably.
//
// Unsupported value types and nested unnamed tuples are refused before generation to
// prevent failed scans or lost values. The walk follows a type's parts down to
// sqlmodel.MaxTypeDepth levels; a type nesting deeper, whose deepest parts go unchecked,
// is refused too, rather than passed as readable.
//
// Takes sqlType (sqlmodel.SQLType) which is the type of a column a query returns.
//
// Returns dialect.ResultProblem which says why and how to name the elements.
// Returns bool which is false for every other type.
func (*Dialect) UnreadableResult(sqlType sqlmodel.SQLType) (dialect.ResultProblem, bool) {
	if holdsUnreadableTuple(&sqlType, positionTyped, 0) {
		return dialect.ResultProblem{Reason: nestedUnnamedTupleReason, Suggestion: nestedUnnamedTupleSuggestion}, true
	}
	if problem, unreadable := unreadableValue(&sqlType, 0); unreadable {
		return problem, true
	}
	if sqlmodel.TypeTooDeep(&sqlType) {
		return dialect.ResultProblem{Reason: deepTypeReason, Suggestion: deepTypeSuggestion}, true
	}
	return dialect.ResultProblem{Reason: "", Suggestion: ""}, false
}

// ArrayJoinElement types the element an ARRAY JOIN unfolds a Map into: one Tuple(keys K,
// values V) per entry, whose elements are named keys and values, as ClickHouse gives it
// (verified on ClickHouse 25.4); a LEFT ARRAY JOIN gives an empty Map's row the tuple's
// default value rather than NULL.
//
// Takes source (sqlmodel.SQLType) which is the type of the value unfolded.
//
// Returns sqlmodel.SQLType which is the tuple's type.
// Returns bool which is false for any value but a Map, which the analyser's default
// types.
func (*Dialect) ArrayJoinElement(source sqlmodel.SQLType) (sqlmodel.SQLType, bool) {
	if source.Category != sqlmodel.TypeCategoryMap || source.KeyType == nil || source.ElementType == nil {
		return sqlmodel.SQLType{}, false
	}
	return tupleOf([]sqlmodel.StructField{
		{Name: subcolumnKeys, SQLType: *source.KeyType, Positional: false},
		{Name: subcolumnValues, SQLType: *source.ElementType, Positional: false},
	}), true
}

// SelectAliasesShadowColumns reports ClickHouse's default alias precedence under
// prefer_column_name_to_alias = 0. Select-list aliases shadow same-name columns in
// SELECT, WHERE and ORDER BY; qualified names still read columns.
//
// Returns bool which is always true.
func (*Dialect) SelectAliasesShadowColumns() bool {
	return true
}

// unreadableValue finds nested values that clickhouse-go cannot read.
//
// This includes unsupported wide integers in Maps and unnamed Tuples, Map keys that
// cannot be decoded, AggregateFunction states, which neither driver decodes, and
// Variants, which neither driver scans into the generated Go type (verified on
// clickhouse-go v2.48 against ClickHouse 25.4).
//
// Takes sqlType (*sqlmodel.SQLType) which is the type.
// Takes depth (int) which is the type's depth; a part at sqlmodel.MaxTypeDepth or deeper
// is not walked.
//
// Returns dialect.ResultProblem which says why and how to read the value instead.
// Returns bool which is false when the walked parts of the SQL type contain no such
// value.
func unreadableValue(sqlType *sqlmodel.SQLType, depth int) (dialect.ResultProblem, bool) {
	if sqlType == nil || depth >= sqlmodel.MaxTypeDepth {
		return dialect.ResultProblem{Reason: "", Suggestion: ""}, false
	}
	switch sqlType.Category {
	case sqlmodel.TypeCategoryMap:
		if isWideInteger(sqlType.KeyType) || isWideInteger(sqlType.ElementType) {
			return dialect.ResultProblem{Reason: wideIntegerMapReason, Suggestion: wideIntegerMapSuggestion}, true
		}
		if sqlType.KeyType != nil && sqlType.KeyType.Category == sqlmodel.TypeCategoryNetwork {
			return dialect.ResultProblem{Reason: addressKeyedMapReason, Suggestion: addressKeyedMapSuggestion}, true
		}
		if key, undecodable := undecodableKey(sqlType.KeyType); undecodable {
			return dialect.ResultProblem{Reason: fmt.Sprintf(undecodableKeyMapReason, key), Suggestion: undecodableKeyMapSuggestion}, true
		}
		return unreadableValue(sqlType.ElementType, depth+1)
	case sqlmodel.TypeCategoryArray:
		return unreadableValue(sqlType.ElementType, depth+1)
	case sqlmodel.TypeCategoryStruct:
		return unreadableTupleValue(sqlType, depth)
	case sqlmodel.TypeCategoryAggregateState:
		return dialect.ResultProblem{Reason: aggregateStateReason, Suggestion: aggregateStateSuggestion}, true
	case sqlmodel.TypeCategoryUnion:
		return dialect.ResultProblem{Reason: variantReason, Suggestion: variantSuggestion}, true
	default:
		return dialect.ResultProblem{Reason: "", Suggestion: ""}, false
	}
}

// unreadableTupleValue finds a value a Tuple holds that clickhouse-go fails on: a 128- or
// 256-bit integer that cannot be NULL as an element of an unnamed Tuple, or such a value
// as unreadableValue finds inside an element.
//
// Takes tuple (*sqlmodel.SQLType) which is the Tuple's type.
// Takes depth (int) which is the Tuple's depth.
//
// Returns dialect.ResultProblem which says why and how to read the value instead.
// Returns bool which is false when the Tuple holds no such value.
func unreadableTupleValue(tuple *sqlmodel.SQLType, depth int) (dialect.ResultProblem, bool) {
	unnamed := isUnnamedTuple(tuple)
	for index := range tuple.StructFields {
		element := &tuple.StructFields[index].SQLType
		if unnamed && isWideInteger(element) {
			return dialect.ResultProblem{Reason: unnamedWideIntegerReason, Suggestion: unnamedWideIntegerSuggestion}, true
		}
		if problem, unreadable := unreadableValue(element, depth+1); unreadable {
			return problem, true
		}
	}
	return dialect.ResultProblem{Reason: "", Suggestion: ""}, false
}

// undecodableKey describes a Map key type that clickhouse-go cannot decode.
//
// Collection keys cannot form Go map keys; Decimal keys confuse the driver's comma-based
// type parser.
//
// Takes key (*sqlmodel.SQLType) which is the key type, or nil.
//
// Returns string which describes the key, such as "an Array".
// Returns bool which is true when clickhouse-go cannot decode it.
func undecodableKey(key *sqlmodel.SQLType) (string, bool) {
	if key == nil {
		return "", false
	}
	switch key.Category {
	case sqlmodel.TypeCategoryArray:
		return "an Array", true
	case sqlmodel.TypeCategoryStruct:
		return "a Tuple", true
	case sqlmodel.TypeCategoryMap:
		return "a Map", true
	case sqlmodel.TypeCategoryDecimal:
		return "a Decimal", true
	default:
		return "", false
	}
}

// isWideInteger reports whether a type is an integer wider than 64 bits that cannot be
// NULL, which clickhouse-go reads as a big.Int value where a column would read a pointer.
//
// Takes sqlType (*sqlmodel.SQLType) which is the type, or nil.
//
// Returns bool which is true for such an integer.
func isWideInteger(sqlType *sqlmodel.SQLType) bool {
	return sqlType != nil && sqlType.Category == sqlmodel.TypeCategoryInteger && sqlType.Width > bitsInLong && !sqlType.Nullable
}

// holdsUnreadableTuple reports whether a type, at a position, is or holds an unnamed
// Tuple at a position no driver reads it at.
//
// Takes sqlType (*sqlmodel.SQLType) which is the type.
// Takes position (tuplePosition) which is where the value sits.
// Takes depth (int) which is the type's depth; a part at sqlmodel.MaxTypeDepth or deeper
// is not walked.
//
// Returns bool which is true when its walked parts hold one.
func holdsUnreadableTuple(sqlType *sqlmodel.SQLType, position tuplePosition, depth int) bool {
	if sqlType == nil || depth >= sqlmodel.MaxTypeDepth {
		return false
	}
	switch sqlType.Category {
	case sqlmodel.TypeCategoryStruct:
		return tupleHoldsUnreadableTuple(sqlType, position, depth)
	case sqlmodel.TypeCategoryArray:
		return holdsUnreadableTuple(sqlType.ElementType, arrayElementPosition(position), depth+1)
	case sqlmodel.TypeCategoryMap:
		return holdsUnreadableTuple(sqlType.ElementType, positionTyped, depth+1)
	default:
		return false
	}
}

// tupleHoldsUnreadableTuple reports whether a Tuple at a position is an unnamed one no
// driver reads there, or holds one in its elements.
//
// Takes tuple (*sqlmodel.SQLType) which is the Tuple's type.
// Takes position (tuplePosition) which is where the Tuple sits.
// Takes depth (int) which is the Tuple's depth.
//
// Returns bool which is true when it is or holds one.
func tupleHoldsUnreadableTuple(tuple *sqlmodel.SQLType, position tuplePosition, depth int) bool {
	elementPosition := positionNamedField
	if isUnnamedTuple(tuple) {
		if position != positionTyped {
			return true
		}
		elementPosition = positionDynamic
	}
	if position == positionDynamic {
		elementPosition = positionDynamic
	}
	for index := range tuple.StructFields {
		if holdsUnreadableTuple(&tuple.StructFields[index].SQLType, elementPosition, depth+1) {
			return true
		}
	}
	return false
}

// arrayElementPosition returns where the elements of an array at a position sit: inside
// an unnamed Tuple they are read as interface values too, and elsewhere into the array's
// own Go element type.
//
// Takes position (tuplePosition) which is where the array sits.
//
// Returns tuplePosition which is where its elements sit.
func arrayElementPosition(position tuplePosition) tuplePosition {
	if position == positionDynamic {
		return positionDynamic
	}
	return positionTyped
}

// isUnnamedTuple reports whether a Tuple has an element without a name, which makes
// clickhouse-go read it by position rather than by name.
//
// Takes tuple (*sqlmodel.SQLType) which is the Tuple's type.
//
// Returns bool which is true when an element has no name.
func isUnnamedTuple(tuple *sqlmodel.SQLType) bool {
	for index := range tuple.StructFields {
		if tuple.StructFields[index].Positional || tuple.StructFields[index].Name == "" {
			return true
		}
	}
	return false
}
