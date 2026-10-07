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

package sqlite

import (
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// operatorConcatenate joins two values as text.
	operatorConcatenate = "||"

	// operatorJSONExtract extracts a JSON value by path, as JSON text.
	operatorJSONExtract = "->"

	// operatorJSONValue extracts a JSON value by path as the SQL value it holds.
	operatorJSONValue = "->>"
)

var (
	// operatorFeatures names the expression feature each SQLite operator that needs one
	// requires. IS and IS NOT compare NULL as a value, so they are NULL tests.
	operatorFeatures = map[string]ast.ExpressionFeature{
		"IS":                ast.SQLFeatureIsNull,
		"IS NOT":            ast.SQLFeatureIsNull,
		operatorConcatenate: ast.SQLFeatureStringConcat,
		operatorJSONExtract: ast.SQLFeatureJSONOp,
		operatorJSONValue:   ast.SQLFeatureJSONOp,
		"&":                 ast.SQLFeatureBitwiseOp,
		"|":                 ast.SQLFeatureBitwiseOp,
		"<<":                ast.SQLFeatureBitwiseOp,
		">>":                ast.SQLFeatureBitwiseOp,
		"LIKE":              ast.SQLFeaturePatternMatch,
		"GLOB":              ast.SQLFeaturePatternMatch,
		"REGEXP":            ast.SQLFeaturePatternMatch,
		"MATCH":             ast.SQLFeaturePatternMatch,
	}

	// bitOperators are SQLite's bit operators, which work on 64-bit integers.
	bitOperators = []string{"&", "|", "<<", ">>"}

	// rowIDAliases are the names of the implicit rowid column of an ordinary table.
	rowIDAliases = []string{"rowid", "_rowid_", "oid"}
)

// coreTypes returns SQLite's core types, each through the dialect's normaliser: BOOLEAN,
// TEXT, BIGINT for counts and the rowid, JSON, INTEGER for a LIMIT or OFFSET parameter,
// and no void type.
//
// Returns dialect.CoreTypes which holds the core types.
func (engine *Dialect) coreTypes() dialect.CoreTypes {
	return dialect.CoreTypes{
		Boolean:  engine.NormaliseTypeName("boolean"),
		Text:     engine.NormaliseTypeName("text"),
		Count:    engine.NormaliseTypeName(typeNameBigint),
		JSON:     engine.NormaliseTypeName("json"),
		RowCount: engine.NormaliseTypeName(typeNameInteger),
		RowID:    engine.NormaliseTypeName(typeNameBigint),
		Void:     sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""),
	}
}

// binaryResult types SQLite operators whose results differ from their operands' common
// type.
//
// Takes operator (string) which is the operator.
//
// Returns sqlmodel.SQLType which is the result's type.
// Returns sqlmodel.FunctionNullableBehaviour which says when the result is NULL.
// Returns bool which is false for an operator left to the structural default.
func (engine *Dialect) binaryResult(operator string, _ sqlmodel.SQLType) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour, bool) {
	switch {
	case operator == operatorConcatenate:
		return engine.NormaliseTypeName("text"), sqlmodel.FunctionNullableReturnsNullOnNull, true
	case slices.Contains(bitOperators, operator):
		return engine.NormaliseTypeName(typeNameBigint), sqlmodel.FunctionNullableReturnsNullOnNull, true
	case operator == operatorJSONExtract:
		return engine.NormaliseTypeName("json"), sqlmodel.FunctionNullableCalledOnNull, true
	case operator == operatorJSONValue:
		return engine.NormaliseTypeName("any"), sqlmodel.FunctionNullableCalledOnNull, true
	default:
		return sqlmodel.SQLType{}, sqlmodel.FunctionNullableReturnsNullOnNull, false
	}
}

// unaryResult types NOT as a boolean that is NULL when its operand is and ~ as a 64-bit
// integer, and leaves every other prefix operator to the structural default.
//
// Takes operator (string) which is the operator.
//
// Returns sqlmodel.SQLType which is the result's type.
// Returns sqlmodel.FunctionNullableBehaviour which says when the result is NULL.
// Returns bool which is false for an operator left to the structural default.
func (engine *Dialect) unaryResult(operator string) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour, bool) {
	switch {
	case strings.EqualFold(operator, keywordNOT):
		return engine.NormaliseTypeName("boolean"), sqlmodel.FunctionNullableReturnsNullOnNull, true
	case operator == "~":
		return engine.NormaliseTypeName(typeNameBigint), sqlmodel.FunctionNullableReturnsNullOnNull, true
	default:
		return sqlmodel.SQLType{}, sqlmodel.FunctionNullableReturnsNullOnNull, false
	}
}
