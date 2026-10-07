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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/fault"
)

func TestCastNeedsAsATypeAndItsClosingParenthesis(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		wantMessage string
	}{
		{name: "a cast with no closing parenthesis", sql: "SELECT CAST(a AS INTEGER FROM t", wantMessage: "expected ')' to close CAST"},
		{name: "a cast with no AS", sql: "SELECT CAST(a INTEGER) FROM t", wantMessage: "expected AS and the type to convert to"},
		{name: "a cast with no type", sql: "SELECT CAST(a AS) FROM t", wantMessage: "expected a type"},
		{name: "a TRY_CAST with no type", sql: "SELECT TRY_CAST(a AS) FROM t", wantMessage: "expected a type"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := analyseQueryError(t, testCase.sql)

			require.ErrorIs(t, err, fault.ErrParse)
			var parseError *dialectkit.ParseError
			require.ErrorAs(t, err, &parseError)
			assert.Equal(t, testCase.wantMessage, parseError.Message)
		})
	}
}

func TestCastReadsTheTypeItConvertsTo(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		wantType string
	}{
		{name: "a plain type", sql: "SELECT CAST(a AS INTEGER) AS c FROM t", wantType: "integer"},
		{name: "a type with modifiers", sql: "SELECT CAST(a AS DECIMAL(10, 2)) AS c FROM t", wantType: "decimal( 10 , 2 )"},
		{name: "a list type", sql: "SELECT CAST(a AS VARCHAR[]) AS c FROM t", wantType: "varchar[]"},
		{name: "a type of two words", sql: "SELECT CAST(a AS DOUBLE PRECISION) AS c FROM t", wantType: "double precision"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, nil, testCase.sql)

			require.Len(t, analysis.OutputColumns, 1)
			cast, isCast := analysis.OutputColumns[0].Expression.(*ast.CastExpression)
			require.True(t, isCast)
			assert.Equal(t, testCase.wantType, cast.TypeName)
		})
	}
}

func TestStructPackOfNamedArgumentsIsAStructConstructor(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		sql        string
		wantNames  []string
		wantStruct bool
	}{
		{name: "every argument named", sql: "SELECT struct_pack(a := 1, b => 'x') AS packed", wantNames: []string{"a", "b"}, wantStruct: true},
		{name: "an argument passed by position", sql: "SELECT struct_pack(a := 1, 2) AS packed", wantStruct: false},
		{name: "a call qualified by a schema", sql: "SELECT main.struct_pack(a := 1) AS packed", wantStruct: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, nil, testCase.sql)

			structure, isStruct := analysis.OutputColumns[0].Expression.(*ast.StructExpression)
			require.Equal(t, testCase.wantStruct, isStruct)
			if !testCase.wantStruct {
				call, isCall := analysis.OutputColumns[0].Expression.(*ast.FunctionCallExpression)
				require.True(t, isCall)
				require.Equal(t, functionStructPack, call.FunctionName)
				return
			}
			require.Equal(t, testCase.wantNames, structure.Names)
		})
	}
}

func TestStructInsertGivesTheFieldsItAddsByNameAsOneStructConstructor(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		sql           string
		wantAdded     []string
		wantArguments int
	}{
		{name: "fields added by name", sql: "SELECT struct_insert({'a': 1}, b := 2, c => 3) AS extended",
			wantAdded: []string{"b", "c"}, wantArguments: 2},
		{name: "a value passed by position", sql: "SELECT struct_insert({'a': 1}, 2) AS extended", wantArguments: 2},
		{name: "a struct passed by name", sql: "SELECT struct_insert(s := {'a': 1}, b := 2) AS extended", wantArguments: 2},
		{name: "no field added", sql: "SELECT struct_insert({'a': 1}) AS extended", wantArguments: 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, nil, testCase.sql)

			call, isCall := analysis.OutputColumns[0].Expression.(*ast.FunctionCallExpression)
			require.True(t, isCall)
			require.Equal(t, functionStructInsert, call.FunctionName)
			require.Len(t, call.Arguments, testCase.wantArguments)
			if testCase.wantAdded == nil {
				if testCase.wantArguments > 1 {
					_, isStruct := call.Arguments[1].(*ast.StructExpression)
					require.False(t, isStruct)
				}
				return
			}
			added, isStruct := call.Arguments[1].(*ast.StructExpression)
			require.True(t, isStruct)
			require.Equal(t, testCase.wantAdded, added.Names)
		})
	}
}
