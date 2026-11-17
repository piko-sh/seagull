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
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/fuzzcase"
	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/schema"
	"seagull.sh/seagull/sdk/schema/schema_source_postgres"
	"seagull.sh/seagull/sdk/schema/schema_source_sqlite"
)

// fuzzSchemaRelation records independently observed relation structure.
type fuzzSchemaRelation struct {
	// indexes maps explicit secondary names to ordered keys.
	indexes map[string]fuzzSchemaIndex

	// columns retains declaration order and observed column metadata.
	columns []fuzzSchemaColumn

	// view distinguishes views from physical tables.
	view bool
}

// fuzzSchemaColumn holds comparable source and engine metadata.
type fuzzSchemaColumn struct {
	// name is the declared column name.
	name string

	// typeName is the engine type name read from live metadata.
	typeName string

	// inferred retains the replayed SQL type for the existing type oracle.
	inferred dialect.SQLType

	// nullable records a declared column constraint.
	nullable bool
}

// fuzzSchemaIndex records explicit secondary key semantics.
type fuzzSchemaIndex struct {
	// columns retains key order.
	columns []string

	// unique records whether the key enforces uniqueness.
	unique bool
}

// runFuzzMigrationSequence checks every prefix of the independent DDL model.
//
// Takes t (*testing.T) which owns the isolated database.
// Takes suite (FuzzSuite) which supplies engine adapters.
// Takes model (fuzzcase.Migration) which supplies bounded operations.
// Takes metadata (*sync.Once) which records the live engine version.
//
// Panics in the watchdog callback if replay ignores cancellation.
func runFuzzMigrationSequence(t *testing.T, suite FuzzSuite, model fuzzcase.Migration, metadata *sync.Once) {
	t.Helper()
	ctx, cancel := context.WithTimeoutCause(t.Context(), scaled(databaseInputDeadline), errFuzzInputDeadline)
	defer cancel()
	watchdog := time.AfterFunc(scaled(databaseWatchdogDeadline), func() { panic("migration fuzz input ignored cancellation: " + t.Name()) })
	defer watchdog.Stop()
	database := suite.Open(t)
	database.SetMaxOpenConns(1)
	metadata.Do(func() { writeFuzzEngineMetadata(ctx, t, database, suite.Engine) })
	initial, ok := fuzzcase.Decode(suite.Engine, []byte{fuzzcase.Version})
	require.True(t, ok, "migration sequence has no initial model")
	statements := initial.Migrations()
	defer func() {
		if t.Failed() {
			t.Logf("migration model: %+v\nattempted prefix:\n%s", model, strings.Join(statements, "\n"))
		}
	}()
	fuzzApply(ctx, t, database, statements)
	checkFuzzMigrationSequence(ctx, t, database, suite, statements)
	for _, statement := range model.Steps {
		statements = append(statements, statement)
		fuzzExec(ctx, t, database, statement, nil)
		checkFuzzMigrationSequence(ctx, t, database, suite, statements)
	}
}

// checkFuzzMigrationSequence compares every model relation at one schema checkpoint.
//
// Takes t (*testing.T) which reports schema differences.
// Takes database (*sql.DB) which holds independently applied migrations.
// Takes suite (FuzzSuite) which supplies engine type observations.
// Takes statements ([]string) which contains the applied prefix.
func checkFuzzMigrationSequence(ctx context.Context, t *testing.T, database *sql.DB, suite FuzzSuite, statements []string) {
	t.Helper()
	replayed := fuzzCatalogueRelations(replayFuzzCatalogue(ctx, t, suite, statements), "")
	observed := observeFuzzMigration(ctx, t, database, suite)
	for _, problem := range fuzzSchemaProblems(replayed, observed) {
		assert.Failf(t, "migration replay differs from the database", "%s\nmigrations: %v", problem, statements)
	}
	for name, relation := range replayed {
		actual, ok := observed[name]
		if !ok || len(relation.columns) != len(actual.columns) {
			continue
		}
		for index := range relation.columns {
			column := &relation.columns[index]
			checkFuzzType(t, suite, column.inferred, actual.columns[index].typeName, name+"."+column.name)
		}
	}
}

// observeFuzzMigration uses public schema sources where the engine supports them.
//
// Takes t (*testing.T) which reports unusable observations.
// Takes database (*sql.DB) which owns the isolated schema.
// Takes suite (FuzzSuite) which supplies engine adapters.
//
// Returns map[string]fuzzSchemaRelation which contains all model relations.
func observeFuzzMigration(ctx context.Context, t *testing.T, database *sql.DB, suite FuzzSuite) map[string]fuzzSchemaRelation {
	t.Helper()
	var source schema.SchemaSourcePort
	scope := ""
	switch {
	case suite.Engine == engineSQLite:
		source = schema_source_sqlite.New(database, suite.Dialect())
	case isPostgresFamily(suite.Engine):
		source = schema_source_postgres.New(database, suite.Dialect())
		scope = fuzzValue(fuzzScalar(ctx, t, database, "SELECT current_schema()"))
	default:
		return observeFuzzMigrationMetadata(ctx, t, database, suite)
	}
	catalogue, diagnostics, err := source.BuildCatalogue(ctx)
	require.NoError(t, err)
	require.Empty(t, diagnostics, "live schema source reported diagnostics: %+v", diagnostics)
	return fuzzCatalogueRelations(catalogue, scope)
}

// fuzzCatalogueRelations projects model objects without migration origins or server
// names.
//
// Takes catalogue (*dialect.Catalogue) which is replayed or introspected.
// Takes scope (string) which selects an isolated live schema or every replayed schema.
//
// Returns map[string]fuzzSchemaRelation which retains supported observable fields.
func fuzzCatalogueRelations(catalogue *dialect.Catalogue, scope string) map[string]fuzzSchemaRelation {
	result := make(map[string]fuzzSchemaRelation)
	for schemaName, namespace := range catalogue.Schemas {
		if scope != "" && schemaName != scope {
			continue
		}
		for name, table := range namespace.Tables {
			if strings.HasPrefix(name, fuzzModelPrefix) {
				result[name] = fuzzTableRelation(table)
			}
		}
		for name, view := range namespace.Views {
			if strings.HasPrefix(name, fuzzModelPrefix) {
				result[name] = fuzzRelationColumns(view.Columns, true)
			}
		}
	}
	return result
}

// fuzzTableRelation records visible columns and explicit secondary indexes.
//
// Takes table (*dialect.Table) which comes from replay or a public schema source.
//
// Returns fuzzSchemaRelation which retains supported table metadata.
func fuzzTableRelation(table *dialect.Table) fuzzSchemaRelation {
	relation := fuzzRelationColumns(table.Columns, false)
	for _, index := range table.Indexes {
		if !index.IsPrimary {
			relation.indexes[index.Name] = fuzzSchemaIndex{columns: slices.Clone(index.Columns), unique: index.IsUnique}
		}
	}
	return relation
}

// fuzzRelationColumns removes only engine-generated hidden columns.
//
// Takes columns ([]dialect.Column) which supplies ordered column metadata.
// Takes view (bool) which identifies a view definition.
//
// Returns fuzzSchemaRelation which contains the model's visible structure.
func fuzzRelationColumns(columns []dialect.Column, view bool) fuzzSchemaRelation {
	relation := fuzzSchemaRelation{columns: nil, indexes: make(map[string]fuzzSchemaIndex), view: view}
	for index := range columns {
		column := &columns[index]
		if !column.Hidden {
			relation.columns = append(relation.columns, fuzzSchemaColumn{
				name: column.Name, typeName: column.SQLType.EngineName, inferred: column.SQLType, nullable: column.Nullable,
			})
		}
	}
	return relation
}

// fuzzSchemaProblems compares relation membership, column order, constraints and indexes.
//
// View metadata does not expose inferred NOT NULL guarantees across these engines.
//
// Takes replayed (map[string]fuzzSchemaRelation) which comes from migration replay.
// Takes observed (map[string]fuzzSchemaRelation) which comes from the live database.
//
// Returns []string which describes omitted, stale or incorrectly replayed operations.
func fuzzSchemaProblems(replayed, observed map[string]fuzzSchemaRelation) []string {
	var problems []string
	if len(replayed) != len(observed) {
		problems = append(problems, fmt.Sprintf("replay has %d relations, database has %d", len(replayed), len(observed)))
	}
	for name, expected := range replayed {
		actual, ok := observed[name]
		if !ok {
			problems = append(problems, "database has no replayed relation "+name)
			continue
		}
		if expected.view != actual.view {
			problems = append(problems, "relation kind differs for "+name)
		}
		if !reflect.DeepEqual(expected.indexes, actual.indexes) {
			problems = append(problems, fmt.Sprintf("%s index keys differ: replay %+v, database %+v", name, expected.indexes, actual.indexes))
		}
		problems = append(problems, fuzzSchemaColumnProblems(name, expected, actual)...)
	}
	return problems
}

// fuzzSchemaColumnProblems compares ordered columns and declared table nullability.
//
// Takes name (string) which identifies the relation.
// Takes expected (fuzzSchemaRelation) which comes from replay.
// Takes actual (fuzzSchemaRelation) which comes from live metadata.
//
// Returns []string which describes lost, reordered or weakened columns.
func fuzzSchemaColumnProblems(name string, expected, actual fuzzSchemaRelation) []string {
	if len(expected.columns) != len(actual.columns) {
		return []string{fmt.Sprintf("%s column count differs: replay %d, database %d", name, len(expected.columns), len(actual.columns))}
	}
	var problems []string
	for index := range expected.columns {
		column, observed := &expected.columns[index], &actual.columns[index]
		if column.name != observed.name {
			problems = append(problems, fmt.Sprintf("%s column %d differs: replay %s, database %s", name, index, column.name, observed.name))
		}
		if !expected.view && column.nullable != observed.nullable {
			problems = append(problems, "column nullability differs for "+name+"."+column.name)
		}
	}
	return problems
}
