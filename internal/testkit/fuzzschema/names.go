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
	"strings"
)

const (
	// featureDefaultName covers a constraint left unnamed and dropped by the name the engine
	// gives it.
	featureDefaultName = "operation/default-constraint-name"

	// featureColumnConstraint covers a key, check or foreign key a column definition
	// declares.
	featureColumnConstraint = "operation/column-constraint"

	// flagUnnamed leaves a new table's constraints unnamed.
	flagUnnamed = 6

	// flagColumnLevel declares an unnamed table's constraints with their columns.
	flagColumnLevel = 7

	// mysqlForeignKeyInfix joins a table's name and a number in a MySQL foreign key's name.
	mysqlForeignKeyInfix = "_ibfk_"

	// mysqlCheckInfix joins a table's name and a number in a MySQL check's name.
	mysqlCheckInfix = "_chk_"

	// mariaDBCheckPrefix starts the name of a MariaDB check.
	mariaDBCheckPrefix = "CONSTRAINT_"

	// mysqlPrimary is the name of every MySQL primary key.
	mysqlPrimary = "PRIMARY"

	// separator joins the parts of a name.
	separator = "_"
)

// constraintKind names the kind of constraint a default name is for.
type constraintKind int

const (
	// kindPrimary is a primary key.
	kindPrimary constraintKind = iota

	// kindUnique is a unique key.
	kindUnique

	// kindForeign is a foreign key.
	kindForeign

	// kindCheck is a CHECK constraint on id.
	kindCheck
)

// defaultName returns the name the engine gives a constraint the statement leaves
// unnamed, as observed on each engine.
//
// Takes table (*relation) which holds the constraint, with the constraints before it.
// Takes kind (constraintKind) which is the constraint's kind.
// Takes column (string) which is its column, unused for a check, which reads id.
// Takes creating (bool) which is true in the table's CREATE TABLE.
//
// Returns string which is the name.
func (state *history) defaultName(table *relation, kind constraintKind, column string, creating bool) string {
	switch {
	case state.engine == engineCockroachDB:
		return cockroachName(table, kind, column)
	case isPostgresFamily(state.engine):
		return state.postgresName(table, kind, column)
	default:
		return state.mysqlName(table, kind, column, creating)
	}
}

// postgresName names a constraint as PostgreSQL does: the table, the column and a label,
// the label numbered until no constraint or index of the schema has the name.
//
// Takes table (*relation) which holds the constraint.
// Takes kind (constraintKind) which is the constraint's kind.
// Takes column (string) which is its column.
//
// Returns string which is the name.
func (state *history) postgresName(table *relation, kind constraintKind, column string) string {
	base := map[constraintKind]string{kindPrimary: table.name + "_pkey", kindUnique: table.name + separator + column + "_key",
		kindForeign: table.name + separator + column + "_fkey", kindCheck: table.name + separator + identifier + "_check"}[kind]
	taken := table.names()
	for _, candidate := range state.relations {
		if candidate.namespace == table.namespace && candidate != table {
			taken = append(taken, candidate.names()...)
		}
	}
	name := base
	for pass := 1; slices.Contains(taken, name); pass++ {
		name = base + strconv.Itoa(pass)
	}
	return name
}

// cockroachName names a constraint as CockroachDB does, numbered until no constraint of
// the table has the name.
//
// Takes table (*relation) which holds the constraint.
// Takes kind (constraintKind) which is the constraint's kind.
// Takes column (string) which is its column.
//
// Returns string which is the name.
func cockroachName(table *relation, kind constraintKind, column string) string {
	base, numberSeparator := table.name+separator+column+"_key", ""
	switch kind {
	case kindPrimary:
		base = table.name + "_pkey"
	case kindForeign:
		base, numberSeparator = table.name+separator+column+"_fkey", separator
	case kindCheck:
		base = "check_" + identifier
	default:
	}
	taken := table.names()
	name := base
	for number := 1; slices.Contains(taken, name); number++ {
		name = base + numberSeparator + strconv.Itoa(number)
	}
	return name
}

// mysqlName names a constraint as MySQL or MariaDB does: a key after its column, a
// foreign key and a MySQL check after the table and the next number, a MariaDB foreign
// key by the next number alone, and a MariaDB check CONSTRAINT_ and the first free
// number.
//
// Takes table (*relation) which holds the constraint.
// Takes kind (constraintKind) which is the constraint's kind.
// Takes column (string) which is its column.
// Takes creating (bool) which numbers from one in CREATE TABLE.
//
// Returns string which is the name.
func (state *history) mysqlName(table *relation, kind constraintKind, column string, creating bool) string {
	switch {
	case kind == kindPrimary:
		return mysqlPrimary
	case kind == kindUnique:
		return table.keyName(column)
	case kind == kindCheck && state.engine == engineMariaDB:
		name := mariaDBCheckPrefix + "1"
		for number := 2; slices.ContainsFunc(table.checks, func(check string) bool { return strings.EqualFold(check, name) }); number++ {
			name = mariaDBCheckPrefix + strconv.Itoa(number)
		}
		return name
	default:
		return state.numberedName(table, kind, creating)
	}
}

// numberedName names a foreign key or a MySQL check by the next number, after the table
// and an infix, or for a MariaDB foreign key by the number alone. CREATE TABLE numbers
// from one, and ALTER TABLE from one past the highest number such a name of the table
// holds.
//
// Takes table (*relation) which holds the constraint.
// Takes kind (constraintKind) which is kindForeign or kindCheck.
// Takes creating (bool) which numbers from one in CREATE TABLE.
//
// Returns string which is the name.
func (state *history) numberedName(table *relation, kind constraintKind, creating bool) string {
	infix := mysqlForeignKeyInfix
	names := make([]string, 0, len(table.foreign))
	for _, key := range table.foreign {
		names = append(names, key.name)
	}
	if kind == kindCheck {
		infix, names = mysqlCheckInfix, table.checks
	}
	prefix := table.name + infix
	if kind == kindForeign && state.engine == engineMariaDB {
		prefix = ""
	}
	highest := 0
	if !creating {
		for _, name := range names {
			highest = max(highest, generatedNumber(name, prefix))
		}
	}
	return prefix + strconv.Itoa(highest+1)
}

// keyName names a MySQL key after its column, numbered from _2 when the table has a key
// or index of that name.
//
// Takes column (string) which is the key's first column.
//
// Returns string which is the name.
func (table *relation) keyName(column string) string {
	taken := make([]string, 0, 1+len(table.uniques)+len(table.indexes))
	taken = append(taken, mysqlPrimary)
	for _, key := range table.uniques {
		taken = append(taken, key.name)
	}
	for _, index := range table.indexes {
		taken = append(taken, index.name)
	}
	holds := func(name string) bool {
		return slices.ContainsFunc(taken, func(existing string) bool { return strings.EqualFold(existing, name) })
	}
	name := column
	for number := 2; holds(name); number++ {
		name = column + separator + strconv.Itoa(number)
	}
	return name
}

// generatedNumber reads the number of a name MySQL generates from a prefix.
//
// Takes name (string) which is the name.
// Takes prefix (string) which is the table's name and the infix.
//
// Returns int which is the number, or zero for another name.
func generatedNumber(name, prefix string) int {
	if len(name) <= len(prefix) || !strings.EqualFold(name[:len(prefix)], prefix) {
		return 0
	}
	number, err := strconv.Atoi(name[len(prefix):])
	if err != nil {
		return 0
	}
	return number
}

// names lists the names of a table's constraints and indexes.
//
// Returns []string which holds them, with the primary key's of each partition.
func (table *relation) names() []string {
	var names []string
	if table.primaryName != "" {
		names = append(names, table.primaryName)
	}
	for _, key := range table.uniques {
		names = append(names, key.name)
	}
	names = append(names, table.checks...)
	for _, key := range table.foreign {
		names = append(names, key.name)
	}
	for _, index := range table.indexes {
		names = append(names, index.name)
	}
	if len(table.primaryKey) != 0 {
		for _, index := range table.partitions {
			names = append(names, partitionName(index)+"_pkey")
		}
	}
	return names
}

// addImplicitIndex adds the index MySQL and MariaDB create for a foreign key on a column
// no key leads with: named after the foreign key, or after the column when the foreign
// key's name is one MySQL generates. It replaces the index made for an earlier foreign
// key on the column.
//
// Takes state (*history) which names the engine.
// Takes key (foreignKey) which is the new foreign key.
// Takes generated (bool) which is true when the engine named the foreign key.
func (table *relation) addImplicitIndex(state *history, key foreignKey, generated bool) {
	if !isMySQLFamily(state.engine) {
		return
	}
	column := key.columns[0]
	served := func(columns []string) bool { return len(columns) != 0 && columns[0] == column }
	if served(table.primaryKey) || slices.ContainsFunc(table.uniques, func(unique namedKey) bool { return served(unique.columns) }) {
		return
	}
	position := slices.IndexFunc(table.indexes, func(index namedKey) bool { return served(index.columns) })
	if position >= 0 {
		table.indexes = slices.Delete(table.indexes, position, position+1)
	} else {
		position = len(table.indexes)
	}
	name := key.name
	if generated || slices.ContainsFunc(table.indexes, func(index namedKey) bool { return strings.EqualFold(index.name, name) }) {
		name = table.keyName(column)
	}
	table.indexes = slices.Insert(table.indexes, position, namedKey{name: name, columns: []string{column}, inline: false})
}

// dropImplicitIndexes drops the indexes MySQL created for foreign keys on a column, which
// a new key on the column replaces, or which go with the dropped column.
//
// Takes column (string) which is the column.
func (table *relation) dropImplicitIndexes(column string) {
	table.indexes = slices.DeleteFunc(table.indexes, func(index namedKey) bool { return slices.Contains(index.columns, column) })
}

// renameGeneratedNames renames the foreign keys and checks whose names MySQL generated
// from a table's name, as renaming the table renames them. MariaDB names neither after
// the table.
//
// Takes state (*history) which names the engine.
// Takes previous (string) which is the table's old name.
func (table *relation) renameGeneratedNames(state *history, previous string) {
	if !isMySQLFamily(state.engine) {
		return
	}
	rename := func(name, infix string) string {
		if generatedNumber(name, previous+infix) == 0 {
			return name
		}
		return table.name + name[len(previous):]
	}
	if state.engine == engineMariaDB {
		return
	}
	for index := range table.foreign {
		table.foreign[index].name = rename(table.foreign[index].name, mysqlForeignKeyInfix)
	}
	if state.engine == engineMySQL {
		for index := range table.checks {
			table.checks[index] = rename(table.checks[index], mysqlCheckInfix)
		}
	}
}
