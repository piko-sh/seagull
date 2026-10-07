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
	"slices"

	"seagull.sh/seagull/internal/dialect/ast"
)

// StarColumn is one column of the joined row an unqualified star expands to.
type StarColumn struct {
	// Table is the relation the column's value comes from. It is nil for a column a FULL
	// join merges, whose value comes from whichever side has a row.
	Table *ScopedTable

	// Column is the column with its type and its nullability after every join.
	Column ast.ScopedColumn

	// index is the place in Table's columns of the first column with the column's name.
	index int

	// merged reports a column a USING clause or a NATURAL join made of two.
	merged bool
}

// relationPlace is where a relation comes in a scope's FROM order.
type relationPlace struct {
	// position is the relation's place in the recorded FROM order, or one past it for a
	// relation the order does not list.
	position int

	// rank orders relations at the same position by when they were registered.
	rank int
}

// before reports whether a place comes before another in FROM order.
//
// Takes other (relationPlace) which is the other place.
//
// Returns bool which is true when this place comes first.
func (place relationPlace) before(other relationPlace) bool {
	return place.position < other.position || (place.position == other.position && place.rank < other.rank)
}

// joinedRow is the row the FROM clause of a scope produces: its columns in the order an
// unqualified star expands them.
type joinedRow struct {
	// positions maps the comparison key of each column name to the places in columns of the
	// columns with that name, in order.
	positions map[string][]int

	// columns holds the row's columns in order.
	columns []StarColumn

	// tableCount is the number of relations the row was built from, so a relation filed
	// directly in Tables, outside the registration methods, still rebuilds it.
	tableCount int
}

// SetFromOrder records relation aliases in FROM order for star expansion and outer-join
// nullability. This order takes precedence over registration order.
//
// Takes names ([]string) which holds the canonical aliases, or names, of the relations.
func (s *ScopeChain) SetFromOrder(names []string) {
	s.fromOrder = make([]string, 0, len(names))
	s.fromPositions = make(map[string]int, len(names))
	for _, name := range names {
		key := s.Key(name)
		if _, listed := s.fromPositions[key]; !listed {
			s.fromPositions[key] = len(s.fromOrder)
		}
		s.fromOrder = append(s.fromOrder, key)
	}
	s.joined = nil
	s.ordered = nil
	s.nullExtendPrecedingRelations()
}

// StarColumns returns the columns an unqualified star expands to: every column of every
// relation in FROM order, with each column a USING clause or a NATURAL join merges
// appearing once, where the dialect places it.
//
// Returns []StarColumn which holds the columns in order.
func (s *ScopeChain) StarColumns() []StarColumn {
	return slices.Clone(s.joinedRow().columns)
}

// StarReferable reports which star columns can be referenced by name without changing
// their values. Catalogue table and view columns and columns merged by FULL joins
// qualify; computed relations may use names the analysis cannot predict.
//
// Takes tableAlias (string) which is the star's qualifier, or empty for an unqualified
// star.
//
// Returns []bool which holds, parallel to the columns the star expands to, whether each
// can be named, or nil when the qualifier names no relation.
func (s *ScopeChain) StarReferable(tableAlias string) []bool {
	if tableAlias != "" {
		table, exists := s.Table(tableAlias)
		if !exists {
			return nil
		}
		referable := make([]bool, len(table.Columns))
		for index := range referable {
			referable[index] = catalogueRelation(table)
		}
		return referable
	}
	columns := s.joinedRow().columns
	referable := make([]bool, len(columns))
	for index := range columns {
		if columns[index].Table == nil {
			referable[index] = columns[index].merged
			continue
		}
		referable[index] = catalogueRelation(columns[index].Table)
	}
	return referable
}

// nullExtendPrecedingRelations makes preceding relations nullable for RIGHT or FULL
// joins, copying columns to preserve shared definitions. Relations already extended are
// left unchanged.
func (s *ScopeChain) nullExtendPrecedingRelations() {
	s.nulling = relationPlace{position: -1, rank: -1}
	tables := s.orderedTables()
	for index, joined := range tables {
		if !joined.JoinKind.Nullable.Includes(ast.JoinSideLeft) {
			continue
		}
		s.nulling = s.placeOf(s.Key(joined.Alias))
		for _, preceding := range tables[:index] {
			s.nullExtend(preceding)
		}
	}
}

// nullExtendAround applies outer-join nullability for a relation just registered: a
// relation whose join leaves the relations before it without a row makes each of them
// nullable, and a relation that comes before such a relation already registered is made
// nullable itself. Only the relations the registration affects are visited.
//
// Takes key (string) which is the relation's comparison key.
// Takes table (*ScopedTable) which is the relation.
func (s *ScopeChain) nullExtendAround(key string, table *ScopedTable) {
	place := s.placeOf(key)
	if place.before(s.nulling) {
		s.nullExtend(table)
	}
	if !table.JoinKind.Nullable.Includes(ast.JoinSideLeft) {
		return
	}
	if s.nulling.before(place) {
		s.nulling = place
	}
	for otherKey, other := range s.Tables {
		if s.placeOf(otherKey).before(place) {
			s.nullExtend(other)
		}
	}
}

// placeOf returns where a relation comes in FROM order: its FROM position, then its
// registration rank among relations at the same position.
//
// Takes key (string) which is the relation's comparison key.
//
// Returns relationPlace which is the relation's place.
func (s *ScopeChain) placeOf(key string) relationPlace {
	return relationPlace{position: s.fromPosition(key), rank: s.registrationRank(key)}
}

// nullExtend makes every column of a relation nullable, its hidden columns included.
//
// Takes table (*ScopedTable) which is the relation.
func (s *ScopeChain) nullExtend(table *ScopedTable) {
	if table.nullExtended {
		return
	}
	table.nullExtended = true
	table.Columns = nullableCopy(table.Columns)
	key := s.Key(table.Alias)
	if hidden, exists := s.hidden[key]; exists {
		s.hidden[key] = nullableCopy(hidden)
	}
	s.joined = nil
}

// joinUsing records the columns a relation's join merges with the relations before it.
//
// Takes alias (string) which is the canonical alias of the joined relation.
// Takes using (*ast.JoinUsing) which describes the merge, or nil for none.
func (s *ScopeChain) joinUsing(alias string, using *ast.JoinUsing) {
	key := s.Key(alias)
	if using == nil {
		delete(s.using, key)
	} else {
		s.using[key] = using
	}
	s.joined = nil
}

// joinedRow returns the scope's joined row, building it on first use after the scope's
// relations last changed.
//
// Returns *joinedRow which holds the row.
func (s *ScopeChain) joinedRow() *joinedRow {
	if s.joined != nil && s.joined.tableCount == len(s.Tables) {
		return s.joined
	}
	row := &joinedRow{positions: nil, columns: nil, tableCount: len(s.Tables)}
	for _, table := range s.orderedTables() {
		right := make([]StarColumn, len(table.Columns))
		first := make(map[string]int, len(table.Columns))
		for index := range table.Columns {
			key := s.Key(table.Columns[index].Name)
			if _, named := first[key]; !named {
				first[key] = index
			}
			right[index] = StarColumn{Table: table, Column: table.Columns[index], index: first[key], merged: false}
		}
		using := s.using[s.Key(table.Alias)]
		if using == nil {
			row.columns = append(row.columns, right...)
			continue
		}
		row.columns = s.mergeJoin(row.columns, table.JoinKind, using, right)
	}
	row.positions = make(map[string][]int, len(row.columns))
	for index := range row.columns {
		key := s.Key(row.columns[index].Column.Name)
		row.positions[key] = append(row.positions[key], index)
	}
	s.joined = row
	return row
}

// mergeJoin joins a relation with a USING clause or a NATURAL join onto the row built so
// far, merging each named column the two sides share into one.
//
// Takes left ([]StarColumn) which holds the row built so far.
// Takes joinKind (ast.JoinKind) which is the join.
// Takes using (*ast.JoinUsing) which describes the merge.
// Takes right ([]StarColumn) which holds the joined relation's columns.
//
// Returns []StarColumn which holds the joined row's columns.
func (s *ScopeChain) mergeJoin(left []StarColumn, joinKind ast.JoinKind, using *ast.JoinUsing, right []StarColumn) []StarColumn {
	names := using.Columns
	if using.Natural {
		names = s.commonColumnNames(left, right)
	}

	var leading []StarColumn
	replaced := make(map[int]StarColumn, len(names))
	droppedRight := make(map[int]bool, len(names))
	for _, name := range names {
		leftIndex := s.lastColumnNamed(left, name)
		rightIndex := s.lastColumnNamed(right, name)
		if leftIndex < 0 || rightIndex < 0 {
			continue
		}
		column := mergedColumn(left[leftIndex], right[rightIndex], joinKind)
		leading = append(leading, column)
		replaced[leftIndex] = column
		droppedRight[rightIndex] = true
	}

	joined := make([]StarColumn, 0, len(left)+len(right))
	if using.Placement == ast.JoinColumnsLead {
		joined = append(joined, leading...)
	}
	for index := range left {
		merged, isMerged := replaced[index]
		switch {
		case !isMerged:
			joined = append(joined, left[index])
		case using.Placement != ast.JoinColumnsLead:
			joined = append(joined, merged)
		}
	}
	for index := range right {
		if !droppedRight[index] {
			joined = append(joined, right[index])
		}
	}
	return joined
}

// commonColumnNames lists the column names both sides of a NATURAL join have, in the
// order the left side has them.
//
// Takes left ([]StarColumn) which holds the left side's columns.
// Takes right ([]StarColumn) which holds the joined relation's columns.
//
// Returns []string which holds the shared names.
func (s *ScopeChain) commonColumnNames(left, right []StarColumn) []string {
	var names []string
	for index := range left {
		name := left[index].Column.Name
		if s.lastColumnNamed(right, name) >= 0 && !slices.ContainsFunc(names, func(listed string) bool {
			return s.rules.Identifiers.Equal(listed, name)
		}) {
			names = append(names, name)
		}
	}
	return names
}

// lastColumnNamed returns the position of the last column with a name, so a merge picks
// the relation joined nearest to it.
//
// Takes columns ([]StarColumn) which holds the columns to search.
// Takes name (string) which is the canonical column name.
//
// Returns int which is the position, or -1 when no column has the name.
func (s *ScopeChain) lastColumnNamed(columns []StarColumn, name string) int {
	for index := range slices.Backward(columns) {
		if s.rules.Identifiers.Equal(columns[index].Column.Name, name) {
			return index
		}
	}
	return -1
}

// nullableCopy returns a copy of columns, each nullable.
//
// Takes columns ([]ast.ScopedColumn) which are the columns; they are not changed.
//
// Returns []ast.ScopedColumn which holds the nullable copies.
func nullableCopy(columns []ast.ScopedColumn) []ast.ScopedColumn {
	copied := slices.Clone(columns)
	for index := range copied {
		copied[index].Nullable = true
	}
	return copied
}

// catalogueRelation reports whether a relation in a scope is a table or view the
// catalogue holds, rather than one the query computes.
//
// Takes table (*ScopedTable) which is the relation.
//
// Returns bool which is true for a catalogue table or view.
func catalogueRelation(table *ScopedTable) bool {
	return table.CatalogueTable != nil && !table.Function
}

// mergedColumn resolves the shared column of a USING or NATURAL join.
//
// It selects the value from the side the join preserves and derives nullability from both
// sides.
//
// Takes left (StarColumn) which is the left side's column.
// Takes right (StarColumn) which is the joined relation's column.
// Takes joinKind (ast.JoinKind) which is the join.
//
// Returns StarColumn which is the merged column.
func mergedColumn(left, right StarColumn, joinKind ast.JoinKind) StarColumn {
	column := left.Column
	column.Nullable = left.Column.Nullable && right.Column.Nullable
	switch {
	case joinKind.Nullable.Includes(ast.JoinSideLeft | ast.JoinSideRight):
		return StarColumn{Table: nil, Column: column, index: 0, merged: true}
	case joinKind.Nullable.Includes(ast.JoinSideLeft):
		return StarColumn{Table: right.Table, Column: column, index: 0, merged: true}
	default:
		return StarColumn{Table: left.Table, Column: column, index: 0, merged: true}
	}
}
