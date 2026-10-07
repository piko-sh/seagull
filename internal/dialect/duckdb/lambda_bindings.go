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
	"seagull.sh/seagull/internal/dialect/ast"
)

const (
	// lambdaIndexType is the type of the 1-based position DuckDB passes a list function's
	// lambda beside each element.
	lambdaIndexType = "bigint"

	// minimumLambdaCallArguments is the fewest arguments a list function taking a lambda
	// has: the list and the lambda.
	minimumLambdaCallArguments = 2

	// reduceInitialArgument is the position of the starting value list_reduce may take after
	// its lambda.
	reduceInitialArgument = 2
)

var (
	// indexedListFunctions lists the list functions whose lambda may take, after the
	// element, the element's 1-based position in the list.
	indexedListFunctions = map[string]struct{}{
		"list_transform": {}, "array_transform": {}, "list_apply": {}, "array_apply": {}, "apply": {},
		"list_filter": {}, "array_filter": {}, "filter": {},
	}

	// reducingListFunctions lists the list functions that fold a list with a lambda taking
	// the accumulated value, then the element, then optionally the element's position.
	reducingListFunctions = map[string]struct{}{"list_reduce": {}, "array_reduce": {}, "reduce": {}}
)

// bindLambdaParameters binds DuckDB lambda accumulators and position parameters.
//
// Positions are BIGINT. Reduce accumulators use the element type widened with the
// optional starting value's type.
//
// Takes name (string) which is the lower-cased function name.
// Takes schema (string) which is the call's schema qualifier; a qualified call is a
// user's function and binds nothing.
// Takes arguments ([]ast.Expression) which are the call's arguments; the lambda is
// rewritten in place.
func bindLambdaParameters(name, schema string, arguments []ast.Expression) {
	if schema != "" || len(arguments) < minimumLambdaCallArguments {
		return
	}
	lambda, isLambda := arguments[1].(*ast.LambdaExpression)
	if !isLambda || lambda == nil {
		return
	}
	if _, indexed := indexedListFunctions[name]; indexed {
		lambda.Bindings = []ast.Expression{nil, lambdaIndex()}
		return
	}
	if _, reducing := reducingListFunctions[name]; reducing {
		lambda.Bindings = []ast.Expression{reduceAccumulator(arguments), nil, lambdaIndex()}
	}
}

// lambdaIndex is the value an element's position in its list stands for.
//
// Returns ast.Expression which is a BIGINT that is never NULL.
func lambdaIndex() ast.Expression {
	return &ast.LiteralExpression{TypeName: lambdaIndexType}
}

// reduceAccumulator is the value a reduce's accumulator stands for: an element of the
// list, or the common type of an element and the starting value the call gives.
//
// Takes arguments ([]ast.Expression) which are the reduce call's arguments.
//
// Returns ast.Expression which is the accumulator's value.
func reduceAccumulator(arguments []ast.Expression) ast.Expression {
	element := &ast.ArraySubscriptExpression{Array: arguments[0], Index: lambdaIndex(), Key: ""}
	if len(arguments) <= reduceInitialArgument {
		return element
	}
	return &ast.CoalesceExpression{Arguments: []ast.Expression{element, arguments[reduceInitialArgument]}}
}
