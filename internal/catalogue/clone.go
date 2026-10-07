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

	"seagull.sh/seagull/internal/sqlmodel"
)

// Clone returns a deep copy of a catalogue for independent modification.
//
// Maps, slices and pointers are not shared with the original.
//
// Takes catalogue (*sqlmodel.Catalogue) which is the catalogue to copy; nil gives nil.
//
// Returns *sqlmodel.Catalogue which is the copy.
func Clone(catalogue *sqlmodel.Catalogue) *sqlmodel.Catalogue {
	if catalogue == nil {
		return nil
	}
	copied := *catalogue
	copied.SearchPath = slices.Clone(catalogue.SearchPath)
	copied.Extensions = maps.Clone(catalogue.Extensions)
	copied.Schemas = clonePointerMap(catalogue.Schemas, cloneSchema)
	return &copied
}

// cloneSchema returns a deep copy of a schema.
//
// Takes schema (*sqlmodel.Schema) which is the schema to copy.
//
// Returns *sqlmodel.Schema which is the copy.
func cloneSchema(schema *sqlmodel.Schema) *sqlmodel.Schema {
	copied := *schema
	copied.Tables = clonePointerMap(schema.Tables, cloneTable)
	copied.Views = clonePointerMap(schema.Views, cloneView)
	copied.Enums = clonePointerMap(schema.Enums, cloneEnum)
	copied.CompositeTypes = clonePointerMap(schema.CompositeTypes, cloneCompositeType)
	copied.Sequences = clonePointerMap(schema.Sequences, cloneSequence)
	copied.Domains = clonePointerMap(schema.Domains, cloneDomain)
	if schema.Functions != nil {
		copied.Functions = make(map[string][]*sqlmodel.Function, len(schema.Functions))
		for name, overloads := range schema.Functions {
			copied.Functions[name] = cloneEach(overloads, cloneFunction)
		}
	}
	return &copied
}

// cloneTable returns a deep copy of a table.
//
// Takes table (*sqlmodel.Table) which is the table to copy.
//
// Returns *sqlmodel.Table which is the copy.
func cloneTable(table *sqlmodel.Table) *sqlmodel.Table {
	copied := *table
	copied.Columns = cloneColumns(table.Columns)
	copied.PrimaryKey = slices.Clone(table.PrimaryKey)
	copied.Indexes = cloneValues(table.Indexes, func(index sqlmodel.Index) sqlmodel.Index {
		index.Columns = slices.Clone(index.Columns)
		return index
	})
	copied.Constraints = cloneValues(table.Constraints, func(constraint sqlmodel.Constraint) sqlmodel.Constraint {
		constraint.Columns = slices.Clone(constraint.Columns)
		constraint.ForeignColumns = slices.Clone(constraint.ForeignColumns)
		return constraint
	})
	if table.PartitionOf != nil {
		copied.PartitionOf = new(*table.PartitionOf)
	}
	return &copied
}

// cloneView returns a deep copy of a view.
//
// Takes view (*sqlmodel.View) which is the view to copy.
//
// Returns *sqlmodel.View which is the copy.
func cloneView(view *sqlmodel.View) *sqlmodel.View {
	copied := *view
	copied.Columns = cloneColumns(view.Columns)
	return &copied
}

// cloneEnum returns a deep copy of an enum.
//
// Takes enum (*sqlmodel.Enum) which is the enum to copy.
//
// Returns *sqlmodel.Enum which is the copy.
func cloneEnum(enum *sqlmodel.Enum) *sqlmodel.Enum {
	copied := *enum
	copied.Values = slices.Clone(enum.Values)
	return &copied
}

// cloneCompositeType returns a deep copy of a composite type.
//
// Takes composite (*sqlmodel.CompositeType) which is the type to copy.
//
// Returns *sqlmodel.CompositeType which is the copy.
func cloneCompositeType(composite *sqlmodel.CompositeType) *sqlmodel.CompositeType {
	copied := *composite
	copied.Fields = cloneColumns(composite.Fields)
	return &copied
}

// cloneSequence returns a deep copy of a sequence and its columns.
//
// Takes sequence (*sqlmodel.Sequence) which is the sequence to copy.
//
// Returns *sqlmodel.Sequence which is the copy.
func cloneSequence(sequence *sqlmodel.Sequence) *sqlmodel.Sequence {
	copied := *sequence
	copied.Columns = cloneColumns(sequence.Columns)
	return &copied
}

// cloneDomain returns a deep copy of a domain.
//
// Takes domain (*sqlmodel.Domain) which is the domain to copy.
//
// Returns *sqlmodel.Domain which is the copy.
func cloneDomain(domain *sqlmodel.Domain) *sqlmodel.Domain {
	copied := *domain
	copied.BaseType = cloneSQLType(domain.BaseType)
	return &copied
}

// cloneFunction returns a deep copy of a function.
//
// Takes function (*sqlmodel.Function) which is the function to copy.
//
// Returns *sqlmodel.Function which is the copy.
func cloneFunction(function *sqlmodel.Function) *sqlmodel.Function {
	copied := *function
	copied.ReturnType = cloneSQLType(function.ReturnType)
	copied.Arguments = cloneValues(function.Arguments, func(argument sqlmodel.FunctionArgument) sqlmodel.FunctionArgument {
		argument.Type = cloneSQLType(argument.Type)
		return argument
	})
	return &copied
}

// cloneColumns returns a deep copy of a column list.
//
// Takes columns ([]sqlmodel.Column) which are the columns to copy.
//
// Returns []sqlmodel.Column which is the copy, nil for nil.
func cloneColumns(columns []sqlmodel.Column) []sqlmodel.Column {
	if columns == nil {
		return nil
	}
	copied := make([]sqlmodel.Column, len(columns))
	for index := range columns {
		copied[index] = cloneColumn(&columns[index])
	}
	return copied
}

// cloneColumn returns a deep copy of a column, sharing none of its type, Go type or
// nullability override with the original.
//
// Takes column (*sqlmodel.Column) which is the column to copy.
//
// Returns sqlmodel.Column which is the copy.
func cloneColumn(column *sqlmodel.Column) sqlmodel.Column {
	copied := *column
	copied.SQLType = cloneSQLType(column.SQLType)
	copied.GoTypeOverride = cloneGoType(column.GoTypeOverride)
	if column.NullableOverride != nil {
		copied.NullableOverride = new(*column.NullableOverride)
	}
	if column.NonNullElements != nil {
		copied.NonNullElements = new(*column.NonNullElements)
	}
	copied.StructTags = maps.Clone(column.StructTags)
	return copied
}

// cloneSQLType returns a deep copy of a SQL type and every type nested in it, down to
// sqlmodel.MaxTypeDepth levels.
//
// Takes sqlType (sqlmodel.SQLType) which is the type to copy.
//
// Returns sqlmodel.SQLType which is the copy.
func cloneSQLType(sqlType sqlmodel.SQLType) sqlmodel.SQLType {
	return cloneSQLTypeAt(sqlType, 0)
}

// cloneSQLTypeAt copies a SQL type nested depth levels deep.
//
// At sqlmodel.MaxTypeDepth, the copy shares deeper types with the original. Type walks
// stop at this limit, so they cannot modify the shared types. Depth validation can still
// recognise the type as too deep.
//
// Takes sqlType (sqlmodel.SQLType) which is the type to copy.
// Takes depth (int) which is how deeply the type is nested.
//
// Returns sqlmodel.SQLType which is the copy.
func cloneSQLTypeAt(sqlType sqlmodel.SQLType, depth int) sqlmodel.SQLType {
	sqlType.Precision = cloneInteger(sqlType.Precision)
	sqlType.Scale = cloneInteger(sqlType.Scale)
	sqlType.Length = cloneInteger(sqlType.Length)
	sqlType.EnumValues = slices.Clone(sqlType.EnumValues)
	if depth >= sqlmodel.MaxTypeDepth {
		return sqlType
	}
	sqlType.ElementType = cloneSQLTypePointer(sqlType.ElementType, depth+1)
	sqlType.KeyType = cloneSQLTypePointer(sqlType.KeyType, depth+1)
	sqlType.StructFields = cloneValues(sqlType.StructFields, func(field sqlmodel.StructField) sqlmodel.StructField {
		field.SQLType = cloneSQLTypeAt(field.SQLType, depth+1)
		return field
	})
	sqlType.UnionMembers = cloneValues(sqlType.UnionMembers, func(member sqlmodel.UnionMember) sqlmodel.UnionMember {
		member.SQLType = cloneSQLTypeAt(member.SQLType, depth+1)
		return member
	})
	return sqlType
}

// cloneSQLTypePointer returns a pointer to a deep copy of a SQL type nested depth levels
// deep.
//
// Takes sqlType (*sqlmodel.SQLType) which is the type to copy; nil gives nil.
// Takes depth (int) which is how deeply the type is nested.
//
// Returns *sqlmodel.SQLType which points at the copy.
func cloneSQLTypePointer(sqlType *sqlmodel.SQLType, depth int) *sqlmodel.SQLType {
	if sqlType == nil {
		return nil
	}
	return new(cloneSQLTypeAt(*sqlType, depth))
}

// cloneGoType returns a pointer to a deep copy of a Go type and every type nested in it,
// down to sqlmodel.MaxTypeDepth levels.
//
// Takes goType (*sqlmodel.GoType) which is the type to copy; nil gives nil.
//
// Returns *sqlmodel.GoType which points at the copy.
func cloneGoType(goType *sqlmodel.GoType) *sqlmodel.GoType {
	return cloneGoTypeAt(goType, 0)
}

// cloneGoTypeAt copies a Go type nested depth levels deep. A type at
// sqlmodel.MaxTypeDepth is copied without its composite parts, which it then shares with
// the original, as cloneSQLTypeAt does for a SQL type.
//
// Takes goType (*sqlmodel.GoType) which is the type to copy; nil gives nil.
// Takes depth (int) which is how deeply the type is nested.
//
// Returns *sqlmodel.GoType which points at the copy.
func cloneGoTypeAt(goType *sqlmodel.GoType, depth int) *sqlmodel.GoType {
	if goType == nil {
		return nil
	}
	copied := *goType
	if goType.Composite != nil && depth < sqlmodel.MaxTypeDepth {
		composite := *goType.Composite
		composite.Key = cloneGoTypeAt(goType.Composite.Key, depth+1)
		composite.Value = cloneGoTypeAt(goType.Composite.Value, depth+1)
		composite.Fields = cloneValues(goType.Composite.Fields, func(field sqlmodel.GoField) sqlmodel.GoField {
			field.Type = *cloneGoTypeAt(&field.Type, depth+1)
			return field
		})
		composite.Arguments = cloneValues(goType.Composite.Arguments, func(argument sqlmodel.GoType) sqlmodel.GoType {
			return *cloneGoTypeAt(&argument, depth+1)
		})
		copied.Composite = &composite
	}
	return &copied
}

// cloneInteger returns a pointer to a copy of an integer.
//
// Takes value (*int) which is the integer to copy; nil gives nil.
//
// Returns *int which points at the copy.
func cloneInteger(value *int) *int {
	if value == nil {
		return nil
	}
	return new(*value)
}

// clonePointerMap returns a copy of a map whose values are each deep-copied.
//
// Takes objects (map[string]*T) which is the map to copy; nil gives nil.
// Takes cloneOne (func(*T) *T) which copies one value.
//
// Returns map[string]*T which is the copy; a nil value stays nil.
func clonePointerMap[T any](objects map[string]*T, cloneOne func(*T) *T) map[string]*T {
	if objects == nil {
		return nil
	}
	copied := make(map[string]*T, len(objects))
	for key, object := range objects {
		if object == nil {
			copied[key] = nil
			continue
		}
		copied[key] = cloneOne(object)
	}
	return copied
}

// cloneEach returns a copy of a slice of pointers whose values are each deep-copied.
//
// Takes objects ([]*T) which is the slice to copy; nil gives nil.
// Takes cloneOne (func(*T) *T) which copies one value.
//
// Returns []*T which is the copy; a nil element stays nil.
func cloneEach[T any](objects []*T, cloneOne func(*T) *T) []*T {
	if objects == nil {
		return nil
	}
	copied := make([]*T, len(objects))
	for index, object := range objects {
		if object != nil {
			copied[index] = cloneOne(object)
		}
	}
	return copied
}

// cloneValues returns a copy of a slice whose elements are each copied by cloneOne.
//
// Takes values ([]T) which is the slice to copy; nil gives nil.
// Takes cloneOne (func(T) T) which copies one element.
//
// Returns []T which is the copy.
func cloneValues[T any](values []T, cloneOne func(T) T) []T {
	if values == nil {
		return nil
	}
	copied := make([]T, len(values))
	for index, value := range values {
		copied[index] = cloneOne(value)
	}
	return copied
}
