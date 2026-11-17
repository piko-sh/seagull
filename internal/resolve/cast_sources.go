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

package resolve

import (
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// readsAsComputedTime reports whether an output column holds a date or timestamp the
// query computes rather than reads straight from a column, which drivers that convert
// only declared temporal columns return as it is stored.
//
// Takes sqlType (sqlmodel.SQLType) which is the column's resolved type.
// Takes expression (ast.Expression) which is the column's expression.
//
// Returns bool which is true for a computed date or timestamp.
func readsAsComputedTime(sqlType sqlmodel.SQLType, expression ast.Expression) bool {
	if sqlType.Category != sqlmodel.TypeCategoryTemporal ||
		(sqlType.Temporal != sqlmodel.TemporalKindDate && sqlType.Temporal != sqlmodel.TemporalKindTimestamp) {
		return false
	}
	_, column := expression.(*ast.ColumnRefExpression)
	return !column
}

// castPreservesValues reports whether a cast keeps every value of its source column and
// compares those values the same way, so a filter or override on the source column means
// the same thing on the cast result.
//
// A cast to another category, such as an integer to text, compares differently, and a
// narrowing cast, such as text to a shorter varchar or a timestamp to a date, changes
// values. Both lose the source column.
//
// Takes source (sqlmodel.SQLType) which is the source column's type.
// Takes target (sqlmodel.SQLType) which is the cast result's resolved type.
//
// Returns bool which is true when the cast neither converts nor narrows the values.
func castPreservesValues(source, target sqlmodel.SQLType) bool {
	if source.Category != target.Category || source.Category == sqlmodel.TypeCategoryUnknown {
		return false
	}
	if source.Temporal != target.Temporal || source.TimeZone != target.TimeZone || source.Unsigned != target.Unsigned {
		return false
	}
	if target.Width != 0 && source.Width != 0 && target.Width < source.Width {
		return false
	}
	return !narrows(source.Length, target.Length) && !narrows(source.Precision, target.Precision) && !narrows(source.Scale, target.Scale)
}

// narrows reports whether a cast's limit on a type parameter is tighter than the
// source's.
//
// Takes source (*int) which is the source's limit, or nil when it has none.
// Takes target (*int) which is the cast's limit, or nil when it sets none.
//
// Returns bool which is true when the cast sets a limit the source does not meet.
func narrows(source, target *int) bool {
	return target != nil && (source == nil || *target < *source)
}

// applyCastColumnSource records the underlying source column of an output expression that
// is a column reference wrapped in nothing but CASTs, so a cast-only projection such as
// page_id::content.uuid_v4 keeps the source metadata a bare column reference would carry.
//
// Cast-only projections retain their source for the dynamic runtime builder's allow-list
// and for column overrides and array wrapping. The expression resolver still determines
// the cast result's type and nullability. Other expression shapes, such as a function
// call, arithmetic or COALESCE, are left without a source.
//
// Takes output (*sqlmodel.OutputColumn) which receives the resolved source metadata.
// Takes expression (ast.Expression) which is the output column's expression.
// Takes scope (*ScopeChain) which resolves the underlying column.
//
// Returns bool which is true when the chain of casts ran past the depth limit, so the
// column behind it was not found.
func applyCastColumnSource(
	output *sqlmodel.OutputColumn,
	expression ast.Expression,
	scope *ScopeChain,
) bool {
	reference, limited := unwrapCasts(expression)
	if reference == nil {
		return limited
	}
	column, table, resolveError := scope.ResolveColumn(reference.TableAlias, reference.ColumnName)
	if resolveError != nil || column == nil {
		return false
	}
	if _, isCast := expression.(*ast.CastExpression); isCast && !castPreservesValues(column.SQLType, output.SQLType) {
		return false
	}
	output.SourceColumn = column.Name
	if table != nil {
		output.SourceTable = table.Name
		output.SourceSchema = table.Schema
		output.SourceQualifier = table.Alias
	}
	return false
}

// unwrapCastToColumnRef peels any chain of CAST expressions and returns the column
// reference they wrap, or nil when the expression is not a column reference behind zero
// or more casts.
//
// Takes expression (ast.Expression) which is the expression to peel.
//
// Returns *ast.ColumnRefExpression which is the wrapped column reference, or nil when the
// expression is not a column behind zero or more casts.
func unwrapCastToColumnRef(expression ast.Expression) *ast.ColumnRefExpression {
	reference, _ := unwrapCasts(expression)
	return reference
}

// unwrapCasts peels a chain of CAST expressions as unwrapCastToColumnRef does, and says
// when the chain ran past the depth limit.
//
// Takes expression (ast.Expression) which is the expression to peel.
//
// Returns *ast.ColumnRefExpression which is the wrapped column reference, or nil.
// Returns bool which is true when the chain is longer than MaxExpressionResolveDepth, so
// it was not followed to its end.
func unwrapCasts(expression ast.Expression) (*ast.ColumnRefExpression, bool) {
	for range MaxExpressionResolveDepth {
		switch typed := expression.(type) {
		case *ast.ColumnRefExpression:
			return typed, false
		case *ast.CastExpression:
			if typed == nil {
				return nil, false
			}
			expression = typed.Inner
		default:
			return nil, false
		}
	}
	_, stillCast := expression.(*ast.CastExpression)
	_, column := expression.(*ast.ColumnRefExpression)
	return nil, stillCast || column
}
