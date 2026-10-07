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

package sqlexec

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/migrate"
	"seagull.sh/seagull/internal/panicguard"
	"seagull.sh/seagull/internal/sqllex"
)

const (
	// customEngineName names a Custom backend whose spec gives no name.
	customEngineName = "custom"

	// maxPlaceholderIndex is the highest bind-parameter index any executor statement uses:
	// the last column of the history INSERT. Custom renders the markers up to it once.
	maxPlaceholderIndex = insertPlaceholderStatementsHash
)

// errInvalidSpec is wrapped by the error a Custom backend records for an incomplete
// BackendSpec.
var errInvalidSpec = fmt.Errorf("invalid backend spec: %w", migrate.ErrInvalidConfig)

// BackendSpec describes the run-time migration SQL of a database engine that has no
// built-in Backend, so a third-party engine can drive NewMigrator, NewSeeder and the
// executors through CustomBackend.
//
// Table names passed to the renderers have passed the same safe-identifier check as the
// built-in backends use, so a renderer can interpolate them directly.
//
// CustomBackend calls each renderer once, for the names and bind-parameter markers the
// backend uses, and the executors reuse what it returned; a renderer that panics there
// makes the executor constructors return an error wrapping migrate.ErrInvalidConfig.
// Split and the Rules callbacks run on each file instead, and a panic in one of them
// fails that call with an error rather than the program.
type BackendSpec struct {
	// Placeholder renders the bind-parameter marker for a 1-based index, such as "$1" or
	// "?". Required.
	Placeholder func(index int) string

	// CreateHistoryTable renders idempotent DDL for the migration history table. It is
	// required; AddHistoryColumn upgrades tables missing columns introduced later.
	CreateHistoryTable func(table string) string

	// CreateSeedTable renders the idempotent DDL of the seed history table, with the columns
	// version, name, checksum, applied_at and duration_ms. Required.
	CreateSeedTable func(table string) string

	// InsertSeed renders the required statement recording an applied seed. Repeated
	// insertion of a version must succeed without adding another row.
	InsertSeed func(table, versionPlaceholder, namePlaceholder, checksumPlaceholder, durationPlaceholder string) string

	// TableExists renders the required query counting a named table in the current schema.
	// The name is its sole bind parameter.
	TableExists func(tablePlaceholder string) string

	// DeleteHistory renders a synchronous deletion of a history row by version. Nil uses a
	// standard DELETE statement.
	DeleteHistory func(table, versionPlaceholder string) string

	// ClearSeedHistory renders the statement that empties the seed history on reseed. Nil
	// uses DELETE FROM <table>.
	ClearSeedHistory func(table string) string

	// AddHistoryColumn renders the DDL that adds a nullable column to a history table that
	// lacks it: splitter_version, an integer, or statements_hash, text holding a
	// 64-character hexadecimal digest. Nil uses ALTER TABLE <table> ADD COLUMN <column> with
	// INTEGER or VARCHAR(64).
	AddHistoryColumn func(table, column string) string

	// Split divides migration or seed files into executable statements.
	//
	// Nil splits under Rules, as the generator splits files; custom splitting must remain
	// stable for safe migration resumption. A panic in it is returned as an error matching
	// the generator's ErrPanic, with its stack logged once at warning level.
	Split func(content string) ([]string, error)

	// AdvisoryLock, when set, lets the backend take LockAdvisory.
	AdvisoryLock *AdvisoryLockSQL

	// TableLock, when set, lets the backend take LockTable.
	TableLock *TableLockSQL

	// Name identifies the engine in error messages. Empty means "custom".
	Name string

	// HistoryReadModifier follows the history table in the applied-versions SELECT, such as
	// " FINAL". Usually empty.
	HistoryReadModifier string

	// Rules defines the engine's lexical rules for statement splitting and transaction
	// detection. Zero uses standard SQL.
	Rules sqllex.Rules

	// DefaultLock is the lock mode used when WithLock is not given. The zero value means
	// LockNone; LockAdvisory and LockTable need AdvisoryLock and TableLock respectively.
	DefaultLock LockMode

	// TransactionalDDL reports that DDL runs inside transactions, so each migration and its
	// history row commit together. When false every migration takes the non-transactional
	// path, which tracks progress statement by statement unless AppendOnlyHistory is set.
	TransactionalDDL bool

	// TransactionalSeeds reports that a seed's SQL and its history row can commit together
	// in one transaction. When false a seed runs on its own and is recorded after it
	// succeeds.
	TransactionalSeeds bool

	// AppendOnlyHistory reports that the engine cannot UPDATE history rows in place, so an
	// up migration records one completed row on success instead of tracking progress.
	AppendOnlyHistory bool

	// MultiStatementSeeds reports that the driver runs a whole seed file in one call;
	// otherwise each seed is split and run statement by statement.
	MultiStatementSeeds bool
}

// AdvisoryLockSQL renders a session-scoped named lock that a query takes with a bounded
// wait, in the manner of MySQL's GET_LOCK. Each lock query returns one row whose first
// column is 1 or true when the lock is held, 0 or false when it is not, or NULL on error.
type AdvisoryLockSQL struct {
	// Name, when set, renders a query on the locking session whose one row and column is the
	// lock name to use for key, such as one scoped to the current schema. Nil uses key
	// itself.
	Name func(key string) string

	// Wait renders the required advisory-lock wait query with one lock-name bind.
	//
	// The query should wait for a bounded time on the server, as MySQL's GET_LOCK(name, 5)
	// does. A zero result is retried until cancellation, with at least half a second between
	// the start of each attempt to prevent tight polling.
	Wait func(namePlaceholder string) string

	// Try renders the query that takes the lock name as its one bind parameter and takes the
	// lock only when it is free. Required.
	Try func(namePlaceholder string) string

	// Release renders the statement that takes the lock name as its one bind parameter and
	// releases the lock. Required.
	Release func(namePlaceholder string) string
}

// TableLockSQL renders a lock held as a row lock on a single-row lock table inside an
// open transaction.
//
// Before each migration or seed the executor runs SELECT 1 in that transaction, to
// confirm it is still open, and returns ErrLockLost when it is not.
type TableLockSQL struct {
	// Create renders the idempotent DDL of the lock table, with an integer lock_id column
	// kept to the single value 1 by the CHECK constraint named constraint. Required.
	Create func(table, constraint string) string

	// InsertRow renders the statement that inserts the row lock_id = 1, doing nothing when
	// it exists. Required.
	InsertRow func(table string) string

	// Lock renders the required query that waits for the table's row lock.
	Lock func(table string) string

	// TryLock renders the required non-blocking row-lock query. A held lock must report
	// SQLSTATE 55P03, through a SQLState method or as "SQLSTATE 55P03" in the message, or a
	// message containing "could not obtain lock".
	TryLock func(table string) string
}

// Custom returns the backend a BackendSpec describes, for a database engine with no
// built-in Backend. The spec and the options are validated as the built-in backends' are;
// a problem is recorded in the Backend and returned by the executor constructors.
//
// Takes spec (BackendSpec) which describes the engine's migration SQL.
// Takes options (...BackendOption) which set the namespace, history table and lock mode.
//
// Returns Backend which is ready for NewMigrator, NewSeeder or NewExecutor.
func Custom(spec BackendSpec, options ...BackendOption) Backend {
	base, err := engineFromSpec(spec)
	if err != nil {
		return failedBackend(err)
	}
	return renderBackend(base, options)
}

// engineFromSpec converts a BackendSpec into an engine and validates it.
//
// Takes spec (BackendSpec) which describes the engine.
//
// Returns *engine which is the validated engine.
// Returns error which wraps migrate.ErrInvalidConfig when the spec is incomplete.
func engineFromSpec(spec BackendSpec) (*engine, error) {
	built := &engine{
		placeholder:         spec.Placeholder,
		createHistoryTable:  spec.CreateHistoryTable,
		createSeedTable:     spec.CreateSeedTable,
		insertSeed:          spec.InsertSeed,
		tableExists:         spec.TableExists,
		deleteHistory:       spec.DeleteHistory,
		clearSeedHistory:    spec.ClearSeedHistory,
		split:               nil,
		addHistoryColumn:    spec.AddHistoryColumn,
		qualifyTable:        nil,
		rules:               spec.Rules,
		ansiQuotesRules:     nil,
		locks:               map[LockMode]lockFactory{LockNone: noLocks},
		name:                spec.Name,
		currentSchemaSQL:    "",
		historyReadModifier: spec.HistoryReadModifier,
		defaultLock:         spec.DefaultLock,
		transactionalDDL:    spec.TransactionalDDL,
		transactionalSeeds:  spec.TransactionalSeeds,
		appendOnlyHistory:   spec.AppendOnlyHistory,
		multiStatementSeeds: spec.MultiStatementSeeds,
	}
	if built.name == "" {
		built.name = customEngineName
	}
	if spec.Split == nil {
		built.split = lexicalSplitter(spec.Rules)
	} else {
		built.split = guardedSplitter(built.name, trimmedSplitter(spec.Split))
	}
	if built.addHistoryColumn == nil {
		built.addHistoryColumn = addColumnRenderer(standardHistoryColumnTypes)
	}
	if built.defaultLock == 0 {
		built.defaultLock = LockNone
	}
	var problems []error
	if spec.AdvisoryLock != nil {
		problems = append(problems, checkRenderers("AdvisoryLock", map[string]bool{
			"Wait": spec.AdvisoryLock.Wait != nil, "Try": spec.AdvisoryLock.Try != nil, "Release": spec.AdvisoryLock.Release != nil,
		}))
		built.locks[LockAdvisory] = customAdvisoryLocks(*spec.AdvisoryLock, spec.Placeholder, built.name)
	}
	if spec.TableLock != nil {
		problems = append(problems, checkRenderers("TableLock", map[string]bool{
			"Create": spec.TableLock.Create != nil, "InsertRow": spec.TableLock.InsertRow != nil,
			"Lock": spec.TableLock.Lock != nil, "TryLock": spec.TableLock.TryLock != nil,
		}))
		built.locks[LockTable] = customTableLocks(*spec.TableLock)
	}
	problems = append(problems, validateEngine(built))
	if err := errors.Join(problems...); err != nil {
		return nil, err
	}
	return built, nil
}

// validateEngine checks that an engine renders everything a Backend needs and supports
// its default lock mode. The built-in engines pass it too.
//
// Takes candidate (*engine) which is the engine to check.
//
// Returns error which wraps migrate.ErrInvalidConfig naming each missing part.
func validateEngine(candidate *engine) error {
	problems := []error{checkRenderers("BackendSpec", map[string]bool{
		"Placeholder":        candidate.placeholder != nil,
		"CreateHistoryTable": candidate.createHistoryTable != nil,
		"CreateSeedTable":    candidate.createSeedTable != nil,
		"InsertSeed":         candidate.insertSeed != nil,
		"TableExists":        candidate.tableExists != nil,
		"Split":              candidate.split != nil,
		"AddHistoryColumn":   candidate.addHistoryColumn != nil,
	})}
	if _, supported := candidate.locks[candidate.defaultLock]; !supported {
		problems = append(problems, fmt.Errorf("%w: the default lock mode %s needs its lock SQL", errInvalidSpec, candidate.defaultLock))
	}
	return errors.Join(problems...)
}

// checkRenderers reports the renderers a spec part is missing.
//
// Takes part (string) which names the spec part.
// Takes present (map[string]bool) which records whether each required renderer is set.
//
// Returns error which wraps migrate.ErrInvalidConfig for each missing renderer, or nil.
func checkRenderers(part string, present map[string]bool) error {
	var problems []error
	for _, field := range slices.Sorted(maps.Keys(present)) {
		if !present[field] {
			problems = append(problems, fmt.Errorf("%w: %s.%s is required", errInvalidSpec, part, field))
		}
	}
	return errors.Join(problems...)
}

// customAdvisoryLocks builds the lock factory of a spec's advisory lock.
//
// Takes statements (AdvisoryLockSQL) which renders the lock's SQL.
// Takes placeholder (func(int) string) which renders the bind-parameter marker.
// Takes engineName (string) which names the engine in error messages.
//
// Returns lockFactory which builds the migration and seed locks.
func customAdvisoryLocks(statements AdvisoryLockSQL, placeholder func(int) string, engineName string) lockFactory {
	return func(names Names, _ bool) lockPair {
		return lockPair{
			migration: newCustomAdvisoryLock(statements, placeholder, names.LockKey, engineName),
			seed:      newCustomAdvisoryLock(statements, placeholder, names.SeedLockKey, engineName+" seed"),
		}
	}
}

// customTableLocks builds the lock factory of a spec's table lock.
//
// Takes statements (TableLockSQL) which renders the lock's SQL.
//
// Returns lockFactory which builds the migration and seed locks.
func customTableLocks(statements TableLockSQL) lockFactory {
	return func(names Names, _ bool) lockPair {
		return lockPair{
			migration: newTableLock(statements, names.MigrationLock, names.MigrationLockConstraint, "migration table"),
			seed:      newTableLock(statements, names.SeedLock, names.SeedLockConstraint, "seed table"),
		}
	}
}

// trimmedSplitter wraps a spec's splitter so that its statements are trimmed of
// surrounding whitespace and blank ones are dropped, as the built-in splitters return
// them, so statement indices never count a statement that does not run.
//
// Takes split (func(string) ([]string, error)) which is the spec's splitter.
//
// Returns func(string) ([]string, error) which returns the non-blank statements.
func trimmedSplitter(split func(content string) ([]string, error)) func(content string) ([]string, error) {
	return func(content string) ([]string, error) {
		statements, err := split(content)
		if err != nil {
			return nil, err
		}
		kept := make([]string, 0, len(statements))
		for _, statement := range statements {
			if trimmed := strings.TrimSpace(statement); trimmed != "" {
				kept = append(kept, trimmed)
			}
		}
		return kept, nil
	}
}

// failedBackend returns the Backend Custom builds for a spec it cannot use.
//
// Takes err (error) which is why the spec cannot be used.
//
// Returns Backend which records err for the executor constructors to return.
func failedBackend(err error) Backend {
	return Backend{err: err, engine: nil, migrationLock: nil, seedLock: nil, names: namesFor(DefaultNamespace), lockMode: 0}
}

// renderBackend applies options to a Custom engine and calls the spec's renderers once
// for everything the backend renders: its lock statements, as buildBackend renders them,
// and every statement the executors build, which renderedEngine keeps.
//
// Renderer panics are recovered, logged once with their stack through the default logger,
// and stored in the Backend. Executor constructors then report them before a migration
// can acquire the lock.
//
// Takes base (*engine) which is the validated Custom engine.
// Takes options ([]BackendOption) which configure the backend.
//
// Returns Backend which is the configured backend, or one recording an error wrapping
// migrate.ErrInvalidConfig when a renderer panicked.
func renderBackend(base *engine, options []BackendOption) (backend Backend) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		panicError := panicguard.Recovered(context.Background(), "rendering the "+base.name+" backend's SQL", recovered)
		backend = failedBackend(fmt.Errorf("%w: %w", errInvalidSpec, panicError))
	}()
	backend = buildBackend(base, options)
	if backend.err != nil {
		return backend
	}
	backend.engine = renderedEngine(backend.engine, backend.historyTable(), backend.seedTable())
	return backend
}

// renderedEngine returns a copy of a Custom engine whose renderers answer from SQL the
// spec's renderers gave here, once, for the tables and bind-parameter markers the
// executors pass them.
//
// Custom engines have no session schema, so executors use unqualified table names. The
// stored SQL covers the tables and bind markers they use; other inputs still call the
// original renderer.
//
// Takes base (*engine) which is the Custom engine.
// Takes historyTable (string) which is the backend's migration history table.
// Takes seedTable (string) which is the backend's seed history table.
//
// Returns *engine which renders from the stored SQL.
func renderedEngine(base *engine, historyTable, seedTable string) *engine {
	rendered := *base
	markers := make([]string, maxPlaceholderIndex)
	for index := range markers {
		markers[index] = base.placeholder(index + 1)
	}
	rendered.placeholder = func(index int) string {
		if index >= 1 && index <= len(markers) {
			return markers[index-1]
		}
		return base.placeholder(index)
	}
	rendered.createHistoryTable = renderedFor(historyTable, base.createHistoryTable)
	rendered.createSeedTable = renderedFor(seedTable, base.createSeedTable)
	rendered.tableExists = renderedFor(markers[0], base.tableExists)
	rendered.clearSeedHistory = renderedFor(seedTable, base.clearSeedHistory)
	rendered.deleteHistory = renderedDeleteHistory(historyTable, markers[0], base.deleteHistory)
	rendered.addHistoryColumn = renderedAddHistoryColumn(historyTable, base.addHistoryColumn)
	rendered.insertSeed = renderedInsertSeed(seedTable, markers, base.insertSeed)
	return &rendered
}

// renderedFor renders input once and returns a renderer that answers input with that
// text, passing any other input to render.
//
// Takes input (string) which is the input the executors pass.
// Takes render (func(string) string) which is the spec's renderer, or nil.
//
// Returns func(string) string which is the stored renderer, or nil when render is nil.
func renderedFor(input string, render func(string) string) func(string) string {
	if render == nil {
		return nil
	}
	text := render(input)
	return func(candidate string) string {
		if candidate == input {
			return text
		}
		return render(candidate)
	}
}

// renderedDeleteHistory renders a spec's history delete once for the history table and
// the first marker.
//
// Takes table (string) which is the history table.
// Takes marker (string) which is the version's bind-parameter marker.
// Takes render (func(string, string) string) which is the spec's renderer, or nil.
//
// Returns func(string, string) string which is the stored renderer, or nil when render is
// nil.
func renderedDeleteHistory(table, marker string, render func(table, versionPlaceholder string) string) func(table, versionPlaceholder string) string {
	if render == nil {
		return nil
	}
	text := render(table, marker)
	return func(candidateTable, candidateMarker string) string {
		if candidateTable == table && candidateMarker == marker {
			return text
		}
		return render(candidateTable, candidateMarker)
	}
}

// renderedAddHistoryColumn renders a spec's history column addition once for each of
// historyColumnAdditions.
//
// Takes table (string) which is the history table.
// Takes render (func(string, string) string) which is the spec's renderer.
//
// Returns func(string, string) string which is the stored renderer.
func renderedAddHistoryColumn(table string, render func(table, column string) string) func(table, column string) string {
	texts := make(map[string]string, len(historyColumnAdditions))
	for _, column := range historyColumnAdditions {
		texts[column] = render(table, column)
	}
	return func(candidateTable, column string) string {
		if text, rendered := texts[column]; rendered && candidateTable == table {
			return text
		}
		return render(candidateTable, column)
	}
}

// renderedInsertSeed renders a spec's seed insert once for the seed table and the markers
// the seed executor binds.
//
// Takes table (string) which is the seed table.
// Takes markers ([]string) which holds the rendered markers from index 1.
// Takes render (func(string, string, string, string, string) string) which is the spec's
// renderer.
//
// Returns func(string, string, string, string, string) string which is the stored
// renderer.
func renderedInsertSeed(
	table string,
	markers []string,
	render func(table, version, name, checksum, duration string) string,
) func(table, version, name, checksum, duration string) string {
	key := [...]string{
		table,
		markers[seedPlaceholderVersion-1],
		markers[seedPlaceholderName-1],
		markers[seedPlaceholderChecksum-1],
		markers[seedPlaceholderDurationMs-1],
	}
	text := render(key[0], key[1], key[2], key[3], key[4])
	return func(candidateTable, version, name, checksum, duration string) string {
		if [...]string{candidateTable, version, name, checksum, duration} == key {
			return text
		}
		return render(candidateTable, version, name, checksum, duration)
	}
}

// guardedSplitter returns a splitter that runs a spec's splitter, which parses migration
// and seed files while the lock is held, under a guard that returns a panic in it as an
// error matching the generator's ErrPanic, with the stack logged once through ctx's
// logger.
//
// Takes engineName (string) which names the engine in the error.
// Takes split (func(string) ([]string, error)) which is the spec's splitter.
//
// Returns func(context.Context, string) ([]string, error) which is the guarded splitter.
func guardedSplitter(engineName string, split func(content string) ([]string, error)) func(ctx context.Context, content string) ([]string, error) {
	operation := engineName + " backend Split"
	return func(ctx context.Context, content string) ([]string, error) {
		return panicguard.RunValue(ctx, operation, func() ([]string, error) {
			return split(content)
		})
	}
}
