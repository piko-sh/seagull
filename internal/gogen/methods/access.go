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

package methods

import (
	"go/ast"

	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// ParameterAccess returns the expression that reads one of a query's parameters in its
// method: the method's argument for a parameter passed on its own, or the field of the
// Params argument, as the naming plan names them.
//
// Takes query (*gogen.Query) which is the query.
// Takes index (int) which is the index into AnalysedQuery.Parameters.
//
// Returns ast.Expr which reads the parameter.
func ParameterAccess(query *gogen.Query, index int) ast.Expr {
	if query.Naming.Params == "" {
		return goastutil.CachedIdent(query.Naming.ParameterFields[0])
	}
	return ParameterFrom(query, goastutil.CachedIdent(query.Naming.ParamsArgument), index)
}

// ParameterFrom returns the expression that reads one of a query's parameters from a
// value holding them: the field of a Params struct, or the value itself when the query
// passes its one parameter on its own.
//
// Takes query (*gogen.Query) which is the query.
// Takes holder (ast.Expr) which reads the Params struct, or the lone parameter.
// Takes index (int) which is the index into AnalysedQuery.Parameters.
//
// Returns ast.Expr which reads the parameter.
func ParameterFrom(query *gogen.Query, holder ast.Expr, index int) ast.Expr {
	if query.Naming.Params == "" {
		return holder
	}
	return goastutil.SelectorExprFrom(holder, query.Naming.ParameterFields[index])
}

// ParameterType returns the Go type a parameter is passed as: an int for a pagination
// bound, the go_type override, or the mapped type, as a slice for a slice parameter.
//
// Takes query (*gogen.Query) which is the query.
// Takes parameter (*sqlmodel.QueryParameter) which is the parameter.
//
// Returns ast.Expr which is the type.
func ParameterType(query *gogen.Query, parameter *sqlmodel.QueryParameter) ast.Expr {
	typeExpression := query.Imports.AddType(parameterGoType(parameter, query.Types))
	if parameter.IsSlice {
		return &ast.ArrayType{Elt: typeExpression}
	}
	return typeExpression
}

// SQLConstant returns the identifier of a query's SQL constant.
//
// Takes query (*gogen.Query) which is the query.
//
// Returns *ast.Ident which names the constant.
func SQLConstant(query *gogen.Query) *ast.Ident {
	return goastutil.CachedIdent(query.Naming.SQLConstant)
}

// parameterGoType resolves the Go type of a parameter, or of each element of a slice
// parameter: a pagination bound is an int, matching across arities so a LIMIT argument
// has the same type whether or not the query has other parameters, and a go_type override
// wins over the mapping.
//
// Takes parameter (*sqlmodel.QueryParameter) which is the parameter.
// Takes mappings (sqlmodel.GoTypeResolver) which maps SQL types to Go types.
//
// Returns sqlmodel.GoType which is the type.
func parameterGoType(parameter *sqlmodel.QueryParameter, mappings sqlmodel.GoTypeResolver) sqlmodel.GoType {
	if parameter.IsPaginationBound() {
		return sqlmodel.GoType{Composite: nil, Name: goast.IdentInt, Package: ""}
	}
	if override := goast.NullableOverride(parameter.GoTypeOverride, parameter.SQLType, parameter.Nullable); override != nil {
		return *override
	}
	return goast.ResolveGoType(parameter.SQLType, parameter.Nullable, mappings)
}
