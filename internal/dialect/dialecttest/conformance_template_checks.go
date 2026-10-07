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
	"slices"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/directive/syntax"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// templateSpecFile is the file a corpus case declares its custom functions in.
	templateSpecFile = "testspec.json"

	// templateMigrationDirectory is the directory a corpus case keeps its migrations in.
	templateMigrationDirectory = "migrations"

	// templatePackage is the package name the template check generates for.
	templatePackage = "generated"
)

// templateSpec is what the template check reads from a corpus case's testspec.json.
type templateSpec struct {
	// CustomFunctions declares functions the dialect does not know.
	CustomFunctions []sqlmodel.CustomFunctionConfig `json:"customFunctions"`

	// ShouldError marks a case whose generation fails as a whole.
	ShouldError bool `json:"shouldError"`
}

// checkTemplatesRenderTheKeptSQL checks that templates preserve SQL tokens and argument
// counts.
//
// Takes fixture (*Fixture) which supplies the dialect and its corpora.
//
// Returns checkOutcome which holds every problem found.
func checkTemplatesRenderTheKeptSQL(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	if fixture.Spans == nil || len(fixture.Spans.Corpora) == 0 {
		return notApplicable("the fixture sets no Spans corpora")
	}
	var problems []string
	checked := 0
	for _, files := range fixture.Spans.Corpora {
		for _, caseDirectory := range queryCaseDirectories(files) {
			caseProblems, queries := checkCaseTemplates(ctx, fixture, files, caseDirectory)
			problems = append(problems, caseProblems...)
			checked += queries
		}
	}
	if checked == 0 {
		problems = append(problems, "the span corpora hold no query the generator could analyse")
	}
	return outcomeOf(problems)
}

// queryCaseDirectories lists the directories of a corpus that hold a queries directory.
//
// Takes files (fs.FS) which is the corpus.
//
// Returns []string which holds the case directories, in walk order.
func queryCaseDirectories(files fs.FS) []string {
	var directories []string
	_ = fs.WalkDir(files, ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr == nil && entry.IsDir() && entry.Name() == queryDirectoryName {
			directories = append(directories, path.Dir(name))
		}
		return nil
	})
	return directories
}

// checkCaseTemplates analyses one corpus case and checks the template of each of its
// queries. A case the generator cannot analyse as a whole is skipped.
//
// Takes fixture (*Fixture) which supplies the dialect.
// Takes files (fs.FS) which is the corpus.
// Takes caseDirectory (string) which is the case's directory in it.
//
// Returns []string which describes each problem found.
// Returns int which is how many queries were checked.
func checkCaseTemplates(ctx context.Context, fixture *Fixture, files fs.FS, caseDirectory string) ([]string, int) {
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
	engine := fixture.NewDialect()
	generator, err := app.New(engine, app.WithCustomFunctions(specification.CustomFunctions...), app.WithLogger(slog.New(slog.DiscardHandler)))
	if err != nil {
		return []string{fmt.Sprintf("%s: creating the generator: %v", caseDirectory, err)}, 0
	}
	report, err := generator.Vet(ctx, app.Input{
		Package:      templatePackage,
		PackagePath:  "",
		Migrations:   caseFiles,
		Queries:      nil,
		MigrationDir: templateMigrationDirectory,
		QueryDir:     queryDirectoryName,
	})
	if err != nil {
		return nil, 0
	}
	info := new(engine.Info())
	var problems []string
	for _, query := range report.Queries {
		problems = append(problems, templateProblems(info, caseDirectory, query)...)
	}
	return problems, len(report.Queries)
}

// templateProblems checks one query's template: it renders in the executed form, binds as
// many arguments as it lists, and reads as the query's kept SQL does but for its
// placeholders.
//
// Takes info (*dialect.Info) which describes the dialect.
// Takes caseDirectory (string) which names the case, for messages.
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
//
// Returns []string which describes each problem found.
func templateProblems(info *dialect.Info, caseDirectory string, query *sqlmodel.AnalysedQuery) []string {
	where := caseDirectory + " query " + query.Name
	if len(query.Template.Segments) == 0 {
		return []string{where + " has no template"}
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
		KeepStars:    true,
	}
	if info.Placeholders.BindsByName() {
		renderer.ListMode = emitkit.ListAsWritten
	}
	rendered, err := emitkit.Render(query.Template, renderer)
	if err != nil {
		return []string{fmt.Sprintf("%s: the template does not render: %v", where, err)}
	}
	renderedTokens, renderedErr := significantTokens(info.Lexical, rendered.SQL)
	lexicon := syntax.Lexicon{LinePrefix: info.Comments.LinePrefix, Rules: info.Lexical}
	keptTokens, keptErr := significantTokens(info.Lexical, syntax.StripDirectives(query.SQL, lexicon, namespace.Default()))
	if renderedErr != nil || keptErr != nil {
		return []string{fmt.Sprintf("%s: the rendered or kept SQL does not read under Info.Lexical", where)}
	}
	var problems []string
	if bound := boundArguments(info.Placeholders.Execution, renderedTokens); bound != len(query.Template.Arguments) {
		problems = append(problems, fmt.Sprintf("%s: %q binds %d arguments, but the template lists %d", where, rendered.SQL, bound,
			len(query.Template.Arguments)))
	}
	if !slices.Equal(textTokens(renderedTokens), textTokens(keptTokens)) {
		problems = append(problems, fmt.Sprintf("%s: %q does not read as the kept SQL %q but for its placeholders", where, rendered.SQL,
			syntax.StripDirectives(query.SQL, lexicon, namespace.Default())))
	}
	return problems
}

// boundArguments counts the arguments SQL written in a form binds: one per placeholder
// for an anonymous form, and one per distinct placeholder otherwise.
//
// Takes form (dialect.PlaceholderSigil) which is the form the SQL is written in.
// Takes tokens ([]sqllex.Token) which are the SQL's significant tokens.
//
// Returns int which is the number of arguments.
func boundArguments(form dialect.PlaceholderSigil, tokens []sqllex.Token) int {
	distinct := make(map[string]bool)
	count := 0
	for _, token := range tokens {
		if token.Kind == sqllex.KindParameter {
			count++
			distinct[token.Raw] = true
		}
	}
	if form.Kind == dialect.PlaceholderAnonymous {
		return count
	}
	return len(distinct)
}

// textTokens returns the text of the tokens that are not placeholders.
//
// Takes tokens ([]sqllex.Token) which are significant tokens.
//
// Returns []string which holds their text, in order.
func textTokens(tokens []sqllex.Token) []string {
	texts := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if token.Kind != sqllex.KindParameter {
			texts = append(texts, token.Raw)
		}
	}
	return texts
}
