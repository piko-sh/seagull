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

package fuzzsemantic

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	// engineClickHouse names the ClickHouse dialect.
	engineClickHouse = "clickhouse"

	// engineSQLite names the SQLite dialect.
	engineSQLite = "sqlite"

	// engineMySQL names the MySQL dialect.
	engineMySQL = "mysql"

	// engineMariaDB names the MariaDB dialect.
	engineMariaDB = "mariadb"

	// enginePostgres names the PostgreSQL dialect.
	enginePostgres = "postgres"

	// engineTimescale names the TimescaleDB dialect.
	engineTimescale = "timescaledb"

	// engineCockroach names the CockroachDB dialect.
	engineCockroach = "cockroachdb"

	// engineDuckDB names the DuckDB dialect.
	engineDuckDB = "duckdb"

	// itemTable names the item table.
	itemTable = "sem_items"

	// peerTable names the peer table.
	peerTable = "sem_peers"

	// itemProjection selects every item column in declaration order.
	itemProjection = "SELECT i.id, i.grp, i.a, i.b, i.label FROM " + itemTable + " i"

	// aggregateProjection selects the aggregates the partition relation combines.
	aggregateProjection = "SELECT COUNT(*) AS total, COUNT(i.a) AS present, SUM(i.a) AS sum_a, MIN(i.a) AS min_a, MAX(i.a) AS max_a, " +
		"MAX(i.label) AS max_label FROM " + itemTable + " i"

	// pageProjection selects the paginated columns.
	pageProjection = "SELECT i.id, i.grp, i.a FROM " + itemTable + " i"

	// itemBranch is the first compound branch.
	itemBranch = "SELECT i.a AS v FROM " + itemTable + " i"

	// peerBranch is the second compound branch.
	peerBranch = "SELECT p.c AS v FROM " + peerTable + " p"

	// whereKeyword starts a condition.
	whereKeyword = " WHERE "

	// closing ends a parenthesised clause.
	closing = ")"

	// countProjection starts a counting query.
	countProjection = "SELECT COUNT(*) AS total FROM "
)

const (
	// methodAll reads every item.
	methodAll = "SemAll"

	// methodAggregateAll aggregates every item.
	methodAggregateAll = "SemAggAll"

	// methodWindow numbers and totals each group.
	methodWindow = "SemWindow"

	// methodGroupSum totals each group.
	methodGroupSum = "SemGroupSum"

	// methodWindowBuilder is the runtime builder over a window query.
	methodWindowBuilder = "SemWindowBuilder"

	// methodLeftJoin pairs items with their peers.
	methodLeftJoin = "SemLeftJoin"

	// methodInnerCount counts matched pairs.
	methodInnerCount = "SemInnerCount"

	// methodAntiCount counts items without peers.
	methodAntiCount = "SemAntiCount"

	// methodFullJoin pairs items and peers from both sides.
	methodFullJoin = "SemFullJoin"

	// methodOrphanCount counts peers without items.
	methodOrphanCount = "SemOrphanCount"

	// methodSemiIn is the IN formulation of the semi-join.
	methodSemiIn = "SemSemiIn"

	// methodSemiExists is the EXISTS formulation of the semi-join.
	methodSemiExists = "SemSemiExists"

	// methodSemiJoin is the DISTINCT join formulation of the semi-join.
	methodSemiJoin = "SemSemiJoin"

	// methodBranchItems is the first compound branch alone.
	methodBranchItems = "SemBranchItems"

	// methodBranchPeers is the second compound branch alone.
	methodBranchPeers = "SemBranchPeers"

	// methodUnionAll concatenates the branches.
	methodUnionAll = "SemUnionAll"

	// methodUnion merges the branches without duplicates.
	methodUnion = "SemUnion"

	// methodUnionDistinct removes duplicates from the concatenation.
	methodUnionDistinct = "SemUnionDistinct"

	// methodIntersect keeps values both branches hold.
	methodIntersect = "SemIntersect"

	// methodExcept keeps values only the first branch holds.
	methodExcept = "SemExcept"

	// methodPageFull reads the ordered rows.
	methodPageFull = "SemPageFull"

	// methodPage reads one page of the ordered rows.
	methodPage = "SemPage"

	// methodBuilder is the runtime builder the builder pages use.
	methodBuilder = "SemBuilder"

	// methodIntersectDistinct spells ClickHouse's INTERSECT DISTINCT.
	methodIntersectDistinct = "SemIntersectDistinct"

	// methodExceptDistinct spells ClickHouse's EXCEPT DISTINCT.
	methodExceptDistinct = "SemExceptDistinct"

	// methodLimitBuilder is the runtime builder over a query with its own LIMIT and OFFSET.
	methodLimitBuilder = "SemLimitBuilder"

	// methodLimitByBuilder is the runtime builder over a ClickHouse query with LIMIT n BY.
	methodLimitByBuilder = "SemLimitByBuilder"
)

// query is one catalogue entry.
type query struct {
	// name is the generated method name.
	name string

	// sql is the query without its terminator.
	sql string

	// dynamic generates a runtime builder.
	dynamic bool
}

// Supported reports whether the package models an engine.
//
// Takes engine (string) which names the dialect.
//
// Returns bool which is true for the eight modelled dialects.
func Supported(engine string) bool {
	switch engine {
	case enginePostgres, engineTimescale, engineCockroach, engineMySQL, engineMariaDB, engineSQLite, engineDuckDB, engineClickHouse:
		return true
	default:
		return false
	}
}

// Schema returns the modelled tables in the engine's syntax.
//
// Takes engine (string) which names the dialect.
//
// Returns []string which holds one CREATE TABLE statement per table.
func Schema(engine string) []string {
	integer, nullable, text, suffix := "BIGINT NOT NULL", "BIGINT", "TEXT", ""
	switch engine {
	case engineSQLite:
		integer, nullable = "INTEGER NOT NULL", "INTEGER"
	case engineClickHouse:
		integer, nullable, text, suffix = "Int64", "Nullable(Int64)", "Nullable(String)", " ENGINE = Memory"
	}
	return []string{
		fmt.Sprintf("CREATE TABLE %s (id %s, grp %s, a %s, b %s, label %s)%s;", itemTable, integer, integer, nullable, nullable, text, suffix),
		fmt.Sprintf("CREATE TABLE %s (id %s, item_id %s, c %s)%s;", peerTable, integer, nullable, nullable, suffix),
	}
}

// Queries returns the engine's catalogue as one query file.
//
// Takes engine (string) which names the dialect.
//
// Returns string which holds every catalogue query with its directive.
func Queries(engine string) string {
	var file strings.Builder
	for _, entry := range catalogue(engine) {
		mode := ""
		if entry.dynamic {
			mode = ", dynamic: runtime"
		}
		fmt.Fprintf(&file, "-- sgl.query(%s, many%s)\n%s;\n\n", entry.name, mode, entry.sql)
	}
	return file.String()
}

// QueryCount returns the number of the engine's catalogue queries.
//
// Takes engine (string) which names the dialect.
//
// Returns int which is the number of generated methods required.
func QueryCount(engine string) int {
	return len(catalogue(engine))
}

// catalogue lists the engine's queries.
//
// Takes engine (string) which names the dialect.
//
// Returns []query which holds the partition queries of every predicate and the queries of
// every other relation the engine admits.
func catalogue(engine string) []query {
	k := placeholder(engine, 1, "k", "Int64")
	var entries []query
	for index, current := range predicates {
		condition := current.sql(k)
		suffix := strconv.Itoa(index)
		entries = append(entries,
			query{name: "SemWhere" + suffix, sql: itemProjection + whereKeyword + condition, dynamic: false},
			query{name: "SemNot" + suffix, sql: itemProjection + " WHERE NOT (" + condition + closing, dynamic: false},
			query{name: "SemNull" + suffix, sql: itemProjection + " WHERE (" + condition + ") IS NULL", dynamic: false},
			query{name: "SemAggWhere" + suffix, sql: aggregateProjection + whereKeyword + condition, dynamic: false},
			query{name: "SemAggNot" + suffix, sql: aggregateProjection + " WHERE NOT (" + condition + closing, dynamic: false},
			query{name: "SemAggNull" + suffix, sql: aggregateProjection + " WHERE (" + condition + ") IS NULL", dynamic: false},
			query{name: "SemCteCount" + suffix, sql: "WITH f AS (SELECT i.id FROM " + itemTable + " i WHERE " + condition + ") " + countProjection + "f",
				dynamic: false},
			query{name: "SemDerivedCount" + suffix, sql: countProjection + "(SELECT i.id FROM " + itemTable + " i WHERE NOT (" + condition + ")) AS d",
				dynamic: false})
	}
	entries = append(entries, query{name: methodAll, sql: itemProjection, dynamic: false},
		query{name: methodAggregateAll, sql: aggregateProjection, dynamic: false})
	entries = append(entries, windowQueries(engine)...)
	entries = append(entries, joinQueries(engine, k)...)
	entries = append(entries, compoundQueries(engine)...)
	limit, offset := placeholder(engine, 1, "lim", "UInt64"), placeholder(engine, 2, "off", "UInt64")
	entries = append(entries,
		query{name: methodPageFull, sql: pageProjection + " ORDER BY i.a, i.id", dynamic: false},
		query{name: methodPage, sql: pageProjection + " ORDER BY i.a, i.id LIMIT " + limit + " OFFSET " + offset, dynamic: false},
		query{name: methodBuilder, sql: pageProjection, dynamic: true},
		query{name: methodLimitBuilder, sql: pageProjection + " ORDER BY i.a, i.id LIMIT 3 OFFSET 1", dynamic: true})
	if engine == engineClickHouse {
		entries = append(entries, query{name: methodLimitByBuilder, sql: pageProjection + " ORDER BY i.a, i.id LIMIT 1 BY i.grp", dynamic: true})
	}
	return entries
}

// windowQueries lists the window relation's queries.
//
// Takes engine (string) which names the dialect.
//
// Returns []query which numbers, counts, totals and links each group.
func windowQueries(engine string) []query {
	previous := "LAG(i.id) OVER (PARTITION BY i.grp ORDER BY i.id)"
	if engine == engineClickHouse {
		previous = "lagInFrame(toNullable(i.id)) OVER (PARTITION BY i.grp ORDER BY i.id ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW)"
	}
	return []query{
		{name: methodWindow, dynamic: false, sql: "SELECT i.id, i.grp, row_number() OVER (PARTITION BY i.grp ORDER BY i.a, i.id) AS rn, " +
			"COUNT(*) OVER (PARTITION BY i.grp) AS n, " +
			"SUM(i.a) OVER (PARTITION BY i.grp ORDER BY i.id ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) AS running, " +
			"SUM(i.a) OVER (PARTITION BY i.grp) AS total, " + previous + " AS previous FROM " + itemTable + " i"},
		{name: methodGroupSum, dynamic: false, sql: "SELECT i.grp, COUNT(*) AS n, SUM(i.a) AS total FROM " + itemTable + " i GROUP BY i.grp"},
		{name: methodWindowBuilder, dynamic: true, sql: "SELECT i.id, i.grp, row_number() OVER (PARTITION BY i.grp ORDER BY i.id) AS rn FROM " +
			itemTable + " i"},
	}
}

// joinQueries lists the join and semi-join relations' queries.
//
// Takes engine (string) which names the dialect.
// Takes k (string) which is the parameter placeholder.
//
// Returns []query which holds the formulations the engine admits.
func joinQueries(engine, k string) []query {
	entries := []query{
		{name: methodLeftJoin, dynamic: false, sql: "SELECT i.id, p.id AS peer_id, p.c FROM " + itemTable + " i LEFT JOIN " + peerTable +
			" p ON p.item_id = i.id"},
		{name: methodInnerCount, dynamic: false, sql: countProjection + itemTable + " i JOIN " + peerTable + " p ON p.item_id = i.id"},
		{name: methodAntiCount, dynamic: false, sql: countProjection + itemTable + " i WHERE i.id NOT IN (SELECT p.item_id FROM " + peerTable +
			" p WHERE p.item_id IS NOT NULL" + closing},
		{name: methodOrphanCount, dynamic: false, sql: countProjection + peerTable + " p WHERE p.item_id IS NULL OR p.item_id NOT IN (SELECT i.id FROM " +
			itemTable + " i)"},
		{name: methodSemiIn, dynamic: false, sql: "SELECT i.id FROM " + itemTable + " i WHERE i.id IN (SELECT p.item_id FROM " + peerTable +
			" p WHERE p.c > " + k + closing},
		{name: methodSemiJoin, dynamic: false, sql: "SELECT DISTINCT i.id FROM " + itemTable + " i JOIN " + peerTable +
			" p ON p.item_id = i.id WHERE p.c > " + k},
	}
	if admits(engine, featureFullJoin) {
		entries = append(entries, query{name: methodFullJoin, dynamic: false, sql: "SELECT i.id, p.id AS peer_id FROM " + itemTable +
			" i FULL JOIN " + peerTable + " p ON p.item_id = i.id"})
	}
	if admits(engine, featureCorrelated) {
		entries = append(entries, query{name: methodSemiExists, dynamic: false, sql: "SELECT i.id FROM " + itemTable +
			" i WHERE EXISTS (SELECT 1 FROM " + peerTable + " p WHERE p.item_id = i.id AND p.c > " + k + closing})
	}
	return entries
}

// compoundQueries lists the compound relations' queries.
//
// ClickHouse needs UNION DISTINCT, as a bare UNION depends on union_default_mode. Its
// bare INTERSECT and EXCEPT keep the first branch's duplicates, so it also spells their
// DISTINCT forms.
//
// Takes engine (string) which names the dialect.
//
// Returns []query which holds the branches and their combinations.
func compoundQueries(engine string) []query {
	distinct := ""
	if engine == engineClickHouse {
		distinct = " DISTINCT"
	}
	entries := []query{
		{name: methodBranchItems, sql: itemBranch, dynamic: false},
		{name: methodBranchPeers, sql: peerBranch, dynamic: false},
		{name: methodUnionAll, sql: itemBranch + " UNION ALL " + peerBranch, dynamic: false},
		{name: methodUnion, sql: itemBranch + " UNION" + distinct + " " + peerBranch, dynamic: false},
		{name: methodUnionDistinct, sql: "SELECT DISTINCT u.v FROM (" + itemBranch + " UNION ALL " + peerBranch + ") AS u", dynamic: false},
		{name: methodIntersect, sql: itemBranch + " INTERSECT " + peerBranch, dynamic: false},
		{name: methodExcept, sql: itemBranch + " EXCEPT " + peerBranch, dynamic: false},
	}
	if engine == engineClickHouse {
		entries = append(entries, query{name: methodIntersectDistinct, sql: itemBranch + " INTERSECT DISTINCT " + peerBranch, dynamic: false},
			query{name: methodExceptDistinct, sql: itemBranch + " EXCEPT DISTINCT " + peerBranch, dynamic: false})
	}
	return entries
}

// placeholder writes the engine's spelling of a parameter.
//
// Takes engine (string) which names the dialect.
// Takes number (int) which is the parameter's position.
// Takes name (string) which names a ClickHouse parameter.
// Takes clickHouseType (string) which types a ClickHouse parameter.
//
// Returns string which is the native placeholder.
func placeholder(engine string, number int, name, clickHouseType string) string {
	switch engine {
	case engineMySQL, engineMariaDB:
		return "?"
	case engineSQLite:
		return "?" + strconv.Itoa(number)
	case engineClickHouse:
		return "{" + name + ":" + clickHouseType + "}"
	default:
		return "$" + strconv.Itoa(number)
	}
}
