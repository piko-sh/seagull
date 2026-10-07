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
	"fmt"
	"strings"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/gotypes"
	"seagull.sh/seagull/internal/sqlmodel"
)

// unmatchedTypeOverrides reports each type override for a SQL type that changes nothing:
// one whose type, or array element type, is a name the dialect does not know and no
// column of the schema has a type of.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the schema the overrides apply to.
//
// Returns []diag.SourceError which holds a Q061 warning for each such override.
func (p *Pipeline) unmatchedTypeOverrides(catalogue *sqlmodel.Catalogue) []diag.SourceError {
	var diagnostics []diag.SourceError
	mapper := gotypes.NewTypeMapper(p.dialect.BuiltinTypes()).WithSchema(catalogue)
	for _, override := range p.typeOverrides {
		if override.Column != "" || override.SQLTypeName == "" {
			continue
		}
		element := innermostElement(mapper.ResolveOverrideType(override))
		if element.Category != sqlmodel.TypeCategoryUnknown || catalogueHoldsUnknownType(catalogue, element.EngineName) {
			continue
		}
		diagnostics = append(diagnostics, diagbuild.Block("", 0, diag.CodeUnmatchedTypeOverride, diag.SeverityWarning, fmt.Sprintf(
			"type override for SQL type %q matches no type: the dialect knows no type %q and no column of the schema "+
				"has one, so the override changes nothing; name the type as the dialect spells it in a migration",
			override.SQLTypeName, element.EngineName)))
	}
	return diagnostics
}

// innermostElement returns the type an array holds at its innermost depth, or the type
// itself when it is not an array.
//
// Takes sqlType (sqlmodel.SQLType) which is the type.
//
// Returns sqlmodel.SQLType which is the innermost element type.
func innermostElement(sqlType sqlmodel.SQLType) sqlmodel.SQLType {
	for depth := 0; depth < sqlmodel.MaxArrayDimensions && sqlType.Category == sqlmodel.TypeCategoryArray && sqlType.ElementType != nil; depth++ {
		sqlType = *sqlType.ElementType
	}
	return sqlType
}

// catalogueHoldsUnknownType reports whether a column of a table or a view has a type the
// dialect does not know by a name, alone or as the elements of an array.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the schema, or nil; a nil schema,
// table or view entry is passed over.
// Takes name (string) which is the type's name, compared regardless of case.
//
// Returns bool which is true when some column has such a type.
func catalogueHoldsUnknownType(catalogue *sqlmodel.Catalogue, name string) bool {
	if catalogue == nil {
		return false
	}
	for _, schema := range catalogue.Schemas {
		if schemaHoldsUnknownType(schema, name) {
			return true
		}
	}
	return false
}

// schemaHoldsUnknownType reports whether a column of one of a schema's tables or views
// has a type the dialect does not know by a name, alone or as the elements of an array.
//
// Takes schema (*sqlmodel.Schema) which holds the tables and views; nil, and a nil table
// or view entry, is passed over.
// Takes name (string) which is the type's name, compared regardless of case.
//
// Returns bool which is true when some column has such a type.
func schemaHoldsUnknownType(schema *sqlmodel.Schema, name string) bool {
	if schema == nil {
		return false
	}
	for _, table := range schema.Tables {
		if table != nil && columnsHoldUnknownType(table.Columns, name) {
			return true
		}
	}
	for _, view := range schema.Views {
		if view != nil && columnsHoldUnknownType(view.Columns, name) {
			return true
		}
	}
	return false
}

// columnsHoldUnknownType reports whether one of the columns has a type the dialect does
// not know by a name, alone or as the elements of an array.
//
// Takes columns ([]sqlmodel.Column) which are the columns.
// Takes name (string) which is the type's name, compared regardless of case.
//
// Returns bool which is true when one has.
func columnsHoldUnknownType(columns []sqlmodel.Column, name string) bool {
	for index := range columns {
		element := innermostElement(columns[index].ValueType())
		if element.Category == sqlmodel.TypeCategoryUnknown && strings.EqualFold(element.EngineName, name) {
			return true
		}
	}
	return false
}
