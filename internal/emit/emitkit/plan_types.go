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

package emitkit

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// suffixItem ends the owner name of an array's element type.
	suffixItem = "Item"

	// suffixKey ends the owner name of a map's key type.
	suffixKey = "Key"

	// suffixValue ends the owner name of a map's value type.
	suffixValue = "Value"

	// unnamedConstant ends the constant of an enum value that holds no letter or digit.
	unnamedConstant = "Unnamed"

	// fallbackStructField names a struct field whose name gives no identifier.
	fallbackStructField = "Field"

	// fallbackModel names a model whose table name gives no identifier.
	fallbackModel = "Model"

	// fallbackType names a named type whose owner gives no identifier.
	fallbackType = "Type"

	// keyCatalogueEnum starts the key of an enum the catalogue declares.
	keyCatalogueEnum = "enum:"

	// keyInlineEnum starts the key of an enum written inline in a column's type.
	keyInlineEnum = "values:"

	// keyStruct starts the key of a struct type.
	keyStruct = "struct:"
)

// typePlanner names the models and the named enum and struct types of a package.
type typePlanner struct {
	// catalogue holds the schema the models and catalogue enums come from.
	catalogue *sqlmodel.Catalogue

	// named reports whether the type mapping gives a type a named Go type.
	named func(sqlType sqlmodel.SQLType) bool

	// scope is the package-wide scope the names are claimed in.
	scope *nameScope

	// index maps the key of each named type to its position in types.
	index map[string]int

	// types holds the named types in declaration order.
	types []emit.NamedType
}

// newTypePlanner creates a planner claiming names in a package scope.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the schema, or nil.
// Takes named (func(sqlmodel.SQLType) bool) which reports which types are named, or nil
// for none.
// Takes scope (*nameScope) which is the package-wide scope.
//
// Returns *typePlanner which is ready to plan.
func newTypePlanner(catalogue *sqlmodel.Catalogue, named func(sqlmodel.SQLType) bool, scope *nameScope) *typePlanner {
	return &typePlanner{catalogue: catalogue, named: named, scope: scope, index: make(map[string]int), types: nil}
}

// planModels names the model of every non-partition table, by table name then schema: the
// table's name in Pascal case, after its schema's when the schema is off the search path,
// and the field of every column it does not hide.
//
// Returns []emit.ModelNaming which holds the models in declaration order.
func (p *typePlanner) planModels() []emit.ModelNaming {
	tables := catalogueTables(p.catalogue)
	models := make([]emit.ModelNaming, 0, len(tables))
	for _, table := range tables {
		model := emit.ModelNaming{
			Schema:   table.Schema,
			Relation: table.Name,
			Type:     p.scope.claim(nonEmpty(p.qualifiedTypeName(table.Schema, table.Name), fallbackModel)),
			Fields:   make([]string, len(table.Columns)),
		}
		fields := newNameScope()
		for index := range table.Columns {
			if !table.Columns[index].Hidden {
				model.Fields[index] = fields.claim(nonEmpty(goast.SnakeToPascalCase(table.Columns[index].Name), fallbackColumnField))
			}
		}
		models = append(models, model)
	}
	return models
}

// qualifiedTypeName returns the Go type name of a catalogue object: its name in Pascal
// case, after its schema's when the schema is off the search path.
//
// Takes schema (string) which is the object's schema.
// Takes name (string) which is the object's name.
//
// Returns string which is the type name.
func (p *typePlanner) qualifiedTypeName(schema, name string) string {
	pascal := goast.SnakeToPascalCase(name)
	if schema == "" || p.catalogue.OnSearchPath(schema) {
		return pascal
	}
	return goast.SnakeToPascalCase(schema) + pascal
}

// planCatalogueEnums names every enum the catalogue declares and the type mapping names,
// by enum name then schema, as qualifiedTypeName does.
func (p *typePlanner) planCatalogueEnums() {
	if p.named == nil || p.catalogue == nil {
		return
	}
	var enums []*sqlmodel.Enum
	for _, schema := range p.catalogue.Schemas {
		for _, enum := range schema.Enums {
			if enum.Schema == "" {
				enum = &sqlmodel.Enum{Name: enum.Name, Schema: schema.Name, Values: enum.Values, Comment: enum.Comment, Origin: enum.Origin}
			}
			enums = append(enums, enum)
		}
	}
	slices.SortFunc(enums, func(left, right *sqlmodel.Enum) int {
		return cmp.Or(cmp.Compare(left.Name, right.Name), cmp.Compare(left.Schema, right.Schema))
	})
	for _, enum := range enums {
		sqlType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryEnum, enum.Name)
		sqlType.Schema = enum.Schema
		sqlType.EnumValues = slices.Clone(enum.Values)
		p.visit(sqlType, "", 0)
	}
}

// planModelTypes names enum and struct types after their owning model and column. It
// visits tables in migration order, so a shared inline type keeps the name of the first
// table declaring it.
//
// Takes models ([]emit.ModelNaming) which are the models planModels named, in order.
func (p *typePlanner) planModelTypes(models []emit.ModelNaming) {
	if p.named == nil {
		return
	}
	tables := catalogueTables(p.catalogue)
	for _, modelIndex := range declarationOrder(tables) {
		table := tables[modelIndex]
		for index := range table.Columns {
			column := &table.Columns[index]
			if column.Hidden || column.GoTypeOverride != nil {
				continue
			}
			p.visit(column.ValueType(), models[modelIndex].Type+models[modelIndex].Fields[index], 0)
		}
	}
}

// planQueryTypes names the enum and struct types a query's parameters and output columns
// hold that no model named first, each owned by the query's method and the parameter or
// column.
//
// Takes queries ([]*sqlmodel.AnalysedQuery) which are the queries in planning order.
func (p *typePlanner) planQueryTypes(queries []*sqlmodel.AnalysedQuery) {
	if p.named == nil {
		return
	}
	for _, query := range queries {
		method, err := exportedQueryName(query.Name)
		if err != nil {
			continue
		}
		for index := range query.Parameters {
			parameter := &query.Parameters[index]
			if parameter.GoTypeOverride != nil || parameter.IsPaginationBound() {
				continue
			}
			p.visit(parameter.SQLType, method+goast.SnakeToPascalCase(parameter.Name), 0)
		}
		for index := range query.OutputColumns {
			column := &query.OutputColumns[index]
			if column.GoTypeOverride != nil {
				continue
			}
			p.visit(column.SQLType, method+goast.SnakeToPascalCase(column.Name), 0)
		}
	}
}

// visit names a type the mapping names when no earlier owner did, then walks into its
// parts: an array's element, a map's key and value, and a named struct's fields, each
// owned by a name built from its parent's.
//
// Takes sqlType (sqlmodel.SQLType) which is the type.
// Takes owner (string) which is the name assigned when declaring the type here.
// Takes depth (int) which is the depth of the walk.
func (p *typePlanner) visit(sqlType sqlmodel.SQLType, owner string, depth int) {
	if depth >= sqlmodel.MaxTypeDepth {
		return
	}
	switch sqlType.Category {
	case sqlmodel.TypeCategoryEnum:
		if p.named(sqlType) {
			p.declare(sqlType, owner)
		}
	case sqlmodel.TypeCategoryStruct:
		p.visitStruct(sqlType, owner, depth)
	case sqlmodel.TypeCategoryArray:
		if sqlType.ElementType != nil {
			p.visit(*sqlType.ElementType, owner+suffixItem, depth+1)
		}
	case sqlmodel.TypeCategoryMap:
		if sqlType.KeyType != nil {
			p.visit(*sqlType.KeyType, owner+suffixKey, depth+1)
		}
		if sqlType.ElementType != nil {
			p.visit(*sqlType.ElementType, owner+suffixValue, depth+1)
		}
	default:
	}
}

// visitStruct names a struct type the mapping names when no earlier owner did, then walks
// into its fields, each owned by the struct's name and the field's.
//
// Takes sqlType (sqlmodel.SQLType) which is the struct type.
// Takes owner (string) which is the name assigned when declaring the type here.
// Takes depth (int) which is the depth of the walk.
func (p *typePlanner) visitStruct(sqlType sqlmodel.SQLType, owner string, depth int) {
	if !p.named(sqlType) {
		return
	}
	position, declared := p.declare(sqlType, owner)
	if !declared {
		return
	}
	name := p.types[position].Name
	fields := p.types[position].Fields
	for index := range sqlType.StructFields {
		p.visit(sqlType.StructFields[index].SQLType, name+fields[index], depth+1)
	}
}

// declare assigns names to a SQL type, its enum constants and its struct fields.
//
// Catalogue enums use qualified names; other types use their owner's name. Field and
// constant names are unique within their scopes.
//
// Takes sqlType (sqlmodel.SQLType) which is the enum or struct type.
// Takes owner (string) which is the name an owned type takes.
//
// Returns int which is the named type's position in types.
// Returns bool which is true when this call declared it.
func (p *typePlanner) declare(sqlType sqlmodel.SQLType, owner string) (int, bool) {
	key := namedTypeKey(&sqlType, p.catalogue)
	if position, seen := p.index[key]; seen {
		return position, false
	}
	sqlType.Nullable = false
	name := owner
	if sqlType.Category == sqlmodel.TypeCategoryEnum && sqlType.Schema != "" {
		name = p.qualifiedTypeName(sqlType.Schema, sqlType.EngineName)
	}
	declared := emit.NamedType{Name: p.scope.claim(nonEmpty(name, fallbackType)), Constants: nil, Fields: nil, SQLType: sqlType}
	if sqlType.Category == sqlmodel.TypeCategoryEnum {
		declared.Constants = make([]string, len(sqlType.EnumValues))
		for index, value := range sqlType.EnumValues {
			declared.Constants[index] = p.scope.claim(declared.Name + nonEmpty(goast.PascalCaseSuffix(value), unnamedConstant))
		}
	} else {
		fields := newNameScope()
		declared.Fields = make([]string, len(sqlType.StructFields))
		for index := range sqlType.StructFields {
			declared.Fields[index] = fields.claim(nonEmpty(goast.SnakeToPascalCase(sqlType.StructFields[index].Name), fallbackStructField))
		}
	}
	p.index[key] = len(p.types)
	p.types = append(p.types, declared)
	return len(p.types) - 1, true
}

// TypeNamer builds a type-mapping namer from the request's naming plan.
//
// Catalogue enums match by schema and name, inline enums by values, and structs
// recursively by field names and types.
//
// Takes request (*emit.Request) which carries the plan in Naming and the catalogue whose
// identifier rules compare schema and type names. TypeNamer does not change it.
//
// Returns func(sqlmodel.SQLType) (string, bool) which returns the named type's name, and
// false for a type the plan does not name.
func TypeNamer(request *emit.Request) func(sqlType sqlmodel.SQLType) (string, bool) {
	catalogue := request.Catalogue
	names := make(map[string]string, len(request.Naming.Types))
	for index := range request.Naming.Types {
		declared := &request.Naming.Types[index]
		names[namedTypeKey(&declared.SQLType, catalogue)] = declared.Name
	}
	return func(sqlType sqlmodel.SQLType) (string, bool) {
		if sqlType.Category != sqlmodel.TypeCategoryEnum && sqlType.Category != sqlmodel.TypeCategoryStruct {
			return "", false
		}
		name, found := names[namedTypeKey(&sqlType, catalogue)]
		return name, found
	}
}

// namedTypeKey returns the key a named type is found by: the schema and name of an enum
// the catalogue declares, compared under the catalogue's identifier rules, the values of
// an enum written inline, or the signature of a struct.
//
// Takes sqlType (*sqlmodel.SQLType) which is the enum or struct type.
// Takes catalogue (*sqlmodel.Catalogue) which compares identifiers, or nil.
//
// Returns string which is the key.
func namedTypeKey(sqlType *sqlmodel.SQLType, catalogue *sqlmodel.Catalogue) string {
	return typeKeyAt(sqlType, catalogue, 0)
}

// typeKeyAt is namedTypeKey for a type at a depth of the walk, so the walk through the
// parts of nested structs stays bounded.
//
// Takes sqlType (*sqlmodel.SQLType) which is the enum or struct type.
// Takes catalogue (*sqlmodel.Catalogue) which compares identifiers, or nil.
// Takes depth (int) which is the depth of the walk.
//
// Returns string which is the key.
func typeKeyAt(sqlType *sqlmodel.SQLType, catalogue *sqlmodel.Catalogue, depth int) string {
	var builder strings.Builder
	switch {
	case sqlType.Category == sqlmodel.TypeCategoryEnum && sqlType.Schema != "":
		builder.WriteString(keyCatalogueEnum)
		builder.WriteString(strconv.Quote(catalogue.Key(sqlType.Schema)))
		builder.WriteString(strconv.Quote(catalogue.Key(sqlType.EngineName)))
	case sqlType.Category == sqlmodel.TypeCategoryEnum:
		builder.WriteString(keyInlineEnum)
		writeQuoted(&builder, sqlType.EnumValues)
	default:
		builder.WriteString(keyStruct)
		writeSignature(&builder, sqlType, catalogue, true, depth)
	}
	return builder.String()
}

// writeSignature writes a type's structural signature: every attribute the Go type it
// maps to may depend on, and the signatures of its parts, recursively. The nullability of
// the type itself is left out at the top, where it decides only whether a pointer is
// used.
//
// Takes builder (*strings.Builder) which receives the signature.
// Takes sqlType (*sqlmodel.SQLType) which is the type.
// Takes catalogue (*sqlmodel.Catalogue) which compares identifiers, or nil.
// Takes top (bool) which is true for the type being keyed itself.
// Takes depth (int) which is the depth of the walk.
func writeSignature(builder *strings.Builder, sqlType *sqlmodel.SQLType, catalogue *sqlmodel.Catalogue, top bool, depth int) {
	builder.WriteByte('(')
	defer builder.WriteByte(')')
	if depth >= sqlmodel.MaxTypeDepth {
		return
	}
	if !top && (sqlType.Category == sqlmodel.TypeCategoryEnum || sqlType.Category == sqlmodel.TypeCategoryStruct) {
		builder.WriteString(typeKeyAt(sqlType, catalogue, depth))
		builder.WriteString(strconv.FormatBool(sqlType.Nullable))
		return
	}
	writeQuoted(builder, []string{
		sqlType.Category.String(), sqlType.EngineName, sqlType.Schema,
		strconv.Itoa(sqlType.Width), strconv.Itoa(int(sqlType.Temporal)), strconv.Itoa(int(sqlType.Network)),
		strconv.FormatBool(sqlType.Unsigned), strconv.FormatBool(sqlType.TimeZone), strconv.FormatBool(sqlType.Monetary),
		strconv.FormatBool(sqlType.Nullable && !top),
	})
	writeQuoted(builder, sqlType.EnumValues)
	for _, part := range []*sqlmodel.SQLType{sqlType.ElementType, sqlType.KeyType} {
		if part != nil {
			writeSignature(builder, part, catalogue, false, depth+1)
		}
	}
	for index := range sqlType.StructFields {
		field := &sqlType.StructFields[index]
		writeQuoted(builder, []string{field.Name, strconv.FormatBool(field.Positional)})
		writeSignature(builder, &field.SQLType, catalogue, false, depth+1)
	}
	for index := range sqlType.UnionMembers {
		member := &sqlType.UnionMembers[index]
		writeQuoted(builder, []string{member.Tag})
		writeSignature(builder, &member.SQLType, catalogue, false, depth+1)
	}
}

// writeQuoted writes each value Go-quoted, so no two lists of values write alike.
//
// Takes builder (*strings.Builder) which receives the values.
// Takes values ([]string) which are the values.
func writeQuoted(builder *strings.Builder, values []string) {
	builder.WriteByte('[')
	for _, value := range values {
		builder.WriteString(strconv.Quote(value))
	}
	builder.WriteByte(']')
}

// catalogueTables returns every table of a catalogue that generated code models, by name
// then schema: every table but a partition, which its parent's model stands for.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the schema, or nil.
//
// Returns []*sqlmodel.Table which holds the tables.
func catalogueTables(catalogue *sqlmodel.Catalogue) []*sqlmodel.Table {
	if catalogue == nil {
		return nil
	}
	var tables []*sqlmodel.Table
	for _, schema := range catalogue.Schemas {
		for _, table := range schema.Tables {
			if table.PartitionOf == nil {
				tables = append(tables, table)
			}
		}
	}
	slices.SortFunc(tables, func(left, right *sqlmodel.Table) int {
		return cmp.Or(cmp.Compare(left.Name, right.Name), cmp.Compare(left.Schema, right.Schema))
	})
	return tables
}

// declarationOrder returns the positions of tables in the order the migrations declare
// them: by migration file, then by statement within the file, keeping the order the
// tables are given in for tables of one statement or of no migration.
//
// Takes tables ([]*sqlmodel.Table) which are the tables.
//
// Returns []int which holds the positions into tables, in declaration order.
func declarationOrder(tables []*sqlmodel.Table) []int {
	order := make([]int, len(tables))
	for index := range order {
		order[index] = index
	}
	slices.SortStableFunc(order, func(left, right int) int {
		return cmp.Or(
			cmp.Compare(tables[left].Origin.Index, tables[right].Origin.Index),
			cmp.Compare(tables[left].Origin.Statement, tables[right].Origin.Statement),
		)
	})
	return order
}
