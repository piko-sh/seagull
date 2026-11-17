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
	"cmp"
	"context"
	"slices"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// duplicatedColumn finds a column a table definition declares twice, comparing names as
// the dialect does, so a definition every engine refuses is refused here rather than
// built with one column hiding the other.
//
// Takes columns ([]sqlmodel.Column) which are the declared columns.
//
// Returns string which is the repeated name.
// Returns bool which is true when a name repeats.
func (b *catalogueBuilder) duplicatedColumn(columns []sqlmodel.Column) (string, bool) {
	seen := make(map[string]bool, len(columns))
	for index := range columns {
		key := b.key(columns[index].Name)
		if seen[key] {
			return columns[index].Name, true
		}
		seen[key] = true
	}
	return "", false
}

// applyCreateTable adds a new table to the catalogue, resolving custom column types.
//
// The table takes the columns of the tables it inherits first, then its own and those of
// the tables it copies, a column whose name is already present merged into it, as the
// database builds it; a CREATE TABLE ... AS SELECT takes its columns from the query's
// output columns, resolved against the catalogue as it stands. A partition takes its
// parent's columns. A CREATE TABLE IF NOT EXISTS of a name already in use changes
// nothing.
//
// Takes mutation (*ast.CatalogueMutation) which holds the table definition.
//
// Returns error when the table or a partition of that name already exists.
func (b *catalogueBuilder) applyCreateTable(ctx context.Context, mutation *ast.CatalogueMutation) error {
	if mutation.PartitionOf != nil {
		return b.applyCreatePartition(mutation)
	}
	schema := b.resolveSchema(mutation.SchemaName)
	if schema.Tables[b.key(mutation.TableName)] != nil || (mutation.IfNotExists && schema.Views[b.key(mutation.TableName)] != nil) {
		if mutation.IfNotExists {
			return nil
		}
		return duplicateObject("table %s already exists", qualifiedName(schema.Name, mutation.TableName))
	}
	if name, duplicated := b.duplicatedColumn(mutation.Columns); duplicated {
		return duplicateObject("column %s is declared more than once in table %s", name, mutation.TableName)
	}

	columns, parents, local := b.createdTableColumns(ctx, mutation)
	for index := range columns {
		columns[index].Origin = mutation.Origin
		b.resolveCustomColumnType(&columns[index])
	}
	primaryKey, constraints := b.copiedKeys(mutation)
	if len(mutation.PrimaryKey) > 0 {
		primaryKey = mutation.PrimaryKey
	}
	b.forgetCopiedKeyNames(constraints)
	declaredKey := primaryKey
	if len(mutation.PrimaryKey) == 0 && slices.ContainsFunc(constraints, isPrimaryKeyConstraint) {
		declaredKey = nil
	}
	constraints = append(constraints, b.declaredKeys(declaredKey, mutation.Constraints)...)
	for index := range constraints {
		constraints[index].Origin = mutation.Origin
	}
	table := &sqlmodel.Table{
		Name:              mutation.TableName,
		Schema:            schema.Name,
		Columns:           columns,
		PrimaryKey:        primaryKey,
		Constraints:       constraints,
		PartitionOf:       nil,
		IsVirtual:         mutation.IsVirtual,
		VirtualModuleName: mutation.VirtualModuleName,
		IsWithoutRowID:    mutation.IsWithoutRowID,
		Origin:            mutation.Origin,
		Comment:           mutation.Comment,
		Indexes:           b.copiedIndexes(mutation),
	}
	if err := b.refuseDuplicateKeyNames(nil, table.Constraints); err != nil {
		return err
	}
	schema.Tables[b.key(mutation.TableName)] = table
	b.recordInheritance(table, parents, local)
	b.declareNames(table, mutation)
	b.keepNotNull(table, table.Constraints)
	b.nameConstraints(table, 0, true)
	return nil
}

// queryTableColumns derives columns for CREATE TABLE AS SELECT.
//
// It keeps output names and types but does not inherit source-column constraints or
// overrides.
//
// Takes mutation (*ast.CatalogueMutation) which holds the query.
//
// Returns []sqlmodel.Column which holds the table's columns.
func (b *catalogueBuilder) queryTableColumns(ctx context.Context, mutation *ast.CatalogueMutation) []sqlmodel.Column {
	b.applyPendingColumnOverrides()
	columns, problems := b.resolveViewColumns(ctx, mutation.ViewDefinition)
	b.reportViewProblems("table "+mutation.TableName, problems)
	for index := range columns {
		columns[index].Nullable = true
		columns[index].GoTypeOverride = nil
		columns[index].SQLTypeOverride = ""
		columns[index].NullableOverride = nil
	}
	return columns
}

// applyDropTable removes a table from the catalogue, with every table that inherits from
// it. A name that only a view holds leaves the view in place, unless the mutation drops
// views too.
//
// Takes mutation (*ast.CatalogueMutation) which identifies the table to drop.
//
// Returns error when no table or view of the name exists and the statement did not say IF
// EXISTS.
func (b *catalogueBuilder) applyDropTable(mutation *ast.CatalogueMutation) error {
	key := b.key(mutation.TableName)
	schema := b.schemaHolding(mutation.SchemaName, func(candidate *sqlmodel.Schema) bool { return candidate.Tables[key] != nil })
	if schema == nil {
		if b.catalogue.LookupView(mutation.SchemaName, mutation.TableName) != nil {
			if mutation.DropsView {
				return b.applyDropView(mutation)
			}
			return nil
		}
		return missingDropTarget(mutation, tableNotFoundFormat, mutation.TableName, b.searchedSchemas(mutation.SchemaName))
	}
	if err := b.applyDropToViews(mutation); err != nil {
		return err
	}
	b.dropInheritors(schema.Tables[key])
	delete(schema.Tables, key)
	return nil
}

// applyAlterTableAddColumn adds columns to an existing table, last or where the
// mutation's placement puts them, and to every table that inherits from it.
//
// A column the table already has is skipped when the statement said IF NOT EXISTS, and is
// an error otherwise, as the database reports it.
//
// Takes mutation (*ast.CatalogueMutation) which holds the columns to add.
//
// Returns error when the target table is not found, a column already exists or the
// placement's anchor is missing.
func (b *catalogueBuilder) applyAlterTableAddColumn(mutation *ast.CatalogueMutation) error {
	table, err := b.findTable(mutation.SchemaName, mutation.TableName)
	if err != nil {
		return err
	}
	var added []sqlmodel.Column
	for index := range mutation.Columns {
		column := mutation.Columns[index]
		if b.hasColumn(table.Columns, column.Name) || b.hasColumn(added, column.Name) {
			if mutation.IfNotExists {
				continue
			}
			return duplicateObject("column %s already exists in table %s", column.Name, table.Name)
		}
		column.Origin = mutation.Origin
		b.resolveCustomColumnType(&column)
		added = append(added, column)
	}
	if len(added) == 0 {
		return nil
	}
	index, err := b.placementIndex(table.Columns, mutation.Placement, table.Name)
	if err != nil {
		return err
	}
	table.Columns = slices.Insert(slices.Clone(table.Columns), index, added...)
	for index := range added {
		b.markLocal(table, added[index].Name)
	}
	b.declareNames(table, mutation)
	if !mutation.Only {
		b.propagateAddedColumns(table, added)
	}
	return nil
}

// hasColumn reports whether a column list holds a column of the given name.
//
// Takes columns ([]sqlmodel.Column) which are the columns.
// Takes name (string) which is the name.
//
// Returns bool which is true when a column has the name.
func (b *catalogueBuilder) hasColumn(columns []sqlmodel.Column, name string) bool {
	return slices.ContainsFunc(columns, func(column sqlmodel.Column) bool { return b.sameName(column.Name, name) })
}

// applyAlterTableDropColumn removes a column from an existing table, and with it every
// column whose name starts with the mutation's DroppedColumnPrefix, and from every table
// that inherits the column.
//
// Takes mutation (*ast.CatalogueMutation) which identifies the column to drop.
//
// Returns error when the target table is not found, or it holds no column the mutation
// drops, as the database refuses DROP COLUMN of a missing column without IF EXISTS.
func (b *catalogueBuilder) applyAlterTableDropColumn(mutation *ast.CatalogueMutation) error {
	table, err := b.findTable(mutation.SchemaName, mutation.TableName)
	if err != nil {
		return err
	}
	filtered := make([]sqlmodel.Column, 0, len(table.Columns))
	for index := range table.Columns {
		dropped := b.columnDroppedBy(table.Columns[index].Name, mutation)
		if !dropped {
			filtered = append(filtered, table.Columns[index])
		}
	}
	if len(filtered) == len(table.Columns) {
		return unknownObject("column %s not found in table %s", mutation.ColumnName, table.Name)
	}
	if err := b.checkColumnIndexDependencies(table, mutation); err != nil {
		return err
	}
	if err := b.refuseCheckedColumn(table, mutation.ColumnName, true); err != nil {
		return err
	}
	table.Columns = filtered
	dropped := func(column string) bool { return b.columnDroppedBy(column, mutation) }
	dropColumnConstraints(table, sqlmodel.ConstraintCheck, dropped)
	dropColumnConstraints(table, sqlmodel.ConstraintNotNull, dropped)
	b.dropIndexColumns(table, mutation)
	b.propagateDroppedColumn(table, mutation)
	return nil
}

// applyAlterTableAlterColumn changes a column's definition, nullability or position and
// propagates the change to inheriting tables. Metadata annotations on views leave them
// unchanged.
//
// Takes mutation (*ast.CatalogueMutation) which holds the new column definition or the
// nullability change.
//
// Returns error when the target table or column is not found.
func (b *catalogueBuilder) applyAlterTableAlterColumn(mutation *ast.CatalogueMutation) error {
	table, err := b.findTable(mutation.SchemaName, mutation.TableName)
	if err != nil {
		if b.annotatesView(mutation) {
			return nil
		}
		return err
	}
	if mutation.SetsNotNull || mutation.DropsNotNull {
		return b.applyColumnNullability(table, mutation)
	}
	if mutation.SetsDefault || mutation.DropsDefault {
		return b.applyColumnDefault(table, mutation)
	}
	if len(mutation.Columns) == 0 {
		return nil
	}
	if mutation.SetsType {
		return b.applyColumnType(table, mutation)
	}
	if mutation.RefusesNullKey && b.inPrimaryKey(table, mutation.ColumnName) {
		return refused(errKeyColumnNotNull, "column %s of table %s is in a primary key, so it cannot be declared NULL",
			mutation.ColumnName, table.Name)
	}
	return b.replaceColumn(table, mutation)
}

// replaceColumn replaces a table's column with the mutation's definition, as MySQL MODIFY
// COLUMN does, and moves it where the mutation places it.
//
// Takes table (*sqlmodel.Table) which holds the column.
// Takes mutation (*ast.CatalogueMutation) which names the column and carries the new
// definition.
//
// Returns error when the table has no such column or the placement's anchor is missing.
func (b *catalogueBuilder) replaceColumn(table *sqlmodel.Table, mutation *ast.CatalogueMutation) error {
	altered := mutation.Columns[0]
	b.resolveCustomColumnType(&altered)
	for index := range table.Columns {
		if b.sameName(table.Columns[index].Name, mutation.ColumnName) {
			altered.Origin = table.Columns[index].Origin
			if mutation.KeepsUnstated {
				keepUnstated(&altered, &table.Columns[index])
			}
			b.keepKeyColumnNotNull(table, &altered, mutation.ColumnName)
			table.Columns[index] = altered
			if mutation.DropsColumnChecks {
				dropColumnConstraints(table, sqlmodel.ConstraintCheck, func(column string) bool { return b.sameName(column, mutation.ColumnName) })
			}
			b.renameKeyColumns(table, mutation.ColumnName, altered.Name)
			b.propagateColumnChange(table, mutation, func(column *sqlmodel.Column) {
				origin := column.Origin
				*column = cloneColumn(&altered)
				column.Origin = origin
			})
			return b.moveColumn(table, altered.Name, mutation.Placement)
		}
	}
	return columnNotFound(mutation)
}

// annotatesView reports whether an ALTER mutation changes no column, only the relation's
// engine-specific metadata, and names a view, such as a policy a dialect records on a
// continuous aggregate or a materialised view; such a mutation changes nothing the
// catalogue holds.
//
// Takes mutation (*ast.CatalogueMutation) which is the mutation.
//
// Returns bool which is true for an annotation of a view.
func (b *catalogueBuilder) annotatesView(mutation *ast.CatalogueMutation) bool {
	if changesColumn(mutation) {
		return false
	}
	return b.catalogue.LookupView(mutation.SchemaName, mutation.TableName) != nil
}

// applyColumnNullability sets whether a table's column may hold NULL, as a SET NOT NULL
// or DROP NOT NULL does, leaving the rest of the column as it was.
//
// Takes table (*sqlmodel.Table) which holds the column.
// Takes mutation (*ast.CatalogueMutation) which names the column and the change.
//
// Returns error when the table has no such column.
func (b *catalogueBuilder) applyColumnNullability(table *sqlmodel.Table, mutation *ast.CatalogueMutation) error {
	for index := range table.Columns {
		if b.sameName(table.Columns[index].Name, mutation.ColumnName) {
			if mutation.DropsNotNull && b.postgresFamily() && b.inPrimaryKey(table, table.Columns[index].Name) {
				return refused(errKeyColumnNotNull, "column %s of table %s is in a primary key, so it cannot drop NOT NULL",
					table.Columns[index].Name, table.Name)
			}
			table.Columns[index].Nullable = mutation.DropsNotNull
			if mutation.DropsNotNull {
				dropColumnConstraints(table, sqlmodel.ConstraintNotNull, func(column string) bool { return b.sameName(column, mutation.ColumnName) })
			}
			b.propagateColumnChange(table, mutation, func(column *sqlmodel.Column) {
				column.Nullable = mutation.DropsNotNull
			})
			return nil
		}
	}
	return columnNotFound(mutation)
}

// applyColumnDefault sets whether a table's column has a default, as a SET DEFAULT or
// DROP DEFAULT does, leaving the rest of the column as it was and moving it where the
// mutation places it.
//
// Takes table (*sqlmodel.Table) which holds the column.
// Takes mutation (*ast.CatalogueMutation) which names the column and the change.
//
// Returns error when the table has no such column or the placement's anchor is missing.
func (b *catalogueBuilder) applyColumnDefault(table *sqlmodel.Table, mutation *ast.CatalogueMutation) error {
	column := b.namedColumn(table.Columns, mutation.ColumnName)
	if column == nil {
		return columnNotFound(mutation)
	}
	column.HasDefault = mutation.SetsDefault
	b.propagateColumnChange(table, mutation, func(inherited *sqlmodel.Column) {
		inherited.HasDefault = mutation.SetsDefault
	})
	return b.moveColumn(table, column.Name, mutation.Placement)
}

// applyColumnType changes the type of a table's column, as ALTER COLUMN ...
//
// TYPE does, leaving its nullability, default and overrides as they were. A type that
// names a NOT NULL domain makes the column NOT NULL.
//
// Takes table (*sqlmodel.Table) which holds the column.
// Takes mutation (*ast.CatalogueMutation) which names the column and carries the new
// type.
//
// Returns error when the table has no such column.
func (b *catalogueBuilder) applyColumnType(table *sqlmodel.Table, mutation *ast.CatalogueMutation) error {
	changed := mutation.Columns[0]
	b.resolveCustomColumnType(&changed)
	retype := func(column *sqlmodel.Column) {
		column.SQLType = cloneSQLType(changed.SQLType)
		column.ArrayDimensions = changed.ArrayDimensions
		column.IsArray = changed.ArrayDimensions > 0
		if !changed.Nullable {
			column.Nullable = false
		}
	}
	for index := range table.Columns {
		if b.sameName(table.Columns[index].Name, mutation.ColumnName) {
			retype(&table.Columns[index])
			b.propagateColumnChange(table, mutation, retype)
			return nil
		}
	}
	return columnNotFound(mutation)
}

// applyAlterTableRenameColumn renames a column of an existing table, and of every table
// that inherits it, or of a view.
//
// Takes mutation (*ast.CatalogueMutation) which holds the old and new column names.
//
// Returns error when the target table or column is not found, or the table already has a
// column under the new name.
func (b *catalogueBuilder) applyAlterTableRenameColumn(mutation *ast.CatalogueMutation) error {
	table, err := b.findTable(mutation.SchemaName, mutation.TableName)
	if err != nil {
		if _, view := b.findView(mutation.SchemaName, mutation.TableName); view != nil {
			return b.renameViewColumn(mutation)
		}
		return err
	}
	column := b.namedColumn(table.Columns, mutation.ColumnName)
	if column == nil {
		return columnNotFound(mutation)
	}
	if !b.sameName(mutation.ColumnName, mutation.NewName) && b.hasColumn(table.Columns, mutation.NewName) {
		return duplicateObject("column %s already exists in table %s", mutation.NewName, table.Name)
	}
	if !b.sameName(mutation.ColumnName, mutation.NewName) {
		if err := b.refuseCheckedColumn(table, mutation.ColumnName, false); err != nil {
			return err
		}
	}
	column.Name = mutation.NewName
	b.renameKeyColumns(table, mutation.ColumnName, mutation.NewName)
	b.propagateColumnChange(table, mutation, func(inherited *sqlmodel.Column) {
		inherited.Name = mutation.NewName
	})
	return nil
}

// applyAlterTableRenameTable renames a table, a view or a sequence within its schema, an
// unqualified name found along the search path. A mutation restricted to one kind of
// object renames only an object of that kind.
//
// Takes mutation (*ast.CatalogueMutation) which holds the old and new names.
//
// Returns error when no object of the kind has the old name, or the schema already holds
// one under the new name.
func (b *catalogueBuilder) applyAlterTableRenameTable(mutation *ast.CatalogueMutation) error {
	switch b.relationKind(mutation) {
	case ast.ObjectKindTable:
		return b.renameTable(mutation)
	case ast.ObjectKindView:
		return b.renameView(mutation)
	case ast.ObjectKindSequence:
		return b.moveSequence(mutation.SchemaName, mutation.TableName, "", mutation.NewName)
	default:
		return b.missingRelation(mutation)
	}
}

// applyAlterTableSetSchema moves a table, a view or a sequence from one schema to
// another, an unqualified name found along the search path.
//
// A mutation restricted to one kind of object moves only an object of that kind. Moving
// an object to the schema it is in changes nothing.
//
// Takes mutation (*ast.CatalogueMutation) which holds the object's name and the target
// schema's name.
//
// Returns error when no object of the kind has the name, or the target schema already
// holds one under it.
func (b *catalogueBuilder) applyAlterTableSetSchema(mutation *ast.CatalogueMutation) error {
	switch b.relationKind(mutation) {
	case ast.ObjectKindTable:
		return b.moveTable(mutation)
	case ast.ObjectKindView:
		return b.moveView(mutation)
	case ast.ObjectKindSequence:
		return b.moveSequence(mutation.SchemaName, mutation.TableName, mutation.NewName, "")
	default:
		return b.missingRelation(mutation)
	}
}

// relationKind returns the object kind a relation rename or move applies to.
//
// The mutation's kind restriction takes precedence. Otherwise, the kind comes from the
// named schema or the first schema on the search path holding a table, view or sequence
// of that name.
//
// Takes mutation (*ast.CatalogueMutation) which names the object.
//
// Returns ast.ObjectKind which is the kind, or ast.ObjectKindAny when no object has the
// name.
func (b *catalogueBuilder) relationKind(mutation *ast.CatalogueMutation) ast.ObjectKind {
	if mutation.TargetKind != ast.ObjectKindAny {
		return mutation.TargetKind
	}
	key := b.key(mutation.TableName)
	schema := b.schemaHolding(mutation.SchemaName, func(candidate *sqlmodel.Schema) bool {
		return candidate.Tables[key] != nil || candidate.Views[key] != nil || candidate.Sequences[key] != nil
	})
	switch {
	case schema == nil:
		return ast.ObjectKindAny
	case schema.Tables[key] != nil:
		return ast.ObjectKindTable
	case schema.Views[key] != nil:
		return ast.ObjectKindView
	default:
		return ast.ObjectKindSequence
	}
}

// missingRelation reports that a rename or a move names no table, view or sequence.
//
// Takes mutation (*ast.CatalogueMutation) which names the object.
//
// Returns error which matches errUnknownObject.
func (b *catalogueBuilder) missingRelation(mutation *ast.CatalogueMutation) error {
	return unknownObject(tableNotFoundFormat, mutation.TableName, b.searchedSchemas(mutation.SchemaName))
}

// renameTable renames a table within its schema.
//
// Takes mutation (*ast.CatalogueMutation) which names the table and its new name.
//
// Returns error when there is no such table, or the schema already holds a table or view
// under the new name.
func (b *catalogueBuilder) renameTable(mutation *ast.CatalogueMutation) error {
	key := b.key(mutation.TableName)
	schema := b.schemaHolding(mutation.SchemaName, func(candidate *sqlmodel.Schema) bool { return candidate.Tables[key] != nil })
	if schema == nil {
		return unknownObject(tableNotFoundFormat, mutation.TableName, b.searchedSchemas(mutation.SchemaName))
	}
	if b.relationTaken(schema, mutation.NewName, key) {
		return duplicateObject("relation %s already exists", qualifiedName(schema.Name, mutation.NewName))
	}
	if err := b.applyRenameToViews(mutation.TableName, mutation.NewName); err != nil {
		return err
	}
	table := schema.Tables[key]
	b.retargetForeignKeys(schema.Name, table.Name, schema.Name, mutation.NewName)
	b.renameGeneratedNames(table, mutation.NewName)
	delete(schema.Tables, key)
	table.Name = mutation.NewName
	schema.Tables[b.key(mutation.NewName)] = table
	b.restoreBrokenViews()
	return nil
}

// moveTable moves a table to another schema, creating the target schema when the
// catalogue does not hold it yet.
//
// Takes mutation (*ast.CatalogueMutation) which names the table and the target schema.
//
// Returns error when there is no such table, or the target schema already holds a table
// or view under its name.
func (b *catalogueBuilder) moveTable(mutation *ast.CatalogueMutation) error {
	key := b.key(mutation.TableName)
	source := b.schemaHolding(mutation.SchemaName, func(candidate *sqlmodel.Schema) bool { return candidate.Tables[key] != nil })
	if source == nil {
		return unknownObject(tableNotFoundFormat, mutation.TableName, b.searchedSchemas(mutation.SchemaName))
	}
	target := b.resolveSchema(mutation.NewName)
	if target == source {
		return nil
	}
	table := source.Tables[key]
	if b.relationTaken(target, table.Name, "") {
		return duplicateObject("relation %s already exists", qualifiedName(target.Name, table.Name))
	}
	b.retargetForeignKeys(source.Name, table.Name, target.Name, table.Name)
	delete(source.Tables, key)
	table.Schema = target.Name
	target.Tables[key] = table
	return nil
}

// relationTaken reports whether a schema holds a table or a view under a name, other than
// the object being renamed within it.
//
// Takes schema (*sqlmodel.Schema) which is the schema the object goes to.
// Takes name (string) which is the name it takes there.
// Takes ownKey (string) which is the key the object is filed under in that schema
// already, or empty when it comes from another schema.
//
// Returns bool which is true when another table or view has the name.
func (b *catalogueBuilder) relationTaken(schema *sqlmodel.Schema, name, ownKey string) bool {
	key := b.key(name)
	if key == ownKey {
		return false
	}
	return schema.Tables[key] != nil || schema.Views[key] != nil
}

// moveSequence renames a sequence, or moves it to another schema, as ALTER SEQUENCE or
// ALTER TABLE naming a sequence does.
//
// Takes schemaName (string) which is the sequence's schema qualifier, or empty to follow
// the search path.
// Takes name (string) which is the sequence's name.
// Takes targetSchema (string) which is the schema to move it to, or empty to keep it.
// Takes newName (string) which is its new name, or empty to keep it.
//
// Returns error when there is no such sequence, or the schema it goes to already holds a
// sequence under the name it takes.
func (b *catalogueBuilder) moveSequence(schemaName, name, targetSchema, newName string) error {
	key := b.key(name)
	source := b.schemaHolding(schemaName, func(candidate *sqlmodel.Schema) bool { return candidate.Sequences[key] != nil })
	if source == nil {
		return unknownObject("sequence %s not found in schema %s", name, b.searchedSchemas(schemaName))
	}
	sequence := source.Sequences[key]
	target := source
	if targetSchema != "" {
		target = b.resolveSchema(targetSchema)
	}
	finalName := cmp.Or(newName, sequence.Name)
	ownKey := ""
	if target == source {
		ownKey = key
	}
	if finalKey := b.key(finalName); finalKey != ownKey && target.Sequences[finalKey] != nil {
		return duplicateObject("sequence %s already exists", qualifiedName(target.Name, finalName))
	}
	delete(source.Sequences, key)
	sequence.Name = finalName
	sequence.Schema = target.Name
	target.Sequences[b.key(finalName)] = sequence
	return nil
}

// applyCreateSequence adds a new sequence to the catalogue, with the columns a query
// reading it as a relation sees, unless the statement said IF NOT EXISTS and the sequence
// exists.
//
// Takes mutation (*ast.CatalogueMutation) which holds the sequence definition.
//
// Returns error (always nil).
func (b *catalogueBuilder) applyCreateSequence(mutation *ast.CatalogueMutation) error {
	schema := b.resolveSchema(mutation.SchemaName)
	if mutation.IfNotExists && schema.Sequences[b.key(mutation.SequenceName)] != nil {
		return nil
	}
	columns := cloneColumns(mutation.Columns)
	for index := range columns {
		columns[index].Origin = mutation.Origin
	}
	schema.Sequences[b.key(mutation.SequenceName)] = &sqlmodel.Sequence{
		Name:          mutation.SequenceName,
		Schema:        schema.Name,
		OwnedByTable:  mutation.OwnedByTable,
		OwnedByColumn: mutation.OwnedByColumn,
		Columns:       columns,
		Origin:        mutation.Origin,
	}
	return nil
}

// applyDropSequence removes a sequence from the catalogue.
//
// Takes mutation (*ast.CatalogueMutation) which identifies the sequence to drop.
//
// Returns error when no sequence of the name exists and the statement did not say IF
// EXISTS.
func (b *catalogueBuilder) applyDropSequence(mutation *ast.CatalogueMutation) error {
	key := b.key(mutation.SequenceName)
	schema := b.schemaHolding(mutation.SchemaName, func(candidate *sqlmodel.Schema) bool { return candidate.Sequences[key] != nil })
	if schema == nil {
		return missingDropTarget(mutation, "sequence %s not found in schema %s", mutation.SequenceName, b.searchedSchemas(mutation.SchemaName))
	}
	delete(schema.Sequences, key)
	return nil
}

// applyAlterTableAddConstraint appends new constraints to an existing table, and records
// the primary key an ADD PRIMARY KEY declares.
//
// Takes mutation (*ast.CatalogueMutation) which holds the constraints to add.
//
// Returns error when the target table is not found.
func (b *catalogueBuilder) applyAlterTableAddConstraint(mutation *ast.CatalogueMutation) error {
	table, err := b.findTable(mutation.SchemaName, mutation.TableName)
	if err != nil {
		return err
	}
	added, err := b.newNotNull(table, slices.Clone(mutation.Constraints))
	if err != nil {
		return err
	}
	if err := b.refuseDuplicateKeyNames(table, added); err != nil {
		return err
	}
	for index := range added {
		added[index].Origin = mutation.Origin
	}
	if len(mutation.PrimaryKey) > 0 {
		added = b.withPrimaryKeyConstraint(added, mutation.PrimaryKey, primaryKeyName(table))
		if mutation.ReplacesHiddenKey {
			b.dropHiddenKey(table, mutation.PrimaryKey)
		}
		dropPrimaryKey(table)
	}
	from := len(table.Constraints)
	table.Constraints = append(slices.Clip(table.Constraints), added...)
	b.keepNotNull(table, added)
	if len(mutation.PrimaryKey) > 0 {
		table.PrimaryKey = mutation.PrimaryKey
		if b.mysqlFamily() {
			b.dropImplicitIndexes(table, mutation.PrimaryKey)
		}
	}
	b.nameConstraints(table, from, false)
	if len(mutation.PrimaryKey) > 0 {
		b.propagatePrimaryKey(table)
	}
	return nil
}

// dropHiddenKey drops the column a table is keyed by when that key is a single column the
// table hides and the table is given another key, as CockroachDB drops the rowid it keys
// a table created without a key by.
//
// Takes table (*sqlmodel.Table) which is the table.
// Takes primaryKey ([]string) which is the key the table is given.
func (b *catalogueBuilder) dropHiddenKey(table *sqlmodel.Table, primaryKey []string) {
	if len(table.PrimaryKey) != 1 || slices.ContainsFunc(primaryKey, func(name string) bool { return b.sameName(name, table.PrimaryKey[0]) }) {
		return
	}
	table.Columns = slices.DeleteFunc(slices.Clone(table.Columns), func(column sqlmodel.Column) bool {
		return column.Hidden && b.sameName(column.Name, table.PrimaryKey[0])
	})
}

// applyAlterTableDropConstraint removes a named constraint from an existing table, and
// the primary key when the constraint is the key or the mutation drops the key.
//
// Uses a freshly allocated slice rather than the in-place `table.Constraints[:0]`
// pattern, matching applyAlterTableDropColumn so both drop paths behave the same way. The
// in-place form would otherwise leak references to dropped constraints through the
// underlying array.
//
// Takes mutation (*ast.CatalogueMutation) which identifies the constraint to drop.
//
// Returns error when the target table is not found.
func (b *catalogueBuilder) applyAlterTableDropConstraint(mutation *ast.CatalogueMutation) error {
	table, err := b.findTable(mutation.SchemaName, mutation.TableName)
	if err != nil {
		return err
	}
	if mutation.DropsPrimaryKey {
		dropPrimaryKey(table)
		return nil
	}
	if err := b.refuseKeyNotNullDrop(table, mutation.ConstraintName); err != nil {
		return err
	}
	if mutation.RefusesUnknownConstraint {
		if handled, err := b.dropDeclaredName(table, mutation.ConstraintName); handled || err != nil {
			return err
		}
	}
	filtered := make([]sqlmodel.Constraint, 0, len(table.Constraints))
	for _, constraint := range table.Constraints {
		switch {
		case !b.sameName(constraint.Name, mutation.ConstraintName), b.keepsColumnCheck(&constraint):
			filtered = append(filtered, constraint)
		case constraint.Kind == sqlmodel.ConstraintPrimaryKey:
			table.PrimaryKey = nil
		case constraint.Kind == sqlmodel.ConstraintNotNull:
			b.allowNull(table, constraint.Columns)
		}
	}
	if len(filtered) == len(table.Constraints) {
		if column := b.defaultNotNullColumn(table, mutation.ConstraintName); column != "" {
			b.allowNull(table, []string{column})
		}
		b.dropUniqueIndexConstraint(table, mutation.ConstraintName)
	}
	table.Constraints = filtered
	return nil
}

// dropUniqueIndexConstraint drops the unique index DROP CONSTRAINT names on CockroachDB,
// which since 26.4 lists every unique index as a unique constraint.
//
// Takes table (*sqlmodel.Table) which holds the index.
// Takes name (string) which is the name dropped.
func (b *catalogueBuilder) dropUniqueIndexConstraint(table *sqlmodel.Table, name string) {
	if b.constraintNames != dialect.ConstraintNamesCockroachDB {
		return
	}
	table.Indexes = slices.DeleteFunc(slices.Clone(table.Indexes), func(index sqlmodel.Index) bool {
		return index.IsUnique && !index.IsPrimary && b.sameName(index.Name, name)
	})
}

// declaredNames are the names a SQLite table's definitions give its keys, which DROP
// CONSTRAINT refuses to drop, and its columns' DEFAULT and COLLATE clauses, which DROP
// CONSTRAINT drops, leaving the clause as it was.
type declaredNames struct {
	// keys holds the primary key and unique constraint names.
	keys []string

	// attributes holds the DEFAULT and COLLATE names.
	attributes []string
}

// declareNames records the key and attribute names a CREATE TABLE or ADD COLUMN gives.
//
// Takes table (*sqlmodel.Table) which is the table.
// Takes mutation (*ast.CatalogueMutation) which holds the names.
func (b *catalogueBuilder) declareNames(table *sqlmodel.Table, mutation *ast.CatalogueMutation) {
	if b.declaredNames == nil || len(mutation.KeyNames)+len(mutation.AttributeNames) == 0 {
		return
	}
	names := b.declaredNames[table]
	if names == nil {
		names = &declaredNames{keys: nil, attributes: nil}
		b.declaredNames[table] = names
	}
	names.keys = append(names.keys, mutation.KeyNames...)
	names.attributes = append(names.attributes, mutation.AttributeNames...)
}

// dropDeclaredName applies a SQLite DROP CONSTRAINT to the names the catalogue holds no
// constraint for: it refuses a key's name or a unique constraint's, drops an attribute's
// name, and refuses a name the table does not have, as SQLite 3.54 does.
//
// Takes table (*sqlmodel.Table) which is the table.
// Takes name (string) which is the name dropped.
//
// Returns bool which is true when the name was dealt with here.
// Returns error when SQLite refuses the drop.
func (b *catalogueBuilder) dropDeclaredName(table *sqlmodel.Table, name string) (bool, error) {
	names := b.declaredNames[table]
	isKey := func(candidate string) bool { return b.sameName(candidate, name) }
	if names != nil && slices.ContainsFunc(names.keys, isKey) ||
		slices.ContainsFunc(table.Constraints, func(constraint sqlmodel.Constraint) bool {
			return b.sameName(constraint.Name, name) &&
				(constraint.Kind == sqlmodel.ConstraintUnique || constraint.Kind == sqlmodel.ConstraintPrimaryKey)
		}) {
		return true, refused(errKeyConstraintDrop, "constraint %s of table %s is a primary key or unique constraint, which "+
			"cannot be dropped", name, table.Name)
	}
	if slices.ContainsFunc(table.Constraints, func(constraint sqlmodel.Constraint) bool { return b.sameName(constraint.Name, name) }) {
		return false, nil
	}
	if names != nil && slices.ContainsFunc(names.attributes, isKey) {
		names.attributes = slices.DeleteFunc(names.attributes, isKey)
		return true, nil
	}
	return true, unknownObject("constraint %s not found in table %s", name, table.Name)
}

// keepKeyColumnNotNull keeps a primary key's column NOT NULL when MODIFY or CHANGE
// redefines it, as MariaDB does with a definition that says NULL.
//
// Takes table (*sqlmodel.Table) which holds the key.
// Takes altered (*sqlmodel.Column) which is the new definition, changed in place.
// Takes name (string) which is the column's name before the change.
func (b *catalogueBuilder) keepKeyColumnNotNull(table *sqlmodel.Table, altered *sqlmodel.Column, name string) {
	if b.mysqlFamily() && b.inPrimaryKey(table, name) {
		altered.Nullable = false
	}
}

// refuseCheckedColumn refuses renaming a column a MySQL check reads, or dropping one a
// check reads with another column, as MySQL refuses both with error 3959. Dropping the
// only column a check reads drops the check.
//
// Takes table (*sqlmodel.Table) which holds the checks.
// Takes column (string) which is the column renamed or dropped.
// Takes dropping (bool) which is true for a drop.
//
// Returns error when a check holds on to the column.
func (b *catalogueBuilder) refuseCheckedColumn(table *sqlmodel.Table, column string, dropping bool) error {
	if b.constraintNames != dialect.ConstraintNamesMySQL || column == "" {
		return nil
	}
	for _, constraint := range table.Constraints {
		if constraint.Kind != sqlmodel.ConstraintCheck ||
			!slices.ContainsFunc(constraint.Columns, func(name string) bool { return b.sameName(name, column) }) {
			continue
		}
		if dropping && len(constraint.Columns) == 1 {
			continue
		}
		return refused(errCheckUsesColumn, "check constraint %s of table %s uses column %s, so it cannot be dropped or renamed",
			constraint.Name, table.Name, column)
	}
	return nil
}

// keepNotNull keeps the columns NOT NULL constraints name from holding NULL.
//
// Takes table (*sqlmodel.Table) which holds the columns.
// Takes constraints ([]sqlmodel.Constraint) which are the constraints added.
func (b *catalogueBuilder) keepNotNull(table *sqlmodel.Table, constraints []sqlmodel.Constraint) {
	for _, constraint := range constraints {
		if constraint.Kind != sqlmodel.ConstraintNotNull {
			continue
		}
		for index := range table.Columns {
			if slices.ContainsFunc(constraint.Columns, func(name string) bool { return b.sameName(name, table.Columns[index].Name) }) {
				table.Columns[index].Nullable = false
			}
		}
	}
}

// newNotNull leaves out of the constraints ALTER TABLE adds a NOT NULL constraint on a
// column that holds no NULL already, under the same name or none, and refuses one under
// another name, as PostgreSQL does.
//
// Takes table (*sqlmodel.Table) which holds the columns.
// Takes added ([]sqlmodel.Constraint) which are the constraints the statement adds.
//
// Returns []sqlmodel.Constraint which holds the constraints to add.
// Returns error when a NOT NULL constraint renames the one a column has.
func (b *catalogueBuilder) newNotNull(table *sqlmodel.Table, added []sqlmodel.Constraint) ([]sqlmodel.Constraint, error) {
	if b.constraintNames != dialect.ConstraintNamesPostgres {
		return added, nil
	}
	kept := added[:0]
	for _, constraint := range added {
		var column *sqlmodel.Column
		if constraint.Kind == sqlmodel.ConstraintNotNull && len(constraint.Columns) == 1 {
			column = b.namedColumn(table.Columns, constraint.Columns[0])
		}
		if column == nil || column.Nullable {
			kept = append(kept, constraint)
			continue
		}
		if existing := b.notNullName(table, column); constraint.Name != "" && !b.sameName(existing, constraint.Name) {
			return nil, refused(errNotNullTaken, "column %s of table %s has the NOT NULL constraint %s, so %s cannot be added",
				column.Name, table.Name, existing, constraint.Name)
		}
	}
	return kept, nil
}

// notNullName returns the name of the NOT NULL constraint that keeps a column from
// holding NULL on PostgreSQL: the name a migration gave it, or the one PostgreSQL 18
// generates.
//
// Takes table (*sqlmodel.Table) which holds the column.
// Takes column (*sqlmodel.Column) which is the column, which holds no NULL.
//
// Returns string which is the name.
func (b *catalogueBuilder) notNullName(table *sqlmodel.Table, column *sqlmodel.Column) string {
	for _, constraint := range table.Constraints {
		if constraint.Kind == sqlmodel.ConstraintNotNull && len(constraint.Columns) == 1 && b.sameName(constraint.Columns[0], column.Name) {
			return constraint.Name
		}
	}
	return b.postgresNotNullName(table, column.Name)
}

// defaultNotNullColumn finds the column whose unnamed NOT NULL constraint PostgreSQL 18
// names as given, so DROP CONSTRAINT of that name lets the column hold NULL.
//
// Takes table (*sqlmodel.Table) which holds the columns.
// Takes name (string) which is the constraint's name.
//
// Returns string which is the column's name, or empty on another engine or when no
// column's constraint has the name.
func (b *catalogueBuilder) defaultNotNullColumn(table *sqlmodel.Table, name string) string {
	if b.constraintNames != dialect.ConstraintNamesPostgres {
		return ""
	}
	for index := range table.Columns {
		column := &table.Columns[index]
		if !column.Nullable && b.sameName(b.notNullName(table, column), name) {
			return column.Name
		}
	}
	return ""
}

// refuseKeyNotNullDrop refuses dropping the NOT NULL constraint of a primary key's
// column, which PostgreSQL refuses as the column is in a primary key.
//
// Takes table (*sqlmodel.Table) which holds the constraint.
// Takes name (string) which is the constraint dropped.
//
// Returns error when the constraint keeps a key column from holding NULL.
func (b *catalogueBuilder) refuseKeyNotNullDrop(table *sqlmodel.Table, name string) error {
	if b.constraintNames != dialect.ConstraintNamesPostgres {
		return nil
	}
	for index := range table.Columns {
		column := &table.Columns[index]
		if !column.Nullable && b.inPrimaryKey(table, column.Name) && b.sameName(b.notNullName(table, column), name) {
			return refused(errKeyColumnNotNull, "column %s of table %s is in a primary key, so its NOT NULL constraint %s cannot be dropped",
				column.Name, table.Name, name)
		}
	}
	return nil
}

// inPrimaryKey reports whether a column is one of a table's primary key columns.
//
// Takes table (*sqlmodel.Table) which is the table.
// Takes column (string) which is the column's name.
//
// Returns bool which is true when the key holds it.
func (b *catalogueBuilder) inPrimaryKey(table *sqlmodel.Table, column string) bool {
	return slices.ContainsFunc(table.PrimaryKey, func(name string) bool { return b.sameName(name, column) })
}

// allowNull lets columns hold NULL, as dropping the NOT NULL constraint that kept them
// from it does.
//
// Takes table (*sqlmodel.Table) which holds the columns.
// Takes names ([]string) which name the columns.
func (b *catalogueBuilder) allowNull(table *sqlmodel.Table, names []string) {
	for index := range table.Columns {
		if slices.ContainsFunc(names, func(name string) bool { return b.sameName(name, table.Columns[index].Name) }) {
			table.Columns[index].Nullable = true
		}
	}
}

// keepsColumnCheck reports whether DROP CONSTRAINT leaves a check alone, as MariaDB does
// with a check a column declares, which only dropping or redefining the column removes.
//
// Takes constraint (*sqlmodel.Constraint) which is the constraint named.
//
// Returns bool which is true for a MariaDB column check.
func (b *catalogueBuilder) keepsColumnCheck(constraint *sqlmodel.Constraint) bool {
	return b.constraintNames == dialect.ConstraintNamesMariaDB && constraint.Kind == sqlmodel.ConstraintCheck && len(constraint.Columns) > 0
}

// applyComment records or removes comments on catalogue tables, views, columns and enums.
// Unsupported or missing objects are ignored.
//
// Takes mutation (*ast.CatalogueMutation) which names the object and holds the comment.
//
// Returns error (always nil).
func (b *catalogueBuilder) applyComment(mutation *ast.CatalogueMutation) error {
	switch {
	case mutation.EnumName != "":
		if enum, err := b.findEnum(mutation); err == nil {
			enum.Comment = mutation.Comment
		}
	case mutation.TableName != "":
		b.commentOnRelation(mutation)
	}
	return nil
}

// commentOnRelation records a comment on the table or view a mutation names, or on the
// column of it the mutation names.
//
// Takes mutation (*ast.CatalogueMutation) which names the relation, and the column when
// it comments on one.
func (b *catalogueBuilder) commentOnRelation(mutation *ast.CatalogueMutation) {
	if table, err := b.findTable(mutation.SchemaName, mutation.TableName); err == nil {
		if mutation.ColumnName == "" {
			table.Comment = mutation.Comment
			return
		}
		if column := b.namedColumn(table.Columns, mutation.ColumnName); column != nil {
			column.Comment = mutation.Comment
		}
		return
	}
	_, view := b.findView(mutation.SchemaName, mutation.TableName)
	if view == nil {
		return
	}
	if mutation.ColumnName == "" {
		view.Comment = mutation.Comment
		return
	}
	if column := b.namedColumn(view.Columns, mutation.ColumnName); column != nil {
		column.Comment = mutation.Comment
	}
}

// namedColumn finds a column by name, as the dialect compares names.
//
// Takes columns ([]sqlmodel.Column) which are a relation's columns.
// Takes name (string) which is the column's name.
//
// Returns *sqlmodel.Column which aliases the column, or nil when there is none.
func (b *catalogueBuilder) namedColumn(columns []sqlmodel.Column, name string) *sqlmodel.Column {
	for index := range columns {
		if b.sameName(columns[index].Name, name) {
			return &columns[index]
		}
	}
	return nil
}

// columnNotFound reports that an ALTER TABLE names a column its table does not have.
//
// Takes mutation (*ast.CatalogueMutation) which names the table and the column.
//
// Returns error which describes the missing column.
func columnNotFound(mutation *ast.CatalogueMutation) error {
	return unknownObject("column %s not found in table %s", mutation.ColumnName, mutation.TableName)
}

// keepUnstated gives a redefined column the comment, and the default or generation
// expression, of the column it replaces where its definition states none.
//
// Takes altered (*sqlmodel.Column) which is the new definition.
// Takes previous (*sqlmodel.Column) which is the column it replaces.
func keepUnstated(altered, previous *sqlmodel.Column) {
	if altered.Comment == "" {
		altered.Comment = previous.Comment
	}
	if altered.HasDefault || altered.IsGenerated {
		return
	}
	altered.HasDefault = previous.HasDefault
	altered.IsGenerated = previous.IsGenerated
	altered.GeneratedKind = previous.GeneratedKind
	altered.Hidden = previous.Hidden
}

// changesColumn reports whether an ALTER TABLE mutation changes a column rather than
// annotating the relation itself.
//
// Takes mutation (*ast.CatalogueMutation) which is the mutation.
//
// Returns bool which is true when it names, adds or alters a column.
func changesColumn(mutation *ast.CatalogueMutation) bool {
	if len(mutation.Columns) > 0 || mutation.ColumnName != "" {
		return true
	}
	return mutation.SetsNotNull || mutation.DropsNotNull || mutation.SetsType || mutation.SetsDefault || mutation.DropsDefault
}

// dropColumnConstraints removes the constraints of a kind that constrain only columns a
// change drops or redefines. One naming no columns is kept, since what it reads is not
// known.
//
// Takes table (*sqlmodel.Table) which holds the constraints.
// Takes kind (sqlmodel.ConstraintKind) which is the kind removed.
// Takes dropped (func(string) bool) which reports whether a column goes.
func dropColumnConstraints(table *sqlmodel.Table, kind sqlmodel.ConstraintKind, dropped func(string) bool) {
	table.Constraints = slices.DeleteFunc(slices.Clone(table.Constraints), func(constraint sqlmodel.Constraint) bool {
		return constraint.Kind == kind && len(constraint.Columns) > 0 && !slices.ContainsFunc(constraint.Columns, func(column string) bool {
			return !dropped(column)
		})
	})
}
