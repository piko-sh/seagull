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
)

// groupingKeys tracks grouping keys omitted from some result rows.
//
// Source columns become nullable when their key is omitted; expression keys are tracked
// by their select aliases.
type groupingKeys struct {
	// columns holds the source columns the keys read.
	columns map[groupingKeyColumn]struct{}

	// aliases holds the unqualified names of keys that name no source column.
	aliases []string

	// active is true while the scope's select list is resolved.
	active bool
}

// groupingKeyColumn identifies a source column a grouping key reads.
type groupingKeyColumn struct {
	// table is the relation the column belongs to, as the scope registered it.
	table *ScopedTable

	// name is the column's name as the relation holds it.
	name string
}

// registerGroupingKeys records the keys a query's grouping modifier leaves out of some
// rows, once the query's relations are registered, so its select list reads them as NULL.
// A modifier whose engine fills a default value in such a row is recorded as no keys.
//
// Takes sets (*ast.GroupingSets) which describes the modifier, or is nil for a query
// without one.
func (s *ScopeChain) registerGroupingKeys(sets *ast.GroupingSets) {
	if s == nil || sets == nil || sets.FillsDefaults || len(sets.Keys) == 0 {
		return
	}
	keys := &groupingKeys{columns: make(map[groupingKeyColumn]struct{}), aliases: nil, active: false}
	for _, key := range sets.Keys {
		for _, reference := range columnReferencesIn(key) {
			column, table, err := s.ResolveColumn(reference.TableAlias, reference.ColumnName)
			if err == nil && column != nil {
				keys.columns[groupingKeyColumn{table: table, name: column.Name}] = struct{}{}
				continue
			}
			if reference.TableAlias == "" {
				keys.aliases = append(keys.aliases, reference.ColumnName)
			}
		}
	}
	s.grouping = keys
}

// activateGroupingKeys makes the scope's grouping keys read as NULL until the returned
// function is called, for the time its select list is resolved.
//
// Returns func() which deactivates them again.
func (s *ScopeChain) activateGroupingKeys() func() {
	if s == nil || s.grouping == nil {
		return func() {}
	}
	s.grouping.active = true
	return func() { s.grouping.active = false }
}

// readsLeftOutKey reports whether a column a select list being resolved reads is a key
// the query's grouping modifier leaves out of some rows, in this scope or a scope it
// sees, such as the query a scalar subquery of the select list is nested in.
//
// Takes table (*ScopedTable) which is the relation the column was resolved in.
// Takes name (string) which is the column's name.
//
// Returns bool which is true when the column may read as NULL.
func (s *ScopeChain) readsLeftOutKey(table *ScopedTable, name string) bool {
	for scope, depth := s, 0; scope != nil && depth < maxScopeChainDepth; scope, depth = scope.parent, depth+1 {
		if scope.grouping == nil || !scope.grouping.active {
			continue
		}
		if _, found := scope.grouping.columns[groupingKeyColumn{table: table, name: name}]; found {
			return true
		}
	}
	return false
}

// nullGroupedAliases makes nullable each output column whose select item a grouping key
// names by its alias.
//
// Takes rawColumns ([]ast.RawOutputColumn) which is the select list.
// Takes projection (*Projection) which holds the resolved columns and their origins.
func (s *ScopeChain) nullGroupedAliases(rawColumns []ast.RawOutputColumn, projection *Projection) {
	if s == nil || s.grouping == nil || len(s.grouping.aliases) == 0 {
		return
	}
	for index := range projection.Columns {
		if index >= len(projection.Origins) {
			return
		}
		raw := rawColumns[projection.Origins[index]]
		if raw.IsStar || raw.Name == "" {
			continue
		}
		for _, alias := range s.grouping.aliases {
			if s.rules.Identifiers.Equal(raw.Name, alias) {
				projection.Columns[index].Nullable = true
			}
		}
	}
}

// leftOutColumn returns a column read while a select list is resolved as nullable when it
// is a key the query's grouping modifier leaves out of some rows.
//
// Takes column (*ast.ScopedColumn) which is the resolved column.
// Takes table (*ScopedTable) which is the relation it was resolved in.
//
// Returns *ast.ScopedColumn which is the column, or a nullable copy of it.
func (s *ScopeChain) leftOutColumn(column *ast.ScopedColumn, table *ScopedTable) *ast.ScopedColumn {
	if column == nil || column.Nullable || !s.readsLeftOutKey(table, column.Name) {
		return column
	}
	nullable := *column
	nullable.Nullable = true
	return &nullable
}

// columnReferencesIn returns the column references an expression holds outside any
// subquery, the arguments of function calls included.
//
// Takes expression (ast.Expression) which is the expression.
//
// Returns []*ast.ColumnRefExpression which holds the references in order.
func columnReferencesIn(expression ast.Expression) []*ast.ColumnRefExpression {
	var references []*ast.ColumnRefExpression
	for node := range preorder(expression) {
		if reference, isColumn := node.(*ast.ColumnRefExpression); isColumn && reference != nil {
			references = append(references, reference)
		}
	}
	return references
}
