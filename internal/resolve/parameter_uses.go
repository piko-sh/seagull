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
	"context"
	"slices"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/directive"
	"seagull.sh/seagull/internal/sqlmodel"
)

// problemSet collects problems, each reported once.
type problemSet struct {
	// reported holds the problems already collected.
	reported map[Error]bool

	// diagnostics holds the problems in the order they were found.
	diagnostics []*Error
}

// add collects a problem unless it was collected before.
//
// Takes problem (*Error) which is the problem; nil adds nothing.
func (set *problemSet) add(problem *Error) {
	if problem == nil || set.reported[*problem] {
		return
	}
	set.reported[*problem] = true
	set.diagnostics = append(set.diagnostics, problem)
}

// directUse resolves a parameter's type when it is directly assigned to or compared with
// a column. Casts, calls and parameters embedded in larger assigned expressions do not
// fix a type here.
//
// Takes raw (ast.RawParameterReference) which is the use.
// Takes scope (*ScopeChain) which is the scope the column is read in.
//
// Returns sqlmodel.SQLType which is the column's type.
// Returns bool which is false when the use fixes no type.
func (r *TypeResolver) directUse(raw ast.RawParameterReference, scope *ScopeChain) (sqlmodel.SQLType, bool) {
	unknown := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
	if raw.CastType != nil || raw.ColumnReference == nil || raw.EnclosingFunctionName != "" || r.assigned.partial[raw.Number] {
		return unknown, false
	}
	if raw.Context != sqlmodel.ParameterContextAssignment && raw.Context != sqlmodel.ParameterContextComparison {
		return unknown, false
	}
	column, _, err := scope.ResolveColumn(raw.ColumnReference.TableAlias, raw.ColumnReference.ColumnName)
	if err != nil || column == nil {
		return unknown, false
	}
	if compared, found := r.parameterComparisons[raw.Number]; found && raw.Context == sqlmodel.ParameterContextComparison &&
		compared.sqlType.Category != column.SQLType.Category {
		return unknown, false
	}
	return column.SQLType, true
}

// conflictingUse reports Q004 when direct uses of a parameter require types the engine
// converts between only explicitly. The first direct use establishes the reference type.
//
// Takes directTypes (map[int]sqlmodel.SQLType) which maps each parameter to the type its
// first such use fixes; the use is recorded there when it is the first.
// Takes existing (*sqlmodel.QueryParameter) which is the parameter as typed so far, or
// nil.
// Takes raw (ast.RawParameterReference) which is the use being merged.
// Takes scope (*ScopeChain) which is the scope the use's column is read in.
//
// Returns *Error which is the Q004 warning, or nil.
func (r *TypeResolver) conflictingUse(
	directTypes map[int]sqlmodel.SQLType,
	existing *sqlmodel.QueryParameter,
	raw ast.RawParameterReference,
	scope *ScopeChain,
) *Error {
	sqlType, direct := r.directUse(raw, scope)
	if !direct {
		return nil
	}
	known, seen := directTypes[raw.Number]
	if !seen {
		directTypes[raw.Number] = sqlType
		return nil
	}
	if existing == nil || known.Category == sqlmodel.TypeCategoryUnknown || sqlType.Category == sqlmodel.TypeCategoryUnknown ||
		known.Category == sqlType.Category {
		return nil
	}
	if r.engine.CanImplicitCast(known, sqlType) || r.engine.CanImplicitCast(sqlType, known) {
		return nil
	}
	return newError(diag.CodeExpressionTypeError,
		"parameter %s is used both where %s and where %s is expected, which the engine converts between only explicitly; "+
			"the generated code takes %s, so a value of the other type fails; cast one use, or pass the value as two parameters",
		existing.Name, typeLabel(known), typeLabel(sqlType), typeLabel(existing.SQLType))
}

// typeFlatParameter types one use of a parameter of the query's own clauses against the
// query's scope, or the scope of a compound arm where the query's cannot, nullable where
// COALESCE falls back from it.
//
// Takes raw (ast.RawParameterReference) which is the use.
// Takes flat (flatParameterPass) which holds the scopes.
//
// Returns sqlmodel.SQLType which is the type the use expects.
// Returns bool which is true when the parameter may be NULL there.
// Returns error which is the problem typing it, or nil.
func (r *TypeResolver) typeFlatParameter(ctx context.Context, raw ast.RawParameterReference, flat flatParameterPass) (sqlmodel.SQLType, bool, error) {
	sqlType, nullable, resolveError := r.resolveParameterType(ctx, raw, flat.scope)
	if resolveError != nil {
		if armType, armNullable, found := r.resolveParameterInArms(ctx, raw, flat.armScopes); found {
			sqlType, nullable, resolveError = armType, armNullable, nil
		}
	}
	if compared, found := r.parameterComparisons[raw.Number]; found && compared.nullable {
		nullable = true
	}
	return sqlType, nullable, resolveError
}

// fallbackParameterName names a parameter nothing it is compared with names: after the
// argument a function's signature declares it as, limit or offset, or its position.
//
// Takes raw (ast.RawParameterReference) which is the parameter.
//
// Returns string which is the name.
func (r *TypeResolver) fallbackParameterName(raw ast.RawParameterReference) string {
	if raw.Context == sqlmodel.ParameterContextFunctionArgument && raw.EnclosingFunctionName != "" {
		if argumentName, named := r.functionResolver.ArgumentName(raw.EnclosingFunctionName, raw.ArgumentOrdinal); named {
			return argumentName
		}
	}
	switch raw.Context {
	case sqlmodel.ParameterContextLimit:
		return "limit"
	case sqlmodel.ParameterContextOffset:
		return "offset"
	default:
	}
	return positionalName(raw.Number)
}

// assignPositionalTargets gives each parameter an INSERT without a column list assigns to
// a column by its position the column as its target, unless the dialect recorded one or a
// cast types it.
//
// Takes rawParameters ([]ast.RawParameterReference) which are the parameters; they are
// copied, never changed.
// Takes targets (map[int]ast.ColumnReference) which maps parameter numbers to columns.
//
// Returns []ast.RawParameterReference which holds the parameters with their targets.
func assignPositionalTargets(rawParameters []ast.RawParameterReference, targets map[int]ast.ColumnReference) []ast.RawParameterReference {
	if len(targets) == 0 {
		return rawParameters
	}
	assigned := slices.Clone(rawParameters)
	for index := range assigned {
		target, found := targets[assigned[index].Number]
		if !found || assigned[index].ColumnReference != nil || assigned[index].CastType != nil {
			continue
		}
		assigned[index].ColumnReference = &target
		assigned[index].Context = sqlmodel.ParameterContextAssignment
	}
	return assigned
}

// typeLabel names a type for a message: its engine name, or its category when it has
// none.
//
// Takes sqlType (sqlmodel.SQLType) which is the type.
//
// Returns string which is the label.
func typeLabel(sqlType sqlmodel.SQLType) string {
	if sqlType.EngineName != "" {
		return sqlType.EngineName
	}
	return sqlType.Category.String()
}

// declaredParameterName returns the name a query gives a parameter itself: the name its
// binding directive gives, or the placeholder's own name.
//
// Takes raw (ast.RawParameterReference) which is the parameter.
// Takes directiveNumberMap (map[int]*directive.ParameterDirective) which specifies
// directives keyed by number.
// Takes directiveNameMap (map[string]*directive.ParameterDirective) which specifies
// directives keyed by name.
//
// Returns string which is the name.
// Returns bool which is false when the query names the parameter nowhere.
func declaredParameterName(
	raw ast.RawParameterReference,
	directiveNumberMap map[int]*directive.ParameterDirective,
	directiveNameMap map[string]*directive.ParameterDirective,
) (string, bool) {
	if raw.Name != "" {
		if parameterDirective, exists := directiveNameMap[raw.Name]; exists {
			return parameterDirective.Name, true
		}
		return raw.Name, true
	}
	if parameterDirective, exists := directiveNumberMap[raw.Number]; exists {
		return parameterDirective.Name, true
	}
	return "", false
}
