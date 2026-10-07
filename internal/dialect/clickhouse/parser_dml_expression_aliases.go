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
	"slices"

	"seagull.sh/seagull/internal/dialect/ast"
)

// resolveExpressionAliases resolves WITH expression names throughout a query tree.
//
// Expression aliases are replaced with unknown-typed values, including references in
// nested queries.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the query, rewritten in place.
// Takes enclosing ([]string) which lists the names the enclosing queries give.
func resolveExpressionAliases(analysis *ast.RawQueryAnalysis, enclosing []string) {
	if analysis == nil {
		return
	}
	aliases := enclosing
	if detail, found := analysis.Detail.(*queryDetail); found && len(detail.expressionAliases) > 0 {
		aliases = append(slices.Clone(enclosing), detail.expressionAliases...)
	}
	for index := range analysis.ClauseExpressions {
		analysis.ClauseExpressions[index].Expression = replaceAliasReferences(analysis.ClauseExpressions[index].Expression, aliases)
	}
	for index := range analysis.RawDerivedTables {
		resolveExpressionAliases(analysis.RawDerivedTables[index].InnerQuery, aliases)
	}
	for index := range analysis.CompoundBranches {
		resolveExpressionAliases(analysis.CompoundBranches[index].Query, aliases)
	}
	for index := range analysis.CTEDefinitions {
		resolveExpressionAliases(analysis.CTEDefinitions[index].Query, aliases)
	}
	resolveExpressionAliases(analysis.InsertSelect, aliases)
}

// replaceAliasReferences returns an expression with every unqualified reference to one of
// the names replaced by an expression of unknown type.
//
// Every node ast.RewriteChildren knows is searched, lambda bodies, named arguments and
// row values included, and the queries nested in the expression have their own references
// replaced in place. A lambda's parameters shadow the names they share inside its body.
//
// Takes expression (ast.Expression) which is the expression, whose own nodes are not
// changed.
// Takes aliases ([]string) which are the names.
//
// Returns ast.Expression which is the expression itself, or a copy holding the
// replacements.
func replaceAliasReferences(expression ast.Expression, aliases []string) ast.Expression {
	if ast.IsNil(expression) {
		return expression
	}
	if column, isColumn := expression.(*ast.ColumnRefExpression); isColumn {
		if column.TableAlias == "" && slices.Contains(aliases, column.ColumnName) {
			return &ast.UnknownExpression{}
		}
		return column
	}
	if inner, isSubquery := ast.InnerQuery(expression); isSubquery {
		resolveExpressionAliases(inner, aliases)
		return expression
	}
	if lambda, isLambda := expression.(*ast.LambdaExpression); isLambda {
		return replaceLambdaAliasReferences(lambda, aliases)
	}
	return ast.RewriteChildren(expression, func(child ast.Expression) ast.Expression {
		return replaceAliasReferences(child, aliases)
	})
}

// replaceLambdaAliasReferences returns a lambda with the references to the names
// replaced: in its bindings, which the enclosing query gives, every name; in its body,
// every name none of its parameters shadows.
//
// Takes lambda (*ast.LambdaExpression) which is the lambda, whose own nodes are not
// changed.
// Takes aliases ([]string) which are the names.
//
// Returns ast.Expression which is the lambda itself, or a copy holding the replacements.
func replaceLambdaAliasReferences(lambda *ast.LambdaExpression, aliases []string) ast.Expression {
	bodyAliases := aliases
	if slices.ContainsFunc(lambda.Parameters, func(parameter string) bool { return slices.Contains(aliases, parameter) }) {
		bodyAliases = slices.DeleteFunc(slices.Clone(aliases), func(alias string) bool {
			return slices.Contains(lambda.Parameters, alias)
		})
	}
	bodyPending := lambda.Body != nil
	return ast.RewriteChildren(lambda, func(child ast.Expression) ast.Expression {
		if bodyPending {
			bodyPending = false
			return replaceAliasReferences(child, bodyAliases)
		}
		return replaceAliasReferences(child, aliases)
	})
}
