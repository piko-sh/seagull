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
	"cmp"
	"maps"
	"slices"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// maxScopeChainDepth caps how many scopes a lookup visits: the scope it starts in and
	// those enclosing it.
	//
	// Each nested query adds one scope. The cap allows a lookup from the deepest permitted
	// query to reach the outermost scope; query nesting itself is bounded by
	// MaxExpressionResolveDepth.
	maxScopeChainDepth = MaxExpressionResolveDepth + 1

	// maxSuggestionsPerChain caps the "did you mean" suggestions the scopes of one chain
	// work out for unknown columns and relations.
	//
	// Each suggestion compares the unknown name with all visible names. After the cap is
	// reached, problems are still reported but suggestions are omitted, limiting the cost
	// for wide relations.
	maxSuggestionsPerChain = 1_000
)

// ScopeChain provides nested scope resolution for column references. Each scope can
// contain tables and CTEs, and can delegate to a parent scope for correlated subqueries,
// whose relations include those of a LATERAL join.
//
// This enables correct column resolution across CTEs and subqueries using nested scopes
// rather than a flat lookup.
//
// A ScopeChain is not safe for concurrent use.
type ScopeChain struct {
	// parent holds the enclosing scope for correlated subqueries.
	parent *ScopeChain

	// suggestions counts the suggestions the scopes of the chain may still work out; every
	// scope of a chain shares the root's.
	suggestions *suggestionBudget

	// Tables holds the tables registered in this scope, keyed by alias.
	Tables map[string]*ScopedTable

	// order holds the keys of Tables in the order the relations were registered.
	order []string

	// registered maps the key of each registered relation to its place in order.
	registered map[string]int

	// fromOrder holds the keys of the relations in the order the FROM clause lists them,
	// when the dialect recorded it; it takes precedence over the registration order.
	fromOrder []string

	// fromPositions maps each key of fromOrder to its first place there.
	fromPositions map[string]int

	// ordered caches the relations in the order OrderedTables gives them; it is cleared
	// whenever a relation is registered or the FROM order is set.
	ordered []*ScopedTable

	// cteOrder caches the keys of CTEs in sorted order; it is rebuilt when a CTE is added.
	cteOrder []string

	// using maps the key of each relation joined with a USING clause or a NATURAL join to
	// the columns the join merges.
	using map[string]*ast.JoinUsing

	// hidden maps the key of each relation to its hidden columns: columns a query can name
	// but that are not part of the relation's row, so a star leaves them out.
	hidden map[string][]ast.ScopedColumn

	// joined caches the row the scope's relations produce; it is cleared whenever a relation
	// is registered or its join changes.
	joined *joinedRow

	// CTEs holds the resolved CTEs in this scope, keyed under the dialect's identifier
	// rules.
	CTEs map[string]*ResolvedCTE

	// declaredColumns holds the columns the query declares for a source whose columns are
	// known only when the query runs, such as a file a table function reads; the first such
	// source registered in the scope takes them.
	declaredColumns []ast.ScopedColumn

	// grouping holds the keys the query's grouping modifier leaves out of some rows, or is
	// nil for a query without one.
	grouping *groupingKeys

	// selectList holds, while the scope's select list is resolved and where the dialect has
	// lateral column aliases, the items resolved so far, which a later item may name by
	// alias; nil otherwise.
	selectList *selectListAliases

	// aliasOperands holds the aliased items of the scope's resolved select list, which an
	// expression of a clause the dialect lists in Info.SelectAliasOperands may name; nil
	// where it lists none.
	aliasOperands []selectListItem

	// rules holds the dialect's identifier rules and implicit row identifier column, shared
	// by every scope in the chain.
	rules ScopeRules

	// nulling is the latest place in FROM order of a registered relation whose join leaves
	// the relations before it without a row, as a RIGHT or FULL join does; its position is
	// -1 when no such relation is registered.
	nulling relationPlace

	// depth is the number of scopes enclosing this one: the nesting depth of the query the
	// scope belongs to, which every walk into a nested query continues from, so the queries
	// of one statement share one nesting budget.
	depth int

	// kind holds the scope kind (query, CTE, subquery or parameter).
	kind ScopeKind

	// grouped records whether the scope's query has a GROUP BY clause, or that nothing
	// recorded it.
	grouped ungroupedGrouping

	// groupingWithheld marks a scope whose query was built without the dialect's record of
	// its clauses, such as the body of a CTE the dialect gives no analysis of its own, so
	// whether it has a GROUP BY clause is not known.
	groupingWithheld bool
}

// NewScopeChain creates a new scope with the given kind and optional parent.
//
// Takes kind (ScopeKind) which specifies the scope kind.
// Takes parent (*ScopeChain) which specifies the enclosing scope, or nil for a root
// scope.
//
// Returns *ScopeChain which holds the initialised scope with empty tables and CTEs.
func NewScopeChain(kind ScopeKind, parent *ScopeChain) *ScopeChain {
	var rules ScopeRules
	if parent != nil {
		rules = parent.rules
	}
	return newScopeChain(kind, parent, rules)
}

// NewRootScopeChain creates the outermost scope of a chain under a dialect's scope rules,
// which every scope created from it inherits.
//
// Takes kind (ScopeKind) which specifies the scope kind.
// Takes rules (ScopeRules) which holds the dialect's identifier rules and implicit row
// identifier column.
//
// Returns *ScopeChain which holds the new scope.
func NewRootScopeChain(kind ScopeKind, rules ScopeRules) *ScopeChain {
	return newScopeChain(kind, nil, rules)
}

// newScopeChain creates a scope with the given parent and rules.
//
// Takes kind (ScopeKind) which specifies the scope kind.
// Takes parent (*ScopeChain) which is the enclosing scope, or nil.
// Takes rules (ScopeRules) which holds the dialect's name-resolution rules.
//
// Returns *ScopeChain which holds the new scope.
func newScopeChain(kind ScopeKind, parent *ScopeChain, rules ScopeRules) *ScopeChain {
	depth := 0
	suggestions := &suggestionBudget{left: maxSuggestionsPerChain}
	if parent != nil {
		depth = parent.depth + 1
		suggestions = parent.suggestions
	}
	return &ScopeChain{
		parent:           parent,
		suggestions:      suggestions,
		depth:            depth,
		Tables:           make(map[string]*ScopedTable),
		order:            nil,
		registered:       make(map[string]int),
		fromOrder:        nil,
		fromPositions:    nil,
		ordered:          nil,
		nulling:          relationPlace{position: -1, rank: -1},
		cteOrder:         nil,
		using:            make(map[string]*ast.JoinUsing),
		hidden:           make(map[string][]ast.ScopedColumn),
		joined:           nil,
		CTEs:             make(map[string]*ResolvedCTE),
		rules:            rules,
		kind:             kind,
		declaredColumns:  nil,
		selectList:       nil,
		aliasOperands:    nil,
		grouping:         nil,
		grouped:          groupingUnknown,
		groupingWithheld: false,
	}
}

// DeclareRuntimeColumns records the columns a query declares, through its column
// directives, for a source whose columns are known only when the query runs, such as a
// table function reading a file. The first such source the scope registers takes them, so
// the query's names and SELECT * resolve against them.
//
// Takes columns ([]ast.ScopedColumn) which are the declared columns, in declaration
// order.
func (s *ScopeChain) DeclareRuntimeColumns(columns []ast.ScopedColumn) {
	s.declaredColumns = columns
}

// Rules returns the scope rules the chain resolves names under.
//
// Returns ScopeRules which holds the dialect's identifier rules and implicit row
// identifier column.
func (s *ScopeChain) Rules() ScopeRules {
	return s.rules
}

// Key returns the key a canonical name is filed under in this chain's maps.
//
// Takes name (string) which is a canonical table alias or CTE name.
//
// Returns string which is the comparison key under the chain's identifier rules.
func (s *ScopeChain) Key(name string) string {
	return s.rules.Identifiers.Key(name)
}

// Table returns the table registered in this scope under an alias.
//
// Takes alias (string) which is the canonical table alias.
//
// Returns *ScopedTable which is the table.
// Returns bool which is true when the scope holds a table under the alias.
func (s *ScopeChain) Table(alias string) (*ScopedTable, bool) {
	table, exists := s.Tables[s.Key(alias)]
	return table, exists
}

// CTE returns the CTE registered in this scope under a name.
//
// Takes name (string) which is the canonical CTE name.
//
// Returns *ResolvedCTE which is the CTE.
// Returns bool which is true when the scope holds a CTE under the name.
func (s *ScopeChain) CTE(name string) (*ResolvedCTE, bool) {
	cte, exists := s.CTEs[s.Key(name)]
	return cte, exists
}

// AddTable registers a catalogue table and adjusts column nullability for its join. Outer
// joins make columns nullable on any side that may lack a row, including relations
// preceding RIGHT or FULL joins.
//
// Takes table (ast.TableReference) which specifies the table name, schema, and alias.
// Takes joinKind (ast.JoinKind) which specifies the join type for nullability adjustment.
// Takes catalogueTable (*sqlmodel.Table) which specifies the catalogue entry with base
// column definitions.
func (s *ScopeChain) AddTable(
	table ast.TableReference,
	joinKind ast.JoinKind,
	catalogueTable *sqlmodel.Table,
) {
	alias := table.Alias
	if alias == "" {
		alias = table.Name
	}

	columns := make([]ast.ScopedColumn, 0, len(catalogueTable.Columns))
	var hidden []ast.ScopedColumn
	for index := range catalogueTable.Columns {
		nullable := catalogueTable.Columns[index].Nullable
		if joinKind.Nullable.Includes(ast.JoinSideRight) {
			nullable = true
		}
		column := ast.ScopedColumn{
			Name:     catalogueTable.Columns[index].Name,
			SQLType:  arrayWrappedSQLType(&catalogueTable.Columns[index]),
			Nullable: nullable,
		}
		if catalogueTable.Columns[index].Hidden {
			hidden = append(hidden, column)
			continue
		}
		columns = append(columns, column)
	}

	s.registerTable(alias, &ScopedTable{
		Schema:         table.Schema,
		Name:           table.Name,
		Alias:          alias,
		Columns:        columns,
		CatalogueTable: catalogueTable,
		Function:       false,
		JoinKind:       joinKind,
		IsWithoutRowID: catalogueTable.IsWithoutRowID,
		nullExtended:   false,
	})
	s.hidden[s.Key(alias)] = hidden
}

// AddCTE registers a resolved CTE in the current scope.
//
// Takes name (string) which specifies the CTE name as declared in the WITH clause.
// Takes columns ([]ast.ScopedColumn) which specifies the resolved output columns of the
// CTE.
func (s *ScopeChain) AddCTE(name string, columns []ast.ScopedColumn) {
	s.CTEs[s.Key(name)] = &ResolvedCTE{
		name:    name,
		columns: columns,
	}
	s.cteOrder = nil
}

// AddCTEAsTable registers a CTE referenced from a FROM or JOIN clause as a scoped table
// under the given alias.
//
// Unqualified column lookups search Tables before falling back to CTE definitions, so
// referenced CTEs participate in column lookup and ambiguity detection alongside the
// other relations of the FROM clause.
//
// Takes alias (string) which specifies the alias the CTE was referenced under, or the
// CTE's own name when no AS clause was used.
// Takes columns ([]ast.ScopedColumn) which specifies the resolved output columns of the
// CTE.
// Takes joinKind (ast.JoinKind) which specifies the join kind so nullability propagates
// correctly when the CTE participates in an outer JOIN.
//
// The supplied columns are copied into a separate slice so outer-join nullability can
// change the registered columns without changing the CTE definition's columns. Nested
// SQLType data remains shared.
func (s *ScopeChain) AddCTEAsTable(alias string, columns []ast.ScopedColumn, joinKind ast.JoinKind) {
	scoped := make([]ast.ScopedColumn, len(columns))
	copy(scoped, columns)
	if joinKind.Nullable.Includes(ast.JoinSideRight) {
		for index := range scoped {
			scoped[index].Nullable = true
		}
	}
	s.registerTable(alias, &ScopedTable{
		Name:           alias,
		Alias:          alias,
		Columns:        scoped,
		CatalogueTable: nil,
		Function:       false,
		JoinKind:       joinKind,
		Schema:         "",
		IsWithoutRowID: false,
		nullExtended:   false,
	})
}

// AddDerivedTable registers a virtual table (from UNNEST, FLATTEN, table-valued
// functions, or subqueries in FROM) in the current scope. Derived tables are resolved
// identically to catalogue tables.
//
// Takes reference (DerivedTableReference) which specifies the derived table alias,
// columns, and join kind.
func (s *ScopeChain) AddDerivedTable(reference DerivedTableReference) {
	columns := reference.Columns
	if reference.JoinKind.Nullable.Includes(ast.JoinSideRight) {
		columns = make([]ast.ScopedColumn, len(reference.Columns))
		copy(columns, reference.Columns)
		for index := range columns {
			columns[index].Nullable = true
		}
	}
	s.registerTable(reference.Alias, &ScopedTable{
		Alias:          reference.Alias,
		Columns:        columns,
		CatalogueTable: nil,
		Function:       reference.Source == DerivedSourceTableFunction,
		JoinKind:       reference.JoinKind,
		Schema:         "",
		Name:           "",
		IsWithoutRowID: false,
		nullExtended:   false,
	})
	s.joinUsing(reference.Alias, reference.Using)
}

// OrderedTables returns the relations of this scope in FROM order: the order the dialect
// recorded for the FROM clause where it did, and otherwise the order the relations were
// registered in. A relation filed directly in Tables, outside the registration methods,
// follows in alias order.
//
// Returns []*ScopedTable which holds the relations in order, in a slice of the caller's
// own.
func (s *ScopeChain) OrderedTables() []*ScopedTable {
	return slices.Clone(s.orderedTables())
}

// ResolveColumn walks the scope chain to find a column by optional table alias and column
// name.
//
// When tableAlias is set, the named table is sought in the current scope and, for a
// subquery scope, in the parent when absent. When tableAlias is empty, every table is
// searched for the column name, requiring exactly one match because multiple matches
// produce Q002 (ambiguity). CTEs in the current scope are checked next, then a subquery
// scope traverses to the parent and repeats. A column not found anywhere produces Q001
// (unknown column).
//
// Takes tableAlias (string) which specifies the qualifying table alias, or empty for
// unqualified lookup.
// Takes columnName (string) which specifies the column name to resolve.
//
// Returns *ast.ScopedColumn which holds the resolved column, or nil on error.
// Returns *ScopedTable which holds the containing table, or nil on error.
// Returns error which indicates a resolution failure (Q001 unknown, Q002 ambiguous).
func (s *ScopeChain) ResolveColumn(
	tableAlias string,
	columnName string,
) (*ast.ScopedColumn, *ScopedTable, error) {
	var column *ast.ScopedColumn
	var table *ScopedTable
	var err error
	if tableAlias != "" {
		column, table, err = s.resolveQualifiedColumn(tableAlias, columnName, 0)
	} else {
		column, table, err = s.resolveUnqualifiedColumn(columnName, 0)
	}
	if err != nil {
		return column, table, err
	}
	return s.leftOutColumn(column, table), table, nil
}

// ResolveRowValue resolves a relation name as its current row where the dialect permits
// it.
//
// Visible scopes are searched from the innermost outward.
//
// Takes name (string) which is the canonical name.
//
// Returns *ast.ScopedColumn which holds the row as a column of the name, typed by the
// dialect, and NULL where the dialect's type says the row may be.
// Returns *ScopedTable which holds the relation.
// Returns bool which is false when the dialect has no row values or no relation is so
// named.
func (s *ScopeChain) ResolveRowValue(name string) (*ast.ScopedColumn, *ScopedTable, bool) {
	if s.rules.RowValues == nil || name == "" {
		return nil, nil, false
	}
	for scope, depth := s, 0; scope != nil && depth < maxScopeChainDepth; scope, depth = scope.visibleParent(), depth+1 {
		if table, exists := scope.Table(name); exists {
			return s.rowValueColumn(name, table), table, true
		}
	}
	return nil, nil, false
}

// Depth returns the number of scopes enclosing this one, which is the nesting depth of
// the query the scope belongs to: zero for a root scope, and one more than its parent's
// for any other.
//
// Returns int which is the depth; zero for a nil scope.
func (s *ScopeChain) Depth() int {
	if s == nil {
		return 0
	}
	return s.depth
}

// CreateChildScope creates a new scope linked to this one as parent.
//
// Takes kind (ScopeKind) which specifies the child scope kind.
//
// Returns *ScopeChain which holds the new child scope with this scope as parent.
func (s *ScopeChain) CreateChildScope(kind ScopeKind) *ScopeChain {
	return NewScopeChain(kind, s)
}

// nestedTooDeeply reports whether the query of this scope sits MaxExpressionResolveDepth
// or more levels deep, so the queries nested in it are not analysed.
//
// Returns bool which is true at or past the limit; false for a nil scope.
func (s *ScopeChain) nestedTooDeeply() bool {
	return s.Depth() >= MaxExpressionResolveDepth
}

// orderedTables returns the relations of this scope in the order OrderedTables gives
// them, sorting them only when a relation was registered, or filed in Tables directly, or
// the FROM order set, since they were last sorted.
//
// Returns []*ScopedTable which holds the relations in order; callers must not change it.
func (s *ScopeChain) orderedTables() []*ScopedTable {
	if s.ordered != nil && len(s.ordered) == len(s.Tables) {
		return s.ordered
	}
	keys := make([]string, 0, len(s.Tables))
	listed := make(map[string]struct{}, len(s.Tables))
	for _, key := range s.order {
		if _, exists := s.Tables[key]; exists {
			if _, seen := listed[key]; !seen {
				listed[key] = struct{}{}
				keys = append(keys, key)
			}
		}
	}
	for _, key := range slices.Sorted(maps.Keys(s.Tables)) {
		if _, seen := listed[key]; !seen {
			keys = append(keys, key)
		}
	}
	slices.SortStableFunc(keys, func(left, right string) int {
		return cmp.Compare(s.fromPosition(left), s.fromPosition(right))
	})
	tables := make([]*ScopedTable, len(keys))
	for index, key := range keys {
		tables[index] = s.Tables[key]
	}
	s.ordered = tables
	return tables
}

// visibleCTE resolves a relation name to a CTE in this scope or the nearest enclosing
// scope. CTE names remain visible throughout the query headed by their WITH clause.
//
// Takes name (string) which is the canonical relation name.
//
// Returns *ResolvedCTE which is the CTE.
// Returns bool which is true when a scope of the chain holds a CTE under the name.
func (s *ScopeChain) visibleCTE(name string) (*ResolvedCTE, bool) {
	for scope, depth := s, 0; scope != nil && depth < maxScopeChainDepth; scope, depth = scope.parent, depth+1 {
		if cte, exists := scope.CTE(name); exists {
			return cte, true
		}
	}
	return nil, false
}

// rowValueColumn builds the column the current row of a relation stands for.
//
// Takes name (string) which is the name the query wrote.
// Takes table (*ScopedTable) which is the relation.
//
// Returns *ast.ScopedColumn which holds the row, typed by the dialect.
func (s *ScopeChain) rowValueColumn(name string, table *ScopedTable) *ast.ScopedColumn {
	relation := dialect.RowRelation{
		Name:        "",
		Schema:      "",
		Columns:     slices.Clone(table.Columns),
		Function:    table.Function,
		OuterJoined: table.JoinKind.Nullable.Includes(ast.JoinSideRight) || table.nullExtended,
	}
	if table.CatalogueTable != nil {
		relation.Name, relation.Schema = table.CatalogueTable.Name, table.CatalogueTable.Schema
	}
	row := s.rules.RowValues.RowValueType(relation)
	return &ast.ScopedColumn{Name: name, SQLType: row, Nullable: row.Nullable}
}

// openSelectList starts resolving the scope's select list: where the dialect has lateral
// column aliases, its items become visible to the later ones as they are shared. A nil
// scope opens nothing.
//
// Returns *selectListAliases which shares the items and remembers the select list
// resolving before, or nil where the dialect has no lateral column aliases.
func (s *ScopeChain) openSelectList() *selectListAliases {
	if s == nil || !s.rules.LateralAliases {
		return nil
	}
	s.selectList = &selectListAliases{items: nil, named: nil, outer: s.selectList}
	return s.selectList
}

// closeSelectList ends resolving the scope's select list, so its aliases are visible to
// nothing after it, and restores the select list resolving before.
//
// Takes aliases (*selectListAliases) which is what openSelectList returned.
//
// Returns []int which lists the items a later item named by alias.
func (s *ScopeChain) closeSelectList(aliases *selectListAliases) []int {
	if aliases == nil {
		return nil
	}
	s.selectList = aliases.outer
	return aliases.named
}

// addAliasOperand keeps an aliased item of the scope's select list for the clauses in
// which the dialect lets an expression name it, or in which the alias shadows a column of
// its name. A scope whose dialect lists no such clause keeps nothing.
//
// Takes index (int) which is the item's index in the select list.
// Takes alias (string) which is the item's alias.
// Takes column (*sqlmodel.OutputColumn) which is the item's resolved column.
func (s *ScopeChain) addAliasOperand(index int, alias string, column *sqlmodel.OutputColumn) {
	if s == nil || s.rules.AliasOperands|s.rules.ShadowingAliases == 0 || alias == "" {
		return
	}
	s.aliasOperands = append(s.aliasOperands, selectListItem{
		column: ast.ScopedColumn{Name: alias, SQLType: column.SQLType, Nullable: column.Nullable},
		index:  index,
	})
}

// exposeAliasOperands makes the aliased items of the scope's select list visible to the
// expressions of a clause the dialect lets name them, after every column the clause can
// see, or before them where the dialect's aliases shadow columns, until the returned
// function is called.
//
// Takes clause (ast.Clause) which is the clause the expressions sit in.
//
// Returns func() which restores what the scope saw before.
func (s *ScopeChain) exposeAliasOperands(clause ast.Clause) func() {
	if s == nil || len(s.aliasOperands) == 0 || !(s.rules.AliasOperands | s.rules.ShadowingAliases).Includes(clause) {
		return func() {}
	}
	enclosing := s.selectList
	s.selectList = &selectListAliases{items: s.aliasOperands, named: nil, outer: nil}
	return func() { s.selectList = enclosing }
}

// selectItem returns the earlier item of the select list being resolved that a name is
// the alias of, the latest when several share it, and records that a later item named it.
//
// Takes name (string) which is the name.
//
// Returns *ast.ScopedColumn which holds a copy of the item's column, or nil when no item
// has that alias.
func (s *ScopeChain) selectItem(name string) *ast.ScopedColumn {
	if s.selectList == nil {
		return nil
	}
	for index := range slices.Backward(s.selectList.items) {
		item := &s.selectList.items[index]
		if !s.rules.Identifiers.Equal(item.column.Name, name) {
			continue
		}
		if !slices.Contains(s.selectList.named, item.index) {
			s.selectList.named = append(s.selectList.named, item.index)
			slices.Sort(s.selectList.named)
		}
		return new(item.column)
	}
	return nil
}

// takeDeclaredColumns returns the columns declared for a runtime-derived source and
// forgets them, so a second such source takes none.
//
// Returns []ast.ScopedColumn which holds the declared columns, or nil when there are
// none.
func (s *ScopeChain) takeDeclaredColumns() []ast.ScopedColumn {
	columns := s.declaredColumns
	s.declaredColumns = nil
	return columns
}

// registerTable registers a relation under its alias, preserves FROM order and applies
// outer-join nullability to preceding relations. Replacing an alias keeps its position.
//
// Takes alias (string) which is the canonical alias the relation is visible under.
// Takes table (*ScopedTable) which is the relation.
func (s *ScopeChain) registerTable(alias string, table *ScopedTable) {
	key := s.Key(alias)
	if _, exists := s.Tables[key]; !exists {
		s.registered[key] = len(s.order)
		s.order = append(s.order, key)
	}
	s.Tables[key] = table
	delete(s.hidden, key)
	s.joined = nil
	s.ordered = nil
	s.nullExtendAround(key, table)
}

// fromPosition returns where the FROM clause lists a relation, for ordering: its place in
// the recorded FROM order, or a place after every listed relation when it is not listed.
//
// Takes key (string) which is the relation's comparison key.
//
// Returns int which is the position.
func (s *ScopeChain) fromPosition(key string) int {
	if position, listed := s.fromPositions[key]; listed {
		return position
	}
	return len(s.fromOrder)
}

// registrationRank returns where a relation comes among relations at the same FROM
// position: its place in the registration order, or a place after every registered
// relation for one filed in Tables directly.
//
// Takes key (string) which is the relation's comparison key.
//
// Returns int which is the rank.
func (s *ScopeChain) registrationRank(key string) int {
	if rank, registered := s.registered[key]; registered {
		return rank
	}
	return len(s.order)
}

// resolveQualifiedColumn resolves a column reference that includes a table alias
// qualifier.
//
// It searches the current scope's tables, then, for a subquery scope, its parent's in
// turn. A CTE names a relation only where a FROM clause reads it, which makes it one of
// the scope's tables; a qualifier naming a CTE no FROM clause reads is unknown, as the
// engines report a missing FROM-clause entry. The depth counter caps the parent-chain
// traversal at maxScopeChainDepth to defend against runaway recursion on a malformed
// scope chain.
//
// Takes tableAlias (string) which specifies the qualifying table alias.
// Takes columnName (string) which specifies the column name to find.
// Takes depth (int) which holds the current parent-traversal depth. The root call passes
// zero and each recursive parent call increments by one.
//
// Returns *ast.ScopedColumn which holds the resolved column, or nil on error.
// Returns *ScopedTable which holds the containing table, or nil on error.
// Returns error which indicates Q001 if the table alias or column is unknown, or
// ErrScopeChainDepthExceeded when the parent chain ran past maxScopeChainDepth.
func (s *ScopeChain) resolveQualifiedColumn(
	tableAlias string,
	columnName string,
	depth int,
) (*ast.ScopedColumn, *ScopedTable, error) {
	if depth >= maxScopeChainDepth {
		return nil, nil, ErrScopeChainDepthExceeded
	}
	if table, exists := s.Table(tableAlias); exists {
		return s.resolveColumnInTable(table, columnName, tableAlias)
	}

	if parent := s.visibleParent(); parent != nil {
		return parent.resolveQualifiedColumn(tableAlias, columnName, depth+1)
	}

	return nil, nil, newError(diag.CodeUnknownColumn, "unknown table or alias %q", tableAlias).
		withSuggestion(s.suggest(tableAlias, s.relationNames))
}

// resolveColumnInTable searches for a column by name within a single scoped table. Falls
// back to synthesising an implicit rowid column for eligible tables.
//
// Takes table (*ScopedTable) which specifies the table to search.
// Takes columnName (string) which specifies the column name to find.
// Takes tableAlias (string) which specifies the alias used in error messages.
//
// Returns *ast.ScopedColumn which holds the matched column, or nil on error.
// Returns *ScopedTable which holds the containing table, or nil on error.
// Returns error which indicates Q001 if the column is not found.
func (s *ScopeChain) resolveColumnInTable(
	table *ScopedTable,
	columnName string,
	tableAlias string,
) (*ast.ScopedColumn, *ScopedTable, error) {
	for index := range table.Columns {
		if s.rules.Identifiers.Equal(table.Columns[index].Name, columnName) {
			return &table.Columns[index], table, nil
		}
	}
	if column := s.hiddenColumn(table, columnName); column != nil {
		return column, table, nil
	}
	if s.rules.isRowID(columnName) && !table.IsWithoutRowID {
		return &ast.ScopedColumn{
			Name:     columnName,
			SQLType:  s.rules.RowIDType,
			Nullable: false,
		}, table, nil
	}
	if s.rules.isSystemColumn(columnName) {
		return systemColumn(columnName), table, nil
	}
	return nil, nil, newError(diag.CodeUnknownColumn, "unknown column %q in table %q", columnName, tableAlias).
		withSuggestion(s.suggest(columnName, func() []string { return scopedColumnNames(table.Columns) }))
}

// resolveUnqualifiedColumn resolves a column reference without a table qualifier.
//
// It searches tables, implicit rowid, CTEs, and, for a subquery scope, its parent; a
// select-list alias visible where the reference is comes after the tables, or before them
// where the dialect's aliases shadow columns. The depth counter caps the parent-chain
// traversal at maxScopeChainDepth to defend against runaway recursion on a malformed
// scope chain.
//
// Takes columnName (string) which specifies the column name to resolve.
// Takes depth (int) which holds the current parent-traversal depth. The root call passes
// zero and each recursive parent call increments by one.
//
// Returns *ast.ScopedColumn which holds the resolved column, or nil on error.
// Returns *ScopedTable which holds the containing table, or nil on error.
// Returns error which indicates Q001 (unknown), Q002 (ambiguous), or
// ErrScopeChainDepthExceeded when the parent chain ran past maxScopeChainDepth.
func (s *ScopeChain) resolveUnqualifiedColumn(
	columnName string,
	depth int,
) (*ast.ScopedColumn, *ScopedTable, error) {
	if depth >= maxScopeChainDepth {
		return nil, nil, ErrScopeChainDepthExceeded
	}
	if column, table, found, err := s.relationOrAliasColumn(columnName); found || err != nil {
		return column, table, err
	}

	if s.rules.isRowID(columnName) {
		column, table, err := s.resolveImplicitRowID(columnName)
		if column != nil || err != nil {
			return column, table, err
		}
	}

	if s.rules.isSystemColumn(columnName) && len(s.Tables) > 0 {
		return s.resolveSystemColumn(columnName)
	}

	if column := s.selectItem(columnName); column != nil {
		return column, nil, nil
	}

	if column, table := s.resolveFromCTEs(columnName); column != nil {
		return column, table, nil
	}

	if parent := s.visibleParent(); parent != nil {
		return parent.resolveUnqualifiedColumn(columnName, depth+1)
	}

	return nil, nil, newError(diag.CodeUnknownColumn, "unknown column %q", columnName).
		withSuggestion(s.suggest(columnName, s.visibleColumnNames))
}

// relationOrAliasColumn finds a column of the current scope's relations by name, or the
// select-list item of that alias first where the dialect's aliases shadow columns.
//
// Takes columnName (string) which specifies the column name to resolve.
//
// Returns *ast.ScopedColumn which holds the column found, or nil.
// Returns *ScopedTable which holds the relation the column belongs to, or nil for an
// alias.
// Returns bool which is true when a column or an alias was found.
// Returns error which is Q002 when several relations hold a column of the name.
func (s *ScopeChain) relationOrAliasColumn(columnName string) (*ast.ScopedColumn, *ScopedTable, bool, error) {
	if s.rules.ShadowingAliases != 0 {
		if column := s.selectItem(columnName); column != nil {
			return column, nil, true, nil
		}
	}
	column, table, matchCount := s.findColumnInTables(columnName)
	if matchCount > 1 {
		return nil, nil, true, newError(diag.CodeAmbiguousColumn, "ambiguous column reference %q", columnName)
	}
	return column, table, matchCount == 1, nil
}

// findColumnInTables searches the row the current scope's relations produce for a column
// by name.
//
// A column a USING clause or a NATURAL join merged counts once. Returns the match count
// to detect ambiguous references.
//
// Takes columnName (string) which specifies the column name to search for.
//
// Returns *ast.ScopedColumn which holds the last matched column, or nil if none found.
// Returns *ScopedTable which holds the last matched table, or nil if none found.
// Returns int which holds the number of columns of the row with the name.
func (s *ScopeChain) findColumnInTables(
	columnName string,
) (*ast.ScopedColumn, *ScopedTable, int) {
	var foundColumn *ast.ScopedColumn
	var foundTable *ScopedTable
	matchCount := 0

	row := s.joinedRow()
	for _, position := range row.positions[s.Key(columnName)] {
		matchCount++
		foundTable = row.columns[position].Table
		foundColumn = s.tableColumn(&row.columns[position])
	}
	if matchCount > 0 {
		return foundColumn, foundTable, matchCount
	}

	for _, table := range s.orderedTables() {
		if column := s.hiddenColumn(table, columnName); column != nil {
			matchCount++
			foundTable = table
			foundColumn = column
		}
	}
	return foundColumn, foundTable, matchCount
}

// hiddenColumn returns the hidden column of a relation with a name, or nil.
//
// Takes table (*ScopedTable) which is the relation.
// Takes columnName (string) which is the canonical column name.
//
// Returns *ast.ScopedColumn which is the hidden column, or nil when the relation has no
// hidden column of that name.
func (s *ScopeChain) hiddenColumn(table *ScopedTable, columnName string) *ast.ScopedColumn {
	hidden := s.hidden[s.Key(table.Alias)]
	for index := range hidden {
		if s.rules.Identifiers.Equal(hidden[index].Name, columnName) {
			return &hidden[index]
		}
	}
	return nil
}

// tableColumn returns the column a joined-row entry stands for: the relation's own column
// when the entry is one, so a caller sees the relation's column, or the entry's merged
// column otherwise.
//
// Takes entry (*StarColumn) which is the joined-row entry.
//
// Returns *ast.ScopedColumn which is the column.
func (s *ScopeChain) tableColumn(entry *StarColumn) *ast.ScopedColumn {
	if !entry.merged && entry.Table != nil {
		if entry.index < len(entry.Table.Columns) && s.rules.Identifiers.Equal(entry.Table.Columns[entry.index].Name, entry.Column.Name) {
			return &entry.Table.Columns[entry.index]
		}
		for index := range entry.Table.Columns {
			if s.rules.Identifiers.Equal(entry.Table.Columns[index].Name, entry.Column.Name) {
				return &entry.Table.Columns[index]
			}
		}
	}
	return &entry.Column
}

// resolveImplicitRowID resolves an implicit rowid column across all tables in the current
// scope. Exactly one eligible table must exist; multiple eligible tables produce Q002
// (ambiguity).
//
// Takes columnName (string) which specifies the rowid alias (ROWID, _ROWID_, or OID).
//
// Returns *ast.ScopedColumn which holds the synthesised rowid column, or nil if none
// found.
// Returns *ScopedTable which holds the containing table, or nil if none found.
// Returns error which indicates Q002 if multiple tables support implicit rowid.
func (s *ScopeChain) resolveImplicitRowID(
	columnName string,
) (*ast.ScopedColumn, *ScopedTable, error) {
	rowidMatchCount := 0
	var rowidTable *ScopedTable
	for _, table := range s.Tables {
		if !table.IsWithoutRowID {
			rowidMatchCount++
			rowidTable = table
		}
	}
	if rowidMatchCount == 1 {
		return &ast.ScopedColumn{
			Name:     columnName,
			SQLType:  s.rules.RowIDType,
			Nullable: false,
		}, rowidTable, nil
	}
	if rowidMatchCount > 1 {
		return nil, nil, newError(diag.CodeAmbiguousColumn, "ambiguous column reference %q", columnName)
	}
	return nil, nil, nil
}

// resolveSystemColumn resolves a column every table has without declaring it, named
// without a qualifier, in a scope holding at least one relation: the column of the only
// relation, or ambiguous when the scope holds several.
//
// Takes columnName (string) which is the system column's name.
//
// Returns *ast.ScopedColumn which holds the column, of unknown type, or nil when
// ambiguous.
// Returns *ScopedTable which holds the relation it belongs to, or nil when ambiguous.
// Returns error which indicates Q002 when several relations have the column.
func (s *ScopeChain) resolveSystemColumn(columnName string) (*ast.ScopedColumn, *ScopedTable, error) {
	tables := s.orderedTables()
	if len(tables) > 1 {
		return nil, nil, newError(diag.CodeAmbiguousColumn, "ambiguous column reference %q", columnName)
	}
	return systemColumn(columnName), tables[0], nil
}

// resolveFromCTEs searches the scope's CTEs for an unqualified column.
//
// Takes columnName (string) which specifies the column name to find.
//
// Returns *ast.ScopedColumn which holds the matched column, or nil if not found.
// Returns *ScopedTable which holds the CTE as a relation, or nil if not found.
func (s *ScopeChain) resolveFromCTEs(
	columnName string,
) (*ast.ScopedColumn, *ScopedTable) {
	if len(s.cteOrder) != len(s.CTEs) {
		s.cteOrder = slices.Sorted(maps.Keys(s.CTEs))
	}
	for _, name := range s.cteOrder {
		cte := s.CTEs[name]
		for index := range cte.columns {
			if s.rules.Identifiers.Equal(cte.columns[index].Name, columnName) {
				cteTable := &ScopedTable{
					Name:           cte.name,
					Alias:          cte.name,
					Columns:        cte.columns,
					CatalogueTable: nil,
					Function:       false,
					Schema:         "",
					JoinKind:       ast.JoinInner,
					IsWithoutRowID: false,
					nullExtended:   false,
				}
				return &cte.columns[index], cteTable
			}
		}
	}
	return nil, nil
}

// relationNames lists the names of the relations and CTEs visible from this scope, the
// candidates for a "did you mean" suggestion on an unknown qualifier.
//
// Returns []string which holds the names.
func (s *ScopeChain) relationNames() []string {
	var names []string
	for scope, depth := s, 0; scope != nil && depth < maxScopeChainDepth; scope, depth = scope.visibleParent(), depth+1 {
		for _, table := range scope.OrderedTables() {
			names = append(names, table.Alias)
		}
		for _, key := range slices.Sorted(maps.Keys(scope.CTEs)) {
			names = append(names, scope.CTEs[key].name)
		}
	}
	return names
}

// visibleColumnNames lists the names of the columns an unqualified reference can reach
// from this scope, the candidates for a "did you mean" suggestion on an unknown column.
//
// Returns []string which holds the names.
func (s *ScopeChain) visibleColumnNames() []string {
	var names []string
	for scope, depth := s, 0; scope != nil && depth < maxScopeChainDepth; scope, depth = scope.visibleParent(), depth+1 {
		row := scope.joinedRow().columns
		for index := range row {
			names = append(names, row[index].Column.Name)
		}
		for _, key := range slices.Sorted(maps.Keys(scope.CTEs)) {
			names = append(names, scopedColumnNames(scope.CTEs[key].columns)...)
		}
	}
	return names
}

// visibleParent returns the enclosing scope whose names this scope can see: the parent of
// a subquery scope, and nil otherwise.
//
// Returns *ScopeChain which is the parent, or nil.
func (s *ScopeChain) visibleParent() *ScopeChain {
	if s.kind == ScopeKindSubquery {
		return s.parent
	}
	return nil
}

// suggest works out the name the query most likely meant by an unknown name, while the
// chain's suggestion budget lasts.
//
// Takes name (string) which is the unknown name.
// Takes candidates (func() []string) which lists the names the scope can see; it is
// called only while the budget lasts.
//
// Returns string which is the suggestion, or empty when there is none or the budget is
// spent.
func (s *ScopeChain) suggest(name string, candidates func() []string) string {
	if !s.suggestions.take() {
		return ""
	}
	return diagbuild.ClosestSuggestion(name, candidates())
}

// ResolvedCTE holds a CTE with its output columns fully resolved.
type ResolvedCTE struct {
	// name holds the original CTE name as declared in the WITH clause.
	name string

	// columns holds the resolved output columns of the CTE.
	columns []ast.ScopedColumn
}

// systemColumn returns a column every table has without declaring it, of the type the
// dialect does not say.
//
// Takes columnName (string) which is the column's name as the query writes it.
//
// Returns *ast.ScopedColumn which is the column, never NULL.
func systemColumn(columnName string) *ast.ScopedColumn {
	return &ast.ScopedColumn{Name: columnName, SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), Nullable: false}
}

// arrayWrappedSQLType returns the type of a catalogue column's values, with its array
// dimensions applied, as the generated model of its table maps it.
//
// Takes column (*sqlmodel.Column) which is the catalogue column being scoped.
//
// Returns sqlmodel.SQLType which is the column's value type.
func arrayWrappedSQLType(column *sqlmodel.Column) sqlmodel.SQLType {
	return column.ValueType()
}

// scopedColumnNames lists the names of scoped columns.
//
// Takes columns ([]ast.ScopedColumn) which holds the columns.
//
// Returns []string which holds their names in order.
func scopedColumnNames(columns []ast.ScopedColumn) []string {
	names := make([]string, len(columns))
	for index := range columns {
		names[index] = columns[index].Name
	}
	return names
}
