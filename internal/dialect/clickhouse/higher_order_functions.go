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
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// engineNameUInt32 is the canonical ClickHouse UInt32 engine name.
	engineNameUInt32 = "UInt32"

	// minimumLambdaCallArguments is the fewest arguments a higher-order call passing a
	// lambda has: the lambda and one array.
	minimumLambdaCallArguments = 2

	// minimumFoldArguments is the fewest arguments arrayFold takes: the lambda, one array
	// and the accumulator's starting value.
	minimumFoldArguments = 3
)

// higherOrderCall is a call to a higher-order array function, its argument types split
// into the lambda's result, the arrays and arrayFold's accumulator.
type higherOrderCall struct {
	// lambda is the type the lambda returns, or nil for a call without a lambda.
	lambda *sqlmodel.SQLType

	// accumulator is the type of arrayFold's starting value, or nil for any other call.
	accumulator *sqlmodel.SQLType

	// arrays are the types of the array arguments, in order.
	arrays []sqlmodel.SQLType

	// arrayArgument is the position of the first array among the call's arguments.
	arrayArgument int
}

// higherOrderResolver adapts a resolver of a split higher-order call to the shape of the
// base resolvers. A call takes a lambda first when it passes at least two arguments, as
// every such function takes a single array otherwise.
//
// Takes resolve (func(higherOrderCall) *ast.FunctionResolution) which types the call.
//
// Returns baseResolverFunc which splits the argument types and calls resolve, or returns
// nil when no array argument is an array.
func higherOrderResolver(resolve func(call higherOrderCall) *ast.FunctionResolution) baseResolverFunc {
	return func(argumentTypes []sqlmodel.SQLType, _ string) *ast.FunctionResolution {
		call := higherOrderCall{lambda: nil, accumulator: nil, arrays: argumentTypes, arrayArgument: 0}
		if len(argumentTypes) >= minimumLambdaCallArguments {
			call.lambda, call.arrays, call.arrayArgument = &argumentTypes[0], argumentTypes[1:], 1
		}
		if len(call.arrays) == 0 || call.arrays[0].Category != sqlmodel.TypeCategoryArray {
			return nil
		}
		return resolve(call)
	}
}

// resolveArrayFold types arrayFold(lambda, arrays..., start) as its accumulator's type.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call's argument types.
//
// Returns *ast.FunctionResolution which is the resolution, or nil when the call passes
// too few arguments.
func resolveArrayFold(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	if len(argumentTypes) < minimumFoldArguments {
		return nil
	}
	resolution := newReadOnlyResolution(argumentTypes[len(argumentTypes)-1])
	resolution.NullableBehaviour = sqlmodel.FunctionNullableNullWhenArgumentNull
	resolution.NullingArgument = len(argumentTypes) - 1
	return resolution
}

// resolveLambdaArrayMap types arrayMap as an array of what its lambda returns.
//
// Takes call (higherOrderCall) which is the call.
//
// Returns *ast.FunctionResolution which is the resolution, or nil when the lambda's
// result is unknown.
func resolveLambdaArrayMap(call higherOrderCall) *ast.FunctionResolution {
	if call.lambda == nil || call.lambda.Category == sqlmodel.TypeCategoryUnknown {
		return nil
	}
	return arrayCallResolution(arrayOf(*call.lambda), call)
}

// resolveFirstArrayPassthrough types a function returning its first array reordered or
// filtered, such as arrayFilter or arraySort, as that array.
//
// Takes call (higherOrderCall) which is the call.
//
// Returns *ast.FunctionResolution which is the resolution.
func resolveFirstArrayPassthrough(call higherOrderCall) *ast.FunctionResolution {
	return arrayCallResolution(call.arrays[0], call)
}

// resolveArraySplitCall types arraySplit and arrayReverseSplit as an array of pieces of
// the first array.
//
// Takes call (higherOrderCall) which is the call.
//
// Returns *ast.FunctionResolution which is the resolution.
func resolveArraySplitCall(call higherOrderCall) *ast.FunctionResolution {
	return arrayCallResolution(arrayOf(call.arrays[0]), call)
}

// resolveArrayElementCall types arrayFirst and arrayLast as an element of the first
// array: the element type's default when none matches, so NULL only when the elements may
// be.
//
// Takes call (higherOrderCall) which is the call.
//
// Returns *ast.FunctionResolution which is the resolution, or nil for an array of unknown
// elements.
func resolveArrayElementCall(call higherOrderCall) *ast.FunctionResolution {
	element := call.arrays[0].ElementType
	if element == nil {
		return nil
	}
	resolution := arrayCallResolution(*element, call)
	if element.Nullable {
		resolution.NullableBehaviour = sqlmodel.FunctionNullableCalledOnNull
	}
	return resolution
}

// resolveArrayElementOrNullCall types arrayFirstOrNull and arrayLastOrNull as an element
// of the first array that is NULL when none matches.
//
// Takes call (higherOrderCall) which is the call.
//
// Returns *ast.FunctionResolution which is the resolution, or nil for an array of unknown
// elements.
func resolveArrayElementOrNullCall(call higherOrderCall) *ast.FunctionResolution {
	element := call.arrays[0].ElementType
	if element == nil {
		return nil
	}
	nullable := *element
	nullable.Nullable = true
	resolution := arrayCallResolution(nullable, call)
	resolution.NullableBehaviour = sqlmodel.FunctionNullableCalledOnNull
	return resolution
}

// resolveArrayExtremeCall types arrayMin and arrayMax as what the lambda returns, or as
// an element of the array without one.
//
// Takes call (higherOrderCall) which is the call.
//
// Returns *ast.FunctionResolution which is the resolution, or nil when that type is
// unknown.
func resolveArrayExtremeCall(call higherOrderCall) *ast.FunctionResolution {
	value, known := reducedValueType(call)
	if !known {
		return nil
	}
	return arrayCallResolution(value, call)
}

// resolveArraySumCall types arraySum as the sum of what the lambda returns, or of the
// array's elements without one.
//
// Takes call (higherOrderCall) which is the call.
//
// Returns *ast.FunctionResolution which is the resolution, or nil when the summed type is
// unknown.
func resolveArraySumCall(call higherOrderCall) *ast.FunctionResolution {
	value, known := reducedValueType(call)
	if !known {
		return nil
	}
	return arrayCallResolution(sumResultType(value), call)
}

// resolveArrayCumulativeSumCall types arrayCumSum and arrayCumSumNonNegative as an array
// of running sums of what the lambda returns, or of the array's elements without one.
//
// Takes call (higherOrderCall) which is the call.
//
// Returns *ast.FunctionResolution which is the resolution, or nil when the summed type is
// unknown.
func resolveArrayCumulativeSumCall(call higherOrderCall) *ast.FunctionResolution {
	value, known := reducedValueType(call)
	if !known {
		return nil
	}
	return arrayCallResolution(arrayOf(sumResultType(value)), call)
}

// fixedArrayCallResolver returns a resolver typing a higher-order function as one type
// whatever its arguments, such as arrayCount as UInt32.
//
// Takes engineName (string) which is the type's engine name.
//
// Returns func(higherOrderCall) *ast.FunctionResolution which types the call.
func fixedArrayCallResolver(engineName string) func(call higherOrderCall) *ast.FunctionResolution {
	return func(call higherOrderCall) *ast.FunctionResolution {
		return arrayCallResolution(primitiveType(engineName), call)
	}
}

// reducedValueType returns the type a reducing function reduces: what the lambda returns,
// or the array's element type without a lambda.
//
// Takes call (higherOrderCall) which is the call.
//
// Returns sqlmodel.SQLType which is the type.
// Returns bool which is false when the type is unknown.
func reducedValueType(call higherOrderCall) (sqlmodel.SQLType, bool) {
	if call.lambda != nil {
		return *call.lambda, call.lambda.Category != sqlmodel.TypeCategoryUnknown
	}
	if element := call.arrays[0].ElementType; element != nil && element.Category != sqlmodel.TypeCategoryUnknown {
		return *element, true
	}
	return unknownType(), false
}

// arrayCallResolution builds the resolution of a higher-order call, NULL only when its
// first array may be, whatever its lambda returns.
//
// Takes returnType (sqlmodel.SQLType) which is the call's type.
// Takes call (higherOrderCall) which is the call.
//
// Returns *ast.FunctionResolution which is the resolution.
func arrayCallResolution(returnType sqlmodel.SQLType, call higherOrderCall) *ast.FunctionResolution {
	resolution := newReadOnlyResolution(returnType)
	resolution.NullableBehaviour = sqlmodel.FunctionNullableNullWhenArgumentNull
	resolution.NullingArgument = call.arrayArgument
	return resolution
}
