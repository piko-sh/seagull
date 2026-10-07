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

package migrate

import (
	"context"
	"os"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/sqlfile"
	"seagull.sh/seagull/internal/testkit/fakedialect"
	"seagull.sh/seagull/internal/testkit/fakefs"
)

var (
	_ dialect.DialectPort                   = (*fakedialect.Engine)(nil)
	_ dialect.DialectPort                   = (*fakedialect.CatalogueResolverEngine)(nil)
	_ dialect.CatalogueFunctionResolverPort = (*fakedialect.CatalogueResolverEngine)(nil)
	_ dialect.FunctionResolverPort          = (*fakedialect.FunctionResolverEngine)(nil)
	_ sqlfile.FileReader                    = (*fakefs.FileReader)(nil)
	_ os.DirEntry                           = (*fakefs.DirEntry)(nil)
	_ emit.EmitterPort                      = (*fakedialect.Emitter)(nil)
	_ ExecutorPort                          = (*mockMigrationExecutor)(nil)
	_ SeedExecutorPort                      = (*mockSeedExecutor)(nil)
)

type mockMigrationExecutor struct {
	ensureMigrationTableFn func(ctx context.Context) error
	acquireLockFn          func(ctx context.Context) error
	tryAcquireLockFn       func(ctx context.Context) error
	releaseLockFn          func(ctx context.Context) error
	appliedVersionsFn      func(ctx context.Context) ([]AppliedMigration, error)
	executeMigrationFn     func(ctx context.Context, migration MigrationRecord, direction MigrationDirection, useTransaction bool) error
}

type mockSeedExecutor struct {
	ensureSeedTableFn  func(ctx context.Context) error
	appliedSeedsFn     func(ctx context.Context) ([]AppliedSeed, error)
	executeSeedFn      func(ctx context.Context, seed SeedRecord) error
	clearSeedHistoryFn func(ctx context.Context) error
	acquireSeedLockFn  func(ctx context.Context) error
	releaseSeedLockFn  func(ctx context.Context) error
}

func (m *mockMigrationExecutor) EnsureMigrationTable(ctx context.Context) error {
	if m.ensureMigrationTableFn != nil {
		return m.ensureMigrationTableFn(ctx)
	}
	return nil
}

func (m *mockMigrationExecutor) AcquireLock(ctx context.Context) error {
	if m.acquireLockFn != nil {
		return m.acquireLockFn(ctx)
	}
	return nil
}

func (m *mockMigrationExecutor) TryAcquireLock(ctx context.Context) error {
	if m.tryAcquireLockFn != nil {
		return m.tryAcquireLockFn(ctx)
	}
	return nil
}

func (m *mockMigrationExecutor) ReleaseLock(ctx context.Context) error {
	if m.releaseLockFn != nil {
		return m.releaseLockFn(ctx)
	}
	return nil
}

func (m *mockMigrationExecutor) AppliedVersions(ctx context.Context) ([]AppliedMigration, error) {
	if m.appliedVersionsFn != nil {
		return m.appliedVersionsFn(ctx)
	}
	return nil, nil
}

func (m *mockMigrationExecutor) ExecuteMigration(ctx context.Context, migration MigrationRecord, direction MigrationDirection, useTransaction bool) error {
	if m.executeMigrationFn != nil {
		return m.executeMigrationFn(ctx, migration, direction, useTransaction)
	}
	return nil
}

func (m *mockSeedExecutor) EnsureSeedTable(ctx context.Context) error {
	if m.ensureSeedTableFn != nil {
		return m.ensureSeedTableFn(ctx)
	}
	return nil
}

func (m *mockSeedExecutor) AppliedSeeds(ctx context.Context) ([]AppliedSeed, error) {
	if m.appliedSeedsFn != nil {
		return m.appliedSeedsFn(ctx)
	}
	return nil, nil
}

func (m *mockSeedExecutor) ExecuteSeed(ctx context.Context, seed SeedRecord) error {
	if m.executeSeedFn != nil {
		return m.executeSeedFn(ctx, seed)
	}
	return nil
}

func (m *mockSeedExecutor) ClearSeedHistory(ctx context.Context) error {
	if m.clearSeedHistoryFn != nil {
		return m.clearSeedHistoryFn(ctx)
	}
	return nil
}

func (m *mockSeedExecutor) AcquireSeedLock(ctx context.Context) error {
	if m.acquireSeedLockFn != nil {
		return m.acquireSeedLockFn(ctx)
	}
	return nil
}

func (m *mockSeedExecutor) ReleaseSeedLock(ctx context.Context) error {
	if m.releaseSeedLockFn != nil {
		return m.releaseSeedLockFn(ctx)
	}
	return nil
}
