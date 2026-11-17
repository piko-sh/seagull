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

// migrationVersion selects the stable schema-operation decoder.
const migrationVersion = 4

const (
	// migrationCreateTable starts or resets the scratch table.
	migrationCreateTable byte = iota

	// migrationAddColumn adds a nullable scratch column.
	migrationAddColumn

	// migrationRenameColumn changes the scratch column's name.
	migrationRenameColumn

	// migrationDropColumn removes the scratch column.
	migrationDropColumn

	// migrationRenameTable changes the scratch table's name.
	migrationRenameTable

	// migrationDropTable removes the scratch table.
	migrationDropTable

	// migrationCreateIndex starts the secondary-index lifecycle.
	migrationCreateIndex

	// migrationDropIndex ends the secondary-index lifecycle.
	migrationDropIndex

	// migrationCreateView starts the projection-view lifecycle.
	migrationCreateView

	// migrationDropView ends the projection-view lifecycle.
	migrationDropView

	// migrationOperationCount fixes the assigned schema operation identifiers.
	migrationOperationCount
)

// migrationStepBound bounds replay checkpoints per input.
const migrationStepBound = 8

// migrationTerminator ends one generated DDL statement.
const migrationTerminator = ";"

// migrationAlterTable introduces a scratch-table alteration.
const migrationAlterTable = "ALTER TABLE "

// Migration describes ordered statements applied to the initial model schema.
type Migration struct {
	// Engine selects the schema syntax and capabilities.
	Engine string

	// Steps contains at most eight individually checked operations.
	Steps []string
}

// migrationState tracks dependencies needed to construct valid DDL.
type migrationState struct {
	// table names the current scratch table or is empty when absent.
	table string

	// extra names the added column or is empty when absent.
	extra string

	// indexed reports the explicitly created secondary index.
	indexed bool

	// viewed reports the explicitly created projection view.
	viewed bool
}

// statement advances dependencies and writes one supported schema operation.
//
// Takes engine (string) which selects engine-specific DDL spelling.
// Takes operation (byte) which names a stable schema operation.
//
// Returns string which is a valid statement in the updated model state.
func (state *migrationState) statement(engine string, operation byte) string {
	if operation >= migrationCreateIndex {
		return state.auxiliary(engine, operation)
	}
	if state.table == "" {
		return state.create(engine)
	}
	switch operation {
	case migrationCreateTable, migrationDropTable:
		statement := "DROP TABLE " + state.table + migrationTerminator
		state.table, state.extra = "", ""
		return statement
	case migrationAddColumn:
		if state.extra != "" {
			return state.dropColumn()
		}
		return state.addColumn(engine)
	case migrationRenameColumn:
		if state.extra == "" {
			return state.addColumn(engine)
		}
		next := "renamed"
		if state.extra == next {
			next = "extra"
		}
		statement := migrationAlterTable + state.table + " RENAME COLUMN " + state.extra + " TO " + next + migrationTerminator
		state.extra = next
		return statement
	case migrationDropColumn:
		if state.extra == "" {
			return state.addColumn(engine)
		}
		return state.dropColumn()
	default:
		next := "fuzz_stage_renamed"
		if state.table == next {
			next = "fuzz_stage"
		}
		statement := migrationAlterTable + state.table + " RENAME TO " + next + migrationTerminator
		if engine == engineClickHouse {
			statement = "RENAME TABLE " + state.table + " TO " + next + migrationTerminator
		}
		state.table = next
		return statement
	}
}

// addColumn introduces the nullable scratch column.
//
// Takes engine (string) which selects its integer type.
//
// Returns string which adds one bounded column.
func (state *migrationState) addColumn(engine string) string {
	state.extra = "extra"
	integer := "BIGINT"
	if engine == engineSQLite {
		integer = "INTEGER"
	}
	if engine == engineClickHouse {
		integer = "Nullable(Int64)"
	}
	return migrationAlterTable + state.table + " ADD COLUMN extra " + integer + migrationTerminator
}

// create introduces the scratch table when a column operation has no target.
//
// Takes engine (string) which selects SQL types and table storage.
//
// Returns string which creates a third bounded table.
func (state *migrationState) create(engine string) string {
	state.table, state.extra = "fuzz_stage", ""
	integer, nullable, suffix := scratchColumnTypes(engine)
	return fmt.Sprintf("CREATE TABLE fuzz_stage (id %s NOT NULL, score %s)%s;", integer, nullable, suffix)
}

// dropColumn removes the currently added column from the scratch table.
//
// Returns string which names the existing column before clearing its model state.
func (state *migrationState) dropColumn() string {
	statement := migrationAlterTable + state.table + " DROP COLUMN " + state.extra + migrationTerminator
	state.extra = ""
	return statement
}

// auxiliary creates or drops an index or view without disturbing table dependencies.
//
// Takes engine (string) which selects DROP INDEX syntax.
// Takes operation (byte) which selects an index or view family.
//
// Returns string which toggles the requested auxiliary object.
func (state *migrationState) auxiliary(engine string, operation byte) string {
	if operation >= migrationCreateView {
		state.viewed = !state.viewed
		if state.viewed {
			return "CREATE VIEW fuzz_view AS SELECT id, score FROM fuzz_items;"
		}
		return "DROP VIEW fuzz_view;"
	}
	state.indexed = !state.indexed
	if state.indexed {
		return "CREATE INDEX fuzz_model_index ON fuzz_items (category, id);"
	}
	if mysqlFamily(engine) {
		return "DROP INDEX fuzz_model_index ON fuzz_items;"
	}
	return "DROP INDEX fuzz_model_index;"
}

// MigrationSeeds covers supported schema operations and interacting sequences.
//
// Takes engine (string) which selects index support.
//
// Returns [][]byte which includes table, column, index and view changes.
func MigrationSeeds(engine string) [][]byte {
	seeds := [][]byte{
		{migrationVersion},
		{migrationVersion, migrationCreateTable, migrationAddColumn, migrationRenameColumn, migrationDropColumn, migrationRenameTable, migrationDropTable},
		{migrationVersion, migrationCreateView, migrationDropView},
		{migrationVersion, migrationCreateTable, migrationCreateView, migrationAddColumn, migrationRenameTable, migrationRenameColumn, migrationDropView, migrationDropColumn, migrationDropTable},
	}
	if engine != engineClickHouse {
		seeds = append(seeds,
			[]byte{migrationVersion, migrationCreateIndex, migrationDropIndex},
			[]byte{migrationVersion, migrationCreateTable, migrationCreateIndex, migrationCreateView, migrationRenameTable, migrationDropView, migrationDropIndex, migrationDropTable})
	}
	seeds = append(seeds, indexMigrationSeeds(engine)...)
	return append(seeds, indexScopeMigrationSeeds(engine)...)
}

// DecodeMigration constructs bounded valid operation sequences independently of replay.
//
// Takes engine (string) which selects supported SQL syntax.
// Takes data ([]byte) which holds the version followed by fixed operation identifiers.
//
// Returns Migration which supplies SQL after the original two-table schema.
// Returns bool which is false for unsupported envelopes or index operations.
func DecodeMigration(engine string, data []byte) (Migration, bool) {
	if len(data) == 0 || len(data) > MaxBytes || !knownMigrationVersion(data[0]) || !supportsShape(engine, shapeSelect) {
		return Migration{}, false
	}
	if data[0] == indexMigrationVersion {
		return decodeIndexMigration(engine, data)
	}
	if data[0] == indexScopeMigrationVersion {
		return decodeIndexScopeMigration(engine, data)
	}
	operations := data[1:]
	if len(operations) == 0 {
		operations = []byte{0}
	}
	operations = operations[:min(len(operations), migrationStepBound)]
	model := Migration{Engine: engine, Steps: make([]string, 0, len(operations))}
	state := migrationState{table: "", extra: "", indexed: false, viewed: false}
	for _, operation := range operations {
		operation %= migrationOperationCount
		if engine == engineClickHouse && (operation == migrationCreateIndex || operation == migrationDropIndex) {
			return Migration{}, false
		}
		model.Steps = append(model.Steps, state.statement(engine, operation))
	}
	return model, true
}

// knownMigrationVersion reports whether a version byte selects a migration decoder.
//
// Takes version (byte) which is the first input byte.
//
// Returns bool which is true for the plain, index and index scope versions.
func knownMigrationVersion(version byte) bool {
	return version == migrationVersion || version == indexMigrationVersion || version == indexScopeMigrationVersion
}
