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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"seagull.sh/seagull/cmd/internal/assemble"
	"seagull.sh/seagull/cmd/internal/config"
	"seagull.sh/seagull/sdk/dialect"
)

// schemaUsage is the usage of the schema command group, with %[1]s for the program name.
const schemaUsage = `Usage: %[1]s schema dump [--from-db DSN] [--format json|sql] | %[1]s schema diff [--dsn DSN]

Subcommands:
  dump  Print the schema the migrations build, or one read from a live database
  diff  Compare a live database with the schema the migrations build

Run "%[1]s schema <subcommand> -h" for its flags.
`

// schemaDump is the printed form of one database's catalogue.
type schemaDump struct {
	// Database names the database.
	Database string `json:"database"`

	// Tables holds the tables, sorted by schema and name.
	Tables []tableDump `json:"tables"`

	// Views holds the views, sorted by schema and name.
	Views []tableDump `json:"views,omitempty"`

	// Enums holds the enum types, sorted by schema and name.
	Enums []enumDump `json:"enums,omitempty"`
}

// tableDump is one table or view.
type tableDump struct {
	// Schema is the table's schema.
	Schema string `json:"schema"`

	// Name is the table's name.
	Name string `json:"name"`

	// Columns holds the columns in declaration order.
	Columns []columnDump `json:"columns"`

	// PrimaryKey lists the primary key's columns.
	PrimaryKey []string `json:"primary_key,omitempty"`
}

// qualifiedName joins a schema and a name.
//
// Returns string which is schema.name, or name when the schema is empty.
func (t tableDump) qualifiedName() string {
	if t.Schema == "" {
		return t.Name
	}
	return t.Schema + "." + t.Name
}

// columnDump is one column.
type columnDump struct {
	// Name is the column's name.
	Name string `json:"name"`

	// Type is the column's SQL type.
	Type string `json:"type"`

	// Nullable reports whether the column accepts NULL.
	Nullable bool `json:"nullable"`

	// HasDefault reports whether the column has a default.
	HasDefault bool `json:"has_default,omitempty"`

	// Generated reports whether the column is generated.
	Generated bool `json:"generated,omitempty"`
}

// enumDump is one enum type.
type enumDump struct {
	// Schema is the enum's schema.
	Schema string `json:"schema"`

	// Name is the enum's name.
	Name string `json:"name"`

	// Values lists the labels in order.
	Values []string `json:"values"`
}

// runSchema dumps the schema, or compares it with a live database.
//
// Takes arguments ([]string) which holds the subcommand and the flags.
//
// Returns int which is the exit status.
func (inv *invocation) runSchema(ctx context.Context, arguments []string) int {
	if status, proceed := inv.runGroup(arguments, schemaUsage); !proceed {
		return status
	}
	switch arguments[0] {
	case "dump":
		return inv.runSchemaDump(ctx, arguments[1:])
	case "diff":
		return inv.runSchemaDiff(ctx, arguments[1:])
	default:
		return inv.fail(usageError("unknown schema subcommand %q; want dump or diff", arguments[0]))
	}
}

// dumpDatabase builds one database's catalogue, from its migrations or a live database,
// printing the diagnostics found on the way.
//
// Takes loaded (*project) which holds the configuration.
// Takes selected (*config.Database) which is dumped.
// Takes fromDB (string) which is the --from-db value, or empty.
//
// Returns schemaDump which is the printed form of the catalogue.
// Returns error when the catalogue cannot be built.
func (inv *invocation) dumpDatabase(ctx context.Context, loaded *project, selected *config.Database, fromDB string) (schemaDump, error) {
	opened, err := inv.openSession(ctx, loaded, selected, sessionOptions{observer: nil, schemaDSN: fromDB, migrationsOnly: false})
	if err != nil {
		return schemaDump{}, err
	}
	defer func() { _ = opened.Close() }()
	catalogue, diagnostics, err := opened.generator.Catalogue(ctx, opened.input)
	if err != nil {
		err = fmt.Errorf("database %q: %w", selected.Name, err)
		if opened.connection != nil {
			err = migrationFailure(err)
		}
		return schemaDump{}, err
	}
	if err := writeText(inv.streams.Stderr, inv.findingsFor(opened.database, loaded, diagnostics)); err != nil {
		return schemaDump{}, err
	}
	return dumpCatalogue(selected.Name, catalogue), nil
}

// runSchemaDump prints the catalogue built from the migrations or from a live database.
//
// Takes arguments ([]string) which holds the flags.
//
// Returns int which is the exit status.
func (inv *invocation) runSchemaDump(ctx context.Context, arguments []string) int {
	flags := inv.newFlags("schema dump", "schema dump [--from-db DSN] [--format json|sql] [--database name]")
	database := flags.String(databaseFlag, inv.database, "dump only the `name`d database")
	fromDB := flags.String("from-db", "", "introspect the live database at this `DSN` instead of replaying the migrations")
	format := flags.String("format", formatJSON, "output format: json or sql")
	positional, status, proceed := flags.parse(arguments)
	if !proceed {
		return status
	}
	if len(positional) > 0 {
		return inv.fail(usageError("schema dump takes no arguments; got %q", positional[0]))
	}
	if *format != formatJSON && *format != "sql" {
		return inv.fail(usageError("unknown --format %q: want json or sql", *format))
	}
	loaded, err := inv.loadProject(*database)
	if err != nil {
		return inv.fail(err)
	}
	if *fromDB != "" && len(loaded.databases) != 1 {
		return inv.fail(usageError("--from-db needs one database; choose it with --database"))
	}

	dumps := make([]schemaDump, 0, len(loaded.databases))
	for _, selected := range loaded.databases {
		dump, err := inv.dumpDatabase(ctx, loaded, selected, *fromDB)
		if err != nil {
			return inv.fail(err)
		}
		dumps = append(dumps, dump)
	}
	if *format == formatJSON {
		err = writeSchemaJSON(inv.streams.Stdout, dumps)
	} else {
		err = writeSchemaSQL(inv.streams.Stdout, dumps)
	}
	if err != nil {
		return inv.fail(err)
	}
	return exitOK
}

// dumpCatalogue converts a catalogue to its printed form.
//
// Takes database (string) which names the database.
// Takes catalogue (*dialect.Catalogue) which is converted.
//
// Returns schemaDump which is sorted for stable output.
func dumpCatalogue(database string, catalogue *dialect.Catalogue) schemaDump {
	dump := schemaDump{Database: database, Tables: []tableDump{}, Views: nil, Enums: nil}
	if catalogue == nil {
		return dump
	}
	for _, schemaName := range slices.Sorted(maps.Keys(catalogue.Schemas)) {
		schema := catalogue.Schemas[schemaName]
		if schema == nil {
			continue
		}
		for _, tableName := range slices.Sorted(maps.Keys(schema.Tables)) {
			table := schema.Tables[tableName]
			dump.Tables = append(dump.Tables, tableDump{Schema: schemaName, Name: table.Name, Columns: dumpColumns(table.Columns), PrimaryKey: table.PrimaryKey})
		}
		for _, viewName := range slices.Sorted(maps.Keys(schema.Views)) {
			view := schema.Views[viewName]
			dump.Views = append(dump.Views, tableDump{Schema: schemaName, Name: view.Name, Columns: dumpColumns(view.Columns), PrimaryKey: nil})
		}
		for _, enumName := range slices.Sorted(maps.Keys(schema.Enums)) {
			enum := schema.Enums[enumName]
			dump.Enums = append(dump.Enums, enumDump{Schema: schemaName, Name: enum.Name, Values: enum.Values})
		}
	}
	return dump
}

// dumpColumns converts columns to their printed form.
//
// Takes columns ([]dialect.Column) which are converted.
//
// Returns []columnDump which keeps their order.
func dumpColumns(columns []dialect.Column) []columnDump {
	dumped := make([]columnDump, 0, len(columns))
	for index := range columns {
		column := &columns[index]
		typeName := assemble.SQLTypeName(column.SQLType) + strings.Repeat("[]", column.ArrayDimensions)
		dumped = append(dumped, columnDump{
			Name:       column.Name,
			Type:       typeName,
			Nullable:   column.Nullable,
			HasDefault: column.HasDefault,
			Generated:  column.IsGenerated,
		})
	}
	return dumped
}

// writeSchemaJSON prints dumps as one JSON document.
//
// Takes writer (io.Writer) which receives the document.
// Takes dumps ([]schemaDump) which are printed.
//
// Returns error when encoding fails.
func writeSchemaJSON(writer io.Writer, dumps []schemaDump) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(struct {
		Databases []schemaDump `json:"databases"`
	}{Databases: dumps})
}

// writeSchemaSQL prints dumps as approximate DDL, for reading rather than replaying.
//
// Takes writer (io.Writer) which receives the DDL.
// Takes dumps ([]schemaDump) which are printed.
//
// Returns error when writing fails.
func writeSchemaSQL(writer io.Writer, dumps []schemaDump) error {
	var buffer bytes.Buffer
	for _, dump := range dumps {
		fmt.Fprintf(&buffer, "-- database %s\n\n", dump.Database)
		for _, enum := range dump.Enums {
			fmt.Fprintf(&buffer, "-- enum %s.%s: %s\n\n", enum.Schema, enum.Name, strings.Join(enum.Values, ", "))
		}
		for _, table := range dump.Tables {
			writeCreateTable(&buffer, table)
		}
		for _, view := range dump.Views {
			columns := make([]string, 0, len(view.Columns))
			for _, column := range view.Columns {
				columns = append(columns, column.Name+" "+column.Type)
			}
			fmt.Fprintf(&buffer, "-- view %s: %s\n\n", view.qualifiedName(), strings.Join(columns, ", "))
		}
	}
	_, err := writer.Write(buffer.Bytes())
	return err
}

// writeCreateTable prints one table as a CREATE TABLE statement.
//
// Takes buffer (*bytes.Buffer) which receives the statement.
// Takes table (tableDump) which is printed.
func writeCreateTable(buffer *bytes.Buffer, table tableDump) {
	lines := make([]string, 0, len(table.Columns)+1)
	for _, column := range table.Columns {
		line := "    " + column.Name + " " + column.Type
		if !column.Nullable {
			line += " NOT NULL"
		}
		lines = append(lines, line)
	}
	if len(table.PrimaryKey) > 0 {
		lines = append(lines, "    PRIMARY KEY ("+strings.Join(table.PrimaryKey, ", ")+")")
	}
	fmt.Fprintf(buffer, "CREATE TABLE %s (\n%s\n);\n\n", table.qualifiedName(), strings.Join(lines, ",\n"))
}
