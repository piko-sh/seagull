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

package clickhouse

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
)

func placedParameterNumbers(analysis *ast.RawQueryAnalysis) []int {
	if analysis == nil {
		return nil
	}
	var numbers []int
	collect := func(expression ast.Expression) {
		for node := range ast.Preorder(expression) {
			switch typed := node.(type) {
			case *ast.ParameterExpression:
				numbers = append(numbers, typed.Number)
			case *ast.ScalarSubqueryExpression:
				numbers = append(numbers, placedParameterNumbers(typed.InnerQuery)...)
			case *ast.ExistsExpression:
				numbers = append(numbers, placedParameterNumbers(typed.InnerQuery)...)
			}
		}
	}
	for index := range analysis.OutputColumns {
		collect(analysis.OutputColumns[index].Expression)
	}
	for index := range analysis.ClauseExpressions {
		collect(analysis.ClauseExpressions[index].Expression)
	}
	for index := range analysis.CTEDefinitions {
		numbers = append(numbers, placedParameterNumbers(analysis.CTEDefinitions[index].Query)...)
	}
	for index := range analysis.RawDerivedTables {
		numbers = append(numbers, placedParameterNumbers(analysis.RawDerivedTables[index].InnerQuery)...)
	}
	for index := range analysis.CompoundBranches {
		numbers = append(numbers, placedParameterNumbers(analysis.CompoundBranches[index].Query)...)
	}
	for _, subquery := range analysis.PredicateSubqueries {
		numbers = append(numbers, placedParameterNumbers(subquery)...)
	}
	return numbers
}

func referenceNumbers(t *testing.T, analysis *ast.RawQueryAnalysis, names []string) []int {
	t.Helper()
	numbers := make([]int, 0, len(names))
	for _, name := range names {
		number := 0
		for index := range analysis.ParameterReferences {
			if analysis.ParameterReferences[index].Name == name {
				number = analysis.ParameterReferences[index].Number
				break
			}
		}
		require.NotZero(t, number, "the statement must register a parameter named %q", name)
		numbers = append(numbers, number)
	}
	return numbers
}

func TestPlaceholdersAreParametersNumberedAsTheStatementNumbersThem(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		sql   string
		names []string
	}{
		{
			name:  "select list and WHERE",
			sql:   "SELECT id + 1 = {first:Int32} AS matched FROM items WHERE id + 1 > {second:Int32}",
			names: []string{"first", "second"},
		},
		{
			name:  "a placeholder written twice is one parameter",
			sql:   "SELECT {id:UInt32} AS echoed FROM items WHERE id = {id:UInt32}",
			names: []string{"id", "id"},
		},
		{
			name:  "a placeholder in a lambda body",
			sql:   "SELECT arrayFilter(x -> x > {floor:UInt32}, scores) AS kept FROM items",
			names: []string{"floor"},
		},
		{
			name:  "a nullable placeholder",
			sql:   "SELECT ifNull({note:Nullable(String)}, name) AS label FROM items WHERE id = {id:UInt32}",
			names: []string{"note", "id"},
		},
		{
			name:  "a placeholder in a subquery of WHERE",
			sql:   "SELECT id FROM items WHERE id IN (SELECT id FROM others WHERE score > {score:UInt32}) AND id < {limit:UInt32}",
			names: []string{"score", "limit"},
		},
		{
			name:  "a placeholder in a CTE",
			sql:   "WITH low AS (SELECT id FROM items WHERE id > {low:UInt32}) SELECT id FROM low WHERE id < {high:UInt32}",
			names: []string{"high", "low"},
		},
		{
			name:  "a placeholder in a derived table numbered after the select list",
			sql:   "SELECT {shift:UInt32} AS shift, total FROM (SELECT id + {step:UInt32} AS total FROM items) WHERE total > {floor:UInt32}",
			names: []string{"shift", "floor", "step"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyse(t, testCase.sql)
			require.Equal(t, referenceNumbers(t, analysis, testCase.names), placedParameterNumbers(analysis))
		})
	}
}

func describeDeclaredParameters(references []ast.RawParameterReference) []string {
	described := make([]string, 0, len(references))
	for _, reference := range references {
		column, declared := "-", "-"
		if reference.ColumnReference != nil {
			column = reference.ColumnReference.TableAlias + "." + reference.ColumnReference.ColumnName
		}
		if reference.CastType != nil {
			declared = reference.CastType.EngineName
		}
		described = append(described, fmt.Sprintf("%d %s %s %s", reference.Number, reference.Context, column, declared))
	}
	return described
}

func TestAPlaceholderThatIsAWholeAssignedValueIsRecordedAgainstItsColumn(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		expected []string
	}{
		{
			name:     "a placeholder alone in a VALUES row",
			sql:      "INSERT INTO hosts (id, addr) VALUES ({id:UInt64}, {addr:IPv4}), ({second:UInt64}, {other:IPv4})",
			expected: []string{"1 assignment hosts.id UInt64", "2 assignment hosts.addr IPv4", "3 assignment hosts.id UInt64", "4 assignment hosts.addr IPv4"},
		},
		{
			name:     "a placeholder inside an expression of a VALUES row",
			sql:      "INSERT INTO hosts (id, addr) VALUES ({id:UInt64} + 1, toIPv4({text:String}))",
			expected: []string{"1 assignment - UInt64", "2 assignment - String"},
		},
		{
			name:     "a VALUES row longer than the column list",
			sql:      "INSERT INTO hosts (id) VALUES ({id:UInt64}, {extra:UInt64})",
			expected: []string{"1 assignment hosts.id UInt64", "2 assignment - UInt64"},
		},
		{
			name:     "a placeholder an ALTER TABLE UPDATE assigns alone",
			sql:      "ALTER TABLE hosts UPDATE addr = {addr:IPv4}, id = {id:UInt64} + 1 WHERE id = {old:UInt64}",
			expected: []string{"1 assignment hosts.addr IPv4", "2 assignment - UInt64", "3 assignment - UInt64"},
		},
		{
			name:     "a placeholder a lightweight UPDATE assigns alone",
			sql:      "UPDATE hosts SET addr = {addr:IPv4} WHERE id = {id:UInt64}",
			expected: []string{"1 assignment hosts.addr IPv4", "2 comparison - UInt64"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, _ := analyseStatement(t, testCase.sql)

			require.Equal(t, testCase.expected, describeDeclaredParameters(analysis.ParameterReferences))
		})
	}
}

func TestThePlaceholdersOfEveryArmOfACompoundStatementAreItsParameters(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		sql   string
		names []string
	}{
		{
			name:  "a placeholder in each arm",
			sql:   "SELECT id FROM items WHERE id = {first:Int32} UNION ALL SELECT id FROM items WHERE id = {second:Int32}",
			names: []string{"first", "second"},
		},
		{
			name:  "a placeholder in the second arm only",
			sql:   "SELECT id FROM items UNION ALL SELECT id FROM items WHERE id = {only:Int32}",
			names: []string{"only"},
		},
		{
			name:  "a placeholder in the third arm of a chain",
			sql:   "SELECT id FROM items UNION DISTINCT SELECT id FROM items EXCEPT SELECT id FROM items WHERE id = {last:Int32}",
			names: []string{"last"},
		},
		{
			name:  "a placeholder repeated across arms is one parameter",
			sql:   "SELECT id FROM items WHERE id = {id:Int32} UNION ALL SELECT id FROM items WHERE id = {id:Int32}",
			names: []string{"id", "id"},
		},
		{
			name:  "the arms of a derived table",
			sql:   "SELECT id FROM (SELECT id FROM items UNION ALL SELECT id FROM items WHERE id = {inner:Int32}) WHERE id > {outer:Int32}",
			names: []string{"inner", "outer"},
		},
		{
			name:  "the arms of a CTE",
			sql:   "WITH found AS (SELECT id FROM items UNION ALL SELECT id FROM items WHERE id = {inner:Int32}) SELECT id FROM found",
			names: []string{"inner"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyse(t, testCase.sql)
			named := make([]string, 0, len(analysis.ParameterReferences))
			for _, reference := range analysis.ParameterReferences {
				named = append(named, reference.Name)
			}
			require.ElementsMatch(t, testCase.names, named)
		})
	}
}

func TestNullableParamRecordsNullability(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "INSERT INTO t (id, pid, label) VALUES ({id:UInt64}, {pid:Nullable(Int64)}, {label:Nullable(String)})")
	require.Len(t, analysis.ParameterReferences, 3)

	type castInfo struct {
		engineName string
		nullable   bool
	}
	byName := map[string]castInfo{}
	for index := range analysis.ParameterReferences {
		reference := analysis.ParameterReferences[index]
		require.NotNilf(t, reference.CastType, "param %q must have a cast type", reference.Name)
		byName[reference.Name] = castInfo{nullable: reference.CastType.Nullable, engineName: reference.CastType.EngineName}
	}

	assert.False(t, byName["id"].nullable, "UInt64 is not nullable")
	assert.True(t, byName["pid"].nullable, "Nullable(Int64) must record nullability")
	assert.True(t, byName["label"].nullable, "Nullable(String) must record nullability")
	assert.Equal(t, "Int64", byName["pid"].engineName, "Nullable(Int64) unwraps to its inner type")
}
