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

package dialecttest

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
)

const (
	// canonicalDirectivePrefix is the directive prefix the fixtures write.
	canonicalDirectivePrefix = "sgl"

	// aliasDirectivePrefix is a directive prefix the default preset accepts as an alias of
	// the canonical one, longer than it, so an offset that counts a directive line differs
	// between the two.
	aliasDirectivePrefix = "seagull"

	// strayCharacter is a character no dialect reads outside a string or comment.
	strayCharacter = " €"

	// openBlockComment opens a block comment that nothing closes.
	openBlockComment = " /* open"

	// brokenQueryFile is the name of the query file the prefix check writes.
	brokenQueryFile = "broken.sql"

	// brokenMigrationFile is the name of the migration the prefix check writes after the
	// base migration.
	brokenMigrationFile = "002_broken.up.sql"

	// readOnlyMigrationDirective is the body of the migration directive the prefix check
	// writes above the broken statement.
	readOnlyMigrationDirective = "migration(readonly: true)"

	// statementTerminator ends a statement the prefix check writes.
	statementTerminator = ";"
)

// brokenSource is a query or migration file the prefix check writes with a syntax error.
type brokenSource struct {
	// name describes the error.
	name string

	// content is the file's content, its directives written with the canonical prefix.
	content string

	// line is the 1-based line the error is on.
	line int

	// migration reports that the file is a migration rather than a query file.
	migration bool
}

// checkDiagnosticsIgnoreTheDirectivePrefix checks syntax error locations under directive
// prefix aliases.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds every problem found.
func checkDiagnosticsIgnoreTheDirectivePrefix(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	var problems []string
	for _, source := range brokenSources(fixture.NewDialect(), fixture) {
		canonical, err := brokenSourceDiagnostics(ctx, fixture, source, canonicalDirectivePrefix)
		if err != nil {
			problems = append(problems, fmt.Sprintf(generationFatalFormat, err))
			continue
		}
		alias, err := brokenSourceDiagnostics(ctx, fixture, source, aliasDirectivePrefix)
		if err != nil {
			problems = append(problems, fmt.Sprintf(generationFatalFormat, err))
			continue
		}
		if !slices.Equal(canonical, alias) {
			problems = append(problems, fmt.Sprintf("%s: the diagnostics differ between the %s and %s prefixes: %s against %s",
				source.name, canonicalDirectivePrefix, aliasDirectivePrefix, renderedList(canonical), renderedList(alias)))
		}
		if !reportsParseErrorOnLine(canonical, source.line) {
			problems = append(problems, fmt.Sprintf("%s: want a %s error on line %d, got %s",
				source.name, diag.CodeParseError, source.line, renderedList(canonical)))
		}
	}
	return outcomeOf(problems)
}

// brokenSources returns the files the prefix check writes: the fixture's query with a
// token it cannot hold, a stray character and an open block comment after it, and its
// base migration repeated below a migration directive with a doubled comma, a stray
// character and an open block comment.
//
// Takes engine (dialect.DialectPort) which supplies the comment syntax.
// Takes fixture (*Fixture) which supplies the SQL.
//
// Returns []brokenSource which holds the files.
func brokenSources(engine dialect.DialectPort, fixture *Fixture) []brokenSource {
	query := strings.TrimSuffix(strings.TrimSpace(fixture.ParamQuery), statementTerminator)
	queryLine := strings.Count(query, "\n") + 1
	directive := commentPrefix(engine) + " " + canonicalDirectivePrefix + "." + readOnlyMigrationDirective + "\n"
	migration := strings.TrimSuffix(strings.TrimSpace(fixture.BaseMigration), statementTerminator)
	migrationLine := strings.Count(directive, "\n") + 1
	return []brokenSource{
		{name: "a query with a token it cannot hold", content: query + trailingTokens + statementTerminator, line: queryLine, migration: false},
		{name: "a query with a stray character", content: query + strayCharacter + statementTerminator, line: queryLine, migration: false},
		{name: "a query with an open block comment", content: query + openBlockComment, line: queryLine, migration: false},
		{name: "a migration with a doubled comma", content: directive + strings.Replace(migration, ",", ",,", 1) + statementTerminator,
			line: migrationLine, migration: true},
		{name: "a migration with a stray character", content: directive + strings.Replace(migration, ",", strayCharacter+",", 1) + statementTerminator,
			line: migrationLine, migration: true},
		{name: "a migration with an open block comment", content: directive + migration + openBlockComment,
			line: migrationLine, migration: true},
	}
}

// brokenSourceDiagnostics generates over the fixture's base migration and a broken file
// whose directives are written with a prefix, and returns the diagnostics reported in the
// broken file.
//
// Takes fixture (*Fixture) which supplies the dialect and its base migration.
// Takes source (brokenSource) which is the broken file.
// Takes prefix (string) which is the directive prefix to write.
//
// Returns []string which holds each of the file's diagnostics, rendered whole.
// Returns error when generation fails fatally.
func brokenSourceDiagnostics(ctx context.Context, fixture *Fixture, source brokenSource, prefix string) ([]string, error) {
	content := strings.ReplaceAll(source.content, canonicalDirectivePrefix+".", prefix+".")
	migrations := map[string]string{baseMigrationFile: fixture.BaseMigration}
	queries := map[string]string{}
	filename := brokenQueryFile
	if source.migration {
		filename = brokenMigrationFile
		migrations[brokenMigrationFile] = content
	} else {
		queries[brokenQueryFile] = content
	}
	_, result, err := runGenerate(ctx, fixture.NewDialect(), migrations, queries)
	if err != nil {
		return nil, err
	}
	var rendered []string
	for index := range result.Diagnostics {
		diagnostic := &result.Diagnostics[index]
		if !strings.HasSuffix(diagnostic.Filename, filename) {
			continue
		}
		rendered = append(rendered, fmt.Sprintf("%s %s %s:%d:%d-%d:%d %s", diagnostic.Code, diagnostic.Severity, diagnostic.Filename,
			diagnostic.Line, diagnostic.Column, diagnostic.EndLine, diagnostic.EndColumn, diagnostic.Message))
	}
	return rendered, nil
}

// reportsParseErrorOnLine reports whether rendered diagnostics hold a parse error on a
// line.
//
// Takes rendered ([]string) which holds the diagnostics as brokenSourceDiagnostics
// renders them.
// Takes line (int) which is the line.
//
// Returns bool which is true when one of them is a parse error on the line.
func reportsParseErrorOnLine(rendered []string, line int) bool {
	prefix := fmt.Sprintf("%s %s ", diag.CodeParseError, diag.SeverityError)
	position := fmt.Sprintf(":%d:", line)
	return slices.ContainsFunc(rendered, func(diagnostic string) bool {
		location, _, _ := strings.Cut(strings.TrimPrefix(diagnostic, prefix), " ")
		return strings.HasPrefix(diagnostic, prefix) && strings.Contains(location, position)
	})
}

// renderedList joins rendered diagnostics for a problem message.
//
// Takes rendered ([]string) which holds the diagnostics.
//
// Returns string which lists them.
func renderedList(rendered []string) string {
	return "[" + strings.Join(rendered, "; ") + "]"
}

// commentPrefix returns the dialect's line comment prefix, or the SQL default when Info
// declares none.
//
// Takes engine (dialect.DialectPort) which supplies the comment syntax.
//
// Returns string which is the prefix.
func commentPrefix(engine dialect.DialectPort) string {
	if prefix := engine.Info().Comments.LinePrefix; prefix != "" {
		return prefix
	}
	return ast.DefaultSQLCommentStyle().LinePrefix
}
