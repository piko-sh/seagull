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
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/directive/syntax"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

const (
	// fuzzPackageName is the Go package fuzz generation writes.
	fuzzPackageName = "generated"

	// fuzzQueryDirective is the query header the structured models write.
	fuzzQueryDirective = "-- sgl.query(name: FuzzQuery, command: many, dynamic: runtime)"

	// fuzzQueryDirectiveSpelling is the same header in another accepted spelling.
	fuzzQueryDirectiveSpelling = "-- seagull.query( name:FuzzQuery,command:many,dynamic:runtime )"
)

// structuredSpanChecks lists the span assertions every structured model must pass.
var structuredSpanChecks = []func(*spanCorpus, spanSubject) []string{
	placeholderSpanProblems, unboundPlaceholderProblems, clauseSpanProblems, wholeClauseProblems,
	projectionItemProblems, inListSpanProblems, valuesTupleProblems,
}

// FuzzStructuredQuery checks valid models against independent expectations.
//
// Takes f (*testing.F) which registers seeds and failures.
// Takes newDialect (func() dialect.DialectPort) which creates an isolated engine.
func FuzzStructuredQuery(f *testing.F, newDialect func() dialect.DialectPort) {
	f.Helper()
	name := newDialect().Info().Name
	for _, seed := range slices.Concat(fuzzcase.Seeds(name), fuzzcase.ScenarioSeeds(name), fuzzcase.HistorySeeds(name),
		fuzzcase.CommandSeeds(name), fuzzcase.RejectionSeeds()) {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if scenario, ok := fuzzcase.DecodeScenario(name, data); ok {
			checkScenarioGeneration(t, newDialect, scenario)
			return
		}
		if history, ok := fuzzcase.DecodeHistory(name, data); ok {
			checkScenarioGeneration(t, newDialect, history.Direct)
			scenario := history.Direct
			scenario.Schema = append(append([]string(nil), history.Initial...), history.Steps...)
			checkScenarioGeneration(t, newDialect, scenario)
			return
		}
		if commands, ok := fuzzcase.DecodeCommands(name, data); ok {
			checkScenarioGeneration(t, newDialect, commands.Scenario)
			return
		}
		if rejected, ok := fuzzcase.DecodeRejection(name, data); ok {
			checkStructuredRejection(t, newDialect, rejected)
			return
		}
		model, ok := fuzzcase.Decode(name, data)
		if !ok {
			t.Skip("unsupported model version, shape or size")
		}
		checkStructuredQuery(t, newDialect, model)
	})
}

// checkStructuredQuery checks analysis, source spans and metamorphic equivalence.
//
// Takes t (*testing.T) which reports problems.
// Takes newDialect (func() dialect.DialectPort) which creates an engine per generation.
// Takes model (fuzzcase.Model) which supplies SQL and independent expectations.
func checkStructuredQuery(t *testing.T, newDialect func() dialect.DialectPort, model fuzzcase.Model) {
	t.Helper()
	ctx, cancel := fuzzContext(t)
	defer cancel()
	query := model.Query()
	details := structuredGeneration(ctx, t, newDialect(), model, query.SQL)
	if problems := structuredResultProblems(details, query); len(problems) > 0 {
		t.Fatalf("positive model failed its structural oracle: shape %d\n%s\n%s", model.Shape, strings.Join(problems, "\n"), query.SQL)
	}
	for _, problem := range countDiagnosticProblems(model, details.Result.Diagnostics) {
		t.Errorf("%s\n%s", problem, query.SQL)
	}
	analysed := details.Report.Queries[0]
	engine := newDialect()
	info := engine.Info()
	for _, problem := range templateProblems(&info, "structured", analysed) {
		t.Error(problem)
	}
	checkStructuredSpans(ctx, t, engine, model, query.SQL)
	transformed := transformFuzzSQL(query.SQL)
	other := structuredGeneration(ctx, t, newDialect(), model, transformed)
	if len(other.Report.Queries) != 1 || !sameQueryMeaning(analysed, other.Report.Queries[0]) {
		t.Fatalf("safe SQL transformation changed analysis\noriginal: %s\ntransformed: %s", query.SQL, transformed)
	}
	checkStructuredVariants(ctx, t, newDialect, model, analysed)
	checkStructuredFormatting(ctx, t, newDialect, model, analysed)
	checkIndependentQueryOrder(ctx, t, newDialect, model)
}

// checkStructuredFormatting checks that formatting and directive spelling keep the
// meaning.
//
// Takes t (*testing.T) which reports changed SQL or meaning.
// Takes newDialect (func() dialect.DialectPort) which creates fresh engines.
// Takes model (fuzzcase.Model) which supplies the SQL.
// Takes analysed (*sqlmodel.AnalysedQuery) which is the original analysis.
func checkStructuredFormatting(ctx context.Context, t *testing.T, newDialect func() dialect.DialectPort, model fuzzcase.Model, analysed *sqlmodel.AnalysedQuery) {
	t.Helper()
	info := newDialect().Info()
	source := model.Query().SQL
	lexicon := syntax.Lexicon{LinePrefix: info.Comments.LinePrefix, Rules: info.Lexical}
	formatted := syntax.FormatSource(source, lexicon, namespace.Default())
	if syntax.StripDirectives(formatted, lexicon, namespace.Default()) != syntax.StripDirectives(source, lexicon, namespace.Default()) {
		t.Fatal("formatting changed SQL outside directives")
	}
	spelling := strings.Replace(source, fuzzQueryDirective, fuzzQueryDirectiveSpelling, 1)
	other := structuredGeneration(ctx, t, newDialect(), model, formatFuzzSource(&info, spelling, namespace.Default()))
	if len(other.Report.Queries) != 1 || !sameQueryMeaning(analysed, other.Report.Queries[0]) {
		t.Fatal("directive spelling and formatting changed query meaning")
	}
}

// checkStructuredSpans checks every structural assertion on one model.
//
// Takes t (*testing.T) which reports invalid spans.
// Takes engine (dialect.DialectPort) which analyses the SQL.
// Takes model (fuzzcase.Model) which identifies the independent case.
// Takes source (string) which contains its SQL.
func checkStructuredSpans(ctx context.Context, t *testing.T, engine dialect.DialectPort, model fuzzcase.Model, source string) {
	t.Helper()
	info := engine.Info()
	subjects, err := analyseSpanSubjects(ctx, engine, info.Lexical, "structured", source)
	if err != nil {
		t.Fatalf("positive model failed raw analysis: %v\n%s", err, source)
	}
	corpus := &spanCorpus{info: info, subjects: nil, problems: nil}
	for _, subject := range subjects {
		for _, check := range structuredSpanChecks {
			for _, problem := range check(corpus, subject) {
				t.Errorf("shape %d: %s\n%s", model.Shape, problem, source)
			}
		}
	}
}

// checkStructuredVariants compares aliases and expression grouping independently.
//
// Takes t (*testing.T) which reports changed semantics.
// Takes newDialect (func() dialect.DialectPort) which creates fresh engines.
// Takes model (fuzzcase.Model) which supplies controlled transformations.
// Takes analysed (*sqlmodel.AnalysedQuery) which is the original analysis.
func checkStructuredVariants(ctx context.Context, t *testing.T, newDialect func() dialect.DialectPort, model fuzzcase.Model, analysed *sqlmodel.AnalysedQuery) {
	t.Helper()
	info := newDialect().Info()
	for _, variant := range model.Variants() {
		other := structuredGeneration(ctx, t, newDialect(), model, variant.SQL)
		if problems := structuredResultProblems(other, variant); len(problems) > 0 {
			t.Fatalf("transformed positive model failed: %v\n%s", problems, variant.SQL)
		}
		if !sameTransformedQueryMeaning(analysed, other.Report.Queries[0]) {
			t.Logf("original parameters: %+v\ntransformed parameters: %+v", analysed.Parameters, other.Report.Queries[0].Parameters)
			t.Logf("original columns: %+v\ntransformed columns: %+v", analysed.OutputColumns, other.Report.Queries[0].OutputColumns)
			t.Fatalf("alias or parentheses transformation changed analysis\noriginal: %s\ntransformed: %s", model.Query().SQL, variant.SQL)
		}
		for _, problem := range templateProblems(&info, "transformed", other.Report.Queries[0]) {
			t.Error(problem)
		}
	}
}

// structuredGeneration requires a model to generate without error diagnostics.
//
// Takes t (*testing.T) which reports failures.
// Takes engine (dialect.DialectPort) which is fresh for this call.
// Takes model (fuzzcase.Model) which supplies the schema.
// Takes query (string) which is the query variant.
//
// Returns *app.DetailedResult which holds successful analysis and files.
func structuredGeneration(ctx context.Context, t *testing.T, engine dialect.DialectPort, model fuzzcase.Model, query string) *app.DetailedResult {
	t.Helper()
	return structuredFilesGeneration(ctx, t, engine, model, map[string]string{path.Join(queryDirectory, fuzzQueryFile): query})
}

// structuredFilesGeneration requires every supplied query file to generate successfully.
//
// Takes t (*testing.T) which reports failures.
// Takes engine (dialect.DialectPort) which is fresh for this call.
// Takes model (fuzzcase.Model) which supplies migrations.
// Takes queries (map[string]string) which retains query file boundaries.
//
// Returns *app.DetailedResult which holds successful analysis and output.
func structuredFilesGeneration(ctx context.Context, t *testing.T, engine dialect.DialectPort, model fuzzcase.Model, queries map[string]string) *app.DetailedResult {
	t.Helper()
	generator, err := app.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	files := fstest.MapFS{
		path.Join(migrationDirectory, fuzzMigrationFile): &fstest.MapFile{Data: []byte(strings.Join(model.Migrations(), "\n"))},
	}
	for name, query := range queries {
		files[name] = &fstest.MapFile{Data: []byte(query)}
	}
	details, err := generator.GenerateDetailed(ctx, fuzzInput(files))
	if err != nil {
		t.Fatalf("positive shape %d failed: %v\n%v", model.Shape, err, queries)
	}
	if err := details.Result.Err(); err != nil {
		t.Fatalf("positive shape %d reported errors: %v\n%v", model.Shape, err, queries)
	}
	return details
}

// checkIndependentQueryOrder compares queries after exchanging their file order.
//
// Takes t (*testing.T) which reports changed query meaning.
// Takes newDialect (func() dialect.DialectPort) which creates fresh engines.
// Takes model (fuzzcase.Model) which supplies independently named queries.
func checkIndependentQueryOrder(ctx context.Context, t *testing.T, newDialect func() dialect.DialectPort, model fuzzcase.Model) {
	t.Helper()
	query := model.Query()
	first := query.SQL
	second := strings.Replace(first, "name: FuzzQuery", "name: OtherQuery", 1)
	firstFile, secondFile := path.Join(queryDirectory, "a.sql"), path.Join(queryDirectory, "b.sql")
	before := structuredFilesGeneration(ctx, t, newDialect(), model, map[string]string{firstFile: first, secondFile: second})
	after := structuredFilesGeneration(ctx, t, newDialect(), model, map[string]string{firstFile: second, secondFile: first})
	if len(before.Report.Queries) != 2 || len(after.Report.Queries) != 2 || len(before.Result.Files) == 0 || len(after.Result.Files) == 0 {
		t.Fatal("independent query files did not reach their oracle")
	}
	changedByName := make(map[string]*sqlmodel.AnalysedQuery)
	for _, changed := range after.Report.Queries {
		changedByName[changed.Name] = changed
	}
	for _, original := range before.Report.Queries {
		changed, exists := changedByName[original.Name]
		if !exists {
			t.Fatalf("reordering lost query %s", original.Name)
		}
		if !sameQueryMeaning(original, changed) {
			t.Fatalf("reordering independent files changed query %s", original.Name)
		}
	}
}

// transformFuzzSQL inserts harmless comments and changes keyword case.
//
// Takes source (string) which is model-generated SQL.
//
// Returns string which has the same SQL meaning.
func transformFuzzSQL(source string) string {
	return strings.ReplaceAll(source, "SELECT ", "select /* ORDER BY nonsense LIMIT ? {x:Int64} */ ")
}

// sameQueryMeaning compares semantic fields unaffected by lexical changes.
//
// Takes left (*sqlmodel.AnalysedQuery) which is the original analysis.
// Takes right (*sqlmodel.AnalysedQuery) which is the transformed analysis.
//
// Returns bool which reports matching commands, types and bindings.
func sameQueryMeaning(left, right *sqlmodel.AnalysedQuery) bool {
	return left.Name == right.Name && left.Command == right.Command &&
		reflect.DeepEqual(left.Parameters, right.Parameters) && reflect.DeepEqual(left.OutputColumns, right.OutputColumns)
}

// sameTransformedQueryMeaning normalises only deliberately renamed local aliases.
//
// Takes left (*sqlmodel.AnalysedQuery) which is the original analysis.
// Takes right (*sqlmodel.AnalysedQuery) which is the transformed analysis.
//
// Returns bool which reports equivalent commands, outputs and parameters.
func sameTransformedQueryMeaning(left, right *sqlmodel.AnalysedQuery) bool {
	normalised := *right
	normalised.OutputColumns = append([]sqlmodel.OutputColumn(nil), right.OutputColumns...)
	for index := range normalised.OutputColumns {
		for _, field := range []*string{&normalised.OutputColumns[index].SourceQualifier, &normalised.OutputColumns[index].SourceTable} {
			switch *field {
			case "renamed_items":
				*field = "i"
			case "renamed_peers":
				*field = "p"
			}
		}
	}
	return sameQueryMeaning(left, &normalised)
}

// fuzzInput builds the generation input for one set of fuzz files.
//
// Takes files (fs.FS) which holds the migration and query files.
//
// Returns app.Input which reads the files from their usual directories.
func fuzzInput(files fs.FS) app.Input {
	return app.Input{
		Package:      fuzzPackageName,
		PackagePath:  "",
		Queries:      nil,
		Migrations:   files,
		MigrationDir: migrationDirectory,
		QueryDir:     queryDirectory,
	}
}

// fuzzContext bounds one input and crashes a worker that ignores cancellation.
//
// The crash is deliberate. A worker stuck in a loop that never reads its context cannot
// be stopped any other way, and the fuzzer records the crashing input.
//
// Takes t (*testing.T) which owns the input.
//
// Returns context.Context which expires with errFuzzInputDeadline after the input
// deadline.
// Returns context.CancelFunc which stops both timers.
//
// Panics in the watchdog callback if the input ignores cancellation.
func fuzzContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithTimeoutCause(t.Context(), fuzzInputDeadline, errFuzzInputDeadline)
	watchdog := time.AfterFunc(fuzzWatchdogDeadline, func() { panic("fuzz input ignored its context deadline: " + t.Name()) })
	return ctx, func() { watchdog.Stop(); cancel() }
}

// FuzzGenerateFiles fuzzes source boundaries and custom function configuration.
//
// Takes f (*testing.F) which registers seeds and failures.
// Takes newDialect (func() dialect.DialectPort) which creates an isolated engine.
// Takes directory (string) which holds golden cases.
func FuzzGenerateFiles(f *testing.F, newDialect func() dialect.DialectPort, directory string) {
	f.Helper()
	for _, bundle := range fuzzBundles(f, directory) {
		data, err := json.Marshal(bundle)
		if err != nil {
			f.Fatal(err)
		}
		if _, err := fuzzcase.DecodeBundle(data); err == nil {
			f.Add(data)
		}
	}
	f.Add([]byte(`{"version":1,"files":[{"name":"migrations/001_empty.up.sql","sql":""}]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		bundle, err := fuzzcase.DecodeBundle(data)
		if err != nil {
			t.Skip("invalid file envelope")
		}
		checkFuzzBundle(t, newDialect(), bundle)
	})
}

// fuzzBundles reads source files and functions without merging file boundaries.
//
// Takes t (testing.TB) which reports setup failures.
// Takes directory (string) which holds golden cases.
//
// Returns []fuzzcase.Bundle which holds sorted case files.
func fuzzBundles(t testing.TB, directory string) []fuzzcase.Bundle {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	var bundles []fuzzcase.Bundle
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		bundle := readFuzzBundle(t, filepath.Join(directory, entry.Name()))
		if len(bundle.Files) > 0 {
			bundles = append(bundles, bundle)
		}
	}
	if len(bundles) == 0 {
		t.Fatalf("no file bundles in %s", directory)
	}
	return bundles
}

// checkFuzzBundle generates a decoded bundle and validates successful templates.
//
// Takes t (*testing.T) which reports failures.
// Takes engine (dialect.DialectPort) which owns generation.
// Takes bundle (fuzzcase.Bundle) which supplies bounded files and configuration.
func checkFuzzBundle(t *testing.T, engine dialect.DialectPort, bundle fuzzcase.Bundle) {
	t.Helper()
	var functions []sqlmodel.CustomFunctionConfig
	if len(bundle.Functions) > 0 {
		if err := json.Unmarshal(bundle.Functions, &functions); err != nil {
			t.Skip("invalid function envelope")
		}
	}
	ctx, cancel := fuzzContext(t)
	defer cancel()
	generator, err := app.New(engine, app.WithCustomFunctions(functions...))
	if err != nil {
		if !errors.Is(err, fault.ErrInvalidConfig) {
			t.Fatal(err)
		}
		return
	}
	details, err := generator.GenerateDetailed(ctx, fuzzInput(bundle.FS()))
	if cause := context.Cause(ctx); cause != nil {
		t.Fatalf("file bundle stopped early: %v", cause)
	}
	if err != nil {
		if !errors.Is(err, fault.ErrLimit) && !errors.Is(err, fault.ErrMalformedSQL) {
			t.Fatal(err)
		}
		return
	}
	checkFuzzBundleResult(t, engine, details)
	sources := make(map[string]string, len(bundle.Files))
	for _, file := range bundle.Files {
		sources[file.Name] = file.SQL
	}
	for _, problem := range diagnosticLocationProblems(details.Result.Diagnostics, sources) {
		t.Error(problem)
	}
}

// readFuzzBundle reads one seed through a directory-confined filesystem.
//
// Takes t (testing.TB) which reports setup failures.
// Takes directory (string) which identifies the golden case.
//
// Returns fuzzcase.Bundle which preserves sorted source boundaries.
func readFuzzBundle(t testing.TB, directory string) fuzzcase.Bundle {
	t.Helper()
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	bundle := fuzzcase.Bundle{Version: fuzzcase.Version, Origin: directory, Files: nil, Functions: nil}
	files := root.FS()
	for _, subdirectory := range []string{migrationDirectory, queryDirectory} {
		names, err := fs.Glob(files, subdirectory+"/*.sql")
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range names {
			if subdirectory == migrationDirectory && !isUpMigration(name) {
				continue
			}
			content, err := fs.ReadFile(files, name)
			if err != nil {
				t.Fatal(err)
			}
			bundle.Files = append(bundle.Files, fuzzcase.File{Name: name, SQL: string(content)})
		}
	}
	bundle.Functions = readFuzzFunctions(t, files, directory)
	return bundle
}

// checkFuzzBundleResult checks diagnostics and successful template bindings.
//
// Takes t (*testing.T) which reports failures.
// Takes engine (dialect.DialectPort) which supplies lexical facts.
// Takes details (*app.DetailedResult) which holds pipeline output.
func checkFuzzBundleResult(t *testing.T, engine dialect.DialectPort, details *app.DetailedResult) {
	t.Helper()
	for _, diagnostic := range details.Result.Diagnostics {
		if diagnostic.Code == diag.CodeRecoveredPanic || diagnostic.Line < 1 {
			t.Fatalf("invalid diagnostic: %+v", diagnostic)
		}
	}
	info := engine.Info()
	for _, problem := range passingTemplateProblems(&info, "files", details.Report) {
		t.Error(problem)
	}
}

// readFuzzFunctions reads optional custom functions from a golden specification.
//
// Takes t (testing.TB) which reports malformed fixtures.
// Takes files (fs.FS) which confines file access to the case directory.
// Takes directory (string) which identifies the fixture in errors.
//
// Returns json.RawMessage which retains the custom function configuration.
func readFuzzFunctions(t testing.TB, files fs.FS, directory string) json.RawMessage {
	t.Helper()
	content, err := fs.ReadFile(files, "testspec.json")
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatal(err)
		}
		return nil
	}
	var spec struct {
		Functions json.RawMessage `json:"customFunctions"`
	}
	if err := json.Unmarshal(content, &spec); err != nil {
		t.Fatalf("%s: %v", directory, err)
	}
	return spec.Functions
}

// structuredResultProblems requires positive inputs to reach their intended output.
//
// Takes details (*app.DetailedResult) which contains one generation result.
// Takes query (fuzzcase.Query) which records independently expected widths and bindings.
//
// Returns []string which describes missing or inconsistent positive results.
func structuredResultProblems(details *app.DetailedResult, query fuzzcase.Query) []string {
	if details == nil || details.Report == nil || details.Result == nil {
		return []string{"positive model returned no detailed result"}
	}
	if len(details.Report.Queries) != 1 || details.Report.Queries[0] == nil || len(details.Result.Files) == 0 {
		return []string{"positive model returned no query or generated files"}
	}
	if err := details.Result.Err(); err != nil {
		return []string{"positive model returned error diagnostics: " + err.Error()}
	}
	analysed := details.Report.Queries[0]
	if len(analysed.OutputColumns) != query.Columns || len(analysed.Parameters) != len(query.Values) {
		return []string{fmt.Sprintf("expected %d columns and %d parameters, got %d and %d", query.Columns,
			len(query.Values), len(analysed.OutputColumns), len(analysed.Parameters))}
	}
	return nil
}
