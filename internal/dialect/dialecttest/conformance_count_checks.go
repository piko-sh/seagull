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
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// runtimeBuilderCommand is the command of a query generated as a runtime builder.
	runtimeBuilderCommand = "many, dynamic: runtime"
)

// setReturningQuery is a runtime builder query that places the fixture's set-returning
// expression where it adds rows.
type setReturningQuery struct {
	// name is the query's name.
	name string

	// format is the query's SQL, with a %s where the set-returning expression goes.
	format string

	// expression is the set-returning expression, or empty to skip the query.
	expression string
}

// checkSetReturningItemsWrapTheCount checks that counts wrap queries with set-returning
// calls and preserve those calls.
//
// Takes fixture (*Fixture) which supplies the dialect and its set-returning expressions.
//
// Returns checkOutcome which holds every problem found.
func checkSetReturningItemsWrapTheCount(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	if fixture.SetReturningItem == "" && fixture.SetReturningOrderBy == "" {
		return notApplicable("the dialect has no set-returning function in a select list or an ORDER BY")
	}
	queries := []setReturningQuery{
		{name: "ListElements", format: "SELECT id, %s AS element FROM items", expression: fixture.SetReturningItem},
		{name: "ListByElement", format: "SELECT id FROM items ORDER BY %s", expression: fixture.SetReturningOrderBy},
	}
	var problems []string
	for _, query := range queries {
		if query.expression == "" {
			continue
		}
		problems = append(problems, setReturningCountProblems(ctx, fixture, query)...)
	}
	return outcomeOf(problems)
}

// setReturningCountProblems generates one set-returning query and checks its count.
//
// Takes fixture (*Fixture) which supplies the dialect.
// Takes query (setReturningQuery) which is the query to generate.
//
// Returns []string which describes each problem found.
func setReturningCountProblems(ctx context.Context, fixture *Fixture, query setReturningQuery) []string {
	engine := fixture.NewDialect()
	sql := fmt.Sprintf(query.format, query.expression)
	emitter, result, err := runGenerate(ctx, engine,
		map[string]string{"001_items.up.sql": fixture.BaseMigration},
		map[string]string{"elements.sql": directiveLine(engine, query.name, runtimeBuilderCommand) + sql})
	if err != nil {
		return []string{fmt.Sprintf(generationFatalFormat, err)}
	}
	generated := findQuery(emitter.queries, query.name)
	if generated == nil || generated.CountTemplate == nil {
		return []string{fmt.Sprintf("%q must be generated as a runtime builder query with a count, got %s", sql,
			formatDiagnostics(result.Diagnostics))}
	}
	if !slices.ContainsFunc(result.Diagnostics, isCountWrapped) {
		return []string{fmt.Sprintf("the count of %q must wrap the query, which returns a row for each element of the set, and report %s; got %s",
			sql, diag.CodeCountSemanticsWrapped, formatDiagnostics(result.Diagnostics))}
	}
	if counted := templateText(generated.CountTemplate.Segments); !strings.Contains(counted, query.expression) {
		return []string{fmt.Sprintf("the count of %q must keep %s, which makes the query's rows, got %q", sql, query.expression, counted)}
	}
	return nil
}

// isCountWrapped reports whether a diagnostic says a runtime builder query's count wraps
// the query.
//
// Takes diagnostic (diag.SourceError) which is the diagnostic.
//
// Returns bool which is true for the count-wrapping diagnostic.
func isCountWrapped(diagnostic diag.SourceError) bool {
	return diagnostic.Code == diag.CodeCountSemanticsWrapped
}

// templateText returns the text a template's segments write, with the text inside each
// clause and column in its place and nothing for a placeholder.
//
// Takes segments ([]sqlmodel.SQLSegment) which are the template's segments.
//
// Returns string which is the text.
func templateText(segments []sqlmodel.SQLSegment) string {
	var builder strings.Builder
	writeTemplateText(&builder, segments)
	return builder.String()
}

// writeTemplateText writes the text of segments, and of the segments inside them, in
// order.
//
// Takes builder (*strings.Builder) which receives the text.
// Takes segments ([]sqlmodel.SQLSegment) which are the segments.
func writeTemplateText(builder *strings.Builder, segments []sqlmodel.SQLSegment) {
	for index := range segments {
		builder.WriteString(segments[index].Text)
		writeTemplateText(builder, segments[index].Children)
	}
}
