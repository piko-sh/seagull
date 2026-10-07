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

package duckdb

import (
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

// tryParseCompoundType dispatches the struct, map and union type parsers when lower names
// one of those compound forms. LIST(T) and ARRAY(T) are no DuckDB types, as DuckDB writes
// a list of T as T[], so they are recorded as a syntax error, as DuckDB reports them.
//
// Takes engine (typeNormaliser) which resolves nested type names.
// Takes lower (string) which is the candidate compound type keyword lower-cased.
//
// Returns sqlmodel.SQLType which is the parsed compound type when matched, else the zero
// value.
// Returns bool which is true when a compound type was parsed.
func (p *parser) tryParseCompoundType(engine typeNormaliser, lower string) (sqlmodel.SQLType, bool) {
	switch lower {
	case "struct":
		return p.parseStructType(engine), true
	case "map":
		return p.parseMapType(engine), true
	case "union":
		return p.parseUnionType(engine), true
	case "list", "array":
		p.recordSyntaxError(dialectkit.NewParseError(p.PeekAt(-1),
			"LIST(T) and ARRAY(T) are not DuckDB types: write a list of T as T[]"))
		p.skipParenthesisedOrRecord()
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), true
	default:
		return sqlmodel.SQLType{}, false
	}
}

// parseNestedType parses the type of a struct field, union member, or map key or value,
// wrapping it in a list for each [] that follows it, as such a type carries its own
// list-ness rather than a column's array dimensions.
//
// Takes engine (typeNormaliser) which resolves the type name.
//
// Returns sqlmodel.SQLType which is the type.
func (p *parser) parseNestedType(engine typeNormaliser) sqlmodel.SQLType {
	sqlType, dimensions := p.parseColumnType(engine)
	return functionArgumentArrayType(sqlType, dimensions)
}

// parseNamedTypeList parses a parenthesised list of "name type" pairs.
//
// Used by struct and union type parsers, which both expect parallel slices of names and
// SQL types. Every entry needs a name and a type, entries are separated by commas, the
// list closes with a parenthesis and holds at least one entry; anything else records a
// syntax error.
//
// Takes engine (typeNormaliser) which resolves each field's type.
// Takes kind (string) which names the compound type in a syntax error, such as STRUCT.
//
// Returns []string which is the parsed field name list.
// Returns []sqlmodel.SQLType which is the parallel field type list.
func (p *parser) parseNamedTypeList(engine typeNormaliser, kind string) ([]string, []sqlmodel.SQLType) {
	p.Advance()

	var names []string
	var types []sqlmodel.SQLType

	if p.Current().Kind == tokenRightParen {
		p.recordSyntaxError(dialectkit.NewParseError(p.Current(), kind+" needs at least one entry"))
	}
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		name, nameError := p.parseIdentifierOrKeyword()
		if nameError != nil {
			p.recordSyntaxError(nameError)
			break
		}
		sqlType := p.parseNestedType(engine)
		names = append(names, name)
		types = append(types, sqlType)
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
	p.closeCompoundType(kind)

	return names, types
}

// closeCompoundType consumes the parenthesis that closes a compound type, recording a
// syntax error when another token stands there.
//
// Takes kind (string) which names the compound type, such as MAP.
func (p *parser) closeCompoundType(kind string) {
	if p.Current().Kind != tokenRightParen {
		p.recordSyntaxError(dialectkit.NewParseError(p.Current(), "expected ')' to close "+kind))
		return
	}
	p.Advance()
}

// parseStructType parses a STRUCT(...) compound type into its field list. Every field is
// nullable, as a DuckDB struct field takes no NOT NULL constraint and holds NULL whatever
// the struct's own nullability.
//
// Takes engine (typeNormaliser) which resolves each field's type.
//
// Returns sqlmodel.SQLType which is the struct type with its fields populated.
func (p *parser) parseStructType(engine typeNormaliser) sqlmodel.SQLType {
	names, types := p.parseNamedTypeList(engine, "STRUCT")

	fields := make([]sqlmodel.StructField, len(names))
	for index := range names {
		fieldType := types[index]
		fieldType.Nullable = true
		fields[index] = sqlmodel.StructField{
			Name:       names[index],
			SQLType:    fieldType,
			Positional: false,
		}
	}

	structType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryStruct, "struct")
	structType.StructFields = fields
	return structType
}

// parseMapType parses a MAP(key, value) compound type, which needs exactly a key type and
// a value type separated by a comma; anything else records a syntax error.
//
// Takes engine (typeNormaliser) which resolves the key and value types.
//
// Returns sqlmodel.SQLType which is the map type with both type pointers populated.
func (p *parser) parseMapType(engine typeNormaliser) sqlmodel.SQLType {
	p.Advance()

	keyType := p.parseNestedType(engine)

	valueType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
	if p.Current().Kind == tokenComma {
		p.Advance()
		valueType = p.parseNestedType(engine)
	} else {
		p.recordSyntaxError(dialectkit.NewParseError(p.Current(), "expected ',' between the MAP's key and value types"))
	}

	p.closeCompoundType("MAP")

	mapType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryMap, "map")
	mapType.KeyType = &keyType
	mapType.ElementType = &valueType
	return mapType
}

// parseUnionType parses a UNION(...) compound type into its tagged members.
//
// Takes engine (typeNormaliser) which resolves each member's type.
//
// Returns sqlmodel.SQLType which is the union type with members populated.
func (p *parser) parseUnionType(engine typeNormaliser) sqlmodel.SQLType {
	names, types := p.parseNamedTypeList(engine, "UNION")

	members := make([]sqlmodel.UnionMember, len(names))
	for index := range names {
		members[index] = sqlmodel.UnionMember{
			Tag:     names[index],
			SQLType: types[index],
		}
	}

	unionType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnion, "union")
	unionType.UnionMembers = members
	return unionType
}
