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
	"slices"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// columnMutations lists the mutation kinds that act on the one column ColumnName names,
// which IfColumnExists can make conditional.
var columnMutations = map[ast.MutationKind]struct{}{
	ast.MutationAlterTableDropColumn:   {},
	ast.MutationAlterTableAlterColumn:  {},
	ast.MutationAlterTableRenameColumn: {},
}

// skipsMissingTarget reports whether a mutation written with IF EXISTS names a table,
// view, type or column that is missing, so it changes nothing, as the database skips it.
//
// Takes mutation (*ast.CatalogueMutation) which is the mutation.
//
// Returns bool which is true when the mutation is to be skipped.
func (b *catalogueBuilder) skipsMissingTarget(mutation *ast.CatalogueMutation) bool {
	exists := b.targetExists(mutation)
	if mutation.IfExists && !exists {
		return true
	}
	if !mutation.IfColumnExists || !exists {
		return false
	}
	if _, onColumn := columnMutations[mutation.Kind]; !onColumn {
		return false
	}
	return b.catalogue.FindColumn(mutation.SchemaName, mutation.TableName, mutation.ColumnName) == nil
}

// targetExists reports whether the table, view, sequence or type a mutation acts on
// exists. A mutation restricted to one kind of object looks for an object of that kind
// alone.
//
// Takes mutation (*ast.CatalogueMutation) which is the mutation.
//
// Returns bool which is true when the object exists, or the mutation names none.
func (b *catalogueBuilder) targetExists(mutation *ast.CatalogueMutation) bool {
	if mutation.TableName != "" {
		table := func() bool { return b.catalogue.LookupTable(mutation.SchemaName, mutation.TableName) != nil }
		view := func() bool { return b.catalogue.LookupView(mutation.SchemaName, mutation.TableName) != nil }
		sequence := func() bool { return b.catalogue.LookupSequence(mutation.SchemaName, mutation.TableName) != nil }
		switch mutation.TargetKind {
		case ast.ObjectKindTable:
			return table()
		case ast.ObjectKindView:
			return view()
		case ast.ObjectKindSequence:
			return sequence()
		default:
			return table() || view() || sequence()
		}
	}
	if mutation.EnumName != "" {
		schema, _ := b.lookupType(mutation.SchemaName, mutation.EnumName)
		return schema != nil
	}
	return true
}

// placementIndex returns where a placement puts a column among a table's columns.
//
// Takes columns ([]sqlmodel.Column) which are the columns the placement is among, without
// the column placed.
// Takes placement (ast.Placement) which is the placement.
// Takes tableName (string) which names the table, for the error.
//
// Returns int which is the index the column goes at, len(columns) for the end.
// Returns error when the placement's anchor names no column.
func (b *catalogueBuilder) placementIndex(columns []sqlmodel.Column, placement ast.Placement, tableName string) (int, error) {
	switch placement.Kind {
	case ast.PlacementFirst:
		return 0, nil
	case ast.PlacementBefore, ast.PlacementAfter:
		anchor := slices.IndexFunc(columns, func(column sqlmodel.Column) bool { return b.sameName(column.Name, placement.Anchor) })
		if anchor < 0 {
			return 0, unknownObject("column %s not found in table %s", placement.Anchor, tableName)
		}
		if placement.Kind == ast.PlacementAfter {
			anchor++
		}
		return anchor, nil
	default:
		return len(columns), nil
	}
}

// moveColumn moves a table's column to where a placement puts it, leaving it in place for
// the default placement.
//
// Takes table (*sqlmodel.Table) which holds the column.
// Takes name (string) which is the column's name.
// Takes placement (ast.Placement) which says where it goes.
//
// Returns error when the column or the placement's anchor is missing.
func (b *catalogueBuilder) moveColumn(table *sqlmodel.Table, name string, placement ast.Placement) error {
	if placement.Kind == ast.PlacementDefault {
		return nil
	}
	current := slices.IndexFunc(table.Columns, func(column sqlmodel.Column) bool { return b.sameName(column.Name, name) })
	if current < 0 {
		return unknownObject("column %s not found in table %s", name, table.Name)
	}
	moved := table.Columns[current]
	remaining := slices.Delete(slices.Clone(table.Columns), current, current+1)
	index, err := b.placementIndex(remaining, placement, table.Name)
	if err != nil {
		return err
	}
	table.Columns = slices.Insert(remaining, index, moved)
	return nil
}

// insertEnumValue adds a value to an enum's values where a placement puts it.
//
// Takes enum (*sqlmodel.Enum) which is the enum.
// Takes value (string) which is the value.
// Takes placement (ast.Placement) which says where it goes.
//
// Returns error when the placement's anchor is not one of the enum's values.
func insertEnumValue(enum *sqlmodel.Enum, value string, placement ast.Placement) error {
	index := len(enum.Values)
	switch placement.Kind {
	case ast.PlacementFirst:
		index = 0
	case ast.PlacementBefore, ast.PlacementAfter:
		index = slices.Index(enum.Values, placement.Anchor)
		if index < 0 {
			return unknownObject("enum %s has no value %q", enum.Name, placement.Anchor)
		}
		if placement.Kind == ast.PlacementAfter {
			index++
		}
	default:
	}
	enum.Values = slices.Insert(slices.Clone(enum.Values), index, value)
	return nil
}

// qualifiedName joins a schema and an object name for a message, leaving out an empty
// schema.
//
// Takes schemaName (string) which is the schema's name, or empty.
// Takes name (string) which is the object's name.
//
// Returns string which is the qualified name.
func qualifiedName(schemaName, name string) string {
	if schemaName == "" {
		return name
	}
	return schemaName + "." + name
}

// missingDropTarget reports a DROP statement whose object is missing: nothing for one
// written IF EXISTS, which the database skips, and a Q068 error otherwise, as the
// database refuses it.
//
// Takes mutation (*ast.CatalogueMutation) which is the DROP mutation.
// Takes format (string) which describes the missing object.
// Takes arguments (...any) which fill format.
//
// Returns error which is nil for IF EXISTS, and otherwise wraps errUnknownObject.
func missingDropTarget(mutation *ast.CatalogueMutation, format string, arguments ...any) error {
	if mutation.IfExists {
		return nil
	}
	return unknownObject(format, arguments...)
}
