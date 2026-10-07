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
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"strings"
	"testing/fstest"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// expandedQueriesFile is the query file the star check writes the expanded queries to.
	expandedQueriesFile = "expanded.sql"
)

// expandedQuery is a query whose SQL names the columns its stars stand for.
type expandedQuery struct {
	// original is the query as the generator analysed it.
	original *sqlmodel.AnalysedQuery

	// sql is the query's SQL with its stars expanded.
	sql string
}

// checkExpandedStarsReadTheStarsColumns checks that expanding a star preserves output
// column names and types. Stars whose columns cannot be named individually are skipped.
//
// Takes fixture (*Fixture) which supplies the dialect and its corpora.
//
// Returns checkOutcome which holds every problem found.
func checkExpandedStarsReadTheStarsColumns(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	if fixture.Spans == nil || len(fixture.Spans.Corpora) == 0 {
		return notApplicable("the fixture sets no Spans corpora")
	}
	var problems []string
	checked := 0
	for _, files := range fixture.Spans.Corpora {
		for _, caseDirectory := range queryCaseDirectories(files) {
			caseProblems, expanded := checkCaseStarExpansions(ctx, fixture, files, caseDirectory)
			problems = append(problems, caseProblems...)
			checked += expanded
		}
	}
	if checked == 0 {
		return notApplicable("the span corpora hold no star the generated code names column by column")
	}
	return outcomeOf(problems)
}

// checkCaseStarExpansions analyses one corpus case, expands the stars of its queries as
// the generated code does, analyses the expanded queries against the same migrations and
// compares their output columns with the originals'.
//
// Takes fixture (*Fixture) which supplies the dialect.
// Takes files (fs.FS) which is the corpus.
// Takes caseDirectory (string) which is the case's directory in it.
//
// Returns []string which describes each problem found.
// Returns int which is how many queries were expanded and checked.
func checkCaseStarExpansions(ctx context.Context, fixture *Fixture, files fs.FS, caseDirectory string) ([]string, int) {
	caseFiles, err := fs.Sub(files, caseDirectory)
	if err != nil {
		return nil, 0
	}
	var specification templateSpec
	if content, readErr := fs.ReadFile(caseFiles, templateSpecFile); readErr == nil {
		_ = json.Unmarshal(content, &specification)
	}
	if specification.ShouldError {
		return nil, 0
	}
	report, err := vetCase(ctx, fixture.NewDialect(), specification, caseFiles)
	if err != nil {
		return nil, 0
	}
	info := new(fixture.NewDialect().Info())
	var expanded []expandedQuery
	for _, query := range report.Queries {
		if sql, expands := expandedSQL(info, query); expands {
			expanded = append(expanded, expandedQuery{original: query, sql: sql})
		}
	}
	if len(expanded) == 0 {
		return nil, 0
	}
	overlay, err := expandedCase(caseFiles, fixture.NewDialect(), expanded)
	if err != nil {
		return []string{fmt.Sprintf("%s: writing the expanded queries: %v", caseDirectory, err)}, 0
	}
	reanalysed, err := vetCase(ctx, fixture.NewDialect(), specification, overlay)
	if err != nil {
		return []string{fmt.Sprintf("%s: the expanded queries do not analyse: %v", caseDirectory, err)}, len(expanded)
	}
	return compareExpanded(caseDirectory, expanded, reanalysed), len(expanded)
}

// vetCase runs the generator's analysis over a corpus case.
//
// Takes engine (dialect.DialectPort) which is the dialect.
// Takes specification (templateSpec) which declares the case's custom functions.
// Takes caseFiles (fs.FS) which holds the case's migrations and queries.
//
// Returns *app.Report which holds the analysed queries.
// Returns error when the generator cannot be built or the case cannot be analysed.
func vetCase(ctx context.Context, engine dialect.DialectPort, specification templateSpec, caseFiles fs.FS) (*app.Report, error) {
	generator, err := app.New(engine, app.WithCustomFunctions(specification.CustomFunctions...), app.WithLogger(slog.New(slog.DiscardHandler)))
	if err != nil {
		return nil, err
	}
	return generator.Vet(ctx, app.Input{
		Package:      templatePackage,
		PackagePath:  "",
		Migrations:   caseFiles,
		Queries:      nil,
		MigrationDir: templateMigrationDirectory,
		QueryDir:     queryDirectoryName,
	})
}

// expandedSQL renders a query as the generated code does by default, its stars named
// column by column, when it holds a star the renderer expands.
//
// Takes info (*dialect.Info) which describes the dialect.
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
//
// Returns string which is the expanded SQL.
// Returns bool which is true when the query holds a star the renderer expands.
func expandedSQL(info *dialect.Info, query *sqlmodel.AnalysedQuery) (string, bool) {
	if !holdsExpandingStar(query.Template.Segments) {
		return "", false
	}
	renderer := &emitkit.Renderer{
		WrapColumn:   nil,
		Query:        query,
		Quote:        info.IdentifierQuote,
		Lists:        info.Lists,
		Form:         info.Placeholders.Execution,
		Placeholders: info.Placeholders,
		Omit:         0,
		ListMode:     emitkit.ListExpand,
		Prefix:       "",
		KeepStars:    false,
	}
	if info.Placeholders.BindsByName() {
		renderer.ListMode = emitkit.ListAsWritten
	}
	rendered, err := emitkit.Render(query.Template, renderer)
	if err != nil {
		return "", false
	}
	return rendered.SQL, true
}

// holdsExpandingStar reports whether segments hold a star the renderer expands unless it
// keeps stars: one not marked AsWritten whose every column has a name.
//
// Takes segments ([]sqlmodel.SQLSegment) which are the template's segments.
//
// Returns bool which is true when such a star is found.
func holdsExpandingStar(segments []sqlmodel.SQLSegment) bool {
	for index := range segments {
		segment := &segments[index]
		if segment.Kind == sqlmodel.SegmentStar && !segment.AsWritten && namesEveryColumn(segment.Star) {
			return true
		}
		if holdsExpandingStar(segment.Children) {
			return true
		}
	}
	return false
}

// namesEveryColumn reports whether a star names every column it stands for.
//
// Takes columns ([]sqlmodel.StarColumn) which are the star's columns.
//
// Returns bool which is true when the star has columns and each has a name.
func namesEveryColumn(columns []sqlmodel.StarColumn) bool {
	for index := range columns {
		if columns[index].Name == "" {
			return false
		}
	}
	return len(columns) > 0
}

// expandedCase returns a copy of a corpus case's migrations with one query file holding
// the expanded queries, each under its own name and the many command.
//
// Takes caseFiles (fs.FS) which holds the case.
// Takes engine (dialect.DialectPort) which supplies the comment syntax of the headers.
// Takes expanded ([]expandedQuery) which are the expanded queries.
//
// Returns fs.FS which holds the copy.
// Returns error when the migrations cannot be read.
func expandedCase(caseFiles fs.FS, engine dialect.DialectPort, expanded []expandedQuery) (fs.FS, error) {
	overlay := fstest.MapFS{
		templateMigrationDirectory: &fstest.MapFile{Mode: fs.ModeDir},
		queryDirectoryName:         &fstest.MapFile{Mode: fs.ModeDir},
	}
	err := fs.WalkDir(caseFiles, templateMigrationDirectory, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		content, readErr := fs.ReadFile(caseFiles, name)
		if readErr != nil {
			return readErr
		}
		overlay[name] = &fstest.MapFile{Data: content}
		return nil
	})
	if err != nil {
		return nil, err
	}
	var queries strings.Builder
	for _, query := range expanded {
		queries.WriteString(directiveLine(engine, query.original.Name, "many"))
		queries.WriteString(strings.TrimSpace(query.sql))
		queries.WriteString("\n\n")
	}
	overlay[path.Join(queryDirectoryName, expandedQueriesFile)] = &fstest.MapFile{Data: []byte(queries.String())}
	return overlay, nil
}

// compareExpanded reports each expanded query whose output columns differ from its
// original's in number, name or type.
//
// Takes caseDirectory (string) which names the case, for messages.
// Takes expanded ([]expandedQuery) which are the expanded queries.
// Takes reanalysed (*app.Report) which holds their analysis.
//
// Returns []string which describes each problem found.
func compareExpanded(caseDirectory string, expanded []expandedQuery, reanalysed *app.Report) []string {
	byName := make(map[string]*sqlmodel.AnalysedQuery, len(reanalysed.Queries))
	for _, query := range reanalysed.Queries {
		byName[query.Name] = query
	}
	var problems []string
	for _, query := range expanded {
		where := caseDirectory + " query " + query.original.Name
		again, found := byName[query.original.Name]
		if !found {
			problems = append(problems, fmt.Sprintf("%s: the expanded SQL %q does not analyse", where, query.sql))
			continue
		}
		if shape, againShape := columnShapes(query.original.OutputColumns), columnShapes(again.OutputColumns); shape != againShape {
			problems = append(problems, fmt.Sprintf("%s: the expanded SQL %q reads %s, but the stars read %s", where, query.sql, againShape, shape))
		}
	}
	return problems
}

// columnShapes describes output columns by name and type, in order.
//
// Takes columns ([]sqlmodel.OutputColumn) which are the columns.
//
// Returns string which describes them.
func columnShapes(columns []sqlmodel.OutputColumn) string {
	shapes := make([]string, 0, len(columns))
	for index := range columns {
		shapes = append(shapes, columns[index].Name+" "+columns[index].SQLType.Category.String()+" "+columns[index].SQLType.EngineName)
	}
	return "[" + strings.Join(shapes, ", ") + "]"
}
