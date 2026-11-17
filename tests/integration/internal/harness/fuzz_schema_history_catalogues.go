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
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"seagull.sh/seagull/internal/testkit/fuzzschema"
)

const (
	// postgresRelations lists the relations and sequences of two schemas.
	postgresRelations = "SELECT n.nspname, c.relname, c.relkind::text, c.oid::INT8 FROM pg_class c " +
		"JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname IN ($1, $2) AND c.relkind IN ('r', 'p', 'v', 'm', 'S')"

	// postgresColumns lists a relation's columns.
	postgresColumns = "SELECT a.attname, format_type(a.atttypid, a.atttypmod), t.typtype::text, t.typcategory::text, a.attnotnull, " +
		"a.atthasdef, a.attgenerated::text FROM pg_attribute a JOIN pg_type t ON t.oid = a.atttypid " +
		"WHERE a.attrelid = $1::text::INT8::OID AND a.attnum > 0 AND NOT a.attisdropped ORDER BY a.attnum"

	// postgresConstraints lists a table's key, unique, check and foreign key constraints.
	postgresConstraints = "SELECT con.contype::text, con.conname, array_to_string(ARRAY(SELECT a.attname FROM unnest(con.conkey) " +
		"WITH ORDINALITY k(num, ord) JOIN pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = k.num ORDER BY k.ord), ','), " +
		"COALESCE(fn.nspname, ''), COALESCE(fc.relname, ''), COALESCE(array_to_string(ARRAY(SELECT a.attname FROM unnest(con.confkey) " +
		"WITH ORDINALITY k(num, ord) JOIN pg_attribute a ON a.attrelid = con.confrelid AND a.attnum = k.num ORDER BY k.ord), ','), '') " +
		"FROM pg_constraint con LEFT JOIN pg_class fc ON fc.oid = con.confrelid LEFT JOIN pg_namespace fn ON fn.oid = fc.relnamespace " +
		"WHERE con.conrelid = $1::text::INT8::OID AND con.contype IN ('p', 'u', 'c', 'f')"

	// postgresUniqueIndexes lists unique indexes no constraint owns.
	postgresUniqueIndexes = "SELECT array_to_string(ARRAY(SELECT a.attname FROM unnest(i.indkey::int2[]) WITH ORDINALITY k(num, ord) " +
		"JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = k.num ORDER BY k.ord), ',') FROM pg_index i " +
		"WHERE i.indrelid = $1::text::INT8::OID AND i.indisunique AND NOT i.indisprimary " +
		"AND NOT EXISTS (SELECT 1 FROM pg_constraint c WHERE c.conindid = i.indexrelid)"

	// postgresParent finds a partition's parent.
	postgresParent = "SELECT n.nspname, p.relname FROM pg_inherits i JOIN pg_class p ON p.oid = i.inhparent " +
		"JOIN pg_namespace n ON n.oid = p.relnamespace WHERE i.inhrelid = $1::text::INT8::OID"

	// postgresEnums lists enum types and their values.
	postgresEnums = "SELECT t.typname, string_agg(e.enumlabel, ',' ORDER BY e.enumsortorder) FROM pg_type t " +
		"JOIN pg_enum e ON e.enumtypid = t.oid JOIN pg_namespace n ON n.oid = t.typnamespace WHERE n.nspname IN ($1, $2) GROUP BY t.typname"

	// duckdbRelations lists DuckDB's tables and views with their stored definitions.
	duckdbRelations = "SELECT schema_name, table_name, sql, 'table' FROM duckdb_tables() WHERE database_name = current_database() " +
		"UNION ALL SELECT schema_name, view_name, sql, 'view' FROM duckdb_views() WHERE NOT internal AND database_name = current_database()"

	// cockroachHidden lists CockroachDB's hidden columns, such as rowid.
	cockroachHidden = "SELECT column_name FROM information_schema.columns WHERE table_schema = $1 AND table_name = $2 AND is_hidden = 'YES'"
)

var (
	// postgresRelationKinds maps a pg_class.relkind that is not a table to the model's kind.
	postgresRelationKinds = map[string]string{"v": fuzzschema.KindView, "m": fuzzschema.KindMaterialisedView}

	// clickhouseRelationKinds maps a ClickHouse view engine to the model's kind.
	clickhouseRelationKinds = map[string]string{"View": fuzzschema.KindView, "MaterializedView": fuzzschema.KindMaterialisedView}

	// postgresHistoryFamilies maps a PostgreSQL base type to the model's family.
	postgresHistoryFamilies = map[string]string{"bigint": fuzzschema.FamilyInteger, "integer": fuzzschema.FamilyInteger, "smallint": fuzzschema.FamilyInteger,
		"double precision": fuzzschema.FamilyFloat, "real": fuzzschema.FamilyFloat, "numeric": fuzzschema.FamilyDecimal,
		"boolean": fuzzschema.FamilyBoolean, "text": fuzzschema.FamilyText, "character varying": fuzzschema.FamilyText,
		"date": fuzzschema.FamilyDate, "timestamp without time zone": fuzzschema.FamilyTimestamp,
		"timestamp with time zone": fuzzschema.FamilyTimestampTZ, "uuid": fuzzschema.FamilyUUID, "jsonb": fuzzschema.FamilyJSON,
		"json": fuzzschema.FamilyJSON, "bytea": fuzzschema.FamilyBinary}

	// duckdbHistoryFamilies maps a DuckDB data type to the model's family.
	duckdbHistoryFamilies = map[string]string{"BIGINT": fuzzschema.FamilyInteger, "INTEGER": fuzzschema.FamilyInteger, "DOUBLE": fuzzschema.FamilyFloat,
		"BOOLEAN": fuzzschema.FamilyBoolean, "VARCHAR": fuzzschema.FamilyText, "DATE": fuzzschema.FamilyDate,
		"TIMESTAMP": fuzzschema.FamilyTimestamp, "TIMESTAMP WITH TIME ZONE": fuzzschema.FamilyTimestampTZ, "UUID": fuzzschema.FamilyUUID,
		"JSON": fuzzschema.FamilyJSON, "BLOB": fuzzschema.FamilyBinary}

	// clickhouseHistoryPrefixes maps ClickHouse type prefixes to the model's family, in the
	// order they are tried.
	clickhouseHistoryPrefixes = []struct{ prefix, family string }{{"Array(", fuzzschema.FamilyArray}, {"Enum8(", fuzzschema.FamilyEnum},
		{"Enum16(", fuzzschema.FamilyEnum}, {"Decimal(", fuzzschema.FamilyDecimal}, {"DateTime64(", fuzzschema.FamilyTimestampTZ},
		{"Date", fuzzschema.FamilyDate}, {"Int", fuzzschema.FamilyInteger}, {"Float", fuzzschema.FamilyFloat},
		{"Bool", fuzzschema.FamilyBoolean}, {"String", fuzzschema.FamilyText}, {"UUID", fuzzschema.FamilyUUID}}
)

// observePostgres reads PostgreSQL, TimescaleDB and CockroachDB's pg_catalog.
//
// Takes t (*testing.T) which reports failed queries.
// Takes namespace (string) which is the second schema's name.
func (live *historyLive) observePostgres(ctx context.Context, t *testing.T, namespace string) {
	t.Helper()
	current := live.scalar(ctx, t, "SELECT current_schema()")
	scope := func(schemaName string) string { return historyScope(schemaName, current, namespace) }
	for _, row := range live.rows(ctx, t, postgresRelations, current, namespace) {
		var schemaName, name, kind, oid string
		historyFields(t, row, &schemaName, &name, &kind, &oid)
		switch {
		case !strings.HasPrefix(name, historyPrefix):
		case kind == "S":
			live.schema.Sequences = append(live.schema.Sequences, historyQualify(scope(schemaName), name))
		default:
			live.schema.Relations = append(live.schema.Relations, live.postgresRelation(ctx, t, schemaName, name, kind, oid, scope))
		}
	}
	for _, row := range live.rows(ctx, t, postgresEnums, current, namespace) {
		var name, values string
		historyFields(t, row, &name, &values)
		if strings.HasPrefix(name, historyPrefix) {
			live.schema.Enums[name] = historySplit(values)
		}
	}
}

// postgresRelation reads one relation's columns and constraints.
//
// Takes t (*testing.T) which reports failed queries.
// Takes schemaName (string) which is the relation's schema.
// Takes name (string) which is its name.
// Takes kind (string) which is its pg_class.relkind.
// Takes oid (string) which identifies it.
// Takes scope (func(string) string) which maps schema names to model namespaces.
//
// Returns fuzzschema.Relation which is the observation.
func (live *historyLive) postgresRelation(ctx context.Context, t *testing.T, schemaName, name, kind, oid string,
	scope func(string) string) fuzzschema.Relation {
	t.Helper()
	relation := newHistoryRelation(scope(schemaName), name, cmp.Or(postgresRelationKinds[kind], fuzzschema.KindTable))
	var hidden []string
	if live.engine == engineCockroachDB {
		for _, column := range live.rows(ctx, t, cockroachHidden, schemaName, name) {
			hidden = append(hidden, column[0])
		}
	}
	for _, column := range live.rows(ctx, t, postgresColumns, oid) {
		var columnName, formatted, typeType, category, notNull, hasDefault, generation string
		historyFields(t, column, &columnName, &formatted, &typeType, &category, &notNull, &hasDefault, &generation)
		if slices.Contains(hidden, columnName) {
			continue
		}
		relation.Columns = append(relation.Columns, fuzzschema.Column{Name: columnName, Family: postgresHistoryFamily(formatted, typeType, category),
			Nullable: !historyBool(notNull), Default: historyBool(hasDefault) && generation == "", Generated: generation != ""})
	}
	if relation.Kind == fuzzschema.KindTable {
		live.postgresConstraints(ctx, t, &relation, oid, scope)
	}
	return relation
}

// postgresConstraints reads a table's constraints, unique indexes and partition parent.
//
// Takes t (*testing.T) which reports failed queries.
// Takes relation (*fuzzschema.Relation) which receives them.
// Takes oid (string) which identifies the table.
// Takes scope (func(string) string) which maps schema names to model namespaces.
func (live *historyLive) postgresConstraints(ctx context.Context, t *testing.T, relation *fuzzschema.Relation, oid string, scope func(string) string) {
	t.Helper()
	for _, row := range live.rows(ctx, t, postgresConstraints, oid) {
		var kind, name, columns, referencedSchema, referenced, references string
		historyFields(t, row, &kind, &name, &columns, &referencedSchema, &referenced, &references)
		switch kind {
		case "p":
			relation.PrimaryKey = historySplit(columns)
		case "u":
			relation.Uniques = append(relation.Uniques, historySplit(columns))
		case "c":
			relation.Checks = append(relation.Checks, name)
		default:
			relation.ForeignKeys = append(relation.ForeignKeys, fuzzschema.ForeignKey{Table: historyQualify(scope(referencedSchema), referenced),
				Columns: historySplit(columns), References: historySplit(references)})
		}
	}
	if live.engine == engineCockroachDB {
		return
	}
	for _, row := range live.rows(ctx, t, postgresUniqueIndexes, oid) {
		relation.Uniques = append(relation.Uniques, historySplit(row[0]))
	}
	for _, row := range live.rows(ctx, t, postgresParent, oid) {
		var parentSchema, parent string
		historyFields(t, row, &parentSchema, &parent)
		relation.PartitionOf = historyQualify(scope(parentSchema), parent)
	}
}

// observeDuckDB reads DuckDB's catalogue functions.
//
// Takes t (*testing.T) which reports failed queries.
// Takes namespace (string) which is the second schema's name.
func (live *historyLive) observeDuckDB(ctx context.Context, t *testing.T, namespace string) {
	t.Helper()
	scope := func(name string) string { return historyScope(name, "main", namespace) }
	for _, row := range live.rows(ctx, t, duckdbRelations) {
		var schemaName, name, definition, kind string
		historyFields(t, row, &schemaName, &name, &definition, &kind)
		if strings.HasPrefix(name, historyPrefix) {
			live.schema.Relations = append(live.schema.Relations, live.duckdbRelation(ctx, t, schemaName, name, definition, kind, scope))
		}
	}
	for _, row := range live.rows(ctx, t, "SELECT schema_name, sequence_name FROM duckdb_sequences() WHERE database_name = current_database()") {
		var schemaName, name string
		historyFields(t, row, &schemaName, &name)
		if strings.HasPrefix(name, historyPrefix) {
			live.schema.Sequences = append(live.schema.Sequences, historyQualify(scope(schemaName), name))
		}
	}
	for _, row := range live.rows(ctx, t, "SELECT type_name FROM duckdb_types() WHERE logical_type = 'ENUM' AND database_name = current_database()") {
		if name := row[0]; strings.HasPrefix(name, historyPrefix) {
			live.schema.Enums[name] = historySplit(live.scalar(ctx, t, fmt.Sprintf("SELECT array_to_string(enum_range(NULL::%s), ',')", name)))
		}
	}
}

// duckdbRelation reads one DuckDB relation's columns and constraints.
//
// Takes t (*testing.T) which reports failed queries.
// Takes schemaName (string) which is the relation's schema.
// Takes name (string) which is its name.
// Takes definition (string) which is its stored CREATE statement.
// Takes kind (string) which is table or view.
// Takes scope (func(string) string) which maps schema names to model namespaces.
//
// Returns fuzzschema.Relation which is the observation.
func (live *historyLive) duckdbRelation(ctx context.Context, t *testing.T, schemaName, name, definition, kind string,
	scope func(string) string) fuzzschema.Relation {
	t.Helper()
	relation := newHistoryRelation(scope(schemaName), name, kind)
	matches := historyGeneratedPattern.FindAllStringSubmatch(definition, -1)
	generated := make([]string, 0, len(matches))
	for _, match := range matches {
		generated = append(generated, match[1])
	}
	for _, column := range live.rows(ctx, t, "SELECT column_name, data_type, is_nullable, column_default IS NOT NULL FROM duckdb_columns() "+
		"WHERE database_name = current_database() AND schema_name = ? AND table_name = ? ORDER BY column_index", schemaName, name) {
		var columnName, dataType, nullable, hasDefault string
		historyFields(t, column, &columnName, &dataType, &nullable, &hasDefault)
		isGenerated := slices.Contains(generated, columnName)
		relation.Columns = append(relation.Columns, fuzzschema.Column{Name: columnName, Family: duckdbHistoryFamily(dataType),
			Nullable: historyBool(nullable), Default: historyBool(hasDefault) && !isGenerated, Generated: isGenerated})
	}
	if relation.Kind == fuzzschema.KindTable {
		live.duckdbConstraints(ctx, t, &relation, schemaName, scope)
	}
	return relation
}

// duckdbConstraints reads a DuckDB table's constraints and unique indexes.
//
// Takes t (*testing.T) which reports failed queries.
// Takes relation (*fuzzschema.Relation) which receives them.
// Takes schemaName (string) which is the table's schema.
// Takes scope (func(string) string) which maps schema names to model namespaces.
func (live *historyLive) duckdbConstraints(ctx context.Context, t *testing.T, relation *fuzzschema.Relation, schemaName string, scope func(string) string) {
	t.Helper()
	for _, row := range live.rows(ctx, t, "SELECT constraint_type, array_to_string(constraint_column_names, ','), COALESCE(referenced_table, ''), "+
		"array_to_string(referenced_column_names, ','), constraint_name FROM duckdb_constraints() "+
		"WHERE database_name = current_database() AND schema_name = ? AND table_name = ?", schemaName, relation.Name) {
		var kind, columns, referenced, references, name string
		historyFields(t, row, &kind, &columns, &referenced, &references, &name)
		switch kind {
		case "PRIMARY KEY":
			relation.PrimaryKey = historySplit(columns)
		case "UNIQUE":
			relation.Uniques = append(relation.Uniques, historySplit(columns))
		case "CHECK":
			relation.Checks = append(relation.Checks, name)
		case "FOREIGN KEY":
			relation.ForeignKeys = append(relation.ForeignKeys, fuzzschema.ForeignKey{Table: historyQualify(scope(schemaName), referenced),
				Columns: historySplit(columns), References: historySplit(references)})
		default:
		}
	}
	for _, row := range live.rows(ctx, t, "SELECT expressions FROM duckdb_indexes() WHERE is_unique AND database_name = current_database() "+
		"AND schema_name = ? AND table_name = ?", schemaName, relation.Name) {
		relation.Uniques = append(relation.Uniques, historySplit(strings.Trim(row[0], "[]")))
	}
}

// observeClickHouse reads ClickHouse's system tables.
//
// Takes t (*testing.T) which reports failed queries.
func (live *historyLive) observeClickHouse(ctx context.Context, t *testing.T) {
	t.Helper()
	for _, row := range live.rows(ctx, t, "SELECT name, engine, create_table_query FROM system.tables WHERE database = currentDatabase()") {
		var name, engine, definition string
		historyFields(t, row, &name, &engine, &definition)
		if !strings.HasPrefix(name, historyPrefix) {
			continue
		}
		kind := cmp.Or(clickhouseRelationKinds[engine], fuzzschema.KindTable)
		relation := newHistoryRelation("", name, kind)
		for _, column := range live.rows(ctx, t, "SELECT name, type, default_kind, is_in_primary_key FROM system.columns "+
			"WHERE database = currentDatabase() AND table = ? ORDER BY position", name) {
			var columnName, columnType, defaultKind, keyed string
			historyFields(t, column, &columnName, &columnType, &defaultKind, &keyed)
			base, nullable := strings.CutPrefix(columnType, "Nullable(")
			relation.Columns = append(relation.Columns, fuzzschema.Column{Name: columnName, Family: clickhouseHistoryFamily(base),
				Nullable: nullable, Default: defaultKind == "DEFAULT", Generated: defaultKind == "MATERIALIZED" || defaultKind == "ALIAS"})
			if kind == fuzzschema.KindTable && historyBool(keyed) {
				relation.PrimaryKey = append(relation.PrimaryKey, columnName)
			}
		}
		if kind == fuzzschema.KindTable {
			relation.Checks = historyChecks(definition)
		}
		live.schema.Relations = append(live.schema.Relations, relation)
	}
}

// postgresHistoryFamily maps a PostgreSQL type to the model's family.
//
// Takes formatted (string) which is format_type's spelling.
// Takes typeType (string) which is pg_type.typtype.
// Takes category (string) which is pg_type.typcategory.
//
// Returns string which is the family, or the type of an unmapped one.
func postgresHistoryFamily(formatted, typeType, category string) string {
	if typeType == "e" {
		return fuzzschema.FamilyEnum
	}
	if category == "A" || strings.HasSuffix(formatted, "[]") {
		return fuzzschema.FamilyArray
	}
	base, _, _ := strings.Cut(formatted, "(")
	if family, ok := postgresHistoryFamilies[strings.TrimSpace(base)]; ok {
		return family
	}
	return "other:" + formatted
}

// duckdbHistoryFamily maps a DuckDB type to the model's family.
//
// Takes dataType (string) which is duckdb_columns' data_type.
//
// Returns string which is the family, or the type of an unmapped one.
func duckdbHistoryFamily(dataType string) string {
	switch {
	case strings.HasSuffix(dataType, "[]"):
		return fuzzschema.FamilyArray
	case strings.HasPrefix(dataType, "ENUM("):
		return fuzzschema.FamilyEnum
	case strings.HasPrefix(dataType, "DECIMAL("):
		return fuzzschema.FamilyDecimal
	default:
	}
	if family, ok := duckdbHistoryFamilies[dataType]; ok {
		return family
	}
	return "other:" + dataType
}

// clickhouseHistoryFamily maps a ClickHouse type, without Nullable, to the model's
// family.
//
// Takes columnType (string) which is the type.
//
// Returns string which is the family, or the type of an unmapped one.
func clickhouseHistoryFamily(columnType string) string {
	for _, candidate := range clickhouseHistoryPrefixes {
		if strings.HasPrefix(columnType, candidate.prefix) {
			return candidate.family
		}
	}
	return "other:" + columnType
}
