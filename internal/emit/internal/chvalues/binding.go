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

package chvalues

import (
	"go/ast"
	"strconv"
	"strings"

	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/helpers"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// suffixValue is the helper-prefix suffix of the value helper.
	suffixValue = "ClickHouseValue"

	// suffixEncoded is the helper-prefix suffix of the function that binds a JSON parameter
	// as the document its value encodes to.
	suffixEncoded = "ClickHouseEncoded"

	// suffixAppendRow is the helper-prefix suffix of the function appending one row to a
	// native batch through the conversion.
	suffixAppendRow = "AppendNativeRow"

	// suffixNullable is the helper-prefix suffix of the function handing either driver a
	// nullable parameter a runtime builder binds through a pointer.
	suffixNullable = "ClickHouseNullable"

	// maximumFractionDigits is the most fraction digits a date and time is written with.
	maximumFractionDigits = 9

	// shapeText writes text, quoted inside an array, a map or a tuple.
	shapeText = "q"

	// shapeIPv4 writes an IP address of a type holding only IPv4 ones, quoted like text.
	shapeIPv4 = "4"

	// shapeAddress writes an IP address of a type holding either version, quoted like text.
	shapeAddress = "6"

	// bitsInIPv4 is the width of an address type holding only IPv4 addresses.
	bitsInIPv4 = 32

	// shapeNumber writes a number or a boolean bare.
	shapeNumber = "n"

	// shapeDate writes a calendar date.
	shapeDate = "d"

	// shapeDateTime writes a date and time as Unix seconds, followed by the number of
	// fraction digits when there are any.
	shapeDateTime = "t"

	// shapeUnknown writes whatever the Go value is.
	shapeUnknown = "a"

	// arrayTypeOpen and arrayTypeClose are written around the type of a list placeholder,
	// which both ClickHouse drivers bind to the whole slice as one array value.
	arrayTypeOpen = "Array("

	// arrayTypeClose closes a ClickHouse Array type expression.
	arrayTypeClose = ")"
)

// Shape returns the shape the value helper writes a value of a SQL type with: how the
// type is read at each level, which the generated code passes beside the value.
//
// Takes sqlType (*sqlmodel.SQLType) which is the parameter's SQL type.
//
// Returns string which is the shape, such as n for a number, [q] for an array of text or
// ('id'=n,'name'=q) for a named tuple.
func Shape(sqlType *sqlmodel.SQLType) string {
	switch sqlType.Category {
	case sqlmodel.TypeCategoryInteger, sqlmodel.TypeCategoryFloat, sqlmodel.TypeCategoryDecimal, sqlmodel.TypeCategoryBoolean:
		return shapeNumber
	case sqlmodel.TypeCategoryTemporal:
		return temporalShape(sqlType)
	case sqlmodel.TypeCategoryArray:
		return "[" + elementShape(sqlType.ElementType) + "]"
	case sqlmodel.TypeCategoryMap:
		return "{" + elementShape(sqlType.KeyType) + ":" + elementShape(sqlType.ElementType) + "}"
	case sqlmodel.TypeCategoryStruct:
		return tupleShape(sqlType.StructFields)
	case sqlmodel.TypeCategoryNetwork:
		return networkShape(sqlType)
	case sqlmodel.TypeCategoryText, sqlmodel.TypeCategoryBinary, sqlmodel.TypeCategoryJSON, sqlmodel.TypeCategoryUUID,
		sqlmodel.TypeCategoryEnum:
		return shapeText
	default:
		return shapeUnknown
	}
}

// ParameterShape returns the shape of a parameter's value: its SQL type's, or an array of
// it for a slice parameter, which is bound whole.
//
// Takes parameter (*sqlmodel.QueryParameter) which is the parameter.
//
// Returns string which is the shape.
func ParameterShape(parameter *sqlmodel.QueryParameter) string {
	shape := Shape(&parameter.SQLType)
	if parameter.IsSlice {
		return "[" + shape + "]"
	}
	return shape
}

// Bind builds `sglClickHouseValue(access, "<shape>")`, which writes the parameter's value
// as the text ClickHouse parses for its type.
//
// Takes vocabulary (helpers.Vocabulary) which names the helper.
// Takes access (ast.Expr) which reads the parameter's value.
// Takes parameter (*sqlmodel.QueryParameter) which is the parameter.
//
// Returns ast.Expr which is the call.
func Bind(vocabulary helpers.Vocabulary, access ast.Expr, parameter *sqlmodel.QueryParameter) ast.Expr {
	return goastutil.CallExpr(goastutil.CachedIdent(vocabulary.Helper(suffixValue)), access, goastutil.StrLit(ParameterShape(parameter)))
}

// BindPositional builds an expression for binding runtime builder parameters through
// driver.Valuer. Nullable pointer parameters use sglClickHouseNullable to bind NULL
// safely, since the ClickHouse drivers may otherwise call a value-receiver method on a
// nil pointer and panic; other parameters remain unchanged.
//
// Takes vocabulary (helpers.Vocabulary) which names the helper.
// Takes access (ast.Expr) which reads the parameter's value.
// Takes parameter (*sqlmodel.QueryParameter) which is the parameter.
// Takes types (sqlmodel.GoTypeResolver) which maps the request's SQL types.
//
// Returns ast.Expr which binds the parameter.
func BindPositional(vocabulary helpers.Vocabulary, access ast.Expr, parameter *sqlmodel.QueryParameter, types sqlmodel.GoTypeResolver) ast.Expr {
	if !heldByNullablePointer(parameter, types) {
		return access
	}
	return goastutil.CallExpr(goastutil.CachedIdent(vocabulary.Helper(suffixNullable)), access)
}

// BindsNullablesPositionally reports whether a runtime builder of the queries binds a
// nullable parameter held through a pointer through sglClickHouseNullable, so the value
// helper's file holds it.
//
// Takes queries ([]*sqlmodel.AnalysedQuery) which are the queries.
// Takes types (sqlmodel.GoTypeResolver) which maps the request's SQL types.
//
// Returns bool which is true when one does.
func BindsNullablesPositionally(queries []*sqlmodel.AnalysedQuery, types sqlmodel.GoTypeResolver) bool {
	for _, query := range queries {
		if !query.DynamicRuntime {
			continue
		}
		for index := range query.Parameters {
			if heldByNullablePointer(&query.Parameters[index], types) {
				return true
			}
		}
	}
	return false
}

// BindsDocuments reports whether any query binds a JSON parameter as the document its
// value encodes to, as gogen.EncodesJSONDocument says, so the value helper's file holds
// sglClickHouseEncoded.
//
// Takes queries ([]*sqlmodel.AnalysedQuery) which are the queries.
// Takes types (sqlmodel.GoTypeResolver) which maps the request's SQL types.
//
// Returns bool which is true when some parameter is bound so.
func BindsDocuments(queries []*sqlmodel.AnalysedQuery, types sqlmodel.GoTypeResolver) bool {
	for _, query := range queries {
		for index := range query.Parameters {
			if gogen.EncodesJSONDocument(types, &query.Parameters[index]) {
				return true
			}
		}
	}
	return false
}

// BindsDurations reports whether any query binds a parameter holding a time of day, a
// Time or Time64 value the generated code holds in a time.Duration, so the value helper's
// file holds sglClickHouseDuration.
//
// Takes queries ([]*sqlmodel.AnalysedQuery) which are the queries.
//
// Returns bool which is true when some parameter holds one.
func BindsDurations(queries []*sqlmodel.AnalysedQuery) bool {
	for _, query := range queries {
		for index := range query.Parameters {
			if holdsTimeOfDay(&query.Parameters[index].SQLType, 0) {
				return true
			}
		}
	}
	return false
}

// Document wraps a JSON parameter's access as `sglClickHouseEncoded(access)`, which binds
// the document the value encodes to, for a parameter gogen.EncodesJSONDocument reports.
//
// Takes vocabulary (helpers.Vocabulary) which names the helper.
// Takes access (ast.Expr) which reads the parameter.
//
// Returns ast.Expr which is the call.
func Document(vocabulary helpers.Vocabulary, access ast.Expr) ast.Expr {
	return goastutil.CallExpr(goastutil.CachedIdent(vocabulary.Helper(suffixEncoded)), access)
}

// Appends reports SQL types whose native batch values require checking or conversion.
//
// Addresses are checked against their SQL type; Maps holding addresses or tuples may need
// driver-specific Go forms.
//
// Takes sqlType (*sqlmodel.SQLType) which is the parameter's SQL type.
//
// Returns bool which is true when the value is appended through the conversion.
func Appends(sqlType *sqlmodel.SQLType) bool {
	return holdsAddress(sqlType, 0) || holdsMapOfTuples(sqlType, false, 0)
}

// AppendRow builds a checked and converted native batch row append call.
//
// The row shape names values after parameters; values appended directly use the unknown
// shape.
//
// Takes vocabulary (helpers.Vocabulary) which names the helper.
// Takes appendRow (ast.Expr) which is the batch's Append method.
// Takes parameters ([]*sqlmodel.QueryParameter) which are the row's parameters, in the
// order they are appended.
// Takes values ([]ast.Expr) which read the parameters' values, parallel to parameters.
//
// Returns ast.Expr which is the call.
func AppendRow(vocabulary helpers.Vocabulary, appendRow ast.Expr, parameters []*sqlmodel.QueryParameter, values []ast.Expr) ast.Expr {
	arguments := append([]ast.Expr{appendRow, goastutil.StrLit(rowShape(parameters))}, values...)
	return goastutil.CallExpr(goastutil.CachedIdent(vocabulary.Helper(suffixAppendRow)), arguments...)
}

// ArrayLists changes typed slice placeholders to ClickHouse array types.
//
// This allows either driver to bind a whole slice for an IN list. Queries are copied only
// when a placeholder changes.
//
// Takes queries ([]*sqlmodel.AnalysedQuery) which are the queries.
//
// Returns []*sqlmodel.AnalysedQuery which holds the queries to generate from.
func ArrayLists(queries []*sqlmodel.AnalysedQuery) []*sqlmodel.AnalysedQuery {
	var changed []*sqlmodel.AnalysedQuery
	for index, query := range queries {
		if !holdsTypedList(query.Template.Segments) && (query.CountTemplate == nil || !holdsTypedList(query.CountTemplate.Segments)) {
			continue
		}
		if changed == nil {
			changed = append([]*sqlmodel.AnalysedQuery(nil), queries...)
		}
		copied := *query
		copied.Template.Segments = arrayListSegments(query.Template.Segments)
		if query.CountTemplate != nil {
			count := *query.CountTemplate
			count.Segments = arrayListSegments(count.Segments)
			copied.CountTemplate = &count
		}
		changed[index] = &copied
	}
	if changed == nil {
		return queries
	}
	return changed
}

// networkShape returns the shape of a network type: an IP address of a type holding only
// IPv4 ones, an address of a type holding either version, or text for a network or a
// hardware address.
//
// Takes sqlType (*sqlmodel.SQLType) which is the network type.
//
// Returns string which is the shape.
func networkShape(sqlType *sqlmodel.SQLType) string {
	switch {
	case sqlType.Network != sqlmodel.NetworkKindAddress:
		return shapeText
	case sqlType.Width == bitsInIPv4:
		return shapeIPv4
	default:
		return shapeAddress
	}
}

// holdsAddress reports whether a type holds an IP address at any depth, which the value
// helper and the conversion of appended values check against the type, within a bounded
// depth.
//
// Takes sqlType (*sqlmodel.SQLType) which is the type, or nil.
// Takes depth (int) which is how deep the type is nested.
//
// Returns bool which is true when the SQL type contains an address.
func holdsAddress(sqlType *sqlmodel.SQLType, depth int) bool {
	if sqlType == nil || depth >= sqlmodel.MaxTypeDepth {
		return false
	}
	switch sqlType.Category {
	case sqlmodel.TypeCategoryNetwork:
		return sqlType.Network == sqlmodel.NetworkKindAddress
	case sqlmodel.TypeCategoryArray, sqlmodel.TypeCategoryMap:
		return holdsAddress(sqlType.KeyType, depth+1) || holdsAddress(sqlType.ElementType, depth+1)
	case sqlmodel.TypeCategoryStruct:
		for index := range sqlType.StructFields {
			if holdsAddress(&sqlType.StructFields[index].SQLType, depth+1) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// heldByNullablePointer reports whether a parameter may be NULL and the generated code
// holds it through a pointer, nil for NULL.
//
// Takes parameter (*sqlmodel.QueryParameter) which is the parameter.
// Takes types (sqlmodel.GoTypeResolver) which maps the request's SQL types.
//
// Returns bool which is true for such a parameter.
func heldByNullablePointer(parameter *sqlmodel.QueryParameter, types sqlmodel.GoTypeResolver) bool {
	if !parameter.Nullable || parameter.IsSlice {
		return false
	}
	return strings.HasPrefix(gogen.ChosenGoType(types, parameter.GoTypeOverride, parameter.SQLType, true).Name, "*")
}

// holdsTimeOfDay reports whether a type is a time of day or holds one as an element of an
// Array, a key or value of a Map or an element of a Tuple.
//
// Takes sqlType (*sqlmodel.SQLType) which is the type, or nil.
// Takes depth (int) which bounds the walk.
//
// Returns bool which is true for a time of day or a composite containing one.
func holdsTimeOfDay(sqlType *sqlmodel.SQLType, depth int) bool {
	if sqlType == nil || depth >= sqlmodel.MaxTypeDepth {
		return false
	}
	if sqlType.Category == sqlmodel.TypeCategoryTemporal {
		return sqlType.Temporal == sqlmodel.TemporalKindTime
	}
	if holdsTimeOfDay(sqlType.ElementType, depth+1) || holdsTimeOfDay(sqlType.KeyType, depth+1) {
		return true
	}
	for index := range sqlType.StructFields {
		if holdsTimeOfDay(&sqlType.StructFields[index].SQLType, depth+1) {
			return true
		}
	}
	return false
}

// rowShape describes the ordered parameters of a checked native batch row.
//
// Takes parameters ([]*sqlmodel.QueryParameter) which holds row parameters in append
// order.
//
// Returns string which is the named tuple descriptor for the row.
func rowShape(parameters []*sqlmodel.QueryParameter) string {
	parts := make([]string, len(parameters))
	for index, parameter := range parameters {
		shape := shapeUnknown
		if Appends(&parameter.SQLType) {
			shape = ParameterShape(parameter)
		}
		parts[index] = quotedName(parameter.Name) + "=" + shape
	}
	return "(" + strings.Join(parts, ",") + ")"
}

// holdsMapOfTuples reports whether a type holds a Map whose values hold a tuple at any
// depth, within a bounded depth.
//
// Takes sqlType (*sqlmodel.SQLType) which is the type, or nil.
// Takes inMap (bool) which reports that the type is within a Map's values.
// Takes depth (int) which is how deep the type is nested.
//
// Returns bool which is true when the SQL type contains one.
func holdsMapOfTuples(sqlType *sqlmodel.SQLType, inMap bool, depth int) bool {
	if sqlType == nil || depth >= sqlmodel.MaxTypeDepth {
		return false
	}
	switch sqlType.Category {
	case sqlmodel.TypeCategoryMap:
		return holdsMapOfTuples(sqlType.ElementType, true, depth+1)
	case sqlmodel.TypeCategoryArray:
		return holdsMapOfTuples(sqlType.ElementType, inMap, depth+1)
	case sqlmodel.TypeCategoryStruct:
		if inMap {
			return true
		}
		for index := range sqlType.StructFields {
			if holdsMapOfTuples(&sqlType.StructFields[index].SQLType, false, depth+1) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// holdsTypedList reports whether segments hold, at any depth, a list whose placeholder
// carries a type.
//
// Takes segments ([]sqlmodel.SQLSegment) which are the segments.
//
// Returns bool which is true when a typed list is found.
func holdsTypedList(segments []sqlmodel.SQLSegment) bool {
	for index := range segments {
		segment := &segments[index]
		if segment.Kind == sqlmodel.SegmentList && segment.TypeText != "" || holdsTypedList(segment.Children) {
			return true
		}
	}
	return false
}

// arrayListSegments copies segments, typing every typed list's placeholder as an array.
//
// Takes segments ([]sqlmodel.SQLSegment) which are the segments.
//
// Returns []sqlmodel.SQLSegment which holds the copies.
func arrayListSegments(segments []sqlmodel.SQLSegment) []sqlmodel.SQLSegment {
	if segments == nil {
		return nil
	}
	copied := make([]sqlmodel.SQLSegment, len(segments))
	for index := range segments {
		copied[index] = segments[index]
		copied[index].Children = arrayListSegments(segments[index].Children)
		if copied[index].Kind == sqlmodel.SegmentList && copied[index].TypeText != "" {
			copied[index].TypeText = arrayTypeOpen + copied[index].TypeText + arrayTypeClose
		}
	}
	return copied
}

// temporalShape returns the shape of a temporal type: a calendar date, a date and time
// with the type's fraction digits, or text for a time of day or an interval.
//
// Takes sqlType (*sqlmodel.SQLType) which is the temporal type.
//
// Returns string which is the shape.
func temporalShape(sqlType *sqlmodel.SQLType) string {
	switch sqlType.Temporal {
	case sqlmodel.TemporalKindDate:
		return shapeDate
	case sqlmodel.TemporalKindTimestamp:
		if sqlType.Precision == nil || *sqlType.Precision <= 0 {
			return shapeDateTime
		}
		return shapeDateTime + strconv.Itoa(min(*sqlType.Precision, maximumFractionDigits))
	default:
		return shapeText
	}
}

// elementShape returns the shape of a nested type, or the unknown shape when there is
// none.
//
// Takes sqlType (*sqlmodel.SQLType) which is the nested type, or nil.
//
// Returns string which is the shape.
func elementShape(sqlType *sqlmodel.SQLType) string {
	if sqlType == nil {
		return shapeUnknown
	}
	return Shape(sqlType)
}

// tupleShape returns the shape of a tuple: its fields' shapes in order, each after its
// name in quotes when it has one.
//
// Takes fields ([]sqlmodel.StructField) which are the tuple's fields.
//
// Returns string which is the shape, or the unknown shape for a tuple without fields.
func tupleShape(fields []sqlmodel.StructField) string {
	if len(fields) == 0 {
		return shapeUnknown
	}
	parts := make([]string, len(fields))
	for index := range fields {
		parts[index] = Shape(&fields[index].SQLType)
		if name := fields[index].Name; name != "" {
			parts[index] = quotedName(name) + "=" + parts[index]
		}
	}
	return "(" + strings.Join(parts, ",") + ")"
}

// quotedName returns a tuple element's name as a shape writes it: between quotes, a quote
// within it doubled.
//
// Takes name (string) which is the name.
//
// Returns string which is the quoted name.
func quotedName(name string) string {
	return "'" + strings.ReplaceAll(name, "'", "''") + "'"
}
