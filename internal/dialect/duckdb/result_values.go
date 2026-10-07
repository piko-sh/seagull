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

package duckdb

import (
	"fmt"
	"iter"
	"slices"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// unnamedStructReason says why duckdb-go cannot read a value holding a STRUCT whose
	// fields have no names.
	unnamedStructReason = "it holds a STRUCT whose fields have no names, as ROW(a, b) and (a, b) build, which duckdb-go " +
		"cannot read (it fails with \"empty name\")"

	// unnamedStructSuggestion says how to build the STRUCT so duckdb-go can read it.
	unnamedStructSuggestion = "name the struct's fields: struct_pack(a := a, b := b) or {'a': a, 'b': b}"

	// compositeKeyReason says why duckdb-go cannot read a value holding a MAP keyed by a
	// value that holds others.
	compositeKeyReason = "it holds a MAP keyed by a LIST, STRUCT, MAP or UNION, which duckdb-go cannot read (it fails with " +
		"\"MAP key type not supported\")"

	// compositeKeySuggestion says how to read such a MAP instead.
	compositeKeySuggestion = "read map_entries(m), a list of its key and value structs, or map_keys(m) and map_values(m) instead"

	// deepTypeSuggestion says how to read a value whose type nests too deeply to check.
	deepTypeSuggestion = "return the deeply nested part as JSON, with to_json(...), or as separate columns"
)

const (
	// typeSearchAbsent is a search that walked the whole type and found nothing.
	typeSearchAbsent typeSearch = iota

	// typeSearchFound is a search that found what it looked for.
	typeSearchFound

	// typeSearchTruncated is a search that found nothing in the parts it walked but stopped
	// at sqlmodel.MaxTypeDepth before walking every part.
	typeSearchTruncated
)

var (
	// compositeKeyCategories lists the categories of the MAP keys duckdb-go cannot read: the
	// types that hold other values.
	compositeKeyCategories = []sqlmodel.SQLTypeCategory{
		sqlmodel.TypeCategoryArray, sqlmodel.TypeCategoryStruct, sqlmodel.TypeCategoryMap, sqlmodel.TypeCategoryUnion,
	}

	// deepTypeReason says why a value whose type nests too deeply to check is refused.
	deepTypeReason = fmt.Sprintf("its type nests %d or more levels deep, deeper than Seagull checks that duckdb-go "+
		"can read it", sqlmodel.MaxTypeDepth)
)

// typeSearch is what a search through the parts of a result's type found.
type typeSearch uint8

// UnreadableResult identifies values the DuckDB driver cannot read.
//
// It rejects unnamed struct fields and map keys whose types cannot be represented as Go
// map keys, including nested occurrences. A type nesting sqlmodel.MaxTypeDepth levels or
// more deep, whose parts past that depth go unchecked, is rejected too, rather than
// passed as readable.
//
// Takes sqlType (sqlmodel.SQLType) which is the type of a column a query returns.
//
// Returns dialect.ResultProblem which says why and how to read the value instead.
// Returns bool which is false for every other type.
func (*Dialect) UnreadableResult(sqlType sqlmodel.SQLType) (dialect.ResultProblem, bool) {
	unnamed := searchResultType(&sqlType, 0, holdsUnnamedField)
	if unnamed == typeSearchFound {
		return dialect.ResultProblem{Reason: unnamedStructReason, Suggestion: unnamedStructSuggestion}, true
	}
	composite := searchResultType(&sqlType, 0, isCompositeKeyedMap)
	if composite == typeSearchFound {
		return dialect.ResultProblem{Reason: compositeKeyReason, Suggestion: compositeKeySuggestion}, true
	}
	if unnamed == typeSearchTruncated || composite == typeSearchTruncated {
		return dialect.ResultProblem{Reason: deepTypeReason, Suggestion: deepTypeSuggestion}, true
	}
	return dialect.ResultProblem{Reason: "", Suggestion: ""}, false
}

// searchResultType reports whether a type is, or holds in an element, key, value, field
// or union member, a type that matches.
//
// The root is at depth 0, and each nested type adds one level. The search skips types at
// sqlmodel.MaxTypeDepth or deeper and reports that it reached the limit.
//
// Takes sqlType (*sqlmodel.SQLType) which is the type; nil holds nothing.
// Takes depth (int) which is the type's depth.
// Takes matches (func(*sqlmodel.SQLType) bool) which reports a type searched for.
//
// Returns typeSearch which is typeSearchFound when a walked part matches, or else
// typeSearchTruncated when the walk stopped at the depth limit.
func searchResultType(sqlType *sqlmodel.SQLType, depth int, matches func(*sqlmodel.SQLType) bool) typeSearch {
	if sqlType == nil {
		return typeSearchAbsent
	}
	if depth >= sqlmodel.MaxTypeDepth {
		return typeSearchTruncated
	}
	if matches(sqlType) {
		return typeSearchFound
	}
	result := typeSearchAbsent
	for part := range typeParts(sqlType) {
		switch searchResultType(part, depth+1, matches) {
		case typeSearchFound:
			return typeSearchFound
		case typeSearchTruncated:
			result = typeSearchTruncated
		case typeSearchAbsent:
		}
	}
	return result
}

// typeParts yields the types a type holds directly: its struct fields' and union members'
// types, then its element and key types.
//
// Takes sqlType (*sqlmodel.SQLType) which is the type, not nil.
//
// Returns iter.Seq[*sqlmodel.SQLType] which yields each part; an absent element or key
// type is yielded as nil.
func typeParts(sqlType *sqlmodel.SQLType) iter.Seq[*sqlmodel.SQLType] {
	return func(yield func(*sqlmodel.SQLType) bool) {
		for index := range sqlType.StructFields {
			if !yield(&sqlType.StructFields[index].SQLType) {
				return
			}
		}
		for index := range sqlType.UnionMembers {
			if !yield(&sqlType.UnionMembers[index].SQLType) {
				return
			}
		}
		if yield(sqlType.ElementType) {
			yield(sqlType.KeyType)
		}
	}
}

// isCompositeKeyedMap reports whether a type is a MAP keyed by a LIST, STRUCT, MAP or
// UNION.
//
// Takes sqlType (*sqlmodel.SQLType) which is the type, not nil.
//
// Returns bool which is true when it is.
func isCompositeKeyedMap(sqlType *sqlmodel.SQLType) bool {
	return sqlType.Category == sqlmodel.TypeCategoryMap && sqlType.KeyType != nil &&
		slices.Contains(compositeKeyCategories, sqlType.KeyType.Category)
}

// holdsUnnamedField reports whether a type is a STRUCT with a field that has no name.
//
// Takes sqlType (*sqlmodel.SQLType) which is the type, not nil.
//
// Returns bool which is true when one of its fields is positional or unnamed.
func holdsUnnamedField(sqlType *sqlmodel.SQLType) bool {
	for index := range sqlType.StructFields {
		if sqlType.StructFields[index].Positional || sqlType.StructFields[index].Name == "" {
			return true
		}
	}
	return false
}
