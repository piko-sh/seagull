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
)

const (
	// featureAddColumn covers ADD COLUMN.
	featureAddColumn = "operation/add-column"

	// featurePosition covers ADD COLUMN with FIRST or AFTER.
	featurePosition = "operation/add-column-position"

	// featureDropColumn covers DROP COLUMN.
	featureDropColumn = "operation/drop-column"

	// featureRenameColumn covers RENAME COLUMN.
	featureRenameColumn = "operation/rename-column"

	// featureRenameKeyed covers renaming a column a key uses.
	featureRenameKeyed = "operation/rename-keyed-column"

	// featureModifyDefaulted covers ClickHouse MODIFY COLUMN with a new type on a column
	// with a default, which keeps the default.
	featureModifyDefaulted = "operation/modify-defaulted-column"

	// featureRedefine covers MODIFY and CHANGE COLUMN, which restate the whole column.
	featureRedefine = "operation/redefine-column"

	// featureAlterType covers changing a column's type.
	featureAlterType = "operation/alter-type"

	// featureSetDefault covers setting a default.
	featureSetDefault = "operation/set-default"

	// featureDropDefault covers dropping a default.
	featureDropDefault = "operation/drop-default"

	// featureSetNotNull covers making a column NOT NULL.
	featureSetNotNull = "operation/set-not-null"

	// featureDropNotNull covers making a column nullable.
	featureDropNotNull = "operation/drop-not-null"

	// featureAddGenerated covers adding a generated column.
	featureAddGenerated = "operation/add-generated-column"

	// maxColumns bounds a table's columns.
	maxColumns = 7

	// addColumn starts every ADD COLUMN clause.
	addColumn = " ADD COLUMN "

	// alterColumn starts every ALTER COLUMN clause.
	alterColumn = " ALTER COLUMN "

	// space separates a column's name from its type.
	space = " "

	// modifyColumn starts every MODIFY COLUMN clause.
	modifyColumn = " MODIFY COLUMN "
)

const (
	// flagNullable makes a new column nullable.
	flagNullable = iota

	// flagDefault gives a new column a default.
	flagDefault

	// flagAfter adds a column after id.
	flagAfter

	// flagFirst adds a column first.
	flagFirst
)

// columnPool names ordinary columns.
var columnPool = []string{"c1", "c2", "c3", "c4", "c5"}

// renamedPool names renamed columns.
var renamedPool = []string{"r1", "r2", "r3"}

// generatedPool names generated columns.
var generatedPool = []string{"g1", "g2"}

// columnRef is a column of a table.
type columnRef struct {
	// table is the table.
	table *relation

	// column is the column.
	column *column
}

// columns lists the non-id columns of physical tables a predicate admits.
//
// Takes admit (func(*relation, *column) bool) which filters them.
//
// Returns []columnRef which keeps table and column order.
func (state *history) columns(admit func(*relation, *column) bool) []columnRef {
	var result []columnRef
	for _, table := range state.tables(func(*relation) bool { return true }) {
		for _, member := range table.columns {
			if member.name != identifier && admit(table, member) {
				result = append(result, columnRef{table: table, column: member})
			}
		}
	}
	return result
}

// usableFamily replaces the enum family with text where enum types are steered out.
//
// Takes family (string) which is the chosen family.
//
// Returns string which is a family the history may use.
func (state *history) usableFamily(family string) string {
	if family == FamilyEnum && supports(state.engine, featureEnumType) && !state.allowed(featureEnumType) {
		return FamilyText
	}
	return family
}

// newColumn models a new ordinary column, creating its enum type where needed.
//
// Takes table (*relation) which receives the column later.
// Takes family (string) which is the column's family.
// Takes nullable (bool) which admits NULL.
// Takes withDefault (bool) which gives it a default where the family takes one.
// Takes start (int) which rotates the name pool.
//
// Returns *column which is the new column.
// Returns []string which holds statements that must precede it.
func (state *history) newColumn(table *relation, family string, nullable, withDefault bool, start int) (*column, []string) {
	family = state.usableFamily(family)
	added := &column{name: table.freeColumn(columnPool, start), family: family, enum: "", nullable: nullable,
		hasDefault: withDefault && defaultable(family), generated: false}
	if state.engine == engineClickHouse && family == FamilyArray {
		added.nullable = false
	}
	var prelude []string
	if family == FamilyEnum && supports(state.engine, featureEnumType) {
		if len(state.enums) == 0 {
			prelude = state.createEnum(0)
		}
		added.enum = state.enums[start%len(state.enums)].name
	}
	state.cover(familyFeature(family))
	return added, prelude
}

// renameableColumn reports whether an engine permits renaming a column.
//
// PostgreSQL views keep their column names after a source column is renamed. Other
// engines' views break or refuse. DuckDB and ClickHouse reject renames of columns used by
// a key or index.
//
// Takes table (*relation) which is the table.
// Takes member (*column) which is the column.
//
// Returns bool which is true when a rename applies.
func (state *history) renameableColumn(table *relation, member *column) bool {
	viewsFollow := state.engine == enginePostgres || state.engine == engineTimescale
	if (state.viewed(table, member.name) && !viewsFollow) || state.locked(table) {
		return false
	}
	if !table.keyed(member.name) {
		return true
	}
	return state.engine != engineDuckDB && state.engine != engineClickHouse && state.allowed(featureRenameKeyed)
}

// foreignColumn reports whether a foreign key of the table uses a column.
//
// Takes name (string) which is the column's name.
//
// Returns bool which is true when a foreign key includes it.
func (table *relation) foreignColumn(name string) bool {
	for _, key := range table.foreign {
		if slices.Contains(key.columns, name) {
			return true
		}
	}
	return false
}

// modify spells the type, nullability and default MODIFY or CHANGE COLUMN restates, and
// drops the model's default when the statement leaves it out.
//
// Takes target (*column) which holds the column's new type and nullability.
// Takes keepDefault (bool) which restates the default.
//
// Returns string which follows the column's name.
func (state *history) modify(target *column, keepDefault bool) string {
	text := nativeType(state.engine, target)
	if !keepDefault {
		target.hasDefault = false
	}
	if target.hasDefault {
		text += " DEFAULT " + defaultLiteral(state.engine, target.family)
	}
	if target.nullable {
		text += " NULL"
	} else {
		text += notNull
	}
	state.cover(featureRedefine)
	return text
}

// modifiable reports whether a ClickHouse MODIFY COLUMN with a type may change a column,
// and covers the default it keeps.
//
// Takes member (*column) which is the column.
//
// Returns bool which is false for a defaulted ClickHouse column while its finding is
// open.
func (state *history) modifiable(member *column) bool {
	if state.engine != engineClickHouse || !member.hasDefault {
		return true
	}
	return state.allowed(featureModifyDefaulted)
}

// operationAddColumn adds an ordinary column.
//
// Takes state (*history) which holds the schema.
// Takes chosen (choice) which picks the table, family, nullability, default and place.
//
// Returns []string which holds the statements, or nil when no table has room.
func operationAddColumn(state *history, chosen choice) []string {
	candidates := state.tables(func(candidate *relation) bool { return len(candidate.columns) < maxColumns })
	if len(candidates) == 0 {
		return nil
	}
	target := candidates[chosen.table%len(candidates)]
	if target.freeColumn(columnPool, chosen.column) == "" {
		return nil
	}
	usable := families(state.engine)
	added, statements := state.newColumn(target, usable[chosen.variant%len(usable)], chosen.flag(flagNullable), chosen.flag(flagDefault), chosen.column)
	if state.engine == engineSQLite && !added.nullable && !added.hasDefault {

		added.hasDefault = defaultable(added.family)
		added.nullable = !added.hasDefault
	}
	if state.locked(target) {
		added.nullable = true
	}
	declared := *added
	if state.engine == engineDuckDB {

		declared.nullable = true
	}
	clause := alterTable + qualified(target) + addColumn + definition(state.engine, &declared, false)
	position := len(target.columns)
	if !target.partitioned && state.allowed(featurePosition) && chosen.flag(flagAfter) {
		clause, position = clause+" AFTER id", 1
		state.cover(featurePosition)
	} else if !target.partitioned && state.allowed(featurePosition) && chosen.flag(flagFirst) {
		clause, position = clause+" FIRST", 0
		state.cover(featurePosition)
	}
	statements = append(statements, clause)
	if declared.nullable != added.nullable {
		statements = append(statements, alterTable+qualified(target)+alterColumn+added.name+" SET NOT NULL")
	}
	target.columns = slices.Insert(target.columns, position, added)
	state.cover(featureAddColumn)
	return statements
}

// operationDropColumn drops a column no key, index or view uses.
//
// Takes state (*history) which holds the schema.
// Takes chosen (choice) which picks the column.
//
// Returns []string which holds the statement, or nil when no column can be dropped.
func operationDropColumn(state *history, chosen choice) []string {
	candidates := state.columns(func(table *relation, member *column) bool {
		if state.locked(table) || (state.engine == engineDuckDB && table.indexedAfter(member.name)) {
			return false
		}
		return len(table.columns) > 2 && !table.keyed(member.name) && !state.viewed(table, member.name)
	})
	if len(candidates) == 0 {
		return nil
	}
	target := candidates[chosen.column%len(candidates)]
	target.table.columns = slices.DeleteFunc(target.table.columns, func(member *column) bool { return member == target.column })
	target.table.dropImplicitIndexes(target.column.name)
	state.cover(featureDropColumn)
	return []string{alterTable + qualified(target.table) + " DROP COLUMN " + target.column.name}
}

// operationRenameColumn renames a column, or redefines it under a new name with CHANGE
// COLUMN on MySQL and MariaDB.
//
// Takes state (*history) which holds the schema.
// Takes chosen (choice) which picks the column, its name and the spelling.
//
// Returns []string which holds the statement, or nil when no column can be renamed.
func operationRenameColumn(state *history, chosen choice) []string {
	candidates := state.columns(state.renameableColumn)
	if len(candidates) == 0 {
		return nil
	}
	target := candidates[chosen.column%len(candidates)]
	name := target.table.freeColumn(renamedPool, chosen.variant)
	if name == "" {
		return nil
	}
	previous := target.column.name
	statement := alterTable + qualified(target.table) + " RENAME COLUMN " + previous + " TO " + name
	if chosen.flag(0) && state.allowed(featureRedefine) && !target.column.generated && !target.table.foreignColumn(previous) {
		if !chosen.flag(1) {
			target.column.nullable = true
		}
		statement = alterTable + qualified(target.table) + " CHANGE COLUMN " + previous + " " + name + space + state.modify(target.column, chosen.flag(1))
	}
	if target.table.keyed(previous) {
		state.cover(featureRenameKeyed)
	}
	target.table.renameColumn(state, previous, name)
	state.cover(featureRenameColumn)
	return []string{statement}
}

// operationAlterType changes a scalar column's type to text, or an integer's to decimal.
//
// Takes state (*history) which holds the schema.
// Takes chosen (choice) which picks the column, the new type and what MODIFY restates.
//
// Returns []string which holds the statement, or nil when no column can change.
func operationAlterType(state *history, chosen choice) []string {
	if !state.allowed(featureAlterType) {
		return nil
	}
	convertible := []string{FamilyInteger, FamilyFloat, FamilyDecimal, FamilyBoolean, FamilyDate, FamilyTimestamp, FamilyTimestampTZ, FamilyUUID}
	candidates := state.columns(func(table *relation, member *column) bool {
		return !member.generated && slices.Contains(convertible, member.family) && !table.keyed(member.name) && !state.viewed(table, member.name) &&
			!state.locked(table) && state.modifiable(member)
	})
	if len(candidates) == 0 {
		return nil
	}
	target := candidates[chosen.column%len(candidates)]
	if target.column.family == FamilyInteger && chosen.flag(1) {
		target.column.family = FamilyDecimal
	} else {
		target.column.family = FamilyText
	}
	state.cover(featureAlterType, familyFeature(target.column.family))
	prefix := alterTable + qualified(target.table)
	native := nativeType(state.engine, target.column)
	switch {
	case isMySQLFamily(state.engine):
		if !chosen.flag(0) {
			target.column.nullable = true
		}
		return []string{prefix + modifyColumn + target.column.name + space + state.modify(target.column, chosen.flag(0))}
	case state.engine == engineClickHouse:
		if target.column.hasDefault {
			state.cover(featureModifyDefaulted)
		}
		return []string{prefix + modifyColumn + target.column.name + space + columnType(state.engine, target.column)}
	case state.engine == engineDuckDB:
		return []string{prefix + alterColumn + target.column.name + " TYPE " + native}
	default:
		return []string{prefix + alterColumn + target.column.name + " TYPE " + native + " USING " + target.column.name + "::" + native}
	}
}

// operationDefault sets or drops a column's default.
//
// Takes state (*history) which holds the schema.
// Takes chosen (choice) which picks the column and whether to replace a default.
//
// Returns []string which holds the statement, or nil when no column takes a default.
func operationDefault(state *history, chosen choice) []string {
	candidates := state.columns(func(_ *relation, member *column) bool {
		if member.generated || !defaultable(member.family) {
			return false
		}
		return state.allowed(map[bool]string{true: featureDropDefault, false: featureSetDefault}[member.hasDefault && !chosen.flag(0)])
	})
	if len(candidates) == 0 {
		return nil
	}
	target := candidates[chosen.column%len(candidates)]
	prefix := alterTable + qualified(target.table)
	if target.column.hasDefault && !chosen.flag(0) {
		target.column.hasDefault = false
		state.cover(featureDropDefault)
		if state.engine == engineClickHouse {
			return []string{prefix + modifyColumn + target.column.name + " REMOVE DEFAULT"}
		}
		return []string{prefix + alterColumn + target.column.name + " DROP DEFAULT"}
	}
	target.column.hasDefault = true
	state.cover(featureSetDefault)
	literal := defaultLiteral(state.engine, target.column.family)
	if state.engine == engineClickHouse {
		return []string{prefix + modifyColumn + target.column.name + " DEFAULT " + literal}
	}
	return []string{prefix + alterColumn + target.column.name + " SET DEFAULT " + literal}
}

// operationNullability makes a nullable column NOT NULL, or a NOT NULL column nullable.
//
// Takes state (*history) which holds the schema.
// Takes chosen (choice) which picks the column and what MODIFY restates.
//
// Returns []string which holds the statement, or nil when no column can change.
func operationNullability(state *history, chosen choice) []string {
	candidates := state.columns(func(table *relation, member *column) bool {
		if member.generated || slices.Contains(table.primaryKey, member.name) || (state.engine == engineClickHouse && member.family == FamilyArray) ||
			state.locked(table) || !state.modifiable(member) {
			return false
		}
		if isMySQLFamily(state.engine) && table.foreignColumn(member.name) {
			return false
		}
		return state.allowed(map[bool]string{true: featureSetNotNull, false: featureDropNotNull}[member.nullable])
	})
	if len(candidates) == 0 {
		return nil
	}
	target := candidates[chosen.column%len(candidates)]
	target.column.nullable = !target.column.nullable
	clause := map[bool]string{true: " DROP NOT NULL", false: " SET NOT NULL"}[target.column.nullable]
	state.cover(map[bool]string{true: featureDropNotNull, false: featureSetNotNull}[target.column.nullable])
	prefix := alterTable + qualified(target.table)
	switch {
	case isMySQLFamily(state.engine):
		return []string{prefix + modifyColumn + target.column.name + space + state.modify(target.column, chosen.flag(0))}
	case state.engine == engineClickHouse:
		if target.column.hasDefault {
			state.cover(featureModifyDefaulted)
		}
		return []string{prefix + modifyColumn + target.column.name + space + columnType(state.engine, target.column)}
	default:
		return []string{prefix + alterColumn + target.column.name + clause}
	}
}

// operationGenerated adds a generated column computed from id.
//
// Takes state (*history) which holds the schema.
// Takes chosen (choice) which picks the table and the storage.
//
// Returns []string which holds the statement, or nil when no table has room.
func operationGenerated(state *history, chosen choice) []string {
	if !state.allowed(featureAddGenerated) {
		return nil
	}
	candidates := state.tables(func(candidate *relation) bool {
		return len(candidate.columns) < maxColumns && candidate.freeColumn(generatedPool, 0) != ""
	})
	if len(candidates) == 0 {
		return nil
	}
	target := candidates[chosen.table%len(candidates)]
	added := generatedColumn(state.engine, target.freeColumn(generatedPool, chosen.column))

	stored := chosen.flag(0) && state.engine != engineSQLite
	target.columns = append(target.columns, added)
	state.cover(featureAddGenerated)
	return []string{alterTable + qualified(target) + addColumn + definition(state.engine, added, stored)}
}
