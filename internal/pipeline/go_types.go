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

package pipeline

import (
	"strings"

	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/gotypes"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// goPointerMarker starts the name of a pointer Go type.
	goPointerMarker = "*"

	// goSliceMarker starts the name of a slice Go type.
	goSliceMarker = "[]"
)

// generatedPackage names the package a generation writes.
type generatedPackage struct {
	// name is the Go package name of the generated files.
	name string

	// path is the import path of the generated package, or empty when it is not known.
	path string
}

// finishGoTypes completes the Go types a request names before it is emitted, so every
// emitter reads the same ones.
//
// A Go type an override, a type mapping or a type slot names in the generated package
// itself loses its import path, so the package never imports itself. An override of an
// array value that names a type other than a slice names the type of the array's
// elements: the value becomes a slice of it, one level for each dimension, holding
// pointers where the elements may be NULL, as an array's default Go type does.
//
// Takes request (*emit.Request) which is the request about to be emitted. Its model
// catalogue and queries are updated; an override shared with another value is replaced,
// never changed. A nil schema, table or view entry is passed over.
// Takes mappings (*gotypes.MappingTable) which is the request's type mapping, updated in
// place.
// Takes packagePath (string) which is the import path of the generated package, or empty
// when it is not known.
func finishGoTypes(request *emit.Request, mappings *gotypes.MappingTable, packagePath string) {
	if request.Catalogue != nil {
		for _, schema := range request.Catalogue.Schemas {
			finishSchemaOverrides(schema, packagePath)
		}
	}
	for _, query := range request.Queries {
		finishQueryOverrides(query, packagePath)
	}
	if mappings != nil && packagePath != "" {
		for index := range mappings.Mappings {
			mapping := &mappings.Mappings[index]
			mapping.NotNull = localGoType(mapping.NotNull, packagePath)
			mapping.Nullable = localGoType(mapping.Nullable, packagePath)
		}
		localiseSlots(&mappings.Slots, packagePath)
	}
}

// finishSchemaOverrides completes the override of each column of a schema's tables and
// views that has one.
//
// Takes schema (*sqlmodel.Schema) which is updated in place; nil, and a nil table or view
// entry, is passed over.
// Takes packagePath (string) which is the generated package's import path, or empty.
func finishSchemaOverrides(schema *sqlmodel.Schema, packagePath string) {
	if schema == nil {
		return
	}
	for _, table := range schema.Tables {
		if table != nil {
			finishColumnOverrides(table.Columns, packagePath)
		}
	}
	for _, view := range schema.Views {
		if view != nil {
			finishColumnOverrides(view.Columns, packagePath)
		}
	}
}

// finishQueryOverrides completes the override of each parameter and output column of a
// query that has one. A slice parameter's override names the type of its elements, which
// the generated code binds as a slice of it, so it is not read as an array's.
//
// Takes query (*sqlmodel.AnalysedQuery) which is updated in place.
// Takes packagePath (string) which is the generated package's import path, or empty.
func finishQueryOverrides(query *sqlmodel.AnalysedQuery, packagePath string) {
	for index := range query.Parameters {
		parameter := &query.Parameters[index]
		sqlType := parameter.SQLType
		if parameter.IsSlice {
			sqlType = sqlmodel.SQLType{}
		}
		parameter.GoTypeOverride = finishedOverride(parameter.GoTypeOverride, sqlType, packagePath)
	}
	for index := range query.OutputColumns {
		column := &query.OutputColumns[index]
		column.GoTypeOverride = finishedOverride(column.GoTypeOverride, column.SQLType, packagePath)
	}
}

// finishColumnOverrides completes the override of each column that has one.
//
// Takes columns ([]sqlmodel.Column) which are updated in place.
// Takes packagePath (string) which is the generated package's import path, or empty.
func finishColumnOverrides(columns []sqlmodel.Column, packagePath string) {
	for index := range columns {
		column := &columns[index]
		column.GoTypeOverride = finishedOverride(column.GoTypeOverride, column.ValueType(), packagePath)
	}
}

// finishedOverride returns the Go type an override gives a value of a SQL type: the
// override without the generated package's import path, made a slice of the override for
// an array whose override names a type other than a slice.
//
// Takes override (*sqlmodel.GoType) which is the override, or nil.
// Takes sqlType (sqlmodel.SQLType) which is the value's SQL type.
// Takes packagePath (string) which is the generated package's import path, or empty.
//
// Returns *sqlmodel.GoType which is the finished override, a new value when it differs,
// or nil when there is no override.
func finishedOverride(override *sqlmodel.GoType, sqlType sqlmodel.SQLType, packagePath string) *sqlmodel.GoType {
	if override == nil {
		return nil
	}
	finished := localGoType(*override, packagePath)
	if sqlType.Category == sqlmodel.TypeCategoryArray && !strings.HasPrefix(strings.TrimPrefix(finished.Name, goPointerMarker), goSliceMarker) {
		finished.Name = arrayOfOverride(finished.Name, sqlType)
	}
	if finished == *override {
		return override
	}
	return &finished
}

// arrayOfOverride returns the name of a slice holding an array's elements as an override
// names their type: one slice level for each dimension, around a pointer when the
// innermost elements may be NULL.
//
// Takes name (string) which is the override's name for the element type.
// Takes sqlType (sqlmodel.SQLType) which is the array type.
//
// Returns string which is the name of the slice type.
func arrayOfOverride(name string, sqlType sqlmodel.SQLType) string {
	var prefix strings.Builder
	element := &sqlType
	for depth := 0; depth < sqlmodel.MaxArrayDimensions && element.Category == sqlmodel.TypeCategoryArray && element.ElementType != nil; depth++ {
		prefix.WriteString(goSliceMarker)
		element = element.ElementType
	}
	if element.Nullable && !strings.HasPrefix(name, goPointerMarker) {
		prefix.WriteString(goPointerMarker)
	}
	return prefix.String() + name
}

// localGoType returns a Go type with every part named in the generated package written
// without its import path.
//
// Parts nested sqlmodel.MaxTypeDepth or more levels deep keep their import path, as
// goTypeTooDeep reports and the depth diagnostics warn about.
//
// Takes goType (sqlmodel.GoType) which is the Go type.
// Takes packagePath (string) which is the generated package's import path, or empty.
//
// Returns sqlmodel.GoType which is the Go type, sharing no composite with the original
// when one of its parts changed.
func localGoType(goType sqlmodel.GoType, packagePath string) sqlmodel.GoType {
	return localGoTypeAt(goType, packagePath, 0)
}

// localGoTypeAt localises a Go type nested at a depth, as localGoType describes.
//
// Takes goType (sqlmodel.GoType) which is the Go type.
// Takes packagePath (string) which is the generated package's import path, or empty.
// Takes depth (int) which is how deeply the type is nested; the root sits at depth 0.
//
// Returns sqlmodel.GoType which is the Go type, unchanged at or past the depth limit.
func localGoTypeAt(goType sqlmodel.GoType, packagePath string, depth int) sqlmodel.GoType {
	if packagePath == "" || depth >= sqlmodel.MaxTypeDepth {
		return goType
	}
	if goType.Package == packagePath {
		goType.Package = ""
	}
	if goType.Composite != nil {
		goType.Composite = localComposite(*goType.Composite, packagePath, depth+1)
	}
	return goType
}

// localComposite localises the parts of a composite Go type, as localGoType describes.
//
// Takes composite (sqlmodel.GoComposite) which is a copy of the composite.
// Takes packagePath (string) which is the generated package's import path.
// Takes depth (int) which is how deeply the composite's parts are nested.
//
// Returns *sqlmodel.GoComposite which is a new composite, sharing no part with the
// original.
func localComposite(composite sqlmodel.GoComposite, packagePath string, depth int) *sqlmodel.GoComposite {
	if composite.Key != nil {
		composite.Key = new(localGoTypeAt(*composite.Key, packagePath, depth))
	}
	if composite.Value != nil {
		composite.Value = new(localGoTypeAt(*composite.Value, packagePath, depth))
	}
	if composite.Fields != nil {
		fields := make([]sqlmodel.GoField, len(composite.Fields))
		for index, field := range composite.Fields {
			field.Type = localGoTypeAt(field.Type, packagePath, depth)
			fields[index] = field
		}
		composite.Fields = fields
	}
	if composite.Arguments != nil {
		arguments := make([]sqlmodel.GoType, len(composite.Arguments))
		for index, argument := range composite.Arguments {
			arguments[index] = localGoTypeAt(argument, packagePath, depth)
		}
		composite.Arguments = arguments
	}
	return &composite
}

// goTypeTooDeep reports whether a Go type nests key, value, field or argument types to
// sqlmodel.MaxTypeDepth or beyond, so localGoType stops before reaching the innermost
// part.
//
// Takes goType (*sqlmodel.GoType) which is the type; nil is never too deep.
//
// Returns bool which is true when a nested part sits at depth sqlmodel.MaxTypeDepth or
// deeper.
func goTypeTooDeep(goType *sqlmodel.GoType) bool {
	return goTypeDepthReaches(goType, 0)
}

// goTypeDepthReaches reports whether a Go type, or a part nested in it, sits at
// sqlmodel.MaxTypeDepth or deeper.
//
// Takes goType (*sqlmodel.GoType) which is the type; nil reaches nothing.
// Takes depth (int) which is the type's own depth.
//
// Returns bool which is true when the type or a nested part sits at or past the limit.
func goTypeDepthReaches(goType *sqlmodel.GoType, depth int) bool {
	if goType == nil {
		return false
	}
	if depth >= sqlmodel.MaxTypeDepth {
		return true
	}
	composite := goType.Composite
	if composite == nil {
		return false
	}
	if goTypeDepthReaches(composite.Key, depth+1) || goTypeDepthReaches(composite.Value, depth+1) {
		return true
	}
	for index := range composite.Fields {
		if goTypeDepthReaches(&composite.Fields[index].Type, depth+1) {
			return true
		}
	}
	for index := range composite.Arguments {
		if goTypeDepthReaches(&composite.Arguments[index], depth+1) {
			return true
		}
	}
	return false
}

// localiseSlots writes every type slot's Go types without the generated package's import
// path.
//
// Takes slots (*sqlmodel.TypeSlots) which are updated in place.
// Takes packagePath (string) which is the generated package's import path.
func localiseSlots(slots *sqlmodel.TypeSlots, packagePath string) {
	for _, slot := range []*sqlmodel.TypeSlot{
		&slots.Decimal, &slots.Money, &slots.BigInt, &slots.JSON, &slots.Interval, &slots.TimeOfDay,
		&slots.IPAddress, &slots.Enum, &slots.Map, &slots.Struct, &slots.JSONScanInto,
	} {
		slot.NotNull = localGoType(slot.NotNull, packagePath)
		slot.Nullable = localGoType(slot.Nullable, packagePath)
	}
}
