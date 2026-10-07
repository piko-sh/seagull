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
	"maps"
	"slices"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// applyCreatePartition creates a partition with its parent's columns, primary key and
// constraints, and keeps its columns synchronised with the parent. A partition's own
// primary key survives when the parent has none.
//
// Takes mutation (*ast.CatalogueMutation) which names the partition and its parent.
//
// Returns error when the name is taken or the parent is missing.
func (b *catalogueBuilder) applyCreatePartition(mutation *ast.CatalogueMutation) error {
	schema := b.resolveSchema(mutation.SchemaName)
	if schema.Tables[b.key(mutation.TableName)] != nil || schema.Views[b.key(mutation.TableName)] != nil {
		if mutation.IfNotExists {
			return nil
		}
		return duplicateObject("table %s already exists", qualifiedName(schema.Name, mutation.TableName))
	}
	parent, err := b.findTable(mutation.PartitionOf.Schema, mutation.PartitionOf.Name)
	if err != nil {
		return err
	}
	columns := cloneColumns(parent.Columns)
	for index := range columns {
		columns[index].Origin = mutation.Origin
	}
	primaryKey := slices.Clone(parent.PrimaryKey)
	if len(primaryKey) == 0 {
		primaryKey = slices.Clone(mutation.PrimaryKey)
	}
	constraints := append(clonedConstraints(parent.Constraints, mutation.Origin), clonedConstraints(mutation.Constraints, mutation.Origin)...)
	partition := &sqlmodel.Table{
		Name:              mutation.TableName,
		Schema:            schema.Name,
		Comment:           "",
		VirtualModuleName: "",
		Columns:           columns,
		PrimaryKey:        primaryKey,
		Indexes:           nil,
		Constraints:       constraints,
		PartitionOf:       nil,
		Origin:            mutation.Origin,
		IsVirtual:         false,
		IsWithoutRowID:    false,
	}
	schema.Tables[b.key(mutation.TableName)] = partition
	b.recordPartition(partition, parent)
	return nil
}

// recordPartition records that a table is a partition of another: it inherits the
// parent's columns and declares none itself, so every later change to the parent's
// columns reaches it.
//
// Takes partition (*sqlmodel.Table) which is the partition.
// Takes parent (*sqlmodel.Table) which is the table it partitions.
func (b *catalogueBuilder) recordPartition(partition, parent *sqlmodel.Table) {
	b.inheritances = append(b.inheritances, inheritance{parent: parent, child: partition})
	b.localColumns[partition] = make(map[string]struct{})
	b.partitionParents[partition] = parent
}

// applyAttachPartition makes a table a partition of another, as ALTER TABLE parent ATTACH
// PARTITION does: the table keeps its columns, which the database requires to be the
// parent's, takes the parent's primary key when it has none, and from then on follows the
// parent's column changes.
//
// Takes mutation (*ast.CatalogueMutation) which names the table and its new parent.
//
// Returns error when the mutation names no parent, the table or the parent is missing,
// the table is a partition already, or attaching it would make it a partition of itself.
func (b *catalogueBuilder) applyAttachPartition(mutation *ast.CatalogueMutation) error {
	if mutation.PartitionOf == nil {
		return refused(errMalformedMutation, "ATTACH PARTITION %s names no parent table", mutation.TableName)
	}
	table, err := b.findTable(mutation.SchemaName, mutation.TableName)
	if err != nil {
		return err
	}
	parent, err := b.findTable(mutation.PartitionOf.Schema, mutation.PartitionOf.Name)
	if err != nil {
		return err
	}
	if err := b.checkAttachable(table, parent); err != nil {
		return err
	}
	if len(table.PrimaryKey) == 0 {
		table.PrimaryKey = slices.Clone(parent.PrimaryKey)
	}
	b.recordPartition(table, parent)
	return nil
}

// checkAttachable reports why a table cannot become a partition of another: it is the
// other table, it partitions a table already, or the other table inherits from it or
// partitions it at some level, so the link would close a cycle.
//
// Takes table (*sqlmodel.Table) which is the table to attach.
// Takes parent (*sqlmodel.Table) which is the table it would partition.
//
// Returns error which says why, or nil when the table can be attached.
func (b *catalogueBuilder) checkAttachable(table, parent *sqlmodel.Table) error {
	if table == parent {
		return refused(errPartitionCycle, "table %s cannot be attached as a partition of itself", table.Name)
	}
	if current := b.partitionParents[table]; current != nil {
		return refused(errAlreadyPartition, "table %s is already a partition of %s", table.Name, current.Name)
	}
	if newInheritanceIndex(b.inheritances).ancestorOf(table, parent) {
		return refused(errPartitionCycle, "attaching table %s as a partition of %s would make it a partition of itself",
			table.Name, parent.Name)
	}
	return nil
}

// applyDetachPartition makes a partition a table of its own again, as ALTER TABLE parent
// DETACH PARTITION does: it keeps the columns, primary key and constraints it has, now
// its own, and any partitions under it stay partitions of it.
//
// Takes mutation (*ast.CatalogueMutation) which names the partition and its parent.
//
// Returns error when the mutation names no parent, or the named table is not a partition
// of the named parent.
func (b *catalogueBuilder) applyDetachPartition(mutation *ast.CatalogueMutation) error {
	if mutation.PartitionOf == nil {
		return refused(errMalformedMutation, "DETACH PARTITION %s names no parent table", mutation.TableName)
	}
	table := b.catalogue.LookupTable(mutation.SchemaName, mutation.TableName)
	parent := b.catalogue.LookupTable(mutation.PartitionOf.Schema, mutation.PartitionOf.Name)
	if table == nil || parent == nil || b.partitionParents[table] != parent {
		return unknownObject("table %s is not a partition of %s", mutation.TableName, mutation.PartitionOf.Name)
	}
	delete(b.partitionParents, table)
	b.inheritances = slices.DeleteFunc(b.inheritances, func(link inheritance) bool {
		return link.parent == parent && link.child == table
	})
	if len(b.parentsOf(table)) == 0 {
		delete(b.localColumns, table)
	}
	return nil
}

// propagatePrimaryKey gives the partitions of a table, at every level, the primary key
// the table was given, as the database creates the key on each partition. Each partition
// is changed once.
//
// Takes parent (*sqlmodel.Table) which is the table given the key.
func (b *catalogueBuilder) propagatePrimaryKey(parent *sqlmodel.Table) {
	if len(b.partitionParents) == 0 {
		return
	}
	links := make([]inheritance, 0, len(b.partitionParents))
	for partition, partitioned := range b.partitionParents {
		links = append(links, inheritance{parent: partitioned, child: partition})
	}
	partitions, limited := newInheritanceIndex(links).descendants(parent)
	if limited {
		b.reportInheritanceDepth(parent)
	}
	for _, partition := range partitions {
		partition.PrimaryKey = slices.Clone(parent.PrimaryKey)
	}
}

// dropOrphanedPartitions drops every partition whose parent is no longer in the
// catalogue, at every level, as the database drops the partitions of a table a dropped
// schema held.
func (b *catalogueBuilder) dropOrphanedPartitions() {
	for {
		orphaned := false
		for _, partition := range slices.Collect(maps.Keys(b.partitionParents)) {
			parent := b.partitionParents[partition]
			if b.holdsTable(parent) || !b.holdsTable(partition) {
				continue
			}
			delete(b.catalogue.Schemas[b.key(partition.Schema)].Tables, b.key(partition.Name))
			b.dropInheritors(partition)
			orphaned = true
		}
		if !orphaned {
			return
		}
	}
}

// holdsTable reports whether the catalogue still holds a table under its name.
//
// Takes table (*sqlmodel.Table) which is the table.
//
// Returns bool which is true when the table's schema files it under its name.
func (b *catalogueBuilder) holdsTable(table *sqlmodel.Table) bool {
	schema := b.catalogue.Schemas[b.key(table.Schema)]
	return schema != nil && schema.Tables[b.key(table.Name)] == table
}

// namePartitionParents sets, on every partition, the name its parent has now, for the
// catalogue's readers, and clears it on every other table.
func (b *catalogueBuilder) namePartitionParents() {
	for _, schema := range b.catalogue.Schemas {
		for _, table := range schema.Tables {
			table.PartitionOf = nil
			if parent := b.partitionParents[table]; parent != nil {
				table.PartitionOf = &sqlmodel.TableName{Schema: parent.Schema, Name: parent.Name}
			}
		}
	}
}

// clonedConstraints returns copies of constraints, each recording the migration that
// gives them to a table.
//
// Takes constraints ([]sqlmodel.Constraint) which are the constraints.
// Takes origin (sqlmodel.MigrationOrigin) which is the migration.
//
// Returns []sqlmodel.Constraint which holds the copies.
func clonedConstraints(constraints []sqlmodel.Constraint, origin sqlmodel.MigrationOrigin) []sqlmodel.Constraint {
	copied := make([]sqlmodel.Constraint, 0, len(constraints))
	for _, constraint := range constraints {
		constraint.Columns = slices.Clone(constraint.Columns)
		constraint.ForeignColumns = slices.Clone(constraint.ForeignColumns)
		constraint.Origin = origin
		copied = append(copied, constraint)
	}
	return copied
}
