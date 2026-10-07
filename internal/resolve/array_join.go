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
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// ResolveArrayJoinClauses registers what each ARRAY JOIN entry names in the scope chain.
//
// An entry whose alias is the name of the column it unfolds unfolds the column in place:
// the column, and the columns the entry's ElementPrefix extends it to, take the type of
// their array's element in the relation that holds them, so a reference to one and a star
// read the element. Any other entry adds its alias, naming the element, as a column a
// star leaves out. The element is NULL only where the array's element type says so,
// whether or not the entry is a LEFT ARRAY JOIN, which gives an empty array's row the
// element type's default value.
//
// Takes clauses ([]ast.RawArrayJoinClause) which holds the ARRAY JOIN entries captured by
// the parser.
// Takes scope (*ScopeChain) which holds the relations the sources are looked up in.
//
// Returns []*Error which holds a problem for each source that cannot be found or typed;
// the entry's alias then names a value of unknown type, so its uses are not reported too.
// The entries are no longer resolved once the context is done.
func (b *ScopeBuilder) ResolveArrayJoinClauses(
	ctx context.Context,
	clauses []ast.RawArrayJoinClause,
	scope *ScopeChain,
) []*Error {
	var problems []*Error
	for index := range clauses {
		if ctx.Err() != nil {
			return problems
		}
		if problem := b.resolveArrayJoinClause(ctx, &clauses[index], scope); problem != nil {
			problems = append(problems, problem)
		}
	}
	return problems
}

// resolveArrayJoinClause registers what one ARRAY JOIN entry names.
//
// Takes clause (*ast.RawArrayJoinClause) which is the entry.
// Takes scope (*ScopeChain) which holds the relations the source is looked up in.
//
// Returns *Error which is the problem with the entry's source, or nil.
func (b *ScopeBuilder) resolveArrayJoinClause(ctx context.Context, clause *ast.RawArrayJoinClause, scope *ScopeChain) *Error {
	reference := arrayJoinColumn(clause)
	if reference == nil {
		element, problem := b.arrayJoinExpressionElement(ctx, clause.Source, scope)
		addArrayJoinAlias(scope, clause.Alias, element)
		return problem
	}
	column, table, err := scope.ResolveColumn(reference.TableAlias, reference.ColumnName)
	if err != nil {
		addArrayJoinAlias(scope, clause.Alias, unknownArrayJoinElement())
		return unknownArrayJoinSource(err, reference, scope)
	}
	if scope.rules.Identifiers.Equal(clause.Alias, reference.ColumnName) && scope.holds(table) {
		unfoldInPlace(scope, table, column.Name, clause.ElementPrefix, b.unfoldedElement)
		return nil
	}
	addArrayJoinAlias(scope, clause.Alias, b.arrayJoinElement(column.SQLType))
	return nil
}

// arrayJoinElement returns the element an ARRAY JOIN reads from a value of a type: the
// element the dialect's ArrayJoinTyperPort gives, or else the array's element, NULL where
// its type says so, or the value's own type for one that is not a typed array.
//
// Takes sqlType (sqlmodel.SQLType) which is the source's type.
//
// Returns sqlmodel.SQLType which is the element's type, its Nullable flag saying whether
// the element may be NULL.
func (b *ScopeBuilder) arrayJoinElement(sqlType sqlmodel.SQLType) sqlmodel.SQLType {
	if element, unfolded := b.unfoldedElement(sqlType); unfolded {
		return element
	}
	return sqlType
}

// unfoldedElement returns the element an ARRAY JOIN unfolds a value of a type into: the
// element the dialect's ArrayJoinTyperPort gives, or a typed array's element.
//
// Takes sqlType (sqlmodel.SQLType) which is the source's type.
//
// Returns sqlmodel.SQLType which is the element's type.
// Returns bool which is false for a value that is neither, which an ARRAY JOIN leaves as
// it is.
func (b *ScopeBuilder) unfoldedElement(sqlType sqlmodel.SQLType) (sqlmodel.SQLType, bool) {
	if typer, types := b.arrayJoinTyper(); types {
		if element, typed := typer.ArrayJoinElement(sqlType); typed {
			return element, true
		}
	}
	if sqlType.Category == sqlmodel.TypeCategoryArray && sqlType.ElementType != nil {
		return *sqlType.ElementType, true
	}
	return sqlmodel.SQLType{}, false
}

// arrayJoinTyper returns the dialect's ArrayJoinTyperPort, from the builder's dialect or
// else its type resolver's.
//
// Returns dialect.ArrayJoinTyperPort which types ARRAY JOIN elements.
// Returns bool which is false when the dialect has none.
func (b *ScopeBuilder) arrayJoinTyper() (dialect.ArrayJoinTyperPort, bool) {
	if typer, types := b.engine.(dialect.ArrayJoinTyperPort); types {
		return typer, true
	}
	if b.TypeResolver == nil {
		return nil, false
	}
	typer, types := b.TypeResolver.engine.(dialect.ArrayJoinTyperPort)
	return typer, types
}

// arrayJoinExpressionElement types the element of an expression source.
//
// Takes source (ast.Expression) which is the source, or nil when the dialect did not
// parse it.
// Takes scope (*ScopeChain) which resolves the names the source holds.
//
// Returns sqlmodel.SQLType which is the element's type, unknown when the source is not
// typed.
// Returns *Error which is the problem typing the source, or nil, as it is when typing the
// source was interrupted.
func (b *ScopeBuilder) arrayJoinExpressionElement(ctx context.Context, source ast.Expression, scope *ScopeChain) (sqlmodel.SQLType, *Error) {
	if source == nil || b.TypeResolver == nil {
		return unknownArrayJoinElement(), nil
	}
	sqlType, _, err := b.TypeResolver.ResolveExpressionType(ctx, source, scope, nil)
	if interrupted(err) {
		return unknownArrayJoinElement(), nil
	}
	if err != nil {
		return unknownArrayJoinElement(), asError(err)
	}
	return b.arrayJoinElement(sqlType), nil
}

// holds reports whether a relation is registered in this scope rather than an enclosing
// one.
//
// Takes table (*ScopedTable) which is the relation.
//
// Returns bool which is true when the scope holds it.
func (s *ScopeChain) holds(table *ScopedTable) bool {
	if table == nil {
		return false
	}
	registered, exists := s.Tables[s.Key(table.Alias)]
	return exists && registered == table
}

// arrayJoinColumn returns the column an entry unfolds: the column reference its source
// is, or the column SourceColumn names when the dialect did not parse the source.
//
// Takes clause (*ast.RawArrayJoinClause) which is the entry.
//
// Returns *ast.ColumnRefExpression which is the column, or nil for an expression source.
func arrayJoinColumn(clause *ast.RawArrayJoinClause) *ast.ColumnRefExpression {
	if reference, isColumn := clause.Source.(*ast.ColumnRefExpression); isColumn && reference != nil {
		return reference
	}
	if clause.Source == nil && clause.SourceColumn != "" {
		return &ast.ColumnRefExpression{TableAlias: "", ColumnName: clause.SourceColumn, Span: ast.Span{}}
	}
	return nil
}

// unknownArrayJoinSource describes a source column that cannot be found, suggesting the
// visible column it most likely meant, at the reference when the dialect located it.
//
// Takes err (error) which is the failure looking the column up.
// Takes reference (*ast.ColumnRefExpression) which is the source.
// Takes scope (*ScopeChain) which supplies the suggestions.
//
// Returns *Error which is the problem.
func unknownArrayJoinSource(err error, reference *ast.ColumnRefExpression, scope *ScopeChain) *Error {
	problem := asError(err)
	if problem.Code == diag.CodeUnknownColumn {
		problem = newError(diag.CodeUnknownColumn, "unknown array join source column %q", reference.ColumnName).
			withSuggestion(scope.suggest(reference.ColumnName, scope.visibleColumnNames))
	}
	return asError(locatedAt(problem, reference.Span))
}

// unknownArrayJoinElement is the element of a source that cannot be typed: of unknown
// type and possibly NULL.
//
// Returns sqlmodel.SQLType which is the element's type.
func unknownArrayJoinElement() sqlmodel.SQLType {
	element := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
	element.Nullable = true
	return element
}

// addArrayJoinAlias registers an ARRAY JOIN alias as a relation of its own whose one
// column, the element, a reference can name but a star leaves out.
//
// Takes scope (*ScopeChain) which receives the relation.
// Takes alias (string) which is the alias.
// Takes element (sqlmodel.SQLType) which is the element's type.
func addArrayJoinAlias(scope *ScopeChain, alias string, element sqlmodel.SQLType) {
	if alias == "" {
		return
	}
	scope.AddDerivedTable(DerivedTableReference{
		Alias:    alias,
		Columns:  nil,
		JoinKind: ast.JoinInner,
		Source:   DerivedSourceUnnest,
		Using:    nil,
	})
	scope.hidden[scope.Key(alias)] = []ast.ScopedColumn{{Name: alias, SQLType: element, Nullable: element.Nullable}}
}

// unfoldInPlace gives a column of a relation, and the relation's other columns whose
// names start with prefix, the type of the element an ARRAY JOIN unfolds them into, on
// copies of the relation's columns so a column list another scope shares is left
// unchanged.
//
// Takes scope (*ScopeChain) which holds the relation.
// Takes table (*ScopedTable) which is the relation.
// Takes column (string) which is the name of the column unfolded.
// Takes prefix (string) which starts the names of the columns unfolded with it, or is
// empty.
// Takes element (func(sqlmodel.SQLType) (sqlmodel.SQLType, bool)) which types the element
// of a column's value, reporting false for a value an ARRAY JOIN leaves as it is.
func unfoldInPlace(scope *ScopeChain, table *ScopedTable, column string, prefix string,
	element func(sqlmodel.SQLType) (sqlmodel.SQLType, bool)) {
	identifiers := scope.rules.Identifiers
	unfolds := func(name string) bool {
		if identifiers.Equal(name, column) {
			return true
		}
		return prefix != "" && len(name) > len(prefix) && identifiers.Equal(name[:len(prefix)], prefix)
	}
	key := scope.Key(table.Alias)
	table.Columns = unfoldColumns(table.Columns, unfolds, element)
	if hidden, exists := scope.hidden[key]; exists {
		scope.hidden[key] = unfoldColumns(hidden, unfolds, element)
	}
	scope.joined = nil
}

// unfoldColumns returns a copy of a column list with each column unfolds selects typed as
// the element an ARRAY JOIN unfolds its value into, where it unfolds one.
//
// Takes columns ([]ast.ScopedColumn) which is the list, left unchanged.
// Takes unfolds (func(string) bool) which reports whether a column of a name unfolds.
// Takes element (func(sqlmodel.SQLType) (sqlmodel.SQLType, bool)) which types the element
// of a column's value.
//
// Returns []ast.ScopedColumn which is the copy.
func unfoldColumns(columns []ast.ScopedColumn, unfolds func(string) bool,
	element func(sqlmodel.SQLType) (sqlmodel.SQLType, bool)) []ast.ScopedColumn {
	unfolded := slices.Clone(columns)
	for index := range unfolded {
		if !unfolds(unfolded[index].Name) {
			continue
		}
		if unfoldedType, unfoldable := element(unfolded[index].SQLType); unfoldable {
			unfolded[index].SQLType = unfoldedType
			unfolded[index].Nullable = unfoldedType.Nullable
		}
	}
	return unfolded
}
