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

func TestUnaryResultTypesNotAndLeavesEveryOtherPrefixOperator(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		operator  string
		wantTyped bool
	}{
		{name: "NOT in capitals", operator: "NOT", wantTyped: true},
		{name: "not in lower case", operator: "not", wantTyped: true},
		{name: "a minus sign", operator: "-", wantTyped: false},
		{name: "a plus sign", operator: "+", wantTyped: false},
		{name: "an operator spelt like a struct field", operator: "$struct_field:a", wantTyped: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result, behaviour, typed := unaryResult(testCase.operator)

			require.Equal(t, testCase.wantTyped, typed)
			if testCase.wantTyped {
				require.Equal(t, sqlmodel.TypeCategoryBoolean, result.Category)
				require.Equal(t, sqlmodel.FunctionNullableReturnsNullOnNull, behaviour)
			}
		})
	}
}

func TestBinaryResultLeavesAnOperatorItDoesNotKnowToTheStructuralDefault(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		operator string
		left     sqlmodel.SQLType
	}{
		{name: "addition", operator: "+", left: builtinTypeMap["integer"]},
		{name: "an operator spelt like a struct field on a struct", operator: "$struct_field:a",
			left: sqlmodel.NewSQLType(sqlmodel.TypeCategoryStruct, "struct")},
		{name: "an operator spelt like a struct field on a value of unknown type", operator: "$struct_field:a",
			left: sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, _, typed := binaryResult(testCase.operator, testCase.left, builtinTypeMap["integer"])

			require.False(t, typed)
		})
	}
}
