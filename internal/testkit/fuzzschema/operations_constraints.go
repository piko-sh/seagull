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
	// featureAddCheck covers ADD CONSTRAINT CHECK.
	featureAddCheck = "operation/add-check"

	// featureDropCheck covers dropping a CHECK constraint.
	featureDropCheck = "operation/drop-check"

	// featureAddUnique covers adding a unique constraint or index.
	featureAddUnique = "operation/add-unique"

	// featureDropUnique covers dropping a unique constraint or index.
	featureDropUnique = "operation/drop-unique"

	// featureAddPrimaryKey covers adding a primary key.
	featureAddPrimaryKey = "operation/add-primary-key"

	// featureDropPrimaryKey covers dropping a primary key.
	featureDropPrimaryKey = "operation/drop-primary-key"

	// featureAddForeignKey covers adding a foreign key.
	featureAddForeignKey = "operation/add-foreign-key"

	// featureDropForeignKey covers dropping a foreign key.
	featureDropForeignKey = "operation/drop-foreign-key"

	// dropConstraint starts every DROP CONSTRAINT clause.
	dropConstraint = " DROP CONSTRAINT "

	// addConstraint starts every ADD CONSTRAINT clause.
	addConstraint = " ADD CONSTRAINT "
)

// dropUnique spells dropping a unique constraint or index.
//
// Takes target (*relation) which is the table.
// Takes name (string) which is the constraint or index name.
//
// Returns string which is the statement.
func (state *history) dropUnique(target *relation, name string) string {
	switch {
	case state.engine == engineCockroachDB:
		return "DROP INDEX " + qualified(target) + "@" + name + " CASCADE"
	case isMySQLFamily(state.engine):
		return alterTable + qualified(target) + " DROP INDEX " + name
	case state.engine == engineSQLite:
		return "DROP INDEX " + name
	case state.engine == engineDuckDB:
		return "DROP INDEX " + qualify(target.namespace, name)
	default:
		return alterTable + qualified(target) + dropConstraint + name
	}
}

// operationCheck adds a CHECK constraint, or drops the newest.
//
// Takes state (*history) which holds the schema.
// Takes chosen (choice) which picks the table, the change and the spelling.
//
// Returns []string which holds the statement, or nil when the engine cannot alter checks.
func operationCheck(state *history, chosen choice) []string {
	candidates := state.tables(func(*relation) bool { return true })
	if len(candidates) == 0 {
		return nil
	}
	target := candidates[chosen.table%len(candidates)]
	prefix := alterTable + qualified(target)
	if len(target.checks) != 0 && chosen.flag(0) && state.allowed(featureDropCheck) {
		name := target.checks[len(target.checks)-1]
		target.checks = target.checks[:len(target.checks)-1]
		state.cover(featureDropCheck)
		if state.engine == engineMySQL && chosen.flag(1) {
			return []string{prefix + " DROP CHECK " + name}
		}
		return []string{prefix + dropConstraint + name}
	}
	if !state.allowed(featureAddCheck) {
		return nil
	}
	if chosen.flag(1) && state.allowed(featureDefaultName) {
		target.checks = append(target.checks, state.defaultName(target, kindCheck, identifier, false))
		state.cover(featureAddCheck, featureDefaultName)
		return []string{prefix + " ADD CHECK (" + checkExpression + ")"}
	}
	name := fmt.Sprintf("fuzz_ck_%d", state.next())
	target.checks = append(target.checks, name)
	state.cover(featureAddCheck)
	return []string{prefix + " ADD " + checkClause(state.engine, name)}
}

// operationUnique adds a unique constraint or index, or drops one.
//
// Takes state (*history) which holds the schema.
// Takes chosen (choice) which picks the table and the change.
//
// Returns []string which holds the statement, or nil when no table can change.
func operationUnique(state *history, chosen choice) []string {
	candidates := state.tables(func(candidate *relation) bool { return !candidate.partitioned })
	if len(candidates) == 0 {
		return nil
	}
	target := candidates[chosen.table%len(candidates)]
	droppable := slices.IndexFunc(target.uniques, func(key namedKey) bool {

		if isMySQLFamily(state.engine) && target.foreignColumn(key.columns[0]) {
			return false
		}
		return !key.inline || (state.engine != engineSQLite && state.engine != engineDuckDB)
	})
	if droppable >= 0 && chosen.flag(0) && state.allowed(featureDropUnique) {
		key := target.uniques[droppable]
		target.uniques = slices.Delete(target.uniques, droppable, droppable+1)
		state.cover(featureDropUnique)
		return []string{state.dropUnique(target, key.name)}
	}
	member := uniqueCandidate(target)
	if member == nil || !state.allowed(featureAddUnique) {
		return nil
	}
	if isMySQLFamily(state.engine) {
		target.dropImplicitIndexes(member.name)
	}
	if chosen.flag(1) && state.allowed(featureDefaultName) {
		name := state.defaultName(target, kindUnique, member.name, false)
		target.uniques = append(target.uniques, namedKey{name: name, columns: []string{member.name}, inline: false})
		state.cover(featureAddUnique, featureDefaultName)
		return []string{alterTable + qualified(target) + " ADD UNIQUE (" + member.name + ")"}
	}
	name := fmt.Sprintf("fuzz_uq_%d", state.next())
	target.uniques = append(target.uniques, namedKey{name: name, columns: []string{member.name}, inline: false})
	state.cover(featureAddUnique)
	if state.engine == engineSQLite || state.engine == engineDuckDB {
		return []string{fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (%s)", name, qualified(target), member.name)}
	}
	return []string{alterTable + qualified(target) + addConstraint + name + " UNIQUE (" + member.name + ")"}
}

// operationPrimaryKey adds a primary key on id, or drops one nothing references.
//
// Takes state (*history) which holds the schema.
// Takes chosen (choice) which picks the table.
//
// Returns []string which holds the statement, or nil when no table can change.
func operationPrimaryKey(state *history, chosen choice) []string {
	candidates := state.tables(func(candidate *relation) bool {
		if candidate.partitioned {
			return false
		}
		if len(candidate.primaryKey) == 0 {
			return state.allowed(featureAddPrimaryKey)
		}
		return state.allowed(featureDropPrimaryKey) && !state.referenced(candidate)
	})
	if len(candidates) == 0 {
		return nil
	}
	target := candidates[chosen.table%len(candidates)]
	prefix := alterTable + qualified(target)
	if len(target.primaryKey) != 0 {
		name := target.primaryName
		target.primaryKey, target.primaryName = nil, ""
		state.cover(featureDropPrimaryKey)
		switch {
		case isMySQLFamily(state.engine) && chosen.flag(1):
			state.cover(featureDefaultName)
			return []string{prefix + " DROP INDEX `" + mysqlPrimary + "`"}
		case isMySQLFamily(state.engine):
			return []string{prefix + " DROP PRIMARY KEY"}
		default:
			return []string{prefix + dropConstraint + name}
		}
	}
	target.primaryKey = []string{identifier}
	state.cover(featureAddPrimaryKey)
	if chosen.flag(0) && state.allowed(featureDefaultName) {
		target.primaryName = state.defaultName(target, kindPrimary, identifier, false)
		state.cover(featureDefaultName)
		return []string{prefix + " ADD PRIMARY KEY (id)"}
	}
	target.primaryName = fmt.Sprintf("fuzz_pk_%d", state.next())
	if isMySQLFamily(state.engine) {
		return []string{prefix + " ADD PRIMARY KEY (id)"}
	}
	return []string{prefix + addConstraint + target.primaryName + " PRIMARY KEY (id)"}
}

// operationForeignKey adds a foreign key from an integer column to another table's id, or
// drops one.
//
// Takes state (*history) which holds the schema.
// Takes chosen (choice) which picks the table, column, referenced table and change.
//
// Returns []string which holds the statement, or nil when no table can change.
func operationForeignKey(state *history, chosen choice) []string {
	candidates := state.tables(func(candidate *relation) bool { return !candidate.partitioned })
	if len(candidates) == 0 {
		return nil
	}
	target := candidates[chosen.table%len(candidates)]
	prefix := alterTable + qualified(target)
	if len(target.foreign) != 0 && chosen.flag(0) && state.allowed(featureDropForeignKey) {
		key := target.foreign[0]
		target.foreign = target.foreign[1:]
		state.cover(featureDropForeignKey)
		if isMySQLFamily(state.engine) {
			return []string{prefix + " DROP FOREIGN KEY `" + key.name + "`"}
		}
		return []string{prefix + dropConstraint + key.name}
	}
	if !state.allowed(featureAddForeignKey) {
		return nil
	}
	var members []*column
	for _, member := range target.columns {
		if member.name != identifier && member.family == FamilyInteger && !member.generated && !target.foreignColumn(member.name) {
			members = append(members, member)
		}
	}
	parents := state.tables(func(candidate *relation) bool { return state.referenceable(candidate, target) })
	if len(members) == 0 || len(parents) == 0 {
		return nil
	}
	member, parent := members[chosen.column%len(members)], parents[chosen.variant%len(parents)]
	unnamed := chosen.flag(1) && state.allowed(featureDefaultName)
	key := foreignKey{name: "", table: parent.key(), columns: []string{member.name}, references: []string{identifier}}
	clause := " ADD "
	if unnamed {
		key.name = state.defaultName(target, kindForeign, member.name, false)
		state.cover(featureDefaultName)
	} else {
		key.name = fmt.Sprintf("fuzz_fk_%d", state.next())
		clause = addConstraint + key.name + " "
	}
	target.foreign = append(target.foreign, key)
	target.addImplicitIndex(state, key, unnamed)
	state.cover(featureAddForeignKey)
	if parent.namespace != target.namespace {
		state.cover(featureCrossSchemaKey)
	}
	return []string{prefix + clause + "FOREIGN KEY (" + member.name + ") REFERENCES " + qualified(parent) + " (id)"}
}
