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

	"github.com/stretchr/testify/require"
)

func TestTypeAttributes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		sqlType    SQLType
		structural bool
	}{
		{name: "an integer with a width", sqlType: NewIntegerType("WHOLE64", 64, false), structural: true},
		{name: "an integer without a width", sqlType: NewSQLType(TypeCategoryInteger, "WHOLE"), structural: false},
		{name: "a float with a width", sqlType: NewFloatType("REAL32", 32), structural: true},
		{name: "a float without a width", sqlType: NewSQLType(TypeCategoryFloat, "REAL"), structural: false},
		{name: "a temporal type with a kind", sqlType: NewTemporalType("DAYSTAMP", TemporalKindDate, false), structural: true},
		{name: "a temporal type without a kind", sqlType: NewSQLType(TypeCategoryTemporal, "MOMENT"), structural: false},
		{name: "a network type with a kind", sqlType: NewNetworkType("ADDRESS", NetworkKindAddress), structural: true},
		{name: "a network type without a kind", sqlType: NewSQLType(TypeCategoryNetwork, "ADDRESS"), structural: false},
		{name: "a text type needs no attribute", sqlType: NewSQLType(TypeCategoryText, "TEXTUAL"), structural: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.structural, testCase.sqlType.HasStructuralAttributes())
		})
	}

	unsigned := NewIntegerType("UWHOLE8", 8, true)
	require.True(t, unsigned.Unsigned)
	require.Equal(t, 8, unsigned.Width)
	zoned := NewTemporalType("MOMENTZ", TemporalKindTimestamp, true)
	require.True(t, zoned.TimeZone)
	require.Equal(t, "timestamp", zoned.Temporal.String())
	require.True(t, TemporalKindNone.IsZero())
	require.False(t, TemporalKindInterval.IsZero())
	require.Equal(t, "address", NetworkKindAddress.String())
	require.True(t, NetworkKindNone.IsZero())
	require.Equal(t, "TemporalKind(9)", TemporalKind(9).String())
}
