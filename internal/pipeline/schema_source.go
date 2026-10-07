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

package pipeline

import (
	"maps"
	"slices"

	"seagull.sh/seagull/internal/sqlmodel"
)

// dropNilEntries removes the nil entries a schema source may leave in its catalogue: a
// nil schema, and a nil table, view, enum, composite type, sequence, domain or function
// overload, so nothing downstream reads through one.
//
// Takes catalogue (*sqlmodel.Catalogue) which is updated in place; nil is left alone.
func dropNilEntries(catalogue *sqlmodel.Catalogue) {
	if catalogue == nil {
		return
	}
	maps.DeleteFunc(catalogue.Schemas, func(_ string, schema *sqlmodel.Schema) bool { return schema == nil })
	for _, schema := range catalogue.Schemas {
		dropNilValues(schema.Tables)
		dropNilValues(schema.Views)
		dropNilValues(schema.Enums)
		dropNilValues(schema.CompositeTypes)
		dropNilValues(schema.Sequences)
		dropNilValues(schema.Domains)
		for name, overloads := range schema.Functions {
			schema.Functions[name] = slices.DeleteFunc(overloads, func(function *sqlmodel.Function) bool { return function == nil })
		}
	}
}

// dropNilValues removes the entries of a map whose value is a nil pointer.
//
// Takes entries (map[string]*T) which is updated in place.
func dropNilValues[T any](entries map[string]*T) {
	maps.DeleteFunc(entries, func(_ string, value *T) bool { return value == nil })
}
