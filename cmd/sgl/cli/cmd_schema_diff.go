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

package cli

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"seagull.sh/seagull/cmd/sgl/internal/redact"
	"seagull.sh/seagull/sdk/dialect"
)

// runSchemaDiff compares a live database with the schema the migrations describe.
//
// Takes arguments ([]string) which holds the flags.
//
// Returns int which is 1 when they differ.
func (inv *invocation) runSchemaDiff(ctx context.Context, arguments []string) int {
	flags := inv.newFlags("schema diff", "schema diff [--dsn DSN] [--database name]")
	options := migrateFlagDefaults
	flags.StringVar(&options.database, databaseFlag, inv.database, "compare the `name`d database")
	flags.StringVar(&options.dsn, dsnFlag, "", dsnUsage)
	positional, status, proceed := flags.parse(arguments)
	if !proceed {
		return status
	}
	if len(positional) > 0 {
		return inv.fail(usageError("schema diff takes no arguments; got %q", positional[0]))
	}

	opened, err := inv.connect(ctx, options.database, options.dsn)
	if err != nil {
		return inv.fail(err)
	}
	defer func() { _ = opened.Close() }()
	if !hasIntrospection(opened.dialect.Info().Family) {
		return inv.fail(usageError("dialect %s cannot be introspected, so it cannot be compared", opened.configured.Dialect))
	}

	expected, err := inv.migrationCatalogue(ctx, opened)
	if err != nil {
		return inv.fail(err)
	}
	live, err := inv.liveCatalogue(ctx, opened)
	if err != nil {
		return inv.fail(err)
	}

	differences := diffSchemas(dumpCatalogue(opened.configured.Name, expected), dumpCatalogue(opened.configured.Name, live), opened.bookkeepingTables())
	if len(differences) == 0 {
		fmt.Fprintf(inv.streams.Stdout, "%s: the database matches the migrations\n", opened.configured.Name)
		return exitOK
	}
	writeDifferences(inv.streams.Stdout, differences)
	return exitFindings
}

// migrationCatalogue replays a database's migrations.
//
// Takes opened (*connection) which names the database.
//
// Returns *dialect.Catalogue which the migrations describe.
// Returns error when the migrations cannot be replayed.
func (inv *invocation) migrationCatalogue(ctx context.Context, opened *connection) (*dialect.Catalogue, error) {
	replayed, err := inv.openSession(ctx, opened.loaded, opened.configured, sessionOptions{observer: nil, schemaDSN: "", migrationsOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = replayed.Close() }()
	catalogue, diagnostics, err := replayed.generator.Catalogue(ctx, replayed.input)
	if err != nil {
		return nil, fmt.Errorf("replaying the migrations: %w", err)
	}
	if writeErr := writeText(inv.streams.Stderr, inv.findingsFor(replayed.database, opened.loaded, diagnostics)); writeErr != nil {
		return nil, writeErr
	}
	return catalogue, nil
}

// liveCatalogue introspects the connected database through the connection already open,
// printing the diagnostics found on the way.
//
// Takes opened (*connection) which is the database.
//
// Returns *dialect.Catalogue which the database holds.
// Returns error which is the context's cause when the run was cancelled, or a database
// error, with the connection string redacted, when the database cannot be introspected.
func (inv *invocation) liveCatalogue(ctx context.Context, opened *connection) (*dialect.Catalogue, error) {
	catalogue, diagnostics, err := introspector(opened.database, opened.dialect).BuildCatalogue(ctx)
	if err != nil && ctx.Err() != nil {
		return nil, context.Cause(ctx)
	}
	if err != nil {
		return nil, databaseError(fmt.Errorf("introspecting %s: %s", redact.DSN(opened.dsn), redact.Text(err.Error(), opened.dsn)))
	}
	if writeErr := writeText(inv.streams.Stderr, inv.findingsFor(opened.configured, opened.loaded, diagnostics)); writeErr != nil {
		return nil, writeErr
	}
	return catalogue, nil
}

// bookkeepingTables lists the tables the migrator and seeder keep, which the migrations
// do not describe.
//
// Returns []string which holds their names in lower case.
func (c *connection) bookkeepingTables() []string {
	return bookkeepingTables(c.configured, new(c.dialect.Info()), c.backends)
}

// diffSchemas lists how a live schema differs from the expected one.
//
// Takes expected (schemaDump) which the migrations describe.
// Takes live (schemaDump) which the database holds.
// Takes ignored ([]string) which are table names to leave out.
//
// Returns []string which holds one line per difference, sorted.
func diffSchemas(expected, live schemaDump, ignored []string) []string {
	expectedTables := indexTables(expected.Tables, ignored)
	liveTables := indexTables(live.Tables, ignored)
	var differences []string
	for name, table := range expectedTables {
		other, found := liveTables[name]
		if !found {
			differences = append(differences, fmt.Sprintf("- table %s is in the migrations but not the database", name))
			continue
		}
		differences = append(differences, diffColumns(name, table, other)...)
	}
	for name := range liveTables {
		if _, found := expectedTables[name]; !found {
			differences = append(differences, fmt.Sprintf("+ table %s is in the database but not the migrations", name))
		}
	}
	slices.Sort(differences)
	return differences
}

// indexTables maps tables by lower-case qualified name, leaving out ignored ones.
//
// Takes tables ([]tableDump) which are indexed.
// Takes ignored ([]string) which are table names to leave out.
//
// Returns map[string]tableDump which holds the rest.
func indexTables(tables []tableDump, ignored []string) map[string]tableDump {
	indexed := make(map[string]tableDump, len(tables))
	for _, table := range tables {
		if slices.Contains(ignored, strings.ToLower(table.Name)) {
			continue
		}
		indexed[strings.ToLower(table.qualifiedName())] = table
	}
	return indexed
}

// diffColumns lists how one table's live columns differ from the expected ones.
//
// Takes name (string) which is the table's qualified name.
// Takes expected (tableDump) which the migrations describe.
// Takes live (tableDump) which the database holds.
//
// Returns []string which holds one line per difference.
func diffColumns(name string, expected, live tableDump) []string {
	liveColumns := make(map[string]columnDump, len(live.Columns))
	for _, column := range live.Columns {
		liveColumns[strings.ToLower(column.Name)] = column
	}
	var differences []string
	for _, column := range expected.Columns {
		key := strings.ToLower(column.Name)
		other, found := liveColumns[key]
		delete(liveColumns, key)
		switch {
		case !found:
			differences = append(differences, fmt.Sprintf("- column %s.%s is in the migrations but not the database", name, column.Name))
		case !strings.EqualFold(column.Type, other.Type):
			differences = append(differences, fmt.Sprintf("~ column %s.%s is %s in the migrations but %s in the database", name, column.Name, column.Type, other.Type))
		case column.Nullable != other.Nullable:
			differences = append(differences, fmt.Sprintf("~ column %s.%s is %s in the migrations but %s in the database",
				name, column.Name, nullability(column.Nullable), nullability(other.Nullable)))
		}
	}
	for _, column := range liveColumns {
		differences = append(differences, fmt.Sprintf("+ column %s.%s is in the database but not the migrations", name, column.Name))
	}
	return differences
}

// writeDifferences prints the differences.
//
// Takes writer (io.Writer) which receives them.
// Takes differences ([]string) which are printed.
func writeDifferences(writer io.Writer, differences []string) {
	for _, difference := range differences {
		fmt.Fprintln(writer, difference)
	}
}
