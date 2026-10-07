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

package resolve

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
)

func elementTestArray(element sqlmodel.SQLType) sqlmodel.SQLType {
	array := sqlmodel.SQLType{}
	array.Category = sqlmodel.TypeCategoryArray
	array.EngineName = element.EngineName + "[]"
	array.ElementType = &element
	return array
}

func elementTestMap(key, value sqlmodel.SQLType) sqlmodel.SQLType {
	mapped := sqlmodel.SQLType{}
	mapped.Category = sqlmodel.TypeCategoryMap
	mapped.EngineName = "map"
	mapped.KeyType = &key
	mapped.ElementType = &value
	return mapped
}

func elementTestStruct(field sqlmodel.SQLType) sqlmodel.SQLType {
	structType := sqlmodel.SQLType{}
	structType.Category = sqlmodel.TypeCategoryStruct
	structType.EngineName = "struct"
	structType.StructFields = []sqlmodel.StructField{{Name: "readings", SQLType: field, Positional: false}}
	return structType
}

func holdsEveryElement(sqlType sqlmodel.SQLType) bool {
	return sqlType.Category == sqlmodel.TypeCategoryArray || sqlType.Category == sqlmodel.TypeCategoryMap
}

func TestElementNullabilityMarksTheElementsTheEngineCanHoldNullIn(t *testing.T) {
	t.Parallel()

	integer := sqlmodel.NewIntegerType("int4", 32, false)
	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")
	nullableText := text
	nullableText.Nullable = true
	for _, testCase := range []struct {
		sqlType      sqlmodel.SQLType
		column       *bool
		holds        func(sqlmodel.SQLType) bool
		elementPaths func(sqlmodel.SQLType) []bool
		name         string
		typeChoices  []ElementTypeChoice
		presetChoice bool
	}{
		{
			name:         "an array's elements become nullable by default",
			sqlType:      elementTestArray(integer),
			holds:        holdsEveryElement,
			elementPaths: func(result sqlmodel.SQLType) []bool { return []bool{result.ElementType.Nullable} },
		},
		{
			name:         "the preset declares them non-null",
			sqlType:      elementTestArray(integer),
			holds:        holdsEveryElement,
			presetChoice: true,
			elementPaths: func(result sqlmodel.SQLType) []bool { return []bool{!result.ElementType.Nullable} },
		},
		{
			name:         "the column declares them non-null",
			sqlType:      elementTestArray(integer),
			column:       new(true),
			holds:        holdsEveryElement,
			elementPaths: func(result sqlmodel.SQLType) []bool { return []bool{!result.ElementType.Nullable} },
		},
		{
			name:         "the column declares them nullable over the preset",
			sqlType:      elementTestArray(integer),
			column:       new(false),
			holds:        holdsEveryElement,
			presetChoice: true,
			elementPaths: func(result sqlmodel.SQLType) []bool { return []bool{result.ElementType.Nullable} },
		},
		{
			name:         "a type override declares them non-null",
			sqlType:      elementTestArray(integer),
			holds:        holdsEveryElement,
			typeChoices:  []ElementTypeChoice{{SQLType: elementTestArray(sqlmodel.NewIntegerType("INT4", 32, false)), NonNull: true}},
			elementPaths: func(result sqlmodel.SQLType) []bool { return []bool{!result.ElementType.Nullable} },
		},
		{
			name:         "a type override of another type leaves them to the default",
			sqlType:      elementTestArray(integer),
			holds:        holdsEveryElement,
			typeChoices:  []ElementTypeChoice{{SQLType: elementTestArray(text), NonNull: true}},
			elementPaths: func(result sqlmodel.SQLType) []bool { return []bool{result.ElementType.Nullable} },
		},
		{
			name:         "an element the analysis shows nullable stays nullable when declared non-null",
			sqlType:      elementTestArray(nullableText),
			holds:        holdsEveryElement,
			presetChoice: true,
			elementPaths: func(result sqlmodel.SQLType) []bool { return []bool{result.ElementType.Nullable} },
		},
		{
			name:         "an engine whose types say where NULL can be keeps them",
			sqlType:      elementTestArray(integer),
			holds:        nil,
			elementPaths: func(result sqlmodel.SQLType) []bool { return []bool{!result.ElementType.Nullable} },
		},
		{
			name:    "nested arrays, map values and struct fields are marked, map keys never",
			sqlType: elementTestStruct(elementTestMap(text, elementTestArray(elementTestArray(integer)))),
			holds:   holdsEveryElement,
			elementPaths: func(result sqlmodel.SQLType) []bool {
				mapped := result.StructFields[0].SQLType
				outer := mapped.ElementType
				return []bool{!mapped.KeyType.Nullable, outer.Nullable, outer.ElementType.Nullable, outer.ElementType.ElementType.Nullable}
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			policy := NewElementNullability(testCase.holds, testCase.presetChoice, testCase.typeChoices)
			result := policy.Apply(testCase.sqlType, testCase.column)

			for index, holds := range testCase.elementPaths(result) {
				require.True(t, holds, "element check %d", index)
			}
		})
	}
}

func TestElementNullabilityLeavesTheTypeItWasGiven(t *testing.T) {
	t.Parallel()

	element := sqlmodel.NewIntegerType("int4", 32, false)
	array := elementTestArray(element)

	marked := NewElementNullability(holdsEveryElement, false, nil).Apply(array, nil)

	require.True(t, marked.ElementType.Nullable)
	require.False(t, array.ElementType.Nullable)
}

func nestedArray(element sqlmodel.SQLType, levels int) sqlmodel.SQLType {
	for range levels {
		element = elementTestArray(element)
	}
	return element
}

func nullableLevels(sqlType sqlmodel.SQLType) int {
	levels := 0
	for sqlType.ElementType != nil && sqlType.ElementType.Nullable {
		levels++
		sqlType = *sqlType.ElementType
	}
	return levels
}

func TestElementNullabilityStopsAtTheTypeDepthLimit(t *testing.T) {
	t.Parallel()

	integer := sqlmodel.NewIntegerType("int4", 32, false)
	testCases := []struct {
		name       string
		levels     int
		wantMarked int
	}{
		{name: "arrays nested within the limit", levels: 3, wantMarked: 3},
		{name: "arrays nested to the limit", levels: sqlmodel.MaxTypeDepth, wantMarked: sqlmodel.MaxTypeDepth},
		{name: "arrays nested past the limit", levels: sqlmodel.MaxTypeDepth + 6, wantMarked: sqlmodel.MaxTypeDepth},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			marked := NewElementNullability(holdsEveryElement, false, nil).Apply(nestedArray(integer, testCase.levels), nil)

			require.Equal(t, testCase.wantMarked, nullableLevels(marked), "the levels past the limit are left as they are")
		})
	}
}

func TestATypeChoiceMatchesTypesOnlyWithinTheTypeDepthLimit(t *testing.T) {
	t.Parallel()

	integer := sqlmodel.NewIntegerType("int4", 32, false)
	testCases := []struct {
		name       string
		levels     int
		wantChosen bool
	}{
		{name: "a type nested within the limit", levels: sqlmodel.MaxTypeDepth - 1, wantChosen: true},
		{name: "a type nested past the limit", levels: sqlmodel.MaxTypeDepth, wantChosen: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			sqlType := nestedArray(integer, testCase.levels)
			policy := NewElementNullability(holdsEveryElement, false, []ElementTypeChoice{{SQLType: sqlType, NonNull: true}})

			require.Equal(t, testCase.wantChosen, policy.declaredNonNull(sqlType, nil))
		})
	}
}

func TestDeclaredOnlyKeepsElementsPlainUnlessTheValueDeclaresThemNullable(t *testing.T) {
	t.Parallel()

	integer := sqlmodel.NewIntegerType("int4", 32, false)
	testCases := []struct {
		policy       *ElementNullability
		column       *bool
		name         string
		wantNullable bool
	}{
		{name: "no policy", policy: nil, column: nil, wantNullable: false},
		{name: "a preset and a type override that make elements nullable", column: nil, wantNullable: false,
			policy: NewElementNullability(holdsEveryElement, false, []ElementTypeChoice{{SQLType: elementTestArray(integer), NonNull: false}})},
		{name: "a value that declares its elements nullable", column: new(false), wantNullable: true,
			policy: NewElementNullability(holdsEveryElement, false, nil)},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			declaredOnly := testCase.policy.DeclaredOnly()
			if testCase.policy == nil {
				require.Nil(t, declaredOnly)
			}

			marked := declaredOnly.Apply(elementTestArray(integer), testCase.column)

			require.Equal(t, testCase.wantNullable, marked.ElementType.Nullable)
		})
	}
}
