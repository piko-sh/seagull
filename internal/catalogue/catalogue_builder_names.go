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
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// postgresNameBytes is the most bytes PostgreSQL keeps of a name.
	postgresNameBytes = 63

	// postgresAdditionBytes bounds the joined column names PostgreSQL builds a name from.
	postgresAdditionBytes = 2*(postgresNameBytes+1) - 1

	// labelPrimaryKey ends the name of a primary key.
	labelPrimaryKey = "pkey"

	// labelUnique ends the name of a unique constraint, and of a CockroachDB unique index.
	labelUnique = "key"

	// labelIndex ends the name of an index.
	labelIndex = "idx"

	// labelForeignKey ends the name of a foreign key.
	labelForeignKey = "fkey"

	// labelCheck ends the name of a PostgreSQL check and starts that of a CockroachDB one.
	labelCheck = "check"

	// labelNotNull ends the name of a PostgreSQL NOT NULL constraint.
	labelNotNull = "not_null"

	// mysqlPrimary is the name MySQL gives every primary key.
	mysqlPrimary = "PRIMARY"

	// mysqlForeignKeyInfix joins a table's name and a number in a MySQL foreign key's name.
	mysqlForeignKeyInfix = "_ibfk_"

	// mysqlCheckInfix joins a table's name and a number in a MySQL check's name.
	mysqlCheckInfix = "_chk_"

	// mariaDBCheckPrefix starts the name of a MariaDB check declared on its table.
	mariaDBCheckPrefix = "CONSTRAINT_"

	// mysqlKeySuffixes bounds the numbered names MySQL tries for a key.
	mysqlKeySuffixes = 100

	// nameSeparator joins the parts of a name.
	nameSeparator = "_"
)

// namesConstraints reports whether the dialect gives unnamed constraints names.
//
// Returns bool which is false for an engine without default names.
func (b *catalogueBuilder) namesConstraints() bool {
	return b.constraintNames != dialect.ConstraintNamesNone
}

// postgresFamily reports whether the dialect names constraints as PostgreSQL or
// CockroachDB does.
//
// Returns bool which is true for either.
func (b *catalogueBuilder) postgresFamily() bool {
	return b.constraintNames == dialect.ConstraintNamesPostgres || b.constraintNames == dialect.ConstraintNamesCockroachDB
}

// mysqlFamily reports whether the dialect names constraints as MySQL or MariaDB does.
//
// Returns bool which is true for either.
func (b *catalogueBuilder) mysqlFamily() bool {
	return b.constraintNames == dialect.ConstraintNamesMySQL || b.constraintNames == dialect.ConstraintNamesMariaDB
}

// generatedNumbers holds the last numbers a statement gave MySQL foreign key and check
// names, each numbered on its own.
type generatedNumbers struct {
	// foreignKey is the last foreign key number.
	foreignKey int

	// check is the last check number.
	check int
}

// nameConstraints names the constraints from the given position on that the migration
// left unnamed, as the engine names them, and keeps of a check's columns only the table's
// columns it reads.
//
// Takes table (*sqlmodel.Table) which holds the constraints.
// Takes from (int) which is the position of the first constraint the statement adds.
// Takes creating (bool) which is true when the statement creates the table.
func (b *catalogueBuilder) nameConstraints(table *sqlmodel.Table, from int, creating bool) {
	if !b.namesConstraints() {
		return
	}
	var numbers generatedNumbers
	if !creating {
		numbers = b.highestGeneratedNumbers(table)
	}
	unnamed := make([]bool, len(table.Constraints))
	for index := from; index < len(table.Constraints); index++ {
		unnamed[index] = b.nameConstraint(table, &table.Constraints[index], &numbers)
	}
	if b.mysqlFamily() {
		b.addImplicitIndexes(table, from, unnamed)
	}
}

// nameConstraint names one constraint a statement adds when the migration left it
// unnamed, and keeps of a check's columns only the table's columns it reads.
//
// Takes table (*sqlmodel.Table) which holds the constraint.
// Takes constraint (*sqlmodel.Constraint) which is the constraint.
// Takes numbers (*generatedNumbers) which number MySQL's foreign keys and checks.
//
// Returns bool which is true when the constraint was unnamed.
func (b *catalogueBuilder) nameConstraint(table *sqlmodel.Table, constraint *sqlmodel.Constraint, numbers *generatedNumbers) bool {
	var references []string
	if constraint.Kind == sqlmodel.ConstraintCheck && (b.postgresFamily() || b.constraintNames == dialect.ConstraintNamesMySQL) {
		references = b.tableColumns(table, constraint.Columns)
		constraint.Columns = distinctNames(references)
	}
	if constraint.Kind == sqlmodel.ConstraintUnique && b.mysqlFamily() {
		b.dropImplicitIndexes(table, constraint.Columns)
	}
	if constraint.Name != "" {
		return false
	}
	switch {
	case constraint.Kind == sqlmodel.ConstraintPrimaryKey && b.constraintNames == dialect.ConstraintNamesPostgres:
		constraint.Name = b.postgresRelationName(table, "", labelPrimaryKey, true)
	case constraint.Kind == sqlmodel.ConstraintPrimaryKey && b.constraintNames == dialect.ConstraintNamesCockroachDB:
		constraint.Name = b.cockroachName(table, table.Name+nameSeparator+labelPrimaryKey, "")
	case constraint.Kind == sqlmodel.ConstraintPrimaryKey:
	case b.constraintNames == dialect.ConstraintNamesPostgres:
		constraint.Name = b.postgresConstraintName(table, constraint, references)
	case b.constraintNames == dialect.ConstraintNamesCockroachDB:
		constraint.Name = b.cockroachConstraintName(table, constraint, references)
	default:
		constraint.Name = b.mysqlConstraintName(table, constraint, numbers)
	}
	return true
}

// withPrimaryKeyConstraint adds a constraint recording a primary key ahead of the
// constraints a statement declares, when none of them records it, so that the key gets
// the engine's name for it. MySQL names every key PRIMARY, which a drop reads without
// one.
//
// Takes constraints ([]sqlmodel.Constraint) which are the declared constraints.
// Takes primaryKey ([]string) which is the key's columns.
// Takes previous (string) which names the key the table had before, which CockroachDB
// keeps, or is empty.
//
// Returns []sqlmodel.Constraint which leads with the key's constraint when one is added.
func (b *catalogueBuilder) withPrimaryKeyConstraint(constraints []sqlmodel.Constraint, primaryKey []string, previous string) []sqlmodel.Constraint {
	if !b.postgresFamily() || len(primaryKey) == 0 || slices.ContainsFunc(constraints, isPrimaryKeyConstraint) {
		return constraints
	}
	if b.constraintNames != dialect.ConstraintNamesCockroachDB {
		previous = ""
	}
	key := sqlmodel.Constraint{Name: previous, ForeignTable: "", Columns: slices.Clone(primaryKey), ForeignColumns: nil,
		Origin: sqlmodel.MigrationOrigin{}, Kind: sqlmodel.ConstraintPrimaryKey}
	return append([]sqlmodel.Constraint{key}, constraints...)
}

// declaredKeys returns the constraints created with a table.
//
// A primary key constraint leads the result. On PostgreSQL, a unique constraint that
// repeats the primary key or an earlier unique key is omitted because the keys share one
// index. The index keeps either name when the first key was unnamed.
//
// Takes primaryKey ([]string) which is the table's primary key.
// Takes declared ([]sqlmodel.Constraint) which are the declared constraints.
//
// Returns []sqlmodel.Constraint which is a copy holding the constraints the table gets.
func (b *catalogueBuilder) declaredKeys(primaryKey []string, declared []sqlmodel.Constraint) []sqlmodel.Constraint {
	constraints := b.withPrimaryKeyConstraint(slices.Clone(declared), primaryKey, "")
	if b.constraintNames != dialect.ConstraintNamesPostgres {
		return constraints
	}
	var keys []int
	if primary := slices.IndexFunc(constraints, isPrimaryKeyConstraint); primary >= 0 {
		keys = append(keys, primary)
	}
	merged := make([]bool, len(constraints))
	for index := range constraints {
		if constraints[index].Kind != sqlmodel.ConstraintUnique {
			continue
		}
		prior := slices.IndexFunc(keys, func(key int) bool {
			return slices.EqualFunc(constraints[key].Columns, constraints[index].Columns, b.sameName)
		})
		if prior < 0 {
			keys = append(keys, index)
			continue
		}
		if constraints[keys[prior]].Name == "" {
			constraints[keys[prior]].Name = constraints[index].Name
		}
		merged[index] = true
	}
	kept := constraints[:0]
	for index := range constraints {
		if !merged[index] {
			kept = append(kept, constraints[index])
		}
	}
	return kept
}

// forgetCopiedKeyNames clears the names of the primary and unique keys a CREATE TABLE
// copies from another table, as PostgreSQL names the copied indexes for the new table.
//
// Takes constraints ([]sqlmodel.Constraint) which are the copied constraints.
func (b *catalogueBuilder) forgetCopiedKeyNames(constraints []sqlmodel.Constraint) {
	if b.constraintNames != dialect.ConstraintNamesPostgres {
		return
	}
	for index := range constraints {
		if kind := constraints[index].Kind; kind == sqlmodel.ConstraintPrimaryKey || kind == sqlmodel.ConstraintUnique {
			constraints[index].Name = ""
		}
	}
}

// isPrimaryKeyConstraint reports whether a constraint records a primary key.
//
// Takes constraint (sqlmodel.Constraint) which is the constraint.
//
// Returns bool which is true for a primary key.
func isPrimaryKeyConstraint(constraint sqlmodel.Constraint) bool {
	return constraint.Kind == sqlmodel.ConstraintPrimaryKey
}

// primaryKeyName returns the name of the constraint recording a table's primary key.
//
// Takes table (*sqlmodel.Table) which is the table.
//
// Returns string which is empty when no constraint records it.
func primaryKeyName(table *sqlmodel.Table) string {
	for _, constraint := range table.Constraints {
		if constraint.Kind == sqlmodel.ConstraintPrimaryKey {
			return constraint.Name
		}
	}
	return ""
}

// nameIndex returns the name the engine gives an index the migration leaves unnamed.
//
// Takes table (*sqlmodel.Table) which the index is on.
// Takes index (*sqlmodel.Index) which is the index.
// Takes elements ([]string) which name the index's elements, or are nil to use its
// columns.
//
// Returns string which is the name, or empty when the engine gives none.
func (b *catalogueBuilder) nameIndex(table *sqlmodel.Table, index *sqlmodel.Index, elements []string) string {
	if len(elements) == 0 {
		elements = index.Columns
	}
	elements = indexColumnNames(elements)
	switch b.constraintNames {
	case dialect.ConstraintNamesPostgres:
		return b.postgresRelationName(table, postgresAddition(elements), labelIndex, false)
	case dialect.ConstraintNamesCockroachDB:
		label := labelIndex
		if index.IsUnique {
			label = labelUnique
		}
		return b.cockroachName(table, table.Name+nameSeparator+strings.Join(elements, nameSeparator)+nameSeparator+label, "")
	case dialect.ConstraintNamesMySQL, dialect.ConstraintNamesMariaDB:
		return b.mysqlKeyName(table, index.Columns)
	default:
		return ""
	}
}

// postgresConstraintName names a unique, check or foreign key constraint as PostgreSQL
// does.
//
// Takes table (*sqlmodel.Table) which holds the constraint.
// Takes constraint (*sqlmodel.Constraint) which is the constraint.
// Takes references ([]string) which are the columns a check reads.
//
// Returns string which is the name.
func (b *catalogueBuilder) postgresConstraintName(table *sqlmodel.Table, constraint *sqlmodel.Constraint, references []string) string {
	switch constraint.Kind {
	case sqlmodel.ConstraintUnique:
		return b.postgresRelationName(table, postgresAddition(constraint.Columns), labelUnique, true)
	case sqlmodel.ConstraintForeignKey:
		return b.postgresConstraintOnlyName(table, postgresAddition(constraint.Columns), labelForeignKey)
	case sqlmodel.ConstraintNotNull:
		return b.postgresNotNullName(table, constraint.Columns[0])
	default:
		column := ""
		if distinct := distinctNames(references); len(distinct) == 1 {
			column = distinct[0]
		}
		return b.postgresConstraintOnlyName(table, column, labelCheck)
	}
}

// postgresNotNullName names the NOT NULL constraint PostgreSQL 18 gives a column that
// declares no name for it: the table, the column and not_null, numbered past the names
// the schema's constraints hold.
//
// Takes table (*sqlmodel.Table) which holds the column.
// Takes column (string) which is the column's name.
//
// Returns string which is the name.
func (b *catalogueBuilder) postgresNotNullName(table *sqlmodel.Table, column string) string {
	return b.postgresConstraintOnlyName(table, column, labelNotNull)
}

// postgresRelationName chooses the name of an index, or of the key an index backs, as
// PostgreSQL's ChooseRelationName does: free among the schema's relations and, for a key,
// its constraints, numbering the label until it is.
//
// Takes table (*sqlmodel.Table) which the index is on.
// Takes addition (string) which is the joined column names, or empty.
// Takes label (string) which ends the name.
// Takes constraint (bool) which is true when the name is also a constraint's.
//
// Returns string which is the name.
func (b *catalogueBuilder) postgresRelationName(table *sqlmodel.Table, addition, label string, constraint bool) string {
	schema := b.catalogue.Schemas[b.key(table.Schema)]
	for pass := 0; ; pass++ {
		name := postgresObjectName(table.Name, addition, numberedLabel(label, pass))
		if !b.relationNamed(schema, name) && (!constraint || !b.constraintNamed(schema, name)) {
			return name
		}
	}
}

// postgresConstraintOnlyName chooses the name of a check or foreign key as PostgreSQL's
// ChooseConstraintName does: free among the schema's constraints.
//
// Takes table (*sqlmodel.Table) which holds the constraint.
// Takes addition (string) which is the column part, or empty.
// Takes label (string) which ends the name.
//
// Returns string which is the name.
func (b *catalogueBuilder) postgresConstraintOnlyName(table *sqlmodel.Table, addition, label string) string {
	schema := b.catalogue.Schemas[b.key(table.Schema)]
	for pass := 0; ; pass++ {
		name := postgresObjectName(table.Name, addition, numberedLabel(label, pass))
		if !b.constraintNamed(schema, name) {
			return name
		}
	}
}

// relationNamed reports whether a schema holds a relation of the name: a table, view,
// sequence or index, counting the index behind each primary key and unique constraint.
//
// Takes schema (*sqlmodel.Schema) which is the schema, or nil.
// Takes name (string) which is the candidate.
//
// Returns bool which is true when the name is taken.
func (b *catalogueBuilder) relationNamed(schema *sqlmodel.Schema, name string) bool {
	if schema == nil {
		return false
	}
	key := b.key(name)
	if schema.Tables[key] != nil || schema.Views[key] != nil || schema.Sequences[key] != nil {
		return true
	}
	for _, table := range schema.Tables {
		if slices.ContainsFunc(table.Indexes, func(index sqlmodel.Index) bool { return b.sameName(index.Name, name) }) ||
			slices.ContainsFunc(table.Constraints, func(constraint sqlmodel.Constraint) bool {
				return (constraint.Kind == sqlmodel.ConstraintPrimaryKey || constraint.Kind == sqlmodel.ConstraintUnique) && b.sameName(constraint.Name, name)
			}) {
			return true
		}
	}
	return false
}

// constraintNamed reports whether any table of a schema has a constraint of the name.
//
// Takes schema (*sqlmodel.Schema) which is the schema, or nil.
// Takes name (string) which is the candidate.
//
// Returns bool which is true when the name is taken.
func (b *catalogueBuilder) constraintNamed(schema *sqlmodel.Schema, name string) bool {
	if schema == nil {
		return false
	}
	for _, table := range schema.Tables {
		if b.tableConstraintNamed(table, name) {
			return true
		}
	}
	return false
}

// tableConstraintNamed reports whether a table has a constraint of the name.
//
// Takes table (*sqlmodel.Table) which is the table.
// Takes name (string) which is the candidate.
//
// Returns bool which is true when the name is taken.
func (b *catalogueBuilder) tableConstraintNamed(table *sqlmodel.Table, name string) bool {
	return slices.ContainsFunc(table.Constraints, func(constraint sqlmodel.Constraint) bool { return b.sameName(constraint.Name, name) })
}

// cockroachConstraintName names a unique, check or foreign key constraint as CockroachDB
// does.
//
// Takes table (*sqlmodel.Table) which holds the constraint.
// Takes constraint (*sqlmodel.Constraint) which is the constraint.
// Takes references ([]string) which are the columns a check reads, in order and with
// repeats.
//
// Returns string which is the name.
func (b *catalogueBuilder) cockroachConstraintName(table *sqlmodel.Table, constraint *sqlmodel.Constraint, references []string) string {
	switch constraint.Kind {
	case sqlmodel.ConstraintUnique:
		return b.cockroachName(table, table.Name+nameSeparator+strings.Join(constraint.Columns, nameSeparator)+nameSeparator+labelUnique, "")
	case sqlmodel.ConstraintForeignKey:
		return b.cockroachName(table, table.Name+nameSeparator+strings.Join(constraint.Columns, nameSeparator)+nameSeparator+labelForeignKey, nameSeparator)
	default:
		return b.cockroachName(table, strings.Join(append([]string{labelCheck}, references...), nameSeparator), "")
	}
}

// cockroachName numbers a name until no constraint or index of the table holds it, as
// CockroachDB does.
//
// Takes table (*sqlmodel.Table) which is the table.
// Takes base (string) which is the name.
// Takes separator (string) which goes before the number.
//
// Returns string which is the free name.
func (b *catalogueBuilder) cockroachName(table *sqlmodel.Table, base, separator string) string {
	name := base
	for number := 1; b.tableConstraintNamed(table, name) || b.tableIndexNamed(table, name); number++ {
		name = base + separator + strconv.Itoa(number)
	}
	return name
}

// tableIndexNamed reports whether a table has an index of the name.
//
// Takes table (*sqlmodel.Table) which is the table.
// Takes name (string) which is the candidate.
//
// Returns bool which is true when the name is taken.
func (b *catalogueBuilder) tableIndexNamed(table *sqlmodel.Table, name string) bool {
	return slices.ContainsFunc(table.Indexes, func(index sqlmodel.Index) bool { return b.sameName(index.Name, name) })
}

// mysqlConstraintName names a unique, check or foreign key constraint as MySQL or MariaDB
// does.
//
// Takes table (*sqlmodel.Table) which holds the constraint.
// Takes constraint (*sqlmodel.Constraint) which is the constraint.
// Takes numbers (*generatedNumbers) which number the statement's foreign keys and checks.
//
// Returns string which is the name.
func (b *catalogueBuilder) mysqlConstraintName(table *sqlmodel.Table, constraint *sqlmodel.Constraint, numbers *generatedNumbers) string {
	switch {
	case constraint.Kind == sqlmodel.ConstraintUnique:
		return b.mysqlKeyName(table, constraint.Columns)
	case constraint.Kind == sqlmodel.ConstraintCheck && b.constraintNames == dialect.ConstraintNamesMariaDB:
		return mariaDBCheckName(table, &numbers.check)
	case constraint.Kind == sqlmodel.ConstraintCheck:
		numbers.check++
		return table.Name + mysqlCheckInfix + strconv.Itoa(numbers.check)
	default:
		numbers.foreignKey++
		return b.foreignKeyPrefix(table.Name) + strconv.Itoa(numbers.foreignKey)
	}
}

// foreignKeyPrefix returns what an unnamed foreign key's generated name starts with:
// MySQL's table name and _ibfk_, or nothing on MariaDB, which since 12 names one by its
// number alone.
//
// Takes tableName (string) which is the table's name.
//
// Returns string which is the prefix.
func (b *catalogueBuilder) foreignKeyPrefix(tableName string) string {
	if b.constraintNames == dialect.ConstraintNamesMariaDB {
		return ""
	}
	return tableName + mysqlForeignKeyInfix
}

// mysqlKeyName names a key after its first column as MySQL does, numbering the name from
// _2 when the table has a key of that name.
//
// Takes table (*sqlmodel.Table) which holds the key.
// Takes columns ([]string) which are the key's columns.
//
// Returns string which is the name.
func (b *catalogueBuilder) mysqlKeyName(table *sqlmodel.Table, columns []string) string {
	if len(columns) == 0 {
		return ""
	}
	taken := func(name string) bool {
		return strings.EqualFold(name, mysqlPrimary) || b.tableIndexNamed(table, name) ||
			slices.ContainsFunc(table.Constraints, func(constraint sqlmodel.Constraint) bool {
				return constraint.Kind == sqlmodel.ConstraintUnique && strings.EqualFold(constraint.Name, name)
			})
	}
	if !taken(columns[0]) {
		return columns[0]
	}
	for number := 2; number < mysqlKeySuffixes; number++ {
		if name := columns[0] + nameSeparator + strconv.Itoa(number); !taken(name) {
			return name
		}
	}
	return ""
}

// mariaDBCheckName names a check declared on its table as MariaDB does, CONSTRAINT_ and
// the first number from the statement's counter on that no check of the table uses.
//
// Takes table (*sqlmodel.Table) which holds the check.
// Takes counter (*int) which is the last number the statement tried.
//
// Returns string which is the name.
func mariaDBCheckName(table *sqlmodel.Table, counter *int) string {
	for {
		*counter++
		name := mariaDBCheckPrefix + strconv.Itoa(*counter)
		if !slices.ContainsFunc(table.Constraints, func(constraint sqlmodel.Constraint) bool {
			return constraint.Kind == sqlmodel.ConstraintCheck && strings.EqualFold(constraint.Name, name)
		}) {
			return name
		}
	}
}

// highestGeneratedNumbers returns the highest numbers of the MySQL foreign key and check
// names generated for a table, from which ALTER TABLE numbers the next. MariaDB numbers
// its checks from one in every statement.
//
// Takes table (*sqlmodel.Table) which is the table.
//
// Returns generatedNumbers which holds zero where the table has none.
func (b *catalogueBuilder) highestGeneratedNumbers(table *sqlmodel.Table) generatedNumbers {
	var numbers generatedNumbers
	if !b.mysqlFamily() {
		return numbers
	}
	for _, constraint := range table.Constraints {
		switch {
		case constraint.Kind == sqlmodel.ConstraintForeignKey:
			if number, generated := generatedNumber(constraint.Name, b.foreignKeyPrefix(table.Name)); generated {
				numbers.foreignKey = max(numbers.foreignKey, number)
			}
		case constraint.Kind == sqlmodel.ConstraintCheck && b.constraintNames == dialect.ConstraintNamesMySQL:
			if number, generated := generatedNumber(constraint.Name, table.Name+mysqlCheckInfix); generated {
				numbers.check = max(numbers.check, number)
			}
		default:
		}
	}
	return numbers
}

// generatedNumber reads the number of a name MySQL generated: a prefix and digits.
//
// Takes name (string) which is the name.
// Takes prefix (string) which is the table's name and the infix.
//
// Returns int which is the number.
// Returns bool which is false when the name is not of that form.
func generatedNumber(name, prefix string) (int, bool) {
	if len(name) <= len(prefix) || !strings.EqualFold(name[:len(prefix)], prefix) {
		return 0, false
	}
	number, err := strconv.Atoi(name[len(prefix):])
	return number, err == nil && number > 0
}

// renameGeneratedNames renames the foreign keys and checks whose names MySQL generated
// from a table's name, as renaming the table renames them. MariaDB names neither after
// the table.
//
// Takes table (*sqlmodel.Table) which is the renamed table, under its old name.
// Takes newName (string) which is its new name.
func (b *catalogueBuilder) renameGeneratedNames(table *sqlmodel.Table, newName string) {
	if !b.mysqlFamily() {
		return
	}
	for index := range table.Constraints {
		constraint := &table.Constraints[index]
		infix := mysqlForeignKeyInfix
		switch {
		case constraint.Kind == sqlmodel.ConstraintCheck && b.constraintNames == dialect.ConstraintNamesMySQL:
			infix = mysqlCheckInfix
		case constraint.Kind != sqlmodel.ConstraintForeignKey, b.constraintNames == dialect.ConstraintNamesMariaDB:
			continue
		}
		if _, generated := generatedNumber(constraint.Name, table.Name+infix); generated {
			constraint.Name = newName + infix + constraint.Name[len(table.Name)+len(infix):]
		}
	}
}

// addImplicitIndexes adds the indexes MySQL creates for foreign keys.
//
// A foreign key needs an index whose leading columns match its columns. An implicit index
// takes the foreign key's name, or a name based on its first column when MySQL generated
// the key name. MySQL 9.7 and MariaDB 12.3 replace an implicit index for an earlier
// foreign key on the same columns with one for the new key.
//
// Takes table (*sqlmodel.Table) which holds the foreign keys.
// Takes from (int) which is the position of the first constraint the statement adds.
// Takes unnamed ([]bool) which marks the constraints the migration left unnamed.
func (b *catalogueBuilder) addImplicitIndexes(table *sqlmodel.Table, from int, unnamed []bool) {
	for index := from; index < len(table.Constraints); index++ {
		constraint := table.Constraints[index]
		if constraint.Kind != sqlmodel.ConstraintForeignKey || len(constraint.Columns) == 0 {
			continue
		}
		position := b.takeImplicitIndex(table, constraint.Columns)
		if position < 0 && b.servedByIndex(table, constraint.Columns) {
			continue
		}
		name := constraint.Name
		if _, generated := generatedNumber(name, table.Name+mysqlForeignKeyInfix); unnamed[index] || generated || b.tableIndexNamed(table, name) {
			name = b.mysqlKeyName(table, constraint.Columns)
		}
		created := sqlmodel.Index{Name: name, Columns: slices.Clone(constraint.Columns), Origin: constraint.Origin,
			IsUnique: false, IsPrimary: false, IsFullText: false}
		if position < 0 {
			position = len(table.Indexes)
		}
		table.Indexes = slices.Insert(slices.Clone(table.Indexes), position, created)
		if b.implicitIndexes != nil {
			b.implicitIndexes[table] = append(b.implicitIndexes[table], name)
		}
	}
}

// takeImplicitIndex removes the index MySQL created for an earlier foreign key that alone
// serves a new foreign key's columns, so the new key's index replaces it.
//
// Takes table (*sqlmodel.Table) which holds the index.
// Takes columns ([]string) which are the new foreign key's columns.
//
// Returns int which is the position the index held, or -1 when no such index alone serves
// the columns.
func (b *catalogueBuilder) takeImplicitIndex(table *sqlmodel.Table, columns []string) int {
	names := b.implicitIndexes[table]
	if len(names) == 0 || b.leadsWith(table.PrimaryKey, columns) ||
		slices.ContainsFunc(table.Constraints, func(constraint sqlmodel.Constraint) bool {
			return constraint.Kind == sqlmodel.ConstraintUnique && b.leadsWith(constraint.Columns, columns)
		}) {
		return -1
	}
	position := -1
	for candidate, index := range table.Indexes {
		if index.IsFullText || !b.leadsWith(index.Columns, columns) {
			continue
		}
		if !slices.ContainsFunc(names, func(name string) bool { return b.sameName(index.Name, name) }) {
			return -1
		}
		position = candidate
	}
	if position < 0 {
		return -1
	}
	taken := table.Indexes[position].Name
	b.implicitIndexes[table] = slices.DeleteFunc(slices.Clone(names), func(name string) bool { return b.sameName(name, taken) })
	table.Indexes = slices.Delete(slices.Clone(table.Indexes), position, position+1)
	return position
}

// servedByIndex reports whether an index or key of a table leads with the given columns.
//
// Takes table (*sqlmodel.Table) which is the table.
// Takes columns ([]string) which are the columns.
//
// Returns bool which is true when one does.
func (b *catalogueBuilder) servedByIndex(table *sqlmodel.Table, columns []string) bool {
	if b.leadsWith(table.PrimaryKey, columns) {
		return true
	}
	for _, index := range table.Indexes {
		if !index.IsFullText && b.leadsWith(index.Columns, columns) {
			return true
		}
	}
	return slices.ContainsFunc(table.Constraints, func(constraint sqlmodel.Constraint) bool {
		return constraint.Kind == sqlmodel.ConstraintUnique && b.leadsWith(constraint.Columns, columns)
	})
}

// leadsWith reports whether a key's leading columns are the given columns, in order.
//
// Takes key ([]string) which are the key's columns.
// Takes columns ([]string) which are the leading columns sought.
//
// Returns bool which is true when they lead the key.
func (b *catalogueBuilder) leadsWith(key, columns []string) bool {
	if len(columns) == 0 || len(key) < len(columns) {
		return false
	}
	for index, column := range columns {
		if !b.sameName(key[index], column) {
			return false
		}
	}
	return true
}

// dropImplicitIndexes drops the indexes MySQL created for foreign keys that a new key on
// the given columns serves instead, as MySQL drops them.
//
// Takes table (*sqlmodel.Table) which gains the key.
// Takes columns ([]string) which are the new key's columns.
func (b *catalogueBuilder) dropImplicitIndexes(table *sqlmodel.Table, columns []string) {
	names := b.implicitIndexes[table]
	if len(names) == 0 {
		return
	}
	var kept []string
	for _, name := range names {
		position := slices.IndexFunc(table.Indexes, func(index sqlmodel.Index) bool { return b.sameName(index.Name, name) })
		switch {
		case position < 0:
		case b.leadsWith(columns, table.Indexes[position].Columns):
			table.Indexes = slices.Delete(slices.Clone(table.Indexes), position, position+1)
		default:
			kept = append(kept, name)
		}
	}
	b.implicitIndexes[table] = kept
}

// tableColumns keeps the names that are columns of a table, in order and with repeats.
//
// Takes table (*sqlmodel.Table) which is the table.
// Takes names ([]string) which are the candidates.
//
// Returns []string which holds the table's columns among them.
func (b *catalogueBuilder) tableColumns(table *sqlmodel.Table, names []string) []string {
	var columns []string
	for _, name := range names {
		if column := b.namedColumn(table.Columns, name); column != nil {
			columns = append(columns, column.Name)
		}
	}
	return columns
}

// distinctNames returns names without repeats, in the order each first appears.
//
// Takes names ([]string) which are the names.
//
// Returns []string which holds each once.
func distinctNames(names []string) []string {
	var distinct []string
	for _, name := range names {
		if !slices.Contains(distinct, name) {
			distinct = append(distinct, name)
		}
	}
	return distinct
}

// indexColumnNames makes the names of an index's elements unique by numbering a repeat,
// as PostgreSQL and CockroachDB do.
//
// Takes elements ([]string) which name the elements.
//
// Returns []string which holds the unique names.
func indexColumnNames(elements []string) []string {
	names := make([]string, 0, len(elements))
	for _, original := range elements {
		name := original
		for number := 1; slices.Contains(names, name); number++ {
			suffix := strconv.Itoa(number)
			name = clipBytes(original, postgresNameBytes-len(suffix)) + suffix
		}
		names = append(names, name)
	}
	return names
}

// postgresAddition joins column names as PostgreSQL does for a name, stopping once the
// result is longer than a name may be.
//
// Takes names ([]string) which are the column names.
//
// Returns string which is the joined names.
func postgresAddition(names []string) string {
	var joined strings.Builder
	for _, name := range names {
		if joined.Len() > 0 {
			joined.WriteString(nameSeparator)
		}
		joined.WriteString(name)
		if joined.Len() > postgresNameBytes {
			break
		}
	}
	result := joined.String()
	if len(result) > postgresAdditionBytes {
		result = result[:postgresAdditionBytes]
	}
	return result
}

// postgresObjectName builds a name from a table name, an optional addition and a label as
// PostgreSQL's makeObjectName does, cutting the longer part first so that the whole fits
// in 63 bytes.
//
// Takes first (string) which is the table's name.
// Takes second (string) which is the addition, or empty.
// Takes label (string) which ends the name.
//
// Returns string which is the name.
func postgresObjectName(first, second, label string) string {
	first = clipBytes(first, postgresNameBytes)
	overhead := len(label) + 1
	if second != "" {
		overhead++
	}
	available := postgresNameBytes - overhead
	firstLength, secondLength := len(first), len(second)
	for firstLength+secondLength > available {
		if firstLength > secondLength {
			firstLength--
		} else {
			secondLength--
		}
	}
	name := clipBytes(first, firstLength)
	if second != "" {
		name += nameSeparator + clipBytes(second, secondLength)
	}
	return name + nameSeparator + label
}

// numberedLabel appends a number after the unnumbered label has been tried.
//
// Takes label (string) which is the label.
// Takes pass (int) which counts the names already tried.
//
// Returns string which is the label, numbered when pass is positive.
func numberedLabel(label string, pass int) string {
	if pass == 0 {
		return label
	}
	return label + strconv.Itoa(pass)
}

// clipBytes cuts a name to at most a number of bytes without splitting a character.
//
// Takes name (string) which is the name.
// Takes limit (int) which is the most bytes to keep.
//
// Returns string which is the cut name.
func clipBytes(name string, limit int) string {
	if len(name) <= limit {
		return name
	}
	for limit > 0 && !utf8.RuneStart(name[limit]) {
		limit--
	}
	return name[:limit]
}
