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

import (
	"fmt"
	"slices"
	"strings"
)

// lifecycleVersion is the first byte of a version 20 migration history.
//
// A history holds up to twelve migrations with gaps, version 0 and large versions,
// migrations revealed below the applied ones, several seed files, encodings, transaction
// directives and failures injected in up and down files at a chosen statement.
const lifecycleVersion = 20

const (
	// lifecycleMaxMigrations bounds the migrations of one history.
	lifecycleMaxMigrations = 12

	// lifecycleMaxSteps bounds the operations of one history.
	lifecycleMaxSteps = 16

	// lifecycleMaxInserts bounds the rows an up file inserts after its CREATE TABLE.
	lifecycleMaxInserts = 3

	// lifecycleMaxSeeds bounds the seed files.
	lifecycleMaxSeeds = 3

	// lifecycleHeader is how many bytes precede the migrations' bytes.
	lifecycleHeader = 4

	// lifecycleSeedRowBase numbers the rows the seeds insert, apart from the migrations'.
	lifecycleSeedRowBase = 100

	// LifecycleGate is the table an injected failure inserts into. It exists only once a fix
	// step creates it.
	LifecycleGate = "fuzz_life_gate"

	// lifecycleDeclarationMarker stands for a created table's declaration in a statement.
	lifecycleDeclarationMarker = " %s"
)

const (
	// lifecycleNameUp names the up operation.
	lifecycleNameUp = "up"

	// lifecycleNameUpTo names the up-to operation.
	lifecycleNameUpTo = "up-to"

	// lifecycleNameDown names the down operation.
	lifecycleNameDown = "down"

	// lifecycleNameDownTo names the down-to operation.
	lifecycleNameDownTo = "down-to"

	// lifecycleNameSeed names the seed operation.
	lifecycleNameSeed = "seed"

	// lifecycleNameValidate names the validate operation.
	lifecycleNameValidate = "validate"

	// lifecycleNameReveal names the reveal operation.
	lifecycleNameReveal = "reveal"

	// lifecycleNameFix names the fix operation.
	lifecycleNameFix = "fix"
)

const (
	// lifecycleUp applies every pending migration.
	lifecycleUp byte = iota

	// lifecycleUpTo applies the pending migrations at or below a version.
	lifecycleUpTo

	// lifecycleDown rolls back the newest one or two applied migrations.
	lifecycleDown

	// lifecycleDownTo rolls back the applied migrations above a version.
	lifecycleDownTo

	// lifecycleSeed applies the seeds not yet applied.
	lifecycleSeed

	// lifecycleValidate compares the applied migrations with their files.
	lifecycleValidate

	// lifecycleReveal writes the hidden migrations' files.
	lifecycleReveal

	// lifecycleFix creates the gate table, so injected failures pass.
	lifecycleFix

	// lifecycleOperations is how many operations a byte chooses from.
	lifecycleOperations
)

const (
	// lifecycleCountMask selects the migration count from the first header byte.
	lifecycleCountMask = 0x0f

	// lifecycleSeedsShift places the seed file count in the first header byte.
	lifecycleSeedsShift = 4

	// lifecycleInsertsMask selects a migration's insert count from its byte.
	lifecycleInsertsMask = 0x03

	// lifecycleEncodingShift places a migration's encoding in its byte.
	lifecycleEncodingShift = 2

	// lifecycleEncodingMask selects the encoding once shifted.
	lifecycleEncodingMask = 0x03

	// lifecycleNoDownBit leaves a migration without a down file.
	lifecycleNoDownBit = 0x10

	// lifecycleNoTransactionBit marks a migration to run without a transaction.
	lifecycleNoTransactionBit = 0x20

	// lifecycleHiddenBit hides a migration other than the first until a reveal.
	lifecycleHiddenBit = 0x40

	// lifecycleWideGapBit leaves a wide gap before a migration's version.
	lifecycleWideGapBit = 0x80

	// lifecycleWideGap is the gap lifecycleWideGapBit leaves.
	lifecycleWideGap = 10

	// lifecycleFailureMask selects the failing migration, counted from 1, from the failure
	// byte, where 0 injects no failure.
	lifecycleFailureMask = 0x0f

	// lifecycleFailAtShift places the failing statement's index in the failure byte.
	lifecycleFailAtShift = 4

	// lifecycleFailAtMask selects the failing statement's index once shifted.
	lifecycleFailAtMask = 0x07

	// lifecycleDownFailsBit makes the failing migration's down file fail too.
	lifecycleDownFailsBit = 0x80
)

const (
	// lifecycleLF writes a file with LF line ends.
	lifecycleLF = iota

	// lifecycleCRLF writes a file with CRLF line ends.
	lifecycleCRLF

	// lifecycleBOM writes a file after a byte order mark.
	lifecycleBOM

	// lifecycleCommented writes a file after a comment holding a semicolon, with trailing
	// spaces and blank lines.
	lifecycleCommented
)

// lifecycleBases is how many first versions a history chooses from.
const lifecycleBases = byte(len(lifecycleVersionBases))

// lifecycleVersionBases are the versions a history's first migration takes. They are
// zero, one, a version above a 32-bit integer and a timestamp.
var lifecycleVersionBases = [...]int64{0, 1, 1 << 33, 20260115093000}

// lifecycleDefaultOperations are the operations of an input that gives none.
var lifecycleDefaultOperations = []byte{lifecycleUp, lifecycleSeed, lifecycleFix, lifecycleUp, lifecycleReveal, lifecycleUp,
	lifecycleValidate, lifecycleDown, lifecycleSeed}

// lifecycleOperationNames names the operations, indexed by their byte.
var lifecycleOperationNames = [...]string{lifecycleNameUp, lifecycleNameUpTo, lifecycleNameDown, lifecycleNameDownTo,
	lifecycleNameSeed, lifecycleNameValidate, lifecycleNameReveal, lifecycleNameFix}

// lifecycleAdvances applies each named operation to a state.
var lifecycleAdvances = map[string]func(state *LifecycleState, model *LifecycleCase, step LifecycleStep) LifecycleOutcome{
	lifecycleNameUp: func(state *LifecycleState, model *LifecycleCase, step LifecycleStep) LifecycleOutcome {
		return state.up(model, step.Target, true)
	},
	lifecycleNameUpTo: func(state *LifecycleState, model *LifecycleCase, step LifecycleStep) LifecycleOutcome {
		return state.up(model, step.Target, false)
	},
	lifecycleNameDown: func(state *LifecycleState, model *LifecycleCase, step LifecycleStep) LifecycleOutcome {
		return state.down(model, state.newestApplied(int(step.Target)))
	},
	lifecycleNameDownTo: func(state *LifecycleState, model *LifecycleCase, step LifecycleStep) LifecycleOutcome {
		return state.down(model, state.appliedAbove(model, step.Target))
	},
	lifecycleNameSeed: func(state *LifecycleState, model *LifecycleCase, _ LifecycleStep) LifecycleOutcome {
		return state.seed(model)
	},
	lifecycleNameValidate: func(state *LifecycleState, _ *LifecycleCase, _ LifecycleStep) LifecycleOutcome {
		return LifecycleOutcome{Count: 0, Fails: state.dirty() >= 0}
	},
	lifecycleNameReveal: func(state *LifecycleState, _ *LifecycleCase, _ LifecycleStep) LifecycleOutcome {
		state.Revealed = true
		return LifecycleOutcome{Count: 0, Fails: false}
	},
	lifecycleNameFix: func(state *LifecycleState, _ *LifecycleCase, _ LifecycleStep) LifecycleOutcome {
		state.Gate = true
		return LifecycleOutcome{Count: 0, Fails: false}
	},
}

// LifecycleMigration is one migration of a version 20 history.
type LifecycleMigration struct {
	// Version is the migration's version.
	Version int64

	// Inserts is how many rows the up file inserts after creating its table.
	Inserts int

	// Encoding writes the files with LF (0), CRLF (1), a byte order mark (2) or trailing
	// spaces and a comment holding a semicolon (3).
	Encoding int

	// FailAt is the index among the up file's statements of an insert into the gate table,
	// or -1 when the up file has none.
	FailAt int

	// Down reports that the migration has a down file.
	Down bool

	// DownFails reports that the down file inserts into the gate table before it drops the
	// migration's table.
	DownFails bool

	// NoTransaction marks the up file to run without a transaction.
	NoTransaction bool

	// Hidden reports that the files are written only by a reveal step.
	Hidden bool
}

// LifecycleStep is one operation of a version 20 history.
type LifecycleStep struct {
	// Operation names the operation, such as up or down-to.
	Operation string

	// Target is the version of up-to and down-to, or the count of down.
	Target int64
}

// LifecycleCase is a decoded version 20 history.
type LifecycleCase struct {
	// Engine is the engine the history runs on.
	Engine string

	// Migrations holds the migrations in version order.
	Migrations []LifecycleMigration

	// Steps holds the operations in order.
	Steps []LifecycleStep

	// Data is the input the case was decoded from.
	Data []byte

	// Seeds is how many seed files there are.
	Seeds int
}

// LifecycleMigrationState is the model's state of one migration.
type LifecycleMigrationState struct {
	// Rows holds the ids its table holds, in insertion order, or nil without a table.
	Rows []int64

	// Completed is how many of the up file's statements ran in the current attempt.
	Completed int

	// Applied reports that the migration is recorded as applied.
	Applied bool

	// Dirty reports that the migration is recorded as interrupted.
	Dirty bool

	// RollingBack reports that a down of it stopped part-way.
	RollingBack bool

	// HasTable reports that its table exists.
	HasTable bool
}

// LifecycleState is the model's expectation of the database after each step.
type LifecycleState struct {
	// Migrations holds each migration's state, in version order.
	Migrations []LifecycleMigrationState

	// SeedsApplied is how many seed files are recorded as applied.
	SeedsApplied int

	// Revealed reports that the hidden migrations' files are written.
	Revealed bool

	// Gate reports that the gate table exists.
	Gate bool
}

// LifecycleOutcome is what the model expects one step to give.
type LifecycleOutcome struct {
	// Count is how many migrations or seeds the step applied or rolled back.
	Count int

	// Fails reports that the step returns an error.
	Fails bool
}

// InitialState returns the state before any step, with nothing applied and no gate.
//
// Returns LifecycleState which is the empty state.
func (model *LifecycleCase) InitialState() LifecycleState {
	return LifecycleState{Migrations: make([]LifecycleMigrationState, len(model.Migrations)), SeedsApplied: 0, Revealed: false, Gate: false}
}

// Clone returns a deep copy of the state.
//
// Returns LifecycleState which is the copy.
func (state LifecycleState) Clone() LifecycleState {
	clone := state
	clone.Migrations = make([]LifecycleMigrationState, len(state.Migrations))
	for index, migration := range state.Migrations {
		migration.Rows = slices.Clone(migration.Rows)
		clone.Migrations[index] = migration
	}
	return clone
}

// Advance applies a step to the state and returns what the step gives.
//
// Takes model (*LifecycleCase) which is the history.
// Takes step (LifecycleStep) which is the step.
//
// Returns LifecycleOutcome which is the step's expected result.
func (state *LifecycleState) Advance(model *LifecycleCase, step LifecycleStep) LifecycleOutcome {
	advance, ok := lifecycleAdvances[step.Operation]
	if !ok {
		return LifecycleOutcome{Count: 0, Fails: false}
	}
	return advance(state, model, step)
}

// appliedAbove returns the indexes of migrations with a history row above a version,
// newest first.
//
// Takes model (*LifecycleCase) which is the history.
// Takes target (int64) which is the version to keep.
//
// Returns []int which holds the indexes.
func (state *LifecycleState) appliedAbove(model *LifecycleCase, target int64) []int {
	var above []int
	for index, migration := range slices.Backward(state.Migrations) {
		if (migration.Applied || migration.Dirty) && model.Migrations[index].Version > target {
			above = append(above, index)
		}
	}
	return above
}

// visible reports whether a migration's files are in the directory.
//
// Takes model (*LifecycleCase) which is the history.
// Takes index (int) which is the migration.
//
// Returns bool which is true when they are.
func (state *LifecycleState) visible(model *LifecycleCase, index int) bool {
	return !model.Migrations[index].Hidden || state.Revealed
}

// dirty returns the index of the dirty migration, or -1.
//
// Returns int which is the index.
func (state *LifecycleState) dirty() int {
	return slices.IndexFunc(state.Migrations, func(migration LifecycleMigrationState) bool { return migration.Dirty })
}

// up applies pending migrations in version order through the target.
//
// A dirty migration within the target resumes first after its completed statements. One
// above the target remains dirty. A visible pending migration below a dirty one, or a
// dirty down migration, stops the run. An injected insert fails when no gate exists.
//
// Takes model (*LifecycleCase) which is the history.
// Takes target (int64) which bounds the versions, unless every is true.
// Takes every (bool) which applies every pending migration.
//
// Returns LifecycleOutcome which is the step's expected result.
func (state *LifecycleState) up(model *LifecycleCase, target int64, every bool) LifecycleOutcome {
	if state.dirtyBlocksUp(model, target, every) {
		return LifecycleOutcome{Count: 0, Fails: true}
	}
	applied := 0
	for index, migration := range model.Migrations {
		current := &state.Migrations[index]
		if current.Applied || !state.visible(model, index) || !every && migration.Version > target {
			continue
		}
		if !state.runUp(model, index) {
			return LifecycleOutcome{Count: applied, Fails: true}
		}
		applied++
	}
	return LifecycleOutcome{Count: applied, Fails: false}
}

// dirtyBlocksUp reports whether dirty history prevents an up run.
//
// A run with no work succeeds. Otherwise the dirty record must match the first migration
// to run. A dirty migration above the target or after a visible pending migration blocks
// the run. A dirty down migration always blocks it.
//
// Takes model (*LifecycleCase) which is the history.
// Takes target (int64) which bounds the run's versions, unless every is true.
// Takes every (bool) which runs every pending migration.
//
// Returns bool which is true when the run fails.
func (state *LifecycleState) dirtyBlocksUp(model *LifecycleCase, target int64, every bool) bool {
	dirty := state.dirty()
	if dirty < 0 {
		return false
	}
	first := -1
	for index, migration := range model.Migrations {
		current := state.Migrations[index]
		if !current.Applied && state.visible(model, index) && (every || migration.Version <= target) {
			first = index
			break
		}
	}
	return first >= 0 && (first != dirty || state.Migrations[dirty].RollingBack)
}

// runUp runs one up file from where it stopped, or from the start.
//
// Takes model (*LifecycleCase) which is the history.
// Takes index (int) which is the migration.
//
// Returns bool which is false when the injected insert failed.
func (state *LifecycleState) runUp(model *LifecycleCase, index int) bool {
	migration := model.Migrations[index]
	current := &state.Migrations[index]
	statements := lifecycleStatements(migration)
	start := current.Completed
	transactional := lifecycleTransactional(model.Engine) && !migration.NoTransaction && !current.Dirty
	before := current.Clone()
	for position := start; position < len(statements); position++ {
		if position == migration.FailAt && !state.Gate {
			if transactional {
				*current = before
				return false
			}
			current.Dirty = model.Engine != engineClickHouse
			return false
		}
		state.effect(migration, index, position)
		current.Completed = position + 1
	}
	current.Applied, current.Dirty, current.Completed = true, false, 0
	return true
}

// effect applies one statement of an up file to the state.
//
// Takes migration (LifecycleMigration) which is the migration.
// Takes index (int) which is the migration's index.
// Takes position (int) which is the statement's index.
func (state *LifecycleState) effect(migration LifecycleMigration, index, position int) {
	current := &state.Migrations[index]
	switch {
	case position == migration.FailAt:
	case position == lifecycleCreateIndex(migration):
		current.HasTable, current.Rows = true, []int64{}
	default:
		current.Rows = append(current.Rows, int64(lifecycleInsertNumber(migration, position)))
	}
}

// Clone returns a copy of a migration's state.
//
// Returns LifecycleMigrationState which is the copy.
func (migration LifecycleMigrationState) Clone() LifecycleMigrationState {
	migration.Rows = slices.Clone(migration.Rows)
	return migration
}

// newestApplied returns the indexes of the newest migrations with a history row, applied
// or dirty, newest first.
//
// Takes count (int) which is how many.
//
// Returns []int which holds the indexes.
func (state *LifecycleState) newestApplied(count int) []int {
	var newest []int
	for index := len(state.Migrations) - 1; index >= 0 && len(newest) < count; index-- {
		if state.Migrations[index].Applied || state.Migrations[index].Dirty {
			newest = append(newest, index)
		}
	}
	return newest
}

// down rolls back selected migrations from newest to oldest.
//
// Dirty rows count among those selected. With none selected, it succeeds. A dirty
// migration or missing down file prevents rollback before it starts. An injected failure
// leaves a transactional migration applied. Other engines mark it dirty as rolling back,
// except ClickHouse, which records no such state.
//
// Takes model (*LifecycleCase) which is the history.
// Takes indexes ([]int) which are the migrations, newest first.
//
// Returns LifecycleOutcome which is the step's expected result.
func (state *LifecycleState) down(model *LifecycleCase, indexes []int) LifecycleOutcome {
	if len(indexes) == 0 {
		return LifecycleOutcome{Count: 0, Fails: false}
	}
	if state.dirty() >= 0 {
		return LifecycleOutcome{Count: 0, Fails: true}
	}
	for _, index := range indexes {
		if !model.Migrations[index].Down || !state.visible(model, index) {
			return LifecycleOutcome{Count: 0, Fails: true}
		}
	}
	for count, index := range indexes {
		current := &state.Migrations[index]
		if model.Migrations[index].DownFails && !state.Gate {
			if !lifecycleTransactional(model.Engine) && model.Engine != engineClickHouse {
				current.Dirty, current.RollingBack = true, true
			}
			return LifecycleOutcome{Count: count, Fails: true}
		}
		*current = LifecycleMigrationState{Rows: nil, Completed: 0, Applied: false, Dirty: false, RollingBack: false, HasTable: false}
	}
	return LifecycleOutcome{Count: len(indexes), Fails: false}
}

// seed applies the seed files not yet applied. They insert into the first migration's
// table and fail when it does not exist.
//
// Takes model (*LifecycleCase) which is the history.
//
// Returns LifecycleOutcome which is the step's expected result.
func (state *LifecycleState) seed(model *LifecycleCase) LifecycleOutcome {
	if state.SeedsApplied == model.Seeds {
		return LifecycleOutcome{Count: 0, Fails: false}
	}
	first := &state.Migrations[0]
	if !first.HasTable {
		return LifecycleOutcome{Count: 0, Fails: true}
	}
	applied := model.Seeds - state.SeedsApplied
	for seed := state.SeedsApplied + 1; seed <= model.Seeds; seed++ {
		first.Rows = append(first.Rows, int64(lifecycleSeedRowBase+seed))
	}
	state.SeedsApplied = model.Seeds
	return LifecycleOutcome{Count: applied, Fails: false}
}

// Files returns the files of the visible migrations and the seeds.
//
// Takes revealed (bool) which includes the hidden migrations.
//
// Returns map[string][]byte which maps each path to its content.
func (model *LifecycleCase) Files(revealed bool) map[string][]byte {
	files := map[string][]byte{}
	declaration := LifecycleTableDeclaration(model.Engine)
	for _, migration := range model.Migrations {
		if migration.Hidden && !revealed {
			continue
		}
		stem := fmt.Sprintf("migrations/%04d_life_%d", migration.Version, migration.Version)
		files[stem+".up.sql"] = encodeLifecycle(lifecycleUpFile(migration, declaration), migration.Encoding)
		if migration.Down {
			down := "DROP TABLE " + LifecycleTable(migration) + ";\n"
			if migration.DownFails {
				down = fmt.Sprintf("INSERT INTO %s (id) VALUES (%d);\n", LifecycleGate, -migration.Version) + down
			}
			files[stem+".down.sql"] = encodeLifecycle(down, migration.Encoding)
		}
	}
	first := LifecycleTable(model.Migrations[0])
	for seed := 1; seed <= model.Seeds; seed++ {
		files[fmt.Sprintf("seeds/%04d_rows.sql", seed)] = fmt.Appendf(nil, "INSERT INTO %s (id) VALUES (%d);\n", first, lifecycleSeedRowBase+seed)
	}
	return files
}

// DecodeLifecycle decodes a version 20 history.
//
// The input holds a byte of the migration and seed file counts, a byte choosing the
// starting version, a byte choosing the injected failure, one byte per migration and one
// per operation.
//
// Takes engine (string) which is the engine's name.
// Takes data ([]byte) which is the input.
//
// Returns LifecycleCase which is the history.
// Returns bool which is false for another version or an unsupported engine.
func DecodeLifecycle(engine string, data []byte) (LifecycleCase, bool) {
	if len(data) < lifecycleHeader || len(data) > ScenarioMaxBytes || data[0] != lifecycleVersion || !supportsShape(engine, shapeSelect) {
		return LifecycleCase{}, false
	}
	count := int(data[1]&lifecycleCountMask)%lifecycleMaxMigrations + 1
	model := LifecycleCase{Engine: engine, Migrations: make([]LifecycleMigration, count), Steps: nil,
		Data: append([]byte(nil), data...), Seeds: int(data[1]>>lifecycleSeedsShift)%lifecycleMaxSeeds + 1}
	version := lifecycleVersionBases[int(data[2])%len(lifecycleVersionBases)]
	body := data[lifecycleHeader:]
	for index := range model.Migrations {
		var choice byte
		if index < len(body) {
			choice = body[index]
		}
		if index > 0 {
			version += 1 + int64(lifecycleWideGap-1)*int64(boolInt(choice&lifecycleWideGapBit != 0))
		}
		model.Migrations[index] = decodeLifecycleMigration(version, choice, index > 0)
	}
	failure := scenarioByte(data, lifecycleHeader-1)
	if failing := int(failure&lifecycleFailureMask) % (count + 1); failing > 0 {
		migration := &model.Migrations[failing-1]
		migration.FailAt = int(failure>>lifecycleFailAtShift&lifecycleFailAtMask) % (migration.Inserts + 2)
		migration.DownFails = migration.Down && failure&lifecycleDownFailsBit != 0
	}
	var operations []byte
	for index, choice := range body {
		if index >= count {
			operations = append(operations, choice)
		}
	}
	if len(operations) == 0 {
		operations = lifecycleDefaultOperations
	}
	for _, choice := range operations[:min(len(operations), lifecycleMaxSteps)] {
		model.Steps = append(model.Steps, lifecycleStepOf(&model, choice))
	}
	return model, true
}

// decodeLifecycleMigration decodes one migration's byte.
//
// Takes version (int64) which is the migration's version.
// Takes choice (byte) which is its byte.
// Takes hideable (bool) which is false for the first migration, which the seeds fill.
//
// Returns LifecycleMigration which is the migration, with no failure.
func decodeLifecycleMigration(version int64, choice byte, hideable bool) LifecycleMigration {
	return LifecycleMigration{
		Version:       version,
		Inserts:       int(choice & lifecycleInsertsMask),
		Encoding:      int(choice >> lifecycleEncodingShift & lifecycleEncodingMask),
		FailAt:        -1,
		Down:          choice&lifecycleNoDownBit == 0,
		DownFails:     false,
		NoTransaction: choice&lifecycleNoTransactionBit != 0,
		Hidden:        hideable && choice&lifecycleHiddenBit != 0,
	}
}

// lifecycleStepOf turns an operation byte into a step whose target the history can name.
//
// Takes model (*LifecycleCase) which is the history.
// Takes choice (byte) which chooses the operation and its argument.
//
// Returns LifecycleStep which is the step.
func lifecycleStepOf(model *LifecycleCase, choice byte) LifecycleStep {
	operation := choice % lifecycleOperations
	argument := int(choice / lifecycleOperations)
	step := LifecycleStep{Operation: lifecycleOperationNames[operation], Target: 0}
	switch operation {
	case lifecycleDown:
		step.Target = int64(argument%2 + 1)
	case lifecycleUpTo, lifecycleDownTo:
		index := argument % (len(model.Migrations) + 1)
		step.Target = model.Migrations[0].Version - 1
		if index > 0 {
			step.Target = model.Migrations[index-1].Version
		}
	}
	return step
}

// lifecycleTransactional reports whether an engine runs a migration and its history row
// in one transaction, unless the migration asks to run without one.
//
// Takes engine (string) which is the engine's name.
//
// Returns bool which is true for PostgreSQL, TimescaleDB, SQLite and DuckDB.
func lifecycleTransactional(engine string) bool {
	return ExecutionAtomicDDL(engine)
}

// LifecycleTable names a migration's table.
//
// Takes migration (LifecycleMigration) which is the migration.
//
// Returns string which is the table's name.
func LifecycleTable(migration LifecycleMigration) string {
	return fmt.Sprintf("fuzz_life_%d", migration.Version)
}

// lifecycleCreateIndex returns the index of an up file's CREATE TABLE, after the gate
// insert when that comes first.
//
// Takes migration (LifecycleMigration) which is the migration.
//
// Returns int which is the index.
func lifecycleCreateIndex(migration LifecycleMigration) int {
	if migration.FailAt == 0 {
		return 1
	}
	return 0
}

// lifecycleInsertNumber returns the id an up file's statement inserts.
//
// Takes migration (LifecycleMigration) which is the migration.
// Takes position (int) which is an insert's index among the statements.
//
// Returns int which is the id, counted from 1.
func lifecycleInsertNumber(migration LifecycleMigration, position int) int {
	number := position
	if migration.FailAt >= 0 && migration.FailAt < position {
		number--
	}
	return number
}

// lifecycleStatements returns an up file's statements, without separators.
//
// Takes migration (LifecycleMigration) which is the migration.
//
// Returns []string which holds the statements in order.
func lifecycleStatements(migration LifecycleMigration) []string {
	statements := make([]string, 0, migration.Inserts+2)
	table := LifecycleTable(migration)
	for position := range migration.Inserts + 1 + boolInt(migration.FailAt >= 0) {
		switch {
		case position == migration.FailAt:
			statements = append(statements, fmt.Sprintf("INSERT INTO %s (id) VALUES (%d)", LifecycleGate, migration.Version))
		case position == lifecycleCreateIndex(migration):
			statements = append(statements, "CREATE TABLE "+table+lifecycleDeclarationMarker)
		default:
			statements = append(statements, fmt.Sprintf("INSERT INTO %s (id) VALUES (%d)", table, lifecycleInsertNumber(migration, position)))
		}
	}
	return statements
}

// boolInt returns 1 for true and 0 for false.
//
// Takes value (bool) which is the value.
//
// Returns int which is 1 or 0.
func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// LifecycleTableDeclaration returns the columns, and on ClickHouse the engine, of a table
// the history creates.
//
// Takes engine (string) which is the engine's name.
//
// Returns string which follows the table's name in CREATE TABLE.
func LifecycleTableDeclaration(engine string) string {
	if engine == engineClickHouse {
		return "(id Int64) ENGINE = MergeTree ORDER BY id"
	}
	return "(id BIGINT NOT NULL)"
}

// lifecycleUpFile returns an up file's content, with LF line ends.
//
// Takes migration (LifecycleMigration) which is the migration.
// Takes declaration (string) which follows a created table's name.
//
// Returns string which is the content.
func lifecycleUpFile(migration LifecycleMigration, declaration string) string {
	var up strings.Builder
	if migration.NoTransaction {
		up.WriteString("-- sgl.migration(no_transaction: true)\n")
	}
	for _, statement := range lifecycleStatements(migration) {
		if base, ok := strings.CutSuffix(statement, lifecycleDeclarationMarker); ok {
			statement = base + " " + declaration
		}
		up.WriteString(statement + ";\n")
	}
	return up.String()
}

// encodeLifecycle writes a file's content in one of the encodings.
//
// Takes content (string) which is the content with LF line ends.
// Takes encoding (int) which chooses the encoding.
//
// Returns []byte which is the encoded content.
func encodeLifecycle(content string, encoding int) []byte {
	switch encoding {
	case lifecycleCRLF:
		return []byte(strings.ReplaceAll(content, "\n", "\r\n"))
	case lifecycleBOM:
		return []byte("\xef\xbb\xbf" + content)
	case lifecycleCommented:
		return []byte("-- a comment; with a semicolon\n" + strings.ReplaceAll(content, ";\n", ";   \n") + "\n\n")
	default:
		return []byte(content)
	}
}

// LifecycleSeeds returns the seed inputs. They cover each version scheme over every
// encoding, failures at each statement of an up file and in a down file, a migration
// revealed below applied ones and the most migrations a history holds.
//
// Returns [][]byte which holds the seeds.
func LifecycleSeeds() [][]byte {
	seeds := [][]byte{lifecycleInput(1, 1, 0, 0, nil)}
	const pairs, seedFiles, mixedCount, revealedCount = 2, lifecycleMaxSeeds, 4, 3
	mixed := []byte{
		lifecycleMigrationByte(1, lifecycleCRLF, 0),
		lifecycleMigrationByte(2, lifecycleBOM, lifecycleHiddenBit),
		lifecycleMigrationByte(lifecycleMaxInserts, lifecycleCommented, lifecycleWideGapBit),
		lifecycleMigrationByte(0, lifecycleLF, lifecycleNoTransactionBit),
	}
	for base := range lifecycleBases {
		seeds = append(seeds, lifecycleInput(mixedCount, seedFiles, base, 0, mixed))
	}
	pair := []byte{lifecycleMigrationByte(2, lifecycleLF, 0), lifecycleMigrationByte(1, lifecycleCRLF, lifecycleNoTransactionBit)}
	recovery := []byte{lifecycleUp, lifecycleUp, lifecycleValidate, lifecycleDown, lifecycleFix, lifecycleUp, lifecycleValidate,
		lifecycleDown + lifecycleOperations, lifecycleDown, lifecycleUp}
	for failing := byte(1); failing <= pairs; failing++ {
		for at := range byte(lifecycleMaxInserts + 1) {
			for _, downFails := range []bool{false, true} {
				seeds = append(seeds, lifecycleInput(pairs, 1, 0, lifecycleFailure(failing, at, downFails), pair, recovery...))
			}
		}
	}
	revealed := []byte{lifecycleMigrationByte(1, lifecycleLF, 0), lifecycleMigrationByte(2, lifecycleLF, lifecycleHiddenBit),
		lifecycleMigrationByte(1, lifecycleBOM, 0)}
	seeds = append(seeds, lifecycleInput(revealedCount, 2, 1, 0, revealed, lifecycleUp, lifecycleSeed, lifecycleReveal, lifecycleUp,
		lifecycleSeed, lifecycleDownTo+lifecycleOperations, lifecycleUpTo+2*lifecycleOperations, lifecycleUp, lifecycleValidate))
	most := make([]byte, lifecycleMaxMigrations)
	for index := range byte(lifecycleMaxMigrations) {
		most[index] = lifecycleMigrationByte(index%(lifecycleMaxInserts+1), index%(lifecycleCommented+1), lifecycleWideGapBit)
	}
	return append(seeds, lifecycleInput(lifecycleMaxMigrations, seedFiles, 0, lifecycleFailure(lifecycleMaxMigrations, 1, false), most,
		lifecycleUp, lifecycleFix, lifecycleUp, lifecycleSeed, lifecycleDownTo, lifecycleUp))
}

// lifecycleInput assembles a version 20 input.
//
// Takes migrations (byte) which is the number of migrations.
// Takes seeds (byte) which is the number of seed files.
// Takes base (byte) which chooses the starting version.
// Takes failure (byte) which chooses the injected failure.
// Takes bytes ([]byte) which holds one byte per migration.
// Takes operations (...byte) which are the operations, the default ones when none.
//
// Returns []byte which is the input.
func lifecycleInput(migrations, seeds, base, failure byte, bytes []byte, operations ...byte) []byte {
	input := make([]byte, 0, lifecycleHeader+len(bytes)+len(operations))
	input = append(input, lifecycleVersion, (migrations-1)|(seeds-1)<<lifecycleSeedsShift, base, failure)
	return append(append(input, bytes...), operations...)
}

// lifecycleMigrationByte assembles one migration's byte.
//
// Takes inserts (byte) which is how many rows the up file inserts.
// Takes encoding (byte) which is the files' encoding.
// Takes flags (byte) which holds the migration's bits, such as lifecycleHiddenBit.
//
// Returns byte which is the migration's byte.
func lifecycleMigrationByte(inserts, encoding, flags byte) byte {
	return inserts&lifecycleInsertsMask | encoding&lifecycleEncodingMask<<lifecycleEncodingShift | flags
}

// lifecycleFailure assembles the failure byte.
//
// Takes migration (byte) which is the failing migration, counted from 1.
// Takes at (byte) which is the failing statement's index in its up file.
// Takes downFails (bool) which makes its down file fail too.
//
// Returns byte which is the failure byte.
func lifecycleFailure(migration, at byte, downFails bool) byte {
	failure := migration&lifecycleFailureMask | at&lifecycleFailAtMask<<lifecycleFailAtShift
	if downFails {
		failure |= lifecycleDownFailsBit
	}
	return failure
}
