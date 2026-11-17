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

// indexScopeMigrationVersion adds relation collisions and multiple-index drops.
const indexScopeMigrationVersion = 7

const (
	// indexScopeFirst creates the first independently named index.
	indexScopeFirst byte = iota

	// indexScopeSecond creates an index on the second owner.
	indexScopeSecond

	// indexScopeDropBoth drops both independently named indexes.
	indexScopeDropBoth

	// indexScopeTable creates a relation sharing the collision name.
	indexScopeTable

	// indexScopeView creates a view sharing the collision name.
	indexScopeView

	// indexScopeDropRelation removes the collision relation.
	indexScopeDropRelation

	// indexScopeCreateIndex creates the collision index conditionally.
	indexScopeCreateIndex

	// indexScopeDropIndex removes the collision index.
	indexScopeDropIndex

	// indexScopeMigrationOperations fixes the assigned namespace operation count.
	indexScopeMigrationOperations
)

// indexScopeCreatePrefix introduces a namespace object's creation.
const indexScopeCreatePrefix = "CREATE "

// indexScopeSequence selects a sequence for the relation creation operation.
const indexScopeSequence = 64

// indexScopeState tracks collisions between explicit indexes and relations.
type indexScopeState struct {
	// engine selects index qualification and namespace rules.
	engine string

	// relation names the existing collision object's SQL kind.
	relation string

	// indexed reports an installed index sharing the collision object's name.
	indexed bool
}

// statement applies one stable namespace operation.
//
// Takes choice (byte) which selects an operation and optional uniqueness.
//
// Returns string which preserves a valid dependency state.
func (state *indexScopeState) statement(choice byte) string {
	unique := ""
	if choice&indexMigrationUnique != 0 {
		unique = "UNIQUE "
	}
	switch choice % indexScopeMigrationOperations {
	case indexScopeFirst:
		return indexScopeCreatePrefix + unique + "INDEX IF NOT EXISTS fuzz_first_index ON fuzz_items (category, id);"
	case indexScopeSecond:
		return indexScopeCreatePrefix + unique + "INDEX IF NOT EXISTS fuzz_second_index ON fuzz_peers (id);"
	case indexScopeDropBoth:
		return "DROP INDEX IF EXISTS " + state.qualified("fuzz_items", "fuzz_first_index") + ", " + state.qualified("fuzz_peers", "fuzz_second_index") + migrationTerminator
	case indexScopeTable:
		if choice&indexScopeSequence != 0 {
			return state.createRelation("SEQUENCE")
		}
		return state.createRelation("TABLE")
	case indexScopeView:
		return state.createRelation("VIEW")
	case indexScopeDropRelation:
		if state.relation == "" {
			return state.createRelation("TABLE")
		}
		return state.dropRelation()
	case indexScopeCreateIndex:
		return state.createIndex(unique)
	default:
		if !state.indexed {
			return state.createIndex(unique)
		}
		return state.dropIndex()
	}
}

// qualified writes an index name with its owner when required.
//
// Takes table (string) which owns the index.
// Takes index (string) which names the index.
//
// Returns string which follows the engine's index namespace syntax.
func (state *indexScopeState) qualified(table, index string) string {
	if state.engine == engineCockroach {
		return table + "@" + index
	}
	return index
}

// createRelation creates the requested object after removing name conflicts.
//
// Takes kind (string) which identifies a table, view or sequence.
//
// Returns string which changes one relation or first clears a dependency.
func (state *indexScopeState) createRelation(kind string) string {
	if state.relation != "" {
		return state.dropRelation()
	}
	if state.indexed && state.engine != engineCockroach {
		return state.dropIndex()
	}
	state.relation = kind
	suffix := ""
	switch kind {
	case "TABLE":
		suffix = " (id BIGINT)"
	case "VIEW":
		suffix = " AS SELECT id FROM fuzz_peers"
	}
	return indexScopeCreatePrefix + kind + " fuzz_model_index" + suffix + migrationTerminator
}

// dropRelation removes the named collision object without changing other tables.
//
// Returns string which uses the object's original kind.
func (state *indexScopeState) dropRelation() string {
	kind := state.relation
	state.relation = ""
	return "DROP " + kind + " fuzz_model_index;"
}

// createIndex requests an index without replacing an existing relation or index.
//
// Takes unique (string) which selects the independently generated constraint.
//
// Returns string which exercises the engine's conditional namespace lookup.
func (state *indexScopeState) createIndex(unique string) string {
	state.indexed = state.relation == "" || state.engine == engineCockroach
	return indexScopeCreatePrefix + unique + "INDEX IF NOT EXISTS fuzz_model_index ON fuzz_items (category, id);"
}

// dropIndex removes the collision index using explicit ownership.
//
// Returns string which leaves the independent relation unchanged.
func (state *indexScopeState) dropIndex() string {
	state.indexed = false
	return "DROP INDEX " + state.qualified("fuzz_items", "fuzz_model_index") + migrationTerminator
}

// supportsIndexScopeMigration identifies engines with multiple-index DROP syntax.
//
// Takes engine (string) which selects the SQL grammar.
//
// Returns bool which enables the independently verified engine profiles.
func supportsIndexScopeMigration(engine string) bool {
	return postgresFamily(engine)
}

// indexScopeMigrationSeeds covers multiple drops and relation namespace collisions.
//
// Takes engine (string) which selects supported operations.
//
// Returns [][]byte which retains stable operation identifiers.
func indexScopeMigrationSeeds(engine string) [][]byte {
	if !supportsIndexScopeMigration(engine) {
		return nil
	}
	return [][]byte{
		{indexScopeMigrationVersion},
		{indexScopeMigrationVersion, indexScopeFirst, indexScopeSecond, indexScopeDropBoth, indexScopeDropBoth},
		{indexScopeMigrationVersion, indexMigrationUnique, indexMigrationUnique | indexScopeSecond, indexScopeDropBoth, indexScopeFirst, indexScopeSecond, indexScopeDropBoth},
		{indexScopeMigrationVersion, indexScopeTable, indexScopeCreateIndex, indexScopeDropRelation, indexScopeCreateIndex, indexScopeDropIndex},
		{indexScopeMigrationVersion, indexScopeView, indexScopeCreateIndex, indexScopeDropRelation, indexScopeCreateIndex, indexScopeDropIndex},
		{indexScopeMigrationVersion, indexScopeSequence | indexScopeTable, indexScopeCreateIndex, indexScopeDropRelation, indexScopeCreateIndex, indexScopeDropIndex},
		{indexScopeMigrationVersion, indexScopeCreateIndex, indexScopeTable, indexScopeTable, indexScopeCreateIndex, indexScopeDropRelation, indexScopeDropIndex},
	}
}

// decodeIndexScopeMigration emits bounded namespace and index-list interactions.
//
// Takes engine (string) which selects independently verified grammar.
// Takes data ([]byte) which has passed envelope validation.
//
// Returns Migration which supplies one statement per operation.
// Returns bool which excludes unsupported multiple-index syntax.
func decodeIndexScopeMigration(engine string, data []byte) (Migration, bool) {
	if !supportsIndexScopeMigration(engine) {
		return Migration{}, false
	}
	operations := data[1:]
	if len(operations) == 0 {
		operations = []byte{indexScopeFirst}
	}
	operations = operations[:min(len(operations), migrationStepBound)]
	state := indexScopeState{engine: engine, relation: "", indexed: false}
	model := Migration{Engine: engine, Steps: make([]string, 0, len(operations))}
	for _, operation := range operations {
		model.Steps = append(model.Steps, state.statement(operation))
	}
	return model, true
}
