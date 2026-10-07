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
	"errors"
	"maps"
	"os"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
	"seagull.sh/seagull/internal/testkit/fakefs"
)

type replayRecorder struct {
	replayed []string
	mu       sync.Mutex
}

func (recorder *replayRecorder) engine() *fakedialect.Engine {
	return &fakedialect.Engine{
		ParseStatementsFunc: func(_ context.Context, sql string) ([]ast.ParsedStatement, error) {
			recorder.mu.Lock()
			defer recorder.mu.Unlock()
			recorder.replayed = append(recorder.replayed, sql)
			return nil, nil
		},
	}
}

func TestNewMigrationCatalogueProvider(t *testing.T) {
	t.Parallel()

	engine := &fakedialect.Engine{}
	fileReader := &fakefs.FileReader{}

	provider := NewMigrationCatalogueProvider(engine, fileReader, "/migrations", namespace.Default(), Seed{})

	require.NotNil(t, provider)
	assert.Equal(t, engine, provider.engine)
	assert.Equal(t, fileReader, provider.fileReader)
	assert.Equal(t, "/migrations", provider.directory)
}

func TestMigrationCatalogueProvider_BuildCatalogue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		engine           *fakedialect.Engine
		fileReader       *fakefs.FileReader
		directory        string
		wantErrorMessage string
		wantDiagnostics  int
		wantSchemas      int
	}{
		{
			name:   "empty directory returns empty catalogue with no diagnostics",
			engine: &fakedialect.Engine{},
			fileReader: &fakefs.FileReader{
				Directories: map[string][]os.DirEntry{
					"/migrations": {},
				},
			},
			directory:       "/migrations",
			wantDiagnostics: 0,
			wantSchemas:     1,
		},
		{
			name:   "ReadDir error returns error",
			engine: &fakedialect.Engine{},
			fileReader: &fakefs.FileReader{
				ReadDirErrors: map[string]error{
					"/migrations": errors.New("permission denied"),
				},
			},
			directory:        "/migrations",
			wantErrorMessage: "reading migration directory",
		},
		{
			name: "single migration file builds catalogue with mutations",
			engine: &fakedialect.Engine{
				ParseStatementsFunc: func(_ context.Context, sql string) ([]ast.ParsedStatement, error) {
					return []ast.ParsedStatement{
						{Location: 0, Length: len(sql)},
					}, nil
				},
				ApplyDDLFunc: func(_ context.Context, _ ast.ParsedStatement) (*ast.CatalogueMutation, error) {
					return &ast.CatalogueMutation{
						Kind:       ast.MutationCreateTable,
						SchemaName: "public",
						TableName:  "users",
						Columns: []sqlmodel.Column{
							{
								Name:    "id",
								SQLType: sqlmodel.SQLType{EngineName: "integer"},
							},
						},
					}, nil
				},
			},
			fileReader: &fakefs.FileReader{
				Directories: map[string][]os.DirEntry{
					"/migrations": {
						&fakefs.DirEntry{EntryName: "001_create_users.up.sql", Directory: false},
					},
				},
				Files: map[string][]byte{
					"/migrations/001_create_users.up.sql": []byte("CREATE TABLE users (id INTEGER NOT NULL);"),
				},
			},
			directory:       "/migrations",
			wantDiagnostics: 0,
			wantSchemas:     1,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			provider := NewMigrationCatalogueProvider(testCase.engine, testCase.fileReader, testCase.directory, namespace.Default(), Seed{})
			ctx := t.Context()

			catalogue, diagnostics, err := provider.BuildCatalogue(ctx)

			if testCase.wantErrorMessage != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.wantErrorMessage)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, catalogue)
			assert.Len(t, diagnostics, testCase.wantDiagnostics)
			assert.Len(t, catalogue.Schemas, testCase.wantSchemas)
		})
	}
}

func migrationDirectory(files map[string]string) *fakefs.FileReader {
	reader := &fakefs.FileReader{
		Directories: map[string][]os.DirEntry{"/migrations": nil},
		Files:       make(map[string][]byte, len(files)),
	}
	for _, name := range slices.Sorted(maps.Keys(files)) {
		reader.Directories["/migrations"] = append(reader.Directories["/migrations"], &fakefs.DirEntry{EntryName: name, Directory: false})
		reader.Files["/migrations/"+name] = []byte(files[name])
	}
	return reader
}

func TestBuildCatalogueReplaysMigrationsAsTheMigrationRunnerSelectsThem(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		files        map[string]string
		name         string
		wantReplayed []string
		wantWarned   []string
	}{
		{
			name:         "versions are ordered as numbers, not as file names",
			files:        map[string]string{"10_later.up.sql": "ten", "9_earlier.up.sql": "nine"},
			wantReplayed: []string{"nine", "ten"},
		},
		{
			name:         "down migrations are not replayed",
			files:        map[string]string{"1_init.up.sql": "up", "1_init.down.sql": "down"},
			wantReplayed: []string{"up"},
		},
		{
			name:         "a .sql file outside the naming convention is warned about and not replayed",
			files:        map[string]string{"1_init.up.sql": "up", "schema.sql": "stray", "init.up.sql": "unversioned"},
			wantReplayed: []string{"up"},
			wantWarned:   []string{"init.up.sql", "schema.sql"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			recorder := &replayRecorder{}
			provider := NewMigrationCatalogueProvider(recorder.engine(), migrationDirectory(testCase.files), "/migrations", namespace.Default(), Seed{})

			_, diagnostics, err := provider.BuildCatalogue(t.Context())
			require.NoError(t, err)
			require.Equal(t, testCase.wantReplayed, recorder.replayed)

			var warned []string
			for _, diagnostic := range diagnostics {
				require.Equal(t, diag.CodeIgnoredMigrationFile, diagnostic.Code)
				warned = append(warned, diagnostic.Filename)
			}
			require.ElementsMatch(t, testCase.wantWarned, warned)
		})
	}
}

func TestBuildCatalogueRefusesADirectoryWithoutAnUpMigration(t *testing.T) {
	t.Parallel()

	recorder := &replayRecorder{}
	files := migrationDirectory(map[string]string{"001_schema.sql": "schema", "002_orders.sql": "orders"})
	provider := NewMigrationCatalogueProvider(recorder.engine(), files, "/migrations", namespace.Default(), Seed{})

	_, diagnostics, err := provider.BuildCatalogue(t.Context())
	require.NoError(t, err)
	require.Len(t, diagnostics, 1)
	assert.Equal(t, diag.CodeIgnoredMigrationFile, diagnostics[0].Code)
	assert.Equal(t, diag.SeverityError, diagnostics[0].Severity)
	assert.Contains(t, diagnostics[0].Message, `"/migrations"`)
	assert.Empty(t, recorder.replayed)
}

func TestBuildCatalogueReportsTwoMigrationsSharingAVersion(t *testing.T) {
	t.Parallel()

	recorder := &replayRecorder{}
	files := migrationDirectory(map[string]string{"1_users.up.sql": "users", "1_orders.up.sql": "orders", "2_more.up.sql": "more"})
	provider := NewMigrationCatalogueProvider(recorder.engine(), files, "/migrations", namespace.Default(), Seed{})

	_, diagnostics, err := provider.BuildCatalogue(t.Context())
	require.NoError(t, err)
	require.Len(t, diagnostics, 1)
	require.Equal(t, diag.CodeDuplicateMigrationVersion, diagnostics[0].Code)
	require.Equal(t, diag.SeverityError, diagnostics[0].Severity)
	require.Equal(t, "1_users.up.sql", diagnostics[0].Filename)
	require.Contains(t, diagnostics[0].Message, "1_orders.up.sql")
	require.Equal(t, []string{"orders", "more"}, recorder.replayed)
}

func TestBuildCatalogueRefusesAnUpMigrationHoldingAnotherToolsDownSection(t *testing.T) {
	t.Parallel()

	recorder := &replayRecorder{}
	files := migrationDirectory(map[string]string{
		"1_users.up.sql": "-- +goose Up\nCREATE TABLE users (id INT);\n\n-- +goose Down\nDROP TABLE users;\n",
		"2_more.up.sql":  "CREATE TABLE more (id INT);\n",
	})
	provider := NewMigrationCatalogueProvider(recorder.engine(), files, "/migrations", namespace.Default(), Seed{})

	_, diagnostics, err := provider.BuildCatalogue(t.Context())

	require.NoError(t, err)
	require.Len(t, diagnostics, 1)
	require.Equal(t, diag.CodeIgnoredMigrationFile, diagnostics[0].Code)
	require.Equal(t, diag.SeverityError, diagnostics[0].Severity)
	require.Equal(t, "1_users.up.sql", diagnostics[0].Filename)
	require.Equal(t, 4, diagnostics[0].Line)
	require.Contains(t, diagnostics[0].Message, "goose down section")
	require.Equal(t, "move the statements from line 4 on into 1_users.down.sql", diagnostics[0].Suggestion)
}

func TestBuildCatalogueReturnsACancellationDuringTheLastMigrationAsAnError(t *testing.T) {
	t.Parallel()

	cause := errors.New("the run was stopped")
	testCases := []struct {
		name     string
		cancelOn string
	}{
		{name: "while parsing the last migration", cancelOn: "parse"},
		{name: "while applying its last statement", cancelOn: "apply"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			engine := &fakedialect.Engine{
				SchemaSearchPathFunc: func() []string { return []string{"public"} },
				ParseStatementsFunc: func(ctx context.Context, sql string) ([]ast.ParsedStatement, error) {
					if sql == "last" && testCase.cancelOn == "parse" {
						cancel(cause)
						return nil, context.Cause(ctx)
					}
					return []ast.ParsedStatement{{Location: 0, Length: len(sql)}}, nil
				},
				ApplyDDLFunc: func(context.Context, ast.ParsedStatement) (*ast.CatalogueMutation, error) {
					if testCase.cancelOn == "apply" {
						cancel(cause)
					}
					return nil, nil
				},
			}
			files := migrationDirectory(map[string]string{"1_first.up.sql": "first", "2_last.up.sql": "last"})
			if testCase.cancelOn == "apply" {
				files = migrationDirectory(map[string]string{"1_last.up.sql": "last"})
			}
			provider := NewMigrationCatalogueProvider(engine, files, "/migrations", namespace.Default(), Seed{})

			catalogue, diagnostics, err := provider.BuildCatalogue(ctx)

			require.ErrorIs(t, err, cause)
			require.Nil(t, catalogue)
			require.Empty(t, diagnostics)
		})
	}
}

func TestBuildCatalogueRecoversAPanicInTheDialect(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name               string
		engine             *fakedialect.Engine
		wantErr            bool
		wantDiagnosticCode string
	}{
		{
			name: "reading the dialect's settings",
			engine: &fakedialect.Engine{
				InfoFunc: func() dialect.Info { panic("info") },
			},
			wantErr:            true,
			wantDiagnosticCode: "",
		},
		{
			name: "normalising a custom function's types",
			engine: &fakedialect.Engine{
				SchemaSearchPathFunc:  func() []string { return []string{"public"} },
				NormaliseTypeNameFunc: func(string, ...int) sqlmodel.SQLType { panic("normalise") },
			},
			wantErr:            true,
			wantDiagnosticCode: "",
		},
		{
			name: "reading the built-in functions",
			engine: &fakedialect.Engine{
				SchemaSearchPathFunc: func() []string { return []string{"public"} },
				ParseStatementsFunc: func(context.Context, string) ([]ast.ParsedStatement, error) {
					return nil, nil
				},
				BuiltinFunctionsFunc: func() *ast.FunctionCatalogue { panic("builtins") },
			},
			wantErr:            false,
			wantDiagnosticCode: diag.CodeRecoveredPanic,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			seed := Seed{CustomFunctions: []sqlmodel.CustomFunctionConfig{{Name: "touch", ReturnType: "int"}}, ColumnOverrides: nil}
			provider := NewMigrationCatalogueProvider(testCase.engine, migrationDirectory(map[string]string{"1_init.up.sql": "init"}),
				"/migrations", namespace.Default(), seed)

			var (
				catalogue   *sqlmodel.Catalogue
				diagnostics []diag.SourceError
				err         error
			)
			require.NotPanics(t, func() { catalogue, diagnostics, err = provider.BuildCatalogue(t.Context()) })

			if testCase.wantErr {
				require.ErrorIs(t, err, fault.ErrPanic)
				require.Nil(t, catalogue)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, catalogue)
			require.Len(t, diagnostics, 1)
			require.Equal(t, testCase.wantDiagnosticCode, diagnostics[0].Code)
		})
	}
}
