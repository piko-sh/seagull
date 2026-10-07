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
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
)

func TestOnlyValuesHoldingAStructWithoutFieldNamesAreUnreadable(t *testing.T) {
	t.Parallel()

	integer := sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger, Width: 32}
	unnamed := sqlmodel.SQLType{EngineName: "struct", Category: sqlmodel.TypeCategoryStruct, StructFields: []sqlmodel.StructField{
		{Name: "element1", SQLType: integer, Positional: true},
	}}
	named := sqlmodel.SQLType{EngineName: "struct", Category: sqlmodel.TypeCategoryStruct, StructFields: []sqlmodel.StructField{
		{Name: "a", SQLType: integer},
	}}
	listOf := func(element sqlmodel.SQLType) sqlmodel.SQLType {
		return sqlmodel.SQLType{EngineName: "list", Category: sqlmodel.TypeCategoryArray, ElementType: &element}
	}
	mapOf := func(value sqlmodel.SQLType) sqlmodel.SQLType {
		return sqlmodel.SQLType{
			EngineName:  "map",
			Category:    sqlmodel.TypeCategoryMap,
			KeyType:     new(sqlmodel.SQLType{EngineName: "varchar", Category: sqlmodel.TypeCategoryText}),
			ElementType: &value,
		}
	}
	holding := func(field sqlmodel.SQLType) sqlmodel.SQLType {
		return sqlmodel.SQLType{EngineName: "struct", Category: sqlmodel.TypeCategoryStruct, StructFields: []sqlmodel.StructField{
			{Name: "x", SQLType: field},
		}}
	}
	unionOf := func(member sqlmodel.SQLType) sqlmodel.SQLType {
		return sqlmodel.SQLType{EngineName: "union", Category: sqlmodel.TypeCategoryUnion, UnionMembers: []sqlmodel.UnionMember{
			{Tag: "only", SQLType: member},
		}}
	}
	nameless := sqlmodel.SQLType{EngineName: "struct", Category: sqlmodel.TypeCategoryStruct, StructFields: []sqlmodel.StructField{
		{Name: "", SQLType: integer},
	}}
	testCases := []struct {
		name       string
		sqlType    sqlmodel.SQLType
		unreadable bool
	}{
		{name: "a struct built by ROW", sqlType: unnamed, unreadable: true},
		{name: "a struct with a field of no name", sqlType: nameless, unreadable: true},
		{name: "a list of them", sqlType: listOf(unnamed), unreadable: true},
		{name: "a map to them", sqlType: mapOf(unnamed), unreadable: true},
		{name: "a named struct holding one", sqlType: holding(unnamed), unreadable: true},
		{name: "a union holding one", sqlType: unionOf(unnamed), unreadable: true},
		{name: "a struct with named fields", sqlType: named},
		{name: "a list of named structs", sqlType: listOf(named)},
		{name: "a map to named structs", sqlType: mapOf(holding(named))},
		{name: "an integer", sqlType: integer},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			problem, unreadable := New().UnreadableResult(testCase.sqlType)

			require.Equal(t, testCase.unreadable, unreadable)
			if testCase.unreadable {
				require.Contains(t, problem.Reason, "duckdb-go")
				require.Contains(t, problem.Suggestion, "struct_pack")
				return
			}
			require.Empty(t, problem.Reason)
		})
	}
}

func TestOnlyValuesHoldingAMapWithACompositeKeyAreUnreadableToo(t *testing.T) {
	t.Parallel()

	integer := sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger, Width: 32}
	text := sqlmodel.SQLType{EngineName: "varchar", Category: sqlmodel.TypeCategoryText}
	day := sqlmodel.SQLType{EngineName: "date", Category: sqlmodel.TypeCategoryTemporal, Temporal: sqlmodel.TemporalKindDate}
	list := sqlmodel.SQLType{EngineName: "list", Category: sqlmodel.TypeCategoryArray, ElementType: &integer}
	structure := sqlmodel.SQLType{EngineName: "struct", Category: sqlmodel.TypeCategoryStruct, StructFields: []sqlmodel.StructField{{Name: "a", SQLType: integer}}}
	union := sqlmodel.SQLType{EngineName: "union", Category: sqlmodel.TypeCategoryUnion, UnionMembers: []sqlmodel.UnionMember{{Tag: "n", SQLType: integer}}}
	keyedBy := func(key sqlmodel.SQLType) sqlmodel.SQLType {
		return sqlmodel.SQLType{EngineName: "map", Category: sqlmodel.TypeCategoryMap, KeyType: &key, ElementType: &integer}
	}
	inner := keyedBy(text)
	listOf := func(element sqlmodel.SQLType) sqlmodel.SQLType {
		return sqlmodel.SQLType{EngineName: "list", Category: sqlmodel.TypeCategoryArray, ElementType: &element}
	}
	holding := func(field sqlmodel.SQLType) sqlmodel.SQLType {
		return sqlmodel.SQLType{EngineName: "struct", Category: sqlmodel.TypeCategoryStruct, StructFields: []sqlmodel.StructField{{Name: "x", SQLType: field}}}
	}
	testCases := []struct {
		name       string
		sqlType    sqlmodel.SQLType
		unreadable bool
	}{
		{name: "a map keyed by a list", sqlType: keyedBy(list), unreadable: true},
		{name: "a map keyed by a struct", sqlType: keyedBy(structure), unreadable: true},
		{name: "a map keyed by a map", sqlType: keyedBy(inner), unreadable: true},
		{name: "a map keyed by a union", sqlType: keyedBy(union), unreadable: true},
		{name: "a list of maps keyed by a struct", sqlType: listOf(keyedBy(structure)), unreadable: true},
		{name: "a struct holding a map keyed by a list", sqlType: holding(keyedBy(list)), unreadable: true},
		{name: "a map keyed by text", sqlType: inner},
		{name: "a map keyed by a date", sqlType: keyedBy(day)},
		{name: "a list of maps keyed by text", sqlType: listOf(inner)},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			problem, unreadable := New().UnreadableResult(testCase.sqlType)

			require.Equal(t, testCase.unreadable, unreadable)
			if testCase.unreadable {
				require.Contains(t, problem.Reason, "MAP key type not supported")
				require.Contains(t, problem.Suggestion, "map_entries")
				return
			}
			require.Empty(t, problem.Reason)
		})
	}
}

func nestedInLists(element sqlmodel.SQLType, depth int) sqlmodel.SQLType {
	nested := element
	for range depth {
		nested = sqlmodel.SQLType{EngineName: "list", Category: sqlmodel.TypeCategoryArray, ElementType: new(nested)}
	}
	return nested
}

func TestAResultTypeNestedTooDeeplyToCheckIsUnreadable(t *testing.T) {
	t.Parallel()

	integer := sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger, Width: 32}
	unnamed := sqlmodel.SQLType{EngineName: "struct", Category: sqlmodel.TypeCategoryStruct, StructFields: []sqlmodel.StructField{
		{Name: "element1", SQLType: integer, Positional: true},
	}}
	testCases := []struct {
		name       string
		wantReason string
		sqlType    sqlmodel.SQLType
		unreadable bool
	}{
		{name: "an unnamed struct just inside the depth limit", sqlType: nestedInLists(unnamed, sqlmodel.MaxTypeDepth-1),
			unreadable: true, wantReason: unnamedStructReason},
		{name: "an unnamed struct past the depth limit", sqlType: nestedInLists(unnamed, sqlmodel.MaxTypeDepth+10),
			unreadable: true, wantReason: deepTypeReason},
		{name: "a readable type just inside the depth limit", sqlType: nestedInLists(integer, sqlmodel.MaxTypeDepth-1),
			unreadable: false},
		{name: "a readable type reaching the depth limit", sqlType: nestedInLists(integer, sqlmodel.MaxTypeDepth),
			unreadable: true, wantReason: deepTypeReason},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			problem, unreadable := New().UnreadableResult(testCase.sqlType)

			require.Equal(t, testCase.unreadable, unreadable)
			require.Equal(t, testCase.wantReason, problem.Reason)
		})
	}
}
