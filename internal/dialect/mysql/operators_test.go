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

package mysql

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestBinaryResultTypesTheDifferenceOfTwoTemporalValuesAsANumber(t *testing.T) {
	t.Parallel()

	engine := New()
	datetime := engine.NormaliseTypeName("datetime")
	fractional := engine.NormaliseTypeName("datetime", 3)
	timestamp := engine.NormaliseTypeName("timestamp")
	date := engine.NormaliseTypeName("date")
	timeOfDay := engine.NormaliseTypeName("time")

	testCases := []struct {
		name     string
		left     sqlmodel.SQLType
		right    sqlmodel.SQLType
		category sqlmodel.SQLTypeCategory
	}{
		{name: "datetime minus datetime", left: datetime, right: datetime, category: sqlmodel.TypeCategoryInteger},
		{name: "timestamp minus datetime", left: timestamp, right: datetime, category: sqlmodel.TypeCategoryInteger},
		{name: "date minus date", left: date, right: date, category: sqlmodel.TypeCategoryInteger},
		{name: "time minus time", left: timeOfDay, right: timeOfDay, category: sqlmodel.TypeCategoryInteger},
		{name: "datetime with fractional seconds", left: fractional, right: datetime, category: sqlmodel.TypeCategoryDecimal},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result, behaviour, typed := engine.BinaryResult("-", testCase.left, testCase.right)
			require.True(t, typed)
			require.Equal(t, testCase.category, result.Category)
			require.Equal(t, sqlmodel.FunctionNullableReturnsNullOnNull, behaviour)
		})
	}
}

func TestBinaryResultLeavesTemporalAdditionToTheDefault(t *testing.T) {
	t.Parallel()

	engine := New()
	datetime := engine.NormaliseTypeName("datetime")
	_, _, typed := engine.BinaryResult("+", datetime, datetime)
	require.False(t, typed)
}

func TestTheNullSafeEqualityIsANullTest(t *testing.T) {
	t.Parallel()

	require.Equal(t, ast.SQLFeatureIsNull, New().Info().OperatorFeatures["<=>"])
}
