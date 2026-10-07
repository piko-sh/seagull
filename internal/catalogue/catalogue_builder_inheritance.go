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

package catalogue

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// maxInheritanceDepth bounds how many levels below a table a change to its columns, its
	// primary key or its existence is carried, through the tables that inherit from it and
	// its partitions.
	maxInheritanceDepth = 256
)

// inheritance records that a table inherits the columns of another, as INHERITS makes it,
// so a later change to the parent's columns reaches the child.
type inheritance struct {
	// parent is the table whose columns are inherited.
	parent *sqlmodel.Table

	// child is the table that inherits them.
	child *sqlmodel.Table
}

// inheritanceIndex holds, for each table that takes part in an inheritance, the tables
// that inherit from it directly and the tables it inherits from directly, each once.
type inheritanceIndex struct {
	// children maps a table to the tables that inherit from it directly, in the order the
	// links were made.
	children map[*sqlmodel.Table][]*sqlmodel.Table

	// parents maps a table to the tables it inherits from directly.
	parents map[*sqlmodel.Table][]*sqlmodel.Table
}

// newInheritanceIndex indexes inheritance links by parent and by child, leaving out a
// link made twice.
//
// Takes links ([]inheritance) which are the links.
//
// Returns inheritanceIndex which holds them.
func newInheritanceIndex(links []inheritance) inheritanceIndex {
	index := inheritanceIndex{
		children: make(map[*sqlmodel.Table][]*sqlmodel.Table, len(links)),
		parents:  make(map[*sqlmodel.Table][]*sqlmodel.Table, len(links)),
	}
	seen := make(map[inheritance]struct{}, len(links))
	for _, link := range links {
		if _, repeated := seen[link]; repeated {
			continue
		}
		seen[link] = struct{}{}
		index.children[link.parent] = append(index.children[link.parent], link.child)
		index.parents[link.child] = append(index.parents[link.child], link.parent)
	}
	return index
}

// descendants returns every table below a root, each once and after every parent of it
// that also lies below the root, so a change carried down reaches a table after the
// tables it inherits it from.
//
// Tables more than maxInheritanceDepth levels below the root are left out, and a table
// caught in a cycle of links is never reached.
//
// Takes root (*sqlmodel.Table) which is the table the change starts at.
//
// Returns []*sqlmodel.Table which holds the tables below the root in that order.
// Returns bool which is true when the depth limit left tables out.
func (index inheritanceIndex) descendants(root *sqlmodel.Table) ([]*sqlmodel.Table, bool) {
	reached, limited := index.reachable(root)
	return index.parentsFirst(root, reached), limited
}

// parentsFirst orders the tables reached below a root so each comes after every parent of
// it that was reached too.
//
// Takes root (*sqlmodel.Table) which is the table the walk started at.
// Takes reached (map[*sqlmodel.Table]struct{}) which holds the root and the tables
// reached below it.
//
// Returns []*sqlmodel.Table which holds the reached tables, the root left out, in order.
func (index inheritanceIndex) parentsFirst(root *sqlmodel.Table, reached map[*sqlmodel.Table]struct{}) []*sqlmodel.Table {
	waiting := index.parentsReached(reached)
	ordered := make([]*sqlmodel.Table, 0, len(reached))
	for ready := []*sqlmodel.Table{root}; len(ready) > 0; ready = ready[1:] {
		for _, child := range index.children[ready[0]] {
			if _, inside := reached[child]; !inside || child == root {
				continue
			}
			waiting[child]--
			if waiting[child] == 0 {
				ordered = append(ordered, child)
				ready = append(ready, child)
			}
		}
	}
	return ordered
}

// parentsReached counts, for each reached table, how many of its parents were reached
// too, so an ordering knows when every one of them has gone before it.
//
// Takes reached (map[*sqlmodel.Table]struct{}) which holds the reached tables.
//
// Returns map[*sqlmodel.Table]int which holds the count for each table with any.
func (index inheritanceIndex) parentsReached(reached map[*sqlmodel.Table]struct{}) map[*sqlmodel.Table]int {
	waiting := make(map[*sqlmodel.Table]int, len(reached))
	for table := range reached {
		for _, parent := range index.parents[table] {
			if _, inside := reached[parent]; inside {
				waiting[table]++
			}
		}
	}
	return waiting
}

// reachable returns the tables below a root, level by level, down to maxInheritanceDepth
// levels.
//
// Takes root (*sqlmodel.Table) which is the table to start at.
//
// Returns map[*sqlmodel.Table]struct{} which holds the root and the tables reached.
// Returns bool which is true when a table at the deepest level has a child not reached.
func (index inheritanceIndex) reachable(root *sqlmodel.Table) (map[*sqlmodel.Table]struct{}, bool) {
	reached := map[*sqlmodel.Table]struct{}{root: {}}
	frontier := []*sqlmodel.Table{root}
	for range maxInheritanceDepth {
		frontier = index.nextLevel(frontier, reached)
		if len(frontier) == 0 {
			return reached, false
		}
	}
	return reached, len(index.nextLevel(frontier, maps.Clone(reached))) > 0
}

// nextLevel returns the children of a level of tables that have not been reached yet, and
// records them as reached.
//
// Takes level ([]*sqlmodel.Table) which is the level.
// Takes reached (map[*sqlmodel.Table]struct{}) which holds the tables reached so far; it
// gains the children returned.
//
// Returns []*sqlmodel.Table which holds the new children.
func (index inheritanceIndex) nextLevel(level []*sqlmodel.Table, reached map[*sqlmodel.Table]struct{}) []*sqlmodel.Table {
	var next []*sqlmodel.Table
	for _, table := range level {
		for _, child := range index.children[table] {
			if _, seen := reached[child]; !seen {
				reached[child] = struct{}{}
				next = append(next, child)
			}
		}
	}
	return next
}

// ancestorOf reports whether one table lies above another: whether following the tables
// the other inherits from or partitions, at every level, reaches it. Each table is
// followed once, so a cycle of links ends the walk.
//
// Takes ancestor (*sqlmodel.Table) which is the table looked for.
// Takes table (*sqlmodel.Table) which is the table the walk starts at.
//
// Returns bool which is true when the walk reaches the ancestor.
func (index inheritanceIndex) ancestorOf(ancestor, table *sqlmodel.Table) bool {
	visited := map[*sqlmodel.Table]struct{}{table: {}}
	for pending := []*sqlmodel.Table{table}; len(pending) > 0; {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if current == ancestor {
			return true
		}
		for _, parent := range index.parents[current] {
			if _, seen := visited[parent]; !seen {
				visited[parent] = struct{}{}
				pending = append(pending, parent)
			}
		}
	}
	return false
}

// tableColumns gathers the columns of a table a CREATE TABLE builds, merging each column
// whose name is already present into the one there, as the database does.
type tableColumns struct {
	// builder supplies the dialect's identifier rules.
	builder *catalogueBuilder

	// local holds the keys of the columns the table declares itself or copies, rather than
	// inherits.
	local map[string]struct{}

	// columns are the columns gathered so far, in order.
	columns []sqlmodel.Column
}

// add appends a column, or merges it into the column of the same name already gathered:
// the merged column is NOT NULL when either is.
//
// Takes column (*sqlmodel.Column) which is the column; the gathered list holds a copy.
// Takes local (bool) which is true for a column the table declares itself or copies.
func (gathered *tableColumns) add(column *sqlmodel.Column, local bool) {
	if local {
		gathered.local[gathered.builder.key(column.Name)] = struct{}{}
	}
	for index := range gathered.columns {
		existing := &gathered.columns[index]
		if gathered.builder.sameName(existing.Name, column.Name) {
			existing.Nullable = existing.Nullable && column.Nullable
			existing.HasDefault = existing.HasDefault || column.HasDefault
			return
		}
	}
	gathered.columns = append(gathered.columns, *column)
}

// createdTableColumns combines columns inherited from tables, derived from SELECT,
// declared directly or copied by CREATE TABLE, merging duplicate names. Inherited columns
// come first, followed by SELECT output and columns in declaration order.
//
// Takes mutation (*ast.CatalogueMutation) which holds the table definition.
//
// Returns []sqlmodel.Column which holds the table's columns.
// Returns []*sqlmodel.Table which holds the parent tables found.
// Returns map[string]struct{} which holds the keys of the columns the table declares or
// copies itself.
func (b *catalogueBuilder) createdTableColumns(
	ctx context.Context,
	mutation *ast.CatalogueMutation,
) ([]sqlmodel.Column, []*sqlmodel.Table, map[string]struct{}) {
	gathered := &tableColumns{builder: b, local: make(map[string]struct{}), columns: nil}
	var parents []*sqlmodel.Table
	for _, reference := range mutation.InheritsTables {
		parent, findError := b.findTable(reference.Schema, reference.Name)
		if findError != nil || slices.Contains(parents, parent) {
			continue
		}
		parents = append(parents, parent)
		for index := range parent.Columns {
			gathered.add(new(cloneColumn(&parent.Columns[index])), false)
		}
	}
	if mutation.ViewDefinition != nil {
		queried := b.queryTableColumns(ctx, mutation)
		for index := range queried {
			gathered.add(&queried[index], true)
		}
	}
	for index := range mutation.Columns {
		b.addCopiedColumns(gathered, mutation, index)
		gathered.add(&mutation.Columns[index], true)
	}
	b.addCopiedColumns(gathered, mutation, len(mutation.Columns))
	return gathered.columns, parents, gathered.local
}

// addCopiedColumns adds the columns of every table the mutation copies at a position
// among its own columns; at the last position it adds those of every table copied at or
// beyond it.
//
// Takes gathered (*tableColumns) which receives the columns.
// Takes mutation (*ast.CatalogueMutation) which holds the copied tables.
// Takes position (int) which is how many of the mutation's own columns come before.
func (b *catalogueBuilder) addCopiedColumns(gathered *tableColumns, mutation *ast.CatalogueMutation, position int) {
	for _, copied := range mutation.CopiedTables {
		last := position == len(mutation.Columns)
		if copied.Position != position && (!last || copied.Position < position) {
			continue
		}
		source, findError := b.findTable(copied.Table.Schema, copied.Table.Name)
		if findError != nil {
			continue
		}
		for index := range source.Columns {
			gathered.add(new(cloneColumn(&source.Columns[index])), true)
		}
	}
}

// copiedKeys returns the primary key and constraints a created table takes from the
// tables it copies with their keys, for a table that declares no primary key of its own.
//
// Takes mutation (*ast.CatalogueMutation) which holds the copied tables.
//
// Returns []string which is the primary key copied, or nil.
// Returns []sqlmodel.Constraint which are the constraints copied.
func (b *catalogueBuilder) copiedKeys(mutation *ast.CatalogueMutation) ([]string, []sqlmodel.Constraint) {
	var primaryKey []string
	var constraints []sqlmodel.Constraint
	for _, copied := range mutation.CopiedTables {
		if !copied.Keys {
			continue
		}
		source, findError := b.findTable(copied.Table.Schema, copied.Table.Name)
		if findError != nil {
			continue
		}
		if primaryKey == nil {
			primaryKey = slices.Clone(source.PrimaryKey)
		}
		constraints = append(constraints, slices.Clone(source.Constraints)...)
	}
	return primaryKey, constraints
}

// recordInheritance records that a created table inherits its parents' columns and which
// of its columns it declares itself.
//
// Takes child (*sqlmodel.Table) which is the created table.
// Takes parents ([]*sqlmodel.Table) which are the tables it inherits.
// Takes local (map[string]struct{}) which holds the keys of its own columns.
func (b *catalogueBuilder) recordInheritance(child *sqlmodel.Table, parents []*sqlmodel.Table, local map[string]struct{}) {
	if len(parents) == 0 {
		return
	}
	for _, parent := range parents {
		b.inheritances = append(b.inheritances, inheritance{parent: parent, child: child})
	}
	b.localColumns[child] = local
}

// inheritorsOf returns every table that inherits from a table, at every level, each once
// and after the tables it inherits from, and reports on the statement when the depth
// limit leaves some out.
//
// Takes table (*sqlmodel.Table) which is the table.
//
// Returns []*sqlmodel.Table which holds the tables that inherit from it.
func (b *catalogueBuilder) inheritorsOf(table *sqlmodel.Table) []*sqlmodel.Table {
	if len(b.inheritances) == 0 {
		return nil
	}
	inheritors, limited := newInheritanceIndex(b.inheritances).descendants(table)
	if limited {
		b.reportInheritanceDepth(table)
	}
	return inheritors
}

// reportInheritanceDepth warns, on the statement being applied, that the tables below a
// table nest more deeply than a change is carried.
//
// Takes table (*sqlmodel.Table) which is the table the change starts at.
func (b *catalogueBuilder) reportInheritanceDepth(table *sqlmodel.Table) {
	b.statementDiagnostics = append(b.statementDiagnostics, diagbuild.Block(b.statement.filename, max(b.statement.line, 1),
		diag.CodeDepthLimit, diag.SeverityWarning,
		fmt.Sprintf("the tables that inherit from or partition table %s nest more than %d levels deep, so the change "+
			"does not reach the tables past that depth", table.Name, maxInheritanceDepth)))
}

// parentsOf returns the tables whose columns a table inherits directly.
//
// Takes child (*sqlmodel.Table) which is the table.
//
// Returns []*sqlmodel.Table which holds its parents.
func (b *catalogueBuilder) parentsOf(child *sqlmodel.Table) []*sqlmodel.Table {
	var parents []*sqlmodel.Table
	for _, link := range b.inheritances {
		if link.child == child {
			parents = append(parents, link.parent)
		}
	}
	return parents
}

// markLocal records that a table declares a column itself, as an ALTER TABLE adding it to
// that table does.
//
// Takes table (*sqlmodel.Table) which is the table.
// Takes name (string) which is the column's name.
func (b *catalogueBuilder) markLocal(table *sqlmodel.Table, name string) {
	local, inherits := b.localColumns[table]
	if !inherits {
		return
	}
	local[b.key(name)] = struct{}{}
}

// propagateAddedColumns adds the columns an ADD COLUMN gave a table to every table that
// inherits from it, at every level, merging each into a column of the same name a child
// already has. Each table is changed once, however many paths lead to it.
//
// Takes parent (*sqlmodel.Table) which is the table the columns were added to.
// Takes columns ([]sqlmodel.Column) which are the added columns.
func (b *catalogueBuilder) propagateAddedColumns(parent *sqlmodel.Table, columns []sqlmodel.Column) {
	for _, child := range b.inheritorsOf(parent) {
		gathered := &tableColumns{builder: b, local: make(map[string]struct{}), columns: child.Columns}
		for index := range columns {
			gathered.add(new(cloneColumn(&columns[index])), false)
		}
		child.Columns = gathered.columns
	}
}

// propagateDroppedColumn drops a column a parent dropped from every table that inherits
// it, at every level, unless the table declares the column itself or inherits it from a
// parent that still has it.
//
// Parents are visited before their children. A column inherited through several paths
// remains until every path drops it. ALTER TABLE ONLY keeps the children's copies as
// locally declared columns.
//
// Takes parent (*sqlmodel.Table) which is the table the column was dropped from.
// Takes mutation (*ast.CatalogueMutation) which names the column and says whether ONLY
// was written.
func (b *catalogueBuilder) propagateDroppedColumn(parent *sqlmodel.Table, mutation *ast.CatalogueMutation) {
	if len(b.inheritances) == 0 {
		return
	}
	index := newInheritanceIndex(b.inheritances)
	if mutation.Only {
		for _, child := range index.children[parent] {
			b.markLocal(child, mutation.ColumnName)
		}
		return
	}
	inheritors, limited := index.descendants(parent)
	if limited {
		b.reportInheritanceDepth(parent)
	}
	for _, child := range inheritors {
		if _, local := b.localColumns[child][b.key(mutation.ColumnName)]; local {
			continue
		}
		if slices.ContainsFunc(index.parents[child], func(other *sqlmodel.Table) bool { return b.hasColumn(other.Columns, mutation.ColumnName) }) {
			continue
		}
		child.Columns = slices.DeleteFunc(child.Columns, func(column sqlmodel.Column) bool {
			return b.sameName(column.Name, mutation.ColumnName)
		})
	}
}

// propagateColumnChange applies a change to a parent's column to the same column of every
// table that inherits it, at every level, once each, unless the ALTER TABLE was written
// with ONLY.
//
// Takes parent (*sqlmodel.Table) which is the table whose column changed.
// Takes mutation (*ast.CatalogueMutation) which names the column and the change.
// Takes change (func(*sqlmodel.Column)) which changes one column.
func (b *catalogueBuilder) propagateColumnChange(parent *sqlmodel.Table, mutation *ast.CatalogueMutation, change func(*sqlmodel.Column)) {
	if mutation.Only {
		return
	}
	for _, child := range b.inheritorsOf(parent) {
		for index := range child.Columns {
			if b.sameName(child.Columns[index].Name, mutation.ColumnName) {
				change(&child.Columns[index])
			}
		}
	}
}

// dropInheritors drops every table that inherits from a dropped table, at every level, as
// the database requires DROP TABLE ... CASCADE to drop a parent and drops its children
// with it, and forgets every inheritance the dropped tables took part in.
//
// Takes table (*sqlmodel.Table) which is the dropped table.
func (b *catalogueBuilder) dropInheritors(table *sqlmodel.Table) {
	if len(b.inheritances) == 0 {
		return
	}
	dropped := map[*sqlmodel.Table]struct{}{table: {}}
	for _, child := range b.inheritorsOf(table) {
		dropped[child] = struct{}{}
		if schema := b.catalogue.Schemas[b.key(child.Schema)]; schema != nil && schema.Tables[b.key(child.Name)] == child {
			delete(schema.Tables, b.key(child.Name))
		}
	}
	b.inheritances = slices.DeleteFunc(b.inheritances, func(link inheritance) bool {
		_, parentDropped := dropped[link.parent]
		_, childDropped := dropped[link.child]
		return parentDropped || childDropped
	})
	for droppedTable := range dropped {
		delete(b.localColumns, droppedTable)
		delete(b.partitionParents, droppedTable)
	}
}
