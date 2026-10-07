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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

var floatPrecisionCases = []struct {
	name       string
	written    string
	wantEngine string
	wantWidth  int
}{
	{name: "FLOAT is single precision", written: "FLOAT", wantEngine: "float4", wantWidth: 32},
	{name: "FLOAT4 is single precision", written: "FLOAT4", wantEngine: "float4", wantWidth: 32},
	{name: "REAL is single precision", written: "REAL", wantEngine: "float4", wantWidth: 32},
	{name: "FLOAT of 24 bits is single precision", written: "FLOAT(24)", wantEngine: "float4", wantWidth: 32},
	{name: "FLOAT of 25 bits is double precision", written: "FLOAT(25)", wantEngine: "float8", wantWidth: 64},
	{name: "FLOAT of 53 bits is double precision", written: "FLOAT(53)", wantEngine: "float8", wantWidth: 64},
	{name: "DOUBLE is double precision", written: "DOUBLE", wantEngine: "float8", wantWidth: 64},
	{name: "FLOAT8 is double precision", written: "FLOAT8", wantEngine: "float8", wantWidth: 64},
}

func TestNormaliseTypeNameReadsTypesWrittenWithTheirParts(t *testing.T) {
	t.Parallel()

	engine := New()
	tests := []struct {
		name  string
		check func(t *testing.T, sqlType sqlmodel.SQLType)
	}{
		{name: "MAP(VARCHAR, INTEGER)", check: func(t *testing.T, sqlType sqlmodel.SQLType) {
			t.Helper()
			require.Equal(t, sqlmodel.TypeCategoryMap, sqlType.Category)
			require.NotNil(t, sqlType.KeyType)
			require.Equal(t, sqlmodel.TypeCategoryText, sqlType.KeyType.Category)
			require.NotNil(t, sqlType.ElementType)
			require.Equal(t, sqlmodel.TypeCategoryInteger, sqlType.ElementType.Category)
		}},
		{name: "STRUCT(a INTEGER, b VARCHAR)", check: func(t *testing.T, sqlType sqlmodel.SQLType) {
			t.Helper()
			require.Equal(t, sqlmodel.TypeCategoryStruct, sqlType.Category)
			require.Len(t, sqlType.StructFields, 2)
			require.Equal(t, "b", sqlType.StructFields[1].Name)
			require.Equal(t, sqlmodel.TypeCategoryText, sqlType.StructFields[1].SQLType.Category)
		}},
		{name: "union(n integer, s varchar)", check: func(t *testing.T, sqlType sqlmodel.SQLType) {
			t.Helper()
			require.Equal(t, sqlmodel.TypeCategoryUnion, sqlType.Category)
			require.Len(t, sqlType.UnionMembers, 2)
		}},
		{name: "INTEGER[3]", check: func(t *testing.T, sqlType sqlmodel.SQLType) {
			t.Helper()
			require.Equal(t, sqlmodel.TypeCategoryArray, sqlType.Category)
			require.NotNil(t, sqlType.ElementType)
			require.Equal(t, sqlmodel.TypeCategoryInteger, sqlType.ElementType.Category)
		}},
		{name: "VARCHAR[][]", check: func(t *testing.T, sqlType sqlmodel.SQLType) {
			t.Helper()
			require.Equal(t, sqlmodel.TypeCategoryArray, sqlType.Category)
			require.Equal(t, sqlmodel.TypeCategoryArray, sqlType.ElementType.Category)
			require.Equal(t, sqlmodel.TypeCategoryText, sqlType.ElementType.ElementType.Category)
		}},
		{name: "MAP(VARCHAR, DOUBLE)[]", check: func(t *testing.T, sqlType sqlmodel.SQLType) {
			t.Helper()
			require.Equal(t, sqlmodel.TypeCategoryArray, sqlType.Category)
			require.Equal(t, sqlmodel.TypeCategoryMap, sqlType.ElementType.Category)
		}},
		{name: "DECIMAL(10, 2)", check: func(t *testing.T, sqlType sqlmodel.SQLType) {
			t.Helper()
			require.Equal(t, sqlmodel.TypeCategoryDecimal, sqlType.Category)
			require.NotNil(t, sqlType.Precision)
			require.Equal(t, 10, *sqlType.Precision)
			require.NotNil(t, sqlType.Scale)
			require.Equal(t, 2, *sqlType.Scale)
		}},
		{name: "STRUCT(a INTEGER", check: func(t *testing.T, sqlType sqlmodel.SQLType) {
			t.Helper()
			require.Equal(t, sqlmodel.TypeCategoryUnknown, sqlType.Category)
		}},
		{name: "INTEGER[3] trailing", check: func(t *testing.T, sqlType sqlmodel.SQLType) {
			t.Helper()
			require.Equal(t, sqlmodel.TypeCategoryUnknown, sqlType.Category)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			test.check(t, engine.NormaliseTypeName(test.name))
		})
	}
}

func TestFloatIsSinglePrecisionUnlessItsPrecisionNeedsADouble(t *testing.T) {
	t.Parallel()

	engine := New()
	for _, testCase := range floatPrecisionCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			for _, written := range []string{testCase.written, testCase.written + "[]", "STRUCT(x " + testCase.written + ")"} {
				sqlType := engine.NormaliseTypeName(written)
				switch sqlType.Category {
				case sqlmodel.TypeCategoryArray:
					require.NotNil(t, sqlType.ElementType, written)
					sqlType = *sqlType.ElementType
				case sqlmodel.TypeCategoryStruct:
					require.Len(t, sqlType.StructFields, 1, written)
					sqlType = sqlType.StructFields[0].SQLType
				default:
				}
				require.Equal(t, sqlmodel.TypeCategoryFloat, sqlType.Category, written)
				require.Equal(t, testCase.wantEngine, sqlType.EngineName, written)
				require.Equal(t, testCase.wantWidth, sqlType.Width, written)
			}
		})
	}
}

func TestNormaliseTypeNameLeavesATypeWrittenTooLongUnread(t *testing.T) {
	t.Parallel()

	fields := strings.Repeat("a integer, ", maxWrittenTypeRunes/len("a integer, ")+1)
	testCases := []struct {
		name         string
		written      string
		wantCategory sqlmodel.SQLTypeCategory
	}{
		{name: "a struct within the limit", written: "struct(a integer, b varchar)", wantCategory: sqlmodel.TypeCategoryStruct},
		{name: "a struct past the limit", written: "struct(" + fields + "b integer)", wantCategory: sqlmodel.TypeCategoryUnknown},
		{name: "a map missing its value type", written: "map(integer)", wantCategory: sqlmodel.TypeCategoryUnknown},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.wantCategory, New().NormaliseTypeName(testCase.written).Category)
		})
	}
}

func TestWrittenTypeReadBudgetStopsAParseOnceItsReadsAreSpent(t *testing.T) {
	t.Parallel()

	interrupt := writtenTypeReadBudget(2)
	for range writtenTypeReadsPerToken * 3 {
		require.NoError(t, interrupt())
	}

	require.ErrorIs(t, interrupt(), dialectkit.ErrWalkBudget)
	require.ErrorIs(t, interrupt(), dialectkit.ErrWalkBudget)
}
