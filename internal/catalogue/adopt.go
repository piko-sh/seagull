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
	"maps"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/sqlmodel"
)

// Adopt prepares an external catalogue for lookups under a dialect's identifier rules.
//
// An empty schema or search path is filled from the dialect, and objects, functions
// included, are rekeyed using its identifier rules.
//
// Takes catalogue (*sqlmodel.Catalogue) which is updated in place; nil is ignored.
// Takes info (*dialect.Info) which describes the dialect the catalogue is analysed with.
func Adopt(catalogue *sqlmodel.Catalogue, info *dialect.Info) {
	if catalogue == nil {
		return
	}
	catalogue.Identifiers = info.Identifiers
	if len(catalogue.SearchPath) == 0 {
		for _, entry := range info.SchemaSearchPath {
			catalogue.SearchPath = append(catalogue.SearchPath, info.Identifiers.Fold(entry, false))
		}
	}
	if catalogue.DefaultSchema == "" && len(catalogue.SearchPath) > 0 {
		catalogue.DefaultSchema = catalogue.SearchPath[0]
	}
	schemas := make(map[string]*sqlmodel.Schema, len(catalogue.Schemas))
	for name, schema := range catalogue.Schemas {
		if schema == nil {
			schemas[catalogue.Key(name)] = nil
			continue
		}
		schema.Tables = rekeyed(catalogue, schema.Tables, func(table *sqlmodel.Table) string { return table.Name })
		schema.Views = rekeyed(catalogue, schema.Views, func(view *sqlmodel.View) string { return view.Name })
		schema.Enums = rekeyed(catalogue, schema.Enums, func(enum *sqlmodel.Enum) string { return enum.Name })
		schema.CompositeTypes = rekeyed(catalogue, schema.CompositeTypes, func(composite *sqlmodel.CompositeType) string { return composite.Name })
		schema.Sequences = rekeyed(catalogue, schema.Sequences, func(sequence *sqlmodel.Sequence) string { return sequence.Name })
		schema.Domains = rekeyed(catalogue, schema.Domains, func(domain *sqlmodel.Domain) string { return domain.Name })
		schema.Functions = rekeyedFunctions(catalogue, schema.Functions)
		schemas[catalogue.Key(name)] = schema
	}
	catalogue.Schemas = schemas
}

// LeaveOutTables removes the tables with the given names from every schema of a catalogue
// built outside the migration replay, matching names without regard to case.
//
// A live database holds tables no migration describes, such as the history, seed and lock
// tables of the migration runtime, and generated code should not model them.
//
// Takes catalogue (*sqlmodel.Catalogue) which is updated in place; nil is ignored.
// Takes names ([]string) which are the tables to leave out; none leaves every table.
func LeaveOutTables(catalogue *sqlmodel.Catalogue, names []string) {
	if catalogue == nil || len(names) == 0 {
		return
	}
	for _, schema := range catalogue.Schemas {
		if schema == nil {
			continue
		}
		maps.DeleteFunc(schema.Tables, func(key string, table *sqlmodel.Table) bool {
			name := key
			if table != nil && table.Name != "" {
				name = table.Name
			}
			return slices.ContainsFunc(names, func(left string) bool { return strings.EqualFold(left, name) })
		})
	}
}

// rekeyed returns a map holding the same objects keyed by each object's name under the
// catalogue's identifier rules, falling back to the old key for an object with no name.
//
// Takes catalogue (*sqlmodel.Catalogue) which supplies the identifier rules.
// Takes objects (map[string]*T) which is the map to rekey; nil stays nil.
// Takes nameOf (func(*T) string) which reads an object's name.
//
// Returns map[string]*T which holds the rekeyed objects.
func rekeyed[T any](catalogue *sqlmodel.Catalogue, objects map[string]*T, nameOf func(*T) string) map[string]*T {
	if objects == nil {
		return nil
	}
	keyed := make(map[string]*T, len(objects))
	for key, object := range objects {
		name := key
		if object != nil && nameOf(object) != "" {
			name = nameOf(object)
		}
		keyed[catalogue.Key(name)] = object
	}
	return keyed
}

// rekeyedFunctions returns a map holding the same overloads keyed by each function's name
// under the catalogue's identifier rules, falling back to the old key for an overload
// with no name.
//
// Overloads that come to share a key are kept in the order of their old keys, so the
// result does not depend on map order.
//
// Takes catalogue (*sqlmodel.Catalogue) which supplies the identifier rules.
// Takes functions (map[string][]*sqlmodel.Function) which is the map to rekey; nil stays
// nil.
//
// Returns map[string][]*sqlmodel.Function which holds the rekeyed overloads.
func rekeyedFunctions(catalogue *sqlmodel.Catalogue, functions map[string][]*sqlmodel.Function) map[string][]*sqlmodel.Function {
	if functions == nil {
		return nil
	}
	keyed := make(map[string][]*sqlmodel.Function, len(functions))
	for _, key := range slices.Sorted(maps.Keys(functions)) {
		for _, function := range functions[key] {
			name := key
			if function != nil && function.Name != "" {
				name = function.Name
			}
			newKey := catalogue.Key(name)
			keyed[newKey] = append(keyed[newKey], function)
		}
	}
	return keyed
}
