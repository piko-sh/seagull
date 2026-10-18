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
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

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

	columns, parents, local := b.createdTableColumns(ctx, mutation)
	for index := range columns {
		columns[index].Origin = mutation.Origin
		b.resolveCustomColumnType(&columns[index])
	}
	primaryKey, constraints := b.copiedKeys(mutation)
	if len(mutation.PrimaryKey) > 0 {
		primaryKey = mutation.PrimaryKey
	}
	constraints = append(constraints, mutation.Constraints...)
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
		Indexes:           nil,
	}
	schema.Tables[b.key(mutation.TableName)] = table
	b.recordInheritance(table, parents, local)
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
// it. A name that only a view holds leaves the view in place.
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
			return nil
		}
		return missingDropTarget(mutation, tableNotFoundFormat, mutation.TableName, b.searchedSchemas(mutation.SchemaName))
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
	prefix := b.key(mutation.DroppedColumnPrefix)
	filtered := make([]sqlmodel.Column, 0, len(table.Columns))
	for index := range table.Columns {
		dropped := b.sameName(table.Columns[index].Name, mutation.ColumnName) ||
			(prefix != "" && strings.HasPrefix(b.key(table.Columns[index].Name), prefix))
		if !dropped {
			filtered = append(filtered, table.Columns[index])
		}
	}
	if len(filtered) == len(table.Columns) {
		return unknownObject("column %s not found in table %s", mutation.ColumnName, table.Name)
	}
	table.Columns = filtered
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
	if len(mutation.Columns) == 0 {
		return nil
	}
	if mutation.SetsType {
		return b.applyColumnType(table, mutation)
	}
	altered := mutation.Columns[0]
	b.resolveCustomColumnType(&altered)
	for index := range table.Columns {
		if b.sameName(table.Columns[index].Name, mutation.ColumnName) {
			altered.Origin = table.Columns[index].Origin
			table.Columns[index] = altered
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
	if len(mutation.Columns) > 0 || mutation.ColumnName != "" || mutation.SetsNotNull || mutation.DropsNotNull || mutation.SetsType {
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
			table.Columns[index].Nullable = mutation.DropsNotNull
			b.propagateColumnChange(table, mutation, func(column *sqlmodel.Column) {
				column.Nullable = mutation.DropsNotNull
			})
			return nil
		}
	}
	return columnNotFound(mutation)
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
	column.Name = mutation.NewName
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
	table := schema.Tables[key]
	delete(schema.Tables, key)
	table.Name = mutation.NewName
	schema.Tables[b.key(mutation.NewName)] = table
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
	for index := range mutation.Constraints {
		mutation.Constraints[index].Origin = mutation.Origin
	}
	table.Constraints = append(table.Constraints, mutation.Constraints...)
	if len(mutation.PrimaryKey) > 0 {
		if mutation.ReplacesHiddenKey {
			b.dropHiddenKey(table, mutation.PrimaryKey)
		}
		table.PrimaryKey = mutation.PrimaryKey
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

// applyAlterTableDropConstraint removes a named constraint from an existing table.
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
	filtered := make([]sqlmodel.Constraint, 0, len(table.Constraints))
	for _, constraint := range table.Constraints {
		if constraint.Name != mutation.ConstraintName {
			filtered = append(filtered, constraint)
		}
	}
	table.Constraints = filtered
	return nil
}

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
	table.Indexes = append(table.Indexes, sqlmodel.Index{
		Name:      mutation.NewName,
		Origin:    mutation.Origin,
		Columns:   nil,
		IsUnique:  false,
		IsPrimary: false,
	})
	return nil
}

// applyDropIndex ignores DROP INDEX mutations; query analysis does not use index records.
//
// The mutation argument is unused.
//
// Returns error (always nil).
func (*catalogueBuilder) applyDropIndex(_ *ast.CatalogueMutation) error {
	return nil
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
