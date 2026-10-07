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

package gogen

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// overrideCoverage is what a request's Go type table offers when it can tell the Go types
// a configured override chose from those the generator built, as the pipeline's table
// does.
type overrideCoverage interface {
	// OverridesWithin reports whether a configured override chooses the Go type of a SQL
	// type or of a part within it.
	//
	// Takes sqlType (sqlmodel.SQLType) which is the SQL type.
	//
	// Returns bool which is true when an override chooses the type or a part of it.
	OverridesWithin(sqlType sqlmodel.SQLType) bool
}

// ReadsOverride reports whether an output column uses the driver's override scan adapter.
//
// Other driver reading strategies take precedence, including conversion for type-level
// overrides.
//
// Takes column (*sqlmodel.OutputColumn) which is the output column.
//
// Returns bool which is true when the column is read through the adapter.
func (e *Emission) ReadsOverride(column *sqlmodel.OutputColumn) bool {
	reader, reads := e.Driver.Reader.(OverrideReader)
	if !reads || !OverrideChosen(e.Request.Types, column.GoTypeOverride, column.SQLType) {
		return false
	}
	if column.GoTypeOverride == nil && e.Reading(column.SQLType) != ColumnReadAsIs {
		return false
	}
	return reader.ReadsOverride(ChosenGoType(e.Request.Types, column.GoTypeOverride, column.SQLType, column.Nullable), column.SQLType)
}

// OverrideScannedColumns identifies output columns read through the driver's override
// scan adapter. Columns handled earlier by JSON wrapping, conversions, raw locals or enum
// string scanning are excluded.
//
// Takes analysed (*sqlmodel.AnalysedQuery) which is one of the request's queries.
//
// Returns []bool which holds, parallel to its output columns, whether each is read so.
// Returns error when the query cannot be rendered.
func (e *Emission) OverrideScannedColumns(analysed *sqlmodel.AnalysedQuery) ([]bool, error) {
	query, err := e.Query(analysed, goast.NewImportTracker())
	if err != nil {
		return nil, err
	}
	columns := analysed.OutputColumns
	scanned := make([]bool, len(columns))
	for index := range columns {
		column := &columns[index]
		kind, wrapped := query.Rendered.Marks.Wrapped[index]
		claimed := e.ReadsRaw(column) || e.ConvertsColumn(column, wrapped) || (wrapped && kind == emitkit.ColumnWrapJSON) ||
			e.Driver.Enums.Scanned(column, query.Types).Shape != 0
		scanned[index] = !claimed && e.ReadsOverride(column)
	}
	return scanned, nil
}

// ReadsAnyOverride reports whether the generated code reads any output column of the
// package's queries through the driver's override scan adapter, so the package needs it.
//
// Returns bool which is true when some column is read so.
func (e *Emission) ReadsAnyOverride() bool {
	for _, analysed := range e.Request.Queries {
		for index := range analysed.OutputColumns {
			if e.ReadsOverride(&analysed.OutputColumns[index]) {
				return true
			}
		}
	}
	return false
}

// OverrideScan returns the scan argument that reads a column into a destination through
// the driver's override scan adapter, or the destination itself when the driver has none.
//
// Takes destination (ast.Expr) which is the address the column is read into.
// Takes column (*sqlmodel.OutputColumn) which is the column.
//
// Returns ast.Expr which is the scan argument.
func (e *Emission) OverrideScan(destination ast.Expr, column *sqlmodel.OutputColumn) ast.Expr {
	reader, reads := e.Driver.Reader.(OverrideReader)
	if !reads {
		return destination
	}
	return reader.OverrideScan(destination, column.SQLType)
}

// OverrideChosen reports Go types selected by column, parameter or SQL type overrides.
//
// Takes resolver (sqlmodel.GoTypeResolver) which maps the request's SQL types, and says
// which mappings an override chose when it can.
// Takes goTypeOverride (*sqlmodel.GoType) which is the column's or parameter's own Go
// type override, or nil.
// Takes sqlType (sqlmodel.SQLType) which is its SQL type.
//
// Returns bool which is true when an override chose its Go type.
func OverrideChosen(resolver sqlmodel.GoTypeResolver, goTypeOverride *sqlmodel.GoType, sqlType sqlmodel.SQLType) bool {
	if goTypeOverride != nil {
		return true
	}
	coverage, covers := resolver.(overrideCoverage)
	return covers && coverage.OverridesWithin(sqlType)
}

// ChosenGoType returns the Go type a column or a parameter is written as: its own Go type
// override, a pointer to it when nullable, or the type the request's table maps its SQL
// type to.
//
// Takes resolver (sqlmodel.GoTypeResolver) which maps the request's SQL types.
// Takes goTypeOverride (*sqlmodel.GoType) which is its own Go type override, or nil.
// Takes sqlType (sqlmodel.SQLType) which is its SQL type.
// Takes nullable (bool) which reports that it can be NULL.
//
// Returns sqlmodel.GoType which is the Go type.
func ChosenGoType(resolver sqlmodel.GoTypeResolver, goTypeOverride *sqlmodel.GoType, sqlType sqlmodel.SQLType, nullable bool) sqlmodel.GoType {
	if override := goast.NullableOverride(goTypeOverride, sqlType, nullable); override != nil {
		return *override
	}
	return goast.ResolveGoType(sqlType, nullable, resolver)
}

// PredeclaredGoType reports whether a Go type is one of Go's predeclared types, such as
// string, int32 or bool, or a pointer to or a slice of one, such as []byte: a type
// database/sql converts every driver value of a basic kind into itself.
//
// Takes goType (sqlmodel.GoType) which is the Go type.
//
// Returns bool which is true for a predeclared type.
func PredeclaredGoType(goType sqlmodel.GoType) bool {
	if goType.Package != "" || goType.Composite != nil {
		return false
	}
	name := strings.TrimLeft(goType.Name, "*")
	for strings.HasPrefix(name, "[]") {
		name = strings.TrimLeft(strings.TrimPrefix(name, "[]"), "*")
	}
	if !token.IsIdentifier(name) {
		return false
	}
	_, predeclared := types.Universe.Lookup(name).(*types.TypeName)
	return predeclared
}

// EncodesJSONDocument reports whether a JSON parameter with an overridden Go type must be
// encoded as document text for binding. Go string and []byte values, pointers to them and
// slice parameters are excluded.
//
// Takes resolver (sqlmodel.GoTypeResolver) which maps the request's SQL types, and says
// which mappings an override chose when it can.
// Takes parameter (*sqlmodel.QueryParameter) which is the parameter.
//
// Returns bool which is true when the parameter is bound as its document.
func EncodesJSONDocument(resolver sqlmodel.GoTypeResolver, parameter *sqlmodel.QueryParameter) bool {
	if parameter.IsSlice || parameter.SQLType.Category != sqlmodel.TypeCategoryJSON ||
		!OverrideChosen(resolver, parameter.GoTypeOverride, parameter.SQLType) {
		return false
	}
	return !HoldsTextOrBytes(ChosenGoType(resolver, parameter.GoTypeOverride, parameter.SQLType, parameter.Nullable))
}

// HoldsTextOrBytes reports whether a Go type is Go's own string or bytes, or a pointer to
// either, which hold a JSON document's text as it is.
//
// Takes goType (sqlmodel.GoType) which is the Go type.
//
// Returns bool which is true for string, []byte and pointers to them.
func HoldsTextOrBytes(goType sqlmodel.GoType) bool {
	if goType.Package != "" || goType.Composite != nil {
		return false
	}
	name := strings.TrimPrefix(goType.Name, "*")
	return name == "string" || name == "[]byte" || name == "[]uint8"
}
