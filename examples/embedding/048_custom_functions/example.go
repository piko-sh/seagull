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
	"strings"
	"unicode"

	"seagull.sh/seagull"
	"seagull.sh/seagull/examples/embedding/048_custom_functions/internal/store"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite"
)

// slugifyFunction declares slugify to Seagull, as custom_functions in sgl.yaml does for
// sgl: one text argument, a text result, and NULL in gives NULL out.
var slugifyFunction = seagull.CustomFunction{
	Name:         "slugify",
	ReturnType:   "text",
	Nullable:     "null_on_null",
	Arguments:    []string{"text"},
	MinArguments: 0,
	IsAggregate:  false,
	IsVariadic:   false,
}

// slugify turns a title into a URL slug: lower-case letters and digits, with one hyphen
// between words. SQLite calls it through the function main.go registers with the driver.
//
// Takes title (string) which is the text to convert.
//
// Returns string which is the slug.
func slugify(title string) string {
	words := strings.FieldsFunc(strings.ToLower(title), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	return strings.Join(words, "-")
}

// run vets the queries without and with the declaration of slugify, then calls the
// generated methods, whose SQL runs slugify inside SQLite.
//
// Takes database (*sql.DB) which is a migrated database with slugify registered.
// Takes project (fs.FS) which holds db/migrations and db/queries.
// Takes output (io.Writer) which receives the results.
//
// Returns error when vetting or a query fails.
func run(ctx context.Context, database *sql.DB, project fs.FS, output io.Writer) error {
	if err := vet(ctx, project, output); err != nil {
		return err
	}
	if err := vet(ctx, project, output, slugifyFunction); err != nil {
		return err
	}

	queries := store.New(database)
	for _, title := range []string{"Gulls of the North Sea", "Why Chips?  A Field Guide"} {
		if _, err := queries.CreateArticle(ctx, title); err != nil {
			return fmt.Errorf("creating %q: %w", title, err)
		}
	}
	articles, err := queries.ListArticleSlugs(ctx)
	if err != nil {
		return fmt.Errorf("listing slugs: %w", err)
	}
	for _, article := range articles {
		fmt.Fprintf(output, "%d\t%-28s %s\n", article.ID, article.Title, article.Slug)
	}

	found, err := queries.FindArticleBySlug(ctx, "why-chips-a-field-guide")
	if err != nil {
		return fmt.Errorf("finding by slug: %w", err)
	}
	fmt.Fprintf(output, "why-chips-a-field-guide is article %d\n", found.ID)
	return nil
}

// vet analyses the queries with the given functions declared, and prints the diagnostics
// and the type Seagull inferred for the slug column.
//
// Takes project (fs.FS) which holds db/migrations and db/queries.
// Takes output (io.Writer) which receives the report.
// Takes functions (...seagull.CustomFunction) which are the functions to declare.
//
// Returns error when the queries cannot be analysed.
func vet(ctx context.Context, project fs.FS, output io.Writer, functions ...seagull.CustomFunction) error {
	generator, err := seagull.New(dialect_engine_sqlite.New(), seagull.WithCustomFunctions(functions...))
	if err != nil {
		return err
	}
	report, err := generator.Vet(ctx, seagull.Input{
		Migrations:   project,
		Queries:      nil,
		Package:      "",
		MigrationDir: "db/migrations",
		QueryDir:     "db/queries",
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(output, "custom functions declared: %d\n", len(functions))
	for _, diagnostic := range report.Diagnostics {
		fmt.Fprintf(output, "  %s:%d: %s[%s]: %s\n", diagnostic.Filename, diagnostic.Line,
			diagnostic.Severity, diagnostic.Code, diagnostic.Message)
	}
	for _, query := range report.Queries {
		for index := range query.OutputColumns {
			column := &query.OutputColumns[index]
			if column.Name != "slug" {
				continue
			}
			sqlType := column.SQLType.EngineName
			if sqlType == "" {
				sqlType = "unknown"
			}
			fmt.Fprintf(output, "  %s.slug: type %s, nullable %t\n", query.Name, sqlType, column.Nullable)
		}
	}
	return nil
}
