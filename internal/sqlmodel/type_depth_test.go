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

package sqlmodel

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func wrapInArray(inner SQLType) SQLType {
	outer := NewSQLType(TypeCategoryArray, "array")
	outer.ElementType = &inner
	return outer
}

func wrapAsMapKey(inner SQLType) SQLType {
	outer := NewSQLType(TypeCategoryMap, "map")
	outer.KeyType = &inner
	return outer
}

func wrapInStructField(inner SQLType) SQLType {
	outer := NewSQLType(TypeCategoryStruct, "struct")
	outer.StructFields = []StructField{{Name: "field", SQLType: inner, Positional: false}}
	return outer
}

func wrapInUnionMember(inner SQLType) SQLType {
	outer := NewSQLType(TypeCategoryUnion, "union")
	outer.UnionMembers = []UnionMember{{Tag: "member", SQLType: inner}}
	return outer
}

func wrapInEachKindInTurn(inner SQLType) SQLType {
	switch {
	case inner.ElementType != nil:
		return wrapAsMapKey(inner)
	case inner.KeyType != nil:
		return wrapInStructField(inner)
	case len(inner.StructFields) > 0:
		return wrapInUnionMember(inner)
	default:
		return wrapInArray(inner)
	}
}

func typeNestedTo(depth int, wrap func(SQLType) SQLType) SQLType {
	sqlType := NewSQLType(TypeCategoryText, "text")
	for range depth {
		sqlType = wrap(sqlType)
	}
	return sqlType
}

func TestTypeTooDeepStopsAtMaxTypeDepth(t *testing.T) {
	t.Parallel()

	chains := []struct {
		wrap func(SQLType) SQLType
		name string
	}{
		{name: "element", wrap: wrapInArray},
		{name: "key", wrap: wrapAsMapKey},
		{name: "field", wrap: wrapInStructField},
		{name: "member", wrap: wrapInUnionMember},
		{name: "mixed", wrap: wrapInEachKindInTurn},
	}
	depths := []struct {
		depth    int
		tooDeep  bool
		position string
	}{
		{position: "one below the limit", depth: MaxTypeDepth - 1, tooDeep: false},
		{position: "at the limit", depth: MaxTypeDepth, tooDeep: true},
		{position: "one past the limit", depth: MaxTypeDepth + 1, tooDeep: true},
	}

	for _, chain := range chains {
		for _, boundary := range depths {
			t.Run(chain.name+" chain "+boundary.position, func(t *testing.T) {
				t.Parallel()

				assert.Equal(t, boundary.tooDeep, TypeTooDeep(new(typeNestedTo(boundary.depth, chain.wrap))))
			})
		}
	}
}

func TestTypeTooDeep(t *testing.T) {
	t.Parallel()

	scalar := NewSQLType(TypeCategoryInteger, "int4")
	deepInLaterField := NewSQLType(TypeCategoryStruct, "struct")
	deepInLaterField.StructFields = []StructField{
		{Name: "shallow", SQLType: scalar, Positional: false},
		{Name: "deep", SQLType: typeNestedTo(MaxTypeDepth, wrapInArray), Positional: false},
	}
	deepMapValue := wrapAsMapKey(scalar)
	deepMapValue.ElementType = new(typeNestedTo(MaxTypeDepth, wrapInArray))
	wideShallow := NewSQLType(TypeCategoryUnion, "union")
	for range 3 * MaxTypeDepth {
		wideShallow.UnionMembers = append(wideShallow.UnionMembers, UnionMember{Tag: "member", SQLType: scalar})
	}

	testCases := []struct {
		sqlType *SQLType
		name    string
		tooDeep bool
	}{
		{name: "a nil type is never too deep", sqlType: nil, tooDeep: false},
		{name: "a scalar is not too deep", sqlType: &scalar, tooDeep: false},
		{name: "a deep part in a later field is found", sqlType: &deepInLaterField, tooDeep: true},
		{name: "a deep map value is found beside a shallow key", sqlType: &deepMapValue, tooDeep: true},
		{name: "many shallow members are not too deep", sqlType: &wideShallow, tooDeep: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.tooDeep, TypeTooDeep(testCase.sqlType))
		})
	}
}
