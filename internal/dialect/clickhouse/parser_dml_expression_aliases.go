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
	"maps"
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
	resolveExpressionAliasValues(analysis, enclosing, nil)
}

// resolveExpressionAliasValues resolves WITH expression names throughout a query tree, a
// name whose expression the parser kept standing for that expression and any other for a
// value of unknown type, in the clauses and in the select list.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the query, rewritten in place.
// Takes enclosing ([]string) which lists the names the enclosing queries give.
// Takes enclosingValues (map[string]ast.Expression) which maps the enclosing names to
// their expressions, where kept.
func resolveExpressionAliasValues(analysis *ast.RawQueryAnalysis, enclosing []string, enclosingValues map[string]ast.Expression) {
	if analysis == nil {
		return
	}
	aliases, values := aliasScope(analysis, enclosing, enclosingValues)
	for index := range analysis.ClauseExpressions {
		analysis.ClauseExpressions[index].Expression = replaceAliasValues(analysis.ClauseExpressions[index].Expression, aliases, values)
	}
	replaceOutputAliases(analysis, aliases, values)
	for index := range analysis.RawDerivedTables {
		resolveExpressionAliasValues(analysis.RawDerivedTables[index].InnerQuery, aliases, values)
	}
	for index := range analysis.CompoundBranches {
		resolveExpressionAliasValues(analysis.CompoundBranches[index].Query, aliases, values)
	}
	for index := range analysis.CTEDefinitions {
		resolveExpressionAliasValues(analysis.CTEDefinitions[index].Query, aliases, values)
	}
	resolveExpressionAliasValues(analysis.InsertSelect, aliases, values)
}

// aliasScope collects the WITH names and expressions visible to a query.
//
// The query's own expression replaces an enclosing one with the same name. Expressions
// that refer to another WITH name or contain a subquery are omitted, leaving the name
// with an unknown type. Expanding them could grow the tree without bound or resolve an
// inner name as a column.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the query.
// Takes enclosing ([]string) which lists the names the enclosing queries give.
// Takes enclosingValues (map[string]ast.Expression) which maps them to kept expressions.
//
// Returns []string which lists the names.
// Returns map[string]ast.Expression which maps names to kept expressions.
func aliasScope(analysis *ast.RawQueryAnalysis, enclosing []string, enclosingValues map[string]ast.Expression) ([]string, map[string]ast.Expression) {
	detail, found := analysis.Detail.(*queryDetail)
	if !found || len(detail.expressionAliases) == 0 {
		return enclosing, enclosingValues
	}
	names := append(slices.Clone(enclosing), detail.expressionAliases...)
	values := make(map[string]ast.Expression, len(enclosingValues)+len(detail.expressionAliasValues))
	maps.Copy(values, enclosingValues)
	for _, name := range detail.expressionAliases {
		delete(values, name)
	}
	for name, value := range detail.expressionAliasValues {
		if standsAlone(value, names) {
			values[name] = value
		}
	}
	return names, values
}

// standsAlone reports whether a WITH expression can stand in for its name as it is,
// because it names no WITH name and holds no subquery.
//
// Takes value (ast.Expression) which is the expression.
// Takes names ([]string) which are the WITH names in scope.
//
// Returns bool which is true when the expression can stand in for its name.
func standsAlone(value ast.Expression, names []string) bool {
	alone := true
	ast.Walk(value, func(node ast.Expression) bool {
		if _, isSubquery := ast.InnerQuery(node); isSubquery {
			alone = false
		}
		if column, isColumn := node.(*ast.ColumnRefExpression); isColumn && column.TableAlias == "" && slices.Contains(names, column.ColumnName) {
			alone = false
		}
		return alone
	})
	return alone
}

// replaceOutputAliases replaces the select list's references to WITH names, a whole item
// naming one standing for what the name stands for rather than for a column.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the query, rewritten in place.
// Takes aliases ([]string) which are the names.
// Takes values (map[string]ast.Expression) which maps names to kept expressions.
func replaceOutputAliases(analysis *ast.RawQueryAnalysis, aliases []string, values map[string]ast.Expression) {
	for index := range analysis.OutputColumns {
		column := &analysis.OutputColumns[index]
		switch {
		case column.IsStar:
		case column.TableAlias == "" && column.ColumnName != "" && slices.Contains(aliases, column.ColumnName):
			column.Expression = aliasStandIn(column.ColumnName, values)
			column.ColumnName = ""
		case !ast.IsNil(column.Expression):
			column.Expression = replaceAliasValues(column.Expression, aliases, values)
		}
	}
}

// aliasStandIn returns what a reference to a WITH name stands for: the expression it
// names, where the parser kept it, or a value of unknown type.
//
// Takes name (string) which is the name.
// Takes values (map[string]ast.Expression) which maps names to kept expressions.
//
// Returns ast.Expression which stands in for the reference.
func aliasStandIn(name string, values map[string]ast.Expression) ast.Expression {
	if value, kept := values[name]; kept {
		return value
	}
	return &ast.UnknownExpression{}
}

// replaceAliasValues returns an expression with every unqualified reference to one of the
// names replaced by what the name stands for, as replaceAliasReferences does with values
// of unknown type.
//
// Takes expression (ast.Expression) which is the expression.
// Takes aliases ([]string) which are the names.
// Takes values (map[string]ast.Expression) which maps names to kept expressions.
//
// Returns ast.Expression which is the expression with the references replaced.
func replaceAliasValues(expression ast.Expression, aliases []string, values map[string]ast.Expression) ast.Expression {
	if len(values) == 0 {
		return replaceAliasReferences(expression, aliases)
	}
	if ast.IsNil(expression) {
		return expression
	}
	if column, isColumn := expression.(*ast.ColumnRefExpression); isColumn {
		if column.TableAlias == "" && slices.Contains(aliases, column.ColumnName) {
			return aliasStandIn(column.ColumnName, values)
		}
		return column
	}
	if _, isSubquery := ast.InnerQuery(expression); isSubquery {
		return replaceAliasReferences(expression, aliases)
	}
	if _, isLambda := expression.(*ast.LambdaExpression); isLambda {
		return replaceAliasReferences(expression, aliases)
	}
	return ast.RewriteChildren(expression, func(child ast.Expression) ast.Expression {
		return replaceAliasValues(child, aliases, values)
	})
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
