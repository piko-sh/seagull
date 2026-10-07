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

package catalogue

import (
	"context"
	"fmt"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/panicguard"
	"seagull.sh/seagull/internal/sqlfile"
	"seagull.sh/seagull/internal/sqlmodel"
)

// Seed holds what a run adds to the catalogue ahead of the migrations, so that every view
// and function body a migration creates is analysed with it in place.
type Seed struct {
	// CustomFunctions declares functions the dialect does not know. They must have passed
	// ValidateCustomFunctions.
	CustomFunctions []sqlmodel.CustomFunctionConfig

	// ColumnOverrides holds the configured type overrides; those naming a column set its Go
	// type before any view over it is analysed, so the view's column carries it too.
	ColumnOverrides []sqlmodel.TypeOverride
}

// MigrationCatalogueProvider builds a schema catalogue by reading migration SQL files and
// replaying their DDL statements. The pipeline uses it when the generator has no schema
// source; its BuildCatalogue returns a fresh catalogue the caller owns.
type MigrationCatalogueProvider struct {
	// engine holds the SQL dialect parser and DDL interpreter.
	engine dialect.DialectPort

	// fileReader holds the filesystem access adapter.
	fileReader sqlfile.FileReader

	// directory holds the path to the migration files.
	directory string

	// prefixes holds the directive prefixes recognised in migration comments.
	prefixes namespace.Set

	// seed holds what the catalogue starts with ahead of the migrations.
	seed Seed
}

// NewMigrationCatalogueProvider creates a catalogue provider that reads migration files
// from the given directory.
//
// Takes engine (dialect.DialectPort) which provides SQL parsing and DDL interpretation.
// Takes fileReader (sqlfile.FileReader) which provides filesystem access.
// Takes directory (string) which is the path to the migration files.
// Takes prefixes (namespace.Set) which names the directive prefixes recognised in
// migration comments.
// Takes seed (Seed) which holds the custom functions and column overrides the catalogue
// starts with.
//
// Returns *MigrationCatalogueProvider which is ready to build catalogues.
func NewMigrationCatalogueProvider(
	engine dialect.DialectPort,
	fileReader sqlfile.FileReader,
	directory string,
	prefixes namespace.Set,
	seed Seed,
) *MigrationCatalogueProvider {
	return &MigrationCatalogueProvider{
		engine:     engine,
		fileReader: fileReader,
		directory:  directory,
		prefixes:   prefixes,
		seed:       seed,
	}
}

// BuildCatalogue builds a catalogue by replaying up migrations in version order.
//
// Invalid filenames and duplicate versions produce diagnostics. A directory containing
// SQL but no up migrations is an error. A panic in the dialect while a statement is
// applied is reported as a Q070 error on the statement, and one while the dialect's
// built-in functions are read as an unlocated Q070 error.
//
// Returns *sqlmodel.Catalogue which holds the built schema state.
// Returns []diag.SourceError which contains any diagnostics from parsing or applying
// migrations.
// Returns error when reading migration files fails, the dialect panics while the replay
// is prepared, or the context is cancelled, wrapping its cause.
func (provider *MigrationCatalogueProvider) BuildCatalogue(
	ctx context.Context,
) (*sqlmodel.Catalogue, []diag.SourceError, error) {
	builder, prepareError := panicguard.RunValue(ctx, "catalogue: prepare the replay", func() (*catalogueBuilder, error) {
		prepared := newCatalogueBuilder(provider.engine, provider.prefixes)
		prepared.applySeed(provider.seed)
		return prepared, nil
	})
	if prepareError != nil {
		return nil, nil, fmt.Errorf("building catalogue from migrations: %w", prepareError)
	}

	listed, readError := sqlfile.ListMigrationFiles(ctx, provider.fileReader, provider.directory)
	if readError != nil {
		return nil, nil, readError
	}

	allDiagnostics := sqlfile.MigrationNamingDiagnostics(ctx, provider.fileReader, provider.directory, listed)
	upMigrations, duplicateDiagnostics := upMigrationsToReplay(listed)
	allDiagnostics = append(allDiagnostics, duplicateDiagnostics...)

	for index := range upMigrations {
		if err := context.Cause(ctx); err != nil {
			return nil, allDiagnostics, fmt.Errorf("building catalogue from migrations cancelled: %w", err)
		}
		migration := upMigrations[index]
		if err := sqlfile.ReadMigrationContent(ctx, provider.fileReader, provider.directory, &migration); err != nil {
			return nil, allDiagnostics, err
		}
		if section, found := sqlfile.FindForeignDownSection(migration.Content); found {
			allDiagnostics = append(allDiagnostics, sqlfile.ForeignDownSectionError(migration.Filename, section))
		}
		diagnostics, err := builder.ApplyMigration(ctx, migration.Filename, migration.Content, index)
		allDiagnostics = append(allDiagnostics, diagnostics...)
		if err != nil {
			return nil, allDiagnostics, fmt.Errorf("building catalogue from migrations cancelled: %w", err)
		}
	}
	if err := context.Cause(ctx); err != nil {
		return nil, allDiagnostics, fmt.Errorf("building catalogue from migrations cancelled: %w", err)
	}

	catalogue := builder.Catalogue()
	builtins, builtinsError := panicguard.RunValue(ctx, "catalogue: read built-in functions", func() (*ast.FunctionCatalogue, error) {
		return provider.engine.BuiltinFunctions(), nil
	})
	if builtinsError != nil {
		allDiagnostics = append(allDiagnostics, diagbuild.Unlocated(diag.CodeRecoveredPanic, diag.SeverityError,
			"reading the dialect's built-in functions: "+builtinsError.Error()))
	}
	propagateDataAccess(catalogue, builder.calledFunctions, builder.calledTableFunctions, builtins)

	return catalogue, allDiagnostics, nil
}

// upMigrationsToReplay keeps the up migrations of a listing, in order, dropping each file
// that shares its version with an earlier one and reporting it.
//
// Takes listed ([]sqlfile.MigrationFile) which are the listed migrations in version
// order, up before down.
//
// Returns []sqlfile.MigrationFile which holds the up migrations to replay.
// Returns []diag.SourceError which holds an error for each dropped duplicate.
func upMigrationsToReplay(listed []sqlfile.MigrationFile) ([]sqlfile.MigrationFile, []diag.SourceError) {
	var upMigrations []sqlfile.MigrationFile
	var diagnostics []diag.SourceError
	for _, migration := range listed {
		if migration.Down {
			continue
		}
		if count := len(upMigrations); count > 0 && upMigrations[count-1].Version == migration.Version {
			diagnostics = append(diagnostics, duplicateVersionDiagnostic(upMigrations[count-1], migration))
			continue
		}
		upMigrations = append(upMigrations, migration)
	}
	return upMigrations, diagnostics
}

// duplicateVersionDiagnostic reports a migration that shares its version with an earlier
// one.
//
// Takes first (sqlfile.MigrationFile) which is the file replayed for the version.
// Takes duplicate (sqlfile.MigrationFile) which is the file that is not.
//
// Returns diag.SourceError which is the error, located at the duplicate's first line.
func duplicateVersionDiagnostic(first, duplicate sqlfile.MigrationFile) diag.SourceError {
	return diagbuild.BlockWithSuggestion(duplicate.Filename, 1, diag.CodeDuplicateMigrationVersion, diag.SeverityError,
		fmt.Sprintf("%s has version %d, as %s does; the migration runner refuses two migrations with one version, "+
			"so only %s is replayed", duplicate.Filename, duplicate.Version, first.Filename, first.Filename),
		"give "+duplicate.Filename+" the next free version")
}
