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

package fuzzschema

import (
	"fmt"
	"slices"
	"strings"
)

const (
	// featureSequence covers creating and dropping sequences.
	featureSequence = "operation/sequence"

	// featureView covers creating and dropping views.
	featureView = "operation/view"

	// featureMaterialised covers creating and dropping materialised views.
	featureMaterialised = "operation/materialised-view"

	// featureEnumType covers enum type objects.
	featureEnumType = "operation/enum-type"

	// featureEnumValue covers adding a value to an enum type.
	featureEnumValue = "operation/enum-add-value"

	// featureDropMaterialisedTable covers dropping a ClickHouse materialised view with DROP
	// TABLE.
	featureDropMaterialisedTable = "operation/drop-materialised-view-as-table"

	// maxSequences bounds the live sequences.
	maxSequences = 2
)

// viewPool names the views.
var viewPool = []string{"fuzz_v", "fuzz_w"}

// enumPool names the enum types.
var enumPool = []string{"fuzz_mood", "fuzz_tone"}

// createEnum creates an enum type from the pool.
//
// Takes index (int) which picks its name.
//
// Returns []string which holds the statement.
func (state *history) createEnum(index int) []string {
	created := &enumType{name: enumPool[index], values: slices.Clone(enumValues)}
	state.enums = append(state.enums, created)
	state.cover(featureEnumType)
	return []string{"CREATE TYPE " + created.name + " AS ENUM ('" + strings.Join(enumValues, "', '") + "')"}
}

// enumUsed reports whether a column of a table has an enum type.
//
// Takes name (string) which is the type's name.
//
// Returns bool which is true while a column uses it.
func (state *history) enumUsed(name string) bool {
	for _, table := range state.relations {
		for _, member := range table.columns {
			if member.enum == name {
				return true
			}
		}
	}
	return false
}

// operationSequence creates a sequence, or drops the newest.
//
// Takes state (*history) which holds the schema.
// Takes chosen (choice) which picks the change.
//
// Returns []string which holds the statement, or nil when the engine has no sequences.
func operationSequence(state *history, chosen choice) []string {
	if !state.allowed(featureSequence) {
		return nil
	}
	state.cover(featureSequence)
	if len(state.sequences) != 0 && (chosen.flag(0) || len(state.sequences) == maxSequences) {
		name := state.sequences[len(state.sequences)-1]
		state.sequences = state.sequences[:len(state.sequences)-1]
		return []string{"DROP SEQUENCE " + name}
	}
	name := fmt.Sprintf("fuzz_seq_%d", state.next())
	state.sequences = append(state.sequences, name)
	return []string{"CREATE SEQUENCE " + name}
}

// operationView creates a view or materialised view of id and one column, or drops one.
//
// Takes state (*history) which holds the schema.
// Takes chosen (choice) which picks the table, column, kind and change.
//
// Returns []string which holds the statement, or nil when nothing can change.
func operationView(state *history, chosen choice) []string {
	views := slices.DeleteFunc(slices.Clone(state.relations), func(candidate *relation) bool { return candidate.kind == KindTable })
	if len(views) != 0 && chosen.flag(0) {
		target := views[chosen.table%len(views)]
		state.remove(target)
		if target.kind == KindMaterialisedView && state.allowed(featureDropMaterialisedTable) {
			state.cover(featureDropMaterialisedTable)
			return []string{"DROP TABLE " + target.name}
		}
		return []string{dropView(state.engine, target)}
	}
	name := state.freeName("", viewPool, chosen.variant)
	sources := state.columns(func(*relation, *column) bool { return true })
	if name == "" || len(sources) == 0 {
		return nil
	}
	source := sources[chosen.column%len(sources)]
	kind := KindView
	if chosen.flag(1) && state.allowed(featureMaterialised) {
		kind = KindMaterialisedView
	}
	identity, member := *source.table.column(identifier), *source.column
	for _, copied := range []*column{&identity, &member} {
		copied.hasDefault, copied.generated = false, false
	}
	view := &relation{namespace: "", name: name, kind: kind, source: source.table.key(), primaryName: "",
		sourceColumns: []string{identifier, source.column.name}, columns: []*column{&identity, &member}, primaryKey: nil,
		uniques: nil, checks: nil, foreign: nil, indexes: nil, partitions: nil, partitioned: false}
	state.relations = append(state.relations, view)
	state.used[view.key()] = true
	selection := fmt.Sprintf("SELECT id, %s FROM %s", source.column.name, qualified(source.table))
	switch {
	case kind == KindView:
		state.cover(featureView)
		return []string{"CREATE VIEW " + name + " AS " + selection}
	case state.engine == engineClickHouse:
		state.cover(featureMaterialised)
		return []string{"CREATE MATERIALIZED VIEW " + name + mergeTree + orderByID + " AS " + selection}
	default:
		state.cover(featureMaterialised)
		return []string{"CREATE MATERIALIZED VIEW " + name + " AS " + selection}
	}
}

// dropView spells dropping a view or materialised view.
//
// ClickHouse drops a materialised view with DROP VIEW as well.
//
// Takes engine (string) which names the dialect.
// Takes target (*relation) which is the view.
//
// Returns string which is the statement.
func dropView(engine string, target *relation) string {
	if target.kind == KindView || engine == engineClickHouse {
		return "DROP VIEW " + target.name
	}
	return "DROP MATERIALIZED VIEW " + target.name
}

// operationEnum creates an enum type, adds a value to one, or drops one no column uses.
//
// Takes state (*history) which holds the schema.
// Takes chosen (choice) which picks the type and the change.
//
// Returns []string which holds the statement, or nil when the engine has no enum types.
func operationEnum(state *history, chosen choice) []string {
	if !state.allowed(featureEnumType) {
		return nil
	}
	if len(state.enums) != 0 && chosen.flag(0) {
		target := state.enums[chosen.table%len(state.enums)]
		if !state.enumUsed(target.name) {
			state.enums = slices.DeleteFunc(state.enums, func(candidate *enumType) bool { return candidate == target })
			state.cover(featureEnumType)
			return []string{"DROP TYPE " + target.name}
		}
	}
	if len(state.enums) != 0 && chosen.flag(1) && state.allowed(featureEnumValue) {
		target := state.enums[chosen.table%len(state.enums)]
		value := fmt.Sprintf("v%d", state.next())
		target.values = append(target.values, value)
		state.cover(featureEnumValue)
		return []string{"ALTER TYPE " + target.name + " ADD VALUE '" + value + "'"}
	}
	for index := range enumPool {
		if !slices.ContainsFunc(state.enums, func(candidate *enumType) bool { return candidate.name == enumPool[index] }) {
			return state.createEnum(index)
		}
	}
	return nil
}
