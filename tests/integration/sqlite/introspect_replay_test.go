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

//go:build integration

package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite"
	"seagull.sh/seagull/sdk/schema/schema_source_sqlite"
	"seagull.sh/seagull/tests/integration/internal/harness"
)

const (
	replayTimeout    = 2 * time.Minute
	featureMigration = `
CREATE TABLE harbours (
	id INTEGER PRIMARY KEY,
	name VARCHAR(100) NOT NULL UNIQUE,
	depth DECIMAL(6, 2),
	opened_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	name_upper TEXT GENERATED ALWAYS AS (upper(name)) VIRTUAL,
	name_length INTEGER GENERATED ALWAYS AS (length(name)) STORED
);
CREATE TABLE berths (
	harbour_id INTEGER NOT NULL REFERENCES harbours (id),
	number INTEGER NOT NULL,
	PRIMARY KEY (harbour_id, number)
) WITHOUT ROWID;
CREATE TABLE logbook (entry INTEGER PRIMARY KEY AUTOINCREMENT, note TEXT CHECK (length(note) < 500));
CREATE TABLE tides (code TEXT PRIMARY KEY, height REAL NOT NULL) STRICT;
CREATE TABLE crews (id INTEGER, name TEXT, PRIMARY KEY (id));
ALTER TABLE harbours ADD COLUMN country TEXT NOT NULL DEFAULT 'GB';
ALTER TABLE crews RENAME COLUMN name TO surname;
CREATE INDEX harbours_depth ON harbours (depth) WHERE depth IS NOT NULL;
CREATE VIEW deep_harbours (harbour, metres) AS SELECT name, depth FROM harbours WHERE depth > 10;
CREATE VIEW berth_counts AS SELECT h.name, count(b.number) AS berths FROM harbours AS h LEFT JOIN berths AS b ON b.harbour_id = h.id GROUP BY h.name;
`
)

var replayAllowlist = harness.Allowlist{}

func TestIntrospectionMatchesMigrationReplay(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	entries, err := os.ReadDir("testdata")
	require.NoError(t, err)

	checked := make(map[string]bool)
	used := make(map[string]bool)
	for _, entry := range entries {
		testCaseDirectory := filepath.Join("testdata", entry.Name())
		if _, statError := os.Stat(filepath.Join(testCaseDirectory, "migrations")); statError != nil {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			checked[entry.Name()] = true
			database := applyMigrations(t, testCaseDirectory)
			differences := compareMainSchemas(replayCatalogue(t, os.DirFS(testCaseDirectory)), introspectCatalogue(t, database))
			replayAllowlist.Check(t, entry.Name(), differences, used)
		})
	}

	replayAllowlist.CheckUsed(t, checked, used)
}

func TestIntrospectionMatchesMigrationReplayForSQLiteFeatures(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	directory := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(directory, "migrations"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "migrations", "001_features.up.sql"), []byte(featureMigration), 0o600))
	database := applyMigrations(t, directory)

	used := make(map[string]bool)
	replayAllowlist.Check(t, "features", compareMainSchemas(replayCatalogue(t, os.DirFS(directory)), introspectCatalogue(t, database)), used)
	replayAllowlist.CheckUsed(t, map[string]bool{"features": true}, used)
}

func applyMigrations(t *testing.T, testCaseDirectory string) *sql.DB {
	t.Helper()

	database, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "replay.db"))
	require.NoError(t, err)
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })

	for _, migration := range harness.CaseMigrations(t, testCaseDirectory) {
		_, execError := database.ExecContext(t.Context(), migration.Content)
		require.NoError(t, execError, "applying migration %s", migration.Name)
	}
	return database
}

func replayCatalogue(t *testing.T, migrations fs.FS) *dialect.Catalogue {
	t.Helper()

	return harness.ReplayCatalogue(t, dialect_engine_sqlite.New(), migrations)
}

func introspectCatalogue(t *testing.T, database *sql.DB) *dialect.Catalogue {
	t.Helper()

	ctx, cancel := context.WithTimeoutCause(t.Context(), replayTimeout, errors.New("catalogue introspection did not finish in time"))
	defer cancel()

	catalogue, sourceErrors, err := schema_source_sqlite.New(database, dialect_engine_sqlite.New()).BuildCatalogue(ctx)
	require.NoError(t, err)
	require.Empty(t, sourceErrors)
	return catalogue
}

func compareMainSchemas(replayed, introspected *dialect.Catalogue) []string {
	return harness.CompareCatalogues(
		harness.CatalogueSchema{Catalogue: keyedByFoldedName(replayed), Name: introspectionDefaultSchema},
		harness.CatalogueSchema{Catalogue: keyedByFoldedName(introspected), Name: introspectionDefaultSchema},
	)
}

func mainSchema(catalogue *dialect.Catalogue) *dialect.Schema {
	if catalogue != nil {
		if schema, found := catalogue.Schemas[introspectionDefaultSchema]; found && schema != nil {
			return schema
		}
	}
	return &dialect.Schema{Name: introspectionDefaultSchema}
}

func keyedByFoldedName(catalogue *dialect.Catalogue) *dialect.Catalogue {
	schema := mainSchema(catalogue)
	folded := &dialect.Schema{Name: schema.Name, Tables: emptyLike(schema.Tables), Views: emptyLike(schema.Views)}
	for _, table := range schema.Tables {
		folded.Tables[strings.ToLower(table.Name)] = table
	}
	for _, view := range schema.Views {
		folded.Views[strings.ToLower(view.Name)] = view
	}
	return &dialect.Catalogue{Schemas: map[string]*dialect.Schema{introspectionDefaultSchema: folded}}
}

func emptyLike[Entries ~map[Key]Entry, Key comparable, Entry any](entries Entries) Entries {
	return make(Entries, len(entries))
}
