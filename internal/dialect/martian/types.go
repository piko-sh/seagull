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

package martian

import (
	"maps"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// typeWhole16 is Martian's 16-bit integer.
	typeWhole16 = "WHOLE16"

	// typeWhole32 is Martian's 32-bit integer.
	typeWhole32 = "WHOLE32"

	// typeWhole64 is Martian's 64-bit integer.
	typeWhole64 = "WHOLE64"

	// typeReal32 is Martian's single-precision float.
	typeReal32 = "REAL32"

	// typeReal64 is Martian's double-precision float.
	typeReal64 = "REAL64"

	// typeExact is Martian's fixed-point decimal, EXACT(precision, scale).
	typeExact = "EXACT"

	// typeTextual is Martian's text, TEXTUAL or TEXTUAL(length).
	typeTextual = "TEXTUAL"

	// typeFlag is Martian's boolean.
	typeFlag = "FLAG"

	// typeMoment is Martian's timestamp.
	typeMoment = "MOMENT"

	// typeDaystamp is Martian's calendar date.
	typeDaystamp = "DAYSTAMP"

	// typeBlob is Martian's byte string.
	typeBlob = "BLOB"

	// widthWhole16 is the width in bits of WHOLE16.
	widthWhole16 = 16

	// widthWhole32 is the width in bits of WHOLE32 and REAL32.
	widthWhole32 = 32

	// widthWhole64 is the width in bits of WHOLE64 and REAL64.
	widthWhole64 = 64
)

var (
	// builtinTypes maps every Martian type name to its structured type.
	builtinTypes = map[string]sqlmodel.SQLType{
		typeWhole16:  sqlmodel.NewIntegerType(typeWhole16, widthWhole16, false),
		typeWhole32:  sqlmodel.NewIntegerType(typeWhole32, widthWhole32, false),
		typeWhole64:  sqlmodel.NewIntegerType(typeWhole64, widthWhole64, false),
		typeReal32:   sqlmodel.NewFloatType(typeReal32, widthWhole32),
		typeReal64:   sqlmodel.NewFloatType(typeReal64, widthWhole64),
		typeExact:    sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, typeExact),
		typeTextual:  sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, typeTextual),
		typeFlag:     sqlmodel.NewSQLType(sqlmodel.TypeCategoryBoolean, typeFlag),
		typeMoment:   sqlmodel.NewTemporalType(typeMoment, sqlmodel.TemporalKindTimestamp, false),
		typeDaystamp: sqlmodel.NewTemporalType(typeDaystamp, sqlmodel.TemporalKindDate, false),
		typeBlob:     sqlmodel.NewSQLType(sqlmodel.TypeCategoryBinary, typeBlob),
	}

	// implicitCasts lists, per source category, the categories a value widens to without a
	// cast.
	implicitCasts = map[sqlmodel.SQLTypeCategory][]sqlmodel.SQLTypeCategory{
		sqlmodel.TypeCategoryInteger: {sqlmodel.TypeCategoryFloat, sqlmodel.TypeCategoryDecimal},
		sqlmodel.TypeCategoryDecimal: {sqlmodel.TypeCategoryFloat},
	}

	// coreTypes holds Martian's core types: FLAG for truth values, TEXTUAL for text, WHOLE64
	// for counts, row counts and row identifiers, and no JSON or void type.
	coreTypes = dialect.CoreTypes{
		Boolean:  builtinTypes[typeFlag],
		Text:     builtinTypes[typeTextual],
		Count:    builtinTypes[typeWhole64],
		JSON:     sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""),
		RowCount: builtinTypes[typeWhole64],
		RowID:    builtinTypes[typeWhole64],
		Void:     sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""),
	}
)

// normaliseTypeName turns a Martian type name and its modifiers into a structured type.
// An unknown name keeps its spelling in the unknown category.
//
// Takes name (string) which is the type name in any case.
// Takes modifiers ([]int) which are the precision and scale of EXACT or the length of
// TEXTUAL.
//
// Returns sqlmodel.SQLType which is the structured type.
func normaliseTypeName(name string, modifiers []int) sqlmodel.SQLType {
	folded := strings.ToUpper(name)
	sqlType, known := builtinTypes[folded]
	if !known {
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, folded)
	}
	switch {
	case folded == typeTextual && len(modifiers) > 0:
		sqlType.Length = new(modifiers[0])
	case folded == typeExact && len(modifiers) > 0:
		sqlType.Precision = new(modifiers[0])
		if len(modifiers) > 1 {
			sqlType.Scale = new(modifiers[1])
		}
	}
	return sqlType
}

// promoteType returns the wider of two types of one category, and the left type
// otherwise.
//
// Takes left (sqlmodel.SQLType) which is the first operand's type.
// Takes right (sqlmodel.SQLType) which is the second operand's type.
//
// Returns sqlmodel.SQLType which is the promoted type.
func promoteType(left, right sqlmodel.SQLType) sqlmodel.SQLType {
	if left.Category != right.Category {
		if left.Category == sqlmodel.TypeCategoryInteger && right.Category == sqlmodel.TypeCategoryFloat {
			return right
		}
		return left
	}
	if right.Width > left.Width {
		return right
	}
	return left
}

// canImplicitCast reports whether Martian widens a value of one type to another without a
// cast: within a category always, and across the categories implicitCasts lists.
//
// Takes from (sqlmodel.SQLType) which is the source type.
// Takes to (sqlmodel.SQLType) which is the target type.
//
// Returns bool which is true when the cast is implicit.
func canImplicitCast(from, to sqlmodel.SQLType) bool {
	if from.Category == to.Category || from.Category == sqlmodel.TypeCategoryUnknown {
		return true
	}
	return slices.Contains(implicitCasts[from.Category], to.Category)
}

// typeCatalogue returns a fresh copy of the built-in type catalogue.
//
// Returns *ast.TypeCatalogue which maps every type name to its structured type.
func typeCatalogue() *ast.TypeCatalogue {
	return &ast.TypeCatalogue{Types: maps.Clone(builtinTypes)}
}
