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
	"cmp"
	"maps"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
)

// resolveElementAccess rewrites a dotted reference whose qualifier names no table in
// scope into an access to an element of a column, as ClickHouse reads it.
//
// In ClickHouse `basket.fish` is the fish subcolumn of a JSON column basket, and
// `location.lat` the lat element of a named tuple, whenever no table or alias in scope is
// called basket or location. The parser cannot tell the two readings apart while it reads
// the projection, because the FROM clause comes after it, so this pass runs once the
// statement is read: a reference qualified by a name no table, alias, CTE or database in
// scope, or in an enclosing scope, carries becomes a StructFieldAccess on the column of
// that name. Without it the column is reported as unknown and dropped. An ARRAY JOIN
// alias names a value, the element, rather than a relation, so a reference it qualifies
// reads a field of the element.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the statement's analysis, rewritten in
// place.
// Takes enclosing (map[string]bool) which holds the qualifiers enclosing scopes provide.
func resolveElementAccess(analysis *ast.RawQueryAnalysis, enclosing map[string]bool) {
	if analysis == nil {
		return
	}
	qualifiers := scopeQualifiers(analysis, enclosing)
	for index := range analysis.OutputColumns {
		resolveOutputElementAccess(&analysis.OutputColumns[index], qualifiers)
	}
	for index := range analysis.ArrayJoinClauses {
		resolveArrayJoinSourceElementAccess(&analysis.ArrayJoinClauses[index], qualifiers)
	}
	analysis.GroupByColumns = qualifiedReferencesOnly(analysis.GroupByColumns, qualifiers)
	readUnfoldedColumns(analysis)
	for index := range analysis.RawDerivedTables {
		resolveElementAccess(analysis.RawDerivedTables[index].InnerQuery, enclosing)
	}
	for index := range analysis.CompoundBranches {
		resolveElementAccess(analysis.CompoundBranches[index].Query, enclosing)
	}
	for index := range analysis.CTEDefinitions {
		resolveCTEElementAccess(&analysis.CTEDefinitions[index], enclosing)
	}
	resolveElementAccess(analysis.InsertSelect, enclosing)
}

// resolveCTEElementAccess applies resolveElementAccess to a CTE definition: to the
// analysis of its body, whose relations, derived tables included, are its scope, and, for
// a definition without one, to its output columns over its own FROM clause.
//
// Takes definition (*ast.RawCTEDefinition) which is the definition, rewritten in place.
// Takes enclosing (map[string]bool) which holds the qualifiers enclosing scopes provide.
func resolveCTEElementAccess(definition *ast.RawCTEDefinition, enclosing map[string]bool) {
	if definition.Query != nil {
		resolveElementAccess(definition.Query, enclosing)
		return
	}
	qualifiers := cloneQualifiers(enclosing)
	addTableQualifiers(qualifiers, definition.FromTables)
	delete(qualifiers, "")
	for index := range definition.OutputColumns {
		resolveOutputElementAccess(&definition.OutputColumns[index], qualifiers)
	}
	for index := range definition.CompoundBranches {
		resolveElementAccess(definition.CompoundBranches[index].Query, enclosing)
	}
}

// scopeQualifiers returns the names a column reference may be qualified by in a scope:
// the enclosing scopes' names and this scope's tables, their aliases and databases, its
// derived tables, table functions and CTEs.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the scope.
// Takes enclosing (map[string]bool) which holds the enclosing scopes' names.
//
// Returns map[string]bool which holds every qualifier in scope.
func scopeQualifiers(analysis *ast.RawQueryAnalysis, enclosing map[string]bool) map[string]bool {
	qualifiers := cloneQualifiers(enclosing)
	addTableQualifiers(qualifiers, analysis.FromTables)
	for index := range analysis.RawDerivedTables {
		qualifiers[analysis.RawDerivedTables[index].Alias] = true
	}
	for index := range analysis.RawTableValuedFunctions {
		function := analysis.RawTableValuedFunctions[index]
		qualifiers[function.Alias], qualifiers[function.FunctionName] = true, true
	}
	for index := range analysis.CTEDefinitions {
		qualifiers[analysis.CTEDefinitions[index].Name] = true
	}
	delete(qualifiers, "")
	return qualifiers
}

// resolveArrayJoinSourceElementAccess rewrites the expression source of an ARRAY JOIN
// entry as an output column's expression is rewritten; a bare column source was read with
// its qualifier already.
//
// Takes clause (*ast.RawArrayJoinClause) which is the entry, rewritten in place.
// Takes qualifiers (map[string]bool) which holds the qualifiers in scope.
func resolveArrayJoinSourceElementAccess(clause *ast.RawArrayJoinClause, qualifiers map[string]bool) {
	if _, isColumn := clause.Source.(*ast.ColumnRefExpression); isColumn || clause.Source == nil {
		return
	}
	clause.Source = rewriteElementAccess(clause.Source, qualifiers)
}

// readUnfoldedColumns resolves element references to columns unfolded by ARRAY JOIN.
//
// An in-place ARRAY JOIN makes a flattened column reference denote its unfolded element.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the scope, rewritten in place.
func readUnfoldedColumns(analysis *ast.RawQueryAnalysis) {
	paths := make(map[string]bool)
	for index := range analysis.ArrayJoinClauses {
		clause := analysis.ArrayJoinClauses[index]
		if strings.Contains(clause.SourceColumn, nestedFieldSeparator) && clause.Alias == clause.SourceColumn {
			paths[clause.SourceColumn] = true
		}
	}
	if len(paths) == 0 {
		return
	}
	for index := range analysis.OutputColumns {
		analysis.OutputColumns[index].Expression = readUnfolded(analysis.OutputColumns[index].Expression, paths)
	}
}

// readUnfolded returns an expression with every read of an element whose path names an
// unfolded column replaced by a reference to that column. Every node ast.RewriteChildren
// knows is searched, lambda bodies, named arguments and row values included, but not the
// subqueries the expression holds, which have scopes of their own.
//
// Takes expression (ast.Expression) which is the expression, which is not changed; nil
// stays nil.
// Takes paths (map[string]bool) which holds the names of the unfolded columns.
//
// Returns ast.Expression which is the expression itself, or a copy holding the
// replacements.
func readUnfolded(expression ast.Expression, paths map[string]bool) ast.Expression {
	if ast.IsNil(expression) {
		return expression
	}
	if path := elementAccessPath(expression); paths[path] {
		return &ast.ColumnRefExpression{TableAlias: "", ColumnName: path, Span: ast.Span{}}
	}
	return ast.RewriteChildren(expression, func(child ast.Expression) ast.Expression {
		return readUnfolded(child, paths)
	})
}

// cloneQualifiers copies a set of qualifiers, so a scope can add its own without changing
// its enclosing scope's.
//
// Takes qualifiers (map[string]bool) which is the set, possibly nil.
//
// Returns map[string]bool which is a copy that is never nil.
func cloneQualifiers(qualifiers map[string]bool) map[string]bool {
	clone := maps.Clone(qualifiers)
	if clone == nil {
		clone = make(map[string]bool)
	}
	return clone
}

// addTableQualifiers adds the names a column of each table may be qualified by: the
// table's name, its alias and its database.
//
// Takes qualifiers (map[string]bool) which receives the names.
// Takes tables ([]ast.TableReference) which are the tables in scope.
func addTableQualifiers(qualifiers map[string]bool, tables []ast.TableReference) {
	for index := range tables {
		qualifiers[tables[index].Name], qualifiers[tables[index].Alias], qualifiers[tables[index].Schema] = true, true, true
	}
}

// resolveOutputElementAccess resolves qualified references in an output expression.
//
// References to column elements become field accesses, with display names following
// ClickHouse's rules.
//
// Takes column (*ast.RawOutputColumn) which is the column, rewritten in place.
// Takes qualifiers (map[string]bool) which holds the qualifiers in scope.
func resolveOutputElementAccess(column *ast.RawOutputColumn, qualifiers map[string]bool) {
	if column.IsStar {
		return
	}
	if column.Expression == nil {
		if column.TableAlias == "" || qualifiers[column.TableAlias] {
			column.Name = cmp.Or(column.Name, column.ColumnName)
			return
		}
		column.Expression = elementAccess(column.TableAlias, column.ColumnName)
		column.Name = cmp.Or(column.Name, column.TableAlias+nestedFieldSeparator+column.ColumnName)
		column.TableAlias, column.ColumnName = "", ""
		return
	}
	column.Expression = rewriteElementAccess(column.Expression, qualifiers)
}

// elementAccessPath returns the name ClickHouse gives an unaliased read of an element of
// a column, the path from the column to the element, such as legs.minutes for
// flights.legs.minutes; a table qualifier is not part of it.
//
// Takes expression (ast.Expression) which is the projected expression.
//
// Returns string which is the path, or empty when the expression reads no element.
func elementAccessPath(expression ast.Expression) string {
	access, isAccess := expression.(*ast.StructFieldAccessExpression)
	if !isAccess {
		return ""
	}
	container := access.Struct
	if unary, isUnary := container.(*ast.UnaryOpExpression); isUnary && unary.Operator == operatorSubcolumns {
		container = unary.Operand
	}
	switch typed := container.(type) {
	case *ast.ColumnRefExpression:
		return typed.ColumnName + nestedFieldSeparator + access.FieldName
	case *ast.StructFieldAccessExpression:
		if path := elementAccessPath(typed); path != "" {
			return path + nestedFieldSeparator + access.FieldName
		}
	}
	return ""
}

// rewriteElementAccess returns an expression with every column reference whose qualifier
// is not in scope turned into an element access. Every node ast.RewriteChildren knows is
// searched, lambda bodies, named arguments and row values included, but not the
// subqueries the expression holds, which have scopes of their own.
//
// Takes expression (ast.Expression) which is the expression, which is not changed.
// Takes qualifiers (map[string]bool) which holds the qualifiers in scope.
//
// Returns ast.Expression which is the expression itself, or a copy holding the
// replacements.
func rewriteElementAccess(expression ast.Expression, qualifiers map[string]bool) ast.Expression {
	if ast.IsNil(expression) {
		return expression
	}
	if column, isColumn := expression.(*ast.ColumnRefExpression); isColumn {
		if column.TableAlias != "" && !qualifiers[column.TableAlias] {
			return elementAccess(column.TableAlias, column.ColumnName)
		}
		return column
	}
	return ast.RewriteChildren(expression, func(child ast.Expression) ast.Expression {
		return rewriteElementAccess(child, qualifiers)
	})
}

// elementAccess builds the access to a named element of a column, typed from the column's
// subcolumns; see subcolumnAccess.
//
// Takes column (string) which is the column's name.
// Takes element (string) which is the element's name.
//
// Returns ast.Expression which is the access.
func elementAccess(column, element string) ast.Expression {
	return subcolumnAccess(&ast.ColumnRefExpression{TableAlias: "", ColumnName: column, Span: ast.Span{}}, element)
}

// qualifiedReferencesOnly drops the references whose qualifier is not in scope, which are
// elements of a column rather than columns of a table.
//
// Takes references ([]ast.ColumnReference) which are the references.
// Takes qualifiers (map[string]bool) which holds the qualifiers in scope.
//
// Returns []ast.ColumnReference which holds the column references.
func qualifiedReferencesOnly(references []ast.ColumnReference, qualifiers map[string]bool) []ast.ColumnReference {
	kept := references[:0:0]
	for _, reference := range references {
		if reference.TableAlias == "" || qualifiers[reference.TableAlias] {
			kept = append(kept, reference)
		}
	}
	return kept
}
