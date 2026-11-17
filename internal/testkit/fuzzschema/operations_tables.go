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
)

const (
	// featureCreateTable covers CREATE TABLE.
	featureCreateTable = "operation/create-table"

	// featureDropTable covers DROP TABLE.
	featureDropTable = "operation/drop-table"

	// featureRecreate covers a table created under a name a dropped or renamed table held.
	featureRecreate = "operation/recreate-table"

	// featureRenameReferenced covers renaming a table a foreign key references.
	featureRenameReferenced = "operation/rename-referenced-table"

	// featureCrossSchemaKey covers a foreign key referencing a table in another schema.
	featureCrossSchemaKey = "operation/cross-schema-foreign-key"

	// featureRenameTable covers renaming a table.
	featureRenameTable = "operation/rename-table"

	// featureCreateCheck covers a CHECK constraint in CREATE TABLE.
	featureCreateCheck = "operation/create-check"

	// featureCreateUnique covers a UNIQUE constraint in CREATE TABLE.
	featureCreateUnique = "operation/create-unique"

	// featureCreatePrimaryKey covers a PRIMARY KEY in CREATE TABLE.
	featureCreatePrimaryKey = "operation/create-primary-key"

	// featureCreateForeignKey covers a FOREIGN KEY in CREATE TABLE.
	featureCreateForeignKey = "operation/create-foreign-key"

	// featureCreateGenerated covers a generated column in CREATE TABLE.
	featureCreateGenerated = "operation/create-generated-column"

	// featurePartition covers partitioned tables.
	featurePartition = "operation/partition"

	// featureDetach covers detaching a partition into a table of its own.
	featureDetach = "operation/detach-partition"

	// featureNamespace covers tables in a second schema.
	featureNamespace = "operation/namespace"

	// featureDropNamespace covers dropping the second schema with its tables.
	featureDropNamespace = "operation/drop-namespace"

	// tablePartitioned names the partitioned table.
	tablePartitioned = "fuzz_p"

	// columnCountMask selects the shape bits that count a new table's columns.
	columnCountMask = 3

	// maxCreateColumns bounds the ordinary columns CREATE TABLE declares.
	maxCreateColumns = 3

	// maxPartitions bounds the partitions of the partitioned table.
	maxPartitions = 2
)

const (
	// shapeGenerated adds a generated column to a new table.
	shapeGenerated = 2 + iota

	// shapeStored stores that generated column where the engine offers a choice.
	shapeStored

	// shapePrimaryKey declares the primary key.
	shapePrimaryKey

	// shapeCheck declares a CHECK constraint.
	shapeCheck

	// shapeUnique declares a UNIQUE constraint.
	shapeUnique

	// shapeForeignKey adds a column with a foreign key.
	shapeForeignKey
)

// tablePool names the tables CREATE TABLE makes in the default schema.
var tablePool = []string{"fuzz_a", "fuzz_b", "fuzz_c"}

// renamePool names the tables a rename produces in the default schema.
var renamePool = []string{"fuzz_a", "fuzz_b", "fuzz_c", "fuzz_d", "fuzz_e"}

// namespacePool names the tables of the second schema.
var namespacePool = []string{"fuzz_q", "fuzz_r"}

// operations lists every operation by its kind byte.
var operations = []func(*history, choice) []string{
	operationCreateTable, operationAddColumn, operationDropColumn, operationRenameColumn, operationAlterType, operationDefault,
	operationNullability, operationCheck, operationUnique, operationPrimaryKey, operationForeignKey, operationDropTable,
	operationRenameTable, operationSequence, operationView, operationEnum, operationGenerated, operationPartition,
	operationNamespace,
}

// freeName returns the first free relation name of a pool.
//
// Takes namespace (string) which is the schema.
// Takes pool ([]string) which lists candidate names.
// Takes start (int) which rotates the pool.
//
// Returns string which is empty when every name is taken.
func (state *history) freeName(namespace string, pool []string, start int) string {
	for offset := range pool {
		if name := pool[(start+offset)%len(pool)]; state.free(namespace, name) {
			return name
		}
	}
	return ""
}

// buildTable creates a table with its columns, keys and constraints.
//
// Takes namespace (string) which is the table's schema.
// Takes name (string) which is the free name.
// Takes chosen (choice) which shapes the table.
// Takes partitioned (bool) which partitions the table by id.
//
// Returns []string which holds the statements.
func (state *history) buildTable(namespace, name string, chosen choice, partitioned bool) []string {
	table := &relation{namespace: namespace, name: name, kind: KindTable, source: "", primaryName: "", sourceColumns: nil,
		columns:    []*column{{name: identifier, family: FamilyInteger, enum: "", nullable: false, hasDefault: false, generated: false}},
		primaryKey: nil, uniques: nil, checks: nil, foreign: nil, indexes: nil, partitions: nil, partitioned: partitioned}
	var statements []string
	usable := families(state.engine)
	for index := range 1 + (chosen.column&columnCountMask)%maxCreateColumns {
		family := usable[(chosen.variant+index*(chosen.extra+1))%len(usable)]
		added, prelude := state.newColumn(table, family, chosen.flag(2*index), chosen.flag(2*index+1), index)
		statements = append(statements, prelude...)
		table.columns = append(table.columns, added)
	}
	shape := choice{table: 0, column: 0, variant: 0, flags: chosen.column, extra: 0}
	if shape.flag(shapeGenerated) && state.allowed(featureCreateGenerated) {
		table.columns = append(table.columns, generatedColumn(state.engine, "g1"))
		state.cover(featureCreateGenerated)
	}
	naming := keyNaming{unnamed: chosen.flag(flagUnnamed) && state.allowed(featureDefaultName), columnLevel: false, suffixes: map[string]string{}}
	naming.columnLevel = naming.unnamed && chosen.flag(flagColumnLevel)
	statements = append(statements, createTable(state.engine, table, state.tableDefinitions(table, shape, &naming)))
	if partitioned && isPostgresFamily(state.engine) && state.free(namespace, partitionName(0)) {
		statements = append(statements, partitionStatement(table, 0))
		table.partitions = []int{0}
	}
	state.cover(featureCreateTable)
	if state.used[table.key()] {
		state.cover(featureRecreate)
	}
	state.used[table.key()] = true
	state.relations = append(state.relations, table)
	return statements
}

// tableDefinitions declares a new table's columns, keys and constraints.
//
// Takes table (*relation) which is the new table, which gains the declared keys.
// Takes shape (choice) which holds the table's shape flags.
// Takes naming (*keyNaming) which says how the table declares its keys.
//
// Returns []string which holds the column and constraint definitions.
func (state *history) tableDefinitions(table *relation, shape choice, naming *keyNaming) []string {
	var reference string
	if shape.flag(shapeForeignKey) && !table.partitioned && state.allowed(featureCreateForeignKey) {
		reference = state.inlineForeignKey(table, naming)
	}
	definitions := make([]string, 0, len(table.columns))
	for _, member := range table.columns {
		definitions = append(definitions, definition(state.engine, member, shape.flag(shapeStored)))
	}
	constraints := state.tableConstraints(table, shape, naming)
	for index, member := range table.columns {
		definitions[index] += naming.suffixes[member.name]
	}
	definitions = append(definitions, constraints...)
	if reference != "" {
		definitions = append(definitions, reference)
	}
	for _, key := range table.foreign {
		table.addImplicitIndex(state, key, naming.unnamed)
	}
	if naming.unnamed && len(table.uniques)+len(table.checks)+len(table.foreign) != 0 {
		state.cover(featureDefaultName)
	}
	if len(naming.suffixes) != 0 {
		state.cover(featureColumnConstraint)
	}
	return definitions
}

// keyNaming holds how a new table declares its keys and constraints.
type keyNaming struct {
	// suffixes maps a column's name to the constraints its definition declares.
	suffixes map[string]string

	// unnamed leaves the keys and constraints for the engine to name.
	unnamed bool

	// columnLevel declares them with their columns where the engine keeps them.
	columnLevel bool
}

// declare adds an unnamed constraint to a column's definition where the engine keeps one
// declared there.
//
// Takes column (string) which is the column.
// Takes clause (string) which is the constraint.
// Takes kept (bool) which is false where the engine ignores or keeps apart such a clause.
//
// Returns bool which is true when the column declares it.
func (naming *keyNaming) declare(column, clause string, kept bool) bool {
	if !naming.columnLevel || !kept {
		return false
	}
	naming.suffixes[column] += " " + clause
	return true
}

// constraintName names a new table's constraint, or leaves it for the engine to name.
//
// Takes table (*relation) which is the new table.
// Takes naming (*keyNaming) which says whether it is named.
// Takes kind (constraintKind) which is the constraint's kind.
// Takes column (string) which is its column.
// Takes prefix (string) which starts a name the history gives.
//
// Returns name (string) which is the constraint's name.
// Returns clause (string) which is the CONSTRAINT clause naming it, or empty.
func (state *history) constraintName(table *relation, naming *keyNaming, kind constraintKind, column, prefix string) (name, clause string) {
	if naming.unnamed {
		return state.defaultName(table, kind, column, true), ""
	}
	name = fmt.Sprintf("%s_%d", prefix, state.next())
	return name, "CONSTRAINT " + name + " "
}

// tableConstraints declares a new table's key and constraints.
//
// Takes table (*relation) which is the new table, which gains the declared keys.
// Takes shape (choice) which holds the table's shape flags.
// Takes naming (*keyNaming) which says how the table declares them.
//
// Returns []string which holds the table-level constraint definitions.
func (state *history) tableConstraints(table *relation, shape choice, naming *keyNaming) []string {
	var definitions []string
	if state.engine == engineClickHouse {
		table.primaryKey = []string{identifier}
		state.cover(featureCreatePrimaryKey)
	} else if shape.flag(shapePrimaryKey) && state.allowed(featureCreatePrimaryKey) {
		name, clause := state.constraintName(table, naming, kindPrimary, identifier, "fuzz_pk")
		table.primaryName = name
		table.primaryKey = []string{identifier}

		if !naming.declare(identifier, "PRIMARY KEY", !isMySQLFamily(state.engine)) {
			definitions = append(definitions, clause+"PRIMARY KEY (id)")
		}
		state.cover(featureCreatePrimaryKey)
		if state.engine == engineSQLite {

			table.column(identifier).hasDefault = true
		}
	}
	if shape.flag(shapeCheck) && state.allowed(featureCreateCheck) {
		definitions = append(definitions, state.tableCheck(table, naming)...)
	}
	if shape.flag(shapeUnique) && !table.partitioned && state.allowed(featureCreateUnique) {
		definitions = append(definitions, state.tableUnique(table, naming)...)
	}
	return definitions
}

// tableCheck declares a new table's CHECK constraint.
//
// Takes table (*relation) which is the new table, which gains the check.
// Takes naming (*keyNaming) which says how the table declares it.
//
// Returns []string which holds its table-level definition, if any.
func (state *history) tableCheck(table *relation, naming *keyNaming) []string {
	name, clause := state.constraintName(table, naming, kindCheck, identifier, "fuzz_ck")
	table.checks = append(table.checks, name)
	state.cover(featureCreateCheck)

	switch {
	case naming.declare(identifier, "CHECK ("+checkExpression+")", state.engine != engineMariaDB):
		return nil
	case clause == "":
		return []string{"CHECK (" + checkExpression + ")"}
	default:
		return []string{checkClause(state.engine, name)}
	}
}

// tableUnique declares a new table's unique key on its first column that may have one.
//
// Takes table (*relation) which is the new table, which gains the key.
// Takes naming (*keyNaming) which says how the table declares it.
//
// Returns []string which holds its table-level definition, if any.
func (state *history) tableUnique(table *relation, naming *keyNaming) []string {
	target := uniqueCandidate(table)
	if target == nil {
		return nil
	}
	name, clause := state.constraintName(table, naming, kindUnique, target.name, "fuzz_uq")
	table.uniques = append(table.uniques, namedKey{name: name, columns: []string{target.name}, inline: true})
	state.cover(featureCreateUnique)
	if naming.declare(target.name, "UNIQUE", true) {
		return nil
	}
	return []string{clause + "UNIQUE (" + target.name + ")"}
}

// inlineForeignKey adds a referencing column and its foreign key to a new table.
//
// Takes table (*relation) which is the new table, which gains the column.
// Takes naming (*keyNaming) which says how the table declares the key.
//
// Returns string which is the table-level constraint definition, or empty when no table
// can be referenced or the column declares the key.
func (state *history) inlineForeignKey(table *relation, naming *keyNaming) string {
	parents := state.tables(func(candidate *relation) bool { return state.referenceable(candidate, table) })
	name := table.freeColumn(columnPool, 0)
	if len(parents) == 0 || name == "" {
		return ""
	}
	parent := parents[0]
	added := &column{name: name, family: FamilyInteger, enum: "", nullable: true, hasDefault: false, generated: false}
	table.columns = append(table.columns, added)
	keyName, clause := state.constraintName(table, naming, kindForeign, name, "fuzz_fk")
	key := foreignKey{name: keyName, table: parent.key(), columns: []string{name}, references: []string{identifier}}
	table.foreign = append(table.foreign, key)
	state.cover(featureCreateForeignKey)
	if parent.namespace != table.namespace {
		state.cover(featureCrossSchemaKey)
	}

	if naming.declare(name, "REFERENCES "+qualified(parent)+" (id)", state.engine != engineMySQL) {
		return ""
	}
	return fmt.Sprintf("%sFOREIGN KEY (%s) REFERENCES %s (id)", clause, name, qualified(parent))
}

// referenceable reports whether a foreign key of a table may reference a candidate.
//
// Takes candidate (*relation) which is the referenced table.
// Takes child (*relation) which is the referencing table.
//
// Returns bool which is true for another unpartitioned table keyed by id alone.
func (state *history) referenceable(candidate, child *relation) bool {
	if candidate == child || candidate.partitioned || !slices.Equal(candidate.primaryKey, []string{identifier}) {
		return false
	}
	return candidate.namespace == child.namespace || state.allowed(featureCrossSchemaKey)
}

// renameableTable reports whether a table may be renamed. That needs no view reading it,
// no partitioning, and no DuckDB entry depending on it.
//
// Takes candidate (*relation) which is the table.
//
// Returns bool which is true when a rename applies.
func (state *history) renameableTable(candidate *relation) bool {
	if candidate.partitioned || state.viewed(candidate, "") || (state.referenced(candidate) && !state.allowed(featureRenameReferenced)) {
		return false
	}
	return !state.locked(candidate)
}

// retarget points foreign keys at a renamed table, as every engine that renames a
// referenced table does.
//
// Takes previous (string) which is the table's old key.
// Takes next (string) which is its new key.
func (state *history) retarget(previous, next string) {
	for _, candidate := range state.relations {
		for index := range candidate.foreign {
			if candidate.foreign[index].table == previous {
				candidate.foreign[index].table = next
			}
		}
	}
}

// detach turns the first partition into a table of its own.
//
// Takes parent (*relation) which is the partitioned table.
//
// Returns []string which holds the statement.
func (state *history) detach(parent *relation) []string {
	index := parent.partitions[0]
	parent.partitions = parent.partitions[1:]
	child := parent.partition(index)
	state.relations = append(state.relations, child)
	state.used[child.key()] = true
	state.cover(featureDetach)
	return []string{alterTable + qualified(parent) + " DETACH PARTITION " + qualified(child)}
}

// partition models one partition of a partitioned table, which carries the table's
// columns, primary key and CHECK constraints.
//
// Takes index (int) which numbers the partition.
//
// Returns *relation which is the partition as a table.
func (table *relation) partition(index int) *relation {
	columns := make([]*column, 0, len(table.columns))
	for _, member := range table.columns {
		duplicate := *member
		columns = append(columns, &duplicate)
	}
	name := partitionName(index)
	primaryName := ""
	if len(table.primaryKey) != 0 {
		primaryName = name + "_pkey"
	}
	return &relation{namespace: table.namespace, name: name, kind: KindTable, source: "", primaryName: primaryName, sourceColumns: nil,
		columns: columns, primaryKey: slices.Clone(table.primaryKey), uniques: nil, checks: slices.Clone(table.checks), foreign: nil,
		indexes: nil, partitions: nil, partitioned: false}
}

// namespaceDroppable reports whether nothing outside the second schema depends on it.
//
// Returns bool which is true when no foreign key or view outside it reads its tables.
func (state *history) namespaceDroppable() bool {
	for _, candidate := range state.relations {
		if candidate.namespace == Namespace {
			continue
		}
		if candidate.kind != KindTable && state.find(candidate.source) != nil && state.find(candidate.source).namespace == Namespace {
			return false
		}
		for _, key := range candidate.foreign {
			if target := state.find(key.table); target != nil && target.namespace == Namespace {
				return false
			}
		}
	}
	return true
}

// operationCreateTable creates a table in the default schema.
//
// Takes state (*history) which holds the schema.
// Takes chosen (choice) which shapes the table.
//
// Returns []string which holds the statements, or nil when every name is taken.
func operationCreateTable(state *history, chosen choice) []string {
	name := state.freeName("", tablePool, chosen.table)
	if name == "" {
		return nil
	}
	return state.buildTable("", name, chosen, false)
}

// uniqueCandidate returns the first column a unique key may cover.
//
// Takes table (*relation) which is the table.
//
// Returns *column which is nil when no column qualifies.
func uniqueCandidate(table *relation) *column {
	for _, candidate := range table.columns {
		if candidate.name != identifier && !candidate.generated && uniqueFamily(candidate.family) && !table.keyed(candidate.name) {
			return candidate
		}
	}
	return nil
}

// uniqueFamily reports families every engine can index uniquely.
//
// Takes family (string) which is the family.
//
// Returns bool which is true for scalar comparable families.
func uniqueFamily(family string) bool {
	return slices.Contains([]string{FamilyInteger, FamilyText, FamilyDecimal, FamilyDate, FamilyTimestamp, FamilyTimestampTZ, FamilyUUID}, family)
}

// generatedColumn models a generated column computed from id.
//
// Takes engine (string) which names the dialect.
// Takes name (string) which is the column's name.
//
// Returns *column which is the model.
func generatedColumn(engine, name string) *column {
	return &column{name: name, family: FamilyInteger, enum: "", nullable: engine != engineClickHouse, hasDefault: false, generated: true}
}

// operationDropTable drops a table no other object depends on.
//
// Takes state (*history) which holds the schema.
// Takes chosen (choice) which picks the table.
//
// Returns []string which holds the statement, or nil when every table has dependants.
func operationDropTable(state *history, chosen choice) []string {
	candidates := state.tables(func(candidate *relation) bool { return !state.referenced(candidate) && !state.viewed(candidate, "") })
	if len(candidates) == 0 {
		return nil
	}
	target := candidates[chosen.table%len(candidates)]
	state.remove(target)
	state.cover(featureDropTable)
	return []string{"DROP TABLE " + qualified(target)}
}

// operationRenameTable renames a table no view reads.
//
// Takes state (*history) which holds the schema.
// Takes chosen (choice) which picks the table, its new name and the spelling.
//
// Returns []string which holds the statement, or nil when no table can be renamed.
func operationRenameTable(state *history, chosen choice) []string {
	candidates := state.tables(state.renameableTable)
	if len(candidates) == 0 {
		return nil
	}
	target := candidates[chosen.table%len(candidates)]
	pool := renamePool
	if target.namespace != "" {
		pool = namespacePool
	}
	name := state.freeName(target.namespace, pool, chosen.column)
	if name == "" {
		return nil
	}
	statement := alterTable + qualified(target) + " RENAME TO " + name
	if state.engine == engineClickHouse || (isMySQLFamily(state.engine) && chosen.flag(0)) {
		statement = "RENAME TABLE " + qualified(target) + " TO " + name
	}
	previous, previousName := target.key(), target.name
	if state.referenced(target) {
		state.cover(featureRenameReferenced)
	}
	target.name = name
	target.renameGeneratedNames(state, previousName)
	state.retarget(previous, target.key())
	state.used[target.key()] = true
	state.cover(featureRenameTable)
	return []string{statement}
}

// operationPartition creates the partitioned table, or detaches or adds a partition.
//
// Takes state (*history) which holds the schema.
// Takes chosen (choice) which shapes the table or picks the change.
//
// Returns []string which holds the statements, or nil when the engine has no partitions.
func operationPartition(state *history, chosen choice) []string {
	if !state.allowed(featurePartition) {
		return nil
	}
	parent := state.find(tablePartitioned)
	if parent == nil {
		if !state.free("", tablePartitioned) {
			return nil
		}
		state.cover(featurePartition)
		return state.buildTable("", tablePartitioned, chosen, true)
	}
	if !parent.partitioned || !isPostgresFamily(state.engine) {
		return nil
	}
	if len(parent.partitions) != 0 && chosen.flag(0) && state.allowed(featureDetach) {
		return state.detach(parent)
	}
	for index := range maxPartitions {
		if !slices.Contains(parent.partitions, index) && state.free("", partitionName(index)) {
			parent.partitions = append(parent.partitions, index)
			state.cover(featurePartition)
			return []string{partitionStatement(parent, index)}
		}
	}
	return nil
}

// operationNamespace creates the second schema and a table in it, adds a table to it, or
// drops it with its tables.
//
// Takes state (*history) which holds the schema.
// Takes chosen (choice) which shapes the table or picks the change.
//
// Returns []string which holds the statements, or nil when the engine has no schemas.
func operationNamespace(state *history, chosen choice) []string {
	if !state.allowed(featureNamespace) {
		return nil
	}
	if !state.namespace {
		state.namespace = true
		state.cover(featureNamespace)
		return append([]string{"CREATE SCHEMA " + Namespace}, state.buildTable(Namespace, namespacePool[0], chosen, false)...)
	}
	if chosen.flag(0) && state.allowed(featureDropNamespace) && state.namespaceDroppable() {
		state.relations = slices.DeleteFunc(state.relations, func(candidate *relation) bool { return candidate.namespace == Namespace })
		state.namespace = false
		state.cover(featureDropNamespace)
		return []string{"DROP SCHEMA " + Namespace + " CASCADE"}
	}
	name := state.freeName(Namespace, namespacePool, chosen.table)
	if name == "" {
		return nil
	}
	state.cover(featureNamespace)
	return state.buildTable(Namespace, name, chosen, false)
}
