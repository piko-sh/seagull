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

package migrate_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/migrate"
	"seagull.sh/seagull/internal/observe"
	"seagull.sh/seagull/internal/testkit/scaling"
)

const (
	createUsers = "CREATE TABLE users (id int);\n"
	addOrders   = "CREATE TABLE orders (id int);\nCREATE INDEX orders_id ON orders (id);\nINSERT INTO orders VALUES (1);\n"
	addTags     = "CREATE TABLE tags (id int);\n"
)

var errHistoryStoreLocked = errors.New("the history store is locked")

type historyStore struct {
	rows        map[int64]migrate.AppliedMigration
	lockContext context.Context
	acquire     func(ctx context.Context) error
	tryError    error
	events      []string
	locked      bool
	ensured     bool
}

func newHistoryStore(rows ...migrate.AppliedMigration) *historyStore {
	store := &historyStore{rows: map[int64]migrate.AppliedMigration{}}
	for _, row := range rows {
		store.rows[row.Version] = row
	}
	return store
}

type endedStage struct {
	err   error
	stage observe.Stage
}

type stageLog struct {
	ended []endedStage
}

func (store *historyStore) EnsureMigrationTable(context.Context) error {
	store.ensured = true
	return nil
}

func (store *historyStore) AcquireLock(ctx context.Context) error {
	store.events = append(store.events, "acquire")
	if store.acquire != nil {
		if err := store.acquire(ctx); err != nil {
			return err
		}
	}
	store.locked = true
	store.lockContext = ctx
	return nil
}

func (store *historyStore) TryAcquireLock(context.Context) error {
	store.events = append(store.events, "try")
	if store.tryError != nil {
		return store.tryError
	}
	store.locked = true
	return nil
}

func (store *historyStore) ReleaseLock(context.Context) error {
	store.events = append(store.events, "release")
	store.locked = false
	return nil
}

func (store *historyStore) AppliedVersions(context.Context) ([]migrate.AppliedMigration, error) {
	versions := slices.Sorted(maps.Keys(store.rows))
	applied := make([]migrate.AppliedMigration, 0, len(versions))
	for _, version := range versions {
		applied = append(applied, store.rows[version])
	}
	return applied, nil
}

func (store *historyStore) ExecuteMigration(_ context.Context, migration migrate.MigrationRecord, direction migrate.MigrationDirection, _ bool) error {
	store.events = append(store.events, "execute")
	if direction == migrate.MigrationDirectionDown {
		delete(store.rows, migration.Version)
		return nil
	}
	store.rows[migration.Version] = migrate.AppliedMigration{Version: migration.Version, Name: migration.Name, Checksum: migration.Checksum, DownChecksum: migration.DownChecksum}
	return nil
}

func (*historyStore) SplitStatements(content []byte) ([]string, error) {
	var statements []string
	for statement := range strings.SplitSeq(string(content), ";") {
		if trimmed := strings.TrimSpace(statement); trimmed != "" {
			statements = append(statements, trimmed)
		}
	}
	return statements, nil
}

func (store *historyStore) RecordApplied(_ context.Context, migration migrate.MigrationRecord) error {
	if !store.locked {
		return errHistoryStoreLocked
	}
	store.events = append(store.events, "record applied")
	row := store.rows[migration.Version]
	row.Version, row.Name, row.Checksum, row.DownChecksum = migration.Version, migration.Name, migration.Checksum, migration.DownChecksum
	row.Dirty, row.LastStatement, row.SplitterVersion, row.StatementsHash = false, nil, 0, ""
	store.rows[migration.Version] = row
	return nil
}

func (store *historyStore) RecordProgress(_ context.Context, migration migrate.MigrationRecord, completed int) error {
	if !store.locked {
		return errHistoryStoreLocked
	}
	store.events = append(store.events, "record progress")
	if completed == 0 {
		delete(store.rows, migration.Version)
		return nil
	}
	store.rows[migration.Version] = migrate.AppliedMigration{
		Version: migration.Version, Name: migration.Name, Checksum: migration.Checksum, DownChecksum: migration.DownChecksum,
		LastStatement: new(completed - 1), SplitterVersion: 1, StatementsHash: "digest", Dirty: true,
	}
	return nil
}

func (log *stageLog) Start(ctx context.Context, stage observe.Stage, _ ...slog.Attr) (context.Context, func(error)) {
	return ctx, func(err error) { log.ended = append(log.ended, endedStage{stage: stage, err: err}) }
}

func recoveryFiles(files map[string]string) fstest.MapFS {
	fileSystem := fstest.MapFS{}
	for name, content := range files {
		fileSystem["migrations/"+name] = &fstest.MapFile{Data: []byte(content)}
	}
	return fileSystem
}

func threeMigrations() fstest.MapFS {
	return recoveryFiles(map[string]string{
		"001_users.up.sql":    createUsers,
		"002_orders.up.sql":   addOrders,
		"002_orders.down.sql": "DROP TABLE orders;\n",
		"003_tags.up.sql":     addTags,
	})
}

func newRecoveryMigrator(t *testing.T, executor migrate.ExecutorPort, files fstest.MapFS, options ...migrate.Option) *migrate.Migrator {
	t.Helper()
	migrator, err := migrate.New(executor, files, "migrations", options...)
	require.NoError(t, err)
	return migrator
}

func appliedRow(version int64, name, content string) migrate.AppliedMigration {
	return migrate.AppliedMigration{Version: version, Name: name, Checksum: computeTestChecksum([]byte(content))}
}

func TestForceRecordsAPendingMigrationAsAppliedWithoutRunningIt(t *testing.T) {
	t.Parallel()

	store := newHistoryStore(appliedRow(1, "users", createUsers))
	migrator := newRecoveryMigrator(t, store, threeMigrations())

	require.NoError(t, migrator.Force(t.Context(), 2))

	assert.Equal(t, []string{"acquire", "record applied", "release"}, store.events)
	assert.True(t, store.ensured)
	assert.Equal(t, computeTestChecksum([]byte(addOrders)), store.rows[2].Checksum)
	assert.Equal(t, computeTestChecksum([]byte("DROP TABLE orders;\n")), store.rows[2].DownChecksum)
	assert.False(t, store.rows[2].Dirty)

	count, err := migrator.Up(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, count, "only the migration after the forced one runs")
}

func TestForceClearsADirtyMigrationAndItsProgress(t *testing.T) {
	t.Parallel()

	dirty := appliedRow(2, "orders", "the file before it was fixed")
	dirty.Dirty, dirty.LastStatement, dirty.SplitterVersion, dirty.StatementsHash = true, new(0), 1, "digest"
	store := newHistoryStore(appliedRow(1, "users", createUsers), dirty)
	migrator := newRecoveryMigrator(t, store, threeMigrations())

	require.NoError(t, migrator.Force(t.Context(), 2))

	row := store.rows[2]
	assert.False(t, row.Dirty)
	assert.Nil(t, row.LastStatement)
	assert.Empty(t, row.StatementsHash)
	assert.Equal(t, computeTestChecksum([]byte(addOrders)), row.Checksum)
	require.NoError(t, migrator.Validate(t.Context()))
}

func TestForceRefusesWhatItCannotForce(t *testing.T) {
	t.Parallel()

	laterDirty := appliedRow(3, "tags", addTags)
	laterDirty.Dirty = true
	tests := []struct {
		check    func(t *testing.T, err error)
		name     string
		rows     []migrate.AppliedMigration
		version  int64
		wantLock bool
	}{
		{
			name: "a version without a file", version: 9,
			check: func(t *testing.T, err error) {
				t.Helper()
				var unknown *migrate.UnknownMigrationError
				require.ErrorAs(t, err, &unknown)
				assert.Equal(t, int64(9), unknown.Version)
				assert.EqualError(t, err, "no migration file has version 9")
			},
		},
		{
			name: "a later applied migration", version: 1, wantLock: true,
			rows: []migrate.AppliedMigration{appliedRow(1, "users", createUsers), appliedRow(2, "orders", addOrders)},
			check: func(t *testing.T, err error) {
				t.Helper()
				var later *migrate.LaterMigrationError
				require.ErrorAs(t, err, &later)
				assert.Equal(t, migrate.LaterMigrationError{Version: 1, Later: 2, LaterDirty: false}, *later)
				assert.ErrorContains(t, err, "roll it back first, or force 2 instead")
				assert.NotContains(t, err.Error(), "sgl ")
			},
		},
		{
			name: "a later dirty migration", version: 2, wantLock: true,
			rows: []migrate.AppliedMigration{appliedRow(1, "users", createUsers), laterDirty},
			check: func(t *testing.T, err error) {
				t.Helper()
				var later *migrate.LaterMigrationError
				require.ErrorAs(t, err, &later)
				assert.True(t, later.LaterDirty)
				assert.ErrorContains(t, err, "repair the history of migration 3 first")
				assert.NotContains(t, err.Error(), "sgl ")
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			store := newHistoryStore(testCase.rows...)
			before := maps.Clone(store.rows)
			err := newRecoveryMigrator(t, store, threeMigrations()).Force(t.Context(), testCase.version)
			testCase.check(t, err)
			assert.Equal(t, before, store.rows, "nothing is recorded")
			assert.False(t, store.locked, "the lock is released")
			assert.Equal(t, testCase.wantLock, slices.Contains(store.events, "acquire"))
		})
	}
}

func TestRecoveryNeedsAHistoryEditor(t *testing.T) {
	t.Parallel()

	migrator := newRecoveryMigrator(t, &mockExecutor{}, threeMigrations())

	require.ErrorIs(t, migrator.Force(t.Context(), 1), migrate.ErrHistoryEditingUnsupported)
	_, err := migrator.PlanRepair(t.Context())
	require.ErrorIs(t, err, migrate.ErrHistoryEditingUnsupported)
	_, err = migrator.Repair(t.Context(), migrate.RepairOptions{Plan: &migrate.RepairPlan{}, Completed: nil})
	require.ErrorIs(t, err, migrate.ErrHistoryEditingUnsupported)
	assert.ErrorIs(t, migrate.ErrHistoryEditingUnsupported, migrate.ErrInvalidConfig)
}

func TestForceHonoursTheNonBlockingLock(t *testing.T) {
	t.Parallel()

	store := newHistoryStore()
	store.tryError = migrate.ErrLockNotAcquired
	migrator := newRecoveryMigrator(t, store, threeMigrations(), migrate.WithNonBlockingLock())

	err := migrator.Force(t.Context(), 1)
	require.ErrorAs(t, err, new(*migrate.LockAcquisitionError))
	require.ErrorIs(t, err, migrate.ErrLockNotAcquired)
	assert.Empty(t, store.rows)
}

func repairHistory() *historyStore {
	changed := appliedRow(1, "users", "CREATE TABLE users (id bigint);\n")
	dirty := appliedRow(2, "orders", addOrders)
	dirty.Dirty, dirty.LastStatement = true, new(0)
	missing := appliedRow(7, "gone", "whatever")
	return newHistoryStore(changed, dirty, missing)
}

func TestPlanRepairListsChangedDirtyAndMissingMigrations(t *testing.T) {
	t.Parallel()

	store := repairHistory()
	plan, err := newRecoveryMigrator(t, store, threeMigrations()).PlanRepair(t.Context())
	require.NoError(t, err)

	assert.Equal(t, &migrate.RepairPlan{
		Versions: nil,
		Changed: []migrate.ChangedMigration{{
			Name: "users", RecordedChecksum: computeTestChecksum([]byte("CREATE TABLE users (id bigint);\n")),
			FileChecksum: computeTestChecksum([]byte(createUsers)), RecordedDownChecksum: "", DownFileChecksum: "", Version: 1,
		}},
		Dirty: []migrate.DirtyMigration{{
			Name:          "orders",
			Statements:    []string{"CREATE TABLE orders (id int)", "CREATE INDEX orders_id ON orders (id)", "INSERT INTO orders VALUES (1)"},
			LastStatement: 0, Version: 2, ChecksumMatch: true,
		}},
		Missing: []int64{7},
	}, plan)
	assert.False(t, plan.Empty())
	assert.Empty(t, store.events, "planning takes no lock")
	assert.False(t, store.ensured, "planning runs no DDL")
}

func TestPlanRepairIsLimitedToTheVersionsAsked(t *testing.T) {
	t.Parallel()

	migrator := newRecoveryMigrator(t, repairHistory(), threeMigrations())

	plan, err := migrator.PlanRepair(t.Context(), 1)
	require.NoError(t, err)
	assert.Equal(t, []int64{1}, plan.Versions)
	assert.Len(t, plan.Changed, 1)
	assert.Empty(t, plan.Dirty)

	plan, err = migrator.PlanRepair(t.Context(), 3)
	require.NoError(t, err)
	assert.True(t, plan.Empty(), "an applied-free version with a file has nothing to repair")

	_, err = migrator.PlanRepair(t.Context(), 42)
	require.ErrorAs(t, err, new(*migrate.UnknownMigrationError))
}

func TestPlanRepairReportsAChangedDownFile(t *testing.T) {
	t.Parallel()

	row := appliedRow(2, "orders", addOrders)
	row.DownChecksum = computeTestChecksum([]byte("DROP TABLE orders CASCADE;\n"))
	plan, err := newRecoveryMigrator(t, newHistoryStore(row), threeMigrations()).PlanRepair(t.Context())
	require.NoError(t, err)
	require.Len(t, plan.Changed, 1)
	assert.Equal(t, computeTestChecksum([]byte("DROP TABLE orders;\n")), plan.Changed[0].DownFileChecksum)
}

func TestRepairResolvesDirtyMigrations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		check     func(t *testing.T, store *historyStore)
		name      string
		completed int
	}{
		{
			name: "no statement ran", completed: 0,
			check: func(t *testing.T, store *historyStore) {
				t.Helper()
				assert.NotContains(t, store.rows, int64(2), "the migration is pending again")
			},
		},
		{
			name: "some statements ran", completed: 2,
			check: func(t *testing.T, store *historyStore) {
				t.Helper()
				assert.True(t, store.rows[2].Dirty)
				assert.Equal(t, new(1), store.rows[2].LastStatement)
			},
		},
		{
			name: "every statement ran", completed: 3,
			check: func(t *testing.T, store *historyStore) {
				t.Helper()
				assert.False(t, store.rows[2].Dirty)
				assert.Nil(t, store.rows[2].LastStatement)
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			store := repairHistory()
			migrator := newRecoveryMigrator(t, store, threeMigrations())
			plan, err := migrator.PlanRepair(t.Context())
			require.NoError(t, err)

			result, err := migrator.Repair(t.Context(), migrate.RepairOptions{Plan: plan, Completed: map[int64]int{2: testCase.completed}})
			require.NoError(t, err)
			assert.Equal(t, migrate.RepairResult{Rebaselined: []int64{1}, Resolved: []int64{2}}, result)
			assert.Equal(t, computeTestChecksum([]byte(createUsers)), store.rows[1].Checksum, "the changed migration is re-baselined")
			assert.Contains(t, store.rows, int64(7), "a migration without a file is left alone")
			assert.False(t, store.locked)
			assert.Equal(t, "acquire", store.events[0])
			assert.Equal(t, "release", store.events[len(store.events)-1])
			testCase.check(t, store)
		})
	}
}

func TestRepairLeavesAnUnresolvedDirtyMigration(t *testing.T) {
	t.Parallel()

	store := repairHistory()
	migrator := newRecoveryMigrator(t, store, threeMigrations())
	plan, err := migrator.PlanRepair(t.Context())
	require.NoError(t, err)

	result, err := migrator.Repair(t.Context(), migrate.RepairOptions{Plan: plan, Completed: nil})
	require.NoError(t, err)
	assert.Equal(t, []int64{1}, result.Rebaselined)
	assert.Empty(t, result.Resolved)
	assert.True(t, store.rows[2].Dirty)
}

func TestRepairRefusesAnOutOfDatePlan(t *testing.T) {
	t.Parallel()

	store := repairHistory()
	migrator := newRecoveryMigrator(t, store, threeMigrations())
	plan, err := migrator.PlanRepair(t.Context())
	require.NoError(t, err)
	progressed := store.rows[2]
	progressed.LastStatement = new(1)
	store.rows[2] = progressed
	before := maps.Clone(store.rows)

	_, err = migrator.Repair(t.Context(), migrate.RepairOptions{Plan: plan, Completed: map[int64]int{2: 1}})
	require.ErrorIs(t, err, migrate.ErrRepairOutOfDate)
	assert.Equal(t, before, store.rows)
	assert.False(t, store.locked)
}

func TestRepairRejectsOptionsThatDoNotFitThePlan(t *testing.T) {
	t.Parallel()

	store := repairHistory()
	migrator := newRecoveryMigrator(t, store, threeMigrations())
	plan, err := migrator.PlanRepair(t.Context())
	require.NoError(t, err)

	tests := []struct {
		options migrate.RepairOptions
		name    string
		message string
	}{
		{name: "no plan", options: migrate.RepairOptions{Plan: nil, Completed: nil}, message: "Repair needs the plan"},
		{name: "a migration that is not dirty", options: migrate.RepairOptions{Plan: plan, Completed: map[int64]int{1: 0}}, message: "migration 1 is not a dirty migration of the plan"},
		{name: "more statements than the file has", options: migrate.RepairOptions{Plan: plan, Completed: map[int64]int{2: 4}}, message: "between 0 and 3 of them ran, not 4"},
		{name: "a negative count", options: migrate.RepairOptions{Plan: plan, Completed: map[int64]int{2: -1}}, message: "not -1"},
		{name: "the lowest of several unfit versions", options: migrate.RepairOptions{Plan: plan, Completed: map[int64]int{9: 0, 7: 0, 1: 0}}, message: "migration 1 is not a dirty migration of the plan"},
	}
	for _, testCase := range tests {
		_, err := migrator.Repair(t.Context(), testCase.options)
		require.ErrorIs(t, err, migrate.ErrInvalidConfig, testCase.name)
		assert.ErrorContains(t, err, testCase.message, testCase.name)
	}
	assert.Empty(t, store.events, "nothing is locked or written")
}

func TestLockTimeoutCutsTheWaitForTheLockShort(t *testing.T) {
	t.Parallel()

	store := newHistoryStore()
	store.acquire = func(ctx context.Context) error {
		<-ctx.Done()
		return context.Cause(ctx)
	}
	migrator := newRecoveryMigrator(t, store, threeMigrations(), migrate.WithLockTimeout(10*time.Millisecond))

	_, err := migrator.Up(t.Context())
	require.ErrorAs(t, err, new(*migrate.LockAcquisitionError))
	require.ErrorIs(t, err, migrate.ErrLockTimeout)
	assert.ErrorContains(t, err, "the migration lock was still held elsewhere after 10ms")
	assert.Empty(t, store.rows)

	err = migrator.Force(t.Context(), 1)
	require.ErrorIs(t, err, migrate.ErrLockTimeout)
}

func TestLockTimeoutEndsTheAcquisitionContextOnceTheLockIsHeld(t *testing.T) {
	t.Parallel()

	store := newHistoryStore()
	migrator := newRecoveryMigrator(t, store, threeMigrations(), migrate.WithLockTimeout(time.Hour))

	count, err := migrator.Up(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 3, count)
	require.Error(t, store.lockContext.Err(), "the lock must outlive the context its acquisition received")
}

func TestLockTimeoutKeepsAnUnrelatedCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancelCause(t.Context())
	store := newHistoryStore()
	store.acquire = func(lockContext context.Context) error {
		cancel(errors.New("deploy cancelled"))
		<-lockContext.Done()
		return context.Cause(lockContext)
	}
	migrator := newRecoveryMigrator(t, store, threeMigrations(), migrate.WithLockTimeout(time.Hour))

	_, err := migrator.Up(ctx)
	require.Error(t, err)
	assert.NotErrorIs(t, err, migrate.ErrLockTimeout)
	assert.ErrorContains(t, err, "deploy cancelled")
}

func TestRecoveryReportsItsStagesToTheObserver(t *testing.T) {
	t.Parallel()

	log := &stageLog{}
	store := repairHistory()
	migrator := newRecoveryMigrator(t, store, threeMigrations(), migrate.WithObserver(log))

	plan, err := migrator.PlanRepair(t.Context())
	require.NoError(t, err)
	_, err = migrator.Repair(t.Context(), migrate.RepairOptions{Plan: plan, Completed: map[int64]int{2: 3}})
	require.NoError(t, err)
	forced := newRecoveryMigrator(t, newHistoryStore(appliedRow(1, "users", createUsers)), threeMigrations(), migrate.WithObserver(log))
	require.NoError(t, forced.Force(t.Context(), 2))
	forceError := migrator.Force(t.Context(), 3)
	require.Error(t, forceError)

	require.Len(t, log.ended, 4)
	assert.Equal(t, []observe.Stage{observe.StageMigrateRepairPlan, observe.StageMigrateRepair, observe.StageMigrateForce, observe.StageMigrateForce},
		[]observe.Stage{log.ended[0].stage, log.ended[1].stage, log.ended[2].stage, log.ended[3].stage})
	assert.NoError(t, log.ended[2].err)
	assert.Equal(t, forceError, log.ended[3].err, "a stage ends with the error its call returns")
}

func TestPlanRepairGrowsLinearlyWithTheHistory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		limited bool
		smaller int
		larger  int
	}{
		{name: "every migration", limited: false, smaller: 2_000, larger: 8_000},
		{name: "limited to every version", limited: true, smaller: 2_000, larger: 8_000},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			planOver := func(count int) func() {
				files := fstest.MapFS{}
				rows := make([]migrate.AppliedMigration, 0, count)
				var versions []int64
				for version := int64(1); version <= int64(count); version++ {
					files[fmt.Sprintf("migrations/%05d_step.up.sql", version)] = &fstest.MapFile{Data: []byte(createUsers)}
					rows = append(rows, migrate.AppliedMigration{Version: version, Name: "step", Checksum: "recorded"})
					if testCase.limited {
						versions = append(versions, version)
					}
				}
				migrator, err := migrate.New(newHistoryStore(rows...), files, "migrations")
				require.NoError(t, err)
				return func() {
					plan, planError := migrator.PlanRepair(t.Context(), versions...)
					require.NoError(t, planError)
					require.Len(t, plan.Changed, count)
				}
			}

			scaling.Linear(t, planOver(testCase.smaller), planOver(testCase.larger))
		})
	}
}
