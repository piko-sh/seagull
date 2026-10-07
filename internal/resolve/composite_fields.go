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

package resolve

import (
	"seagull.sh/seagull/internal/sqlmodel"
)

// compositeField returns the type of a field of a value of a composite type the catalogue
// defines, such as the street of an address created with CREATE TYPE address AS (street
// text, ...), with any named type the field is declared with resolved as a column's is.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the composite types; nil holds none.
// Takes valueType (sqlmodel.SQLType) which is the value's type.
// Takes fieldName (string) which is the field as the expression names it.
//
// Returns sqlmodel.SQLType which is the field's type.
// Returns bool which is true when the field may be NULL in a value that is not.
// Returns []string which holds the names of the type's fields, or nil when the value is
// not of a composite type the catalogue defines.
// Returns bool which is true when the field was found.
func compositeField(catalogue *sqlmodel.Catalogue, valueType sqlmodel.SQLType, fieldName string) (sqlmodel.SQLType, bool, []string, bool) {
	composite := lookupCompositeType(catalogue, valueType)
	if composite == nil {
		return sqlmodel.SQLType{}, true, nil, false
	}
	names := make([]string, len(composite.Fields))
	for index := range composite.Fields {
		field := &composite.Fields[index]
		if catalogue.Key(field.Name) == catalogue.Key(fieldName) {
			return catalogue.ResolveNamedType(field.SQLType), field.Nullable, nil, true
		}
		names[index] = field.Name
	}
	return sqlmodel.SQLType{}, true, names, false
}

// lookupCompositeType returns the composite type the catalogue defines under the name of
// a type.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the composite types; nil holds none.
// Takes valueType (sqlmodel.SQLType) which is the type.
//
// Returns *sqlmodel.CompositeType which is the composite type, or nil when the type names
// none.
func lookupCompositeType(catalogue *sqlmodel.Catalogue, valueType sqlmodel.SQLType) *sqlmodel.CompositeType {
	if catalogue == nil {
		return nil
	}
	resolved := catalogue.ResolveNamedType(valueType)
	if resolved.Category != sqlmodel.TypeCategoryComposite || resolved.EngineName == "" {
		return nil
	}
	schema := catalogue.LookupSchema(resolved.Schema)
	if schema == nil {
		return nil
	}
	return schema.CompositeTypes[catalogue.Key(resolved.EngineName)]
}
