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

package postgres

import (
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// recordTypeName is the name PostgreSQL gives a row of no declared type, which a
	// function returning a set of it must be given a column definition list for.
	recordTypeName = "record"

	// voidTypeName is the type of a function that returns nothing, and of a procedure
	// without output parameters.
	voidTypeName = "void"

	// multipleArrayUnnest is the function that, given several arrays, returns a record of
	// one column per array rather than a set of one array's elements.
	multipleArrayUnnest = "unnest"

	// keywordORDINALITY is the keyword of WITH ORDINALITY after a table function.
	keywordORDINALITY = "ORDINALITY"

	// ordinalityColumnName is the name PostgreSQL gives the column WITH ORDINALITY appends,
	// unless an alias column list names it.
	ordinalityColumnName = "ordinality"

	// ordinalityTypeName is the type of the column WITH ORDINALITY appends.
	ordinalityTypeName = "bigint"
)

var (
	// tableValuedFunctionColumns maps each known PostgreSQL table-valued function name to
	// the output columns it produces.
	tableValuedFunctionColumns = map[string][]ast.ScopedColumn{
		"generate_series": {
			{
				Name:     "generate_series",
				SQLType:  sqlmodel.NewIntegerType("int4", widthInteger, false),
				Nullable: false,
			},
		},
		"generate_subscripts": {
			{
				Name:     "generate_subscripts",
				SQLType:  sqlmodel.NewIntegerType("int4", widthInteger, false),
				Nullable: false,
			},
		},
		"unnest": {
			{Name: "unnest", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), Nullable: true},
		},
		"json_each": {
			{Name: "key", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"), Nullable: false},
			{Name: "value", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryJSON, "json"), Nullable: true},
		},
		"json_each_text": {
			{Name: "key", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"), Nullable: false},
			{Name: "value", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"), Nullable: true},
		},
		"jsonb_each": {
			{Name: "key", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"), Nullable: false},
			{Name: "value", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryJSON, "jsonb"), Nullable: true},
		},
		"jsonb_each_text": {
			{Name: "key", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"), Nullable: false},
			{Name: "value", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"), Nullable: true},
		},
		"json_array_elements": {
			{Name: "value", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryJSON, "json"), Nullable: true},
		},
		"jsonb_array_elements": {
			{Name: "value", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryJSON, "jsonb"), Nullable: true},
		},
		"json_array_elements_text": {
			{Name: "value", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"), Nullable: true},
		},
		"jsonb_array_elements_text": {
			{Name: "value", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"), Nullable: true},
		},
		"json_object_keys": {
			{
				Name:     "json_object_keys",
				SQLType:  sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"),
				Nullable: false,
			},
		},
		"jsonb_object_keys": {
			{
				Name:     "jsonb_object_keys",
				SQLType:  sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"),
				Nullable: false,
			},
		},
		"regexp_matches": {
			{
				Name:     "regexp_matches",
				SQLType:  arrayTypeOf(sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"), 1),
				Nullable: true,
			},
		},
		"regexp_split_to_table": {
			{
				Name:     "regexp_split_to_table",
				SQLType:  sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"),
				Nullable: false,
			},
		},
		"string_to_table": {
			{
				Name:     "string_to_table",
				SQLType:  sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"),
				Nullable: true,
			},
		},
	}
)

// builtinTableFunctionColumns returns the columns a built-in function called in a FROM
// clause produces: those tableValuedFunctionColumns lists for it, or else the shape the
// function's set-returning signatures in the function catalogue agree on.
//
// Takes functions (*ast.FunctionCatalogue) which holds the dialect's built-in functions,
// extra functions included; nil consults the listed functions alone.
// Takes functionName (string) which is the function name as the reference writes it.
//
// Returns []ast.ScopedColumn which holds a copy of the columns, or nil when the function
// is not known as a table-valued function.
func builtinTableFunctionColumns(functions *ast.FunctionCatalogue, functionName string) []ast.ScopedColumn {
	if columns, listed := tableValuedFunctionColumns[functionName]; listed {
		result := make([]ast.ScopedColumn, len(columns))
		copy(result, columns)
		return result
	}
	if functions == nil {
		return nil
	}
	return setReturningShape(functions.Functions[strings.ToLower(functionName)], functionName)
}

// setReturningShape determines the columns shared by a function's set-returning
// signatures.
//
// Conflicting column names produce no shape. Conflicting column types produce an unknown
// type for that column.
//
// Takes signatures ([]*sqlmodel.Function) which are the function's signatures.
// Takes functionName (string) which names the column of a base-type result.
//
// Returns []ast.ScopedColumn which holds the columns, or nil when the signatures give no
// shape.
func setReturningShape(signatures []*sqlmodel.Function, functionName string) []ast.ScopedColumn {
	var shape []ast.ScopedColumn
	for _, signature := range signatures {
		if signature == nil || !signature.ReturnsSet {
			continue
		}
		columns := resultColumns(functionName, signature)
		if columns == nil {
			continue
		}
		if shape == nil {
			shape = columns
			continue
		}
		if !sameColumnNames(shape, columns) {
			return nil
		}
		mergeColumns(shape, columns)
	}
	return shape
}

// resultColumns returns the columns one row of a set-returning signature's result has.
//
// Takes functionName (string) which names the column of a base-type result.
// Takes signature (*sqlmodel.Function) which is the signature.
//
// Returns []ast.ScopedColumn which holds the columns, or nil when the result's shape is
// unknown.
func resultColumns(functionName string, signature *sqlmodel.Function) []ast.ScopedColumn {
	returnType := signature.ReturnType
	if fields := returnType.StructFields; len(fields) > 0 {
		columns := make([]ast.ScopedColumn, len(fields))
		for index := range fields {
			columns[index] = ast.ScopedColumn{Name: fields[index].Name, SQLType: fields[index].SQLType, Nullable: true}
		}
		return columns
	}
	if returnType.Category == sqlmodel.TypeCategoryUnknown {
		return nil
	}
	return []ast.ScopedColumn{{
		Name:     functionName,
		SQLType:  returnType,
		Nullable: signature.NullableBehaviour != sqlmodel.FunctionNullableNeverNull,
	}}
}

// sameColumnNames reports whether two shapes have the same columns in the same order.
//
// Takes left ([]ast.ScopedColumn) which is one shape.
// Takes right ([]ast.ScopedColumn) which is the other.
//
// Returns bool which is true when the names match column for column.
func sameColumnNames(left, right []ast.ScopedColumn) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].Name != right[index].Name {
			return false
		}
	}
	return true
}

// mergeColumns folds another signature's columns into a shape: a column is NULL where
// either is, and of unknown type where their types differ.
//
// Takes shape ([]ast.ScopedColumn) which is updated in place.
// Takes columns ([]ast.ScopedColumn) which are the other signature's columns, of the same
// names.
func mergeColumns(shape, columns []ast.ScopedColumn) {
	for index := range shape {
		shape[index].Nullable = shape[index].Nullable || columns[index].Nullable
		if !sameType(shape[index].SQLType, columns[index].SQLType) {
			shape[index].SQLType = sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
		}
	}
}

// sameType reports whether two column types are the same type.
//
// Takes left (sqlmodel.SQLType) which is one type.
// Takes right (sqlmodel.SQLType) which is the other.
//
// Returns bool which is true when both have the same category and name.
func sameType(left, right sqlmodel.SQLType) bool {
	return left.Category == right.Category && strings.EqualFold(left.EngineName, right.EngineName)
}

// takesAliasAsColumn reports whether a FROM function's single result column takes the
// table alias as its name.
//
// Takes columns ([]ast.ScopedColumn) which are the function's columns.
// Takes functionName (string) which is the function's unqualified name.
// Takes argumentCount (int) which is how many arguments the call passes.
//
// Returns bool which is true when the alias names the column.
func takesAliasAsColumn(columns []ast.ScopedColumn, functionName string, argumentCount int) bool {
	if functionName == multipleArrayUnnest && argumentCount > 1 {
		return false
	}
	return len(columns) == 1 && columns[0].Name == functionName
}
