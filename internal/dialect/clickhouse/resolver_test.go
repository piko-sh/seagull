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
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestSplitCombinatorSuffixesRecognisesAllSuffixes(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		input        string
		expectedBase string
		expectedTail []string
	}{
		"countIf":              {input: "countIf", expectedBase: "count", expectedTail: []string{"If"}},
		"sumIf":                {input: "sumIf", expectedBase: "sum", expectedTail: []string{"If"}},
		"sumOrNull":            {input: "sumOrNull", expectedBase: "sum", expectedTail: []string{"OrNull"}},
		"sumOrDefault":         {input: "sumOrDefault", expectedBase: "sum", expectedTail: []string{"OrDefault"}},
		"sumArray":             {input: "sumArray", expectedBase: "sum", expectedTail: []string{"Array"}},
		"sumResample":          {input: "sumResample", expectedBase: "sum", expectedTail: []string{"Resample"}},
		"sumDistinct":          {input: "sumDistinct", expectedBase: "sum", expectedTail: []string{"Distinct"}},
		"sumMap":               {input: "sumMap", expectedBase: "sum", expectedTail: []string{"Map"}},
		"sumForEach":           {input: "sumForEach", expectedBase: "sum", expectedTail: []string{"ForEach"}},
		"sumState":             {input: "sumState", expectedBase: "sum", expectedTail: []string{"State"}},
		"sumMerge":             {input: "sumMerge", expectedBase: "sum", expectedTail: []string{"Merge"}},
		"countIfOrNull":        {input: "countIfOrNull", expectedBase: "count", expectedTail: []string{"If", "OrNull"}},
		"sumArrayOrDefault":    {input: "sumArrayOrDefault", expectedBase: "sum", expectedTail: []string{"Array", "OrDefault"}},
		"BareNameUntouched":    {input: "sum", expectedBase: "sum", expectedTail: nil},
		"EmptyName":            {input: "", expectedBase: "", expectedTail: nil},
		"UnknownSuffixNoSplit": {input: "sumUnknown", expectedBase: "sumUnknown", expectedTail: nil},
	}
	for testName, testCase := range cases {
		input := testCase.input
		expectedBase := testCase.expectedBase
		expectedTail := testCase.expectedTail
		t.Run(testName, func(testRunner *testing.T) {
			testRunner.Parallel()
			gotBase, gotTail := splitCombinatorSuffixes(input)
			if gotBase != expectedBase {
				testRunner.Errorf("base mismatch: want %q got %q", expectedBase, gotBase)
			}
			if len(gotTail) != len(expectedTail) {
				testRunner.Fatalf("tail length mismatch: want %v got %v", expectedTail, gotTail)
			}
			for index := range expectedTail {
				if gotTail[index] != expectedTail[index] {
					testRunner.Errorf("tail[%d] mismatch: want %q got %q", index, expectedTail[index], gotTail[index])
				}
			}
		})
	}
}

func TestResolveArrayMapReturnsArrayOfLambdaResult(t *testing.T) {
	t.Parallel()
	resolver := newFunctionResolver()
	argumentTypes := []sqlmodel.SQLType{
		{Category: sqlmodel.TypeCategoryInteger, EngineName: "UInt32"},
		{Category: sqlmodel.TypeCategoryArray, EngineName: "Array", ElementType: &sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "String"}},
	}
	resolution, err := resolver.ResolveFunctionCall(t.Context(), nil, "arrayMap", "", argumentTypes)
	require.NoError(t, err)
	require.NotNil(t, resolution, "expected a resolution for arrayMap")
	assert.Equal(t, sqlmodel.TypeCategoryArray, resolution.ReturnType.Category, "expected array return")
}

func TestResolveArrayFilterReturnsInputArrayType(t *testing.T) {
	t.Parallel()
	resolver := newFunctionResolver()
	argumentTypes := []sqlmodel.SQLType{
		{Category: sqlmodel.TypeCategoryBoolean, EngineName: "Bool"},
		{Category: sqlmodel.TypeCategoryArray, EngineName: "Array", ElementType: new(sqlmodel.NewIntegerType("UInt64", 64, true))},
	}
	resolution, err := resolver.ResolveFunctionCall(t.Context(), nil, "arrayFilter", "", argumentTypes)
	require.NoError(t, err)
	require.NotNil(t, resolution, "expected a resolution for arrayFilter")
	assert.Equal(t, sqlmodel.TypeCategoryArray, resolution.ReturnType.Category, "expected array return")
}

func TestResolveArrayJoinReturnsElementType(t *testing.T) {
	t.Parallel()
	resolver := newFunctionResolver()
	argumentTypes := []sqlmodel.SQLType{
		{
			Category:    sqlmodel.TypeCategoryArray,
			EngineName:  "Array",
			ElementType: new(sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "String"}),
		},
	}
	resolution, err := resolver.ResolveFunctionCall(t.Context(), nil, "arrayJoin", "", argumentTypes)
	require.NoError(t, err)
	require.NotNil(t, resolution, "expected a resolution for arrayJoin")
	assert.Equal(t, sqlmodel.TypeCategoryText, resolution.ReturnType.Category, "expected string return (element of Array(String))")
}

func TestResolveIfReturnsCommonType(t *testing.T) {
	t.Parallel()
	resolver := newFunctionResolver()
	argumentTypes := []sqlmodel.SQLType{
		{Category: sqlmodel.TypeCategoryBoolean, EngineName: "Bool"},
		{Category: sqlmodel.TypeCategoryText, EngineName: "String"},
		{Category: sqlmodel.TypeCategoryText, EngineName: "String"},
	}
	resolution, err := resolver.ResolveFunctionCall(t.Context(), nil, "if", "", argumentTypes)
	require.NoError(t, err)
	require.NotNil(t, resolution, "expected a resolution for if(bool, String, String)")
	assert.Equal(t, sqlmodel.TypeCategoryText, resolution.ReturnType.Category, "expected String return type")
}

func TestResolveIfMixedSignEqualWidthWidensToSigned(t *testing.T) {
	t.Parallel()
	resolver := newFunctionResolver()
	cases := []struct {
		then     string
		other    string
		expected string
	}{
		{then: "Int32", other: "UInt32", expected: "Int64"},
		{then: "UInt32", other: "Int32", expected: "Int64"},
		{then: "Int8", other: "UInt8", expected: "Int16"},
		{then: "UInt16", other: "Int16", expected: "Int32"},
	}
	for _, testCase := range cases {
		t.Run(testCase.then+"_"+testCase.other, func(subtest *testing.T) {
			subtest.Parallel()
			argumentTypes := []sqlmodel.SQLType{
				{Category: sqlmodel.TypeCategoryBoolean, EngineName: "Bool"},
				{Category: sqlmodel.TypeCategoryInteger, EngineName: testCase.then},
				{Category: sqlmodel.TypeCategoryInteger, EngineName: testCase.other},
			}
			resolution, err := resolver.ResolveFunctionCall(t.Context(), nil, "if", "", argumentTypes)
			if err != nil {
				subtest.Fatalf("unexpected error: %v", err)
			}
			if resolution == nil {
				subtest.Fatal("expected a resolution for if over mixed-sign integers")
			}
			if resolution.ReturnType.EngineName != testCase.expected {
				subtest.Errorf("expected %s for mixed-sign if, got %q",
					testCase.expected, resolution.ReturnType.EngineName)
			}
		})
	}
}

func TestResolveIfMixedSign64BitKeepsLeftOperand(t *testing.T) {
	t.Parallel()
	resolver := newFunctionResolver()
	argumentTypes := []sqlmodel.SQLType{
		{Category: sqlmodel.TypeCategoryBoolean, EngineName: "Bool"},
		{Category: sqlmodel.TypeCategoryInteger, EngineName: "Int64"},
		{Category: sqlmodel.TypeCategoryInteger, EngineName: "UInt64"},
	}
	resolution, err := resolver.ResolveFunctionCall(t.Context(), nil, "if", "", argumentTypes)
	require.NoError(t, err)
	require.NotNil(t, resolution, "expected a resolution for if(bool, Int64, UInt64)")
	assert.Equal(t, "Int64", resolution.ReturnType.EngineName,
		"Int64 vs UInt64 is out of scope (stock ClickHouse throws); expected the left operand Int64")
}

func TestResolveCoalesceReturnsFirstNonNullType(t *testing.T) {
	t.Parallel()
	resolver := newFunctionResolver()
	argumentTypes := []sqlmodel.SQLType{
		{Category: sqlmodel.TypeCategoryText, EngineName: "String"},
		{Category: sqlmodel.TypeCategoryText, EngineName: "String"},
	}
	resolution, err := resolver.ResolveFunctionCall(t.Context(), nil, "coalesce", "", argumentTypes)
	require.NoError(t, err)
	require.NotNil(t, resolution, "expected resolution for coalesce")
	assert.Equal(t, sqlmodel.TypeCategoryText, resolution.ReturnType.Category, "expected String")
}

func TestResolveIdentityReturnsFirstArgumentType(t *testing.T) {
	t.Parallel()
	resolver := newFunctionResolver()
	argumentTypes := []sqlmodel.SQLType{
		{Category: sqlmodel.TypeCategoryInteger, EngineName: "UInt32"},
	}
	for _, name := range []string{"any", "anyLast", "anyHeavy", "min", "max"} {
		resolution, err := resolver.ResolveFunctionCall(t.Context(), nil, name, "", argumentTypes)
		require.NoError(t, err, "%s", name)
		if !assert.NotNil(t, resolution, "expected resolution for %s", name) {
			continue
		}
		assert.Equal(t, sqlmodel.TypeCategoryInteger, resolution.ReturnType.Category, "expected integer for %s", name)
	}
}

func TestResolveArgMinMaxReturnsFirstArgumentType(t *testing.T) {
	t.Parallel()
	resolver := newFunctionResolver()
	argumentTypes := []sqlmodel.SQLType{
		{Category: sqlmodel.TypeCategoryText, EngineName: "String"},
		{Category: sqlmodel.TypeCategoryInteger, EngineName: "UInt32"},
	}
	for _, name := range []string{"argMin", "argMax"} {
		resolution, err := resolver.ResolveFunctionCall(t.Context(), nil, name, "", argumentTypes)
		require.NoError(t, err, "%s", name)
		if !assert.NotNil(t, resolution, "expected resolution for %s", name) {
			continue
		}
		assert.Equal(t, sqlmodel.TypeCategoryText, resolution.ReturnType.Category, "expected first-arg type (String) for %s", name)
	}
}

func TestResolveTupleElementReturnsNthFieldType(t *testing.T) {
	t.Parallel()
	resolver := newFunctionResolver()
	tupleType := sqlmodel.SQLType{
		Category:   sqlmodel.TypeCategoryStruct,
		EngineName: "Tuple",
		StructFields: []sqlmodel.StructField{
			{Name: "_1", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}},
			{Name: "_2", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}},
		},
	}
	argumentTypes := []sqlmodel.SQLType{
		tupleType,
		{Category: sqlmodel.TypeCategoryInteger, EngineName: "UInt8"},
	}
	resolution, err := resolver.ResolveFunctionCall(t.Context(), nil, "tupleElement", "", argumentTypes)
	require.NoError(t, err)
	require.NotNil(t, resolution, "expected resolution for tupleElement")

}

func TestUnknownFunctionReturnsNilResolution(t *testing.T) {
	t.Parallel()
	resolver := newFunctionResolver()
	resolution, err := resolver.ResolveFunctionCall(t.Context(), nil, "definitelyNotAFunction", "", nil)
	require.ErrorIs(t, err, errResolverNoOpinion)
	assert.Nil(t, resolution, "expected nil resolution")
}

func TestApplyCombinatorOrNullMarksNullable(t *testing.T) {
	t.Parallel()
	base := &ast.FunctionResolution{
		ReturnType: sqlmodel.NewIntegerType("UInt64", 64, true),
	}
	transformed := applyCombinator(base, "OrNull", combinatorCall{})
	require.NotNil(t, transformed, "expected non-nil resolution")
	assert.Equal(t, sqlmodel.TypeCategoryInteger, transformed.ReturnType.Category, "OrNull should preserve the category")
	assert.Equal(t, sqlmodel.FunctionNullableCalledOnNull, transformed.NullableBehaviour, "OrNull should make the result NULL when the aggregate sees no rows")
}

func TestApplyCombinatorArrayKeepsReturnType(t *testing.T) {
	t.Parallel()
	base := &ast.FunctionResolution{
		ReturnType: sqlmodel.NewIntegerType("UInt64", 64, true),
	}
	transformed := applyCombinator(base, "Array", combinatorCall{})
	require.NotNil(t, transformed, "expected non-nil resolution")
	assert.Equal(t, sqlmodel.TypeCategoryInteger, transformed.ReturnType.Category, "Array combinator (aggregate-over-array) keeps the base return type")
}

func TestApplyCombinatorResampleWrapsReturnInArray(t *testing.T) {
	t.Parallel()
	base := &ast.FunctionResolution{
		ReturnType: sqlmodel.NewIntegerType("UInt64", 64, true),
	}
	transformed := applyCombinator(base, "Resample", combinatorCall{})
	require.NotNil(t, transformed, "expected non-nil resolution")
	assert.Equal(t, sqlmodel.TypeCategoryArray, transformed.ReturnType.Category, "expected Resample to wrap return in Array")
}

func TestApplyCombinatorStateConvertsToAggregateFunction(t *testing.T) {
	t.Parallel()
	base := &ast.FunctionResolution{
		ReturnType: sqlmodel.NewIntegerType("UInt64", 64, true),
	}
	transformed := applyCombinator(base, "State", combinatorCall{})
	require.NotNil(t, transformed, "expected non-nil resolution")
	assert.Equal(t, "AggregateFunction", transformed.ReturnType.EngineName, "expected EngineName = AggregateFunction")
}

func TestApplyCombinatorIfDoesNotChangeReturn(t *testing.T) {
	t.Parallel()
	base := &ast.FunctionResolution{
		ReturnType: sqlmodel.NewIntegerType("UInt64", 64, true),
	}
	transformed := applyCombinator(base, "If", combinatorCall{})
	require.NotNil(t, transformed, "expected non-nil resolution")
	assert.Equal(t, sqlmodel.TypeCategoryInteger, transformed.ReturnType.Category, "If combinator should not change return type")
}

func TestApplyCombinatorOrDefaultPreservesReturn(t *testing.T) {
	t.Parallel()
	base := &ast.FunctionResolution{
		ReturnType: sqlmodel.NewIntegerType("UInt64", 64, true),
	}
	transformed := applyCombinator(base, "OrDefault", combinatorCall{})
	require.NotNil(t, transformed, "expected non-nil resolution")
	assert.False(t, transformed.ReturnType.Nullable, "OrDefault should keep result non-nullable (default fills the slot)")
}

func TestApplyCombinatorDistinctPreservesReturn(t *testing.T) {
	t.Parallel()
	base := &ast.FunctionResolution{
		ReturnType: sqlmodel.NewIntegerType("UInt64", 64, true),
	}
	transformed := applyCombinator(base, "Distinct", combinatorCall{})
	require.NotNil(t, transformed, "expected non-nil resolution")
	assert.Equal(t, "UInt64", transformed.ReturnType.EngineName, "Distinct should preserve the base return type")
}

func TestApplyCombinatorMergeUnwrapsAggregateState(t *testing.T) {
	t.Parallel()
	base := &ast.FunctionResolution{
		ReturnType: sqlmodel.SQLType{
			Category:    sqlmodel.TypeCategoryAggregateState,
			EngineName:  "AggregateFunction",
			ElementType: new(sqlmodel.NewIntegerType("UInt64", 64, true)),
		},
	}
	transformed := applyCombinator(base, "Merge", combinatorCall{})
	require.NotNil(t, transformed, "expected non-nil resolution")
	assert.Equal(t, sqlmodel.TypeCategoryInteger, transformed.ReturnType.Category, "Merge should finalise to the element category")
	assert.Equal(t, "UInt64", transformed.ReturnType.EngineName, "Merge should finalise AggregateFunction(sum, UInt64) to UInt64")
}

func TestApplyCombinatorMergeLeavesNonStateUnchanged(t *testing.T) {
	t.Parallel()
	base := &ast.FunctionResolution{
		ReturnType: sqlmodel.NewIntegerType("UInt64", 64, true),
	}
	transformed := applyCombinator(base, "Merge", combinatorCall{})
	require.NotNil(t, transformed, "expected non-nil resolution")
	assert.Equal(t, "UInt64", transformed.ReturnType.EngineName, "Merge over a non-state base should pass through")
}

func TestApplyCombinatorMergeStateMarksAggregateFunction(t *testing.T) {
	t.Parallel()
	base := &ast.FunctionResolution{
		ReturnType: sqlmodel.NewIntegerType("UInt64", 64, true),
	}
	transformed := applyCombinator(base, "MergeState", combinatorCall{})
	require.NotNil(t, transformed, "expected non-nil resolution")
	assert.Equal(t, "AggregateFunction", transformed.ReturnType.EngineName, "MergeState should mark EngineName=AggregateFunction")
}

func TestApplyCombinatorSimpleStateGivesTheValueASimpleAggregateFunctionHolds(t *testing.T) {
	t.Parallel()
	base := &ast.FunctionResolution{
		ReturnType: sqlmodel.NewIntegerType("UInt64", 64, true),
	}
	transformed := applyCombinator(base, "SimpleState", combinatorCall{})
	require.NotNil(t, transformed)
	require.Equal(t, "UInt64", transformed.ReturnType.EngineName)
	require.Equal(t, sqlmodel.TypeCategoryInteger, transformed.ReturnType.Category)
}

func TestApplyCombinatorForEachWrapsInArray(t *testing.T) {
	t.Parallel()
	base := &ast.FunctionResolution{
		ReturnType: sqlmodel.NewIntegerType("UInt64", 64, true),
	}
	transformed := applyCombinator(base, "ForEach", combinatorCall{})
	require.NotNil(t, transformed, "expected non-nil resolution")
	assert.Equal(t, sqlmodel.TypeCategoryArray, transformed.ReturnType.Category, "ForEach should wrap the return in Array")
	require.NotNil(t, transformed.ReturnType.ElementType, "ForEach should keep an element type")
	assert.Equal(t, "UInt64", transformed.ReturnType.ElementType.EngineName, "ForEach should preserve inner element type UInt64")
}

func TestApplyCombinatorMapWrapsInMap(t *testing.T) {
	t.Parallel()
	base := &ast.FunctionResolution{
		ReturnType: sqlmodel.NewIntegerType("UInt64", 64, true),
	}
	transformed := applyCombinator(base, "Map", combinatorCall{})
	require.NotNil(t, transformed, "expected non-nil resolution")
	assert.Equal(t, sqlmodel.TypeCategoryMap, transformed.ReturnType.Category, "Map should wrap the return in Map")
	require.NotNil(t, transformed.ReturnType.KeyType, "Map should populate KeyType")
	require.NotNil(t, transformed.ReturnType.ElementType, "Map should populate ElementType")
	assert.Equal(t, "UInt64", transformed.ReturnType.ElementType.EngineName, "Map should preserve inner value type UInt64")
}

func TestApplyCombinatorArgMinPreservesReturn(t *testing.T) {
	t.Parallel()
	base := &ast.FunctionResolution{
		ReturnType: sqlmodel.NewIntegerType("UInt64", 64, true),
	}
	transformed := applyCombinator(base, "ArgMin", combinatorCall{})
	require.NotNil(t, transformed, "expected non-nil resolution")
	assert.Equal(t, "UInt64", transformed.ReturnType.EngineName, "ArgMin should preserve the base return type")
}

func TestApplyCombinatorArgMaxPreservesReturn(t *testing.T) {
	t.Parallel()
	base := &ast.FunctionResolution{
		ReturnType: sqlmodel.NewIntegerType("UInt64", 64, true),
	}
	transformed := applyCombinator(base, "ArgMax", combinatorCall{})
	require.NotNil(t, transformed, "expected non-nil resolution")
	assert.Equal(t, "UInt64", transformed.ReturnType.EngineName, "ArgMax should preserve the base return type")
}

func TestSplitCombinatorSuffixesRecognisesNewCombinators(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		input        string
		expectedBase string
		expectedTail []string
	}{
		"quantileMergeState": {input: "quantileMergeState", expectedBase: "quantile", expectedTail: []string{"MergeState"}},
		"sumSimpleState":     {input: "sumSimpleState", expectedBase: "sum", expectedTail: []string{"SimpleState"}},
		"sumArgMin":          {input: "sumArgMin", expectedBase: "sum", expectedTail: []string{"ArgMin"}},
		"sumArgMax":          {input: "sumArgMax", expectedBase: "sum", expectedTail: []string{"ArgMax"}},
		"argMaxIf":           {input: "argMaxIf", expectedBase: "argMax", expectedTail: []string{"If"}},
		"quantileExactIf":    {input: "quantileExactIf", expectedBase: "quantileExact", expectedTail: []string{"If"}},
	}
	for testName, testCase := range cases {
		input := testCase.input
		expectedBase := testCase.expectedBase
		expectedTail := testCase.expectedTail
		t.Run(testName, func(testRunner *testing.T) {
			testRunner.Parallel()
			gotBase, gotTail := splitCombinatorSuffixes(input)
			if gotBase != expectedBase {
				testRunner.Errorf("base mismatch: want %q got %q", expectedBase, gotBase)
			}
			if len(gotTail) != len(expectedTail) {
				testRunner.Fatalf("tail length mismatch: want %v got %v", expectedTail, gotTail)
			}
			for index := range expectedTail {
				if gotTail[index] != expectedTail[index] {
					testRunner.Errorf("tail[%d] mismatch: want %q got %q", index, expectedTail[index], gotTail[index])
				}
			}
		})
	}
}

func TestResolveCountIfReturnsUInt64(t *testing.T) {
	t.Parallel()

	resolver := newFunctionResolver()
	arguments := []sqlmodel.SQLType{
		{Category: sqlmodel.TypeCategoryInteger, EngineName: "UInt32"},
		{Category: sqlmodel.TypeCategoryBoolean, EngineName: "Bool"},
	}

	for _, name := range []string{"countIf", "count", "countDistinct"} {
		resolution, err := resolver.ResolveFunctionCall(t.Context(), nil, name, "", arguments)
		require.NoError(t, err, "%s", name)
		require.Equal(t, "UInt64", resolution.ReturnType.EngineName, "expected UInt64 return for %s", name)
		require.Equal(t, sqlmodel.TypeCategoryInteger, resolution.ReturnType.Category, "expected integer category for %s", name)
	}
}

func TestResolveSumIfReturnsInt64(t *testing.T) {
	t.Parallel()

	resolver := newFunctionResolver()
	arguments := []sqlmodel.SQLType{
		{Category: sqlmodel.TypeCategoryInteger, EngineName: "Int32"},
		{Category: sqlmodel.TypeCategoryBoolean, EngineName: "Bool"},
	}
	resolution, err := resolver.ResolveFunctionCall(t.Context(), nil, "sumIf", "", arguments)
	require.NoError(t, err)
	require.NotNil(t, resolution, "expected resolution for sumIf")
	assert.Equal(t, "Int64", resolution.ReturnType.EngineName, "expected Int64 return for sumIf over Int32")
}

func TestResolveUniqIfNoOpinion(t *testing.T) {
	t.Parallel()

	resolver := newFunctionResolver()
	arguments := []sqlmodel.SQLType{
		{Category: sqlmodel.TypeCategoryText, EngineName: "String"},
		{Category: sqlmodel.TypeCategoryBoolean, EngineName: "Bool"},
	}
	_, err := resolver.ResolveFunctionCall(t.Context(), nil, "uniqIf", "", arguments)
	require.ErrorIs(t, err, errResolverNoOpinion, "uniqIf")
}

func TestResolveArgMaxIfReturnsFirstArgType(t *testing.T) {
	t.Parallel()
	resolver := newFunctionResolver()
	arguments := []sqlmodel.SQLType{
		{Category: sqlmodel.TypeCategoryText, EngineName: "String"},
		{Category: sqlmodel.TypeCategoryInteger, EngineName: "UInt32"},
		{Category: sqlmodel.TypeCategoryBoolean, EngineName: "Bool"},
	}
	resolution, err := resolver.ResolveFunctionCall(t.Context(), nil, "argMaxIf", "", arguments)
	require.NoError(t, err)
	require.NotNil(t, resolution, "expected resolution for argMaxIf")
	assert.Equal(t, sqlmodel.TypeCategoryText, resolution.ReturnType.Category, "expected first-arg type (String) for argMaxIf")
}

func TestResolveCatalogueAggregatesYieldNoOpinion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		arguments []sqlmodel.SQLType
	}{
		{
			name: "quantileIf",
			arguments: []sqlmodel.SQLType{
				{Category: sqlmodel.TypeCategoryFloat, EngineName: "Float64"},
				{Category: sqlmodel.TypeCategoryBoolean, EngineName: "Bool"},
			},
		},
		{
			name: "uniqState",
			arguments: []sqlmodel.SQLType{
				{Category: sqlmodel.TypeCategoryInteger, EngineName: "UInt32"},
			},
		},
		{
			name: "quantileMerge",
			arguments: []sqlmodel.SQLType{
				{Category: sqlmodel.TypeCategoryFloat, EngineName: "Float64"},
			},
		},
	}
	for _, testCase := range cases {
		name := testCase.name
		arguments := testCase.arguments
		t.Run(name, func(testRunner *testing.T) {
			testRunner.Parallel()
			resolver := newFunctionResolver()
			_, err := resolver.ResolveFunctionCall(t.Context(), nil, name, "", arguments)
			if !errors.Is(err, errResolverNoOpinion) {
				testRunner.Fatalf("expected errResolverNoOpinion for %s, got %v", name, err)
			}
		})
	}
}

func TestApplyCombinatorChainCountIfOrNull(t *testing.T) {
	t.Parallel()
	base := &ast.FunctionResolution{
		ReturnType: sqlmodel.NewIntegerType("UInt64", 64, true),
	}
	step1 := applyCombinator(base, "If", combinatorCall{})
	step2 := applyCombinator(step1, "OrNull", combinatorCall{})
	assert.Equal(t, sqlmodel.TypeCategoryInteger, step2.ReturnType.Category, "expected integer category after If+OrNull")
	assert.Equal(t, sqlmodel.FunctionNullableCalledOnNull, step2.NullableBehaviour, "expected a result that may be NULL after OrNull")
}

func TestResolveMapContainsReturnsUInt8(t *testing.T) {
	t.Parallel()
	resolver := newFunctionResolver()
	key := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "String"}
	mapArgument := sqlmodel.SQLType{
		Category:    sqlmodel.TypeCategoryMap,
		EngineName:  "Map",
		KeyType:     &key,
		ElementType: new(sqlmodel.NewIntegerType("UInt64", 64, true)),
	}
	arguments := []sqlmodel.SQLType{mapArgument, key}
	resolution, err := resolver.ResolveFunctionCall(t.Context(), nil, "mapContains", "", arguments)
	require.NoError(t, err)
	require.Equal(t, "UInt8", resolution.ReturnType.EngineName)
	require.Equal(t, sqlmodel.TypeCategoryInteger, resolution.ReturnType.Category)
}

func TestResolveMapAddReturnsInputMap(t *testing.T) {
	t.Parallel()
	resolver := newFunctionResolver()
	mapArgument := sqlmodel.SQLType{
		Category:    sqlmodel.TypeCategoryMap,
		EngineName:  "Map",
		KeyType:     new(sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "String"}),
		ElementType: new(sqlmodel.NewIntegerType("UInt64", 64, true)),
	}
	resolution, err := resolver.ResolveFunctionCall(t.Context(), nil, "mapAdd", "", []sqlmodel.SQLType{mapArgument, mapArgument})
	require.NoError(t, err)
	require.NotNil(t, resolution, "expected a resolution for mapAdd")
	assert.Equal(t, sqlmodel.TypeCategoryMap, resolution.ReturnType.Category, "expected Map resolution for mapAdd")
}

func TestResolveVariantTypeReturnsString(t *testing.T) {
	t.Parallel()
	resolver := newFunctionResolver()
	variant := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryUnion, EngineName: "Variant"}
	resolution, err := resolver.ResolveFunctionCall(t.Context(), nil, "variantType", "", []sqlmodel.SQLType{variant})
	require.NoError(t, err)
	require.NotNil(t, resolution, "expected a resolution for variantType")
	assert.Equal(t, sqlmodel.TypeCategoryText, resolution.ReturnType.Category, "expected String resolution for variantType")
}

func TestResolveDynamicTypeReturnsString(t *testing.T) {
	t.Parallel()
	resolver := newFunctionResolver()
	dynamic := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryUnknown, EngineName: "Dynamic"}
	resolution, err := resolver.ResolveFunctionCall(t.Context(), nil, "dynamicType", "", []sqlmodel.SQLType{dynamic})
	require.NoError(t, err)
	require.NotNil(t, resolution, "expected a resolution for dynamicType")
	assert.Equal(t, sqlmodel.TypeCategoryText, resolution.ReturnType.Category, "expected String resolution for dynamicType")
}

func TestResolveVariantElementReturnsMemberType(t *testing.T) {
	t.Parallel()
	resolver := newFunctionResolver()
	memberType := sqlmodel.NewIntegerType("UInt64", 64, true)
	variant := sqlmodel.SQLType{
		Category:     sqlmodel.TypeCategoryUnion,
		EngineName:   "Variant",
		UnionMembers: []sqlmodel.UnionMember{{SQLType: memberType}},
	}
	literalType := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "String"}
	resolution, err := resolver.ResolveFunctionCall(t.Context(), nil, "variantElement", "", []sqlmodel.SQLType{variant, literalType})
	require.NoError(t, err)
	require.NotNil(t, resolution, "expected resolution for variantElement")
	assert.True(t, resolution.ReturnType.Nullable, "expected variantElement to return nullable type")
}

func TestFinalizeAggregationGivesTheResultOfTheAggregateThatBuiltTheState(t *testing.T) {
	t.Parallel()

	unnamed := sqlmodel.SQLType{
		Category:    sqlmodel.TypeCategoryAggregateState,
		EngineName:  "AggregateFunction",
		ElementType: new(sqlmodel.NewIntegerType("UInt64", 64, true)),
	}
	testCases := []struct {
		name       string
		state      sqlmodel.SQLType
		wantEngine string
	}{
		{name: "uniq counts its distinct values", state: normaliseBuiltinTypeName("AggregateFunction(uniq, String)"), wantEngine: "UInt64"},
		{name: "sum widens its values", state: normaliseBuiltinTypeName("AggregateFunction(sum, UInt32)"), wantEngine: "UInt64"},
		{name: "anyLast keeps its value type", state: normaliseBuiltinTypeName("AggregateFunction(anyLast, String)"), wantEngine: "String"},
		{name: "count takes no argument", state: normaliseBuiltinTypeName("AggregateFunction(count)"), wantEngine: "UInt64"},
		{name: "a state whose aggregate is not recorded", state: unnamed, wantEngine: ""},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver := newCatalogueFunctionResolver(New().BuiltinFunctions())
			resolution, err := resolver.ResolveFunctionCall(t.Context(), nil, "finalizeAggregation", "", []sqlmodel.SQLType{testCase.state})
			require.NoError(t, err)
			require.NotNil(t, resolution)
			require.Equal(t, testCase.wantEngine, resolution.ReturnType.EngineName)
			if testCase.wantEngine == "" {
				require.Equal(t, sqlmodel.TypeCategoryUnknown, resolution.ReturnType.Category)
			}
		})
	}
}

func TestResolveSumMergeFinalisesAggregateState(t *testing.T) {
	t.Parallel()
	resolver := newFunctionResolver()
	state := sqlmodel.SQLType{
		Category:    sqlmodel.TypeCategoryAggregateState,
		EngineName:  "AggregateFunction",
		ElementType: new(sqlmodel.NewIntegerType("UInt64", 64, true)),
	}
	resolution, err := resolver.ResolveFunctionCall(t.Context(), nil, "sumMerge", "", []sqlmodel.SQLType{state})
	require.NoError(t, err)
	require.NotNil(t, resolution, "expected resolution for sumMerge over an aggregate state")
	assert.Equal(t, sqlmodel.TypeCategoryInteger, resolution.ReturnType.Category, "expected integer category for sumMerge")
	assert.Equal(t, "UInt64", resolution.ReturnType.EngineName, "expected sumMerge(AggregateFunction(sum, UInt64)) to finalise to UInt64")
}

func TestResolveMaxMergeFinalisesAggregateState(t *testing.T) {
	t.Parallel()
	resolver := newFunctionResolver()
	state := sqlmodel.SQLType{
		Category:    sqlmodel.TypeCategoryAggregateState,
		EngineName:  "AggregateFunction",
		ElementType: new(sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "String"}),
	}
	resolution, err := resolver.ResolveFunctionCall(t.Context(), nil, "maxMerge", "", []sqlmodel.SQLType{state})
	require.NoError(t, err)
	require.NotNil(t, resolution, "expected resolution for maxMerge over an aggregate state")
	assert.Equal(t, "String", resolution.ReturnType.EngineName, "expected maxMerge(AggregateFunction(max, String)) to finalise to String")
}

func TestResolveInitializeAggregationWrapsValueInState(t *testing.T) {
	t.Parallel()
	resolver := newFunctionResolver()
	aggregateName := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "String"}
	value := sqlmodel.NewIntegerType("UInt32", 32, true)
	resolution, err := resolver.ResolveFunctionCall(t.Context(), nil, "initializeAggregation", "", []sqlmodel.SQLType{aggregateName, value})
	require.NoError(t, err)
	require.NotNil(t, resolution, "expected a resolution for initializeAggregation")
	assert.Equal(t, "AggregateFunction", resolution.ReturnType.EngineName, "expected AggregateFunction wrap")
}

func TestIsStandaloneFunctionRecognisesWindowAliases(t *testing.T) {
	t.Parallel()
	names := []string{"lagInFrame", "leadInFrame", "denseRank", "percentRank", "nth_value", "percent_rank", "cume_dist"}
	for _, name := range names {
		assert.True(t, isStandaloneFunctionName(name), "expected %q to be standalone", name)
	}
}

func TestIsStandaloneFunctionRecognisesVariantHelpers(t *testing.T) {
	t.Parallel()
	names := []string{"variantElement", "variantType", "dynamicElement", "dynamicType"}
	for _, name := range names {
		assert.True(t, isStandaloneFunctionName(name), "expected %q to be standalone", name)
	}
}

func TestLogicalFunctionsReturnBoolOnlyForABoolArgument(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		function  string
		arguments []string
		want      string
	}{
		{name: "and of integers", function: "and", arguments: []string{"UInt32", "UInt8"}, want: "UInt8"},
		{name: "and of a Bool and an integer", function: "and", arguments: []string{"Bool", "UInt32"}, want: "Bool"},
		{name: "or of Bools", function: "or", arguments: []string{"Bool", "Bool", "Bool"}, want: "Bool"},
		{name: "xor of integers", function: "xor", arguments: []string{"UInt32", "UInt32"}, want: "UInt8"},
		{name: "not of an integer", function: "not", arguments: []string{"UInt32"}, want: "UInt8"},
		{name: "not of a Bool", function: "not", arguments: []string{"Bool"}, want: "Bool"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			argumentTypes := make([]sqlmodel.SQLType, len(testCase.arguments))
			for index, argument := range testCase.arguments {
				argumentTypes[index] = normaliseBuiltinTypeName(argument)
			}
			resolution, err := newFunctionResolver().ResolveFunctionCall(t.Context(), nil, testCase.function, "", argumentTypes)
			require.NoError(t, err)
			require.Equal(t, testCase.want, resolution.ReturnType.EngineName)
			require.Equal(t, sqlmodel.FunctionNullableReturnsNullOnNull, resolution.NullableBehaviour)
		})
	}
}

func TestContainerConstructorsAreNeverNullThemselves(t *testing.T) {
	t.Parallel()

	nullableText := normaliseBuiltinTypeName("String")
	nullableText.Nullable = true
	count := normaliseBuiltinTypeName("UInt32")
	testCases := []struct {
		name      string
		function  string
		arguments []sqlmodel.SQLType
	}{
		{name: "a tuple of a nullable value", function: "tuple", arguments: []sqlmodel.SQLType{nullableText, count}},
		{name: "a map of a nullable value", function: "map", arguments: []sqlmodel.SQLType{normaliseBuiltinTypeName("String"), nullableText}},
		{name: "an array of a nullable value", function: "array", arguments: []sqlmodel.SQLType{nullableText}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolution, err := newFunctionResolver().ResolveFunctionCall(t.Context(), nil, testCase.function, "", testCase.arguments)
			require.NoError(t, err)
			require.Equal(t, sqlmodel.FunctionNullableNeverNull, resolution.NullableBehaviour)
		})
	}

	t.Run("a tuple keeps the nullability of each field", func(t *testing.T) {
		t.Parallel()

		resolution, err := newFunctionResolver().ResolveFunctionCall(t.Context(), nil, "tuple", "", []sqlmodel.SQLType{nullableText, count})
		require.NoError(t, err)
		require.True(t, resolution.ReturnType.StructFields[0].SQLType.Nullable)
		require.False(t, resolution.ReturnType.StructFields[1].SQLType.Nullable)
	})
}

func TestResolveRoundingKeepsTheArgumentType(t *testing.T) {
	t.Parallel()

	engine := New()
	resolver := newFunctionResolver()
	decimal := engine.NormaliseTypeName("Decimal(12, 2)")
	unsigned := engine.NormaliseTypeName("UInt32")
	float := engine.NormaliseTypeName("Float32")
	scale := engine.NormaliseTypeName("Int64")

	testCases := []struct {
		functionName string
		arguments    []sqlmodel.SQLType
		want         sqlmodel.SQLType
	}{
		{functionName: "round", arguments: []sqlmodel.SQLType{decimal, scale}, want: decimal},
		{functionName: "round", arguments: []sqlmodel.SQLType{decimal}, want: decimal},
		{functionName: "roundBankers", arguments: []sqlmodel.SQLType{decimal, scale}, want: decimal},
		{functionName: "floor", arguments: []sqlmodel.SQLType{decimal}, want: decimal},
		{functionName: "ceil", arguments: []sqlmodel.SQLType{decimal, scale}, want: decimal},
		{functionName: "ceiling", arguments: []sqlmodel.SQLType{unsigned}, want: unsigned},
		{functionName: "truncate", arguments: []sqlmodel.SQLType{decimal, scale}, want: decimal},
		{functionName: "trunc", arguments: []sqlmodel.SQLType{float}, want: float},
		{functionName: "round", arguments: []sqlmodel.SQLType{unsigned}, want: unsigned},
	}
	for _, testCase := range testCases {
		t.Run(testCase.functionName, func(t *testing.T) {
			t.Parallel()

			resolution, err := resolver.ResolveFunctionCall(t.Context(), nil, testCase.functionName, "", testCase.arguments)
			require.NoError(t, err)
			require.NotNil(t, resolution)
			require.Equal(t, testCase.want, resolution.ReturnType)
			require.False(t, resolution.IsAggregate)
		})
	}
}

func TestResolveExactQuantilesKeepTheArgumentType(t *testing.T) {
	t.Parallel()

	engine := New()
	resolver := newFunctionResolver()
	unsigned := engine.NormaliseTypeName("UInt32")
	decimal := engine.NormaliseTypeName("Decimal(12, 2)")
	date := engine.NormaliseTypeName("Date")
	weight := engine.NormaliseTypeName("UInt64")

	testCases := []struct {
		functionName string
		arguments    []sqlmodel.SQLType
		want         sqlmodel.SQLType
	}{
		{functionName: "quantileExact", arguments: []sqlmodel.SQLType{unsigned}, want: unsigned},
		{functionName: "quantileExactLow", arguments: []sqlmodel.SQLType{decimal}, want: decimal},
		{functionName: "quantileExactHigh", arguments: []sqlmodel.SQLType{date}, want: date},
		{functionName: "quantileExactWeighted", arguments: []sqlmodel.SQLType{unsigned, weight}, want: unsigned},
		{functionName: "quantilesExact", arguments: []sqlmodel.SQLType{unsigned}, want: arrayOf(unsigned)},
		{functionName: "quantilesExactLow", arguments: []sqlmodel.SQLType{decimal}, want: arrayOf(decimal)},
		{functionName: "quantilesExactHigh", arguments: []sqlmodel.SQLType{date}, want: arrayOf(date)},
	}
	for _, testCase := range testCases {
		t.Run(testCase.functionName, func(t *testing.T) {
			t.Parallel()

			resolution, err := resolver.ResolveFunctionCall(t.Context(), nil, testCase.functionName, "", testCase.arguments)
			require.NoError(t, err)
			require.NotNil(t, resolution)
			require.Equal(t, testCase.want, resolution.ReturnType)
			require.True(t, resolution.IsAggregate)
		})
	}
}

func TestResolverTypesFromClickHouseSemantics(t *testing.T) {
	t.Parallel()

	resolver := newCatalogueFunctionResolver(New().BuiltinFunctions())
	state := func(typeName string) sqlmodel.SQLType {
		return normaliseBuiltinTypeName(typeName)
	}
	nullableState := state("AggregateFunction(max, Nullable(UInt32))")
	testCases := []struct {
		name        string
		function    string
		want        string
		wantElement string
		arguments   []sqlmodel.SQLType
		mayBeNull   bool
	}{
		{name: "uniqMerge reads uniq's result", function: "uniqMerge", arguments: []sqlmodel.SQLType{state("AggregateFunction(uniq, String)")}, want: "UInt64"},
		{name: "sumMerge reads sum's result", function: "sumMerge", arguments: []sqlmodel.SQLType{state("AggregateFunction(sum, UInt32)")}, want: "UInt64"},
		{name: "maxMerge reads the value type", function: "maxMerge", arguments: []sqlmodel.SQLType{state("AggregateFunction(max, UInt32)")}, want: "UInt32"},
		{
			name: "a merge of Nullable values may be NULL", function: "maxMerge", arguments: []sqlmodel.SQLType{nullableState}, want: "UInt32",
			mayBeNull: true,
		},
		{name: "countMerge of a state with no argument", function: "countMerge", arguments: []sqlmodel.SQLType{state("AggregateFunction(count)")}, want: "UInt64"},
		{name: "avgMerge", function: "avgMerge", arguments: []sqlmodel.SQLType{state("AggregateFunction(avg, UInt32)")}, want: "Float64"},
		{
			name: "groupArrayMerge", function: "groupArrayMerge", arguments: []sqlmodel.SQLType{state("AggregateFunction(groupArray, String)")},
			want: "Array", wantElement: "String",
		},
		{name: "uniqIf drops the condition", function: "uniqIf", arguments: []sqlmodel.SQLType{primitiveType("String"), primitiveType("Bool")}, want: "UInt64"},
		{name: "sumArray sums the elements", function: "sumArray", arguments: []sqlmodel.SQLType{arrayOf(primitiveType("Int8"))}, want: "Int64"},
		{
			name: "sumState holds the value type", function: "sumState", arguments: []sqlmodel.SQLType{primitiveType("UInt32")}, want: "AggregateFunction",
			wantElement: "UInt32",
		},
		{name: "groupArray collects its argument", function: "groupArray", arguments: []sqlmodel.SQLType{primitiveType("UInt64")}, want: "Array", wantElement: "UInt64"},
		{name: "topK collects its argument", function: "topK", arguments: []sqlmodel.SQLType{primitiveType("String")}, want: "Array", wantElement: "String"},
		{name: "arraySum of unsigned integers", function: "arraySum", arguments: []sqlmodel.SQLType{arrayOf(primitiveType("UInt32"))}, want: "UInt64"},
		{name: "arraySum of floats", function: "arraySum", arguments: []sqlmodel.SQLType{arrayOf(primitiveType("Float32"))}, want: "Float64"},
		{
			name: "arraySum of decimals", function: "arraySum", arguments: []sqlmodel.SQLType{arrayOf(normaliseBuiltinTypeName("Decimal(9, 2)"))},
			want: "Decimal128",
		},
		{name: "arrayElement of a map reads its value", function: "arrayElement", arguments: []sqlmodel.SQLType{mapOf(primitiveType("String"), primitiveType("UInt32")), primitiveType("String")}, want: "UInt32"},
		{name: "arrayReverse keeps the array", function: "arrayReverse", arguments: []sqlmodel.SQLType{arrayOf(primitiveType("String"))}, want: "Array", wantElement: "String"},
		{name: "array of literals", function: "array", arguments: []sqlmodel.SQLType{primitiveType("UInt8"), primitiveType("UInt16")}, want: "Array", wantElement: "UInt16"},
		{name: "multiply follows the operator", function: "multiply", arguments: []sqlmodel.SQLType{primitiveType("Float64"), primitiveType("UInt8")}, want: "Float64"},
		{name: "intDiv follows the operator", function: "intDiv", arguments: []sqlmodel.SQLType{primitiveType("Int64"), primitiveType("UInt8")}, want: "Int64"},
		{name: "negate follows the prefix minus", function: "negate", arguments: []sqlmodel.SQLType{primitiveType("UInt8")}, want: "Int16"},
		{name: "abs is unsigned", function: "abs", arguments: []sqlmodel.SQLType{primitiveType("Int32")}, want: "UInt32"},
		{name: "lagInFrame reads its argument", function: "lagInFrame", arguments: []sqlmodel.SQLType{primitiveType("String")}, want: "String"},
		{
			name: "nullIf may be NULL", function: "nullIf", arguments: []sqlmodel.SQLType{primitiveType("String"), primitiveType("String")}, want: "String",
			mayBeNull: true,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			resolution, err := resolver.ResolveFunctionCall(t.Context(), nil, testCase.function, "", testCase.arguments)
			require.NoError(t, err)
			assert.Equal(t, testCase.want, resolution.ReturnType.EngineName)
			if testCase.wantElement != "" {
				require.NotNil(t, resolution.ReturnType.ElementType)
				assert.Equal(t, testCase.wantElement, resolution.ReturnType.ElementType.EngineName)
			}
			assert.Equal(t, testCase.mayBeNull, resolution.NullableBehaviour == sqlmodel.FunctionNullableCalledOnNull,
				"a result that may be NULL from values that are not")
		})
	}
}

func TestResolveFloatResultKeepsAFloat32(t *testing.T) {
	t.Parallel()

	assert.Equal(t, primitiveType("Float32"), resolveFloatResult([]sqlmodel.SQLType{primitiveType("Float32")}).ReturnType)
	assert.Equal(t, primitiveType("Float64"), resolveFloatResult([]sqlmodel.SQLType{primitiveType("Int32")}).ReturnType)
	assert.Nil(t, resolveFloatResult([]sqlmodel.SQLType{primitiveType("String")}))
	assert.Nil(t, resolveFloatResult(nil))
}

func TestResolveSupertypeTypesGreatestByTheLeastSupertype(t *testing.T) {
	t.Parallel()

	decimal := sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "Decimal(12, 3)")
	testCases := []struct {
		name      string
		arguments []sqlmodel.SQLType
		want      string
	}{
		{name: "two integers meet in the wider", arguments: []sqlmodel.SQLType{primitiveType("Int16"), primitiveType("Int32")}, want: "Int32"},
		{name: "a short integer and a Float32 meet in the Float32", arguments: []sqlmodel.SQLType{primitiveType("Int16"), primitiveType("Float32")}, want: "Float32"},
		{name: "a wide integer and a Float32 meet in Float64", arguments: []sqlmodel.SQLType{primitiveType("Float32"), primitiveType("Int32")}, want: "Float64"},
		{name: "a decimal and a float meet in Float64", arguments: []sqlmodel.SQLType{decimal, primitiveType("Float32")}, want: "Float64"},
		{name: "an integer and a decimal meet in the decimal", arguments: []sqlmodel.SQLType{primitiveType("Int64"), decimal}, want: "Decimal(12, 3)"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, resolveSupertype(testCase.arguments).ReturnType.EngineName)
		})
	}
	assert.Nil(t, resolveSupertype(nil))
}

func TestResolveCoalesceTypesOnlyUpToTheFirstNonNullableArgument(t *testing.T) {
	t.Parallel()

	nullableInt16 := primitiveType("Int16")
	nullableInt16.Nullable = true
	float32Type := primitiveType("Float32")

	assert.Equal(t, "Int16", resolveCoalesce([]sqlmodel.SQLType{primitiveType("Int16"), float32Type}).ReturnType.EngineName,
		"a non-nullable first argument is all ClickHouse types")
	assert.Equal(t, "Float32", resolveCoalesce([]sqlmodel.SQLType{nullableInt16, float32Type}).ReturnType.EngineName,
		"a nullable first argument meets the next in their supertype")
	assert.True(t, New().CoalesceTypedUpToNonNullable())
}
