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
	"fmt"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// applyCreateIndex adds an index record to the target table.
//
// Takes mutation (*ast.CatalogueMutation) which holds the index definition.
//
// Returns error when no table or view of that name exists, as the engine refuses an index
// on a missing relation even with IF NOT EXISTS, which only covers the index's own name.
func (b *catalogueBuilder) applyCreateIndex(mutation *ast.CatalogueMutation) error {
	table, err := b.findTable(mutation.SchemaName, mutation.TableName)
	if err != nil {
		if _, view := b.findView(mutation.SchemaName, mutation.TableName); view != nil {
			return nil
		}
		return err
	}
	definition := sqlmodel.Index{Name: mutation.NewName, Origin: mutation.Origin, Columns: nil, IsUnique: false, IsPrimary: false, IsFullText: false}
	if mutation.Index != nil {
		definition = *mutation.Index
		definition.Columns = slices.Clone(mutation.Index.Columns)
		definition.Origin = mutation.Origin
	}
	if b.mysqlFamily() && !definition.IsFullText {
		b.dropImplicitIndexes(table, definition.Columns)
	}
	if definition.Name == "" {
		definition.Name = b.nameIndex(table, &definition, mutation.IndexElements)
	}
	if mutation.IfNotExists && b.indexExists(table, definition.Name, mutation.IndexScope) {
		return nil
	}
	if b.mysqlFamily() && b.mysqlKeyNameTaken(table, definition.Name) {
		return duplicateObject("key name %s is already used in table %s", definition.Name, table.Name)
	}
	table.Indexes = append(table.Indexes, definition)
	return nil
}

// mysqlKeyNameTaken reports whether a MySQL or MariaDB table already has a key of a name:
// PRIMARY, an index or a unique key, which share one namespace there.
//
// Takes table (*sqlmodel.Table) which is the table.
// Takes name (string) which is the name.
//
// Returns bool which is true when a key holds the name.
func (b *catalogueBuilder) mysqlKeyNameTaken(table *sqlmodel.Table, name string) bool {
	if name == "" {
		return false
	}
	return strings.EqualFold(name, mysqlPrimary) || b.tableIndexNamed(table, name) ||
		slices.ContainsFunc(table.Constraints, func(constraint sqlmodel.Constraint) bool {
			return constraint.Kind == sqlmodel.ConstraintUnique && strings.EqualFold(constraint.Name, name)
		})
}

// refuseDuplicateKeyNames refuses unique keys a MySQL or MariaDB statement adds under a
// name a key of the table, or another the statement adds, already holds.
//
// Takes table (*sqlmodel.Table) which holds the keys the table had before, or nil for a
// table the statement creates.
// Takes added ([]sqlmodel.Constraint) which are the constraints the statement adds.
//
// Returns error when two keys share a name.
func (b *catalogueBuilder) refuseDuplicateKeyNames(table *sqlmodel.Table, added []sqlmodel.Constraint) error {
	if !b.mysqlFamily() {
		return nil
	}
	seen := make(map[string]bool)
	for _, constraint := range added {
		if constraint.Kind != sqlmodel.ConstraintUnique || constraint.Name == "" {
			continue
		}
		key := strings.ToLower(constraint.Name)
		if seen[key] || table != nil && b.mysqlKeyNameTaken(table, constraint.Name) {
			return duplicateObject("key name %s is already used in table %s", constraint.Name, table.Name)
		}
		seen[key] = true
	}
	return nil
}

// applyDropIndex removes a named index from its table or the visible schema.
//
// Takes mutation (*ast.CatalogueMutation) which names an index and optional table.
//
// Returns error when a table-qualified drop names a missing table.
func (b *catalogueBuilder) applyDropIndex(mutation *ast.CatalogueMutation) error {
	if mutation.TableName != "" {
		table, err := b.findTable(mutation.SchemaName, mutation.TableName)
		if err != nil {
			return err
		}
		b.removeIndex(table, mutation.NewName)
		return nil
	}
	for _, name := range b.catalogue.SchemaNamesFor(mutation.SchemaName) {
		schema := b.catalogue.Schemas[name]
		if schema == nil {
			continue
		}
		for _, table := range schema.Tables {
			if b.removeIndex(table, mutation.NewName) {
				return nil
			}
		}
	}
	return nil
}

// removeIndex deletes a matching index, or the unique constraint an index of that name
// backs, while preserving every other declaration.
//
// The indexes are copied first, as another table or a replay snapshot may share them.
//
// Takes table (*sqlmodel.Table) which owns the candidate index.
// Takes name (string) which is compared using the dialect's identifier rules.
//
// Returns bool which reports whether an index or constraint was removed.
func (b *catalogueBuilder) removeIndex(table *sqlmodel.Table, name string) bool {
	before := len(table.Indexes)
	table.Indexes = slices.DeleteFunc(slices.Clone(table.Indexes), func(index sqlmodel.Index) bool { return b.sameName(index.Name, name) })
	return len(table.Indexes) != before || b.dropUniqueConstraint(table, name)
}

// indexExists searches the table or schema namespace used by CREATE INDEX.
//
// Takes table (*sqlmodel.Table) which is the proposed index owner.
// Takes name (string) which identifies the new index.
// Takes scope (ast.IndexNameScope) which selects the objects sharing names.
//
// Returns bool which reports an existing index in the required namespace.
func (b *catalogueBuilder) indexExists(table *sqlmodel.Table, name string, scope ast.IndexNameScope) bool {
	matches := func(index sqlmodel.Index) bool { return b.sameName(index.Name, name) }
	if scope == ast.IndexNamesInTable {
		return slices.ContainsFunc(table.Indexes, matches)
	}
	schema := b.catalogue.Schemas[b.key(table.Schema)]
	if schema == nil {
		return false
	}
	if scope == ast.IndexNamesInRelations && schemaHoldsRelation(schema, b.key(name)) {
		return true
	}
	for _, candidate := range schema.Tables {
		if slices.ContainsFunc(candidate.Indexes, matches) {
			return true
		}
	}
	return false
}

// columnDroppedBy reports whether a column belongs to a DROP COLUMN mutation.
//
// Takes name (string) which identifies a column or index key.
// Takes mutation (*ast.CatalogueMutation) which specifies the dropped names.
//
// Returns bool which includes any flattened nested columns.
func (b *catalogueBuilder) columnDroppedBy(name string, mutation *ast.CatalogueMutation) bool {
	prefix := b.key(mutation.DroppedColumnPrefix)
	return b.sameName(name, mutation.ColumnName) || prefix != "" && strings.HasPrefix(b.key(name), prefix)
}

// checkColumnIndexDependencies rejects column drops blocked by index dependencies.
//
// Takes table (*sqlmodel.Table) which owns the column and indexes.
// Takes mutation (*ast.CatalogueMutation) which specifies the dependency policy.
//
// Returns error when a tracked index prevents the column drop.
func (b *catalogueBuilder) checkColumnIndexDependencies(table *sqlmodel.Table, mutation *ast.CatalogueMutation) error {
	policy := mutation.DroppedColumnIndexes
	if policy != ast.ColumnIndexesRestrict && policy != ast.ColumnIndexesPreserveUnique {
		return nil
	}
	for _, index := range table.Indexes {
		dropped := 0
		for _, column := range index.Columns {
			if b.columnDroppedBy(column, mutation) {
				dropped++
			}
		}
		if indexBlocksDrop(policy, index, dropped) {
			return fmt.Errorf("cannot drop column %s while index %s depends on it", mutation.ColumnName, index.Name)
		}
	}
	return nil
}

// dropIndexColumns applies the dialect's dependency rule after a column drop.
//
// The indexes and their columns are copied before they change, as another table or a
// replay snapshot may share them.
//
// Takes table (*sqlmodel.Table) which owns the affected indexes.
// Takes mutation (*ast.CatalogueMutation) which identifies dropped columns and their
// policy.
func (b *catalogueBuilder) dropIndexColumns(table *sqlmodel.Table, mutation *ast.CatalogueMutation) {
	policy := mutation.DroppedColumnIndexes
	drops := func(column string) bool { return b.columnDroppedBy(column, mutation) }
	if policy == ast.ColumnIndexesDrop {
		table.Indexes = slices.DeleteFunc(slices.Clone(table.Indexes), func(index sqlmodel.Index) bool {
			return slices.ContainsFunc(index.Columns, drops)
		})
		return
	}
	if policy != ast.ColumnIndexesTrim && policy != ast.ColumnIndexesPreserveUnique {
		return
	}
	kept := make([]sqlmodel.Index, 0, len(table.Indexes))
	for _, index := range table.Indexes {
		before := len(index.Columns)
		index.Columns = slices.DeleteFunc(slices.Clone(index.Columns), drops)
		if before == 0 || len(index.Columns) > 0 {
			kept = append(kept, index)
		}
	}
	table.Indexes = kept
}

// schemaHoldsRelation reports whether a schema holds a table, view or sequence under a
// name key, as those share one namespace with indexes on some engines.
//
// Takes schema (*sqlmodel.Schema) which is the schema to search.
// Takes key (string) which is the key of the name.
//
// Returns bool which is true when such an object exists.
func schemaHoldsRelation(schema *sqlmodel.Schema, key string) bool {
	return schema.Tables[key] != nil || schema.Views[key] != nil || schema.Sequences[key] != nil
}

// indexBlocksDrop reports whether an index prevents a column drop.
//
// A restricting dialect refuses any drop an index depends on. A dialect that keeps unique
// indexes refuses only a drop that would leave a unique index on fewer columns, which
// would change what it enforces.
//
// Takes policy (ast.ColumnIndexPolicy) which is the dialect's rule.
// Takes index (sqlmodel.Index) which is the index.
// Takes dropped (int) which counts the index's columns the drop removes.
//
// Returns bool which is true when the drop must be refused.
func indexBlocksDrop(policy ast.ColumnIndexPolicy, index sqlmodel.Index, dropped int) bool {
	if dropped == 0 {
		return false
	}
	if policy == ast.ColumnIndexesRestrict {
		return true
	}
	return index.IsUnique && dropped < len(index.Columns)
}
