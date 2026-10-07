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

package sqlmodel

import (
	"cmp"
	"iter"
	"maps"
	"slices"
	"strings"
)

// Key returns the key the catalogue files a canonical name under, following the dialect's
// identifier rules.
//
// Takes name (string) which is a canonical name.
//
// Returns string which is the map key for the name.
func (c *Catalogue) Key(name string) string {
	if c == nil {
		return name
	}
	return c.Identifiers.Key(name)
}

// SchemaNamesFor returns, in order, the keys of the schemas a reference qualified by
// schema is looked up in: the named schema alone, or every schema on the search path when
// the reference is unqualified.
//
// Takes schema (string) which is the reference's canonical schema qualifier, or empty.
//
// Returns []string which holds the schema keys to search, in order.
func (c *Catalogue) SchemaNamesFor(schema string) []string {
	if c == nil {
		return nil
	}
	if schema != "" {
		return []string{c.Key(schema)}
	}
	if len(c.SearchPath) == 0 {
		return []string{c.Key(c.DefaultSchema)}
	}
	names := make([]string, 0, len(c.SearchPath))
	for _, entry := range c.SearchPath {
		names = append(names, c.Key(entry))
	}
	return names
}

// LookupSchema returns the first schema a reference qualified by schema resolves to: the
// named schema, or the first schema on the search path the catalogue holds.
//
// Takes schema (string) which is the reference's canonical schema qualifier, or empty.
//
// Returns *Schema which is the schema, or nil when none exists.
func (c *Catalogue) LookupSchema(schema string) *Schema {
	for _, name := range c.SchemaNamesFor(schema) {
		if found := c.Schemas[name]; found != nil {
			return found
		}
	}
	return nil
}

// LookupTable returns the table a reference names, searching the named schema or, for an
// unqualified reference, each schema on the search path in order.
//
// Takes schema (string) which is the reference's canonical schema qualifier, or empty.
// Takes name (string) which is the table's canonical name.
//
// Returns *Table which is the first matching table, or nil when there is none.
func (c *Catalogue) LookupTable(schema, name string) *Table {
	for _, schemaName := range c.SchemaNamesFor(schema) {
		if found := c.Schemas[schemaName]; found != nil {
			if table := found.Tables[c.Key(name)]; table != nil {
				return table
			}
		}
	}
	return nil
}

// LookupView returns the view a reference names, searching the named schema or, for an
// unqualified reference, each schema on the search path in order.
//
// Takes schema (string) which is the reference's canonical schema qualifier, or empty.
// Takes name (string) which is the view's canonical name.
//
// Returns *View which is the first matching view, or nil when there is none.
func (c *Catalogue) LookupView(schema, name string) *View {
	for _, schemaName := range c.SchemaNamesFor(schema) {
		if found := c.Schemas[schemaName]; found != nil {
			if view := found.Views[c.Key(name)]; view != nil {
				return view
			}
		}
	}
	return nil
}

// LookupSequence returns the sequence a reference names, searching the named schema or,
// for an unqualified reference, each schema on the search path in order.
//
// Takes schema (string) which is the reference's canonical schema qualifier, or empty.
// Takes name (string) which is the sequence's canonical name.
//
// Returns *Sequence which is the first matching sequence, or nil when there is none.
func (c *Catalogue) LookupSequence(schema, name string) *Sequence {
	for _, schemaName := range c.SchemaNamesFor(schema) {
		if found := c.Schemas[schemaName]; found != nil {
			if sequence := found.Sequences[c.Key(name)]; sequence != nil {
				return sequence
			}
		}
	}
	return nil
}

// FunctionsOnPath yields, in lookup order, each schema a function reference resolves
// through together with that schema's overloads of the function: the named schema alone
// for a qualified reference, or each schema on the search path for an unqualified one.
//
// Schema and function names are both keyed through Key, as the catalogue files them, so a
// quoted mixed-case name and the unquoted name it differs from by case find different
// functions in a dialect that tells them apart. A schema the catalogue lacks, or one
// without the function, is skipped, and a schema listed twice on the search path is
// yielded once. A nil catalogue yields nothing.
//
// Takes schema (string) which is the reference's canonical schema qualifier, or empty.
// Takes name (string) which is the function's unqualified canonical name.
//
// Returns iter.Seq2[*Schema, []*Function] which yields each schema holding the function
// with the schema's own slice of its overloads.
func (c *Catalogue) FunctionsOnPath(schema, name string) iter.Seq2[*Schema, []*Function] {
	return func(yield func(*Schema, []*Function) bool) {
		if c == nil {
			return
		}
		key := c.Key(name)
		for _, schemaName := range c.distinctSchemaNamesFor(schema) {
			found := c.Schemas[schemaName]
			if found == nil {
				continue
			}
			overloads := found.Functions[key]
			if len(overloads) == 0 {
				continue
			}
			if !yield(found, overloads) {
				return
			}
		}
	}
}

// OnSearchPath reports whether a schema is on the catalogue's search path, so that its
// objects are reachable without a qualifier.
//
// Takes schema (string) which is the schema's canonical name.
//
// Returns bool which is true when the schema is on the search path.
func (c *Catalogue) OnSearchPath(schema string) bool {
	return slices.Contains(c.SchemaNamesFor(""), c.Key(schema))
}

// FindColumn finds a table or view column using schema search and identifier rules.
//
// Tables take precedence over views; unqualified names use the first matching schema on
// the search path.
//
// The returned pointer aliases the underlying slice element, so callers can mutate the
// catalogue in place.
//
// Takes schema (string) which is the canonical schema qualifier, or empty to follow the
// search path.
// Takes tableName (string) which is the canonical name of the table or view.
// Takes columnName (string) which is the column's canonical name.
//
// Returns *Column which is the matching column, or nil when the relation or column does
// not exist or the catalogue is nil.
func (c *Catalogue) FindColumn(schema, tableName, columnName string) *Column {
	if c == nil {
		return nil
	}
	var columns []Column
	if table := c.LookupTable(schema, tableName); table != nil {
		columns = table.Columns
	} else if view := c.LookupView(schema, tableName); view != nil {
		columns = view.Columns
	}
	for columnIndex := range columns {
		if c.Identifiers.Equal(columns[columnIndex].Name, columnName) {
			return &columns[columnIndex]
		}
	}
	return nil
}

// ResolveNamedType looks up a type in the catalogue's domains, enums and composites.
//
// Qualified names use their schema. Other names follow the search path, then the
// remaining schemas. Domains resolve to their underlying value type.
//
// Takes sqlType (SQLType) which is the type to resolve; one whose category is known is
// returned unchanged.
//
// Returns SQLType which is the domain's value type, or the enum or composite type the
// name names, with its schema, values and unqualified name, or sqlType unchanged when
// nothing matches. The values are a copy, so changing them leaves the enum alone.
func (c *Catalogue) ResolveNamedType(sqlType SQLType) SQLType {
	if c == nil || sqlType.Category != TypeCategoryUnknown || sqlType.EngineName == "" {
		return sqlType
	}
	schema, name := sqlType.Schema, sqlType.EngineName
	if qualifier, unqualified, found := strings.CutLast(name, "."); schema == "" && found && qualifier != "" && unqualified != "" {
		schema, name = qualifier, unqualified
	}
	key := c.Key(name)
	for _, schemaName := range c.typeSchemaOrder(schema) {
		found := c.Schemas[schemaName]
		if found == nil {
			continue
		}
		if domain := found.Domains[key]; domain != nil {
			valueType := domain.ValueType()
			valueType.EnumValues = slices.Clone(valueType.EnumValues)
			return valueType
		}
		if enum := found.Enums[key]; enum != nil {
			sqlType.Category = TypeCategoryEnum
			sqlType.EnumValues = slices.Clone(enum.Values)
			sqlType.Schema = cmp.Or(enum.Schema, found.Name)
			sqlType.EngineName = name
			return sqlType
		}
		if found.CompositeTypes[key] != nil {
			sqlType.Category = TypeCategoryComposite
			sqlType.Schema = found.Name
			sqlType.EngineName = name
			return sqlType
		}
	}
	return sqlType
}

// LookupDomain returns the domain a type name names: in the named schema, or, for an
// unqualified name, along the search path and then in the other schemas in name order, as
// ResolveNamedType looks a type up.
//
// Takes schema (string) which is the name's schema qualifier, or empty.
// Takes name (string) which is the domain's name.
//
// Returns *Domain which is the domain, or nil when there is none.
func (c *Catalogue) LookupDomain(schema, name string) *Domain {
	if c == nil {
		return nil
	}
	if qualifier, unqualified, found := strings.CutLast(name, "."); schema == "" && found && qualifier != "" && unqualified != "" {
		schema, name = qualifier, unqualified
	}
	key := c.Key(name)
	for _, schemaName := range c.typeSchemaOrder(schema) {
		if found := c.Schemas[schemaName]; found != nil && found.Domains[key] != nil {
			return found.Domains[key]
		}
	}
	return nil
}

// distinctSchemaNamesFor returns the schema keys SchemaNamesFor gives, each kept only at
// its first position, so a schema listed twice on the search path is searched once.
//
// Takes schema (string) which is the reference's canonical schema qualifier, or empty.
//
// Returns []string which holds the distinct schema keys to search, in order.
func (c *Catalogue) distinctSchemaNamesFor(schema string) []string {
	names := c.SchemaNamesFor(schema)
	if len(names) < 2 {
		return names
	}
	visited := make(map[string]struct{}, len(names))
	distinct := make([]string, 0, len(names))
	for _, name := range names {
		if _, repeated := visited[name]; repeated {
			continue
		}
		visited[name] = struct{}{}
		distinct = append(distinct, name)
	}
	return distinct
}

// typeSchemaOrder returns the keys of the schemas a type name is looked up in, in order:
// the named schema alone, or the search path followed by every other schema in name
// order.
//
// Takes schema (string) which is the type's schema qualifier, or empty.
//
// Returns []string which holds the schema keys.
func (c *Catalogue) typeSchemaOrder(schema string) []string {
	if schema != "" {
		return []string{c.Key(schema)}
	}
	order := c.SchemaNamesFor("")
	for _, name := range slices.Sorted(maps.Keys(c.Schemas)) {
		if !slices.Contains(order, name) {
			order = append(order, name)
		}
	}
	return order
}

// ValueType returns the type of the domain's values: its base type, wrapped in an array
// type once for each of its array dimensions.
//
// Returns SQLType which is the value type.
func (d *Domain) ValueType() SQLType {
	column := NewColumn(d.Name, d.BaseType, !d.NotNull)
	column.ArrayDimensions = d.ArrayDimensions
	return column.ValueType()
}
