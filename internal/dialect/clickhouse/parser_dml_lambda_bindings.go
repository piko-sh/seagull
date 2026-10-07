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
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
)

// functionArrayFold is the higher-order function whose lambda takes the accumulator first
// and its final argument is the accumulator's starting value.
const functionArrayFold = "arrayfold"

// bindLambdaParameters binds ClickHouse lambda parameters to array elements and fold
// accumulators.
//
// An arrayFold accumulator takes the final starting argument's type. Other lambda
// parameters take the array element types. SQL text is preserved.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the statement's analysis, rewritten in
// place with the queries nested in it.
func bindLambdaParameters(analysis *ast.RawQueryAnalysis) {
	if analysis == nil {
		return
	}
	bindQueryExpressionLambdas(analysis)
	for index := range analysis.CTEDefinitions {
		definition := &analysis.CTEDefinitions[index]
		for column := range definition.OutputColumns {
			bindExpressionLambdas(definition.OutputColumns[column].Expression)
		}
		bindLambdaParameters(definition.Query)
	}
	for index := range analysis.RawDerivedTables {
		bindLambdaParameters(analysis.RawDerivedTables[index].InnerQuery)
	}
	for index := range analysis.CompoundBranches {
		bindLambdaParameters(analysis.CompoundBranches[index].Query)
	}
	for _, subquery := range analysis.PredicateSubqueries {
		bindLambdaParameters(subquery)
	}
	bindLambdaParameters(analysis.InsertSelect)
}

// bindQueryExpressionLambdas binds the lambdas of the expressions a query holds itself:
// its select list, its clauses and the sources of its ARRAY JOIN entries.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the query, rewritten in place.
func bindQueryExpressionLambdas(analysis *ast.RawQueryAnalysis) {
	for index := range analysis.OutputColumns {
		bindExpressionLambdas(analysis.OutputColumns[index].Expression)
	}
	for index := range analysis.ClauseExpressions {
		bindExpressionLambdas(analysis.ClauseExpressions[index].Expression)
	}
	for index := range analysis.ArrayJoinClauses {
		if source := analysis.ArrayJoinClauses[index].Source; source != nil {
			bindExpressionLambdas(source)
		}
	}
}

// bindExpressionLambdas binds the lambdas of every higher-order array call in an
// expression and in the subqueries it holds.
//
// The walk enters every node ast.Children knows except a lambda's bindings: an arrayFold
// lambda's binding is the call's own last argument, which the walk reaches there, so
// walking it again from the lambda would visit it twice, and once more for each arrayFold
// nested in it.
//
// Takes expression (ast.Expression) which is the expression, rewritten in place.
func bindExpressionLambdas(expression ast.Expression) {
	ast.Walk(expression, func(node ast.Expression) bool {
		switch typed := node.(type) {
		case *ast.FunctionCallExpression:
			if typed != nil {
				bindLambdaCall(typed)
			}
		case *ast.ScalarSubqueryExpression:
			if typed != nil {
				bindLambdaParameters(typed.InnerQuery)
			}
		case *ast.ExistsExpression:
			if typed != nil {
				bindLambdaParameters(typed.InnerQuery)
			}
		case *ast.LambdaExpression:
			if typed != nil {
				bindExpressionLambdas(typed.Body)
			}
			return false
		}
		return true
	})
}

// bindLambdaCall binds the first parameter of the lambda arrayFold takes as its first
// argument, the accumulator, to the call's last argument, the starting value.
//
// The analyser binds every other parameter to an element of the array argument in its
// place among those after the lambda, as it binds the parameters of every other
// higher-order function's lambda.
//
// Takes call (*ast.FunctionCallExpression) which is the call, whose lambda is bound in
// place.
func bindLambdaCall(call *ast.FunctionCallExpression) {
	if !strings.EqualFold(call.FunctionName, functionArrayFold) || len(call.Arguments) < minimumFoldArguments {
		return
	}
	lambda, isLambda := call.Arguments[0].(*ast.LambdaExpression)
	if !isLambda || lambda == nil || len(lambda.Parameters) == 0 {
		return
	}
	lambda.Bindings = []ast.Expression{call.Arguments[len(call.Arguments)-1]}
}
