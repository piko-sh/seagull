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

package dialecttest

import (
	"context"
	"io/fs"
	"path"
	"strings"
	"testing/fstest"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// migrationDirectory is the virtual directory the conformance suite serves migration
	// files from.
	migrationDirectory = "migrations"

	// queryDirectory is the virtual directory the conformance suite serves query files from.
	queryDirectory = "queries"

	// packageName is the package name the suites generate into.
	packageName = "conformance"
)

// recordingEmitter captures the catalogue and analysed queries a generation produces, so
// invariants can be asserted against them. It emits no files.
type recordingEmitter struct {
	// catalogue is the catalogue captured from the most recent Emit call.
	catalogue *sqlmodel.Catalogue

	// queries are the analysed queries captured from the most recent Emit call.
	queries []*sqlmodel.AnalysedQuery
}

// Emit records the catalogue and analysed queries and emits nothing.
//
// Takes request (*emit.Request) which carries the catalogue and analysed queries.
//
// Returns []sqlmodel.GeneratedFile which is always nil.
// Returns error which is always nil.
func (emitter *recordingEmitter) Emit(_ context.Context, request *emit.Request) ([]sqlmodel.GeneratedFile, error) {
	emitter.catalogue = request.Catalogue
	emitter.queries = request.Queries
	return nil, nil
}

// virtualFiles returns an in-memory file system holding the given migration and query
// files under the migration and query directories, each of which exists even when it
// holds no files.
//
// Takes migrations (map[string]string) which maps migration file names to SQL.
// Takes queries (map[string]string) which maps query file names to SQL.
//
// Returns fstest.MapFS which serves the files.
func virtualFiles(migrations, queries map[string]string) fstest.MapFS {
	files := fstest.MapFS{
		migrationDirectory: &fstest.MapFile{Mode: fs.ModeDir},
		queryDirectory:     &fstest.MapFile{Mode: fs.ModeDir},
	}
	for name, content := range migrations {
		files[path.Join(migrationDirectory, name)] = &fstest.MapFile{Data: []byte(content)}
	}
	for name, content := range queries {
		files[path.Join(queryDirectory, name)] = &fstest.MapFile{Data: []byte(content)}
	}
	return files
}

// runGenerate generates for one dialect over in-memory migration and query files,
// recording the catalogue and analysed queries.
//
// Takes engine (dialect.DialectPort) which is the dialect under test.
// Takes migrations (map[string]string) which maps migration file names to SQL.
// Takes queries (map[string]string) which maps query file names to SQL.
// Takes options (...seagull.Option) which configure the generator further, such as with
// type overrides.
//
// Returns *recordingEmitter which captured the catalogue and analysed queries.
// Returns *app.Result which holds the run's diagnostics.
// Returns error when the generator cannot be built or generation fails fatally.
func runGenerate(
	ctx context.Context,
	engine dialect.DialectPort,
	migrations, queries map[string]string,
	options ...app.Option,
) (*recordingEmitter, *app.Result, error) {
	emitter := &recordingEmitter{}
	generator, err := app.New(engine, append([]app.Option{app.WithEmitter(emitter)}, options...)...)
	if err != nil {
		return nil, nil, err
	}
	result, err := generator.Generate(ctx, app.Input{
		Package:      packageName,
		PackagePath:  "",
		Migrations:   virtualFiles(migrations, queries),
		Queries:      nil,
		MigrationDir: migrationDirectory,
		QueryDir:     queryDirectory,
	})
	return emitter, result, err
}

// catalogueTableNames returns the lower-cased set of table names across every schema in
// the catalogue, so quoting and splitting checks are dialect-agnostic.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the schema state.
//
// Returns map[string]struct{} which is the set of lower-cased table names.
func catalogueTableNames(catalogue *sqlmodel.Catalogue) map[string]struct{} {
	names := make(map[string]struct{})
	if catalogue == nil {
		return names
	}
	for _, schema := range catalogue.Schemas {
		if schema == nil {
			continue
		}
		for tableName := range schema.Tables {
			names[strings.ToLower(tableName)] = struct{}{}
		}
	}
	return names
}

// findQuery returns the analysed query with the given name, or nil when absent.
//
// Takes queries ([]*sqlmodel.AnalysedQuery) which holds the analysed queries.
// Takes name (string) which is the query name to find.
//
// Returns *sqlmodel.AnalysedQuery which is the matching query, or nil.
func findQuery(queries []*sqlmodel.AnalysedQuery, name string) *sqlmodel.AnalysedQuery {
	for _, query := range queries {
		if query != nil && query.Name == name {
			return query
		}
	}
	return nil
}
