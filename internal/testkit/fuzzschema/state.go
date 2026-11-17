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
	"slices"
	"strconv"
)

// choiceReader reads choice bytes in order, zero past the end.
type choiceReader struct {
	// data holds the input without the regression marker.
	data []byte

	// position is the next choice's index.
	position int
}

// choice holds the argument bytes of one operation.
type choice struct {
	// table selects the relation an operation changes.
	table int

	// column selects the column, or the shape of a new table.
	column int

	// variant selects a type family or spelling.
	variant int

	// flags holds independent yes or no choices.
	flags int

	// extra holds a second family or spelling choice.
	extra int
}

// column is the model of one table column.
type column struct {
	// name is the column's name.
	name string

	// family is the coarse type family.
	family string

	// enum names the enum type of an enum column on an engine with enum types.
	enum string

	// nullable reports whether the column admits NULL.
	nullable bool

	// hasDefault reports a default other than generation.
	hasDefault bool

	// generated reports a generated column.
	generated bool
}

// namedKey is a unique key or check constraint.
type namedKey struct {
	// name is the constraint or index name.
	name string

	// columns holds the key's columns.
	columns []string

	// inline reports a key declared in CREATE TABLE, which SQLite and DuckDB keep as a
	// constraint rather than an index of its own.
	inline bool
}

// foreignKey is the model of a foreign key.
type foreignKey struct {
	// name is the constraint name.
	name string

	// table is the referenced table's key.
	table string

	// columns holds the referencing columns.
	columns []string

	// references holds the referenced columns.
	references []string
}

// relation is the model of a table, view or materialised view.
type relation struct {
	// namespace is empty or Namespace.
	namespace string

	// name is the relation's name.
	name string

	// kind is KindTable, KindView or KindMaterialisedView.
	kind string

	// source is the key of a view's table.
	source string

	// primaryName names the primary key constraint where the engine drops it by name.
	primaryName string

	// sourceColumns holds the table columns a view reads, in its column order.
	sourceColumns []string

	// columns holds the columns in engine order.
	columns []*column

	// primaryKey holds the primary key columns.
	primaryKey []string

	// uniques holds the unique keys.
	uniques []namedKey

	// checks holds the CHECK constraint names.
	checks []string

	// foreign holds the foreign keys.
	foreign []foreignKey

	// indexes holds the indexes MySQL and MariaDB create for foreign keys.
	indexes []namedKey

	// partitions holds the indexes of the attached partitions of a partitioned table.
	partitions []int

	// partitioned reports a partitioned table.
	partitioned bool
}

// enumType is the model of an enum type object.
type enumType struct {
	// name is the type's name.
	name string

	// values holds its values in order.
	values []string
}

// history is the decoder's state while it writes a history.
type history struct {
	// features records the covered features.
	features map[string]bool

	// used records every relation key a history has created.
	used map[string]bool

	// engine names the dialect.
	engine string

	// relations holds the live tables and views in creation order.
	relations []*relation

	// sequences holds the live sequence keys.
	sequences []string

	// enums holds the live enum types.
	enums []*enumType

	// migrations holds the finished migrations.
	migrations []Migration

	// reader supplies the choices.
	reader choiceReader

	// serial numbers constraints, indexes and sequences.
	serial int

	// namespace reports whether the second schema exists.
	namespace bool
}

// next reads the next choice byte.
//
// Returns int which is the byte, or zero past the end.
func (reader *choiceReader) next() int {
	value := reader.at(reader.position)
	reader.position++
	return value
}

// at reads one choice byte by index.
//
// Takes index (int) which locates the byte.
//
// Returns int which is the byte, or zero past the end.
func (reader *choiceReader) at(index int) int {
	if index < 0 || index >= len(reader.data) {
		return 0
	}
	return int(reader.data[index])
}

// flag reports one bit of the flags byte.
//
// Takes bit (int) which is the bit's index.
//
// Returns bool which is true when the bit is set.
func (chosen choice) flag(bit int) bool {
	return chosen.flags>>bit&1 == 1
}

// allowed reports whether a history may use a feature.
//
// Takes feature (string) which names the feature.
//
// Returns bool which is false for features the engine lacks.
func (state *history) allowed(feature string) bool {
	return supports(state.engine, feature)
}

// cover records features the history uses.
//
// Takes features (...string) which name them.
func (state *history) cover(features ...string) {
	for _, feature := range features {
		state.features[feature] = true
	}
}

// next numbers a new constraint, index or sequence.
//
// Returns int which is unique within the history.
func (state *history) next() int {
	state.serial++
	return state.serial
}

// operation decodes one operation, trying the following kinds when the chosen one does
// not apply to the current schema.
//
// Returns []string which holds the operation's statements, or nil when none applies.
func (state *history) operation() []string {
	kind := state.reader.next()
	chosen := choice{table: state.reader.next(), column: state.reader.next(), variant: state.reader.next(),
		flags: state.reader.next(), extra: state.reader.next()}
	for offset := range len(operations) {
		if statements := operations[(kind+offset)%len(operations)](state, chosen); len(statements) != 0 {
			return statements
		}
	}
	return nil
}

// tables lists the physical tables a predicate admits.
//
// Takes admit (func(*relation) bool) which filters them.
//
// Returns []*relation which keeps creation order.
func (state *history) tables(admit func(*relation) bool) []*relation {
	var result []*relation
	for _, candidate := range state.relations {
		if candidate.kind == KindTable && admit(candidate) {
			result = append(result, candidate)
		}
	}
	return result
}

// find returns a live relation by key.
//
// Takes key (string) which is the relation's key.
//
// Returns *relation which is nil when none exists.
func (state *history) find(key string) *relation {
	for _, candidate := range state.relations {
		if candidate.key() == key {
			return candidate
		}
	}
	return nil
}

// free reports whether a relation name is unused in a schema.
//
// Takes namespace (string) which is the schema.
// Takes name (string) which is the candidate name.
//
// Returns bool which is false when a relation or attached partition holds the name.
func (state *history) free(namespace, name string) bool {
	key := qualify(namespace, name)
	for _, candidate := range state.relations {
		if candidate.key() == key {
			return false
		}
		for _, index := range candidate.partitions {
			if qualify(candidate.namespace, partitionName(index)) == key {
				return false
			}
		}
	}
	return true
}

// referenced reports whether another table's foreign key references a table.
//
// Takes target (*relation) which is the table.
//
// Returns bool which is true when a foreign key of another table names it.
func (state *history) referenced(target *relation) bool {
	for _, candidate := range state.relations {
		if candidate == target {
			continue
		}
		for _, key := range candidate.foreign {
			if key.table == target.key() {
				return true
			}
		}
	}
	return false
}

// viewed reports whether a view reads a table, or a column of it.
//
// Takes target (*relation) which is the table.
// Takes name (string) which is a column name, or empty for any.
//
// Returns bool which is true when a view depends on it.
func (state *history) viewed(target *relation, name string) bool {
	for _, candidate := range state.relations {
		if candidate.kind != KindTable && candidate.source == target.key() && (name == "" || slices.Contains(candidate.sourceColumns, name)) {
			return true
		}
	}
	return false
}

// locked reports a DuckDB table whose columns no ALTER may change, as another catalogue
// entry depends on it. That entry is a foreign key of another table, or an index of its
// own.
//
// Takes target (*relation) which is the table.
//
// Returns bool which is true on DuckDB for such a table.
func (state *history) locked(target *relation) bool {
	if state.engine != engineDuckDB {
		return false
	}
	return state.referenced(target) || slices.ContainsFunc(target.uniques, func(key namedKey) bool { return !key.inline })
}

// indexedAfter reports whether a key covers a column after the named one, which DuckDB's
// indexes keep from being dropped.
//
// Takes name (string) which is the column's name.
//
// Returns bool which is true when a later column is keyed.
func (table *relation) indexedAfter(name string) bool {
	position := slices.IndexFunc(table.columns, func(member *column) bool { return member.name == name })
	return slices.ContainsFunc(table.columns[position+1:], func(member *column) bool { return table.keyed(member.name) })
}

// remove deletes a relation from the history.
//
// Takes target (*relation) which is the relation.
func (state *history) remove(target *relation) {
	state.relations = slices.DeleteFunc(state.relations, func(candidate *relation) bool { return candidate == target })
}

// key returns the relation's qualified name.
//
// Returns string which identifies the relation.
func (table *relation) key() string {
	return qualify(table.namespace, table.name)
}

// column returns a column by name.
//
// Takes name (string) which is the column's name.
//
// Returns *column which is nil when the table has no such column.
func (table *relation) column(name string) *column {
	for _, candidate := range table.columns {
		if candidate.name == name {
			return candidate
		}
	}
	return nil
}

// keyed reports whether a column belongs to the primary key, a unique key or a foreign
// key of the table.
//
// Takes name (string) which is the column's name.
//
// Returns bool which is true when a key includes it.
func (table *relation) keyed(name string) bool {
	if slices.Contains(table.primaryKey, name) {
		return true
	}
	for _, key := range table.uniques {
		if slices.Contains(key.columns, name) {
			return true
		}
	}
	for _, key := range table.foreign {
		if slices.Contains(key.columns, name) {
			return true
		}
	}
	return false
}

// freeColumn returns the first unused name of a pool.
//
// Takes pool ([]string) which lists candidate names.
// Takes start (int) which rotates the pool.
//
// Returns string which is empty when every name is used.
func (table *relation) freeColumn(pool []string, start int) string {
	for offset := range pool {
		name := pool[(start+offset)%len(pool)]
		if table.column(name) == nil {
			return name
		}
	}
	return ""
}

// renameColumn renames a column and every key and view reference to it.
//
// Takes state (*history) which holds the views.
// Takes from (string) which is the old name.
// Takes to (string) which is the new name.
func (table *relation) renameColumn(state *history, from, to string) {
	table.column(from).name = to
	rename := func(names []string) {
		for index, name := range names {
			if name == from {
				names[index] = to
			}
		}
	}
	rename(table.primaryKey)
	for index := range table.uniques {
		rename(table.uniques[index].columns)
	}
	for index := range table.foreign {
		rename(table.foreign[index].columns)
	}
	for index := range table.indexes {
		rename(table.indexes[index].columns)
	}
	for _, candidate := range state.relations {
		if candidate.kind != KindTable && candidate.source == table.key() {
			rename(candidate.sourceColumns)
		}
	}
}

// newHistory starts an empty history.
//
// Takes engine (string) which names the dialect.
// Takes data ([]byte) which holds the choices.
//
// Returns *history which is ready to decode operations.
func newHistory(engine string, data []byte) *history {
	return &history{features: make(map[string]bool), used: make(map[string]bool), engine: engine,
		reader: choiceReader{data: data, position: headerWidth}, relations: nil, sequences: nil, enums: nil,
		migrations: nil, serial: 0, namespace: false}
}

// partitionName names a partition by its index.
//
// Takes index (int) which is the partition's index.
//
// Returns string which is the partition table's name.
func partitionName(index int) string {
	return tablePartitioned + "_" + strconv.Itoa(index)
}
