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

package dialecttest_test

import (
	"io/fs"
	"maps"
	"os"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/clickhouse"
	clickhouse_lexical "seagull.sh/seagull/internal/dialect/clickhouse/lexical"
	"seagull.sh/seagull/internal/dialect/cockroachdb"
	"seagull.sh/seagull/internal/dialect/dialecttest"
	"seagull.sh/seagull/internal/dialect/duckdb"
	duckdb_lexical "seagull.sh/seagull/internal/dialect/duckdb/lexical"
	"seagull.sh/seagull/internal/dialect/mariadb"
	"seagull.sh/seagull/internal/dialect/martian"
	"seagull.sh/seagull/internal/dialect/mysql"
	mysql_lexical "seagull.sh/seagull/internal/dialect/mysql/lexical"
	"seagull.sh/seagull/internal/dialect/postgres"
	postgres_lexical "seagull.sh/seagull/internal/dialect/postgres/lexical"
	"seagull.sh/seagull/internal/dialect/sqlite"
	sqlite_lexical "seagull.sh/seagull/internal/dialect/sqlite/lexical"
	"seagull.sh/seagull/internal/dialect/timescaledb"
	timescaledb_lexical "seagull.sh/seagull/internal/dialect/timescaledb/lexical"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/sqlmodel"
)

const userFunctionName = "seagull_user_function"

const starViews = "CREATE VIEW star_view AS SELECT * FROM items;\n" +
	"CREATE VIEW qualified_view AS SELECT items.* FROM items;\n" +
	"CREATE VIEW named_view (item_id, item_name) AS SELECT * FROM items;"

const (
	martianSelectsFromATable  = "Martian's SELECT always reads a table, so it has no SELECT without FROM"
	martianHasNoCoalesce      = "Martian has no COALESCE function"
	martianHasNoBetween       = "Martian has no BETWEEN"
	martianHasNoJoins         = "Martian's SELECT reads one table, with no joins"
	martianHasNoNestedQueries = "Martian has no WITH, subqueries, derived tables or set operations"
)

var postgresUserOptions = []postgres.Option{
	postgres.WithExtraFunctions(func(builder *postgres.FunctionCatalogueBuilder) {
		builder.NeverNull(userFunctionName, nil, userFunctionType())
	}),
	postgres.WithTypeNormaliserHook(declineEveryType),
}

var postgresBracketedValues = []string{"ARRAY[1, 2]", "ARRAY[1, 2] || ARRAY[3, 4]", "ARRAY[(1), 2]"}

var postgresSpanStatements = []string{
	"SELECT DISTINCT ON (i.id) i.id, i.name AS label, count(*) total, i.* FROM items i JOIN items j ON j.id = i.id " +
		"WHERE i.id > $1 AND i.name NOT IN ($2) AND i.id IN ($3, $4) GROUP BY i.id, i.name HAVING count(*) > $5 " +
		"ORDER BY i.id DESC NULLS LAST LIMIT $6 OFFSET $7;",
	"SELECT ALL id FROM items WHERE id = :id ORDER BY id FETCH FIRST 1 ROWS ONLY FOR UPDATE SKIP LOCKED",
	"SELECT id FROM items WHERE id = $1 FOR SHARE OF items NOWAIT LIMIT 1",
	"SELECT id FROM items WHERE id = $1 UNION ALL SELECT id FROM items WHERE name = $2 ORDER BY id LIMIT $3;",
	"WITH recent AS (SELECT id, name FROM items WHERE id > $1) SELECT r.*, r.name FROM recent r WHERE r.name LIKE $2",
	"INSERT INTO public.items AS t (id, name) VALUES ($1, $2), ($3, lower($4)) ON CONFLICT (id) DO UPDATE " +
		"SET name = EXCLUDED.name RETURNING id, name AS label;",
	"INSERT INTO items (id, name) SELECT id, name FROM items WHERE id = $1",
	"UPDATE items SET name = $1, (id, name) = ($2, upper(name)) WHERE id = $3 RETURNING *;",
	"DELETE FROM items WHERE id IN (SELECT id FROM items WHERE name = $1) RETURNING id",
	"VALUES ($1, 2), (3, $2) ORDER BY 1 LIMIT 1",
}

var postgresAcceptedForms = []string{
	"CREATE FUNCTION touch() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RETURN NEW; END$$;\n" +
		"CREATE OR REPLACE TRIGGER items_touch BEFORE UPDATE ON items FOR EACH ROW EXECUTE FUNCTION touch();\n" +
		"CREATE OR REPLACE TRIGGER items_touch AFTER UPDATE ON items FOR EACH ROW EXECUTE FUNCTION touch();\n" +
		"CREATE CONSTRAINT TRIGGER items_check AFTER UPDATE ON items DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION touch();",
	"CREATE TABLE app.public.accounts (id integer PRIMARY KEY, name text);\n" +
		"ALTER TABLE app.public.accounts ADD COLUMN extra integer;\n" +
		"CREATE INDEX accounts_name ON app.public.accounts (name);\n" +
		"CREATE VIEW app.public.account_names AS SELECT name FROM app.public.accounts;\n" +
		"COMMENT ON COLUMN app.public.accounts.name IS 'a name';\n" +
		"CREATE SEQUENCE app.public.counter;\n" +
		"DROP VIEW app.public.account_names;",
	"CREATE SCHEMA shop;\n" +
		"SET search_path TO shop, public;\n" +
		"CREATE TABLE orders (id integer PRIMARY KEY, item_id integer REFERENCES items (id));\n" +
		"SET LOCAL search_path = 'shop';\n" +
		"RESET search_path;\n" +
		"SET SCHEMA 'public';\n" +
		"SET search_path TO DEFAULT;",
	"CREATE TABLE readings (id integer, taken_on date NOT NULL, PRIMARY KEY (id, taken_on)) PARTITION BY RANGE (taken_on);\n" +
		"CREATE TABLE readings_2026 PARTITION OF readings (taken_on WITH OPTIONS NOT NULL, CONSTRAINT positive CHECK (id > 0)) " +
		"FOR VALUES FROM ('2026-01-01') TO ('2027-01-01');\n" +
		"CREATE TABLE readings_2027 (LIKE readings INCLUDING ALL);\n" +
		"ALTER TABLE readings ATTACH PARTITION readings_2027 FOR VALUES FROM ('2027-01-01') TO ('2028-01-01');\n" +
		"ALTER TABLE readings DETACH PARTITION readings_2026;",
}

var mysqlUserOptions = []mysql.Option{
	mysql.WithExtraFunctions(func(builder *mysql.FunctionCatalogueBuilder) {
		builder.NeverNull(userFunctionName, nil, userFunctionType())
	}),
	mysql.WithTypeNormaliserHook(declineEveryType),
}

var mysqlSpanStatements = []string{
	"SELECT DISTINCT SQL_NO_CACHE i.id, i.name AS label, count(*) total, i.* FROM items i JOIN items j ON j.id = i.id " +
		"WHERE i.id > ? AND i.name NOT IN (?) AND i.id IN (?, ?) GROUP BY i.id, i.name WITH ROLLUP " +
		"HAVING count(*) > ? ORDER BY i.id DESC LIMIT ?, ? FOR UPDATE SKIP LOCKED;",
	"SELECT SQL_CALC_FOUND_ROWS id ident FROM items WHERE id = :id ORDER BY id LIMIT 1 OFFSET ? LOCK IN SHARE MODE",
	"SELECT ALL id FROM items WHERE id = ? FOR SHARE NOWAIT",
	"SELECT id FROM items WHERE id = ? UNION ALL SELECT id FROM items WHERE name = ? ORDER BY id LIMIT ?;",
	"WITH recent AS (SELECT id, name FROM items WHERE id > ?) SELECT r.*, r.name FROM recent r WHERE r.name LIKE ?",
	"INSERT INTO shop.items (id, name) VALUES (?, ?), (?, lower(?)) ON DUPLICATE KEY UPDATE name = VALUES(name);",
	"INSERT INTO items (id, name) SELECT id, name FROM items WHERE id = ?",
	"REPLACE INTO items (id, name) VALUES (?, ?) RETURNING id, name AS label",
	"UPDATE items i JOIN tags t ON t.item_id = i.id SET i.name = ?, t.label = upper(?) WHERE i.id = ? ORDER BY i.id LIMIT 1;",
	"DELETE FROM items WHERE id IN (SELECT id FROM items WHERE name = ?) RETURNING id",
	"DELETE i FROM items i JOIN tags t ON t.item_id = i.id WHERE t.tag = ?",
}

var sqliteSpanStatements = []string{
	"SELECT DISTINCT i.id, i.name AS label, count(*) total, i.* FROM items i JOIN items j ON j.id = i.id " +
		"WHERE i.id > ?1 AND i.name NOT IN (?2) AND i.id IN (?3, ?4) GROUP BY i.id, i.name HAVING count(*) > :minimum " +
		"ORDER BY i.id DESC NULLS LAST LIMIT @size OFFSET $skip;",
	"SELECT ALL id FROM items WHERE id = ?1 ORDER BY id LIMIT ?2, ?3",
	"SELECT id FROM items WHERE id = ?1 UNION ALL SELECT id FROM items WHERE name = ?2 ORDER BY id LIMIT ?3;",
	"WITH recent AS (SELECT id, name FROM items WHERE id > ?1) SELECT r.*, r.name FROM recent r WHERE r.name LIKE ?2",
	"INSERT INTO main.items AS t (id, name) VALUES (?1, ?2), (?3, lower(?4)) ON CONFLICT (id) DO UPDATE " +
		"SET name = excluded.name RETURNING id, name AS label;",
	"INSERT OR REPLACE INTO items (id, name) SELECT id, name FROM items WHERE id = :id",
	"UPDATE items SET name = ?1, (id, name) = (?2, upper(name)) WHERE id = ?3 RETURNING *;",
	"DELETE FROM items WHERE id IN (SELECT id FROM items WHERE name = ?1) RETURNING id",
	"VALUES (?1, 2), (3, ?2) UNION ALL SELECT id, ?3 FROM items ORDER BY 1 LIMIT 1",
}

var sqliteFixture = dialecttest.Fixture{
	KnownFailures: knownFailures(),
	Spans: &dialecttest.SpanFixture{
		Statements: sqliteSpanStatements,
		Corpora: []fs.FS{
			os.DirFS("../sqlite/testdata"),
			os.DirFS("../../../tests/integration/sqlite/testdata"),
			os.DirFS("../../../tests/integration/sqlite_cgo/testdata"),
			os.DirFS("../../../examples/sqlite"),
		},
	},
	DeclaredParameters: sqliteDeclaredParameters(),
	NewDialect:         func() dialect.DialectPort { return sqlite.New() },
	Replay:             sqliteReplay(),
	BaseMigration:      "CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT);",
	UnboundStatement:   "ATTACH DATABASE ? AS archive",
	ParamQuery: "-- sgl.query(name: GetItem, command: one)\n" +
		"SELECT id, name FROM items WHERE id = ?;",
	SplitterMigration: "CREATE TABLE alpha (id INTEGER);\nCREATE TABLE beta (id INTEGER);",
	QuotedMigration:   `CREATE TABLE "weird name" (id INTEGER);`,
	ReservedWordMigration: `CREATE TABLE reserved_items (id INTEGER PRIMARY KEY, "check" INTEGER NOT NULL, ` +
		`"constraint" TEXT NOT NULL, "index" INTEGER NOT NULL, "primary" TEXT NOT NULL, "group" TEXT NOT NULL, ` +
		`"where" INTEGER NOT NULL);`,
	ReservedWordQuery: "-- sgl.query(name: GetReserved, command: many)\n" +
		`SELECT id, "group" FROM reserved_items WHERE "where" = ?;`,
	TypedMigration: "CREATE TABLE typed_items (id INTEGER PRIMARY KEY AUTOINCREMENT, " +
		"name VARCHAR(255) COLLATE NOCASE NOT NULL, price NUMERIC(10, 2) DEFAULT 0, data BLOB, " +
		"size UNSIGNED BIG INT NOT NULL, label VARYING CHARACTER(255), CONSTRAINT typed_name UNIQUE (name));",
	TypedColumns: []string{"id", "name", "price", "data", "size", "label"},
	Tokeniser:    nil,
	RowLocks:     nil,
	PlaceholderQuery: "-- sgl.query(name: CountPlaceholders, command: many)\n" +
		"SELECT .5 AS half, id FROM items WHERE id = ? AND name = ?;",
	PlaceholderCount: 2,
	CTEColumnLists:   true,
	RowComparisons:   true,
	ValuesArms:       true,
	NotInQuery: "-- sgl.query(name: ExcludeItems, command: many)\n" +
		"SELECT id FROM items WHERE id NOT IN (?);",
	NewDialectWithExtraType: func(name string, sqlType sqlmodel.SQLType) dialect.DialectPort {
		return sqlite.New(sqlite.WithExtraTypes(map[string]sqlmodel.SQLType{name: sqlType}))
	},
}

var duckdbBracketedValues = []string{"[1, 2]", "[[1, 2], [3]][1]", "{'a': [1, 2], 'b': 3}.a", "map_values(MAP {1: 1, 2: 2})", "[1, 2] || [3]"}

var duckdbSpanStatements = []string{
	"SELECT DISTINCT ON (i.id) i.id, i.name AS label, count(*) total, row_number() OVER w AS rank, i.* " +
		"FROM items i JOIN items j ON j.id = i.id WHERE i.id > $1 AND i.name NOT IN ($2) AND i.id IN ($3, $4) " +
		"GROUP BY i.id, i.name HAVING count(*) > $5 WINDOW w AS (ORDER BY i.id) QUALIFY rank = 1 " +
		"ORDER BY i.id DESC NULLS LAST LIMIT $6 OFFSET $7;",
	"SELECT ALL id FROM items WHERE id = :id ORDER BY ALL LIMIT 10%",
	"SELECT name, count(*) FROM items GROUP BY ALL USING SAMPLE 10% ORDER BY ALL",
	"SELECT id FROM items WHERE len(list_filter(tags, x -> x > $1)) > 0 AND address.city = $2",
	"FROM items SELECT id WHERE id = $1",
	"SELECT id FROM items WHERE id = $1 UNION ALL SELECT id FROM items WHERE name = $2 ORDER BY id LIMIT $3;",
	"WITH recent AS (SELECT id, name FROM items WHERE id > $1) SELECT r.*, r.name FROM recent r WHERE r.name LIKE $2",
	"INSERT INTO main.items AS t (id, name) VALUES ($1, $2), ($3, lower($4)) ON CONFLICT (id) DO UPDATE " +
		"SET name = EXCLUDED.name RETURNING id, name AS label;",
	"INSERT INTO items (id, name) SELECT id, name FROM items WHERE id = $1",
	"UPDATE items SET name = $1, (id, name) = ($2, upper(name)) WHERE id = $3 RETURNING *;",
	"DELETE FROM items WHERE id IN (SELECT id FROM items WHERE name = $1) RETURNING id",
	"SELECT add_default(1, b := $1) AS total FROM items",
	"VALUES ($1, 2), (3, $2) ORDER BY 1 LIMIT 1",
}

var duckdbFixture = dialecttest.Fixture{
	KnownFailures: knownFailures(),
	Spans: &dialecttest.SpanFixture{
		Statements: duckdbSpanStatements,
		Corpora: []fs.FS{
			os.DirFS("../duckdb/testdata"),
			os.DirFS("../../../tests/integration/duckdb/testdata"),
			os.DirFS("../../../examples/duckdb"),
		},
	},
	DeclaredParameters: duckdbDeclaredParameters(),
	NewDialect:         func() dialect.DialectPort { return duckdb.New() },
	BaseMigration:      "CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT);",
	GroupingQuery:      "SELECT id, count(*) AS total FROM items GROUP BY ROLLUP (id);",
	Elements: &dialecttest.ElementFixture{Migration: "CREATE TABLE samples (id INTEGER PRIMARY KEY, readings INTEGER[] NOT NULL);",
		Aggregate: "list(id)", Parameter: "SELECT id FROM samples WHERE readings = $1", Quantified: "", Literals: duckdbBracketedValues},
	Replay: duckdbReplay(),
	ParamQuery: "-- sgl.query(name: GetItem, command: one)\n" +
		"SELECT id, name FROM items WHERE id = $1;",
	SplitterMigration: "CREATE TABLE alpha (id INTEGER);\nCREATE TABLE beta (id INTEGER);",
	QuotedMigration:   `CREATE TABLE "weird name" (id INTEGER);`,
	ReservedWordMigration: `CREATE TABLE reserved_items (id INTEGER PRIMARY KEY, "check" INTEGER NOT NULL, ` +
		`"constraint" TEXT NOT NULL, "index" INTEGER NOT NULL, "primary" TEXT NOT NULL, "group" TEXT NOT NULL, ` +
		`"where" INTEGER NOT NULL);`,
	ReservedWordQuery: "-- sgl.query(name: GetReserved, command: many)\n" +
		`SELECT id, "group" FROM reserved_items WHERE "where" = $1;`,
	TypedMigration: "CREATE TABLE typed_items (id INTEGER PRIMARY KEY, tags INTEGER[] NOT NULL, " +
		"point STRUCT(x DOUBLE, y DOUBLE), attributes MAP(VARCHAR, INTEGER), " +
		"created_at TIMESTAMP WITH TIME ZONE NOT NULL);",
	TypedColumns: []string{"id", "tags", "point", "attributes", "created_at"},
	Tokeniser:    nil,
	RowLocks:     nil,
	PlaceholderQuery: "-- sgl.query(name: CountPlaceholders, command: many)\n" +
		"SELECT .5 AS half, id FROM items WHERE id = $1 AND name = $2;",
	PlaceholderCount:   2,
	DerivedColumnLists: true,
	CTEColumnLists:     true,
	TableColumnLists:   true,
	ValuesArms:         true,
	UnboundStatement:   "SET threads = $1",
	NotInQuery: "-- sgl.query(name: ExcludeItems, command: many)\n" +
		"SELECT id FROM items WHERE id NOT IN ($1);",
	NewDialectWithExtraType: func(name string, sqlType sqlmodel.SQLType) dialect.DialectPort {
		return duckdb.New(duckdb.WithExtraTypes(map[string]sqlmodel.SQLType{name: sqlType}))
	},
}

var clickhouseBracketedValues = []string{"[1, 2]", "[[1, 2], [3]][1]"}

var clickhouseSpanStatements = []string{
	"SELECT DISTINCT id, name AS label FROM items FINAL SAMPLE 0.1 ARRAY JOIN tags AS tag " +
		"PREWHERE id > {min:UInt64} WHERE name = {name:String} AND id NOT IN ({excluded:UInt64}) " +
		"GROUP BY id, name WITH TOTALS HAVING count() > 1 QUALIFY rn = 1 ORDER BY id WITH FILL " +
		"LIMIT 1 BY id LIMIT {limit:UInt64} OFFSET {offset:UInt64} SETTINGS max_threads = 1 FORMAT JSON;",
	"SELECT id FROM items WHERE arrayExists(x -> x > {min:UInt32}, tags) AND basket.fish = 'a' LIMIT 5, 10",
	"SELECT id FROM items WHERE id = {a:UInt64} UNION ALL SELECT id FROM items WHERE name = {b:String} ORDER BY id LIMIT 5",
	"SELECT id FROM items UNION ALL SELECT id FROM items INTERSECT SELECT id FROM items WHERE id = {a:UInt64}",
	"SELECT id FROM (SELECT id FROM items UNION ALL SELECT id FROM items WHERE id = {a:UInt64}) ORDER BY id",
	"WITH found AS (SELECT id FROM items UNION ALL SELECT id FROM items WHERE name = {b:String}) SELECT id FROM found",
	"WITH recent AS (SELECT id, name FROM items WHERE id > {min:UInt64}) SELECT r.*, r.name FROM recent r " +
		"WHERE r.name LIKE {pattern:String}",
	"WITH now() - 7 AS bound SELECT id FROM items WHERE created > bound",
	"INSERT INTO default.items (id, name) VALUES ({a:UInt64}, {b:String}), (2, lower({c:String}))",
	"INSERT INTO items (id, name) SELECT id, name FROM items WHERE id = {id:UInt64}",
	"ALTER TABLE items UPDATE name = upper(name) WHERE id = {id:UInt64}",
	"ALTER TABLE items DELETE WHERE id IN ({ids:Array(UInt64)})",
	"UPDATE items SET name = {name:String} WHERE id = {id:UInt64}",
	"DELETE FROM items WHERE id = {id:UInt64}",
}

var clickhouseFixture = dialecttest.Fixture{
	KnownFailures: knownFailures(),
	Spans: &dialecttest.SpanFixture{
		Statements: clickhouseSpanStatements,
		Corpora: []fs.FS{
			os.DirFS("../clickhouse/testdata"),
			os.DirFS("../../../tests/integration/clickhouse/testdata"),
			os.DirFS("../../../examples/clickhouse"),
		},
	},
	DeclaredParameters: clickhouseDeclaredParameters(),
	NewDialect:         func() dialect.DialectPort { return clickhouse.New() },
	BaseMigration:      "CREATE TABLE items (id Int32, name String) ENGINE = MergeTree ORDER BY id;",
	UnboundStatement:   "SET max_threads = {threads:UInt8}",
	GroupingQuery:      "SELECT id, count(*) AS total FROM items GROUP BY id WITH ROLLUP;",
	Elements: &dialecttest.ElementFixture{Migration: "CREATE TABLE samples (id Int32, readings Array(Int32)) ENGINE = MergeTree ORDER BY id;",
		Aggregate: "groupArray(id)", Parameter: "SELECT id FROM samples WHERE readings = {readings:Array(Int32)}", Quantified: "",
		Literals: clickhouseBracketedValues},
	Replay: clickhouseReplay(),
	ParamQuery: "-- sgl.query(name: GetItem, command: one)\n" +
		"SELECT id, name FROM items WHERE id = {id:Int32};",
	SplitterMigration: "CREATE TABLE alpha (id Int32) ENGINE = MergeTree ORDER BY id;\n" +
		"CREATE TABLE beta (id Int32) ENGINE = MergeTree ORDER BY id;",
	QuotedMigration: "CREATE TABLE `weird name` (id Int32) ENGINE = MergeTree ORDER BY id;",
	ReservedWordMigration: "CREATE TABLE reserved_items (id Int32, `check` Int32, `constraint` String, `index` Int32, " +
		"`primary` String, `group` String, `where` Int32) ENGINE = MergeTree ORDER BY id;",
	ReservedWordQuery: "-- sgl.query(name: GetReserved, command: many)\n" +
		"SELECT id, `group` FROM reserved_items WHERE `where` = {where_value:Int32};",
	TypedMigration: "CREATE TABLE typed_items (id UInt64, name LowCardinality(String), tags Array(String), " +
		"attributes Map(String, UInt32), location Tuple(lat Float64, lon Float64), " +
		"note Nullable(String) CODEC(ZSTD(1)), INDEX name_index name TYPE bloom_filter GRANULARITY 1, " +
		"CONSTRAINT id_positive CHECK id > 0) ENGINE = MergeTree ORDER BY id;",
	TypedColumns: []string{"id", "name", "tags", "attributes", "location", "note"},
	Tokeniser:    nil,
	RowLocks:     nil,
	PlaceholderQuery: "-- sgl.query(name: CountPlaceholders, command: many)\n" +
		"SELECT .5 AS half, id FROM items WHERE id = {id:Int32} AND name = {name:String};",
	PlaceholderCount: 2,
	NotInQuery: "-- sgl.query(name: ExcludeItems, command: many)\n" +
		"SELECT id FROM items WHERE id NOT IN ({id:Int32});",
	NewDialectWithExtraType: func(name string, sqlType sqlmodel.SQLType) dialect.DialectPort {
		return clickhouse.New(clickhouse.WithExtraTypes(map[string]sqlmodel.SQLType{name: sqlType}))
	},
}

func knownFailures(reasons ...map[string]string) map[string]string {
	merged := make(map[string]string)
	for _, reason := range reasons {
		maps.Copy(merged, reason)
	}
	return merged
}

func userFunctionType() sqlmodel.SQLType {
	return sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")
}

func declineEveryType(string, []int) *sqlmodel.SQLType {
	return nil
}

func postgresDeclaredParameters(differingType string) *dialecttest.DeclaredParameterFixture {
	return &dialecttest.DeclaredParameterFixture{
		Migration: "CREATE TABLE declared_items (value integer, label text);",
		Restating: "-- sgl.query(name: CompareRestated, command: many)\n" +
			"SELECT value FROM declared_items WHERE value = $1::integer OR value <> CAST($2 AS integer) " +
			"OR $3::int = value OR CAST($4 AS integer) < value OR value = $5 OR $6 = value;\n" +
			"-- sgl.query(name: ListRestated, command: many)\n" +
			"SELECT value FROM declared_items WHERE value IN ($1::integer, CAST($2 AS integer), $3) " +
			"OR value NOT IN ($4::integer);\n" +
			"-- sgl.query(name: BoundRestated, command: many)\n" +
			"SELECT value FROM declared_items WHERE value BETWEEN $1::integer AND CAST($2 AS integer) " +
			"OR value BETWEEN $3 AND $4;\n" +
			"-- sgl.query(name: InsertRestated, command: exec)\n" +
			"INSERT INTO declared_items (value) VALUES ($1::integer), (CAST($2 AS integer)), ($3);\n" +
			"-- sgl.query(name: UpdateRestated, command: exec)\n" +
			"UPDATE declared_items SET value = CAST($1 AS integer) WHERE value = $2::integer;\n" +
			"-- sgl.query(name: NestRestated, command: many)\n" +
			"WITH found AS (SELECT value FROM declared_items WHERE value = $1::integer) SELECT value FROM found;\n",
		Differing: "-- sgl.query(name: CompareDiffering, command: many)\n" +
			"SELECT value FROM declared_items WHERE value = $1::" + differingType + " OR value <> CAST($2 AS " + differingType + ") " +
			"OR $3::" + differingType + " = value;\n" +
			"-- sgl.query(name: ListDiffering, command: many)\n" +
			"SELECT value FROM declared_items WHERE value IN ($1::" + differingType + ", CAST($2 AS " + differingType + "));\n" +
			"-- sgl.query(name: BoundDiffering, command: many)\n" +
			"SELECT value FROM declared_items WHERE value BETWEEN $1::" + differingType + " AND CAST($2 AS " + differingType + ");\n" +
			"-- sgl.query(name: InsertDiffering, command: exec)\n" +
			"INSERT INTO declared_items (value) VALUES ($1::" + differingType + "), (CAST($2 AS " + differingType + "));\n" +
			"-- sgl.query(name: UpdateDiffering, command: exec)\n" +
			"UPDATE declared_items SET value = CAST($1 AS " + differingType + ") WHERE value = $2::" + differingType + ";\n",
		Patterns: "-- sgl.query(name: MatchPatterns, command: many)\n" +
			"SELECT value FROM declared_items WHERE label LIKE $1 OR label ILIKE $2::text OR label NOT LIKE CAST($3 AS text) " +
			"OR label SIMILAR TO $4 OR label ~ $5::text OR label !~* $6;\n",
		ProjectionViews: "CREATE VIEW declared_view AS SELECT value, CAST(value AS text) AS other FROM declared_items;",
		Projections: "-- sgl.query(name: ProjectAsIs, command: many)\n" +
			"SELECT value, value::integer AS restated, CAST(value AS integer) AS aliased FROM declared_items;\n" +
			"-- sgl.query(name: ProjectThroughView, command: many)\n" +
			"SELECT value FROM declared_view;\n",
		CastProjections: "-- sgl.query(name: ProjectCast, command: many)\n" +
			"SELECT value::text AS as_text, CAST(value AS numeric) AS exact, value::smallint AS narrowed FROM declared_items;\n" +
			"-- sgl.query(name: ProjectCastThroughDerived, command: many)\n" +
			"WITH found AS (SELECT CAST(value AS text) AS other FROM declared_items) SELECT found.other, derived.other AS derived_other " +
			"FROM found, (SELECT value::text AS other FROM declared_items) AS derived;\n" +
			"-- sgl.query(name: ProjectCastThroughView, command: many)\n" +
			"SELECT other FROM declared_view;\n",
	}
}

func postgresReplay() *dialecttest.ReplayFixture {
	return &dialecttest.ReplayFixture{
		Idempotent: "CREATE TABLE IF NOT EXISTS items (id bigint PRIMARY KEY, label text, extra integer);\n" +
			"ALTER TABLE items ADD COLUMN IF NOT EXISTS name text;\n" +
			"ALTER TABLE IF EXISTS missing_items ADD COLUMN extra integer;\n" +
			"ALTER TABLE IF EXISTS missing_items RENAME TO other_items;\n" +
			"CREATE TYPE mood AS ENUM ('calm', 'busy');\n" +
			"ALTER TYPE mood ADD VALUE IF NOT EXISTS 'calm';\n" +
			"CREATE INDEX IF NOT EXISTS items_name ON items (name);\n" +
			"CREATE INDEX IF NOT EXISTS items_name ON items (name);\n" +
			"CREATE SEQUENCE IF NOT EXISTS counter;\n" +
			"CREATE SEQUENCE IF NOT EXISTS counter;",
		KeyedTables: "CREATE TABLE pairs (left_id integer, right_id integer, note text, PRIMARY KEY (left_id, right_id));\n" +
			"CREATE TABLE later_pairs (left_id integer, right_id integer, note text);\n" +
			"ALTER TABLE later_pairs ADD PRIMARY KEY (left_id, right_id);\n" +
			"CREATE TABLE named_pairs (left_id integer, right_id integer, note text, " +
			"CONSTRAINT named_pairs_key PRIMARY KEY (left_id, right_id));",
		Renames: "CREATE SCHEMA archive;\n" +
			"ALTER TABLE items RENAME TO renamed_items;\n" +
			"CREATE VIEW items_view AS SELECT id, name FROM renamed_items;\n" +
			"ALTER VIEW items_view RENAME TO renamed_view;\n" +
			"CREATE MATERIALIZED VIEW items_snapshot AS SELECT id, name FROM renamed_items;\n" +
			"ALTER MATERIALIZED VIEW items_snapshot RENAME TO renamed_snapshot;\n" +
			"ALTER MATERIALIZED VIEW renamed_snapshot SET SCHEMA archive;\n" +
			"ALTER TABLE renamed_items SET SCHEMA archive;\n" +
			"CREATE TYPE mood AS ENUM ('calm');\n" +
			"ALTER TYPE mood RENAME TO feeling;\n" +
			"ALTER TYPE feeling SET SCHEMA archive;\n" +
			"ALTER TYPE archive.feeling ADD VALUE 'busy';",
		RenamedRelations: []string{"renamed_items", "renamed_view", "renamed_snapshot"},
		StarViews:        starViews,
		ColumnAddition:   "ALTER TABLE items ADD COLUMN extra integer;",
		DerivedRelations: "CREATE TABLE readings (id integer, taken_on date NOT NULL, PRIMARY KEY (id, taken_on)) " +
			"PARTITION BY RANGE (taken_on);\n" +
			"CREATE TABLE readings_2026 PARTITION OF readings FOR VALUES FROM ('2026-01-01') TO ('2027-01-01') PARTITION BY RANGE (id);\n" +
			"CREATE TABLE readings_2026_low PARTITION OF readings_2026 FOR VALUES FROM (0) TO (100);\n" +
			"ALTER TABLE readings ADD COLUMN note text;\n" +
			"CREATE SEQUENCE counter;\n" +
			"CREATE SCHEMA shop;\n" +
			"SET search_path TO shop;\n" +
			"CREATE TABLE orders (id integer PRIMARY KEY);\n" +
			"CREATE SEQUENCE order_numbers;",
		DerivedQueries: "-- sgl.query(name: ReadPartition, command: many)\n" +
			"SELECT id, taken_on, note FROM readings_2026 WHERE id = $1;\n" +
			"-- sgl.query(name: ReadSubPartition, command: many)\n" +
			"SELECT * FROM readings_2026_low;\n" +
			"-- sgl.query(name: ReadSequence, command: one)\n" +
			"SELECT last_value, is_called FROM counter;\n" +
			"-- sgl.query(name: ReadSchemaSequence, command: one)\n" +
			"SELECT s.last_value FROM shop.order_numbers AS s;\n" +
			"-- sgl.query(name: ReadSchemaTable, command: many)\n" +
			"SELECT id FROM shop.orders;",
		AcceptedForms: postgresAcceptedForms,
	}
}

func cockroachDBReplay() *dialecttest.ReplayFixture {
	replay := *postgresReplay()
	replay.DerivedRelations = "CREATE SEQUENCE counter;\nCREATE TABLE notes (body STRING);"
	replay.DerivedQueries = "-- sgl.query(name: ReadSequence, command: one)\n" +
		"SELECT last_value, log_cnt, is_called FROM counter;\n" +
		"-- sgl.query(name: ReadRowID, command: many)\n" +
		"SELECT rowid, body FROM notes;"
	replay.AcceptedForms = []string{
		"CREATE TABLE app.public.accounts (id INT8 PRIMARY KEY, name STRING);\n" +
			"ALTER TABLE app.public.accounts ADD COLUMN extra INT8;",
		"CREATE SCHEMA shop;\nSET search_path TO shop, public;\nCREATE TABLE orders (id INT8 PRIMARY KEY);\nRESET search_path;",
		"CREATE TABLE notes (body STRING NOT NULL, kind INT8);\nALTER TABLE notes ALTER PRIMARY KEY USING COLUMNS (body);",
		"CREATE TABLE tags (label STRING NOT NULL);\nALTER TABLE tags ADD PRIMARY KEY (label);",
		"CREATE TABLE marks (rowid INT8, label STRING);",
		"CREATE TABLE t (id INT8 PRIMARY KEY, a STRING, b STRING AS (lower(a)) STORED, INDEX t_a_idx (a) STORING (b), " +
			"UNIQUE INDEX t_b_key (b), FAMILY f1 (id, a, b));",
	}
	return &replay
}

func mysqlReplay() *dialecttest.ReplayFixture {
	return &dialecttest.ReplayFixture{
		Idempotent: "CREATE TABLE IF NOT EXISTS items (id BIGINT PRIMARY KEY, label TEXT, extra INT);\n" +
			"CREATE DATABASE IF NOT EXISTS archive;\n" +
			"CREATE DATABASE IF NOT EXISTS archive;",
		KeyedTables: "CREATE TABLE pairs (left_id INT, right_id INT, note TEXT, PRIMARY KEY (left_id, right_id));\n" +
			"CREATE TABLE later_pairs (left_id INT, right_id INT, note TEXT);\n" +
			"ALTER TABLE later_pairs ADD PRIMARY KEY (left_id, right_id);",
		Placement: "CREATE TABLE placed (a INT NOT NULL, b INT NOT NULL, c INT NOT NULL);\n" +
			"ALTER TABLE placed ADD COLUMN z INT FIRST;\n" +
			"ALTER TABLE placed ADD COLUMN m INT AFTER a;\n" +
			"ALTER TABLE placed MODIFY COLUMN c INT NOT NULL FIRST;\n" +
			"ALTER TABLE placed CHANGE COLUMN b b2 BIGINT NOT NULL AFTER c;",
		PlacedColumns: []string{"c", "b2", "z", "a", "m"},
		Renames: "RENAME TABLE items TO renamed_items;\n" +
			"CREATE VIEW items_view AS SELECT id, name FROM renamed_items;\n" +
			"RENAME TABLE items_view TO renamed_view;",
		RenamedRelations: []string{"renamed_items", "renamed_view"},
		StarViews:        starViews,
		ColumnAddition:   "ALTER TABLE items ADD COLUMN extra INT;",
	}
}

func mariaDBReplay() *dialecttest.ReplayFixture {
	replay := mysqlReplay()
	replay.Idempotent += "\nALTER TABLE items ADD COLUMN IF NOT EXISTS name VARCHAR(255);\n" +
		"ALTER TABLE IF EXISTS missing_items ADD COLUMN extra INT;\n" +
		"ALTER TABLE items MODIFY COLUMN IF EXISTS missing_column INT;\n" +
		"CREATE INDEX IF NOT EXISTS items_name ON items (name);\n" +
		"CREATE INDEX IF NOT EXISTS items_name ON items (name);"
	replay.AcceptedForms = []string{
		"CREATE OR REPLACE TABLE items (id BIGINT PRIMARY KEY, label TEXT);\n" +
			"CREATE OR REPLACE TABLE copied LIKE items;\n" +
			"CREATE OR REPLACE TABLE totals AS SELECT id FROM items;\n" +
			"CREATE OR REPLACE TEMPORARY TABLE scratch (id INT);",
		"CREATE OR REPLACE INDEX items_name ON items (name(10));\n" +
			"CREATE OR REPLACE DATABASE archive;\n" +
			"CREATE OR REPLACE SEQUENCE counter;\n" +
			"CREATE OR REPLACE VIEW item_names AS SELECT name FROM items;\n" +
			"CREATE OR REPLACE TRIGGER items_touch BEFORE INSERT ON items FOR EACH ROW SET NEW.name = 'x';",
	}
	return replay
}

func sqliteReplay() *dialecttest.ReplayFixture {
	return &dialecttest.ReplayFixture{
		Idempotent: "CREATE TABLE IF NOT EXISTS items (id INTEGER PRIMARY KEY, label TEXT, extra INTEGER);\n" +
			"CREATE INDEX IF NOT EXISTS items_name ON items (name);\n" +
			"CREATE INDEX IF NOT EXISTS items_name ON items (name);\n" +
			"CREATE VIEW IF NOT EXISTS items_view AS SELECT id, name FROM items;\n" +
			"CREATE VIEW IF NOT EXISTS items_view AS SELECT id FROM items;",
		KeyedTables: "CREATE TABLE pairs (left_id INTEGER, right_id INTEGER, note TEXT, PRIMARY KEY (left_id, right_id));\n" +
			"CREATE TABLE text_pairs (left_id TEXT PRIMARY KEY, note TEXT);",
		KeyColumnsNullable: true,
		Renames: "ALTER TABLE items RENAME TO renamed_items;\n" +
			"CREATE VIEW items_view AS SELECT id, name FROM renamed_items;",
		RenamedRelations: []string{"renamed_items", "items_view"},
		StarViews:        starViews,
		ColumnAddition:   "ALTER TABLE items ADD COLUMN extra INTEGER;",
		AcceptedForms: []string{
			"CREATE TABLE pairs (a TEXT NOT NULL, b TEXT NOT NULL, UNIQUE (a, b) ON CONFLICT REPLACE, CHECK (a <> b));\n" +
				"CREATE TABLE keyed (a TEXT, b TEXT, PRIMARY KEY (a) ON CONFLICT IGNORE, CONSTRAINT pair UNIQUE (a, b) ON CONFLICT ABORT);",
		},
	}
}

func duckdbReplay() *dialecttest.ReplayFixture {
	return &dialecttest.ReplayFixture{
		Idempotent: "CREATE TABLE IF NOT EXISTS items (id BIGINT PRIMARY KEY, label TEXT, extra INTEGER);\n" +
			"ALTER TABLE items ADD COLUMN IF NOT EXISTS name TEXT;\n" +
			"ALTER TABLE IF EXISTS missing_items ADD COLUMN extra INTEGER;\n" +
			"CREATE VIEW IF NOT EXISTS items_view AS SELECT id, name FROM items;\n" +
			"CREATE VIEW IF NOT EXISTS items_view AS SELECT id FROM items;\n" +
			"CREATE SEQUENCE IF NOT EXISTS counter;\n" +
			"CREATE SEQUENCE IF NOT EXISTS counter;\n" +
			"CREATE INDEX IF NOT EXISTS items_name ON items (name);",
		KeyedTables: "CREATE TABLE pairs (left_id INTEGER, right_id INTEGER, note TEXT, PRIMARY KEY (left_id, right_id));",
		Renames: "ALTER TABLE items RENAME TO renamed_items;\n" +
			"CREATE VIEW items_view AS SELECT id, name FROM renamed_items;\n" +
			"ALTER VIEW items_view RENAME TO renamed_view;",
		RenamedRelations: []string{"renamed_items", "renamed_view"},
		StarViews:        starViews,
		ColumnAddition:   "ALTER TABLE items ADD COLUMN extra INTEGER;",
	}
}

func clickhouseReplay() *dialecttest.ReplayFixture {
	return &dialecttest.ReplayFixture{
		Idempotent: "CREATE TABLE IF NOT EXISTS items (id Int64, label String) ENGINE = MergeTree ORDER BY id;\n" +
			"ALTER TABLE items ADD COLUMN IF NOT EXISTS name String;\n" +
			"ALTER TABLE items MODIFY COLUMN IF EXISTS missing_column String;\n" +
			"CREATE VIEW IF NOT EXISTS items_view AS SELECT id, name FROM items;\n" +
			"CREATE VIEW IF NOT EXISTS items_view AS SELECT id FROM items;",
		Placement: "CREATE TABLE placed (a Int32, b Int32, c Int32) ENGINE = MergeTree ORDER BY a;\n" +
			"ALTER TABLE placed ADD COLUMN z Int32 FIRST;\n" +
			"ALTER TABLE placed ADD COLUMN m Int32 AFTER a;\n" +
			"ALTER TABLE placed MODIFY COLUMN c Int64 FIRST;\n" +
			"ALTER TABLE placed MODIFY COLUMN b Int64 AFTER c;",
		PlacedColumns: []string{"c", "b", "z", "a", "m"},
		Renames: "RENAME TABLE items TO renamed_items;\n" +
			"CREATE VIEW items_view AS SELECT id, name FROM renamed_items;\n" +
			"RENAME TABLE items_view TO renamed_view;",
		RenamedRelations: []string{"renamed_items", "renamed_view"},
		StarViews: "CREATE VIEW star_view AS SELECT * FROM items;\n" +
			"CREATE VIEW qualified_view AS SELECT items.* FROM items;",
		ColumnAddition: "ALTER TABLE items ADD COLUMN extra Int32;",
	}
}

func postgresFamilyFixture(newDialect func(...postgres.Option) *postgres.Dialect) dialecttest.Fixture {
	return dialecttest.Fixture{
		KnownFailures: knownFailures(),
		Spans: &dialecttest.SpanFixture{
			Statements: postgresSpanStatements,
			Corpora: []fs.FS{
				os.DirFS("../postgres/testdata"),
				os.DirFS("../../../tests/integration/postgres/testdata"),
				os.DirFS("../../../examples/postgres"),
			},
		},
		DeclaredParameters: postgresDeclaredParameters("smallint"),
		NewDialect:         func() dialect.DialectPort { return newDialect() },
		BaseMigration:      "CREATE TABLE items (id integer PRIMARY KEY, name text);",
		GroupingQuery:      "SELECT id, count(*) AS total FROM items GROUP BY ROLLUP (id);",
		Elements: &dialecttest.ElementFixture{Migration: "CREATE TABLE samples (id integer PRIMARY KEY, readings integer[] NOT NULL);",
			Aggregate: "array_agg(id)", Parameter: "SELECT id FROM samples WHERE readings = $1",
			Quantified: "SELECT id FROM samples WHERE id = ANY($1)", Literals: postgresBracketedValues},
		Replay: postgresReplay(),
		ParamQuery: "-- sgl.query(name: GetItem, command: one)\n" +
			"SELECT id, name FROM items WHERE id = $1;",
		SplitterMigration: "CREATE TABLE alpha (id integer);\nCREATE TABLE beta (id integer);",
		QuotedMigration:   `CREATE TABLE "weird name" (id integer);`,
		ReservedWordMigration: `CREATE TABLE reserved_items (id integer PRIMARY KEY, "check" integer NOT NULL, ` +
			`"constraint" text NOT NULL, "index" integer NOT NULL, "primary" text NOT NULL, "group" text NOT NULL, ` +
			`"where" integer NOT NULL);`,
		ReservedWordQuery: "-- sgl.query(name: GetReserved, command: many)\n" +
			`SELECT id, "group" FROM reserved_items WHERE "where" = $1;`,
		TypedMigration: "CREATE TABLE typed_items (id integer PRIMARY KEY, created_at timestamp(3) with time zone NOT NULL, " +
			"updated_at timestamp(3) without time zone, tags integer ARRAY NOT NULL, label text NOT NULL);",
		TypedColumns: []string{"id", "created_at", "updated_at", "tags", "label"},
		Tokeniser:    nil,
		RowLocks:     []string{"FOR UPDATE", "FOR NO KEY UPDATE", "FOR SHARE", "FOR KEY SHARE", "FOR UPDATE SKIP LOCKED"},
		PlaceholderQuery: "-- sgl.query(name: CountPlaceholders, command: many)\n" +
			"SELECT .5 AS half, id FROM items WHERE id = $1 AND name = $2;",
		PlaceholderCount:   2,
		DerivedColumnLists: true,
		CTEColumnLists:     true,
		TableColumnLists:   true,
		JoinAliases:        true,
		QuantifiedArrays:   true,
		UnboundStatement:   "SET LOCAL app.user_id = $1",
		RowComparisons:     true,
		ValuesArms:         true,
		NotInQuery: "-- sgl.query(name: ExcludeItems, command: many)\n" +
			"SELECT id FROM items WHERE id NOT IN ($1);",
		NewDialectWithExtraType: func(name string, sqlType sqlmodel.SQLType) dialect.DialectPort {
			return newDialect(postgres.WithExtraTypes(map[string]sqlmodel.SQLType{name: sqlType}))
		},
	}
}

func mysqlDeclaredParameters() *dialecttest.DeclaredParameterFixture {
	return &dialecttest.DeclaredParameterFixture{
		Migration: "CREATE TABLE declared_items (value DATE, label VARCHAR(40));",
		Restating: "-- sgl.query(name: CompareRestated, command: many)\n" +
			"SELECT value FROM declared_items WHERE value = CAST(? AS DATE) OR CAST(? AS DATE) <> value " +
			"OR value = ? OR ? = value OR value = CONVERT(?, DATE);\n" +
			"-- sgl.query(name: ListRestated, command: many)\n" +
			"SELECT value FROM declared_items WHERE value IN (CAST(? AS DATE), ?) OR value NOT IN (CAST(? AS DATE));\n" +
			"-- sgl.query(name: BoundRestated, command: many)\n" +
			"SELECT value FROM declared_items WHERE value BETWEEN CAST(? AS DATE) AND ? OR value BETWEEN ? AND ?;\n" +
			"-- sgl.query(name: InsertRestated, command: exec)\n" +
			"INSERT INTO declared_items (value) VALUES (CAST(? AS DATE)), (?), (CONVERT(?, DATE));\n" +
			"-- sgl.query(name: UpdateRestated, command: exec)\n" +
			"UPDATE declared_items SET value = CAST(? AS DATE) WHERE value = CAST(? AS DATE);\n" +
			"-- sgl.query(name: NestRestated, command: many)\n" +
			"WITH found AS (SELECT value FROM declared_items WHERE value = CAST(? AS DATE)) SELECT value FROM found;\n",
		Differing: "-- sgl.query(name: CompareDiffering, command: many)\n" +
			"SELECT value FROM declared_items WHERE value = CAST(? AS DATETIME) OR CAST(? AS DATETIME) <> value " +
			"OR value = CONVERT(?, DATETIME);\n" +
			"-- sgl.query(name: ListDiffering, command: many)\n" +
			"SELECT value FROM declared_items WHERE value IN (CAST(? AS DATETIME));\n" +
			"-- sgl.query(name: BoundDiffering, command: many)\n" +
			"SELECT value FROM declared_items WHERE value BETWEEN CAST(? AS DATETIME) AND CAST(? AS DATETIME);\n" +
			"-- sgl.query(name: InsertDiffering, command: exec)\n" +
			"INSERT INTO declared_items (value) VALUES (CAST(? AS DATETIME)), (CONVERT(?, DATETIME));\n" +
			"-- sgl.query(name: UpdateDiffering, command: exec)\n" +
			"UPDATE declared_items SET value = CAST(? AS DATETIME) WHERE value = CAST(? AS DATETIME);\n",
		Patterns: "-- sgl.query(name: MatchPatterns, command: many)\n" +
			"SELECT value FROM declared_items WHERE label LIKE ? OR label NOT LIKE CAST(? AS CHAR) OR label REGEXP ? " +
			"OR label RLIKE ? OR label SOUNDS LIKE ?;\n",
		ProjectionViews: "CREATE VIEW declared_view AS SELECT value, CAST(value AS CHAR) AS other FROM declared_items;",
		Projections: "-- sgl.query(name: ProjectAsIs, command: many)\n" +
			"SELECT value, CAST(value AS DATE) AS restated, CONVERT(value, DATE) AS converted FROM declared_items;\n" +
			"-- sgl.query(name: ProjectThroughView, command: many)\n" +
			"SELECT value FROM declared_view;\n",
		CastProjections: "-- sgl.query(name: ProjectCast, command: many)\n" +
			"SELECT CAST(value AS CHAR) AS as_text, CAST(value AS DATETIME) AS moment, CONVERT(value, CHAR) AS converted " +
			"FROM declared_items;\n" +
			"-- sgl.query(name: ProjectCastThroughDerived, command: many)\n" +
			"WITH found AS (SELECT CAST(value AS CHAR) AS other FROM declared_items) SELECT found.other, derived.other AS derived_other " +
			"FROM found, (SELECT CAST(value AS DATETIME) AS other FROM declared_items) AS derived;\n" +
			"-- sgl.query(name: ProjectCastThroughView, command: many)\n" +
			"SELECT other FROM declared_view;\n",
	}
}

func mysqlFamilyFixture(newDialect func(...mysql.Option) *mysql.Dialect) dialecttest.Fixture {
	return dialecttest.Fixture{
		KnownFailures: knownFailures(),
		Spans: &dialecttest.SpanFixture{
			Statements: mysqlSpanStatements,
			Corpora: []fs.FS{
				os.DirFS("../mysql/testdata"),
				os.DirFS("../mariadb/testdata"),
				os.DirFS("../../../tests/integration/mysql/testdata"),
				os.DirFS("../../../tests/integration/mariadb/testdata"),
				os.DirFS("../../../examples/mysql"),
				os.DirFS("../../../examples/mariadb"),
			},
		},
		DeclaredParameters: mysqlDeclaredParameters(),
		NewDialect:         func() dialect.DialectPort { return newDialect() },
		BaseMigration:      "CREATE TABLE items (id INT PRIMARY KEY, name VARCHAR(255));",
		Replay:             mysqlReplay(),
		GroupingQuery:      "SELECT id, count(*) AS total FROM items GROUP BY id WITH ROLLUP;",
		ParamQuery: "-- sgl.query(name: GetItem, command: one)\n" +
			"SELECT id, name FROM items WHERE id = ?;",
		SplitterMigration: "CREATE TABLE alpha (id INT);\nCREATE TABLE beta (id INT);",
		QuotedMigration:   "CREATE TABLE `weird name` (id INT);",
		ReservedWordMigration: "CREATE TABLE reserved_items (id INT PRIMARY KEY, `check` INT NOT NULL, " +
			"`constraint` VARCHAR(255) NOT NULL, `index` INT NOT NULL, `primary` VARCHAR(255) NOT NULL, " +
			"`group` VARCHAR(255) NOT NULL, `where` INT NOT NULL);",
		ReservedWordQuery: "-- sgl.query(name: GetReserved, command: many)\n" +
			"SELECT id, `group` FROM reserved_items WHERE `where` = ?;",
		TypedMigration: "CREATE TABLE typed_items (id BIGINT NOT NULL AUTO_INCREMENT, " +
			"name VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL, " +
			"amount INT UNSIGNED ZEROFILL NOT NULL, PRIMARY KEY (id));",
		TypedColumns: []string{"id", "name", "amount"},
		Tokeniser:    nil,
		RowLocks:     []string{"FOR UPDATE", "FOR SHARE", "LOCK IN SHARE MODE", "FOR UPDATE SKIP LOCKED", "FOR SHARE NOWAIT"},
		PlaceholderQuery: "-- sgl.query(name: CountPlaceholders, command: many)\n" +
			"SELECT .5 AS half, id FROM items WHERE id = ? AND name = ?;",
		PlaceholderCount:   2,
		DerivedColumnLists: true,
		CTEColumnLists:     true,
		RowComparisons:     true,
		UnboundStatement:   "SET @user_id = ?",
		NotInQuery: "-- sgl.query(name: ExcludeItems, command: many)\n" +
			"SELECT id FROM items WHERE id NOT IN (?);",
		NewDialectWithExtraType: func(name string, sqlType sqlmodel.SQLType) dialect.DialectPort {
			return newDialect(mysql.WithExtraTypes(map[string]sqlmodel.SQLType{name: sqlType}))
		},
	}
}

func sqliteDeclaredParameters() *dialecttest.DeclaredParameterFixture {
	return &dialecttest.DeclaredParameterFixture{
		Migration: "CREATE TABLE declared_items (value TEXT, label TEXT);",
		Restating: "-- sgl.query(name: CompareRestated, command: many)\n" +
			"SELECT value FROM declared_items WHERE value = CAST(?1 AS TEXT) OR CAST(?2 AS TEXT) <> value " +
			"OR value = ?3 OR ?4 = value;\n" +
			"-- sgl.query(name: ListRestated, command: many)\n" +
			"SELECT value FROM declared_items WHERE value IN (CAST(?1 AS TEXT), ?2) OR value NOT IN (CAST(?3 AS TEXT));\n" +
			"-- sgl.query(name: BoundRestated, command: many)\n" +
			"SELECT value FROM declared_items WHERE value BETWEEN CAST(?1 AS TEXT) AND ?2 OR value BETWEEN ?3 AND ?4;\n" +
			"-- sgl.query(name: InsertRestated, command: exec)\n" +
			"INSERT INTO declared_items (value) VALUES (CAST(?1 AS TEXT)), (?2);\n" +
			"-- sgl.query(name: UpdateRestated, command: exec)\n" +
			"UPDATE declared_items SET value = CAST(?1 AS TEXT) WHERE value = CAST(?2 AS TEXT);\n" +
			"-- sgl.query(name: NestRestated, command: many)\n" +
			"WITH found AS (SELECT value FROM declared_items WHERE value = CAST(?1 AS TEXT)) SELECT value FROM found;\n",
		Differing: "-- sgl.query(name: CompareDiffering, command: many)\n" +
			"SELECT value FROM declared_items WHERE value = CAST(?1 AS INTEGER) OR CAST(?2 AS INTEGER) <> value;\n" +
			"-- sgl.query(name: ListDiffering, command: many)\n" +
			"SELECT value FROM declared_items WHERE value IN (CAST(?1 AS INTEGER));\n" +
			"-- sgl.query(name: BoundDiffering, command: many)\n" +
			"SELECT value FROM declared_items WHERE value BETWEEN CAST(?1 AS INTEGER) AND CAST(?2 AS INTEGER);\n" +
			"-- sgl.query(name: InsertDiffering, command: exec)\n" +
			"INSERT INTO declared_items (value) VALUES (CAST(?1 AS INTEGER));\n" +
			"-- sgl.query(name: UpdateDiffering, command: exec)\n" +
			"UPDATE declared_items SET value = CAST(?1 AS INTEGER) WHERE value = CAST(?2 AS INTEGER);\n",
		Patterns: "-- sgl.query(name: MatchPatterns, command: many)\n" +
			"SELECT value FROM declared_items WHERE label LIKE ?1 OR label GLOB CAST(?2 AS TEXT) OR label NOT LIKE ?3 " +
			"OR label REGEXP ?4;\n",
		ProjectionViews: "CREATE VIEW declared_view AS SELECT value, CAST(value AS INTEGER) AS other FROM declared_items;",
		Projections: "-- sgl.query(name: ProjectAsIs, command: many)\n" +
			"SELECT value, CAST(value AS TEXT) AS restated FROM declared_items;\n" +
			"-- sgl.query(name: ProjectThroughView, command: many)\n" +
			"SELECT value FROM declared_view;\n",
		CastProjections: "-- sgl.query(name: ProjectCast, command: many)\n" +
			"SELECT CAST(value AS INTEGER) AS as_number, CAST(value AS BLOB) AS as_bytes FROM declared_items;\n" +
			"-- sgl.query(name: ProjectCastThroughDerived, command: many)\n" +
			"WITH found AS (SELECT CAST(value AS INTEGER) AS other FROM declared_items) SELECT found.other, derived.other AS derived_other " +
			"FROM found, (SELECT CAST(value AS REAL) AS other FROM declared_items) AS derived;\n" +
			"-- sgl.query(name: ProjectCastThroughView, command: many)\n" +
			"SELECT other FROM declared_view;\n",
	}
}

func duckdbDeclaredParameters() *dialecttest.DeclaredParameterFixture {
	return &dialecttest.DeclaredParameterFixture{
		Migration: "CREATE TABLE declared_items (value INTEGER, label VARCHAR);",
		Restating: "-- sgl.query(name: CompareRestated, command: many)\n" +
			"SELECT value FROM declared_items WHERE value = $1::INTEGER OR value <> CAST($2 AS INTEGER) " +
			"OR $3::INT4 = value OR CAST($4 AS INTEGER) < value OR value = $5 OR $6 = value;\n" +
			"-- sgl.query(name: ListRestated, command: many)\n" +
			"SELECT value FROM declared_items WHERE value IN ($1::INTEGER, CAST($2 AS INTEGER), $3) " +
			"OR value NOT IN ($4::INTEGER);\n" +
			"-- sgl.query(name: BoundRestated, command: many)\n" +
			"SELECT value FROM declared_items WHERE value BETWEEN $1::INTEGER AND CAST($2 AS INTEGER) " +
			"OR value BETWEEN $3 AND $4;\n" +
			"-- sgl.query(name: InsertRestated, command: exec)\n" +
			"INSERT INTO declared_items (value) VALUES ($1::INTEGER), (CAST($2 AS INTEGER)), ($3);\n" +
			"-- sgl.query(name: UpdateRestated, command: exec)\n" +
			"UPDATE declared_items SET value = CAST($1 AS INTEGER) WHERE value = $2::INTEGER;\n" +
			"-- sgl.query(name: NestRestated, command: many)\n" +
			"WITH found AS (SELECT value FROM declared_items WHERE value = $1::INTEGER) SELECT value FROM found;\n",
		Differing: "-- sgl.query(name: CompareDiffering, command: many)\n" +
			"SELECT value FROM declared_items WHERE value = $1::SMALLINT OR value <> CAST($2 AS SMALLINT) " +
			"OR $3::SMALLINT = value;\n" +
			"-- sgl.query(name: ListDiffering, command: many)\n" +
			"SELECT value FROM declared_items WHERE value IN ($1::SMALLINT, CAST($2 AS SMALLINT));\n" +
			"-- sgl.query(name: BoundDiffering, command: many)\n" +
			"SELECT value FROM declared_items WHERE value BETWEEN $1::SMALLINT AND CAST($2 AS SMALLINT);\n" +
			"-- sgl.query(name: InsertDiffering, command: exec)\n" +
			"INSERT INTO declared_items (value) VALUES ($1::SMALLINT), (CAST($2 AS SMALLINT));\n" +
			"-- sgl.query(name: UpdateDiffering, command: exec)\n" +
			"UPDATE declared_items SET value = CAST($1 AS SMALLINT) WHERE value = $2::SMALLINT;\n",
		Patterns: "-- sgl.query(name: MatchPatterns, command: many)\n" +
			"SELECT value FROM declared_items WHERE label LIKE $1 OR label ILIKE $2::VARCHAR OR label GLOB $3 " +
			"OR label SIMILAR TO $4 OR label ~~ $5::VARCHAR OR label !~~* $6 OR label ^@ $7;\n",
		ProjectionViews: "CREATE VIEW declared_view AS SELECT value, CAST(value AS VARCHAR) AS other FROM declared_items;",
		Projections: "-- sgl.query(name: ProjectAsIs, command: many)\n" +
			"SELECT value, value::INTEGER AS restated, CAST(value AS INT4) AS aliased FROM declared_items;\n" +
			"-- sgl.query(name: ProjectThroughView, command: many)\n" +
			"SELECT value FROM declared_view;\n",
		CastProjections: "-- sgl.query(name: ProjectCast, command: many)\n" +
			"SELECT value::VARCHAR AS as_text, CAST(value AS BIGINT) AS widened, value::SMALLINT AS narrowed FROM declared_items;\n" +
			"-- sgl.query(name: ProjectCastThroughDerived, command: many)\n" +
			"WITH found AS (SELECT CAST(value AS VARCHAR) AS other FROM declared_items) SELECT found.other, derived.other AS derived_other " +
			"FROM found, (SELECT value::DOUBLE AS other FROM declared_items) AS derived;\n" +
			"-- sgl.query(name: ProjectCastThroughView, command: many)\n" +
			"SELECT other FROM declared_view;\n",
	}
}

func clickhouseDeclaredParameters() *dialecttest.DeclaredParameterFixture {
	return &dialecttest.DeclaredParameterFixture{
		Migration: "CREATE TABLE declared_items (value IPv4, label String) ENGINE = MergeTree ORDER BY tuple();",
		Restating: "-- sgl.query(name: CompareRestated, command: many)\n" +
			"SELECT value FROM declared_items WHERE value = {first:IPv4} OR {second:IPv4} <> value;\n" +
			"-- sgl.query(name: ListRestated, command: many)\n" +
			"SELECT value FROM declared_items WHERE value IN ({first:IPv4}, {second:IPv4}) OR value NOT IN ({third:IPv4});\n" +
			"-- sgl.query(name: BoundRestated, command: many)\n" +
			"SELECT value FROM declared_items WHERE value BETWEEN {low:IPv4} AND {high:IPv4};\n" +
			"-- sgl.query(name: InsertRestated, command: exec)\n" +
			"INSERT INTO declared_items (value) VALUES ({first:IPv4}), ({second:IPv4});\n" +
			"-- sgl.query(name: MutateRestated, command: asyncexec)\n" +
			"ALTER TABLE declared_items UPDATE value = {first:IPv4} WHERE value = {second:IPv4};\n" +
			"-- sgl.query(name: UpdateRestated, command: exec)\n" +
			"UPDATE declared_items SET value = {first:IPv4} WHERE value = {second:IPv4};\n" +
			"-- sgl.query(name: NestRestated, command: many)\n" +
			"WITH found AS (SELECT value FROM declared_items WHERE value = {first:IPv4}) SELECT value FROM found;\n",
		Differing: "-- sgl.query(name: CompareDiffering, command: many)\n" +
			"SELECT value FROM declared_items WHERE value = {first:String} OR {second:String} <> value " +
			"OR value = CAST({third:String} AS IPv4);\n" +
			"-- sgl.query(name: ListDiffering, command: many)\n" +
			"SELECT value FROM declared_items WHERE value IN ({first:String}, {second:String});\n" +
			"-- sgl.query(name: BoundDiffering, command: many)\n" +
			"SELECT value FROM declared_items WHERE value BETWEEN {low:String} AND {high:String};\n" +
			"-- sgl.query(name: InsertDiffering, command: exec)\n" +
			"INSERT INTO declared_items (value) VALUES ({first:String});\n" +
			"-- sgl.query(name: MutateDiffering, command: asyncexec)\n" +
			"ALTER TABLE declared_items UPDATE value = {first:String} WHERE value = {second:String};\n" +
			"-- sgl.query(name: UpdateDiffering, command: exec)\n" +
			"UPDATE declared_items SET value = {first:String} WHERE value = {second:String};\n",
		Patterns: "-- sgl.query(name: MatchPatterns, command: many)\n" +
			"SELECT value FROM declared_items WHERE label LIKE {first:String} OR label ILIKE {second:String} " +
			"OR label NOT LIKE {third:String} OR label NOT ILIKE {fourth:String} OR match(label, {fifth:String});\n",
		ProjectionViews: "CREATE VIEW declared_view AS SELECT value, CAST(value AS String) AS other FROM declared_items;",
		Projections: "-- sgl.query(name: ProjectAsIs, command: many)\n" +
			"SELECT value, CAST(value AS IPv4) AS restated, value::IPv4 AS shorthand FROM declared_items;\n" +
			"-- sgl.query(name: ProjectThroughView, command: many)\n" +
			"SELECT value FROM declared_view;\n",
		CastProjections: "-- sgl.query(name: ProjectCast, command: many)\n" +
			"SELECT CAST(value AS String) AS as_text, value::UInt32 AS as_number, toString(value) AS printed FROM declared_items;\n" +
			"-- sgl.query(name: ProjectCastThroughDerived, command: many)\n" +
			"WITH found AS (SELECT CAST(value AS String) AS other FROM declared_items) SELECT other FROM found;\n" +
			"-- sgl.query(name: ProjectCastThroughView, command: many)\n" +
			"SELECT other FROM declared_view;\n",
	}
}

func martianFixture() dialecttest.Fixture {
	return dialecttest.Fixture{
		KnownFailures: map[string]string{
			"BeginAliasSplitsBothStatements":                        martianSelectsFromATable,
			"ScalarFunctionsInFromFollowTheDialect":                 martianSelectsFromATable,
			"CoalescedParametersTakeTheirSiblingsType":              martianHasNoCoalesce,
			"ComparedExpressionParametersAreNotTypedAsTheColumn":    martianHasNoCoalesce,
			"ComparedParametersAreNamedAfterTheirColumns":           martianHasNoBetween,
			"PredicatesHaveTheStatedType":                           martianHasNoBetween,
			"PredicatesOverNullableOperandsAreNullable":             martianHasNoJoins,
			"OuterJoinedRelationsAreNullable":                       martianHasNoJoins,
			"UsingJoinColumnAppearsOnce":                            martianHasNoJoins,
			"NestedQueriesSeeEnclosingRelations":                    martianHasNoNestedQueries,
			"ParametersComparedWithScalarSubqueriesAreTyped":        martianHasNoNestedQueries,
			"ScalarSubqueriesTakeTheirColumnsType":                  martianHasNoNestedQueries,
			"UnknownNamesInNestedQueriesAreReported":                martianHasNoNestedQueries,
			"ParametersStandingForConditionsAreBooleans":            "Martian has no CASE expression",
			"UngroupedColumnsFollowTheDialect":                      "Martian has no GROUP BY clause",
			"ReturningFollowsInfo":                                  "Martian has no RETURNING clause, so its parser refuses one as a syntax error before the analyser can report it",
			"ValuesWithoutAColumnListTypeTheirParametersByPosition": "Martian's INSERT always names its columns",
			"InsertShapesTheEngineRefusesAreReported": "Martian's INSERT takes one VALUES row and no query, and its parser refuses a row of the " +
				"wrong length as a syntax error before the analyser can report it",
		},
		NewDialect:    func() dialect.DialectPort { return martian.New() },
		BaseMigration: "CREATE TABLE items (id WHOLE64 PRIMARY KEY, name TEXTUAL);",
		ParamQuery: "-- sgl.query(name: GetItem, command: one)\n" +
			"SELECT id, name FROM items WHERE id = @p1;",
		SplitterMigration: "CREATE TABLE alpha (id WHOLE32);\nCREATE TABLE beta (id WHOLE32);",
		QuotedMigration:   "CREATE TABLE [weird name] (id WHOLE32);",
		ReservedWordMigration: "CREATE TABLE reserved_items (id WHOLE32 PRIMARY KEY, [check] WHOLE32 NOT NULL, " +
			"[constraint] TEXTUAL NOT NULL, [index] WHOLE32 NOT NULL, [primary] TEXTUAL NOT NULL, [group] TEXTUAL NOT NULL, " +
			"[where] WHOLE32 NOT NULL);",
		ReservedWordQuery: "-- sgl.query(name: GetReserved, command: many)\n" +
			"SELECT id, [group] FROM reserved_items WHERE [where] = @p1;",
		TypedMigration: "CREATE TABLE typed_items (id WHOLE64 PRIMARY KEY, name TEXTUAL(40) NOT NULL, " +
			"price EXACT(10, 2) DEFAULT 0, data BLOB, created MOMENT);",
		TypedColumns: []string{"id", "name", "price", "data", "created"},
		PlaceholderQuery: "-- sgl.query(name: CountPlaceholders, command: many)\n" +
			"SELECT id FROM items WHERE id = @p1 AND name = @p2;",
		PlaceholderCount: 2,
	}
}

func conformanceFixtures() map[string]dialecttest.Fixture {
	cockroachDBFixture := postgresFamilyFixture(cockroachdb.New)
	cockroachDBFixture.GroupingQuery = ""
	cockroachDBFixture.Replay = cockroachDBReplay()
	cockroachDBFixture.Flavour = &dialecttest.FlavourFixture{
		New:              func() dialect.DialectPort { return cockroachdb.New(postgresUserOptions...) },
		UserFunction:     userFunctionName,
		FlavourFunctions: []string{"unique_rowid"},
		FlavourTypes:     []string{"string", "bytes"},
	}

	timescaleDBFixture := postgresFamilyFixture(timescaledb.New)
	timescaleDBFixture.Flavour = &dialecttest.FlavourFixture{
		New:              func() dialect.DialectPort { return timescaledb.New(postgresUserOptions...) },
		UserFunction:     userFunctionName,
		FlavourFunctions: []string{"time_bucket"},
		FlavourTypes:     nil,
	}

	mariaDBFixture := mysqlFamilyFixture(mariadb.New)
	mariaDBFixture.DerivedColumnLists = false
	mariaDBFixture.ValuesArms = true
	mariaDBFixture.RowLocks = []string{"FOR UPDATE", "LOCK IN SHARE MODE", "FOR UPDATE NOWAIT", "FOR UPDATE SKIP LOCKED"}
	mariaDBFixture.Replay = mariaDBReplay()
	mariaDBFixture.Flavour = &dialecttest.FlavourFixture{
		New:              func() dialect.DialectPort { return mariadb.New(mysqlUserOptions...) },
		UserFunction:     userFunctionName,
		FlavourFunctions: []string{"sys_guid"},
		FlavourTypes:     nil,
		WithheldFunctions: []string{
			"any_value", "grouping", "regexp_like", "uuid_to_bin", "bin_to_uuid", "is_uuid",
			"json_storage_size", "json_schema_validation_report",
		},
	}

	return map[string]dialecttest.Fixture{
		"postgres":    postgresFamilyFixture(postgres.New),
		"cockroachdb": cockroachDBFixture,
		"timescaledb": timescaleDBFixture,
		"mysql":       mysqlFamilyFixture(mysql.New),
		"mariadb":     mariaDBFixture,
		"sqlite":      sqliteFixture,
		"duckdb":      duckdbFixture,
		"clickhouse":  clickhouseFixture,
		"martian":     martianFixture(),
	}
}

func TestConformance(t *testing.T) {
	t.Parallel()

	for name, fixture := range conformanceFixtures() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dialecttest.RunConformance(t, &fixture)
		})
	}
}

func functionPointer(function any) uintptr {
	value := reflect.ValueOf(function)
	if value.IsNil() {
		return 0
	}
	return value.Pointer()
}

func requireSameLexicalRules(t *testing.T, want, got sqllex.Rules) {
	t.Helper()

	require.Equal(t, functionPointer(want.ProceduralBlock), functionPointer(got.ProceduralBlock), "ProceduralBlock")
	require.Equal(t, functionPointer(want.RequiresNoTransaction), functionPointer(got.RequiresNoTransaction), "RequiresNoTransaction")
	want.ProceduralBlock, got.ProceduralBlock = nil, nil
	want.RequiresNoTransaction, got.RequiresNoTransaction = nil, nil
	require.Equal(t, want, got)
}

func TestInfoLexicalIsTheEngineLexicalLeaf(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		engine dialect.DialectPort
		name   string
		leaf   sqllex.Rules
	}{
		{name: "postgres", engine: postgres.New(), leaf: postgres_lexical.Rules()},
		{name: "postgres with a custom name", engine: postgres.New(postgres.WithName("custom")), leaf: postgres_lexical.Rules()},
		{name: "cockroachdb", engine: cockroachdb.New(), leaf: postgres_lexical.Rules()},
		{name: "timescaledb", engine: timescaledb.New(), leaf: timescaledb_lexical.Rules()},
		{name: "timescaledb with user options", engine: timescaledb.New(postgresUserOptions...), leaf: timescaledb_lexical.Rules()},
		{name: "mysql", engine: mysql.New(), leaf: mysql_lexical.Rules()},
		{name: "mariadb", engine: mariadb.New(), leaf: mysql_lexical.Rules()},
		{name: "sqlite", engine: sqlite.New(), leaf: sqlite_lexical.Rules()},
		{name: "duckdb", engine: duckdb.New(), leaf: duckdb_lexical.Rules()},
		{name: "clickhouse", engine: clickhouse.New(), leaf: clickhouse_lexical.Rules()},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			requireSameLexicalRules(t, testCase.leaf, testCase.engine.Info().Lexical)
		})
	}
}
