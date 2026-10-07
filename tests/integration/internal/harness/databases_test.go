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

package harness_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/tests/integration/internal/harness"
)

func TestCaseNameIsAnIdentifierPerCase(t *testing.T) {
	t.Parallel()

	require.Equal(t, "test_012_slice_in_clause", harness.CaseName("testdata/012_slice-in-clause"))
}

func TestCaseMigrationsReadsOnlyForwardMigrationsInVersionOrder(t *testing.T) {
	t.Parallel()

	caseDirectory := t.TempDir()
	migrations := filepath.Join(caseDirectory, "migrations")
	require.NoError(t, os.MkdirAll(migrations, 0o750))
	for name, content := range map[string]string{
		"0001_boats.up.sql":     "CREATE TABLE boats (id INT);",
		"0001_boats.down.sql":   "DROP TABLE boats;",
		"0002_docks.up.sql":     "CREATE TABLE docks (id INT);",
		"0002_docks.down.sql":   "DROP TABLE docks;",
		"10_harbours.up.sql":    "CREATE TABLE harbours (id INT);",
		"9_moorings.up.sql":     "CREATE TABLE moorings (id INT);",
		"notes.sql":             "SELECT 1;",
		"seed_boats.up.sql.bak": "INSERT INTO boats VALUES (1);",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(migrations, name), []byte(content), 0o600))
	}

	tests := []struct {
		name          string
		caseDirectory string
		wantNames     []string
	}{
		{name: "forward migrations by version", caseDirectory: caseDirectory,
			wantNames: []string{"0001_boats.up.sql", "0002_docks.up.sql", "9_moorings.up.sql", "10_harbours.up.sql"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			files := harness.CaseMigrations(t, test.caseDirectory)
			names := make([]string, 0, len(files))
			for _, file := range files {
				names = append(names, file.Name)
			}
			require.Equal(t, test.wantNames, names)
			require.Equal(t, "CREATE TABLE boats (id INT);", files[0].Content)
		})
	}
}

func TestIsUpMigrationAcceptsOnlyTheForwardForm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		file string
		want bool
	}{
		{name: "a forward migration", file: "0001_create_widgets.up.sql", want: true},
		{name: "a down migration", file: "0001_create_widgets.down.sql", want: false},
		{name: "a file without a direction", file: "0001_create_widgets.sql", want: false},
		{name: "a file without a version", file: "create_widgets.up.sql", want: false},
		{name: "a version without a name", file: "0001.up.sql", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, test.want, harness.IsUpMigration(test.file))
		})
	}
}

func TestSchemasRunEachCaseInASchemaOfItsOwnThatIsDroppedAfterwards(t *testing.T) {
	t.Parallel()

	caseDirectory := writeCaseMigrations(t, "007-boats")
	tests := []struct {
		prepare       func(t *testing.T, schemas harness.Schemas) []string
		name          string
		wantVariables bool
	}{
		{name: "prepared for a runner", wantVariables: true, prepare: func(t *testing.T, schemas harness.Schemas) []string {
			t.Helper()
			return schemas.Prepare(t.Context(), t, caseDirectory)
		}},
		{name: "created for a test", wantVariables: false, prepare: func(t *testing.T, schemas harness.Schemas) []string {
			t.Helper()
			require.Equal(t, "test_007_boats", schemas.Create(t, caseDirectory))
			return nil
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			server, name := newFakeServer(t)
			schemas := harness.Schemas{
				Driver:     fakeDriverName,
				Connection: "fake://" + name + "/postgres",
				Setup:      []string{"CREATE EXTENSION IF NOT EXISTS citext"},
				SearchPath: []string{"public"},
			}
			t.Run("the case", func(t *testing.T) {
				variables := test.prepare(t, schemas)
				if test.wantVariables {
					require.Equal(t, []string{"DATABASE_URL=fake://" + name + "/postgres", "DATABASE_SCHEMA=test_007_boats"}, variables)
				}
			})
			require.Equal(t, []string{
				"CREATE EXTENSION IF NOT EXISTS citext",
				"DROP SCHEMA IF EXISTS test_007_boats CASCADE",
				"CREATE SCHEMA test_007_boats",
				"SET search_path TO test_007_boats, public;\nCREATE TABLE boats (id INT);",
				"SET search_path TO test_007_boats, public;\nCREATE TABLE docks (id INT);",
				"DROP SCHEMA IF EXISTS test_007_boats CASCADE",
			}, server.recorded())
		})
	}
}

func TestDatabasesRunEachCaseInADatabaseOfItsOwnThatIsDroppedAfterwards(t *testing.T) {
	t.Parallel()

	caseDirectory := writeCaseMigrations(t, "012-docks")
	tests := []struct {
		name    string
		wantRun []string
	}{
		{
			name: "the runner reads the case's database",
			wantRun: []string{
				"DROP DATABASE IF EXISTS test_012_docks",
				"CREATE DATABASE test_012_docks",
				"USE test_012_docks",
				"CREATE TABLE boats (id INT);",
				"CREATE TABLE docks (id INT);",
				"DROP DATABASE IF EXISTS test_012_docks",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			server, name := newFakeServer(t)
			databases := harness.Databases{Driver: fakeDriverName, Connection: name + "/sgl_test?multiStatements=true"}
			t.Run("the case", func(t *testing.T) {
				variables := databases.Prepare(t.Context(), t, caseDirectory)
				require.Equal(t, []string{"DATABASE_URL=" + name + "/test_012_docks?parseTime=true"}, variables)
			})
			require.Equal(t, test.wantRun, server.recorded())
		})
	}
}

func writeCaseMigrations(t *testing.T, caseName string) string {
	t.Helper()

	caseDirectory := filepath.Join(t.TempDir(), caseName)
	migrations := filepath.Join(caseDirectory, "migrations")
	require.NoError(t, os.MkdirAll(migrations, 0o750))
	for name, content := range map[string]string{
		"0001_boats.up.sql":   "CREATE TABLE boats (id INT);",
		"0001_boats.down.sql": "DROP TABLE boats;",
		"0002_docks.up.sql":   "CREATE TABLE docks (id INT);",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(migrations, name), []byte(content), 0o600))
	}
	return caseDirectory
}
