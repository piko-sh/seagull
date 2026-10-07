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
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"slices"
	"strings"
	"testing/fstest"

	"seagull.sh/seagull"
	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite"
	"seagull.sh/seagull/sdk/emit"
)

// maxFileBytes is a file size limit smaller than any file of the broken project.
const maxFileBytes = 32

// sentinels are the errors a program embedding the generator matches with errors.Is. Some
// wrap others: ErrNoSource and ErrInvalidPrefix are ErrInvalidConfig, and ErrFileTooLarge
// is ErrLimit. The generator reports SQL it cannot parse as a Q010 diagnostic; the
// resulting DiagnosticsError also matches ErrParse. ErrMalformedSQL identifies malformed
// SQL returned directly by the migration runner.
var sentinels = []struct {
	err  error
	name string
}{
	{err: seagull.ErrDiagnostics, name: "ErrDiagnostics"},
	{err: seagull.ErrInvalidConfig, name: "ErrInvalidConfig"},
	{err: seagull.ErrNoSource, name: "ErrNoSource"},
	{err: seagull.ErrInvalidPrefix, name: "ErrInvalidPrefix"},
	{err: seagull.ErrLimit, name: "ErrLimit"},
	{err: seagull.ErrFileTooLarge, name: "ErrFileTooLarge"},
}

// run is a small linter built on Seagull's analysis. It prints what Vet learnt about each
// query, applies one rule of its own, prints the tables of the schema catalogue, and
// shows which error sentinels match the ways a run can fail.
//
// Takes project (fs.FS) which holds db/migrations and db/queries.
// Takes output (io.Writer) which receives the report.
//
// Returns error when the project cannot be analysed.
func run(ctx context.Context, project fs.FS, output io.Writer) error {
	generator, err := seagull.New(dialect_engine_sqlite.New())
	if err != nil {
		return err
	}
	input := seagull.Input{
		Migrations:   project,
		Queries:      nil,
		Package:      "",
		MigrationDir: "db/migrations",
		QueryDir:     "db/queries",
	}

	report, err := generator.Vet(ctx, input)
	if err != nil {
		return err
	}
	if err := report.Err(); err != nil {
		return err
	}
	findings := 0
	for _, query := range report.Queries {
		describeQuery(output, query)
		if finding, found := lint(query); found {
			fmt.Fprintf(output, "  lint: %s\n", finding)
			findings++
		}
	}
	fmt.Fprintf(output, "%d queries, %d lint finding(s)\n\n", len(report.Queries), findings)

	catalogue, _, err := generator.Catalogue(ctx, input)
	if err != nil {
		return err
	}
	describeCatalogue(output, catalogue)

	return describeFailures(ctx, generator, output)
}

// lint is the linter's own rule: a query that writes but takes no parameter changes every
// row it touches, which is rarely what its author meant.
//
// Takes query (*emit.AnalysedQuery) which is an analysed query.
//
// Returns string which describes the finding.
// Returns bool which is true when the rule found something.
func lint(query *emit.AnalysedQuery) (string, bool) {
	if query.ReadOnly || len(query.Parameters) > 0 {
		return "", false
	}
	return fmt.Sprintf("%s writes without a parameter, so it changes every row", query.Name), true
}

// describeQuery prints a query's command, whether it only reads, its parameters and its
// output columns with their types and nullability.
//
// Takes output (io.Writer) which receives the description.
// Takes query (*emit.AnalysedQuery) which is an analysed query.
func describeQuery(output io.Writer, query *emit.AnalysedQuery) {
	access := "writes"
	if query.ReadOnly {
		access = "reads"
	}
	fmt.Fprintf(output, "%s (%s, %s) %s:%d\n", query.Name, query.Command, access, query.Filename, query.Line)
	for index := range query.Parameters {
		parameter := &query.Parameters[index]
		fmt.Fprintf(output, "  param  %-6s %-6s %s\n", parameter.Name, parameter.SQLType.EngineName, nullability(parameter.Nullable))
	}
	for index := range query.OutputColumns {
		column := &query.OutputColumns[index]
		source := "expression"
		if column.SourceTable != "" {
			source = column.SourceTable + "." + column.SourceColumn
		}
		fmt.Fprintf(output, "  column %-6s %-6s %-8s from %s\n", column.Name, column.SQLType.EngineName, nullability(column.Nullable), source)
	}
}

// describeCatalogue prints every table of the schema the migrations build.
//
// Takes output (io.Writer) which receives the description.
// Takes catalogue (*dialect.Catalogue) which is the schema.
func describeCatalogue(output io.Writer, catalogue *dialect.Catalogue) {
	schema := catalogue.Schemas[catalogue.DefaultSchema]
	for _, name := range slices.Sorted(maps.Keys(schema.Tables)) {
		table := schema.Tables[name]
		columns := make([]string, 0, len(table.Columns))
		for index := range table.Columns {
			column := &table.Columns[index]
			columns = append(columns, fmt.Sprintf("%s %s %s", column.Name, column.SQLType.EngineName, nullability(column.Nullable)))
		}
		fmt.Fprintf(output, "table %s.%s (primary key %s)\n  %s\n", schema.Name, table.Name,
			strings.Join(table.PrimaryKey, ", "), strings.Join(columns, "\n  "))
	}
	fmt.Fprintln(output)
}

// describeFailures provokes each way a run can fail and prints the sentinels the error
// matches: error diagnostics come back from Result.Err and Report.Err, the rest from New,
// Generate and Vet themselves.
//
// Takes generator (*seagull.Generator) which is a working generator.
// Takes output (io.Writer) which receives the description.
//
// Returns error when a run that should fail succeeds.
func describeFailures(ctx context.Context, generator *seagull.Generator, output io.Writer) error {
	broken := fstest.MapFS{
		"migrations/0001_init.up.sql": {Data: []byte("CREATE TABLE notes (id INTEGER PRIMARY KEY, title TEXT NOT NULL);")},
		"queries/notes.sql":           {Data: []byte("-- sgl.query(GetNote, one, readonly: maybe)\nSELECT id, title FROM notes WHERE id = ?;\n")},
	}
	brokenInput := seagull.Input{Migrations: broken, Queries: nil, Package: "store", MigrationDir: "migrations", QueryDir: "queries"}

	report, err := generator.Vet(ctx, brokenInput)
	if err != nil {
		return err
	}
	diagnosticsErr := report.Err()
	if diagnostics, ok := errors.AsType[*seagull.DiagnosticsError](diagnosticsErr); ok {
		fmt.Fprintln(output, "Vet of a query with readonly: maybe")
		for _, diagnostic := range diagnostics.Diagnostics {
			fmt.Fprintf(output, "  %s:%d:%d: %s[%s]: %s\n", diagnostic.Filename, diagnostic.Line, diagnostic.Column,
				diagnostic.Severity, diagnostic.Code, diagnostic.Message)
		}
	}

	_, noDialectErr := seagull.New(dialect.Dialect{})

	badPackage := brokenInput
	badPackage.Package = "my-store"
	_, badPackageErr := generator.Generate(ctx, badPackage)

	_, badPrefixErr := seagull.New(dialect_engine_sqlite.New(), seagull.WithDirectivePrefixes("Acme"))

	limited, err := seagull.New(dialect_engine_sqlite.New(), seagull.WithMaxFileBytes(maxFileBytes))
	if err != nil {
		return err
	}
	_, tooLargeErr := limited.Vet(ctx, brokenInput)

	failures := []struct {
		err  error
		name string
	}{
		{err: diagnosticsErr, name: "Report.Err with an error diagnostic"},
		{err: noDialectErr, name: "New without a dialect"},
		{err: badPrefixErr, name: "New with the prefix Acme"},
		{err: badPackageErr, name: "Generate into package my-store"},
		{err: tooLargeErr, name: fmt.Sprintf("Vet with WithMaxFileBytes(%d)", maxFileBytes)},
	}
	for _, failure := range failures {
		if failure.err == nil {
			return fmt.Errorf("%s did not fail", failure.name)
		}
		var matched []string
		for _, sentinel := range sentinels {
			if errors.Is(failure.err, sentinel.err) {
				matched = append(matched, sentinel.name)
			}
		}
		fmt.Fprintf(output, "%s: %s\n", failure.name, strings.Join(matched, ", "))
	}
	return nil
}

// nullability names whether a value can be NULL.
//
// Takes nullable (bool) which is true when it can.
//
// Returns string which is null or not null.
func nullability(nullable bool) string {
	if nullable {
		return "null"
	}
	return "not null"
}
