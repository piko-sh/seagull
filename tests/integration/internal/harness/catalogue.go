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
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/sdk/dialect"
)

const (
	// fromDatabase names the database side of a difference.
	fromDatabase = "the database"

	// fromMigrations names the migration side of a difference.
	fromMigrations = "the migrations"

	// replayTimeout bounds replaying a case's migrations into a catalogue, so an analysis
	// that never finishes fails the test by name.
	replayTimeout = 2 * time.Minute
)

// CatalogueSchema is one schema of a catalogue, to compare with a schema of another.
type CatalogueSchema struct {
	// Catalogue is the catalogue the schema belongs to.
	Catalogue *dialect.Catalogue

	// Name is the schema's name in that catalogue.
	Name string
}

// contents returns the schema, or an empty one when there is no catalogue or the
// catalogue has no schema of its name.
//
// Returns *dialect.Schema which is the schema.
func (schema CatalogueSchema) contents() *dialect.Schema {
	if schema.Catalogue != nil {
		if found, exists := schema.Catalogue.Schemas[schema.Name]; exists && found != nil {
			return found
		}
	}
	empty := &dialect.Schema{}
	empty.Name = schema.Name
	return empty
}

// Allowlist holds the differences between an introspected catalogue and a migration
// replay that a suite accepts, each keyed "case: difference" and mapped to the reason.
type Allowlist map[string]string

// Check fails the test for each difference the allowlist does not hold, and marks each
// one it holds as used.
//
// Takes caseName (string) which names the case the differences belong to.
// Takes differences ([]string) which are the case's differences.
// Takes used (map[string]bool) which collects the allowlist entries that matched.
func (allowlist Allowlist) Check(t *testing.T, caseName string, differences []string, used map[string]bool) {
	t.Helper()

	for _, problem := range allowlist.unexpected(caseName, differences, used) {
		assert.Fail(t, problem)
	}
}

// CheckUsed fails the test for each allowlist entry of a checked case that no longer
// occurs, so the allowlist only shrinks as differences are fixed.
//
// Takes checked (map[string]bool) which holds the cases that ran.
// Takes used (map[string]bool) which holds the entries that matched.
func (allowlist Allowlist) CheckUsed(t *testing.T, checked, used map[string]bool) {
	t.Helper()

	for _, problem := range allowlist.stale(checked, used) {
		assert.Fail(t, problem)
	}
}

// unexpected describes each difference the allowlist does not hold, and marks each one it
// holds as used.
//
// Takes caseName (string) which names the case the differences belong to.
// Takes differences ([]string) which are the case's differences.
// Takes used (map[string]bool) which collects the allowlist entries that matched.
//
// Returns []string which holds one description per difference not allowed.
func (allowlist Allowlist) unexpected(caseName string, differences []string, used map[string]bool) []string {
	var problems []string
	for _, difference := range differences {
		key := caseName + ": " + difference
		if _, allowed := allowlist[key]; allowed {
			used[key] = true
			continue
		}
		problems = append(problems, "introspection differs from migration replay: "+key)
	}
	return problems
}

// stale describes each allowlist entry of a checked case that no longer occurs, in key
// order.
//
// Takes checked (map[string]bool) which holds the cases that ran.
// Takes used (map[string]bool) which holds the entries that matched.
//
// Returns []string which holds one description per stale entry.
func (allowlist Allowlist) stale(checked, used map[string]bool) []string {
	var problems []string
	for _, key := range slices.Sorted(maps.Keys(allowlist)) {
		caseName, _, _ := strings.Cut(key, ": ")
		if checked[caseName] && !used[key] {
			problems = append(problems, "allowlisted difference no longer occurs, remove it: "+key)
		}
	}
	return problems
}

// CompareCatalogues lists the differences between a schema replayed from migrations and
// the schema introspected from a database that ran them: tables, views and enums one side
// lacks, and the columns, primary keys, partitions' parents, view columns and enum values
// that differ. A column's type is compared with its schema left out when it is the schema
// compared, since the two sides name the same schema differently.
//
// Takes migrations (CatalogueSchema) which is the replayed schema.
// Takes database (CatalogueSchema) which is the introspected schema.
//
// Returns []string which holds the differences, sorted.
func CompareCatalogues(migrations, database CatalogueSchema) []string {
	expected := migrations.contents()
	actual := database.contents()
	differences := compareTables(expected, actual, migrations.Name, database.Name)
	differences = append(differences, compareViews(expected, actual)...)
	differences = append(differences, compareEnums(expected, actual)...)
	slices.Sort(differences)
	return differences
}

// ReplayCatalogue replays the forward migrations under migrations/ through the generator
// with a dialect, as generation would, and returns the catalogue they build. The replay
// runs under the test's context, ended with a cause naming the time limit once
// replayTimeout has passed.
//
// Takes engine (dialect.Dialect) which replays the migrations.
// Takes migrations (fs.FS) which holds the migrations directory.
//
// Returns *dialect.Catalogue which is the replayed catalogue.
func ReplayCatalogue(t *testing.T, engine dialect.Dialect, migrations fs.FS) *dialect.Catalogue {
	t.Helper()

	ctx, cancel := context.WithTimeoutCause(t.Context(), replayTimeout, errReplayTimeout)
	defer cancel()

	generator, err := seagull.New(engine)
	require.NoError(t, err)
	catalogue, _, err := generator.Catalogue(ctx, seagull.Input{
		Migrations:   migrations,
		Queries:      nil,
		Package:      "",
		PackagePath:  "",
		MigrationDir: migrationDirectory,
		QueryDir:     "",
	})
	require.NoError(t, err, "replaying the migrations: %v", context.Cause(ctx))
	require.NotNil(t, catalogue)
	return catalogue
}

// ColumnsByName indexes columns by name.
//
// Takes columns ([]dialect.Column) which are indexed.
//
// Returns map[string]dialect.Column which maps each name to its column.
func ColumnsByName(columns []dialect.Column) map[string]dialect.Column {
	byName := make(map[string]dialect.Column, len(columns))
	for index := range columns {
		byName[columns[index].Name] = columns[index]
	}
	return byName
}

// compareTables lists the differences between two schemas' tables.
//
// Takes expected (*dialect.Schema) which is the replayed schema.
// Takes actual (*dialect.Schema) which is the introspected schema.
// Takes expectedName (string) which is the replayed schema's name.
// Takes actualName (string) which is the introspected schema's name.
//
// Returns []string which holds the differences.
func compareTables(expected, actual *dialect.Schema, expectedName, actualName string) []string {
	var differences []string
	for _, name := range unionOfKeys(expected.Tables, actual.Tables) {
		expectedTable, inMigrations := expected.Tables[name]
		actualTable, inDatabase := actual.Tables[name]
		switch {
		case !inDatabase:
			differences = append(differences, missing("table", name, fromDatabase))
		case !inMigrations:
			differences = append(differences, missing("table", name, fromMigrations))
		default:
			differences = append(differences, compareRelation("table "+name, expectedTable.Columns, actualTable.Columns, expectedName, actualName)...)
			if !slices.Equal(expectedTable.PrimaryKey, actualTable.PrimaryKey) {
				differences = append(differences, fmt.Sprintf("table %s: primary key differs: migrations %v, database %v",
					name, expectedTable.PrimaryKey, actualTable.PrimaryKey))
			}
			if expected, actual := describePartition(expectedTable, expectedName), describePartition(actualTable, actualName); expected != actual {
				differences = append(differences, fmt.Sprintf("table %s: partition differs: migrations %s, database %s", name, expected, actual))
			}
		}
	}
	return differences
}

// compareViews lists the differences between two schemas' views.
//
// Takes expected (*dialect.Schema) which is the replayed schema.
// Takes actual (*dialect.Schema) which is the introspected schema.
//
// Returns []string which holds the differences.
func compareViews(expected, actual *dialect.Schema) []string {
	var differences []string
	for _, name := range unionOfKeys(expected.Views, actual.Views) {
		expectedView, inMigrations := expected.Views[name]
		actualView, inDatabase := actual.Views[name]
		switch {
		case !inDatabase:
			differences = append(differences, missing("view", name, fromDatabase))
		case !inMigrations:
			differences = append(differences, missing("view", name, fromMigrations))
		case !slices.Equal(columnNames(expectedView.Columns), columnNames(actualView.Columns)):
			differences = append(differences, fmt.Sprintf("view %s: columns differ: migrations %v, database %v",
				name, columnNames(expectedView.Columns), columnNames(actualView.Columns)))
		}
	}
	return differences
}

// compareEnums lists the differences between two schemas' enums.
//
// Takes expected (*dialect.Schema) which is the replayed schema.
// Takes actual (*dialect.Schema) which is the introspected schema.
//
// Returns []string which holds the differences.
func compareEnums(expected, actual *dialect.Schema) []string {
	var differences []string
	for _, name := range unionOfKeys(expected.Enums, actual.Enums) {
		expectedEnum, inMigrations := expected.Enums[name]
		actualEnum, inDatabase := actual.Enums[name]
		switch {
		case !inDatabase:
			differences = append(differences, missing("enum", name, fromDatabase))
		case !inMigrations:
			differences = append(differences, missing("enum", name, fromMigrations))
		case !slices.Equal(expectedEnum.Values, actualEnum.Values):
			differences = append(differences, fmt.Sprintf("enum %s: values differ: migrations %v, database %v",
				name, expectedEnum.Values, actualEnum.Values))
		}
	}
	return differences
}

// compareRelation lists the differences between two versions of a table's or view's
// columns.
//
// Takes label (string) which names the relation, such as "table readings".
// Takes expected ([]dialect.Column) which are the replayed columns.
// Takes actual ([]dialect.Column) which are the introspected columns.
// Takes expectedSchema (string) which is the replayed schema's name.
// Takes actualSchema (string) which is the introspected schema's name.
//
// Returns []string which holds the differences.
func compareRelation(label string, expected, actual []dialect.Column, expectedSchema, actualSchema string) []string {
	var differences []string
	actualColumns := ColumnsByName(actual)
	expectedColumns := ColumnsByName(expected)
	for position := range expected {
		column := &expected[position]
		other, found := actualColumns[column.Name]
		if !found {
			differences = append(differences, fmt.Sprintf("%s: column %s is missing from the database", label, column.Name))
			continue
		}
		expectedAttributes := describeColumn(column, expectedSchema)
		actualAttributes := describeColumn(&other, actualSchema)
		for index, attribute := range expectedAttributes {
			if attribute[1] != actualAttributes[index][1] {
				differences = append(differences, fmt.Sprintf("%s: column %s: %s differs: migrations %s, database %s",
					label, column.Name, attribute[0], attribute[1], actualAttributes[index][1]))
			}
		}
	}
	for position := range actual {
		if _, found := expectedColumns[actual[position].Name]; !found {
			differences = append(differences, fmt.Sprintf("%s: column %s is missing from the migrations", label, actual[position].Name))
		}
	}
	return differences
}

// describeColumn lists the attributes of a column the comparison checks, as name and
// value pairs.
//
// Takes column (*dialect.Column) which is described.
// Takes schemaName (string) which is the schema the column's table is in.
//
// Returns [][2]string which holds the attributes.
func describeColumn(column *dialect.Column, schemaName string) [][2]string {
	generated := "no"
	if column.IsGenerated {
		generated = column.GeneratedKind.String()
	}
	return [][2]string{
		{"type", describeType(&column.SQLType, schemaName)},
		{"array dimensions", strconv.Itoa(column.ArrayDimensions)},
		{"nullable", strconv.FormatBool(column.Nullable)},
		{"has default", strconv.FormatBool(column.HasDefault)},
		{"generated", generated},
		{"hidden", strconv.FormatBool(column.Hidden)},
		{"enum values", fmt.Sprint(column.SQLType.EnumValues)},
	}
}

// describePartition describes the table a table partitions: its name, and its schema when
// that is not the schema compared, or "no partition".
//
// Takes table (*dialect.Table) which is described.
// Takes schemaName (string) which is the schema compared.
//
// Returns string which is the description.
func describePartition(table *dialect.Table, schemaName string) string {
	if table.PartitionOf == nil {
		return "no partition"
	}
	if table.PartitionOf.Schema != schemaName {
		return "partition of " + table.PartitionOf.Name + " in " + table.PartitionOf.Schema
	}
	return "partition of " + table.PartitionOf.Name
}

// describeType describes a SQL type: its category and name, the schema it is in when that
// is not the schema compared, and its length, precision and scale.
//
// Takes sqlType (*dialect.SQLType) which is described.
// Takes schemaName (string) which is the schema compared.
//
// Returns string which is the description.
func describeType(sqlType *dialect.SQLType, schemaName string) string {
	var description strings.Builder
	description.WriteString(sqlType.Category.String() + " " + sqlType.EngineName)
	if sqlType.Schema != "" && sqlType.Schema != schemaName {
		description.WriteString(" in " + sqlType.Schema)
	}
	for _, modifier := range []struct {
		value *int
		label string
	}{
		{label: "length", value: sqlType.Length},
		{label: "precision", value: sqlType.Precision},
		{label: "scale", value: sqlType.Scale},
	} {
		if modifier.value != nil {
			fmt.Fprintf(&description, " %s %d", modifier.label, *modifier.value)
		}
	}
	return description.String()
}

// missing describes a relation or type one side lacks.
//
// Takes kind (string) which is table, view or enum.
// Takes name (string) which is its name.
// Takes side (string) which is the side that lacks it.
//
// Returns string which is the difference.
func missing(kind, name, side string) string {
	return kind + " " + name + " is missing from " + side
}

// columnNames returns the names of columns, in order.
//
// Takes columns ([]dialect.Column) which are named.
//
// Returns []string which holds the names.
func columnNames(columns []dialect.Column) []string {
	names := make([]string, 0, len(columns))
	for index := range columns {
		names = append(names, columns[index].Name)
	}
	return names
}

// unionOfKeys returns the keys of two maps, sorted and without repeats.
//
// Takes left (map[string]Value) which is one map.
// Takes right (map[string]Value) which is the other.
//
// Returns []string which holds the keys.
func unionOfKeys[Value any](left, right map[string]Value) []string {
	keys := slices.Collect(maps.Keys(left))
	for key := range right {
		if _, found := left[key]; !found {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	return keys
}
