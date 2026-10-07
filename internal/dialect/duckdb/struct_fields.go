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
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// queryScope is what a query's FROM clauses make visible to its expressions: the names
// and aliases of its relations, and its base tables by the name they are referred to by.
type queryScope struct {
	// relations holds the lower-cased names and aliases of every visible relation.
	relations map[string]bool

	// tables lists the base tables in scope, innermost query first and in FROM order.
	tables []scopedTable
}

// newQueryScope builds the scope a query's FROM clause makes visible on top of its
// enclosing queries' scope.
//
// Takes outer (*queryScope) which is the enclosing scope, or nil.
// Takes tables ([]ast.TableReference) which are the query's comma-joined tables.
// Takes joins ([]ast.JoinClause) which are its explicit joins.
//
// Returns *queryScope which is the query's scope.
func newQueryScope(outer *queryScope, tables []ast.TableReference, joins []ast.JoinClause) *queryScope {
	scope := &queryScope{relations: map[string]bool{}, tables: nil}
	add := func(table ast.TableReference) {
		name := table.Alias
		if name == "" {
			name = table.Name
		}
		scope.relations[strings.ToLower(name)] = true
		scope.tables = append(scope.tables, scopedTable{name: strings.ToLower(name), reference: table})
	}
	for _, table := range tables {
		add(table)
	}
	for _, join := range joins {
		add(join.Table)
	}
	if outer != nil {
		for name := range outer.relations {
			scope.relations[name] = true
		}
		scope.tables = append(scope.tables, outer.tables...)
	}
	return scope
}

// scopedTable is a base table in scope and the lower-cased name it is referred to by.
type scopedTable struct {
	// name is the table's alias, or its name when it has none, lower-cased.
	name string

	// reference is the table.
	reference ast.TableReference
}

// structFieldResolver resolves dotted names and subscripts after query relations are
// known.
//
// A relation-qualified name reads a column; otherwise a dotted name or string subscript
// may read a struct field.
type structFieldResolver struct {
	// catalogue holds the tables whose struct columns a subscript may read a field of, and
	// the schemas a three-part name may start with; nil when unknown.
	catalogue *sqlmodel.Catalogue
}

// resolveQuery rewrites one query and the queries nested in it.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the query.
// Takes outer (*queryScope) which is the enclosing queries' scope, or nil.
func (r *structFieldResolver) resolveQuery(analysis *ast.RawQueryAnalysis, outer *queryScope) {
	if analysis == nil {
		return
	}
	scope := newQueryScope(outer, analysis.FromTables, analysis.JoinClauses)
	for index := range analysis.RawDerivedTables {
		scope.relations[strings.ToLower(analysis.RawDerivedTables[index].Alias)] = true
		r.resolveQuery(analysis.RawDerivedTables[index].InnerQuery, scope)
	}
	for index := range analysis.RawTableValuedFunctions {
		scope.relations[strings.ToLower(analysis.RawTableValuedFunctions[index].Alias)] = true
	}
	r.resolveColumns(analysis.OutputColumns, scope)
	r.resolveClauseExpressions(analysis.ClauseExpressions, scope)
	for index := range analysis.CTEDefinitions {
		r.resolveCTE(&analysis.CTEDefinitions[index], outer)
	}
	for index := range analysis.CompoundBranches {
		r.resolveQuery(analysis.CompoundBranches[index].Query, outer)
	}
	for _, subquery := range analysis.PredicateSubqueries {
		r.resolveQuery(subquery, scope)
	}
	r.resolveQuery(analysis.InsertSelect, scope)
}

// resolveCTE rewrites a CTE definition: the analysis of its body as any query, so its
// derived tables, table functions and own WITH clause are its scope, or, for a definition
// without one, its output columns over its own FROM clause.
//
// Takes definition (*ast.RawCTEDefinition) which is the definition, rewritten in place.
// Takes outer (*queryScope) which is the enclosing queries' scope, or nil.
func (r *structFieldResolver) resolveCTE(definition *ast.RawCTEDefinition, outer *queryScope) {
	if definition.Query != nil {
		r.resolveQuery(definition.Query, outer)
		return
	}
	r.resolveColumns(definition.OutputColumns, newQueryScope(outer, definition.FromTables, definition.JoinClauses))
	for branch := range definition.CompoundBranches {
		r.resolveQuery(definition.CompoundBranches[branch].Query, outer)
	}
}

// resolveClauseExpressions normalises qualified column names and struct field access.
//
// A two-part name reads a struct field only when its first part is a struct column of a
// table in scope and no relation; other ambiguous two-part names remain unchanged for the
// analyser to resolve.
//
// Takes expressions ([]ast.ClauseExpression) which are rewritten in place.
// Takes scope (*queryScope) which is the query's scope.
func (r *structFieldResolver) resolveClauseExpressions(expressions []ast.ClauseExpression, scope *queryScope) {
	for index := range expressions {
		expressions[index].Expression = rewriteExpression(expressions[index].Expression, func(node ast.Expression) ast.Expression {
			switch typed := node.(type) {
			case *ast.ColumnRefExpression:
				return r.resolveStructColumnField(typed, scope)
			case *ast.StructFieldAccessExpression:
				return r.resolveSchemaRelationColumn(typed, scope)
			case *ast.ArraySubscriptExpression:
				return r.resolveSubscript(typed, scope)
			default:
				return node
			}
		})
	}
}

// resolveStructColumnField rewrites a two-part name a.b in a clause into field b of
// column a when a names no relation in scope and a table in scope holds a struct column
// named a, as DuckDB reads it; any other two-part name is left for the analyser.
//
// Takes column (*ast.ColumnRefExpression) which is the reference.
// Takes scope (*queryScope) which is the scope.
//
// Returns ast.Expression which is the field access, or the reference unchanged.
func (r *structFieldResolver) resolveStructColumnField(column *ast.ColumnRefExpression, scope *queryScope) ast.Expression {
	if column.TableAlias == "" || scope.relations[strings.ToLower(column.TableAlias)] {
		return column
	}
	structColumn := &ast.ColumnRefExpression{TableAlias: "", ColumnName: column.TableAlias, Span: column.Span}
	found := findCatalogueColumn(r.catalogue, structColumn, scope)
	if found == nil || found.SQLType.Category != sqlmodel.TypeCategoryStruct {
		return column
	}
	return &ast.StructFieldAccessExpression{Struct: structColumn, FieldName: column.ColumnName}
}

// resolveColumns rewrites a query's output columns: a column written relation.column
// whose relation is not in scope reads a struct field, and each expression is rewritten.
//
// Takes columns ([]ast.RawOutputColumn) which are rewritten in place.
// Takes scope (*queryScope) which is the query's scope.
func (r *structFieldResolver) resolveColumns(columns []ast.RawOutputColumn, scope *queryScope) {
	for index := range columns {
		column := &columns[index]
		if column.Expression == nil && !column.IsStar && column.TableAlias != "" && !scope.relations[strings.ToLower(column.TableAlias)] {
			column.Expression = &ast.StructFieldAccessExpression{
				Struct:    &ast.ColumnRefExpression{TableAlias: "", ColumnName: column.TableAlias, Span: ast.Span{}},
				FieldName: column.ColumnName,
			}
			column.TableAlias = ""
			column.ColumnName = ""
			continue
		}
		column.Expression = r.resolveExpression(column.Expression, scope)
	}
}

// resolveExpression rewrites one expression's dotted names and literal-key subscripts,
// and the queries nested in it.
//
// Takes expression (ast.Expression) which is the expression.
// Takes scope (*queryScope) which is the scope it is evaluated in.
//
// Returns ast.Expression which is the rewritten expression.
func (r *structFieldResolver) resolveExpression(expression ast.Expression, scope *queryScope) ast.Expression {
	return rewriteExpression(expression, func(node ast.Expression) ast.Expression {
		switch typed := node.(type) {
		case *ast.ColumnRefExpression:
			if typed.TableAlias == "" || scope.relations[strings.ToLower(typed.TableAlias)] {
				return typed
			}
			return &ast.StructFieldAccessExpression{
				Struct:    &ast.ColumnRefExpression{TableAlias: "", ColumnName: typed.TableAlias, Span: typed.Span},
				FieldName: typed.ColumnName,
			}
		case *ast.StructFieldAccessExpression:
			return r.resolveSchemaQualifiedField(typed, scope)
		case *ast.ArraySubscriptExpression:
			return r.resolveSubscript(typed, scope)
		case *ast.ScalarSubqueryExpression:
			r.resolveQuery(typed.InnerQuery, scope)
		case *ast.ExistsExpression:
			r.resolveQuery(typed.InnerQuery, scope)
		}
		return node
	})
}

// resolveSchemaQualifiedField rewrites a three-part name a.b.c the parser read as field c
// of relation a's column b: when a names a schema in the catalogue and b a relation in
// scope, it is column c of relation b.
//
// Takes field (*ast.StructFieldAccessExpression) which is the field access, whose
// children are already rewritten.
// Takes scope (*queryScope) which is the scope.
//
// Returns ast.Expression which is the rewritten expression.
func (r *structFieldResolver) resolveSchemaQualifiedField(field *ast.StructFieldAccessExpression, scope *queryScope) ast.Expression {
	inner, isField := field.Struct.(*ast.StructFieldAccessExpression)
	if !isField || r.catalogue == nil {
		return field
	}
	schemaColumn, isColumn := inner.Struct.(*ast.ColumnRefExpression)
	if !isColumn || schemaColumn.TableAlias != "" || !scope.relations[strings.ToLower(inner.FieldName)] {
		return field
	}
	if r.catalogue.LookupSchema(schemaColumn.ColumnName) == nil {
		return field
	}
	return &ast.ColumnRefExpression{TableAlias: inner.FieldName, ColumnName: field.FieldName, Span: schemaColumn.Span}
}

// resolveSchemaRelationColumn rewrites a three-part name a.b.c the parser read as field c
// of column b of relation a: when a names a schema in the catalogue and b a relation in
// scope, it is column c of relation b.
//
// Takes field (*ast.StructFieldAccessExpression) which is the field access.
// Takes scope (*queryScope) which is the scope.
//
// Returns ast.Expression which is the column, or the field access unchanged.
func (r *structFieldResolver) resolveSchemaRelationColumn(field *ast.StructFieldAccessExpression, scope *queryScope) ast.Expression {
	column, isColumn := field.Struct.(*ast.ColumnRefExpression)
	if !isColumn || r.catalogue == nil || column.TableAlias == "" || !scope.relations[strings.ToLower(column.ColumnName)] ||
		r.catalogue.LookupSchema(column.TableAlias) == nil {
		return field
	}
	return &ast.ColumnRefExpression{TableAlias: column.ColumnName, ColumnName: field.FieldName, Span: column.Span}
}

// resolveSubscript rewrites a subscript written with a string literal key into a field
// access when what it subscripts is a struct column, such as berth['pontoon'], or a
// struct the query builds, such as {'k': 1}['k'].
//
// Takes subscript (*ast.ArraySubscriptExpression) which is the subscript.
// Takes scope (*queryScope) which is the scope.
//
// Returns ast.Expression which is the field access, or the subscript unchanged.
func (r *structFieldResolver) resolveSubscript(subscript *ast.ArraySubscriptExpression, scope *queryScope) ast.Expression {
	key := subscript.Key
	if key == "" {
		return subscript
	}
	if buildsStruct(subscript.Array) {
		return &ast.StructFieldAccessExpression{Struct: subscript.Array, FieldName: key}
	}
	subscripted := r.typeOf(subscript.Array, scope)
	if subscripted == nil || subscripted.Category != sqlmodel.TypeCategoryStruct {
		return subscript
	}
	return &ast.StructFieldAccessExpression{Struct: subscript.Array, FieldName: key}
}

// typeOf finds the type of a column reference, or of a field of one, from the catalogue's
// tables in scope.
//
// Takes expression (ast.Expression) which is the column or field access.
// Takes scope (*queryScope) which is the scope.
//
// Returns *sqlmodel.SQLType which is the type, or nil when it cannot be found.
func (r *structFieldResolver) typeOf(expression ast.Expression, scope *queryScope) *sqlmodel.SQLType {
	switch typed := expression.(type) {
	case *ast.ColumnRefExpression:
		return r.columnType(typed, scope)
	case *ast.StructFieldAccessExpression:
		parent := r.typeOf(typed.Struct, scope)
		if parent == nil {
			return nil
		}
		for index := range parent.StructFields {
			if strings.EqualFold(parent.StructFields[index].Name, typed.FieldName) {
				return &parent.StructFields[index].SQLType
			}
		}
	}
	return nil
}

// columnType finds a column's type among the catalogue tables in scope.
//
// Takes column (*ast.ColumnRefExpression) which is the reference.
// Takes scope (*queryScope) which is the scope.
//
// Returns *sqlmodel.SQLType which is the column's type, or nil when no table in scope
// holds it.
func (r *structFieldResolver) columnType(column *ast.ColumnRefExpression, scope *queryScope) *sqlmodel.SQLType {
	found := findCatalogueColumn(r.catalogue, column, scope)
	if found == nil {
		return nil
	}
	return &found.SQLType
}

// resolveStructFieldReferences rewrites the dotted names and literal-key subscripts of a
// query tree that read struct fields rather than relation columns.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the query tree, rewritten in place.
// Takes outer (*queryScope) which is the enclosing queries' scope, or nil at the top.
// Takes catalogue (*sqlmodel.Catalogue) which holds the tables, or nil.
func resolveStructFieldReferences(analysis *ast.RawQueryAnalysis, outer *queryScope, catalogue *sqlmodel.Catalogue) {
	resolver := &structFieldResolver{catalogue: catalogue}
	resolver.resolveQuery(analysis, outer)
}

// buildsStruct reports whether an expression builds a struct with named fields: a struct
// literal or a struct_pack call, which the parser reads as a struct constructor, or a
// struct_insert call adding its fields by name, which the parser gives a struct
// constructor of the added fields.
//
// Takes expression (ast.Expression) which is the expression.
//
// Returns bool which is true when the expression builds a struct.
func buildsStruct(expression ast.Expression) bool {
	switch typed := expression.(type) {
	case *ast.StructExpression:
		return typed != nil
	case *ast.FunctionCallExpression:
		if typed == nil || typed.Schema != "" || typed.FunctionName != functionStructInsert ||
			len(typed.Arguments) != minimumStructInsertArguments {
			return false
		}
		added, isStruct := typed.Arguments[1].(*ast.StructExpression)
		return isStruct && added != nil
	default:
		return false
	}
}

// findCatalogueColumn finds a column among the catalogue tables in scope.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the tables, or nil.
// Takes column (*ast.ColumnRefExpression) which is the reference.
// Takes scope (*queryScope) which is the scope.
//
// Returns *sqlmodel.Column which is the column, or nil when no table in scope holds it.
func findCatalogueColumn(catalogue *sqlmodel.Catalogue, column *ast.ColumnRefExpression, scope *queryScope) *sqlmodel.Column {
	if catalogue == nil {
		return nil
	}
	for _, scoped := range scope.tables {
		if column.TableAlias != "" && !strings.EqualFold(column.TableAlias, scoped.name) {
			continue
		}
		table := catalogue.LookupTable(scoped.reference.Schema, scoped.reference.Name)
		if table == nil {
			continue
		}
		for index := range table.Columns {
			if strings.EqualFold(table.Columns[index].Name, column.ColumnName) {
				return &table.Columns[index]
			}
		}
	}
	return nil
}
