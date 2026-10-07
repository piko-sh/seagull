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
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// ScopeKind identifies the context that created a scope in the nested scope chain used
// for column resolution.
type ScopeKind uint8

const (
	// ScopeKindQuery is the root scope of a top-level query.
	ScopeKindQuery ScopeKind = iota

	// ScopeKindCTE is a scope created for a common table expression.
	ScopeKindCTE

	// ScopeKindSubquery is a scope created for a subquery, which sees the names of the
	// enclosing scope, as a correlated subquery and a LATERAL join do.
	ScopeKindSubquery

	// ScopeKindParameter is a scope created for a function body where parameter names
	// resolve as Unknown-typed bindings. The shared function-body analyser binds each
	// declared parameter into this scope so a body expression can reference parameters
	// through the existing column-resolution path.
	ScopeKindParameter
)

// ScopedTable is a table within a scope, carrying its columns with JOIN-adjusted
// nullability.
type ScopedTable struct {
	// Schema is the table's schema name.
	Schema string

	// Name is the table name.
	Name string

	// Alias is the table alias used in the query, or the table name if no alias was
	// specified.
	Alias string

	// Columns holds the columns with JOIN-adjusted nullability.
	Columns []ast.ScopedColumn

	// CatalogueTable is the catalogue's table or view the relation reads, or nil for a
	// relation the catalogue does not hold, such as a derived table or a common table
	// expression.
	CatalogueTable *sqlmodel.Table

	// JoinKind is the join type that introduced this table into the scope.
	JoinKind ast.JoinKind

	// Function reports a relation a table-valued function produces.
	Function bool

	// IsWithoutRowID indicates the table has no implicit rowid column.
	IsWithoutRowID bool

	// nullExtended reports a relation a later join may leave without a row, such as the left
	// side of a RIGHT join, whose columns are therefore nullable.
	nullExtended bool
}

// ScopeRules holds the dialect facts used to resolve names and row values. The zero value
// compares names exactly and supplies no implicit columns or row values.
type ScopeRules struct {
	// RowValues types the current row of a relation a query names where a value is expected;
	// nil when the dialect has no such values.
	RowValues dialect.RowValueTyperPort

	// RowIDAliases lists the names the implicit row identifier column answers to; empty when
	// the dialect has no such column.
	RowIDAliases []string

	// SystemColumns lists the columns the engine gives every table without the table
	// declaring them, besides the row identifier; a query may name them wherever a column of
	// a table is visible, and their type is unknown.
	SystemColumns []string

	// RowIDType is the type of the implicit row identifier column.
	RowIDType sqlmodel.SQLType

	// Identifiers is how the dialect compares identifiers.
	Identifiers sqlmodel.IdentifierCase

	// AliasOperands is the set of clauses in which an expression may name a select-list
	// alias as an operand, so a parameter compared with the alias takes its item's type, as
	// the dialect's Info.SelectAliasOperands says.
	AliasOperands ast.Clause

	// LateralAliases reports that a select item may name an earlier item of the same select
	// list by the alias it gives, as the dialect's Info.SelectAliases says by holding
	// ast.ClauseSelectList.
	LateralAliases bool

	// ShadowingAliases is the set of clauses in which a select-list alias takes precedence
	// over a column of the same name: the dialect's Info.SelectAliases where it is a
	// dialect.AliasPrecedencePort whose aliases shadow columns, and none otherwise.
	ShadowingAliases ast.Clause
}

// isSystemColumn reports whether a column name is one of the columns every table has
// without declaring them.
//
// Takes name (string) which is the column name.
//
// Returns bool which is true when the name is a system column's.
func (rules ScopeRules) isSystemColumn(name string) bool {
	for _, column := range rules.SystemColumns {
		if rules.Identifiers.Equal(column, name) || rules.Identifiers.Equal(rules.Identifiers.Fold(column, false), name) {
			return true
		}
	}
	return false
}

// isRowID reports whether a column name is one of the implicit row identifier aliases.
//
// Takes name (string) which is the canonical column name.
//
// Returns bool which is true when the name is an alias of the implicit row identifier.
func (rules ScopeRules) isRowID(name string) bool {
	for _, alias := range rules.RowIDAliases {
		if rules.Identifiers.Equal(rules.Identifiers.Fold(alias, false), name) {
			return true
		}
	}
	return false
}

// selectListAliases is a select list being resolved in a dialect with lateral column
// aliases: the items resolved so far, which a later item may name by alias, the items one
// has named, and the select list resolving before it.
type selectListAliases struct {
	// outer is the select list the scope was resolving before this one, restored when this
	// one ends; nil for none.
	outer *selectListAliases

	// items holds the items resolved so far that have an alias, in order.
	items []selectListItem

	// named lists, in order, the indices of the items a later item named.
	named []int
}

// share makes a resolved item visible to the later items under its alias. A nil receiver,
// a select list without lateral column aliases, shares nothing.
//
// Takes index (int) which is the item's index in the select list.
// Takes alias (string) which is the item's alias or inferred name; empty shares nothing.
// Takes column (*sqlmodel.OutputColumn) which is the item's resolved column.
func (aliases *selectListAliases) share(index int, alias string, column *sqlmodel.OutputColumn) {
	if aliases == nil || alias == "" {
		return
	}
	aliases.items = append(aliases.items, selectListItem{
		column: ast.ScopedColumn{Name: alias, SQLType: column.SQLType, Nullable: column.Nullable},
		index:  index,
	})
}

// selectListItem is one resolved item of a select list a later item may name.
type selectListItem struct {
	// column is the item's column, under its alias.
	column ast.ScopedColumn

	// index is the item's index in the select list.
	index int
}

// suggestionBudget counts the "did you mean" suggestions the scopes of one chain may
// still work out.
type suggestionBudget struct {
	// left is how many more suggestions may be worked out.
	left int
}

// take spends one suggestion of the budget.
//
// Returns bool which is false when the budget is spent, or for a nil budget.
func (budget *suggestionBudget) take() bool {
	if budget == nil || budget.left <= 0 {
		return false
	}
	budget.left--
	return true
}

// ScopeRulesFor reads a dialect's scope rules from its Info and core types.
//
// Takes engine (TypeRules) which is the dialect.
//
// Returns ScopeRules which holds the dialect's identifier rules, implicit row identifier
// column, system columns, row values, whether it has lateral column aliases, the clauses
// in which an alias may be an operand and those in which an alias shadows a column.
func ScopeRulesFor(engine TypeRules) ScopeRules {
	info := engine.Info()
	rules := ScopeRules{
		RowValues:        nil,
		RowIDType:        sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""),
		RowIDAliases:     info.RowIDAliases,
		SystemColumns:    info.SystemColumns,
		Identifiers:      info.Identifiers,
		AliasOperands:    info.SelectAliasOperands & info.SelectAliases,
		LateralAliases:   info.SelectAliases.Includes(ast.ClauseSelectList),
		ShadowingAliases: 0,
	}
	if precedence, ranks := engine.(dialect.AliasPrecedencePort); ranks && precedence.SelectAliasesShadowColumns() {
		rules.ShadowingAliases = info.SelectAliases
	}
	if len(info.RowIDAliases) > 0 {
		rules.RowIDType = engine.CoreTypes().RowID
	}
	if typer, isTyper := engine.(dialect.RowValueTyperPort); isTyper {
		rules.RowValues = typer
	}
	return rules
}
