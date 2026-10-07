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

// recordParameterColumn applies a compared or assigned column's Go type and declarations
// about element nullability to a parameter, preserving earlier declarations. For
// quantified array comparisons, the column's override applies to elements when the
// dialect can type the array.
//
// Takes parameter (*sqlmodel.QueryParameter) which receives the column's declarations;
// nil is ignored.
// Takes raw (ast.RawParameterReference) which is the occurrence.
// Takes scope (*ScopeChain) which resolves the column.
func (r *TypeResolver) recordParameterColumn(
	parameter *sqlmodel.QueryParameter,
	raw ast.RawParameterReference,
	scope *ScopeChain,
) {
	if parameter == nil || (parameter.GoTypeOverride != nil && parameter.NonNullElements != nil) {
		return
	}
	source := r.parameterSourceColumn(raw, scope)
	if source == nil || r.readsQuantified(raw.Number) && parameter.SQLType.Category != sqlmodel.TypeCategoryArray {
		return
	}
	if parameter.GoTypeOverride == nil && source.GoTypeOverride != nil {
		parameter.GoTypeOverride = new(*source.GoTypeOverride)
	}
	if parameter.NonNullElements == nil && source.NonNullElements != nil {
		parameter.NonNullElements = new(*source.NonNullElements)
	}
}

// parameterSourceColumn finds the catalogue column a parameter occurrence takes its value
// from, whose go_type override and element declarations the parameter shares.
//
// A parameter that declares a different SQL type takes none of them, using that type's
// own Go mapping instead.
//
// Takes raw (ast.RawParameterReference) which is the occurrence.
// Takes scope (*ScopeChain) which resolves the column.
//
// Returns *sqlmodel.Column which is the catalogue's column, or nil when there is none.
func (r *TypeResolver) parameterSourceColumn(raw ast.RawParameterReference, scope *ScopeChain) *sqlmodel.Column {
	if r.catalogue == nil {
		return nil
	}
	reference, columnScope, found := r.overrideColumn(raw, scope)
	if !found || columnScope == nil {
		return nil
	}
	column, table, err := columnScope.ResolveColumn(reference.TableAlias, reference.ColumnName)
	if err != nil || column == nil || table == nil {
		return nil
	}
	if raw.CastType != nil && !RestatesColumnType(r.restatedType(raw), column.SQLType) {
		return nil
	}
	return r.catalogue.FindColumn(table.Schema, table.Name, column.Name)
}

// restatedType returns the type a parameter occurrence's cast declares of the column it
// takes its value from: the cast's type, or, for an array a quantified comparison reads,
// as in code = ANY($1::text[]), the type of its elements.
//
// Takes raw (ast.RawParameterReference) which is the occurrence, whose cast is set.
//
// Returns sqlmodel.SQLType which is the type compared with the column's.
func (r *TypeResolver) restatedType(raw ast.RawParameterReference) sqlmodel.SQLType {
	declared := r.resolveCustomType(*raw.CastType)
	if r.readsQuantified(raw.Number) && declared.Category == sqlmodel.TypeCategoryArray && declared.ElementType != nil {
		return *declared.ElementType
	}
	return declared
}

// readsQuantified reports whether a quantified comparison of the query being resolved
// reads a parameter as the array of values it compares a column with, as in code =
// ANY($1).
//
// Takes number (int) which is the parameter's number.
//
// Returns bool which is true when one does.
func (r *TypeResolver) readsQuantified(number int) bool {
	compared, found := r.parameterColumns[number]
	return found && compared.quantified
}

// overrideColumn finds the catalogue column supplying a parameter occurrence's value.
//
// Assignments take precedence over predicates and dialect comparisons. LIKE patterns,
// function arguments, limits and offsets inherit no column override, and neither does a
// parameter a predicate compares with an expression that is no column alone.
//
// Takes raw (ast.RawParameterReference) which is the occurrence.
// Takes scope (*ScopeChain) which resolves a column the dialect reports.
//
// Returns ast.ColumnReference which names the column.
// Returns *ScopeChain which is the scope that resolves it.
// Returns bool which is false when the occurrence takes no column's value.
func (r *TypeResolver) overrideColumn(raw ast.RawParameterReference, scope *ScopeChain) (ast.ColumnReference, *ScopeChain, bool) {
	if compared, found := r.parameterColumns[raw.Number]; found && compared.quantified {
		return compared.reference, compared.scope, true
	}
	switch raw.Context {
	case sqlmodel.ParameterContextLike, sqlmodel.ParameterContextFunctionArgument,
		sqlmodel.ParameterContextLimit, sqlmodel.ParameterContextOffset:
		return ast.ColumnReference{}, nil, false
	case sqlmodel.ParameterContextAssignment:
		if raw.ColumnReference != nil {
			return *raw.ColumnReference, scope, true
		}
	default:
	}
	if compared, found := r.parameterColumns[raw.Number]; found {
		return compared.reference, compared.scope, !compared.expression
	}
	switch raw.Context {
	case sqlmodel.ParameterContextComparison, sqlmodel.ParameterContextInList, sqlmodel.ParameterContextBetween:
		if raw.CastType == nil && raw.ColumnReference != nil && !r.parameterPatterns[raw.Number] {
			return *raw.ColumnReference, scope, true
		}
	default:
	}
	return ast.ColumnReference{}, nil, false
}

// comparedWithExpression reports whether the predicates of the query being resolved
// compare a parameter with an expression that is no column alone, such as a call over a
// column, and with no column.
//
// Takes number (int) which is the parameter's number.
//
// Returns bool which is true when one does.
func (r *TypeResolver) comparedWithExpression(number int) bool {
	compared, found := r.parameterColumns[number]
	return found && compared.expression
}
