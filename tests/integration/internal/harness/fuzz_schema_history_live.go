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

package harness

import (
	"context"
	"database/sql"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"seagull.sh/seagull/internal/testkit/fuzzschema"
)

const (
	// historyPrefix starts the name of every object a history creates.
	historyPrefix = "fuzz_"

	// historySeparator separates the members of a catalogue list.
	historySeparator = ","
)

// historyCheckPattern finds named CHECK constraints in stored table definitions.
var historyCheckPattern = regexp.MustCompile(`(?i)CONSTRAINT\s+["` + "`" + `]?(fuzz_ck_\d+)["` + "`" + `]?\s+CHECK`)

// historyGeneratedPattern finds generated columns in DuckDB's stored table definitions.
var historyGeneratedPattern = regexp.MustCompile(`(?i)(?:\(|,)\s*"?(\w+)"?\s+[\w\[\]]+(?:\([^)]*\))?\s+GENERATED\s+ALWAYS`)

// mysqlHistoryFamilies maps a lower-case MySQL or MariaDB data_type to the model's
// family.
var mysqlHistoryFamilies = map[string]string{"bigint": fuzzschema.FamilyInteger, "int": fuzzschema.FamilyInteger, "tinyint": fuzzschema.FamilyInteger,
	"double": fuzzschema.FamilyFloat, "float": fuzzschema.FamilyFloat, "decimal": fuzzschema.FamilyDecimal, "varchar": fuzzschema.FamilyText,
	"char": fuzzschema.FamilyText, "text": fuzzschema.FamilyText, "longtext": fuzzschema.FamilyText, "date": fuzzschema.FamilyDate,
	"datetime": fuzzschema.FamilyTimestamp, "json": fuzzschema.FamilyJSON, "varbinary": fuzzschema.FamilyBinary,
	"blob": fuzzschema.FamilyBinary, "enum": fuzzschema.FamilyEnum, "uuid": fuzzschema.FamilyUUID}

// sqliteHistoryFamilies maps an upper-case declared SQLite type to the model's family.
var sqliteHistoryFamilies = map[string]string{"INTEGER": fuzzschema.FamilyInteger, "BIGINT": fuzzschema.FamilyInteger, "REAL": fuzzschema.FamilyFloat,
	"NUMERIC": fuzzschema.FamilyDecimal, "DECIMAL": fuzzschema.FamilyDecimal, "BOOLEAN": fuzzschema.FamilyBoolean,
	"TEXT": fuzzschema.FamilyText, "DATE": fuzzschema.FamilyDate, "TIMESTAMP": fuzzschema.FamilyTimestamp,
	"DATETIME": fuzzschema.FamilyTimestamp, "BLOB": fuzzschema.FamilyBinary}

// historyLive collects the live observation of one database.
type historyLive struct {
	// database holds the schema.
	database *sql.DB

	// engine names the dialect.
	engine string

	// schema accumulates the observation.
	schema fuzzschema.Schema
}

// rows runs a catalogue query and spells every value as text.
//
// Takes t (*testing.T) which reports a failed query.
// Takes query (string) which is the SQL.
// Takes arguments (...any) which bind its parameters.
//
// Returns [][]string which holds the rows, with NULL as an empty string.
func (live *historyLive) rows(ctx context.Context, t *testing.T, query string, arguments ...any) [][]string {
	t.Helper()
	scanned := fuzzRows(ctx, t, live.database, query, arguments)
	result := make([][]string, 0, len(scanned))
	for _, row := range scanned {
		texts := make([]string, 0, len(row))
		for _, value := range row {
			texts = append(texts, historyText(value))
		}
		result = append(result, texts)
	}
	return result
}

// scalar runs a catalogue query that returns one value.
//
// Takes t (*testing.T) which reports a failed query or another shape.
// Takes query (string) which is the SQL.
// Takes arguments (...any) which bind its parameters.
//
// Returns string which is the value, empty for NULL.
func (live *historyLive) scalar(ctx context.Context, t *testing.T, query string, arguments ...any) string {
	t.Helper()
	rows := live.rows(ctx, t, query, arguments...)
	require.Len(t, rows, 1, "%s returned another number of rows", query)
	require.Len(t, rows[0], 1, "%s returned another number of columns", query)
	return rows[0][0]
}

// observeMySQL reads MySQL and MariaDB's information_schema.
//
// Takes t (*testing.T) which reports failed queries.
func (live *historyLive) observeMySQL(ctx context.Context, t *testing.T) {
	t.Helper()
	for _, row := range live.rows(ctx, t, "SELECT table_name, table_type FROM information_schema.tables WHERE table_schema = database()") {
		var name, kind string
		historyFields(t, row, &name, &kind)
		if !strings.HasPrefix(name, historyPrefix) {
			continue
		}
		if kind == "SEQUENCE" {
			live.schema.Sequences = append(live.schema.Sequences, name)
			continue
		}
		relation := newHistoryRelation("", name, fuzzschema.KindTable)
		if kind == "VIEW" {
			relation.Kind = fuzzschema.KindView
		}
		for _, column := range live.rows(ctx, t, "SELECT column_name, data_type, column_type, is_nullable, column_default IS NOT NULL, "+
			"COALESCE(column_default, ''), COALESCE(generation_expression, '') FROM information_schema.columns "+
			"WHERE table_schema = database() AND table_name = ? ORDER BY ordinal_position", name) {
			var columnName, dataType, columnType, nullable, hasDefault, defaultText, generation string
			historyFields(t, column, &columnName, &dataType, &columnType, &nullable, &hasDefault, &defaultText, &generation)

			defaulted := historyBool(hasDefault) && (live.engine != engineMariaDB || defaultText != "NULL")
			relation.Columns = append(relation.Columns, fuzzschema.Column{Name: columnName, Family: mysqlHistoryFamily(dataType, columnType),
				Nullable: nullable == "YES", Default: defaulted && generation == "", Generated: generation != ""})
		}
		if relation.Kind == fuzzschema.KindTable {
			live.mysqlConstraints(ctx, t, &relation)
		}
		live.schema.Relations = append(live.schema.Relations, relation)
	}
}

// mysqlConstraints reads a MySQL or MariaDB table's keys and checks.
//
// Takes t (*testing.T) which reports failed queries.
// Takes relation (*fuzzschema.Relation) which receives them.
func (live *historyLive) mysqlConstraints(ctx context.Context, t *testing.T, relation *fuzzschema.Relation) {
	t.Helper()
	kinds := map[string]string{}
	for _, row := range live.rows(ctx, t, "SELECT constraint_name, constraint_type FROM information_schema.table_constraints "+
		"WHERE table_schema = database() AND table_name = ?", relation.Name) {
		var name, kind string
		historyFields(t, row, &name, &kind)
		kinds[name] = kind
		if kind == "CHECK" {
			relation.Checks = append(relation.Checks, name)
		}
	}
	keys := map[string]*fuzzschema.ForeignKey{}
	var order []string
	for _, row := range live.rows(ctx, t, "SELECT constraint_name, column_name, COALESCE(referenced_table_name, ''), "+
		"COALESCE(referenced_column_name, '') FROM information_schema.key_column_usage WHERE table_schema = database() AND table_name = ? "+
		"ORDER BY constraint_name, ordinal_position", relation.Name) {
		var name, columnName, referenced, referencedColumn string
		historyFields(t, row, &name, &columnName, &referenced, &referencedColumn)
		if keys[name] == nil {
			keys[name] = &fuzzschema.ForeignKey{Table: referenced, Columns: nil, References: nil}
			order = append(order, name)
		}
		keys[name].Columns = append(keys[name].Columns, columnName)
		keys[name].References = append(keys[name].References, referencedColumn)
	}
	for _, name := range order {
		switch kinds[name] {
		case "PRIMARY KEY":
			relation.PrimaryKey = keys[name].Columns
		case "UNIQUE":
			relation.Uniques = append(relation.Uniques, keys[name].Columns)
		case "FOREIGN KEY":
			relation.ForeignKeys = append(relation.ForeignKeys, *keys[name])
		default:
		}
	}
}

// observeSQLite reads SQLite's schema table and pragmas.
//
// Takes t (*testing.T) which reports failed queries.
func (live *historyLive) observeSQLite(ctx context.Context, t *testing.T) {
	t.Helper()
	for _, row := range live.rows(ctx, t, "SELECT name, type, COALESCE(sql, '') FROM sqlite_schema WHERE type IN ('table', 'view')") {
		var name, kind, definition string
		historyFields(t, row, &name, &kind, &definition)
		if !strings.HasPrefix(name, historyPrefix) {
			continue
		}
		relation := newHistoryRelation("", name, fuzzschema.KindTable)
		if kind == "view" {
			relation.Kind = fuzzschema.KindView
		}
		for _, column := range live.rows(ctx, t, `SELECT name, type, "notnull", dflt_value IS NOT NULL, hidden IN (2, 3) FROM pragma_table_xinfo(?) ORDER BY cid`, name) {
			var columnName, declared, notNull, hasDefault, generated string
			historyFields(t, column, &columnName, &declared, &notNull, &hasDefault, &generated)
			relation.Columns = append(relation.Columns, fuzzschema.Column{Name: columnName, Family: sqliteHistoryFamily(declared),
				Nullable: !historyBool(notNull), Default: historyBool(hasDefault), Generated: historyBool(generated)})
		}
		if relation.Kind == fuzzschema.KindTable {
			live.sqliteConstraints(ctx, t, &relation, definition)
			sqliteRowidAlias(&relation, live.rows(ctx, t, "SELECT type FROM pragma_table_xinfo(?) WHERE pk > 0", name))
		}
		live.schema.Relations = append(live.schema.Relations, relation)
	}
}

// sqliteConstraints reads a SQLite table's keys and checks.
//
// Takes t (*testing.T) which reports failed queries.
// Takes relation (*fuzzschema.Relation) which receives them.
// Takes definition (string) which is the stored CREATE TABLE.
func (live *historyLive) sqliteConstraints(ctx context.Context, t *testing.T, relation *fuzzschema.Relation, definition string) {
	t.Helper()
	for _, row := range live.rows(ctx, t, "SELECT name FROM pragma_table_xinfo(?) WHERE pk > 0 ORDER BY pk", relation.Name) {
		relation.PrimaryKey = append(relation.PrimaryKey, row[0])
	}
	for _, index := range live.rows(ctx, t, `SELECT name FROM pragma_index_list(?) WHERE "unique" = 1 AND origin IN ('u', 'c')`, relation.Name) {
		var key []string
		for _, row := range live.rows(ctx, t, "SELECT name FROM pragma_index_info(?) ORDER BY seqno", index[0]) {
			key = append(key, row[0])
		}
		relation.Uniques = append(relation.Uniques, key)
	}
	keys := map[string]*fuzzschema.ForeignKey{}
	var order []string
	for _, row := range live.rows(ctx, t, `SELECT id, "table", "from", COALESCE("to", '') FROM pragma_foreign_key_list(?) ORDER BY id, seq`, relation.Name) {
		var id, referenced, from, to string
		historyFields(t, row, &id, &referenced, &from, &to)
		if keys[id] == nil {
			keys[id] = &fuzzschema.ForeignKey{Table: referenced, Columns: nil, References: nil}
			order = append(order, id)
		}
		keys[id].Columns = append(keys[id].Columns, from)
		keys[id].References = append(keys[id].References, to)
	}
	for _, id := range order {
		relation.ForeignKeys = append(relation.ForeignKeys, *keys[id])
	}
	relation.Checks = historyChecks(definition)
}

// observeHistory reads the history's objects from the engine's own catalogue, with SQL
// written here rather than Seagull's schema sources.
//
// Takes t (*testing.T) which reports failed catalogue queries.
// Takes database (*sql.DB) which holds the schema.
// Takes engine (string) which names the dialect.
// Takes namespace (string) which is the second schema's name.
//
// Returns fuzzschema.Schema which is the database's view of the final schema.
func observeHistory(ctx context.Context, t *testing.T, database *sql.DB, engine, namespace string) fuzzschema.Schema {
	t.Helper()
	live := &historyLive{database: database, engine: engine,
		schema: fuzzschema.Schema{Enums: map[string][]string{}, Relations: nil, Sequences: nil}}
	switch {
	case isPostgresFamily(engine):
		live.observePostgres(ctx, t, namespace)
	case engine == engineMySQL || engine == engineMariaDB:
		live.observeMySQL(ctx, t)
	case engine == engineSQLite:
		live.observeSQLite(ctx, t)
	case engine == engineDuckDB:
		live.observeDuckDB(ctx, t, namespace)
	default:
		live.observeClickHouse(ctx, t)
	}
	return live.schema
}

// historyFields assigns a row's values to targets in order.
//
// Takes t (*testing.T) which reports a row narrower than the targets.
// Takes row ([]string) which is the row.
// Takes targets (...*string) which receive its values.
func historyFields(t *testing.T, row []string, targets ...*string) {
	t.Helper()
	require.GreaterOrEqual(t, len(row), len(targets), "catalogue row is narrower than expected")
	for index, target := range targets {
		*target = row[index]
	}
}

// historyText spells a catalogue value.
//
// Takes value (any) which is the scanned value.
//
// Returns string which is its text, empty for NULL.
func historyText(value any) string {
	if value == nil {
		return ""
	}
	return fuzzValue(value)
}

// historyBool reads a catalogue truth value in any driver's spelling.
//
// Takes value (string) which is the value's text.
//
// Returns bool which is true for true, one, t and yes.
func historyBool(value string) bool {
	return slices.Contains([]string{"1", "t", historyTrue, "yes"}, strings.ToLower(value))
}

// historySplit splits a comma-separated list, empty for an empty string.
//
// Takes text (string) which is the list.
//
// Returns []string which holds the trimmed members.
func historySplit(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	parts := strings.Split(text, historySeparator)
	for index, part := range parts {
		parts[index] = strings.Trim(strings.TrimSpace(part), `"`)
	}
	return parts
}

// historyChecks finds the CHECK constraint names of a stored table definition.
//
// Takes definition (string) which is the stored CREATE TABLE.
//
// Returns []string which holds the names.
func historyChecks(definition string) []string {
	matches := historyCheckPattern.FindAllStringSubmatch(definition, -1)
	names := make([]string, 0, len(matches))
	for _, match := range matches {
		names = append(names, match[1])
	}
	return names
}

// newHistoryRelation starts a relation observation.
//
// Takes scope (string) which is the model namespace.
// Takes name (string) which is the relation's name.
// Takes kind (string) which is its kind.
//
// Returns fuzzschema.Relation which has no columns or constraints yet.
func newHistoryRelation(scope, name, kind string) fuzzschema.Relation {
	return fuzzschema.Relation{Namespace: scope, Name: name, Kind: kind, PartitionOf: "", Columns: nil, PrimaryKey: nil, Uniques: nil,
		Checks: nil, ForeignKeys: nil}
}

// mysqlHistoryFamily maps a MySQL or MariaDB type to the model's family.
//
// Takes dataType (string) which is information_schema's data_type.
// Takes columnType (string) which is its column_type.
//
// Returns string which is the family, or the type of an unmapped one.
func mysqlHistoryFamily(dataType, columnType string) string {
	if strings.EqualFold(dataType, "tinyint") && strings.HasPrefix(strings.ToLower(columnType), "tinyint(1)") {
		return fuzzschema.FamilyBoolean
	}
	if family, ok := mysqlHistoryFamilies[strings.ToLower(dataType)]; ok {
		return family
	}
	return "other:" + columnType
}

// sqliteRowidAlias marks a single INTEGER primary key column as defaulted, as it aliases
// the rowid, which SQLite assigns when an insert leaves it out.
//
// Takes relation (*fuzzschema.Relation) which is the table.
// Takes keyTypes ([][]string) which holds the declared types of its primary key columns.
func sqliteRowidAlias(relation *fuzzschema.Relation, keyTypes [][]string) {
	if len(relation.PrimaryKey) != 1 || len(keyTypes) != 1 || !strings.EqualFold(keyTypes[0][0], "INTEGER") {
		return
	}
	for index := range relation.Columns {
		if relation.Columns[index].Name == relation.PrimaryKey[0] {
			relation.Columns[index].Default = true
		}
	}
}

// sqliteHistoryFamily maps a declared SQLite type to the model's family.
//
// Takes declared (string) which is the declared type.
//
// Returns string which is the family, or the declared type of an unmapped one.
func sqliteHistoryFamily(declared string) string {
	base, _, _ := strings.Cut(strings.ToUpper(declared), "(")
	if family, ok := sqliteHistoryFamilies[strings.TrimSpace(base)]; ok {
		return family
	}
	return "other:" + declared
}
