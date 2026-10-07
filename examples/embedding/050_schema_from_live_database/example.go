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

package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"slices"
	"strings"

	"seagull.sh/seagull"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite"
	"seagull.sh/seagull/sdk/schema/schema_source_sqlite"
)

// run generates the store package against the schema of a live SQLite database rather
// than migration files: the schema source reads the tables through PRAGMA, and the
// queries are analysed against what it finds.
//
// Takes database (*sql.DB) which is the live database.
// Takes queries (fs.FS) which holds db/queries.
// Takes output (io.Writer) which receives the schema and the generated code.
//
// Returns error when the database cannot be read or generation fails.
func run(ctx context.Context, database *sql.DB, queries fs.FS, output io.Writer) error {
	dialect := dialect_engine_sqlite.New()
	generator, err := seagull.New(dialect, seagull.WithSchemaSource(schema_source_sqlite.New(database, dialect)))
	if err != nil {
		return err
	}
	input := seagull.Input{
		Migrations:   nil,
		Queries:      queries,
		Package:      "store",
		MigrationDir: "",
		QueryDir:     "db/queries",
	}

	catalogue, _, err := generator.Catalogue(ctx, input)
	if err != nil {
		return fmt.Errorf("reading the schema: %w", err)
	}
	schema := catalogue.Schemas[catalogue.DefaultSchema]
	for _, name := range slices.Sorted(maps.Keys(schema.Tables)) {
		var columns []string
		table := schema.Tables[name]
		for index := range table.Columns {
			columns = append(columns, table.Columns[index].Name+" "+table.Columns[index].SQLType.EngineName)
		}
		fmt.Fprintf(output, "table %s: %s\n", name, strings.Join(columns, ", "))
	}

	result, err := generator.Generate(ctx, input)
	if err != nil {
		return err
	}
	if err := result.Err(); err != nil {
		return err
	}
	for _, file := range result.Files {
		if file.Name == "models.go" || file.Name == "club.sql.go" {
			fmt.Fprintf(output, "\n%s:\n%s", file.Name, declarations(string(file.Content)))
		}
	}
	return nil
}

// declarations keeps the type declarations and method signatures of a generated file,
// which say what the schema source taught the generator.
//
// Takes source (string) which is the generated Go source.
//
// Returns string which holds the struct types and the func lines.
func declarations(source string) string {
	var kept strings.Builder
	inType := false
	for line := range strings.Lines(source) {
		switch {
		case strings.HasPrefix(line, "type ") && strings.HasSuffix(line, "{\n"):
			inType = true
			kept.WriteString(line)
		case inType:
			kept.WriteString(line)
			inType = line != "}\n"
		case strings.HasPrefix(line, "func (queries"):
			kept.WriteString(strings.TrimSuffix(line, " {\n") + "\n")
		}
	}
	return kept.String()
}
