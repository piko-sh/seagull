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
	"maps"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/testkit/fuzztyped"
)

// SchemaHistoryVersion is the first byte of a version 18 schema history.
const SchemaHistoryVersion = 18

// MaxBytes bounds one structured input.
const MaxBytes = 1024

// RegressionMarker marks the end of a saved finding reproduction.
//
// The decoder removes it, so an input behaves the same with or without the marker.
const RegressionMarker = fuzztyped.RegressionMarker

// Namespace is the second schema a history may create. A harness whose suite isolates
// inputs by schema renames it, in the files and in its observations alike.
const Namespace = "fuzz_ns"

const (
	// KindTable is an ordinary or partitioned table.
	KindTable = "table"

	// KindView is a view.
	KindView = "view"

	// KindMaterialisedView is a materialised view.
	KindMaterialisedView = "materialised-view"
)

const (
	// FamilyInteger is a signed 64-bit integer.
	FamilyInteger = "integer"

	// FamilyFloat is a binary floating-point number.
	FamilyFloat = "float"

	// FamilyDecimal is an exact decimal number.
	FamilyDecimal = "decimal"

	// FamilyBoolean is a truth value.
	FamilyBoolean = "boolean"

	// FamilyText is a character string.
	FamilyText = "text"

	// FamilyDate is a calendar date.
	FamilyDate = "date"

	// FamilyTimestamp is a date and time without a zone.
	FamilyTimestamp = "timestamp"

	// FamilyTimestampTZ is a zone-aware date and time.
	FamilyTimestampTZ = "timestamptz"

	// FamilyUUID is a universally unique identifier.
	FamilyUUID = "uuid"

	// FamilyJSON is a JSON document.
	FamilyJSON = "json"

	// FamilyArray is an array of integers.
	FamilyArray = "array"

	// FamilyBinary is a byte string.
	FamilyBinary = "binary"

	// FamilyEnum is an enumerated type.
	FamilyEnum = "enum"
)

const (
	// headerWidth is the number of bytes before the first migration.
	headerWidth = 2

	// maxMigrations bounds the migrations of one history.
	maxMigrations = 6

	// maxOperations bounds the operations of one migration.
	maxOperations = 3
)

// Case is one schema history and the schema the model expects after it.
type Case struct {
	// Engine names the dialect.
	Engine string

	// Data holds a copy of the input.
	Data []byte

	// Migrations holds the history's migrations in version order.
	Migrations []Migration

	// Features lists the operations, families and objects the history covers.
	Features []string

	// Final is the schema the model expects once every migration has run.
	Final Schema
}

// Migration is one versioned migration file.
type Migration struct {
	// Statements holds the migration's statements without terminators.
	Statements []string
}

// Schema is the observable schema state of the history's objects.
type Schema struct {
	// Enums maps each enum type to its values in declaration order.
	Enums map[string][]string

	// Relations holds the tables, views and materialised views by Key.
	Relations []Relation

	// Sequences holds the sequence names, qualified like a relation Key.
	Sequences []string
}

// Relation is a table, view or materialised view.
type Relation struct {
	// Namespace is empty for the default schema, or Namespace.
	Namespace string

	// Name is the relation's name.
	Name string

	// Kind is KindTable, KindView or KindMaterialisedView.
	Kind string

	// PartitionOf is the Key of the table this table is a partition of, or empty.
	PartitionOf string

	// Columns holds the columns in their engine order.
	Columns []Column

	// PrimaryKey holds the primary key's columns, empty for none.
	PrimaryKey []string

	// Uniques holds the columns of each unique constraint or index other than the key.
	Uniques [][]string

	// Checks holds the CHECK constraint names, sorted.
	Checks []string

	// ForeignKeys holds the foreign keys.
	ForeignKeys []ForeignKey
}

// Column is one column of a relation.
type Column struct {
	// Name is the column's name.
	Name string

	// Family is the column's coarse type family.
	Family string

	// Nullable reports whether the column admits NULL.
	Nullable bool

	// Default reports whether the column has a default other than generation.
	Default bool

	// Generated reports whether the engine computes the column from others.
	Generated bool
}

// ForeignKey is a foreign key of a table.
type ForeignKey struct {
	// Table is the Key of the referenced table.
	Table string

	// Columns holds the referencing columns.
	Columns []string

	// References holds the referenced columns.
	References []string
}

// Key returns the relation's name, qualified by its schema outside the default one.
//
// Returns string which identifies the relation within one history.
func (relation *Relation) Key() string {
	return qualify(relation.Namespace, relation.Name)
}

// Files returns the history as migration files under migrations/.
//
// Returns map[string][]byte which maps each file name to its content.
func (model *Case) Files() map[string][]byte {
	files := make(map[string][]byte, len(model.Migrations))
	for index, migration := range model.Migrations {
		var content strings.Builder
		for _, statement := range migration.Statements {
			content.WriteString(statement)
			content.WriteString(";\n")
		}
		files[fmt.Sprintf("migrations/%04d_history.up.sql", index+1)] = []byte(content.String())
	}
	return files
}

// Describe spells the history for failure messages.
//
// Returns string which lists every migration's statements.
func (model *Case) Describe() string {
	var text strings.Builder
	for index, migration := range model.Migrations {
		fmt.Fprintf(&text, "-- migration %d\n", index+1)
		for _, statement := range migration.Statements {
			text.WriteString(statement)
			text.WriteString(";\n")
		}
	}
	return text.String()
}

// Decode reads a version 18 input without consulting Seagull.
//
// Takes engine (string) which names the dialect.
// Takes data ([]byte) which holds the version and choices.
//
// Returns Case which owns a copy of the input.
// Returns bool which is false for another version, an unmodelled engine, an oversized
// input or a history with no statements.
func Decode(engine string, data []byte) (Case, bool) {
	if len(data) == 0 || len(data) > MaxBytes || data[0] != SchemaHistoryVersion || !Supported(engine) {
		return Case{}, false
	}
	body := strings.TrimSuffix(string(data), RegressionMarker)
	history := newHistory(engine, []byte(body))
	migrations := 1 + history.reader.at(1)%maxMigrations
	for range migrations {
		operations := 1 + history.reader.next()%maxOperations
		if engine == engineCockroachDB {

			operations = 1
		}
		var statements []string
		for range operations {
			statements = append(statements, history.operation()...)
		}
		if len(statements) != 0 {
			history.migrations = append(history.migrations, Migration{Statements: statements})
		}
	}
	if len(history.migrations) == 0 {
		return Case{}, false
	}
	features := slices.Sorted(maps.Keys(history.features))
	return Case{Engine: engine, Data: slices.Clone(data), Migrations: history.migrations, Features: features, Final: history.final()}, true
}

// qualify spells a relation key from its schema and name.
//
// Takes namespace (string) which is empty for the default schema.
// Takes name (string) which is the object's name.
//
// Returns string which is the name, qualified outside the default schema.
func qualify(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + "." + name
}
