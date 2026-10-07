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

package catalogue

import (
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// typeReference names a user-defined type, for following it through a rename or a move to
// another schema.
type typeReference struct {
	// schema is the canonical name of the type's schema.
	schema string

	// name is the type's canonical name.
	name string
}

// lookupType finds the schema holding the enum, composite type or domain a name names, in
// the named schema or, for an unqualified name, along the search path.
//
// Takes schemaName (string) which is the name's schema qualifier, or empty.
// Takes name (string) which is the type's name.
//
// Returns *sqlmodel.Schema which holds the type, or nil when there is none.
// Returns string which is the type's name as the schema files it.
func (b *catalogueBuilder) lookupType(schemaName, name string) (*sqlmodel.Schema, string) {
	key := b.key(name)
	for _, schemaKey := range b.catalogue.SchemaNamesFor(schemaName) {
		schema := b.catalogue.Schemas[schemaKey]
		if schema == nil {
			continue
		}
		if enum := schema.Enums[key]; enum != nil {
			return schema, enum.Name
		}
		if composite := schema.CompositeTypes[key]; composite != nil {
			return schema, composite.Name
		}
		if domain := schema.Domains[key]; domain != nil {
			return schema, domain.Name
		}
	}
	return nil, ""
}

// applyAlterTypeRename renames an enum, composite type or domain, and every column,
// field, domain and function signature that uses it follows the new name.
//
// Takes mutation (*ast.CatalogueMutation) which names the type and its new name.
//
// Returns error when the type is not found, or its schema already holds a type under the
// new name.
func (b *catalogueBuilder) applyAlterTypeRename(mutation *ast.CatalogueMutation) error {
	schema, name := b.lookupType(mutation.SchemaName, mutation.EnumName)
	if schema == nil {
		return unknownObject("type %s not found in schema %s", mutation.EnumName,
			b.searchedSchemas(mutation.SchemaName))
	}
	return b.moveType(schema, name, schema, mutation.NewName)
}

// applyAlterTypeSetSchema moves an enum, composite type or domain to another schema, and
// every column, field, domain and function signature that uses it follows it.
//
// Takes mutation (*ast.CatalogueMutation) which names the type and the target schema.
//
// Returns error when the type is not found, or the target schema already holds a type
// under its name.
func (b *catalogueBuilder) applyAlterTypeSetSchema(mutation *ast.CatalogueMutation) error {
	schema, name := b.lookupType(mutation.SchemaName, mutation.EnumName)
	if schema == nil {
		return unknownObject("type %s not found in schema %s", mutation.EnumName,
			b.searchedSchemas(mutation.SchemaName))
	}
	target := b.resolveSchema(mutation.NewName)
	if target == schema {
		return nil
	}
	return b.moveType(schema, name, target, name)
}

// moveType files a type under a new schema and name and points everything that uses it at
// its new place.
//
// Takes source (*sqlmodel.Schema) which holds the type.
// Takes name (string) which is the type's name.
// Takes target (*sqlmodel.Schema) which receives the type.
// Takes newName (string) which is the type's new name.
//
// Returns error when the target schema already holds another type under the new name.
func (b *catalogueBuilder) moveType(source *sqlmodel.Schema, name string, target *sqlmodel.Schema, newName string) error {
	key, newKey := b.key(name), b.key(newName)
	if (target != source || newKey != key) && typeTaken(target, newKey) {
		return duplicateObject("type %s already exists", qualifiedName(target.Name, newName))
	}
	if enum := source.Enums[key]; enum != nil {
		delete(source.Enums, key)
		enum.Name, enum.Schema = newName, target.Name
		target.Enums[newKey] = enum
	}
	if composite := source.CompositeTypes[key]; composite != nil {
		delete(source.CompositeTypes, key)
		composite.Name, composite.Schema = newName, target.Name
		target.CompositeTypes[newKey] = composite
	}
	if domain := source.Domains[key]; domain != nil {
		delete(source.Domains, key)
		domain.Name, domain.Schema = newName, target.Name
		target.Domains[newKey] = domain
	}
	from := typeReference{schema: source.Name, name: name}
	to := typeReference{schema: target.Name, name: newName}
	b.eachCatalogueType(func(sqlType *sqlmodel.SQLType) {
		if b.namesType(sqlType, from) {
			sqlType.EngineName, sqlType.Schema = to.name, to.schema
		}
	})
	return nil
}

// namesType reports whether a type names an enum or composite type at a place.
//
// Takes sqlType (*sqlmodel.SQLType) which is the type.
// Takes place (typeReference) which is the named type's schema and name.
//
// Returns bool which is true when the type is that enum or composite type.
func (b *catalogueBuilder) namesType(sqlType *sqlmodel.SQLType, place typeReference) bool {
	named := sqlType.Category == sqlmodel.TypeCategoryEnum || sqlType.Category == sqlmodel.TypeCategoryComposite
	return named && b.sameName(sqlType.EngineName, place.name) && b.sameName(sqlType.Schema, place.schema)
}

// refreshEnumValues gives every type in the catalogue that names an enum a copy of the
// enum's values as they now are, after a value was added or renamed.
//
// Takes enum (*sqlmodel.Enum) which is the enum.
func (b *catalogueBuilder) refreshEnumValues(enum *sqlmodel.Enum) {
	place := typeReference{schema: enum.Schema, name: enum.Name}
	b.eachCatalogueType(func(sqlType *sqlmodel.SQLType) {
		if sqlType.Category == sqlmodel.TypeCategoryEnum && b.namesType(sqlType, place) {
			sqlType.EnumValues = slices.Clone(enum.Values)
		}
	})
}

// eachCatalogueType calls visit for every type the catalogue's schemas hold, and every
// type nested in each: the column types of tables and views, composite fields, domain
// base types, and function return and argument types. A type nested more deeply than
// sqlmodel.MaxTypeDepth is not visited.
//
// Takes visit (func(*sqlmodel.SQLType)) which may change the type in place.
func (b *catalogueBuilder) eachCatalogueType(visit func(*sqlmodel.SQLType)) {
	for _, schema := range b.catalogue.Schemas {
		if schema != nil {
			eachSchemaType(schema, visit)
		}
	}
}

// renameView renames a view within its schema, as ALTER VIEW ... RENAME TO and an ALTER
// TABLE or RENAME TABLE naming a view do.
//
// Takes mutation (*ast.CatalogueMutation) which names the view and its new name.
//
// Returns error when there is no such view, or the schema already holds a table or view
// under the new name.
func (b *catalogueBuilder) renameView(mutation *ast.CatalogueMutation) error {
	schema, view := b.findView(mutation.SchemaName, mutation.TableName)
	if view == nil {
		return unknownObject("view %s not found in schema %s", mutation.TableName,
			b.searchedSchemas(mutation.SchemaName))
	}
	if b.relationTaken(schema, mutation.NewName, b.key(view.Name)) {
		return duplicateObject("relation %s already exists", qualifiedName(schema.Name, mutation.NewName))
	}
	delete(schema.Views, b.key(view.Name))
	view.Name = mutation.NewName
	schema.Views[b.key(mutation.NewName)] = view
	return nil
}

// moveView moves a view to another schema, as ALTER VIEW ... SET SCHEMA does.
//
// Takes mutation (*ast.CatalogueMutation) which names the view and the target schema.
//
// Returns error when there is no such view, or the target schema already holds a table or
// view under its name.
func (b *catalogueBuilder) moveView(mutation *ast.CatalogueMutation) error {
	schema, view := b.findView(mutation.SchemaName, mutation.TableName)
	if view == nil {
		return unknownObject("view %s not found in schema %s", mutation.TableName,
			b.searchedSchemas(mutation.SchemaName))
	}
	target := b.resolveSchema(mutation.NewName)
	if target == schema {
		return nil
	}
	if b.relationTaken(target, view.Name, "") {
		return duplicateObject("relation %s already exists", qualifiedName(target.Name, view.Name))
	}
	delete(schema.Views, b.key(view.Name))
	view.Schema = target.Name
	target.Views[b.key(view.Name)] = view
	return nil
}

// renameViewColumn renames a column of a view, as ALTER VIEW ... RENAME COLUMN does.
//
// Takes mutation (*ast.CatalogueMutation) which names the view, the column and its new
// name.
//
// Returns error when there is no such view or column, or the view already has a column
// under the new name.
func (b *catalogueBuilder) renameViewColumn(mutation *ast.CatalogueMutation) error {
	_, view := b.findView(mutation.SchemaName, mutation.TableName)
	if view == nil {
		return unknownObject("view %s not found in schema %s", mutation.TableName,
			b.searchedSchemas(mutation.SchemaName))
	}
	column := b.namedColumn(view.Columns, mutation.ColumnName)
	if column == nil {
		return columnNotFound(mutation)
	}
	if !b.sameName(mutation.ColumnName, mutation.NewName) && b.namedColumn(view.Columns, mutation.NewName) != nil {
		return duplicateObject("column %s already exists in view %s", mutation.NewName, view.Name)
	}
	column.Name = mutation.NewName
	return nil
}

// searchedSchemas names the schemas a name is looked up in, for a message saying it was
// not found.
//
// Takes schemaName (string) which is the name's schema qualifier, or empty.
//
// Returns string which lists the schemas, separated by commas.
func (b *catalogueBuilder) searchedSchemas(schemaName string) string {
	return strings.Join(b.catalogue.SchemaNamesFor(schemaName), ", ")
}

// findView finds a view and the schema holding it, in the named schema or, for an
// unqualified name, along the search path.
//
// Takes schemaName (string) which is the name's schema qualifier, or empty.
// Takes name (string) which is the view's name.
//
// Returns *sqlmodel.Schema which holds the view, or nil.
// Returns *sqlmodel.View which is the view, or nil when there is none.
func (b *catalogueBuilder) findView(schemaName, name string) (*sqlmodel.Schema, *sqlmodel.View) {
	for _, schemaKey := range b.catalogue.SchemaNamesFor(schemaName) {
		if schema := b.catalogue.Schemas[schemaKey]; schema != nil {
			if view := schema.Views[b.key(name)]; view != nil {
				return schema, view
			}
		}
	}
	return nil, nil
}

// typeTaken reports whether a schema holds an enum, composite type or domain under a key.
//
// Takes schema (*sqlmodel.Schema) which is the schema.
// Takes key (string) which is the type's key.
//
// Returns bool which is true when a type has the key.
func typeTaken(schema *sqlmodel.Schema, key string) bool {
	return schema.Enums[key] != nil || schema.CompositeTypes[key] != nil || schema.Domains[key] != nil
}

// eachSchemaType calls visit for every type one schema holds, and every type nested in
// each.
//
// Takes schema (*sqlmodel.Schema) which is the schema, not nil.
// Takes visit (func(*sqlmodel.SQLType)) which may change the type in place.
func eachSchemaType(schema *sqlmodel.Schema, visit func(*sqlmodel.SQLType)) {
	for _, table := range schema.Tables {
		if table != nil {
			eachColumnType(table.Columns, visit)
		}
	}
	for _, view := range schema.Views {
		if view != nil {
			eachColumnType(view.Columns, visit)
		}
	}
	for _, composite := range schema.CompositeTypes {
		if composite != nil {
			eachColumnType(composite.Fields, visit)
		}
	}
	for _, domain := range schema.Domains {
		if domain != nil {
			eachNestedType(&domain.BaseType, visit)
		}
	}
	for _, overloads := range schema.Functions {
		eachFunctionType(overloads, visit)
	}
}

// eachFunctionType calls visit for the return and argument types of every overload of a
// function, and every type nested in each.
//
// Takes overloads ([]*sqlmodel.Function) which are the overloads; a nil one is skipped.
// Takes visit (func(*sqlmodel.SQLType)) which may change the type in place.
func eachFunctionType(overloads []*sqlmodel.Function, visit func(*sqlmodel.SQLType)) {
	for _, function := range overloads {
		if function == nil {
			continue
		}
		eachNestedType(&function.ReturnType, visit)
		for index := range function.Arguments {
			eachNestedType(&function.Arguments[index].Type, visit)
		}
	}
}

// eachColumnType calls visit for the type of every column, and every type nested in it.
//
// Takes columns ([]sqlmodel.Column) which are the columns, whose types may change in
// place.
// Takes visit (func(*sqlmodel.SQLType)) which is called for each type.
func eachColumnType(columns []sqlmodel.Column, visit func(*sqlmodel.SQLType)) {
	for index := range columns {
		eachNestedType(&columns[index].SQLType, visit)
	}
}

// eachNestedType calls visit for a type and every type nested in it, its elements, keys,
// fields and members, down to sqlmodel.MaxTypeDepth levels.
//
// Takes sqlType (*sqlmodel.SQLType) which is the outermost type; nil visits nothing.
// Takes visit (func(*sqlmodel.SQLType)) which is called for each type.
func eachNestedType(sqlType *sqlmodel.SQLType, visit func(*sqlmodel.SQLType)) {
	type nestedType struct {
		sqlType *sqlmodel.SQLType
		depth   int
	}
	pending := []nestedType{{sqlType: sqlType, depth: 0}}
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if current.sqlType == nil || current.depth >= sqlmodel.MaxTypeDepth {
			continue
		}
		visit(current.sqlType)
		depth := current.depth + 1
		pending = append(pending, nestedType{sqlType: current.sqlType.ElementType, depth: depth},
			nestedType{sqlType: current.sqlType.KeyType, depth: depth})
		for index := range current.sqlType.StructFields {
			pending = append(pending, nestedType{sqlType: &current.sqlType.StructFields[index].SQLType, depth: depth})
		}
		for index := range current.sqlType.UnionMembers {
			pending = append(pending, nestedType{sqlType: &current.sqlType.UnionMembers[index].SQLType, depth: depth})
		}
	}
}
