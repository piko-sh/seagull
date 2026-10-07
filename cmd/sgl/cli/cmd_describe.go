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
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"seagull.sh/seagull"
	"seagull.sh/seagull/cmd/internal/assemble"
	"seagull.sh/seagull/cmd/internal/config"
	"seagull.sh/seagull/cmd/internal/editdistance"
	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/emit"
	"seagull.sh/seagull/sdk/emit/emit_kit"
)

// description is what describe prints about one query.
type description struct {
	// Name is the query's name.
	Name string `json:"name"`

	// Command is the query's command, such as one or many.
	Command string `json:"command"`

	// Database names the database.
	Database string `json:"database"`

	// File is the query file.
	File string `json:"file"`

	// SQL is the query's SQL.
	SQL string `json:"sql"`

	// Go holds the generated method, the SQL constant it runs and the types it uses, or is
	// empty when the code cannot be generated.
	Go string `json:"go,omitempty"`

	// Parameters describes the query's parameters.
	Parameters []describedParameter `json:"parameters"`

	// Columns describes the query's result columns.
	Columns []describedColumn `json:"columns"`

	// Line is the query's one-based line in its file.
	Line int `json:"line"`
}

// describedParameter is one parameter of a described query.
type describedParameter struct {
	// Name is the parameter's name.
	Name string `json:"name"`

	// Placeholder is how the generated code writes the parameter in its SQL, in the
	// dialect's placeholder form, such as $1, ?1 or {id:UInt64}.
	Placeholder string `json:"placeholder"`

	// Type is the parameter's SQL type.
	Type string `json:"type"`

	// Number is the parameter's one-based position.
	Number int `json:"number"`

	// Nullable reports whether the parameter accepts NULL.
	Nullable bool `json:"nullable"`

	// Slice reports whether the parameter expands to a list.
	Slice bool `json:"slice,omitempty"`

	// Optional reports whether the parameter may be omitted.
	Optional bool `json:"optional,omitempty"`
}

// describedColumn is one result column of a described query.
type describedColumn struct {
	// Name is the column's name.
	Name string `json:"name"`

	// Type is the column's SQL type.
	Type string `json:"type"`

	// Source is the table column it comes from, if any.
	Source string `json:"source,omitempty"`

	// Nullable reports whether the column may be NULL.
	Nullable bool `json:"nullable"`
}

// runDescribe shows one query's parameters, columns, nullability and Go signature.
//
// Takes arguments ([]string) which holds the flags and the query name.
//
// Returns int which is the exit status.
func (inv *invocation) runDescribe(ctx context.Context, arguments []string) int {
	flags := inv.newFlags("describe", "describe [--format text|json] [--database name] <Query>")
	database := flags.String(databaseFlag, inv.database, "look only in the `name`d database")
	outputFormat := flags.String("format", formatText, "output format: text or json")
	positional, status, proceed := flags.parse(arguments)
	if !proceed {
		return status
	}
	if len(positional) != 1 {
		return inv.fail(usageError("describe needs exactly one query name"))
	}
	if *outputFormat != formatText && *outputFormat != formatJSON {
		return inv.fail(usageError("unknown --format %q: want text or json", *outputFormat))
	}
	loaded, err := inv.loadProject(*database)
	if err != nil {
		return inv.fail(err)
	}

	described, err := inv.describeQuery(ctx, loaded, positional[0])
	if err != nil {
		return inv.fail(err)
	}
	if *outputFormat == formatJSON {
		encoder := json.NewEncoder(inv.streams.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(described); err != nil {
			return inv.fail(err)
		}
		return exitOK
	}
	writeDescription(inv.streams.Stdout, described)
	return exitOK
}

// describeQuery finds a query by name in the selected databases and describes it.
//
// Takes loaded (*project) which holds the databases.
// Takes name (string) which is the query's directive name or its method's name.
//
// Returns *description which describes the query.
// Returns error which is a usage error when no database has the query.
func (inv *invocation) describeQuery(ctx context.Context, loaded *project, name string) (*description, error) {
	var known []string
	for _, database := range loaded.databases {
		described, names, err := inv.describeIn(ctx, loaded, database, name)
		if err != nil || described != nil {
			return described, err
		}
		known = append(known, names...)
	}
	message := fmt.Sprintf("no query named %q", name)
	if suggestion := closest(name, known); suggestion != "" {
		message += fmt.Sprintf("; did you mean %q?", suggestion)
	}
	return nil, usageError("%s", message)
}

// describeIn looks for a query in one database.
//
// Takes loaded (*project) which holds the configuration.
// Takes database (*config.Database) which is searched.
// Takes name (string) which is the query's directive name or its method's name.
//
// Returns *description which describes the query, or nil when the database lacks it.
// Returns []string which holds the database's query names, for suggestions.
// Returns error when the database cannot be analysed.
func (inv *invocation) describeIn(ctx context.Context, loaded *project, database *config.Database, name string) (*description, []string, error) {
	opened, err := inv.openSession(ctx, loaded, database, sessionOptions{observer: nil, schemaDSN: "", migrationsOnly: false})
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = opened.Close() }()
	queries, generated, err := describedQueries(ctx, opened)
	if err != nil {
		return nil, nil, fmt.Errorf("database %q: %w", database.Name, err)
	}
	methods := generatedMethods(generated)

	query := findDescribed(queries, methods, name)
	if query == nil {
		return nil, describableNames(queries, methods), nil
	}
	described := describe(query, new(opened.dialect.Info()), database.Name, inv.display(sourcePath(database, query.Filename)))
	if generated != nil {
		described.Go = generatedGo(assemble.NewGoIndex(generated.Files), methodOf(query, methods))
	}
	return described, nil, nil
}

// describedQueries analyses a database's queries and generates their code in one run.
// When the code cannot be generated, the queries are analysed without it.
//
// Takes opened (*session) which is the database's session.
//
// Returns []*emit.AnalysedQuery which holds the analysed queries.
// Returns *seagull.Result which is the generated code, or nil when it has error
// diagnostics or cannot be generated.
// Returns error when the queries cannot be analysed.
func describedQueries(ctx context.Context, opened *session) ([]*emit.AnalysedQuery, *seagull.Result, error) {
	detailed, err := opened.generator.GenerateDetailed(ctx, opened.input)
	if err == nil {
		var generated *seagull.Result
		if detailed.Result.Err() == nil {
			generated = detailed.Result
		}
		return detailed.Report.Queries, generated, nil
	}
	if ctx.Err() != nil {
		return nil, nil, err
	}
	report, err := opened.generator.Vet(ctx, opened.input)
	if err != nil {
		return nil, nil, err
	}
	return report.Queries, nil, nil
}

// generatedMethods returns the method the generated code runs each query with.
//
// Takes generated (*seagull.Result) which is the generation, or nil when the code cannot
// be generated.
//
// Returns map[string]string which maps each query's name to its method's, or nil.
func generatedMethods(generated *seagull.Result) map[string]string {
	if generated == nil {
		return nil
	}
	return generated.Methods
}

// findDescribed finds a query by its directive name or generated method name, preferring
// the directive name.
//
// Takes queries ([]*emit.AnalysedQuery) which are the database's queries.
// Takes methods (map[string]string) which maps each query's name to the method the
// generated code gives it, or is nil when the code cannot be generated.
// Takes name (string) which is the name asked for.
//
// Returns *emit.AnalysedQuery which is the query, or nil when no query has the name.
func findDescribed(queries []*emit.AnalysedQuery, methods map[string]string, name string) *emit.AnalysedQuery {
	for _, query := range queries {
		if query.Name == name {
			return query
		}
	}
	for _, query := range queries {
		if methodOf(query, methods) == name {
			return query
		}
	}
	return nil
}

// describableNames lists the names a query can be described by, for suggestions: each
// query's directive name and, where it differs, its method's name.
//
// Takes queries ([]*emit.AnalysedQuery) which are the database's queries.
// Takes methods (map[string]string) which maps each query's name to the method the
// generated code gives it, or is nil.
//
// Returns []string which holds the names.
func describableNames(queries []*emit.AnalysedQuery, methods map[string]string) []string {
	names := make([]string, 0, len(queries))
	for _, query := range queries {
		names = append(names, query.Name)
		if method := methodOf(query, methods); method != query.Name {
			names = append(names, method)
		}
	}
	return names
}

// methodOf names the generated method that runs a query: the one the generated code gives
// it, or, when the code cannot be generated, the one its name becomes.
//
// Takes query (*emit.AnalysedQuery) which is the query.
// Takes methods (map[string]string) which maps each query's name to the method the
// generated code gives it, or is nil.
//
// Returns string which is the method's name, or the query's own name when it cannot
// become one.
func methodOf(query *emit.AnalysedQuery, methods map[string]string) string {
	if method, planned := methods[query.Name]; planned {
		return method
	}
	method, err := emit_kit.MethodName(query.Name)
	if err != nil {
		return query.Name
	}
	return method
}

// describe converts an analysed query.
//
// Takes query (*emit.AnalysedQuery) which is described.
// Takes info (*dialect.Facts) which says how the dialect writes parameters and
// identifiers.
// Takes database (string) which names its database.
// Takes file (string) which is its file, for printing.
//
// Returns *description which describes it, without the Go code.
func describe(query *emit.AnalysedQuery, info *dialect.Facts, database, file string) *description {
	placeholders := &info.Placeholders
	described := &description{
		Name:       query.Name,
		Command:    query.Command.String(),
		Database:   database,
		File:       file,
		SQL:        executedSQL(query, info),
		Go:         "",
		Parameters: make([]describedParameter, 0, len(query.Parameters)),
		Columns:    make([]describedColumn, 0, len(query.OutputColumns)),
		Line:       query.Line,
	}
	for index := range query.Parameters {
		parameter := &query.Parameters[index]
		described.Parameters = append(described.Parameters, describedParameter{
			Name:        parameter.Name,
			Placeholder: placeholders.Execution.Render(parameter.Number, placeholders.BoundName(parameter.Name), assemble.SQLTypeName(parameter.SQLType)),
			Type:        assemble.SQLTypeName(parameter.SQLType),
			Number:      parameter.Number,
			Nullable:    parameter.Nullable,
			Slice:       parameter.IsSlice,
			Optional:    parameter.IsOptional,
		})
	}
	for index := range query.OutputColumns {
		column := &query.OutputColumns[index]
		source := ""
		if column.SourceTable != "" && column.SourceColumn != "" {
			source = column.SourceTable + "." + column.SourceColumn
		}
		described.Columns = append(described.Columns, describedColumn{
			Name:     column.Name,
			Type:     assemble.SQLTypeName(column.SQLType),
			Source:   source,
			Nullable: column.Nullable,
		})
	}
	return described
}

// executedSQL writes a query's SQL as the generated code writes it: without the directive
// lines of its header, and with each placeholder in the form the query executes in, while
// its stars and lists stay as written.
//
// Takes query (*emit.AnalysedQuery) which is the query.
// Takes info (*dialect.Facts) which says how the dialect writes parameters and
// identifiers.
//
// Returns string which is the SQL, or the query's text as its file holds it when its
// template cannot be written.
func executedSQL(query *emit.AnalysedQuery, info *dialect.Facts) string {
	renderer := &emit_kit.Renderer{
		WrapColumn:   nil,
		Query:        query,
		Prefix:       "",
		Quote:        info.IdentifierQuote,
		Lists:        info.Lists,
		Form:         emit_kit.ExecutionForm(info, query),
		Placeholders: info.Placeholders,
		Omit:         0,
		ListMode:     emit_kit.ListAsWritten,
		KeepStars:    true,
	}
	rendered, err := emit_kit.Render(query.Template, renderer)
	if err != nil || strings.TrimSpace(rendered.SQL) == "" {
		return strings.TrimSpace(query.SQL)
	}
	return strings.TrimSpace(rendered.SQL)
}

// writeDescription prints a description for a terminal.
//
// Takes writer (io.Writer) which receives it.
// Takes described (*description) which is printed.
func writeDescription(writer io.Writer, described *description) {
	fmt.Fprintf(writer, "%s (%s) in database %q, %s:%d\n\n", described.Name, described.Command, described.Database, described.File, described.Line)
	fmt.Fprintf(writer, "SQL:\n    %s\n\n", strings.ReplaceAll(described.SQL, "\n", "\n    "))
	table := newTable(writer)
	fmt.Fprintln(table, "Parameters:")
	if len(described.Parameters) == 0 {
		fmt.Fprintln(table, "    none")
	}
	for _, parameter := range described.Parameters {
		fmt.Fprintf(table, "    %s\t%s\t%s\t%s\n", parameter.Placeholder, parameter.Name, parameter.Type, nullability(parameter.Nullable))
	}
	fmt.Fprintln(table, "\nColumns:")
	if len(described.Columns) == 0 {
		fmt.Fprintln(table, "    none")
	}
	for _, column := range described.Columns {
		fmt.Fprintf(table, "    %s\t%s\t%s\t%s\n", column.Name, column.Type, nullability(column.Nullable), column.Source)
	}
	_ = table.Flush()
	if described.Go != "" {
		goText := strings.ReplaceAll(strings.TrimSpace(described.Go), "\t", "    ")
		fmt.Fprintf(writer, "\nGo:\n    %s\n", strings.ReplaceAll(goText, "\n", "\n    "))
	}
}

// nullability names whether a value may be NULL.
//
// Takes nullable (bool) which is the flag.
//
// Returns string which is "null" or "not null".
func nullability(nullable bool) string {
	if nullable {
		return "null"
	}
	return "not null"
}

// generatedGo prints the Go a query generates: the SQL constant its method runs, the
// struct types the method uses, and the method's signature.
//
// Takes index (*assemble.GoIndex) which holds the generated files' declarations.
// Takes method (string) which is the name of the query's method.
//
// Returns string which is the printed declarations, or empty when the method is absent.
func generatedGo(index *assemble.GoIndex, method string) string {
	signature := index.Signature(method)
	if signature == "" {
		return ""
	}
	identifier, sql, found := index.SQLConst(method)
	if !found {
		return signature
	}
	return "const " + identifier + " = " + goStringLiteral(sql) + "\n\n" + signature
}

// goStringLiteral writes a string as a Go literal: raw when it can be, quoted when it
// holds a backquote or a carriage return.
//
// Takes value (string) which is written.
//
// Returns string which is the literal.
func goStringLiteral(value string) string {
	if strings.ContainsAny(value, "`\r") {
		return strconv.Quote(value)
	}
	return "`" + value + "`"
}

// closest returns the candidate nearest to name, ignoring case, when it is near enough to
// suggest: fewer edits away than half the name's length in runes, plus one.
//
// Takes name (string) which is what was asked for.
// Takes candidates ([]string) which are the known names.
//
// Returns string which is the suggestion, or empty.
func closest(name string, candidates []string) string {
	lowered := strings.ToLower(name)
	best, bestDistance := "", utf8.RuneCountInString(lowered)/2+1
	for _, candidate := range candidates {
		if distance := editdistance.Distance(lowered, strings.ToLower(candidate), bestDistance); distance < bestDistance {
			best, bestDistance = candidate, distance
		}
	}
	return best
}
