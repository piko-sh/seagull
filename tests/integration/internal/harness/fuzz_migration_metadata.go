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
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/sdk/dialect"
)

// observeFuzzMigrationMetadata reads engines without a public schema source directly.
//
// Takes t (*testing.T) which reports malformed observations.
// Takes database (*sql.DB) which contains the isolated model schema.
// Takes suite (FuzzSuite) which supplies the engine name.
//
// Returns map[string]fuzzSchemaRelation which retains live relation structure.
func observeFuzzMigrationMetadata(ctx context.Context, t *testing.T, database *sql.DB, suite FuzzSuite) map[string]fuzzSchemaRelation {
	t.Helper()
	relations := make(map[string]fuzzSchemaRelation)
	for _, row := range fuzzRows(ctx, t, database, fuzzRelationsQuery(suite.Engine), nil) {
		name := fuzzValue(row[0])
		if !strings.HasPrefix(name, fuzzModelPrefix) {
			continue
		}
		relation := fuzzSchemaRelation{columns: nil, indexes: make(map[string]fuzzSchemaIndex), view: fuzzMetadataBool(t, row[1])}
		query := strings.ReplaceAll(fuzzSchemaQuery(suite.Engine), "fuzz_items", name)
		for _, column := range fuzzRows(ctx, t, database, query, nil) {
			relation.columns = append(relation.columns, fuzzSchemaColumn{
				inferred: dialect.SQLType{}, name: fuzzValue(column[0]), typeName: fuzzValue(column[1]), nullable: strings.EqualFold(fuzzValue(column[2]), "YES"),
			})
		}
		if !relation.view {
			relation.indexes = observeFuzzIndexes(ctx, t, database, suite.Engine, name)
		}
		relations[name] = relation
	}
	return relations
}

// fuzzRelationsQuery lists physical tables and views in the current namespace.
//
// Takes engine (string) which selects the metadata catalogue.
//
// Returns string which retains relation kind independently of replay.
func fuzzRelationsQuery(engine string) string {
	switch engine {
	case engineClickHouse:
		return "SELECT name, engine = 'View' FROM system.tables WHERE database = currentDatabase() ORDER BY name"
	case engineMySQL, engineMariaDB:
		return "SELECT table_name, table_type = 'VIEW' FROM information_schema.tables WHERE table_schema = database() ORDER BY table_name"
	default:
		return "SELECT table_name, table_type = 'VIEW' FROM information_schema.tables WHERE table_schema = current_schema() ORDER BY table_name"
	}
}

// fuzzMetadataBool requires a recognised database Boolean representation.
//
// Takes t (*testing.T) which reports metadata outside the driver contract.
// Takes value (any) which contains a Boolean or zero/one representation.
//
// Returns bool which retains the observed truth value.
func fuzzMetadataBool(t *testing.T, value any) bool {
	t.Helper()
	result, err := strconv.ParseBool(fuzzValue(value))
	require.NoError(t, err, "invalid metadata Boolean %T(%v)", value, value)
	return result
}

// observeFuzzIndexes reads explicit index names, uniqueness and ordered key columns.
//
// Takes t (*testing.T) which reports incomplete index observations.
// Takes database (*sql.DB) which owns the isolated schema.
// Takes engine (string) which selects metadata syntax.
// Takes table (string) which is a model-generated table name.
//
// Returns map[string]fuzzSchemaIndex which excludes engine-generated primary keys.
func observeFuzzIndexes(ctx context.Context, t *testing.T, database *sql.DB, engine, table string) map[string]fuzzSchemaIndex {
	t.Helper()
	indexes := make(map[string]fuzzSchemaIndex)
	if engine == engineClickHouse {
		return indexes
	}
	if engine == engineDuckDB {
		query := "SELECT index_name, is_unique, sql FROM duckdb_indexes() WHERE schema_name = current_schema() AND table_name = ? ORDER BY index_name"
		for _, row := range fuzzRows(ctx, t, database, query, []any{table}) {
			columns, err := fuzzDuckIndexColumns(fuzzValue(row[2]))
			require.NoError(t, err)
			indexes[fuzzValue(row[0])] = fuzzSchemaIndex{columns: columns, unique: fuzzMetadataBool(t, row[1])}
		}
		return indexes
	}
	query := "SELECT index_name, non_unique, column_name FROM information_schema.statistics WHERE table_schema = database() AND table_name = ? " +
		"AND index_name <> 'PRIMARY' ORDER BY index_name, seq_in_index"
	for _, row := range fuzzRows(ctx, t, database, query, []any{table}) {
		name := fuzzValue(row[0])
		index := indexes[name]
		index.unique = !fuzzMetadataBool(t, row[1])
		index.columns = append(index.columns, fuzzValue(row[2]))
		indexes[name] = index
	}
	return indexes
}

// fuzzDuckIndexColumns reads the model's simple keys from DuckDB's stored definition.
//
// DuckDB exposes index definitions through sql even when expressions is NULL.
//
// Takes definition (string) which is metadata for a model-created secondary index.
//
// Returns []string which preserves key order without using Seagull's SQL parser.
// Returns error when the definition leaves the bounded simple-column grammar.
func fuzzDuckIndexColumns(definition string) ([]string, error) {
	_, keys, found := strings.Cut(definition, "(")
	if !found {
		return nil, fmt.Errorf("index metadata has no key list: %q", definition)
	}
	keys = strings.TrimSuffix(strings.TrimSpace(keys), ";")
	if !strings.HasSuffix(keys, ")") {
		return nil, fmt.Errorf("index metadata has no closing key list: %q", definition)
	}
	columns := strings.Split(strings.TrimSuffix(keys, ")"), ",")
	for index, column := range columns {
		column = strings.Trim(strings.TrimSpace(column), "\"")
		if column != "category" && column != "id" && column != "renamed" {
			return nil, fmt.Errorf("index metadata has unexpected key %q", column)
		}
		columns[index] = column
	}
	return columns, nil
}
