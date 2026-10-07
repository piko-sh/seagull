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
	"fmt"
	"maps"
	"slices"

	"seagull.sh/seagull/internal/sqlmodel"
)

type modifierTypeNormaliser struct{}

func (modifierTypeNormaliser) NormaliseTypeName(name string, modifiers ...int) sqlmodel.SQLType {
	category := sqlmodel.TypeCategoryUnknown
	switch name {
	case "int4", "int8":
		category = sqlmodel.TypeCategoryInteger
	case "text", "varchar":
		category = sqlmodel.TypeCategoryText
	}
	if len(modifiers) > 0 {
		name = fmt.Sprintf("%s%v", name, modifiers)
	}
	return sqlmodel.NewSQLType(category, name)
}

func schemaNames(catalogue *sqlmodel.Catalogue) []string {
	return slices.Sorted(maps.Keys(catalogue.Schemas))
}

func mapKeys[Value any](entries map[string]Value) []string {
	return slices.Sorted(maps.Keys(entries))
}

func newTestSchema(name string) *sqlmodel.Schema {
	return &sqlmodel.Schema{
		Name:           name,
		Tables:         make(map[string]*sqlmodel.Table),
		Views:          make(map[string]*sqlmodel.View),
		Enums:          make(map[string]*sqlmodel.Enum),
		Functions:      make(map[string][]*sqlmodel.Function),
		CompositeTypes: make(map[string]*sqlmodel.CompositeType),
		Sequences:      make(map[string]*sqlmodel.Sequence),
	}
}

func catalogueOf(schemas ...*sqlmodel.Schema) *sqlmodel.Catalogue {
	catalogue := &sqlmodel.Catalogue{DefaultSchema: "public", Schemas: map[string]*sqlmodel.Schema{}}
	for _, schema := range schemas {
		catalogue.Schemas[schema.Name] = schema
	}
	return catalogue
}
