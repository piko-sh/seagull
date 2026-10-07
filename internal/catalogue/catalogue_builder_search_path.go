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
	"slices"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// searchPaths tracks the search path the migrations replay with, which SET search_path
// changes as the database's session setting does.
//
// The migrations run on one session, so a SET lasts for the rest of the migrations and a
// SET LOCAL until the end of the migration that makes it. Queries run on other sessions,
// whose search path is the one the migrations started with, so the catalogue keeps that
// one once each migration is replayed.
type searchPaths struct {
	// start is the search path the migrations start with: the dialect's.
	start []string

	// session is the search path the migrations have set, or the start one.
	session []string

	// local is the search path a SET LOCAL gave the current migration, when localSet.
	local []string

	// localSet is true while a SET LOCAL holds for the current migration.
	localSet bool
}

// newSearchPaths returns the search paths of migrations that start with a search path.
//
// Takes start ([]string) which is the dialect's search path.
//
// Returns searchPaths which holds the start path as the session's.
func newSearchPaths(start []string) searchPaths {
	return searchPaths{start: slices.Clone(start), session: slices.Clone(start), local: nil, localSet: false}
}

// current returns the search path a statement of the current migration replays with.
//
// Returns []string which is the SET LOCAL path while one holds, or the session's.
func (paths *searchPaths) current() []string {
	if paths.localSet {
		return paths.local
	}
	return paths.session
}

// beginMigration gives the catalogue the search path the migrations' session holds as a
// migration starts.
func (b *catalogueBuilder) beginMigration() {
	b.useSearchPath(b.searchPaths.current())
}

// endMigration ends what a SET LOCAL set, as the migration's transaction ends, and gives
// the catalogue back the search path the migrations started with, for the queries.
func (b *catalogueBuilder) endMigration() {
	b.searchPaths.local, b.searchPaths.localSet = nil, false
	b.useSearchPath(b.searchPaths.start)
}

// applySetSearchPath sets the search path the following statements replay with, as SET
// search_path does.
//
// Takes mutation (*ast.CatalogueMutation) which lists the schemas and says whether the
// path lasts for the migration alone.
//
// Returns error which is always nil.
func (b *catalogueBuilder) applySetSearchPath(mutation *ast.CatalogueMutation) error {
	b.setSearchPath(mutation.SearchPath, mutation.Local)
	return nil
}

// applyResetSearchPath restores the search path the migrations started with, as SET
// search_path TO DEFAULT and RESET search_path do.
//
// Takes mutation (*ast.CatalogueMutation) which says whether the restored path lasts for
// the migration alone.
//
// Returns error which is always nil.
func (b *catalogueBuilder) applyResetSearchPath(mutation *ast.CatalogueMutation) error {
	b.setSearchPath(b.searchPaths.start, mutation.Local)
	return nil
}

// setSearchPath records a search path for the migration alone or for the session, and
// gives it to the catalogue. A path set for the session ends any SET LOCAL.
//
// Takes path ([]string) which is the search path.
// Takes local (bool) which is true for a path that lasts for the migration alone.
func (b *catalogueBuilder) setSearchPath(path []string, local bool) {
	if local {
		b.searchPaths.local, b.searchPaths.localSet = slices.Clone(path), true
	} else {
		b.searchPaths.session = slices.Clone(path)
		b.searchPaths.local, b.searchPaths.localSet = nil, false
	}
	b.useSearchPath(b.searchPaths.current())
}

// useSearchPath gives the catalogue a search path, its first schema the default one.
//
// Takes path ([]string) which is the search path.
func (b *catalogueBuilder) useSearchPath(path []string) {
	b.catalogue.SearchPath = slices.Clone(path)
	b.catalogue.DefaultSchema = ""
	if len(path) > 0 {
		b.catalogue.DefaultSchema = path[0]
	}
}

// creationSchema returns the name of the schema an unqualified object is created in, as
// the database chooses it: the first schema on the search path that exists, or the first
// on the path when none exists yet, since a schema made outside the migrations may hold
// it.
//
// Returns string which is the schema's canonical name.
func (b *catalogueBuilder) creationSchema() string {
	for _, name := range b.catalogue.SearchPath {
		if b.catalogue.Schemas[b.key(name)] != nil {
			return name
		}
	}
	return b.catalogue.DefaultSchema
}

// schemaHolding returns the schema an existing object a statement names is in: the named
// schema, or for an unqualified name the first schema on the search path that holds it,
// as the database looks it up. It never creates a schema.
//
// Takes schemaName (string) which is the name's schema qualifier, or empty.
// Takes holds (func(*sqlmodel.Schema) bool) which reports whether a schema holds the
// object.
//
// Returns *sqlmodel.Schema which is the schema holding the object, or nil when none does.
func (b *catalogueBuilder) schemaHolding(schemaName string, holds func(*sqlmodel.Schema) bool) *sqlmodel.Schema {
	for _, schemaKey := range b.catalogue.SchemaNamesFor(schemaName) {
		if schema := b.catalogue.Schemas[schemaKey]; schema != nil && holds(schema) {
			return schema
		}
	}
	return nil
}
