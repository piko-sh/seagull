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
	"testing"

	"seagull.sh/seagull/internal/apitest"
	"seagull.sh/seagull/sdk/migrate"
)

func TestMigrateFacadeAPI(t *testing.T) {
	t.Parallel()

	surface := apitest.Surface{
		"Migrator":                migrate.Migrator{},
		"Seeder":                  migrate.Seeder{},
		"Option":                  migrate.Option(nil),
		"MigrationStatus":         migrate.MigrationStatus{},
		"SeedStatus":              migrate.SeedStatus{},
		"RepairPlan":              migrate.RepairPlan{},
		"DirtyMigration":          migrate.DirtyMigration{},
		"RepairOptions":           migrate.RepairOptions{},
		"RepairResult":            migrate.RepairResult{},
		"MigrationHookContext":    migrate.MigrationHookContext{},
		"MigrationRunHookContext": migrate.MigrationRunHookContext{},

		"MigrationDirection": migrate.MigrationDirection(0),

		"MigrationDirectionUp":   migrate.MigrationDirectionUp,
		"MigrationDirectionDown": migrate.MigrationDirectionDown,

		"Backend":         migrate.Backend{},
		"BackendOption":   migrate.BackendOption(nil),
		"BackendSpec":     migrate.BackendSpec{},
		"Names":           migrate.Names{},
		"AdvisoryLockSQL": migrate.AdvisoryLockSQL{},
		"TableLockSQL":    migrate.TableLockSQL{},

		"LockMode": migrate.LockMode(0),

		"LockAdvisory": migrate.LockAdvisory,
		"LockTable":    migrate.LockTable,
		"LockNone":     migrate.LockNone,

		"ExecutorPort":      (*migrate.ExecutorPort)(nil),
		"HistoryEditorPort": (*migrate.HistoryEditorPort)(nil),
		"SeedExecutorPort":  (*migrate.SeedExecutorPort)(nil),
		"SeedLockTrierPort": (*migrate.SeedLockTrierPort)(nil),
		"AppliedMigration":  migrate.AppliedMigration{},
		"MigrationRecord":   migrate.MigrationRecord{},
		"AppliedSeed":       migrate.AppliedSeed{},
		"SeedRecord":        migrate.SeedRecord{},
		"Executor":          migrate.Executor{},
		"ExecutorOption":    migrate.ExecutorOption(nil),

		"ChecksumMismatchError":          migrate.ChecksumMismatchError{},
		"DownChecksumMismatchError":      migrate.DownChecksumMismatchError{},
		"MigrationExecutionError":        migrate.MigrationExecutionError{},
		"SeedExecutionError":             migrate.SeedExecutionError{},
		"LockAcquisitionError":           migrate.LockAcquisitionError{},
		"MissingMigrationFileError":      migrate.MissingMigrationFileError{},
		"NoDownMigrationError":           migrate.NoDownMigrationError{},
		"DirtyMigrationError":            migrate.DirtyMigrationError{},
		"UnknownMigrationError":          migrate.UnknownMigrationError{},
		"LaterMigrationError":            migrate.LaterMigrationError{},
		"ResumeMismatchError":            migrate.ResumeMismatchError{},
		"DuplicateMigrationVersionError": migrate.DuplicateMigrationVersionError{},
		"DuplicateSeedVersionError":      migrate.DuplicateSeedVersionError{},
		"HookPanicError":                 migrate.HookPanicError{},

		"ErrLockNotAcquired":   migrate.ErrLockNotAcquired,
		"ErrLockTimeout":       migrate.ErrLockTimeout,
		"ErrLockLost":          migrate.ErrLockLost,
		"ErrDirtyRollback":     migrate.ErrDirtyRollback,
		"ErrRepairOutOfDate":   migrate.ErrRepairOutOfDate,
		"ErrUnsupportedLock":   migrate.ErrUnsupportedLock,
		"ErrInvalidIdentifier": migrate.ErrInvalidIdentifier,

		"Rules":               migrate.Rules{},
		"QuoteRules":          migrate.QuoteRules{},
		"CommentRules":        migrate.CommentRules{},
		"ParameterRules":      migrate.ParameterRules{},
		"ProceduralBlockFunc": migrate.ProceduralBlockFunc(nil),
		"StatementPredicate":  migrate.StatementPredicate(nil),
		"Token":               migrate.Token{},

		"DefaultTerminator": migrate.DefaultTerminator,
		"TokenKind":         migrate.TokenKind(0),

		"KindWhitespace":        migrate.KindWhitespace,
		"KindComment":           migrate.KindComment,
		"KindWord":              migrate.KindWord,
		"KindQuotedIdentifier":  migrate.KindQuotedIdentifier,
		"KindString":            migrate.KindString,
		"KindNumber":            migrate.KindNumber,
		"KindParameter":         migrate.KindParameter,
		"KindPunctuation":       migrate.KindPunctuation,
		"KindExecutableComment": migrate.KindExecutableComment,

		"EscapeMode": migrate.EscapeMode(0),

		"EscapeDoubled":            migrate.EscapeDoubled,
		"EscapeBackslash":          migrate.EscapeBackslash,
		"EscapeDoubledOrBackslash": migrate.EscapeDoubledOrBackslash,

		"DoubleQuoteMeaning": migrate.DoubleQuoteMeaning(0),

		"DoubleQuoteIdentifier": migrate.DoubleQuoteIdentifier,
		"DoubleQuoteString":     migrate.DoubleQuoteString,

		"NewMigrator":               migrate.NewMigrator,
		"NewSeeder":                 migrate.NewSeeder,
		"NewMigratorFor":            migrate.NewMigratorFor,
		"NewSeederFor":              migrate.NewSeederFor,
		"CustomBackend":             migrate.CustomBackend,
		"NewExecutor":               migrate.NewExecutor,
		"WithProgressBatchSize":     migrate.WithProgressBatchSize,
		"WithBeforeMigration":       migrate.WithBeforeMigration,
		"WithAfterMigration":        migrate.WithAfterMigration,
		"WithBeforeRun":             migrate.WithBeforeRun,
		"WithAfterRun":              migrate.WithAfterRun,
		"WithNonBlockingLock":       migrate.WithNonBlockingLock,
		"WithLockTimeout":           migrate.WithLockTimeout,
		"WithDirectivePrefixes":     migrate.WithDirectivePrefixes,
		"WithLogger":                migrate.WithLogger,
		"WithObserver":              migrate.WithObserver,
		"WithNamespace":             migrate.WithNamespace,
		"WithHistoryTable":          migrate.WithHistoryTable,
		"WithLock":                  migrate.WithLock,
		"WithDriverMultiStatements": migrate.WithDriverMultiStatements,
	}

	apitest.Check(t, surface, "facade_test.golden.yaml")
}
