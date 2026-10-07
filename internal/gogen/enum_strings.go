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
	"fmt"
	"go/ast"

	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// SuffixConvertStrings is the suffix of the helper that converts a slice of one string
	// type to another, such as sglConvertStrings.
	SuffixConvertStrings = "ConvertStrings"

	// SuffixConvertStringPointers is the suffix of the helper that converts a slice of
	// pointers to one string type to pointers to another, such as sglConvertStringPointers.
	SuffixConvertStringPointers = "ConvertStringPointers"

	// SuffixConvertStringMap is the suffix of the helper that converts the values of a map
	// from one string type to another, such as sglConvertStringMap.
	SuffixConvertStringMap = "ConvertStringMap"

	// SuffixDereference is the suffix of the helper that reads an optional parameter through
	// the pointer a dynamic query's Params struct holds it by, such as sglDereference,
	// before its slice or map is converted.
	SuffixDereference = "Dereference"

	// enumConverted lists the shapes the generated code converts to and from Go's own
	// strings.
	enumConverted = EnumValue | EnumSlice | EnumMap
)

// EnumShapes is a set of the shapes in which a Go type holds an enum type a package
// declares, a named type over string.
type EnumShapes uint8

const (
	// EnumValue is a value of an enum type the package declares, or, for a nullable one, a
	// pointer to it.
	EnumValue EnumShapes = 1 << iota

	// EnumSlice is a slice of values of an enum type the package declares, or of pointers to
	// them, as an array column, an array of nullable elements or a slice parameter holds.
	EnumSlice

	// EnumMap is a map whose values are of an enum type the package declares and whose key
	// holds none, as a map column of enum values holds.
	EnumMap

	// EnumNested is an enum type the package declares held deeper than the shapes above but
	// through no map: in a slice of slices, or in a field of a struct. The generated code
	// converts none of it.
	EnumNested

	// EnumNestedInMap identifies an enum nested within a map. Generated code does not
	// convert these values.
	EnumNestedInMap
)

// EnumStrings describes where a driver needs generated enums converted to strings.
//
// Simple shapes can be converted for scanning and binding. Unsupported nested shapes are
// reported by Problems. The zero value requires no conversions.
type EnumStrings struct {
	// Scan lists the shapes the driver scans only into strings.
	Scan EnumShapes

	// Bind lists the shapes the driver binds only as strings.
	Bind EnumShapes
}

// Bound converts enum parameters to plain strings when the driver's binding rules require
// it.
//
// An optional parameter a dynamic query's Params struct holds through a pointer, as
// OptionalPointer reports, is read through sglDereference before its slice or map is
// converted, so a nil pointer binds as NULL.
//
// Takes access (ast.Expr) which reads the parameter's value.
// Takes query (*sqlmodel.AnalysedQuery) which is the query the parameter belongs to.
// Takes parameter (*sqlmodel.QueryParameter) which is the parameter.
// Takes resolver (sqlmodel.GoTypeResolver) which maps SQL types to Go types.
// Takes vocabulary (Vocabulary) which names the conversion helper.
//
// Returns ast.Expr which is the value bound.
func (s EnumStrings) Bound(access ast.Expr, query *sqlmodel.AnalysedQuery, parameter *sqlmodel.QueryParameter,
	resolver sqlmodel.GoTypeResolver, vocabulary Vocabulary) ast.Expr {
	converted := s.bound(access, parameter, parameter.IsSlice, resolver, vocabulary)
	if converted == access || !OptionalPointer(query, parameter) {
		return converted
	}
	holding := HeldEnum(parameter.SQLType, parameter.Nullable, parameter.IsSlice, parameter.GoTypeOverride, resolver)
	if holding.Shape != EnumSlice && holding.Shape != EnumMap {
		return converted
	}
	dereferenced := goastutil.CallExpr(goastutil.CachedIdent(vocabulary.Helper(SuffixDereference)), access)
	return holding.Strings(dereferenced, vocabulary)
}

// BoundElement returns the value the generated code hands a driver that binds enums as
// the EnumStrings says for one element of a slice parameter it binds element by element.
//
// Takes element (ast.Expr) which reads the element.
// Takes parameter (*sqlmodel.QueryParameter) which is the slice parameter.
// Takes resolver (sqlmodel.GoTypeResolver) which maps SQL types to Go types.
// Takes vocabulary (Vocabulary) which names the conversion helper.
//
// Returns ast.Expr which is the value bound.
func (s EnumStrings) BoundElement(element ast.Expr, parameter *sqlmodel.QueryParameter, resolver sqlmodel.GoTypeResolver,
	vocabulary Vocabulary) ast.Expr {
	return s.bound(element, parameter, false, resolver, vocabulary)
}

// Scanned selects string-based enum scanning where supported conversions are required.
//
// Takes column (*sqlmodel.OutputColumn) which is the column.
// Takes resolver (sqlmodel.GoTypeResolver) which maps SQL types to Go types.
//
// Returns EnumHolding which says how the column holds the enum type it is read through
// strings as.
func (s EnumStrings) Scanned(column *sqlmodel.OutputColumn, resolver sqlmodel.GoTypeResolver) EnumHolding {
	holding := HeldEnum(column.SQLType, column.Nullable, false, column.GoTypeOverride, resolver)
	if s.Scan&enumConverted&holding.Shape == 0 {
		return EnumHolding{}
	}
	return holding
}

// Problems reports enum shapes the driver requires converted but generated code cannot
// convert.
//
// Slice parameters are checked as individual elements when expanded, and as a whole
// otherwise. Unsupported shapes use Q059.
//
// Takes request (*emit.Request) which holds the dialect and the type mappings. Problems
// does not change it.
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
// Takes lists (emitkit.ListMode) which is the driver's list mode, as Driver.Lists holds
// it.
// Takes driver (string) which names the driver in the messages, such as "pgx".
//
// Returns []emit.Problem which holds the problems, or nil when there are none.
func (s EnumStrings) Problems(request *emit.Request, query *sqlmodel.AnalysedQuery, lists emitkit.ListMode, driver string) []emit.Problem {
	var problems []emit.Problem
	resolver := request.Types
	for index := range query.OutputColumns {
		column := &query.OutputColumns[index]
		holding := HeldEnum(column.SQLType, column.Nullable, false, column.GoTypeOverride, resolver)
		if unconverted(s.Scan, holding) {
			problems = append(problems, enumProblem(query, fmt.Sprintf("column %s", column.Name), "read", holding, driver,
				"cast the column to a type that holds no enum in the query"))
		}
	}
	wholeSlices := !SlicesExpand(lists, &request.Dialect, query)
	for index := range query.Parameters {
		parameter := &query.Parameters[index]
		if parameter.IsPaginationBound() {
			continue
		}
		holding := HeldEnum(parameter.SQLType, parameter.Nullable, parameter.IsSlice && wholeSlices, parameter.GoTypeOverride, resolver)
		if unconverted(s.Bind, holding) {
			problems = append(problems, enumProblem(query, fmt.Sprintf("parameter %s", parameter.Name), "bind", holding, driver,
				"give the parameter a type that holds no enum (sgl.param type:)"))
		}
	}
	return problems
}

// bound returns a value converted to Go's own strings when it holds an enum type the
// package declares in a shape listed in Bind.
//
// Takes access (ast.Expr) which reads the value.
// Takes parameter (*sqlmodel.QueryParameter) which is the parameter the value belongs to.
// Takes slice (bool) which reports that the value is the whole slice of a slice
// parameter.
// Takes resolver (sqlmodel.GoTypeResolver) which maps SQL types to Go types.
// Takes vocabulary (Vocabulary) which names the conversion helper.
//
// Returns ast.Expr which is the value bound.
func (s EnumStrings) bound(access ast.Expr, parameter *sqlmodel.QueryParameter, slice bool, resolver sqlmodel.GoTypeResolver,
	vocabulary Vocabulary) ast.Expr {
	if s.Bind == 0 || parameter.IsPaginationBound() {
		return access
	}
	holding := HeldEnum(parameter.SQLType, parameter.Nullable, slice, parameter.GoTypeOverride, resolver)
	if s.Bind&enumConverted&holding.Shape == 0 {
		return access
	}
	return holding.Strings(access, vocabulary)
}

// OptionalPointer reports whether an optional parameter of a dynamic query uses a pointer
// field in Params. Runtime builders are excluded, and slice or map fields still need the
// pointer.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
// Takes parameter (*sqlmodel.QueryParameter) which is one of its parameters.
//
// Returns bool which is true when the field is such a pointer.
func OptionalPointer(query *sqlmodel.AnalysedQuery, parameter *sqlmodel.QueryParameter) bool {
	return parameter.IsOptional && query.IsDynamic && !query.DynamicRuntime
}

// unconverted reports whether a value holds an enum type the package declares in a shape
// a driver's shapes list but the generated code does not convert.
//
// Takes shapes (EnumShapes) which are the shapes the driver handles only as strings.
// Takes holding (EnumHolding) which says how the value holds the enum type.
//
// Returns bool which is true when the value cannot be handed to or read from the driver.
func unconverted(shapes EnumShapes, holding EnumHolding) bool {
	return shapes&holding.Shape != 0 && enumConverted&holding.Shape == 0
}

// enumProblem returns the problem with a column or parameter whose enum type the driver
// handles only as strings in a shape the generated code does not convert.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
// Takes subject (string) which names the column or parameter, such as "column moods".
// Takes action (string) which is what the driver cannot do with it, "read" or "bind".
// Takes holding (EnumHolding) which says how it holds the enum type.
// Takes driver (string) which names the driver.
// Takes remedy (string) which is the first thing to change.
//
// Returns emit.Problem which describes the refusal.
func enumProblem(query *sqlmodel.AnalysedQuery, subject, action string, holding EnumHolding, driver, remedy string) emit.Problem {
	where := "nested in a slice or a struct"
	if holding.Shape == EnumNestedInMap {
		where = "inside a map, as a key or within a value"
	}
	return emit.Problem{
		Query: query,
		Code:  diag.CodeEmitterUnsupported,
		Message: fmt.Sprintf("%s holds the generated enum type %s %s, which %s cannot %s, and the generated code converts "+
			"an enum only as a value, a pointer, a slice or a map's values; %s, or keep enums as Go strings (types.enum: string)",
			subject, holding.Name, where, driver, action, remedy),
		Severity: diag.SeverityError,
	}
}
