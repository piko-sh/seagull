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

	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/sqlmodel"
)

// unsignedBindingWidth is the width of an unsigned integer whose high values fall outside
// the signed range database/sql converts.
const unsignedBindingWidth = 64

// BindElement keeps exact unsigned values and NULL byte slices in expanded lists.
//
// Takes access (ast.Expr) which reads a scalar slice element.
// Takes parameter (*sqlmodel.QueryParameter) which holds the SQL element type.
//
// Returns ast.Expr which wraps the element when the driver needs it.
func (s *sqlDriver) BindElement(access ast.Expr, parameter *sqlmodel.QueryParameter) ast.Expr {
	if bindsNullableBinary(s.profile, parameter) {
		return driverBinaryAccess(s.vocabulary, access)
	}
	if bindsAsUnsigned(s.profile, parameter) {
		return driverUnsignedAccess(s.vocabulary, access)
	}
	return access
}

// BindRuntimeArguments keeps exact unsigned values and NULL byte slices in runtime
// filters.
//
// Takes access (ast.Expr) which reads the assembled argument slice.
//
// Returns ast.Expr which converts the arguments only for drivers that need it.
func (s *sqlDriver) BindRuntimeArguments(access ast.Expr) ast.Expr {
	if s.profile.bindsUnsignedAsText {
		access = goastutil.CallExpr(goastutil.CachedIdent(s.vocabulary.Helper("UnsignedArguments")), access)
	}
	if s.profile.bindsNilSlicesAsEmpty {
		access = goastutil.CallExpr(goastutil.CachedIdent(s.vocabulary.Helper("BinaryArguments")), access)
	}
	return access
}

// bindsAsUnsigned reports whether a parameter is a wide unsigned integer the driver binds
// through exact decimal text.
//
// Takes profile (*driverProfile) which describes native driver binding.
// Takes parameter (*sqlmodel.QueryParameter) which holds the SQL type.
//
// Returns bool which is true for such a parameter, or for the elements of such a slice.
func bindsAsUnsigned(profile *driverProfile, parameter *sqlmodel.QueryParameter) bool {
	sqlType := &parameter.SQLType
	return profile.bindsUnsignedAsText && sqlType.Category == sqlmodel.TypeCategoryInteger && sqlType.Unsigned &&
		sqlType.Width == unsignedBindingWidth
}

// anyParameterBindsAsUnsigned reports whether the package needs the unsigned wrapper.
//
// Takes profile (*driverProfile) which describes native driver binding.
// Takes queries ([]*sqlmodel.AnalysedQuery) which holds the emitted queries.
//
// Returns bool which is true when a runtime builder or a wide unsigned parameter needs
// it.
func anyParameterBindsAsUnsigned(profile *driverProfile, queries []*sqlmodel.AnalysedQuery) bool {
	if !profile.bindsUnsignedAsText {
		return false
	}
	for _, query := range queries {
		if query.DynamicRuntime {
			return true
		}
		for index := range query.Parameters {
			if bindsAsUnsigned(profile, &query.Parameters[index]) {
				return true
			}
		}
	}
	return false
}

// driverUnsignedAccess wraps a parameter access in the unsigned wrapper.
//
// Takes vocabulary (gogen.Vocabulary) which names the generated helpers.
// Takes access (ast.Expr) which reads a scalar or an expanded slice element.
//
// Returns ast.Expr which is the wrapped access.
func driverUnsignedAccess(vocabulary gogen.Vocabulary, access ast.Expr) ast.Expr {
	return &ast.CompositeLit{
		Type: goastutil.CachedIdent(vocabulary.Helper("DriverUnsigned")),
		Elts: []ast.Expr{&ast.KeyValueExpr{Key: goastutil.CachedIdent(identWrappedValue), Value: access}},
	}
}
