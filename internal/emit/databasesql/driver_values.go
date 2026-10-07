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

package databasesql

import (
	"go/ast"
	"slices"
	"strings"

	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// driverValueHelperFile is the generated file holding the driver-value wrapper.
	driverValueHelperFile = "driver_values.go"

	// suffixDriverValue is the helper-prefix suffix of the driver-value wrapper type.
	suffixDriverValue = "DriverValue"

	// identWrappedValue is the wrapper's field holding the parameter.
	identWrappedValue = "value"

	// suffixDriverText is the helper-prefix suffix of the driver-text wrapper type.
	suffixDriverText = "DriverText"

	// suffixDriverBinary is the helper-prefix suffix of the driver-binary wrapper type.
	suffixDriverBinary = "DriverBinary"

	// suffixDriverDecimal is the helper-prefix suffix of the driver-decimal wrapper type.
	suffixDriverDecimal = "DriverDecimalValue"

	// suffixScanOverride is the helper-prefix suffix of the override scan adapter.
	suffixScanOverride = "ScanOverride"

	// suffixScanOverrideJSON is the helper-prefix suffix of the scan adapter decoding a JSON
	// column's document into the Go type an override chose.
	suffixScanOverrideJSON = "ScanOverrideJSON"

	// suffixDriverJSON is the helper-prefix suffix of the wrapper binding a JSON parameter
	// as the document it encodes to.
	suffixDriverJSON = "DriverJSON"

	// driverCompositeHelperFile is the generated file holding the driver-composite
	// conversion.
	driverCompositeHelperFile = "driver_composites.go"

	// suffixDriverComposite is the helper-prefix suffix of the driver-composite conversion.
	suffixDriverComposite = "DriverComposite"

	// suffixScanDriverComposite is the helper-prefix suffix of the driver-composite scan
	// adapter.
	suffixScanDriverComposite = "ScanDriverComposite"

	// suffixDriverInferredList is the helper-prefix suffix of the list-inference wrapper.
	suffixDriverInferredList = "DriverInferredList"

	// byteWidth is the width in bits of an integer whose Go slice database/sql takes for
	// bytes.
	byteWidth = 8
)

// decomposedDecimals lists the decimal types whose pointer database/sql hands a driver as
// it is: the pointer has a Decompose method, which database/sql takes for a value the
// driver binds itself, though only pgx does. apd's Decimal is the default decimal slot's
// type.
var decomposedDecimals = []sqlmodel.GoType{
	{Composite: nil, Package: "github.com/cockroachdb/apd/v3", Name: "Decimal"},
}

// bindsAsDriverDecimal reports decimal parameters that need a driver.Valuer wrapper. It
// wraps optional or pointer-held decomposable decimals when the driver cannot bind them
// itself, so nil binds NULL.
//
// Takes profile (*driverProfile) which is the driver's profile.
// Takes parameter (*sqlmodel.QueryParameter) which is the parameter.
// Takes types (sqlmodel.GoTypeResolver) which maps the request's SQL types.
//
// Returns bool which is true when the parameter is wrapped.
func bindsAsDriverDecimal(profile *driverProfile, parameter *sqlmodel.QueryParameter, types sqlmodel.GoTypeResolver) bool {
	switch {
	case profile.bindsDecomposedDecimals || parameter.IsSlice:
		return false
	case parameter.Nullable:
		bound := gogen.ChosenGoType(types, parameter.GoTypeOverride, parameter.SQLType, true)
		return strings.HasPrefix(bound.Name, "*") && decomposesItself(bound)
	default:
		return parameter.IsOptional && decomposesItself(gogen.ChosenGoType(types, parameter.GoTypeOverride, parameter.SQLType, false))
	}
}

// decomposesItself reports whether a Go type, or the type a pointer type points to, is
// one of decomposedDecimals.
//
// Takes goType (sqlmodel.GoType) which is the Go type.
//
// Returns bool which is true for a listed decimal type or a pointer to one.
func decomposesItself(goType sqlmodel.GoType) bool {
	return goType.Composite == nil && slices.ContainsFunc(decomposedDecimals, func(decimal sqlmodel.GoType) bool {
		return goType.Package == decimal.Package && strings.TrimPrefix(goType.Name, "*") == decimal.Name
	})
}

// anyParameterBindsAsDriverDecimal reports whether any query binds a parameter through
// the driver-decimal wrapper, so the package needs the wrapper.
//
// Takes profile (*driverProfile) which is the driver's profile.
// Takes queries ([]*sqlmodel.AnalysedQuery) which are the queries.
// Takes types (sqlmodel.GoTypeResolver) which maps the request's SQL types.
//
// Returns bool which is true when some parameter is wrapped.
func anyParameterBindsAsDriverDecimal(profile *driverProfile, queries []*sqlmodel.AnalysedQuery, types sqlmodel.GoTypeResolver) bool {
	for _, query := range queries {
		for index := range query.Parameters {
			if bindsAsDriverDecimal(profile, &query.Parameters[index], types) {
				return true
			}
		}
	}
	return false
}

// driverDecimalAccess wraps a parameter access as `sglDriverDecimalValue{value: access}`.
//
// Takes vocabulary (gogen.Vocabulary) which names the wrapper.
// Takes access (ast.Expr) which reads the parameter.
//
// Returns ast.Expr which is the wrapped access.
func driverDecimalAccess(vocabulary gogen.Vocabulary, access ast.Expr) ast.Expr {
	return &ast.CompositeLit{
		Type: goastutil.CachedIdent(vocabulary.Helper(suffixDriverDecimal)),
		Elts: []ast.Expr{&ast.KeyValueExpr{Key: goastutil.CachedIdent(identWrappedValue), Value: access}},
	}
}

// bindsThroughValuer reports whether a parameter is one the driver's profile hands over
// through the driver-value wrapper: a JSON value, on a driver that binds a named slice
// type as it is rather than through its driver.Valuer.
//
// Takes profile (*driverProfile) which is the driver's profile.
// Takes parameter (*sqlmodel.QueryParameter) which is the parameter.
//
// Returns bool which is true when the parameter is wrapped.
func bindsThroughValuer(profile *driverProfile, parameter *sqlmodel.QueryParameter) bool {
	if !profile.bindsJSONThroughValuer || parameter.IsSlice {
		return false
	}
	category := parameter.SQLType.Category
	return category == sqlmodel.TypeCategoryJSON || category == sqlmodel.TypeCategoryUnion
}

// anyParameterBindsThroughValuer reports whether any query binds a parameter through the
// driver-value wrapper, so the package needs the wrapper's file.
//
// Takes profile (*driverProfile) which is the driver's profile.
// Takes queries ([]*sqlmodel.AnalysedQuery) which are the queries.
// Takes types (sqlmodel.GoTypeResolver) which maps the request's SQL types.
//
// Returns bool which is true when some parameter is wrapped.
func anyParameterBindsThroughValuer(profile *driverProfile, queries []*sqlmodel.AnalysedQuery, types sqlmodel.GoTypeResolver) bool {
	for _, query := range queries {
		for index := range query.Parameters {
			parameter := &query.Parameters[index]
			if !wrapsAsJSONDocument(profile, parameter, types) && !bindsAsDriverBinary(profile, parameter, types) && bindsThroughValuer(profile, parameter) {
				return true
			}
		}
	}
	return false
}

// driverValueAccess wraps a parameter access as `sglDriverValue{value: access}`.
//
// Takes vocabulary (gogen.Vocabulary) which names the wrapper.
// Takes access (ast.Expr) which reads the parameter.
//
// Returns ast.Expr which is the wrapped access.
func driverValueAccess(vocabulary gogen.Vocabulary, access ast.Expr) ast.Expr {
	return &ast.CompositeLit{
		Type: goastutil.CachedIdent(vocabulary.Helper(suffixDriverValue)),
		Elts: []ast.Expr{&ast.KeyValueExpr{Key: goastutil.CachedIdent(identWrappedValue), Value: access}},
	}
}

// bindsAsJSONDocument reports a JSON parameter the driver-JSON wrapper binds as the
// document it encodes to, as gogen.EncodesJSONDocument says.
//
// Takes parameter (*sqlmodel.QueryParameter) which is the parameter.
// Takes types (sqlmodel.GoTypeResolver) which maps the request's SQL types.
//
// Returns bool which is true when the parameter is wrapped.
func bindsAsJSONDocument(parameter *sqlmodel.QueryParameter, types sqlmodel.GoTypeResolver) bool {
	return gogen.EncodesJSONDocument(types, parameter)
}

// wrapsAsJSONDocument reports whether Bind wraps a parameter in the driver-JSON wrapper
// ahead of the other wrappers: a JSON parameter bindsAsJSONDocument reports, on a driver
// that does not format every value itself as a named value.
//
// Takes profile (*driverProfile) which is the driver's profile.
// Takes parameter (*sqlmodel.QueryParameter) which is the parameter.
// Takes types (sqlmodel.GoTypeResolver) which maps the request's SQL types.
//
// Returns bool which is true when the parameter is wrapped so.
func wrapsAsJSONDocument(profile *driverProfile, parameter *sqlmodel.QueryParameter, types sqlmodel.GoTypeResolver) bool {
	return !profile.bindsNamedValues && bindsAsJSONDocument(parameter, types)
}

// anyParameterBindsAsJSONDocument reports whether any query binds a JSON parameter
// through the driver-JSON wrapper, so the package needs the wrapper. A driver binding
// named values formats every value itself, so it wraps none.
//
// Takes profile (*driverProfile) which is the driver's profile.
// Takes info (*dialect.Facts) which describes the dialect's placeholders.
// Takes queries ([]*sqlmodel.AnalysedQuery) which are the queries.
// Takes types (sqlmodel.GoTypeResolver) which maps the request's SQL types.
//
// Returns bool which is true when some parameter is wrapped.
func anyParameterBindsAsJSONDocument(profile *driverProfile, info *dialect.Facts, queries []*sqlmodel.AnalysedQuery,
	types sqlmodel.GoTypeResolver) bool {
	if bindsNamedValues(profile, info.Placeholders) {
		return false
	}
	for _, query := range queries {
		for index := range query.Parameters {
			if bindsAsJSONDocument(&query.Parameters[index], types) {
				return true
			}
		}
	}
	return false
}

// driverJSONAccess wraps a parameter access as `sglDriverJSON{value: access}`.
//
// Takes vocabulary (gogen.Vocabulary) which names the wrapper.
// Takes access (ast.Expr) which reads the parameter.
//
// Returns ast.Expr which is the wrapped access.
func driverJSONAccess(vocabulary gogen.Vocabulary, access ast.Expr) ast.Expr {
	return &ast.CompositeLit{
		Type: goastutil.CachedIdent(vocabulary.Helper(suffixDriverJSON)),
		Elts: []ast.Expr{&ast.KeyValueExpr{Key: goastutil.CachedIdent(identWrappedValue), Value: access}},
	}
}

// bindsAsDriverComposite reports parameters requiring conversion to the driver's
// composite forms.
//
// This covers driver-specific Maps and Structs, nullable lists, JSON list elements and
// selected defined-type overrides.
//
// Takes profile (*driverProfile) which is the driver's profile.
// Takes parameter (*sqlmodel.QueryParameter) which is the parameter.
// Takes types (sqlmodel.GoTypeResolver) which maps the request's SQL types.
//
// Returns bool which is true when the parameter is converted.
func bindsAsDriverComposite(profile *driverProfile, parameter *sqlmodel.QueryParameter, types sqlmodel.GoTypeResolver) bool {
	if parameter.IsSlice {
		return false
	}
	nullableList := parameter.Nullable && parameter.SQLType.Category == sqlmodel.TypeCategoryArray
	if profile.bindsCompositesAsDriverValues && holdsMapOrStruct(&parameter.SQLType) || profile.bindsNilSlicesAsEmpty && nullableList {
		return true
	}
	if profile.bindsJSONThroughValuer && listsJSON(&parameter.SQLType) {
		return true
	}
	return profile.bindsDefinedTypesAsIs && slices.Contains(convertedOverrides, parameter.SQLType.Category) &&
		gogen.OverrideChosen(types, parameter.GoTypeOverride, parameter.SQLType) &&
		!gogen.PredeclaredGoType(gogen.ChosenGoType(types, parameter.GoTypeOverride, parameter.SQLType, parameter.Nullable))
}

// bindsAsDriverText reports overridden text or JSON parameters requiring a text wrapper.
//
// The wrapper converts byte-backed values, including json.RawMessage, to text before
// binding.
//
// Takes profile (*driverProfile) which is the driver's profile.
// Takes parameter (*sqlmodel.QueryParameter) which is the parameter.
// Takes types (sqlmodel.GoTypeResolver) which maps the request's SQL types.
//
// Returns bool which is true when the parameter is wrapped.
func bindsAsDriverText(profile *driverProfile, parameter *sqlmodel.QueryParameter, types sqlmodel.GoTypeResolver) bool {
	category := parameter.SQLType.Category
	if !profile.bindsBytesAsBinary || parameter.IsSlice || category != sqlmodel.TypeCategoryText && category != sqlmodel.TypeCategoryJSON {
		return false
	}
	if !gogen.OverrideChosen(types, parameter.GoTypeOverride, parameter.SQLType) {
		return false
	}
	goType := gogen.ChosenGoType(types, parameter.GoTypeOverride, parameter.SQLType, parameter.Nullable)
	return !gogen.PredeclaredGoType(goType) || holdsBytes(goType)
}

// bindsAsDriverBinary reports a binary parameter whose Go type an override chose, which
// the driver-binary wrapper binds as the bytes the value marshals itself to: a type of
// Go's own binds as it is, and so does a parameter a driver binding named values formats
// itself.
//
// Takes profile (*driverProfile) which is the driver's profile.
// Takes parameter (*sqlmodel.QueryParameter) which is the parameter.
// Takes types (sqlmodel.GoTypeResolver) which maps the request's SQL types.
//
// Returns bool which is true when the parameter is wrapped.
func bindsAsDriverBinary(profile *driverProfile, parameter *sqlmodel.QueryParameter, types sqlmodel.GoTypeResolver) bool {
	if profile.bindsNamedValues || parameter.IsSlice || parameter.SQLType.Category != sqlmodel.TypeCategoryBinary {
		return false
	}
	return gogen.OverrideChosen(types, parameter.GoTypeOverride, parameter.SQLType) &&
		!gogen.PredeclaredGoType(gogen.ChosenGoType(types, parameter.GoTypeOverride, parameter.SQLType, parameter.Nullable))
}

// anyParameterBindsAsDriverBinary reports whether any query binds a parameter through the
// driver-binary wrapper, so the package needs the wrapper.
//
// Takes profile (*driverProfile) which is the driver's profile.
// Takes queries ([]*sqlmodel.AnalysedQuery) which are the queries.
// Takes types (sqlmodel.GoTypeResolver) which maps the request's SQL types.
//
// Returns bool which is true when some parameter is wrapped.
func anyParameterBindsAsDriverBinary(profile *driverProfile, queries []*sqlmodel.AnalysedQuery, types sqlmodel.GoTypeResolver) bool {
	for _, query := range queries {
		for index := range query.Parameters {
			if bindsAsDriverBinary(profile, &query.Parameters[index], types) {
				return true
			}
		}
	}
	return false
}

// driverBinaryAccess wraps a parameter access as `sglDriverBinary{value: access}`.
//
// Takes vocabulary (gogen.Vocabulary) which names the wrapper.
// Takes access (ast.Expr) which reads the parameter.
//
// Returns ast.Expr which is the wrapped access.
func driverBinaryAccess(vocabulary gogen.Vocabulary, access ast.Expr) ast.Expr {
	return &ast.CompositeLit{
		Type: goastutil.CachedIdent(vocabulary.Helper(suffixDriverBinary)),
		Elts: []ast.Expr{&ast.KeyValueExpr{Key: goastutil.CachedIdent(identWrappedValue), Value: access}},
	}
}

// holdsBytes reports whether a predeclared Go type is a byte slice or a pointer to one.
//
// Takes goType (sqlmodel.GoType) which is the Go type.
//
// Returns bool which is true for []byte and *[]byte.
func holdsBytes(goType sqlmodel.GoType) bool {
	name := strings.TrimPrefix(goType.Name, "*")
	return name == "[]byte" || name == "[]uint8"
}

// anyParameterBindsAsDriverText reports whether any query binds a parameter through the
// driver-text wrapper, so the package needs the wrapper.
//
// Takes profile (*driverProfile) which is the driver's profile.
// Takes queries ([]*sqlmodel.AnalysedQuery) which are the queries.
// Takes types (sqlmodel.GoTypeResolver) which maps the request's SQL types.
//
// Returns bool which is true when some parameter is wrapped.
func anyParameterBindsAsDriverText(profile *driverProfile, queries []*sqlmodel.AnalysedQuery, types sqlmodel.GoTypeResolver) bool {
	for _, query := range queries {
		for index := range query.Parameters {
			parameter := &query.Parameters[index]
			if !wrapsAsJSONDocument(profile, parameter, types) && !bindsAsDriverBinary(profile, parameter, types) &&
				!bindsThroughValuer(profile, parameter) && bindsAsDriverText(profile, parameter, types) {
				return true
			}
		}
	}
	return false
}

// driverTextAccess wraps a parameter access as `sglDriverText{value: access}`.
//
// Takes vocabulary (gogen.Vocabulary) which names the wrapper.
// Takes access (ast.Expr) which reads the parameter.
//
// Returns ast.Expr which is the wrapped access.
func driverTextAccess(vocabulary gogen.Vocabulary, access ast.Expr) ast.Expr {
	return &ast.CompositeLit{
		Type: goastutil.CachedIdent(vocabulary.Helper(suffixDriverText)),
		Elts: []ast.Expr{&ast.KeyValueExpr{Key: goastutil.CachedIdent(identWrappedValue), Value: access}},
	}
}

// holdsMapOrStruct reports whether a type is a MAP or a STRUCT, or a list whose elements,
// at any depth, are.
//
// Takes sqlType (*sqlmodel.SQLType) which is the type.
//
// Returns bool which is true when the SQL type contains a MAP or a STRUCT.
func holdsMapOrStruct(sqlType *sqlmodel.SQLType) bool {
	switch sqlType.Category {
	case sqlmodel.TypeCategoryMap, sqlmodel.TypeCategoryStruct:
		return true
	case sqlmodel.TypeCategoryArray:
		return sqlType.ElementType != nil && holdsMapOrStruct(sqlType.ElementType)
	default:
		return false
	}
}

// listsJSON reports whether a type is a list whose elements, at any depth, are JSON
// values or unions, which the generated code holds in its JSON type.
//
// Takes sqlType (*sqlmodel.SQLType) which is the type.
//
// Returns bool which is true when the type is a list of JSON values.
func listsJSON(sqlType *sqlmodel.SQLType) bool {
	if sqlType.Category != sqlmodel.TypeCategoryArray || sqlType.ElementType == nil {
		return false
	}
	switch element := sqlType.ElementType; element.Category {
	case sqlmodel.TypeCategoryJSON, sqlmodel.TypeCategoryUnion:
		return true
	default:
		return listsJSON(element)
	}
}

// anyParameterBindsAsDriverComposite reports whether any query binds a parameter through
// the driver-composite conversion, so the package needs the conversion's file.
//
// Takes profile (*driverProfile) which is the driver's profile.
// Takes queries ([]*sqlmodel.AnalysedQuery) which are the queries.
// Takes types (sqlmodel.GoTypeResolver) which maps the request's SQL types.
//
// Returns bool which is true when some parameter is converted.
func anyParameterBindsAsDriverComposite(profile *driverProfile, queries []*sqlmodel.AnalysedQuery, types sqlmodel.GoTypeResolver) bool {
	for _, query := range queries {
		for index := range query.Parameters {
			parameter := &query.Parameters[index]
			if !wrapsAsJSONDocument(profile, parameter, types) && !bindsAsDriverBinary(profile, parameter, types) &&
				!bindsThroughValuer(profile, parameter) && !bindsAsDriverText(profile, parameter, types) &&
				bindsAsDriverComposite(profile, parameter, types) {
				return true
			}
		}
	}
	return false
}

// driverCompositeAccess wraps a parameter access as `sglDriverComposite(access)`.
//
// Takes vocabulary (gogen.Vocabulary) which names the conversion.
// Takes access (ast.Expr) which reads the parameter.
//
// Returns ast.Expr which is the converted access.
func driverCompositeAccess(vocabulary gogen.Vocabulary, access ast.Expr) ast.Expr {
	return goastutil.CallExpr(goastutil.CachedIdent(vocabulary.Helper(suffixDriverComposite)), access)
}

// bindsAsInferredList reports list parameters needing element conversion and type
// inference. These include nested lists of unsupported elements and uint8 lists that the
// driver would otherwise bind as binary data.
//
// Takes profile (*driverProfile) which is the driver's profile.
// Takes parameter (*sqlmodel.QueryParameter) which is the parameter.
//
// Returns bool which is true when the parameter is wrapped.
func bindsAsInferredList(profile *driverProfile, parameter *sqlmodel.QueryParameter) bool {
	if profile.buildsElement == nil || parameter.IsSlice || parameter.SQLType.Category != sqlmodel.TypeCategoryArray ||
		parameter.SQLType.ElementType == nil {
		return false
	}
	if profile.bindsBytesAsBinary && holdsByteIntegers(parameter.SQLType.ElementType) {
		return true
	}
	leaf := listLeaf(&parameter.SQLType)
	return leaf != nil && !profile.buildsElement(leaf)
}

// holdsByteIntegers reports whether a type is an unsigned 8-bit integer, which a Go
// []uint8 holds.
//
// Takes sqlType (*sqlmodel.SQLType) which is the type.
//
// Returns bool which is true for an unsigned 8-bit integer.
func holdsByteIntegers(sqlType *sqlmodel.SQLType) bool {
	return sqlType.Category == sqlmodel.TypeCategoryInteger && sqlType.Unsigned && sqlType.Width == byteWidth
}

// listLeaf returns the type of the elements a list holds through lists alone: its element
// type, or, for a list of lists, the innermost list's.
//
// Takes sqlType (*sqlmodel.SQLType) which is the list's type.
//
// Returns *sqlmodel.SQLType which is the elements' type, or nil when the walk ends
// without one.
func listLeaf(sqlType *sqlmodel.SQLType) *sqlmodel.SQLType {
	element := sqlType.ElementType
	for depth := 0; element != nil && element.Category == sqlmodel.TypeCategoryArray; depth++ {
		if depth >= sqlmodel.MaxTypeDepth {
			return nil
		}
		element = element.ElementType
	}
	return element
}

// anyParameterBindsAsInferredList reports whether any query binds a parameter through the
// list-inference wrapper, so the package needs it.
//
// Takes profile (*driverProfile) which is the driver's profile.
// Takes queries ([]*sqlmodel.AnalysedQuery) which are the queries.
//
// Returns bool which is true when some parameter is wrapped.
func anyParameterBindsAsInferredList(profile *driverProfile, queries []*sqlmodel.AnalysedQuery) bool {
	for _, query := range queries {
		for index := range query.Parameters {
			if bindsAsInferredList(profile, &query.Parameters[index]) {
				return true
			}
		}
	}
	return false
}

// inferredListAccess wraps a parameter access as `sglDriverInferredList(access)`.
//
// Takes vocabulary (gogen.Vocabulary) which names the wrapper.
// Takes access (ast.Expr) which reads the parameter.
//
// Returns ast.Expr which is the wrapped access.
func inferredListAccess(vocabulary gogen.Vocabulary, access ast.Expr) ast.Expr {
	return goastutil.CallExpr(goastutil.CachedIdent(vocabulary.Helper(suffixDriverInferredList)), access)
}
