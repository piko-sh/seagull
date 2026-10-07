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

func sizedText(engineName string, length int) sqlmodel.SQLType {
	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, engineName)
	text.Length = new(length)
	return text
}

func exactNumber(precision, scale int) sqlmodel.SQLType {
	number := sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "numeric")
	number.Precision = new(precision)
	number.Scale = new(scale)
	return number
}

func addressType(engineName string, width int) sqlmodel.SQLType {
	address := sqlmodel.NewNetworkType(engineName, sqlmodel.NetworkKindAddress)
	address.Width = width
	return address
}

func arrayOf(element sqlmodel.SQLType) sqlmodel.SQLType {
	array := sqlmodel.NewSQLType(sqlmodel.TypeCategoryArray, "Array")
	array.ElementType = &element
	return array
}

func nullableType(sqlType sqlmodel.SQLType) sqlmodel.SQLType {
	sqlType.Nullable = true
	return sqlType
}

func structOf(fields ...sqlmodel.StructField) sqlmodel.SQLType {
	structure := sqlmodel.NewSQLType(sqlmodel.TypeCategoryStruct, "Tuple")
	structure.StructFields = fields
	return structure
}

func inSchema(sqlType sqlmodel.SQLType, schema string) sqlmodel.SQLType {
	sqlType.Schema = schema
	return sqlType
}

func TestADeclaredTypeRestatesOnlyTheColumnsOwnType(t *testing.T) {
	t.Parallel()

	integer := sqlmodel.NewIntegerType("int4", 32, false)
	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")
	mood := sqlmodel.NewSQLType(sqlmodel.TypeCategoryEnum, "mood")
	testCases := []struct {
		name     string
		declared sqlmodel.SQLType
		column   sqlmodel.SQLType
		restates bool
	}{
		{name: "the same type", declared: integer, column: integer, restates: true},
		{name: "the same name written in another case", declared: addressType("ipv4", 32), column: addressType("IPv4", 32), restates: true},
		{name: "another name of the category", declared: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar"), column: text, restates: false},
		{name: "another category", declared: text, column: integer, restates: false},
		{name: "another width of an address", declared: addressType("IPv4", 32), column: addressType("IPv4", 128), restates: false},
		{name: "another signedness", declared: sqlmodel.NewIntegerType("int4", 32, true), column: integer, restates: false},
		{name: "a declaration that leaves the length out", declared: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar"), column: sizedText("varchar", 8), restates: true},
		{name: "a declaration of the column's length", declared: sizedText("varchar", 8), column: sizedText("varchar", 8), restates: true},
		{name: "a declaration of another length", declared: sizedText("varchar", 4), column: sizedText("varchar", 8), restates: false},
		{name: "a declaration of the column's precision and scale", declared: exactNumber(10, 2), column: exactNumber(10, 2), restates: true},
		{name: "a declaration of another scale", declared: exactNumber(10, 4), column: exactNumber(10, 2), restates: false},
		{name: "a declaration of a precision the column leaves out", declared: exactNumber(10, 2), column: sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "numeric"), restates: false},
		{name: "a declaration admitting NULL of a column that does not", declared: nullableType(addressType("IPv4", 32)), column: addressType("IPv4", 32), restates: true},
		{name: "an array of the column's elements", declared: arrayOf(addressType("IPv4", 32)), column: arrayOf(addressType("IPv4", 32)), restates: true},
		{name: "an array of nullable elements for an array of values", declared: arrayOf(nullableType(addressType("IPv4", 32))), column: arrayOf(addressType("IPv4", 32)), restates: false},
		{name: "an array of the column's element type for the column", declared: arrayOf(addressType("IPv4", 32)), column: addressType("IPv4", 32), restates: false},
		{name: "a tuple of the column's fields", declared: structOf(sqlmodel.StructField{Name: "near", SQLType: integer}), column: structOf(sqlmodel.StructField{Name: "near", SQLType: integer}), restates: true},
		{name: "a tuple naming another field", declared: structOf(sqlmodel.StructField{Name: "far", SQLType: integer}), column: structOf(sqlmodel.StructField{Name: "near", SQLType: integer}), restates: false},
		{name: "a type without a schema for a column's type in one", declared: mood, column: inSchema(mood, "public"), restates: true},
		{name: "a type in another schema", declared: inSchema(mood, "audit"), column: inSchema(mood, "public"), restates: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.restates, RestatesColumnType(testCase.declared, testCase.column))
		})
	}
}

func TestADeclaredTypeRestatesAColumnsTypeOnlyWithinTheTypeDepthLimit(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		levels       int
		wantRestates bool
	}{
		{name: "a type nested within the limit", levels: sqlmodel.MaxTypeDepth - 1, wantRestates: true},
		{name: "a type nested past the limit", levels: sqlmodel.MaxTypeDepth, wantRestates: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			sqlType := sizedText("varchar", 10)
			for range testCase.levels {
				sqlType = arrayOf(sqlType)
			}

			require.Equal(t, testCase.wantRestates, RestatesColumnType(sqlType, sqlType))
		})
	}
}
