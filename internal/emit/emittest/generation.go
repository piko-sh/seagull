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

package emittest

import (
	"context"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"
	"sync"
	"testing/fstest"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/panicguard"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// queryDirectory is the virtual directory the suite serves query files from.
	queryDirectory = "queries"

	// generatedPackage is the package name the suite generates into.
	generatedPackage = "store"

	// queryNamesFile is the query-names file the suite asks for.
	queryNamesFile = "query_names.go"

	// queryNamesFunction is the query-names function the suite asks for.
	queryNamesFunction = "QueryName"

	// corpusInterfaceName is the interface of the query methods the suite asks for.
	corpusInterfaceName = "Querier"

	// corpusTagKey is the struct tag the suite asks every field holding a column to carry.
	corpusTagKey = "db"

	// corpusTagValue is the value of the corpusTagKey tag: the column's name.
	corpusTagValue = "{name}"

	// probeFile is the query file a variant is analysed in.
	probeFile = "probe.sql"

	// droppedVariant marks a query left out of a case.
	droppedVariant = -1

	// generationOperation names a generation in the error a panic during it becomes.
	generationOperation = "generating the case"
)

// subject is one dialect under test: the suite, the dialect, and the generations the
// checks share, each made once.
type subject struct {
	// engine is the dialect.
	engine dialect.DialectPort

	// suite is the suite under test.
	suite *Suite

	// lane is the suite's runtime lane.
	lane *runtimeLane

	// cases holds the state of each corpus case by name.
	cases map[string]*caseState

	// corpora holds the generations of the suite's corpora for this dialect.
	corpora *corpusRun

	// info describes the dialect.
	info dialect.Info

	// syntax is how the corpus is written for the dialect.
	syntax dialectSyntax

	// engineMutex serialises use of engine, which need not be safe for concurrent use.
	engineMutex sync.Mutex
}

// newSubject prepares a dialect for the checks.
//
// Takes suite (*Suite) which is the suite under test.
// Takes engine (dialect.DialectPort) which is the dialect.
// Takes lane (*runtimeLane) which is the suite's runtime lane.
//
// Returns *subject which generates on demand.
func newSubject(suite *Suite, engine dialect.DialectPort, lane *runtimeLane) *subject {
	cases := make(map[string]*caseState)
	for _, current := range corpusCases {
		cases[current.name] = &caseState{result: nil, definition: current, generateOnce: sync.Once{}, checkOnce: sync.Once{}}
	}
	return &subject{
		suite:       suite,
		engine:      engine,
		lane:        lane,
		cases:       cases,
		corpora:     &corpusRun{once: sync.Once{}, cases: nil},
		syntax:      newDialectSyntax(engine),
		info:        engine.Info(),
		engineMutex: sync.Mutex{},
	}
}

// generation returns a case's generation, making it on first use. A panic in the emitter
// or the dialect becomes the generation's error, and its stack is logged once through the
// logger the context carries.
//
// Takes name (string) which is the case's name.
//
// Returns *caseResult which is the generation.
func (current *subject) generation(ctx context.Context, name string) *caseResult {
	state := current.cases[name]
	state.generateOnce.Do(func() {
		result, err := panicguard.RunValue(ctx, generationOperation, func() (*caseResult, error) {
			return current.generate(ctx, state.definition), nil
		})
		if err != nil {
			result = &caseResult{generated: nil, generateError: err, again: nil, againError: nil, reversed: nil,
				reversedError: nil, checked: nil, files: nil, dropped: nil, unusable: ""}
		}
		state.result = result
	})
	return state.result
}

// generate writes a case in the dialect's syntax, checks the dialect analyses it, and
// generates it with the emitter; a general case is generated twice more, once as it is
// and once with the queries reversed.
//
// Takes definition (corpusCase) which is the case.
//
// Returns *caseResult which is the generation.
//
// Concurrency: holds the engine mutex throughout generation.
func (current *subject) generate(ctx context.Context, definition corpusCase) *caseResult {
	current.engineMutex.Lock()
	defer current.engineMutex.Unlock()

	result := &caseResult{generated: nil, generateError: nil, again: nil, againError: nil, reversed: nil, reversedError: nil,
		checked: nil, files: nil, dropped: nil, unusable: ""}
	tables := definition.tables
	if tables == nil {
		tables = standardTables
	}
	catalogue := current.syntax.catalogue(tables)
	variants, dropped := current.chooseVariants(ctx, definition, catalogue)
	result.dropped = slices.Sorted(maps.Keys(dropped))
	files, err := current.syntax.renderCase(definition, variants, false)
	switch {
	case err != nil:
		result.unusable = err.Error()
		return result
	case len(files) == 0:
		result.unusable = "the dialect analyses none of the case's queries: " + strings.Join(slices.Sorted(maps.Values(dropped)), "; ")
		return result
	}
	result.files = files
	if diagnostics := current.vet(ctx, catalogue, files); len(diagnostics) > 0 {
		result.unusable = "the dialect does not analyse the case: " + summarise(diagnostics)
		return result
	}
	result.generated, result.generateError = current.run(ctx, catalogue, files)
	if definition.general {
		result.again, result.againError = current.run(ctx, catalogue, files)
		reversed, renderError := current.syntax.renderCase(definition, variants, true)
		if renderError == nil {
			result.reversed, result.reversedError = current.run(ctx, catalogue, reversed)
		}
	}
	return result
}

// chooseVariants picks, for each query, the first SQL variant the dialect analyses
// without an error, and leaves out a query the dialect analyses no variant of.
//
// Takes definition (corpusCase) which is the case.
// Takes catalogue (*sqlmodel.Catalogue) which is the case's schema.
//
// Returns map[string]int which maps a query's name to its variant, or to droppedVariant.
// Returns map[string]string which maps each query left out to why.
func (current *subject) chooseVariants(ctx context.Context, definition corpusCase, catalogue *sqlmodel.Catalogue) (map[string]int, map[string]string) {
	variants := make(map[string]int)
	dropped := make(map[string]string)
	for _, file := range definition.files {
		for index := range file.queries {
			entry := &file.queries[index]
			variant, reason := current.chooseVariant(ctx, entry, catalogue)
			variants[entry.name] = variant
			if variant == droppedVariant {
				dropped[entry.name] = entry.name + ": " + reason
			}
		}
	}
	return variants, dropped
}

// chooseVariant picks the first SQL variant of a query the dialect analyses without an
// error.
//
// Takes entry (*corpusQuery) which is the query.
// Takes catalogue (*sqlmodel.Catalogue) which is the case's schema.
//
// Returns int which is the variant, or droppedVariant when the dialect analyses none.
// Returns string which says why the last variant failed, when none is chosen.
func (current *subject) chooseVariant(ctx context.Context, entry *corpusQuery, catalogue *sqlmodel.Catalogue) (int, string) {
	reason := ""
	for variant := range entry.variants {
		text, err := current.syntax.renderQuery(entry, variant)
		if err != nil {
			reason = err.Error()
			continue
		}
		diagnostics := current.vet(ctx, catalogue, map[string]string{probeFile: text})
		if len(diagnostics) == 0 {
			return variant, ""
		}
		reason = summarise(diagnostics)
	}
	return droppedVariant, reason
}

// vet analyses query files without generating, and returns the error-severity
// diagnostics.
//
// Takes catalogue (*sqlmodel.Catalogue) which is the schema.
// Takes files (map[string]string) which maps query file names to content.
//
// Returns []diag.SourceError which holds the errors, or a single diagnostic describing a
// fatal failure.
func (current *subject) vet(ctx context.Context, catalogue *sqlmodel.Catalogue, files map[string]string) []diag.SourceError {
	generator, err := app.New(current.engine, app.WithSchemaSource(&corpusSchema{catalogue: catalogue}),
		app.WithPreset(corpusPreset()))
	if err != nil {
		return []diag.SourceError{fatalDiagnostic(err)}
	}
	report, err := generator.Vet(ctx, corpusInput(files))
	if err != nil {
		return []diag.SourceError{fatalDiagnostic(err)}
	}
	return errorDiagnostics(report.Diagnostics)
}

// run generates query files with a fresh emitter.
//
// Takes catalogue (*sqlmodel.Catalogue) which is the schema.
// Takes files (map[string]string) which maps query file names to content.
//
// Returns *app.Result which is the generation's result.
// Returns error which is what Generate returned.
func (current *subject) run(ctx context.Context, catalogue *sqlmodel.Catalogue, files map[string]string) (*app.Result, error) {
	generator, err := app.New(current.engine, app.WithEmitter(current.suite.New(new(current.info.Facts()))),
		app.WithSchemaSource(&corpusSchema{catalogue: catalogue}), app.WithPreset(corpusPreset()))
	if err != nil {
		return nil, fmt.Errorf("creating the generator: %w", err)
	}
	return generator.Generate(ctx, corpusInput(files))
}

// checkedOutput returns a case's type-checked output, checking it on first use, or nil
// when the case generated no files.
//
// Takes name (string) which is the case's name.
//
// Returns *checkedPackage which is the checked output, or nil.
func (current *subject) checkedOutput(ctx context.Context, name string) *checkedPackage {
	result := current.generation(ctx, name)
	state := current.cases[name]
	state.checkOnce.Do(func() {
		if result.generated != nil && len(result.generated.Files) > 0 {
			result.checked = typeCheck(result.generated.Files)
		}
	})
	return result.checked
}

// caseState holds one case's generation, made once.
type caseState struct {
	// result is the case's generation.
	result *caseResult

	// definition is the case.
	definition corpusCase

	// generateOnce guards result.
	generateOnce sync.Once

	// checkOnce guards result.checked.
	checkOnce sync.Once
}

// caseResult is what one generation of a case produced.
type caseResult struct {
	// generateError is the error Generate returned.
	generateError error

	// againError is the error the second generation returned.
	againError error

	// reversedError is the error the reversed generation returned.
	reversedError error

	// generated is the generation's result, or nil when Generate failed.
	generated *app.Result

	// again is a second generation of the same files, made for a general case.
	again *app.Result

	// reversed is a generation with each file's queries in reverse order, made for a general
	// case.
	reversed *app.Result

	// checked is the type-checked output, filled by checkedOutput.
	checked *checkedPackage

	// files holds the query files as written for the dialect.
	files map[string]string

	// unusable, when set, says why the dialect cannot take the case.
	unusable string

	// dropped lists the queries left out because the dialect analyses none of their
	// variants.
	dropped []string
}

// corpusPreset returns the corpus's generation preset, enabling query names, the querier
// interface, empty result slices, doc comments, JSON and database tags, and model reuse.
// These options give the style checks generated features to inspect.
//
// Returns app.Preset which is the preset.
func corpusPreset() app.Preset {
	chosen := app.DefaultPreset()
	chosen.QueryNames = app.QueryNames{File: queryNamesFile, Function: queryNamesFunction, Enabled: true}
	chosen.Interface = app.Interface{Name: corpusInterfaceName, Enabled: true}
	chosen.EmptySlices = true
	chosen.DocComments = true
	chosen.JSONTags = app.JSONTags{Case: app.JSONCaseAsWritten, Omit: false, OmitEmpty: true}
	chosen.StructTags = map[string]string{corpusTagKey: corpusTagValue}
	chosen.ReuseModels = true
	return chosen
}

// corpusInput serves query files to the generator.
//
// Takes files (map[string]string) which maps query file names to content.
//
// Returns app.Input which reads them.
func corpusInput(files map[string]string) app.Input {
	served := fstest.MapFS{queryDirectory: &fstest.MapFile{Mode: fs.ModeDir}}
	for name, content := range files {
		served[path.Join(queryDirectory, name)] = &fstest.MapFile{Data: []byte(content)}
	}
	return app.Input{Migrations: nil, Queries: served, Package: generatedPackage, PackagePath: "", MigrationDir: "", QueryDir: queryDirectory}
}

// errorDiagnostics returns the diagnostics with error severity.
//
// Takes diagnostics ([]diag.SourceError) which are a run's diagnostics.
//
// Returns []diag.SourceError which holds the errors.
func errorDiagnostics(diagnostics []diag.SourceError) []diag.SourceError {
	return slices.DeleteFunc(slices.Clone(diagnostics), func(diagnostic diag.SourceError) bool {
		return diagnostic.Severity != diag.SeverityError
	})
}

// fatalDiagnostic describes a failure to analyse at all as a diagnostic.
//
// Takes err (error) which is the failure.
//
// Returns diag.SourceError which describes it.
func fatalDiagnostic(err error) diag.SourceError {
	return diagbuild.Unlocated("", diag.SeverityError, err.Error())
}

// summarise writes diagnostics on one line.
//
// Takes diagnostics ([]diag.SourceError) which are the diagnostics.
//
// Returns string which lists each code, location and message.
func summarise(diagnostics []diag.SourceError) string {
	parts := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		parts = append(parts, fmt.Sprintf("%s %s:%d %s", diagnostic.Code, diagnostic.Filename, diagnostic.Line, diagnostic.Message))
	}
	return "[" + strings.Join(parts, "; ") + "]"
}
