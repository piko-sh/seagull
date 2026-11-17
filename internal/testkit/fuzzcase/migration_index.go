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

package fuzzcase

import "fmt"

// indexMigrationVersion adds dependent index operations while preserving earlier
// versions.
const indexMigrationVersion = 6

const (
	// indexMigrationOperations fixes the number of assigned interaction operations.
	indexMigrationOperations = 8

	// indexMigrationSingle selects an index containing only the mutable column.
	indexMigrationSingle = 8

	// indexMigrationUnique selects a unique index.
	indexMigrationUnique = 16
)

const (
	// indexCreate creates or revisits the main index.
	indexCreate byte = iota

	// indexDropColumn drops its mutable key or first removes a blocking index.
	indexDropColumn

	// indexRenameColumn changes the mutable key name.
	indexRenameColumn

	// indexRenameTable changes the index owner's name.
	indexRenameTable

	// indexDrop removes the main index or first creates it.
	indexDrop

	// indexPeer toggles the peer index under native namespace rules.
	indexPeer

	// indexCreateUnique requests a unique definition without replacing an existing index.
	indexCreateUnique

	// indexDropTable drops the owner and its dependent index.
	indexDropTable
)

const (
	// indexMutableColumn names the original mutable key.
	indexMutableColumn = "category"

	// indexMigrationReverse reverses compound key order.
	indexMigrationReverse = 32
)

// indexMigrationState tracks dependencies while emitting valid schema operations.
type indexMigrationState struct {
	// engine selects SQL spelling and dependency rules.
	engine string

	// table is the current name of the main table.
	table string

	// column is the mutable column name or empty after its removal.
	column string

	// indexed reports an explicit index on the main table.
	indexed bool

	// usesColumn reports whether that index includes the mutable column.
	usesColumn bool

	// columnOnly distinguishes a single mutable key from a compound index.
	columnOnly bool

	// unique records the installed index's uniqueness constraint.
	unique bool

	// peerIndexed reports the same named index on the peer table.
	peerIndexed bool
}

// statement applies one stable interaction operation to the model state.
//
// Takes choice (byte) which selects an operation and optional key attributes.
//
// Returns string which is valid under the updated dependency state.
func (state *indexMigrationState) statement(choice byte) string {
	if state.table == "" {
		return state.createTable()
	}
	switch choice % indexMigrationOperations {
	case indexCreate, indexCreateUnique:
		return state.createIndex(choice)
	case indexDropColumn:
		return state.dropColumn()
	case indexRenameColumn:
		return state.renameColumn()
	case indexRenameTable:
		if state.engine == engineDuckDB && state.indexed {
			return state.dropIndex()
		}
		next := "fuzz_items_renamed"
		if state.table == next {
			next = "fuzz_items"
		}
		statement := migrationAlterTable + state.table + " RENAME TO " + next + migrationTerminator
		state.table = next
		return statement
	case indexDrop:
		if !state.indexed {
			return state.createIndex(choice)
		}
		return state.dropIndex()
	case indexPeer:
		return state.peerIndex(choice)
	default:
		statement := "DROP TABLE " + state.table + migrationTerminator
		state.table, state.column, state.indexed = "", "", false
		state.usesColumn, state.columnOnly, state.unique = false, false, false
		return statement
	}
}

// createTable restores the main table after a drop.
//
// Returns string which recreates its original visible columns.
func (state *indexMigrationState) createTable() string {
	state.table, state.column = "fuzz_items", indexMutableColumn
	integer := "BIGINT"
	if state.engine == engineSQLite {
		integer = "INTEGER"
	}
	return fmt.Sprintf("CREATE TABLE fuzz_items (id %s NOT NULL, score %s, category %s NOT NULL);", integer, integer, integer)
}

// tableScoped reports whether two owners can use the same index name.
//
// Returns bool which selects the independently modelled namespace rule.
func (state *indexMigrationState) tableScoped() bool {
	return mysqlFamily(state.engine) || state.engine == engineCockroach
}

// createIndex creates or safely revisits the main table's named index.
//
// Takes choice (byte) which selects uniqueness and key width.
//
// Returns string which respects existing index ownership.
func (state *indexMigrationState) createIndex(choice byte) string {
	if state.indexed && state.engine == engineMySQL {
		return state.dropIndex()
	}
	keys := "id"
	usesColumn, columnOnly := state.column != "", choice&indexMigrationSingle != 0
	if usesColumn {
		keys = state.column
		if !columnOnly {
			if choice&indexMigrationReverse != 0 {
				keys = "id, " + keys
			} else {
				keys += ", id"
			}
		}
	}
	unique := choice&indexMigrationUnique != 0 || choice%indexMigrationOperations == indexCreateUnique
	if !state.indexed && (!state.peerIndexed || state.tableScoped()) {
		state.indexed, state.usesColumn, state.columnOnly = true, usesColumn, columnOnly
		state.unique = unique
	}
	return state.indexSQL(state.table, keys, unique)
}

// indexSQL writes a named index with the engine's idempotent spelling.
//
// Takes table (string) which owns the index.
// Takes keys (string) which lists independently chosen keys.
// Takes unique (bool) which selects the uniqueness constraint.
//
// Returns string which creates the model's named index.
func (state *indexMigrationState) indexSQL(table, keys string, unique bool) string {
	qualifier, conditional := "", "IF NOT EXISTS "
	if unique {
		qualifier = "UNIQUE "
	}
	if state.engine == engineMySQL {
		conditional = ""
	}
	return "CREATE " + qualifier + "INDEX " + conditional + "fuzz_model_index ON " + table + " (" + keys + ");"
}

// dropIndex removes the main table's explicit index before a dependent operation.
//
// Returns string which names the correct owner for table-scoped dialects.
func (state *indexMigrationState) dropIndex() string {
	state.indexed, state.usesColumn, state.columnOnly, state.unique = false, false, false, false
	return state.dropIndexSQL(state.table)
}

// dropIndexSQL writes a drop with unambiguous ownership.
//
// Takes table (string) which identifies the index owner.
//
// Returns string which uses the engine's supported qualification syntax.
func (state *indexMigrationState) dropIndexSQL(table string) string {
	switch state.engine {
	case engineMySQL, engineMariaDB:
		return "DROP INDEX fuzz_model_index ON " + table + migrationTerminator
	case engineCockroach:
		return "DROP INDEX " + table + "@fuzz_model_index;"
	default:
		return "DROP INDEX fuzz_model_index;"
	}
}

// peerIndex exercises duplicate names across two owners.
//
// Takes choice (byte) which optionally requests uniqueness.
//
// Returns string which creates, ignores or drops the peer index under native rules.
func (state *indexMigrationState) peerIndex(choice byte) string {
	if state.peerIndexed {
		state.peerIndexed = false
		return state.dropIndexSQL("fuzz_peers")
	}
	state.peerIndexed = !state.indexed || state.tableScoped()
	return state.indexSQL("fuzz_peers", "id", choice&indexMigrationUnique != 0)
}

// dropColumn updates dependent indexes or first removes a blocking index.
//
// Returns string which preserves a valid operation sequence on every supported engine.
func (state *indexMigrationState) dropColumn() string {
	if state.column == "" {
		return state.addColumn()
	}
	if state.engine == engineDuckDB && state.indexed {
		return state.dropIndex()
	}
	if state.indexed && state.usesColumn {
		if state.engine == engineSQLite || state.engine == engineMariaDB && state.unique && !state.columnOnly {
			return state.dropIndex()
		}
		state.indexed = mysqlFamily(state.engine) && !state.columnOnly
		state.unique = state.unique && state.indexed
		state.usesColumn, state.columnOnly = false, false
	}
	statement := migrationAlterTable + state.table + " DROP COLUMN " + state.column + migrationTerminator
	state.column = ""
	return statement
}

// addColumn restores the mutable column when an operation needs it.
//
// Returns string which appends the model column without an index.
func (state *indexMigrationState) addColumn() string {
	state.column = indexMutableColumn
	integer := "BIGINT"
	if state.engine == engineSQLite {
		integer = "INTEGER"
	}
	return migrationAlterTable + state.table + " ADD COLUMN category " + integer + migrationTerminator
}

// renameColumn preserves index dependencies while changing a key name.
//
// Returns string which removes a blocking DuckDB index before attempting the rename.
func (state *indexMigrationState) renameColumn() string {
	if state.column == "" {
		return state.addColumn()
	}
	if state.engine == engineDuckDB && state.indexed {
		return state.dropIndex()
	}
	next := "renamed"
	if state.column == next {
		next = indexMutableColumn
	}
	statement := migrationAlterTable + state.table + " RENAME COLUMN " + state.column + " TO " + next + migrationTerminator
	state.column = next
	return statement
}

// indexMigrationSeeds covers key deletion, object scope and dependent renames.
//
// Takes engine (string) which selects supported index syntax.
//
// Returns [][]byte which retains the assigned interaction operations.
func indexMigrationSeeds(engine string) [][]byte {
	if engine == engineClickHouse {
		return nil
	}
	return [][]byte{
		{indexMigrationVersion},
		{indexMigrationVersion, indexCreate, indexDropColumn, indexDropColumn},
		{indexMigrationVersion, indexMigrationSingle, indexDropColumn},
		{indexMigrationVersion, indexMigrationUnique, indexDropColumn, indexDropColumn},
		{indexMigrationVersion, indexMigrationUnique | indexMigrationReverse, indexDropColumn, indexDropColumn},
		{indexMigrationVersion, indexMigrationSingle | indexMigrationUnique, indexDropColumn},
		{indexMigrationVersion, indexCreate, indexPeer, indexDrop, indexPeer, indexDrop},
		{indexMigrationVersion, indexCreate, indexRenameColumn, indexRenameTable, indexDrop},
		{indexMigrationVersion, indexCreate, indexDropTable, indexCreate, indexPeer, indexCreateUnique},
		{indexMigrationVersion, indexCreate, indexCreateUnique},
		{indexMigrationVersion, indexCreate, indexCreateUnique, indexDropColumn},
		{indexMigrationVersion, indexCreate, indexRenameTable, indexRenameTable, indexPeer, indexDrop},
		{indexMigrationVersion, indexMigrationReverse, indexDropColumn},
		{indexMigrationVersion, indexRenameColumn, indexDropColumn, indexCreateUnique, indexCreateUnique, indexDropColumn, indexDropColumn},
		{indexMigrationVersion, indexDropColumn, indexCreate, indexDropColumn, indexRenameColumn},
	}
}

// decodeIndexMigration emits bounded operations on indexed tables.
//
// Takes engine (string) which selects dependency and namespace semantics.
// Takes data ([]byte) which has already passed envelope validation.
//
// Returns Migration which supplies one statement per bounded operation.
// Returns bool which excludes ordinary indexes on the ClickHouse Memory fixture.
func decodeIndexMigration(engine string, data []byte) (Migration, bool) {
	if engine == engineClickHouse {
		return Migration{}, false
	}
	operations := data[1:]
	if len(operations) == 0 {
		operations = []byte{indexCreate}
	}
	operations = operations[:min(len(operations), migrationStepBound)]
	state := indexMigrationState{engine: engine, table: "fuzz_items", column: indexMutableColumn, indexed: false, usesColumn: false, columnOnly: false, unique: false, peerIndexed: false}
	model := Migration{Engine: engine, Steps: make([]string, 0, len(operations))}
	for _, operation := range operations {
		model.Steps = append(model.Steps, state.statement(operation))
	}
	return model, true
}
