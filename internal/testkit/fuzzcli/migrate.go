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

// Package fuzzcli models the sgl command line's migrate and seed commands independently
// of the migration runtime. One fuzz input is checked against the same model on SQLite in
// the command's own tests and on every server engine in the integration suites.
package fuzzcli

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	// migrateVersion is the first byte of a migrate workflow input.
	migrateVersion = 1

	// migrateHeader is how many bytes precede the operations.
	migrateHeader = 3

	// migrationsByte holds the number of migrations a project starts with.
	migrationsByte = 1

	// downsByte holds one bit per starting migration that has a down file.
	downsByte = 2

	// maxMigrations caps the migrations a project starts with.
	maxMigrations = 5

	// maxAddedMigrations caps the migrations an input adds after the start.
	maxAddedMigrations = 2

	// maxOperations caps the operations one input runs.
	maxOperations = 12

	// operationKinds is how many kinds of operation a byte chooses from.
	operationKinds = 9

	// migrateCommand is the sgl command the migration operations run.
	migrateCommand = "migrate"

	// directionUp names an up migration file and the migrate up command.
	directionUp = "up"

	// directionDown names a down migration file and the migrate down command.
	directionDown = "down"

	// seedCommand is the sgl command the seed operations run.
	seedCommand = "seed"

	// exitFindings is the exit status of a validate that finds a changed migration.
	exitFindings = 1

	// exitRefused is the exit status of a migration or seed that fails or is refused before
	// it runs.
	exitRefused = 4

	// seededTable is the table the seed fills.
	seededTable = 1
)

const (
	// operationUp runs migrate up.
	operationUp byte = iota

	// operationDown runs migrate down of one or two migrations.
	operationDown

	// operationTo runs migrate to a version.
	operationTo

	// operationValidate runs migrate validate.
	operationValidate

	// operationSeed runs seed apply.
	operationSeed

	// operationReseed runs seed reseed.
	operationReseed

	// operationEdit edits an applied migration's up file.
	operationEdit

	// operationAdd writes another migration.
	operationAdd

	// operationStatus runs migrate status.
	operationStatus
)

const (
	// everyDown gives each of the five first migrations a down file.
	everyDown byte = 0b11111

	// alternateDowns gives the first, third and fifth migrations a down file.
	alternateDowns byte = 0b10101

	// downChoices is how many migrations a down rolls back at most.
	downChoices = 2
)

// seedOperations is the mix of operations every seed input runs.
var seedOperations = []byte{
	operationUp, operationReseed, operationUp + operationKinds, operationDown, operationTo, operationSeed,
	operationEdit, operationAdd, operationValidate, operationUp, operationStatus, operationDown,
}

// operations runs each kind of operation, indexed by its byte.
var operations = [operationKinds]func(model *migrateModel, t testing.TB, argument int){
	operationUp:       (*migrateModel).runUp,
	operationDown:     (*migrateModel).runDown,
	operationTo:       (*migrateModel).runTo,
	operationValidate: (*migrateModel).runValidate,
	operationSeed:     (*migrateModel).runSeed,
	operationReseed:   (*migrateModel).runReseed,
	operationEdit:     (*migrateModel).runEdit,
	operationAdd:      (*migrateModel).runAdd,
	operationStatus:   (*migrateModel).runStatus,
}

// Result is what one run of the command line gave.
type Result struct {
	// Stdout is what the command wrote to standard output.
	Stdout string

	// Stderr is what the command wrote to standard error.
	Stderr string

	// Code is the command's exit status.
	Code int
}

// Workspace is an initialised sgl project the model's commands run in.
type Workspace interface {
	// Write replaces a project file, relative to the project's root.
	//
	// Takes name (string) which is the file's path.
	// Takes content (string) which is the file's new content.
	Write(name, content string)

	// Read returns a project file's content.
	//
	// Takes name (string) which is the file's path.
	//
	// Returns string which is the content.
	Read(name string) string

	// Remove deletes a project file.
	//
	// Takes name (string) which is the file's path.
	Remove(name string)

	// Run runs sgl in the project with the arguments. The workspace adds the connection
	// string.
	//
	// Takes arguments (...string) which are the command arguments.
	//
	// Returns Result which holds the command's output and exit status.
	Run(arguments ...string) Result
}

// Observation is the database's state as the caller reads it, independently of sgl.
type Observation struct {
	// Tables holds the names of the tables named m followed by a number, in any order.
	Tables []string

	// SeedRows is how many rows m1 holds, read only when m1 exists.
	SeedRows int
}

// Dialect gives the SQL each migration and the seed write on one engine.
type Dialect struct {
	// CreateTable returns the up migration of a version, which creates table m<version>.
	CreateTable func(version int) string

	// DropTable returns the down migration of a version.
	DropTable func(version int) string

	// Seed is the seed's content, which inserts one row into m1.
	Seed string
}

// migrateModel is the independent expectation of a project's migrations and seed.
type migrateModel struct {
	// workspace is the project the commands run in.
	workspace Workspace

	// observe reads the database's state.
	observe func() Observation

	// hasDown marks the versions that have a down file.
	hasDown map[int]bool

	// edited marks the versions whose up file changed after it was applied.
	edited map[int]bool

	// dialect writes the migrations and seed.
	dialect Dialect

	// applied lists the applied versions in ascending order.
	applied []int

	// files is how many migrations exist, numbered from 1.
	files int

	// seedRows is how many rows the seed left in m1.
	seedRows int

	// seeded reports that the seed is recorded as applied.
	seeded bool
}

// addMigration writes the next migration, with a down file when asked.
//
// Takes down (bool) which writes a down file.
func (model *migrateModel) addMigration(down bool) {
	model.files++
	version := model.files
	model.workspace.Write(migrationFile(version, directionUp), model.dialect.CreateTable(version))
	if down {
		model.workspace.Write(migrationFile(version, directionDown), model.dialect.DropTable(version))
		model.hasDown[version] = true
	}
}

// operate runs the operation a byte chooses and checks its exit status against the model,
// which it updates.
//
// Takes choice (byte) which chooses the operation and its argument.
func (model *migrateModel) operate(t testing.TB, choice byte) {
	t.Helper()
	operations[choice%operationKinds](model, t, int(choice)/operationKinds)
}

// runUp runs migrate up.
//
// Takes t (testing.TB) which checks the command's result.
func (model *migrateModel) runUp(t testing.TB, _ int) {
	t.Helper()
	expect(t, model.workspace.Run(migrateCommand, directionUp), model.up(), directionUp, exitRefused)
}

// runDown runs migrate down of one or two migrations.
//
// Takes argument (int) which chooses how many to roll back.
func (model *migrateModel) runDown(t testing.TB, argument int) {
	t.Helper()
	count := strconv.Itoa(argument%downChoices + 1)
	expect(t, model.workspace.Run(migrateCommand, directionDown, count), model.down(argument%downChoices+1), directionDown+" "+count, exitRefused)
}

// runTo runs migrate to a version.
//
// Takes argument (int) which chooses the target version.
func (model *migrateModel) runTo(t testing.TB, argument int) {
	t.Helper()
	target := argument % (model.files + 1)
	expect(t, model.workspace.Run(migrateCommand, "to", strconv.Itoa(target)), model.to(target), "to "+strconv.Itoa(target), exitRefused)
}

// runValidate runs migrate validate.
//
// Takes t (testing.TB) which checks the command's result.
func (model *migrateModel) runValidate(t testing.TB, _ int) {
	t.Helper()
	expect(t, model.workspace.Run(migrateCommand, "validate"), !model.changed(), "validate", exitFindings)
}

// runSeed runs seed apply.
//
// Takes t (testing.TB) which checks the command's result.
func (model *migrateModel) runSeed(t testing.TB, _ int) {
	t.Helper()
	expect(t, model.workspace.Run(seedCommand, "apply"), model.seed(), "seed apply", exitRefused)
}

// runReseed runs seed reseed.
//
// Takes t (testing.TB) which checks the command's result.
func (model *migrateModel) runReseed(t testing.TB, _ int) {
	t.Helper()
	expect(t, model.workspace.Run(seedCommand, "reseed"), model.reseed(), "seed reseed", exitRefused)
}

// runEdit appends a comment to an applied migration's up file.
//
// Takes argument (int) which chooses the applied migration.
func (model *migrateModel) runEdit(_ testing.TB, argument int) {
	if len(model.applied) == 0 {
		return
	}
	version := model.applied[argument%len(model.applied)]
	name := migrationFile(version, directionUp)
	model.workspace.Write(name, model.workspace.Read(name)+"-- edited\n")
	model.edited[version] = true
}

// runAdd writes another migration until the cap is reached.
//
// Takes argument (int) which chooses whether it has a down file.
func (model *migrateModel) runAdd(_ testing.TB, argument int) {
	if model.files < maxMigrations+maxAddedMigrations {
		model.addMigration(argument%downChoices == 0)
	}
}

// runStatus runs migrate status, which always succeeds.
//
// Takes t (testing.TB) which checks the command's result.
func (model *migrateModel) runStatus(t testing.TB, _ int) {
	t.Helper()
	expect(t, model.workspace.Run(migrateCommand, "status"), true, "status", exitRefused)
}

// changed reports whether an applied migration's file changed.
//
// Returns bool which is true when one did.
func (model *migrateModel) changed() bool {
	return slices.ContainsFunc(model.applied, func(version int) bool { return model.edited[version] })
}

// up applies every pending migration, unless an applied one changed.
//
// Returns bool which reports whether the command succeeds.
func (model *migrateModel) up() bool {
	if model.changed() {
		return false
	}
	model.applyThrough(model.files)
	return true
}

// down rolls back the newest applied migrations. Nothing is rolled back when one of them
// has no down file or an applied migration changed.
//
// Takes count (int) which is how many to roll back.
//
// Returns bool which reports whether the command succeeds.
func (model *migrateModel) down(count int) bool {
	if model.changed() {
		return false
	}
	start := max(len(model.applied)-count, 0)
	if !model.allHaveDowns(model.applied[start:]) {
		return false
	}
	model.rollBack(model.applied[start:])
	model.applied = model.applied[:start]
	return true
}

// to rolls back the applied migrations above a target and applies the pending ones at or
// below it. Nothing changes when one to roll back has no down file or an applied
// migration changed.
//
// Takes target (int) which is the version to move to.
//
// Returns bool which reports whether the command succeeds.
func (model *migrateModel) to(target int) bool {
	if model.changed() {
		return false
	}
	var above, kept []int
	for _, version := range model.applied {
		if version > target {
			above = append(above, version)
		} else {
			kept = append(kept, version)
		}
	}
	if !model.allHaveDowns(above) {
		return false
	}
	model.rollBack(above)
	model.applied = kept
	model.applyThrough(target)
	return true
}

// applyThrough marks every migration up to a version as applied.
//
// Takes last (int) which is the highest version to apply.
func (model *migrateModel) applyThrough(last int) {
	for version := 1; version <= last; version++ {
		if !slices.Contains(model.applied, version) {
			model.applied = append(model.applied, version)
		}
	}
	slices.Sort(model.applied)
}

// allHaveDowns reports whether every listed migration has a down file.
//
// Takes versions ([]int) which are the migrations to roll back.
//
// Returns bool which is true when each has one.
func (model *migrateModel) allHaveDowns(versions []int) bool {
	return !slices.ContainsFunc(versions, func(version int) bool { return !model.hasDown[version] })
}

// rollBack notes that dropping m1 drops the seed's rows with it.
//
// Takes versions ([]int) which are the migrations rolled back.
func (model *migrateModel) rollBack(versions []int) {
	if slices.Contains(versions, seededTable) {
		model.seedRows = 0
	}
}

// seed applies the seed once. It needs m1, and a recorded seed does not run again.
//
// Returns bool which reports whether the command succeeds.
func (model *migrateModel) seed() bool {
	if model.seeded {
		return true
	}
	if !slices.Contains(model.applied, seededTable) {
		return false
	}
	model.seeded = true
	model.seedRows++
	return true
}

// reseed clears the seed's record and applies it again. Without m1 it fails and leaves
// the seed unrecorded.
//
// Returns bool which reports whether the command succeeds.
func (model *migrateModel) reseed() bool {
	model.seeded = false
	return model.seed()
}

// check compares the database's tables, the seed's rows and the status sgl reports with
// the model.
//
// Takes t (testing.TB) which reports mismatches.
func (model *migrateModel) check(t testing.TB) {
	t.Helper()
	observed := model.observe()
	tables := slices.Clone(observed.Tables)
	slices.Sort(tables)
	want := make([]string, 0, len(model.applied))
	for _, version := range model.applied {
		want = append(want, "m"+strconv.Itoa(version))
	}
	slices.Sort(want)
	require.True(t, slices.Equal(want, tables), "the tables %v differ from the migrations the model applied %v", tables, want)
	if slices.Contains(model.applied, seededTable) {
		require.Equal(t, model.seedRows, observed.SeedRows, "m1 holds another number of rows than the model expects")
	}
	status := model.workspace.Run(migrateCommand, "status")
	require.Zero(t, status.Code, "migrate status failed\nstdout:\n%s\nstderr:\n%s", status.Stdout, status.Stderr)
	for version := 1; version <= model.files; version++ {
		state := model.state(version)
		pattern := regexp.MustCompile(fmt.Sprintf(`(?m)^%d\s+m%d\s+%s\b`, version, version, state))
		require.Regexp(t, pattern, status.Stdout, "migrate status reports another state than %s for %d", state, version)
	}
}

// state names the status sgl should report for a migration.
//
// Takes version (int) which is the migration.
//
// Returns string which is pending, applied or changed.
func (model *migrateModel) state(version int) string {
	if !slices.Contains(model.applied, version) {
		return "pending"
	}
	if model.edited[version] {
		return "changed"
	}
	return "applied"
}

// DialectFor returns the SQL the model writes on an engine. ClickHouse gets a MergeTree
// table, which needs an engine and an ordering, and other engines get a plain one.
//
// Takes engine (string) which is the dialect's name, such as postgres.
//
// Returns Dialect which writes the migrations and seed.
func DialectFor(engine string) Dialect {
	create := func(version int) string { return fmt.Sprintf("CREATE TABLE m%d (id INTEGER);\n", version) }
	if engine == "clickhouse" {
		create = func(version int) string {
			return fmt.Sprintf("CREATE TABLE m%d (id Int32) ENGINE = MergeTree ORDER BY id;\n", version)
		}
	}
	return Dialect{
		CreateTable: create,
		DropTable:   func(version int) string { return fmt.Sprintf("DROP TABLE m%d;\n", version) },
		Seed:        "INSERT INTO m1 (id) VALUES (1);\n",
	}
}

// MigrateSeeds returns the seed inputs. Each number of migrations appears with no, every
// and alternate down files, and each runs the same mix of operations.
//
// Returns [][]byte which holds the seeds.
func MigrateSeeds() [][]byte {
	var seeds [][]byte
	for migrations := byte(1); migrations <= maxMigrations; migrations++ {
		for _, downs := range []byte{0, everyDown, alternateDowns} {
			seeds = append(seeds, append([]byte{migrateVersion, migrations, downs}, seedOperations...))
		}
	}
	return seeds
}

// RunMigrate runs a migrate workflow input against the command line and the model.
//
// It replaces the project's migrations and seed with the model's, runs each chosen
// operation and checks the exit status, the tables, the seed's rows and the reported
// status after every step.
//
// Takes data ([]byte) which is the input.
// Takes workspace (Workspace) which is an initialised project.
// Takes dialect (Dialect) which writes the migrations and seed.
// Takes observe (func() Observation) which reads the database's state.
func RunMigrate(t testing.TB, data []byte, workspace Workspace, dialect Dialect, observe func() Observation) {
	t.Helper()
	if len(data) < migrateHeader || data[0] != migrateVersion {
		t.Skip("unsupported migrate workflow envelope")
	}
	model := &migrateModel{workspace: workspace, observe: observe, hasDown: map[int]bool{}, edited: map[int]bool{},
		dialect: dialect, applied: nil, files: 0, seedRows: 0, seeded: false}
	for _, name := range []string{"db/migrations/0001_init.up.sql", "db/migrations/0001_init.down.sql", "db/seeds/0001_authors.sql"} {
		workspace.Remove(name)
	}
	migrations, downs := int(data[migrationsByte])%maxMigrations+1, data[downsByte]
	for version := 1; version <= migrations; version++ {
		model.addMigration(downs&(1<<(version-1)) != 0)
	}
	workspace.Write("db/seeds/0001_fill.sql", dialect.Seed)
	for _, choice := range data[migrateHeader:min(len(data), migrateHeader+maxOperations)] {
		model.operate(t, choice)
		model.check(t)
	}
}

// migrationFile names a migration's up or down file.
//
// Takes version (int) which is the migration.
// Takes direction (string) which is up or down.
//
// Returns string which is the path relative to the project's root.
func migrationFile(version int, direction string) string {
	return fmt.Sprintf("db/migrations/%04d_m%d.%s.sql", version, version, direction)
}

// expect checks that a command succeeded or failed as the model says.
//
// Takes result (Result) which is the command's result.
// Takes succeeds (bool) which is the model's expectation.
// Takes command (string) which names the command in a failure.
// Takes failure (int) which is the exit status the command fails with.
func expect(t testing.TB, result Result, succeeds bool, command string, failure int) {
	t.Helper()
	t.Logf("%s: exit %d: %s%s", command, result.Code, strings.TrimSpace(result.Stdout), strings.TrimSpace(result.Stderr))
	want := failure
	if succeeds {
		want = 0
	}
	require.Equal(t, want, result.Code, "%s exited with another status than the model expects\nstdout:\n%s\nstderr:\n%s",
		command, result.Stdout, result.Stderr)
}
