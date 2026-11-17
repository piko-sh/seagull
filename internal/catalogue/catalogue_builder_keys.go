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
	"strings"

	"seagull.sh/seagull/internal/sqlmodel"
)

// renameKeyColumns renames a column in its table's indexes, primary key and constraints,
// and in the foreign keys that reference it.
//
// Takes table (*sqlmodel.Table) which owns the renamed column.
// Takes previous (string) which is the column's old name.
// Takes next (string) which is its new name.
func (b *catalogueBuilder) renameKeyColumns(table *sqlmodel.Table, previous, next string) {
	if previous == next {
		return
	}
	for index := range table.Indexes {
		table.Indexes[index].Columns = b.renamedNames(table.Indexes[index].Columns, previous, next)
	}
	table.PrimaryKey = b.renamedNames(table.PrimaryKey, previous, next)
	for index := range table.Constraints {
		table.Constraints[index].Columns = b.renamedNames(table.Constraints[index].Columns, previous, next)
	}
	b.eachForeignKey(func(holder *sqlmodel.Table, constraint *sqlmodel.Constraint) {
		if b.referencesTable(holder, constraint, table.Schema, table.Name) {
			constraint.ForeignColumns = b.renamedNames(constraint.ForeignColumns, previous, next)
		}
	})
}

// renamedNames returns a copy of names with one name replaced, leaving the shared slice
// untouched.
//
// Takes names ([]string) which are column names.
// Takes previous (string) which is the name to replace.
// Takes next (string) which replaces it.
//
// Returns []string which is names itself when nothing matched.
func (b *catalogueBuilder) renamedNames(names []string, previous, next string) []string {
	if !slices.ContainsFunc(names, func(name string) bool { return b.sameName(name, previous) }) {
		return names
	}
	renamed := slices.Clone(names)
	for index, name := range renamed {
		if b.sameName(name, previous) {
			renamed[index] = next
		}
	}
	return renamed
}

// retargetForeignKeys points the foreign keys that reference a renamed or moved table at
// its new name, as the database follows the table.
//
// Takes fromSchema (string) which is the table's old schema.
// Takes fromName (string) which is its old name.
// Takes toSchema (string) which is its new schema.
// Takes toName (string) which is its new name.
func (b *catalogueBuilder) retargetForeignKeys(fromSchema, fromName, toSchema, toName string) {
	b.eachForeignKey(func(holder *sqlmodel.Table, constraint *sqlmodel.Constraint) {
		if !b.referencesTable(holder, constraint, fromSchema, fromName) {
			return
		}
		schemaName, _, qualified := strings.Cut(constraint.ForeignTable, ".")
		switch {
		case !b.sameName(fromSchema, toSchema):
			constraint.ForeignTable = toSchema + "." + toName
		case qualified:
			constraint.ForeignTable = schemaName + "." + toName
		default:
			constraint.ForeignTable = toName
		}
	})
}

// eachForeignKey calls visit with every foreign key of every table.
//
// Takes visit (func(*sqlmodel.Table, *sqlmodel.Constraint)) which receives the table
// holding the key and the key itself, which it may change.
func (b *catalogueBuilder) eachForeignKey(visit func(*sqlmodel.Table, *sqlmodel.Constraint)) {
	for _, schema := range b.catalogue.Schemas {
		for _, holder := range schema.Tables {
			for index := range holder.Constraints {
				if holder.Constraints[index].Kind == sqlmodel.ConstraintForeignKey {
					visit(holder, &holder.Constraints[index])
				}
			}
		}
	}
}

// referencesTable reports whether a foreign key references a table. An unqualified
// reference names a table in its own table's schema or the default schema.
//
// Takes holder (*sqlmodel.Table) which holds the foreign key.
// Takes constraint (*sqlmodel.Constraint) which is the foreign key.
// Takes schemaName (string) which is the referenced table's schema.
// Takes name (string) which is the referenced table's name.
//
// Returns bool which is true when the key references the table.
func (b *catalogueBuilder) referencesTable(holder *sqlmodel.Table, constraint *sqlmodel.Constraint, schemaName, name string) bool {
	referencedSchema, referencedName, qualified := strings.Cut(constraint.ForeignTable, ".")
	if !qualified {
		if !b.sameName(constraint.ForeignTable, name) {
			return false
		}
		return b.sameName(holder.Schema, schemaName) || b.sameName(b.catalogue.DefaultSchema, schemaName)
	}
	return b.sameName(referencedName, name) && b.sameName(referencedSchema, schemaName)
}

// dropUniqueConstraint removes a unique constraint by name, as dropping the index behind
// it does on engines whose unique constraints are indexes.
//
// Takes table (*sqlmodel.Table) which holds the constraint.
// Takes name (string) which names the index.
//
// Returns bool which is true when a constraint was removed.
func (b *catalogueBuilder) dropUniqueConstraint(table *sqlmodel.Table, name string) bool {
	before := len(table.Constraints)
	table.Constraints = slices.DeleteFunc(slices.Clone(table.Constraints), func(constraint sqlmodel.Constraint) bool {
		return constraint.Kind == sqlmodel.ConstraintUnique && constraint.Name != "" && b.sameName(constraint.Name, name)
	})
	return len(table.Constraints) != before
}

// dropPrimaryKey removes a table's primary key and the named constraint recording it.
//
// Takes table (*sqlmodel.Table) which loses its key.
func dropPrimaryKey(table *sqlmodel.Table) {
	table.PrimaryKey = nil
	table.Constraints = slices.DeleteFunc(slices.Clone(table.Constraints), func(constraint sqlmodel.Constraint) bool {
		return constraint.Kind == sqlmodel.ConstraintPrimaryKey
	})
}
