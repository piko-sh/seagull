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

package harness

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/internal/testkit/fuzzschema"
	"seagull.sh/seagull/sdk/migrate"
)

// historyNamespacePattern finds the model's second schema in migration files.
var historyNamespacePattern = regexp.MustCompile(`\b` + fuzzschema.Namespace + `\b`)

// FuzzSchemaHistory compares modeled, live, and replayed schemas after migrations.
//
// A model and database agreement that differs from Seagull is a Seagull finding. A model
// and database disagreement indicates a model or harness fault.
//
// Takes f (*testing.F) which receives the seeds.
// Takes suite (NativeFuzzSuite) which supplies the isolated connections and dialect.
func FuzzSchemaHistory(f *testing.F, suite NativeFuzzSuite) {
	f.Helper()
	if suite.OpenNative == nil || suite.Dialect == nil {
		require.Fail(f, "incomplete schema history suite")
	}
	for _, seed := range fuzzschema.Seeds(suite.Engine) {
		f.Add(seed)
	}
	var metadata sync.Once
	f.Fuzz(func(t *testing.T, data []byte) {
		model, ok := fuzzschema.Decode(suite.Engine, data)
		if !ok {
			t.Skip("unsupported schema history envelope")
		}
		runSchemaHistory(t, suite, &model, &metadata)
	})
}

// runSchemaHistory applies one history and compares the three observations.
//
// Takes t (*testing.T) which reports findings.
// Takes suite (NativeFuzzSuite) which supplies the database and dialect.
// Takes model (*fuzzschema.Case) which is the history and its expected schema.
// Takes metadata (*sync.Once) which records the engine version once.
//
// Panics in the watchdog callback if the history ignores cancellation.
func runSchemaHistory(t *testing.T, suite NativeFuzzSuite, model *fuzzschema.Case, metadata *sync.Once) {
	t.Helper()
	ctx, cancel := context.WithTimeoutCause(t.Context(), scaled(executionInputDeadline), errFuzzInputDeadline)
	defer cancel()
	watchdog := time.AfterFunc(scaled(executionWatchdogDeadline), func() { panic("schema history ignored cancellation: " + t.Name()) })
	defer watchdog.Stop()
	database, _ := suite.OpenNative(t)
	metadata.Do(func() { writeFuzzEngineMetadata(ctx, t, database, suite.Engine) })
	namespace := historyNamespace(t, suite.Engine)
	dropHistoryNamespace(t, database, namespace)
	files := historyFiles(model, namespace)
	migrator, err := migrate.NewMigrator(database, executionBackend(suite.Engine), files, "migrations")
	require.NoError(t, err)
	count, err := migrator.Up(ctx)
	require.NoError(t, err, "[model] the engine or the migration runner refused the history\n%s", model.Describe())
	require.Equal(t, len(model.Migrations), count, "[model] the migration runner skipped migrations\n%s", model.Describe())
	live := observeHistory(ctx, t, database, suite.Engine, namespace)
	replayed, rejected := replayHistory(ctx, t, suite, files, namespace)
	checkNames := suite.Engine != engineDuckDB
	problems := compareHistory(historyProperties(model.Final, checkNames), historyProperties(live, checkNames),
		historyProperties(replayed, checkNames))
	if len(rejected) != 0 || len(problems) != 0 {
		assert.Failf(t, "schema history mismatch", "%v%v\nhistory (%v)\n%s", rejected, problems, model.Features, model.Describe())
	}
}

// historyNamespace chooses the name of the history's second schema. Suites that isolate
// inputs by schema share one database, so there the name is unique and the schema is
// dropped when the input ends.
//
// Takes t (*testing.T) which owns the clean-up.
// Takes engine (string) which names the dialect.
//
// Returns string which is the schema name the files use.
func historyNamespace(t *testing.T, engine string) string {
	t.Helper()
	if isPostgresFamily(engine) {
		return UniqueName(t) + "_ns"
	}
	return fuzzschema.Namespace
}

// dropHistoryNamespace removes a uniquely named second schema when the input ends.
//
// Takes t (*testing.T) which owns the clean-up.
// Takes database (*sql.DB) which holds the schema.
// Takes namespace (string) which is the schema's name.
func dropHistoryNamespace(t *testing.T, database *sql.DB, namespace string) {
	t.Helper()
	if namespace == fuzzschema.Namespace {
		return
	}
	t.Cleanup(func() {
		ctx, cancel := CleanupContext(t)
		defer cancel()
		_, err := database.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+namespace+" CASCADE")
		assert.NoError(t, err, "dropping %s", namespace)
	})
}

// historyFiles writes the history's migration files with the chosen schema name.
//
// Takes model (*fuzzschema.Case) which is the history.
// Takes namespace (string) which replaces the model's schema name.
//
// Returns fstest.MapFS which holds the files.
func historyFiles(model *fuzzschema.Case, namespace string) fstest.MapFS {
	files := fstest.MapFS{}
	for name, content := range model.Files() {
		files[name] = &fstest.MapFile{Data: historyNamespacePattern.ReplaceAll(content, []byte(namespace))}
	}
	return files
}

// replayHistory builds Seagull's catalogue from the same files.
//
// Takes t (*testing.T) which reports a fatal replay error.
// Takes suite (NativeFuzzSuite) which supplies the dialect.
// Takes files (fstest.MapFS) which holds the migrations.
// Takes namespace (string) which is the second schema's name.
//
// Returns fuzzschema.Schema which is Seagull's view of the final schema.
// Returns []string which describes error diagnostics on files the engine accepted.
func replayHistory(ctx context.Context, t *testing.T, suite NativeFuzzSuite, files fstest.MapFS, namespace string) (fuzzschema.Schema, []string) {
	t.Helper()
	generator, err := seagull.New(suite.Dialect())
	require.NoError(t, err)
	catalogue, diagnostics, err := generator.Catalogue(ctx, seagull.Input{Migrations: files, MigrationDir: "migrations", Queries: nil,
		Package: "", PackagePath: "", QueryDir: ""})
	require.NoError(t, err, "[seagull] replaying migrations the engine accepted failed")
	var rejected []string
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == seagull.SeverityError {
			rejected = append(rejected, fmt.Sprintf("\n[seagull] replay rejected a migration the engine accepted: %s:%d %s %s",
				diagnostic.Filename, diagnostic.Line, diagnostic.Code, diagnostic.Message))
		}
	}
	return seagullHistorySchema(catalogue, namespace), rejected
}
