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

package goast

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
)

func TestNullableOverrideLeavesANullableArrayASlice(t *testing.T) {
	t.Parallel()

	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")
	array := sqlmodel.NewSQLType(sqlmodel.TypeCategoryArray, "text[]")
	array.ElementType = &text
	document := sqlmodel.NewSQLType(sqlmodel.TypeCategoryJSON, "jsonb")
	testCases := []struct {
		name     string
		override *sqlmodel.GoType
		sqlType  sqlmodel.SQLType
		nullable bool
		want     *sqlmodel.GoType
	}{
		{name: "no override", override: nil, sqlType: text, nullable: true, want: nil},
		{name: "a NOT NULL value keeps the type", override: &sqlmodel.GoType{Package: "example.com/app", Name: "Label"}, sqlType: text, want: &sqlmodel.GoType{Package: "example.com/app", Name: "Label"}},
		{name: "a nullable value becomes a pointer", override: &sqlmodel.GoType{Package: "example.com/app", Name: "Label"}, sqlType: text, nullable: true, want: &sqlmodel.GoType{Package: "example.com/app", Name: "*Label"}},
		{name: "a pointer stays one pointer", override: &sqlmodel.GoType{Package: "example.com/app", Name: "*Label"}, sqlType: text, nullable: true, want: &sqlmodel.GoType{Package: "example.com/app", Name: "*Label"}},
		{name: "a nullable array stays a slice, since a nil slice is NULL", override: &sqlmodel.GoType{Package: "example.com/app", Name: "[]*Label"}, sqlType: array, nullable: true, want: &sqlmodel.GoType{Package: "example.com/app", Name: "[]*Label"}},
		{name: "a slice held in a nullable document becomes a pointer", override: &sqlmodel.GoType{Name: "[]string"}, sqlType: document, nullable: true, want: &sqlmodel.GoType{Name: "*[]string"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.want, NullableOverride(testCase.override, testCase.sqlType, testCase.nullable))
		})
	}
}
