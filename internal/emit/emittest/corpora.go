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
	"encoding/json"
	"fmt"
	"io/fs"
	"slices"
	"sync"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/panicguard"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/knownfailure"
)

const (
	// corpusSpecFile marks a directory of a corpus as a case and describes it.
	corpusSpecFile = "testspec.json"

	// corpusMigrations is the migration directory of a corpus case.
	corpusMigrations = "migrations"
)

// corpusChecks lists the checks over the suite's corpora.
var corpusChecks = []check{
	{name: "CorpusGenerates", run: checkCorpusGenerates, toolchain: false},
	{name: "CorpusFilesAreNamedSafely", run: checkCorpusFilesAreNamedSafely, toolchain: false},
	{name: "CorpusTypeChecks", run: corpusCheck(func(name string, checked *checkedPackage, _ *subject) []string {
		return checked.typeProblems(name)
	}), toolchain: true},
	{name: "CorpusParametersAreBound", run: corpusCheck(func(name string, checked *checkedPackage, _ *subject) []string {
		return prefixed(name+": ", bindingProblems(checked))
	}), toolchain: true},
	{name: "CorpusRowFieldsAreScanned", run: corpusCheck(func(name string, checked *checkedPackage, _ *subject) []string {
		return prefixed(name+": ", rowProblems(checked))
	}), toolchain: true},
	{name: "CorpusPlaceholdersUseExecutionForm", run: corpusCheck(func(name string, checked *checkedPackage, current *subject) []string {
		return prefixed(name+": ", placeholderProblems(checked, &current.info))
	}), toolchain: true},
	{name: "CorpusIsDeterministic", run: checkCorpusIsDeterministic, toolchain: false},
}

// corpusSpec is the part of a corpus case's testspec.json the suite reads.
type corpusSpec struct {
	// CustomFunctions declares functions the dialect does not know.
	CustomFunctions []sqlmodel.CustomFunctionConfig `json:"customFunctions"`

	// TypeOverrides maps SQL types or columns to Go types.
	TypeOverrides []sqlmodel.TypeOverride `json:"typeOverrides"`

	// ShouldError marks a case whose generation fails by design.
	ShouldError bool `json:"shouldError"`

	// Skip marks a case to leave out.
	Skip bool `json:"skip"`
}

// corpusRun holds the generations of a dialect's corpora, made once.
type corpusRun struct {
	// cases holds each case's generation.
	cases []*corpusCaseResult

	// once guards cases.
	once sync.Once
}

// corpusCaseResult is what the generations of one corpus case produced.
type corpusCaseResult struct {
	// generated is the first generation's result, or nil when Generate failed.
	generated *app.Result

	// generateError is the error the first generation returned.
	generateError error

	// repeated is the second generation's result.
	repeated *app.Result

	// repeatError is the error the second generation returned.
	repeatError error

	// checked is the type-checked output, filled on first use.
	checked *checkedPackage

	// name locates the case: the corpus's position and the case's directory.
	name string

	// checkOnce guards checked.
	checkOnce sync.Once
}

// generatedFiles reports whether a corpus case generated files to examine: Generate
// succeeded, no diagnostic has error severity, and files were written.
//
// Returns bool which is true when there is output to examine.
func (result *corpusCaseResult) generatedFiles() bool {
	return result.generateError == nil && result.generated != nil && result.generated.Err() == nil && len(result.generated.Files) > 0
}

// checkedOutput returns the case's type-checked output, checking it on first use.
//
// Returns *checkedPackage which is the checked output.
func (result *corpusCaseResult) checkedOutput() *checkedPackage {
	result.checkOnce.Do(func() {
		result.checked = typeCheck(result.generated.Files)
	})
	return result.checked
}

// corpusGenerations returns the generations of the dialect's corpora, making them on
// first use.
//
// Returns []*corpusCaseResult which holds each case's generations.
//
// Concurrency: sync.Once initialises the corpus cache; engine access is serialised.
func (current *subject) corpusGenerations(ctx context.Context) []*corpusCaseResult {
	current.corpora.once.Do(func() {
		current.engineMutex.Lock()
		defer current.engineMutex.Unlock()
		for index, corpus := range current.suite.Corpora {
			if corpus.Dialect == current.info.Name {
				current.corpora.cases = append(current.corpora.cases, current.generateCorpus(ctx, index, corpus)...)
			}
		}
	})
	return current.corpora.cases
}

// generateCorpus generates every case of one corpus twice. A panic in the emitter or the
// dialect becomes the generation's error, and its stack is logged once through the logger
// the context carries.
//
// Takes index (int) which is the corpus's position in the suite.
// Takes corpus (Corpus) which is the corpus.
//
// Returns []*corpusCaseResult which holds each case's generations.
func (current *subject) generateCorpus(ctx context.Context, index int, corpus Corpus) []*corpusCaseResult {
	entries, err := fs.ReadDir(corpus.Cases, ".")
	if err != nil {
		return []*corpusCaseResult{{name: fmt.Sprintf("corpus %d", index), generateError: err, generated: nil,
			repeated: nil, repeatError: nil, checked: nil, checkOnce: sync.Once{}}}
	}
	var results []*corpusCaseResult
	for _, entry := range entries {
		specification, ok := readCorpusSpec(corpus.Cases, entry)
		if !ok {
			continue
		}
		cases, subErr := fs.Sub(corpus.Cases, entry.Name())
		result := &corpusCaseResult{name: fmt.Sprintf("corpus %d case %s", index, entry.Name()), generated: nil,
			generateError: subErr, repeated: nil, repeatError: nil, checked: nil, checkOnce: sync.Once{}}
		if subErr == nil {
			generate := func() (*app.Result, error) { return current.generateCorpusCase(ctx, cases, specification) }
			result.generated, result.generateError = panicguard.RunValue(ctx, generationOperation, generate)
			result.repeated, result.repeatError = panicguard.RunValue(ctx, generationOperation, generate)
		}
		results = append(results, result)
	}
	return results
}

// generateCorpusCase generates one corpus case with a fresh emitter.
//
// Takes cases (fs.FS) which holds the case's directory.
// Takes specification (corpusSpec) which describes it.
//
// Returns *app.Result which is the generation's result.
// Returns error which is what Generate returned.
func (current *subject) generateCorpusCase(ctx context.Context, cases fs.FS, specification corpusSpec) (*app.Result, error) {
	generator, err := app.New(current.engine, app.WithEmitter(current.suite.New(new(current.info.Facts()))),
		app.WithCustomFunctions(specification.CustomFunctions...), app.WithTypeOverrides(specification.TypeOverrides...))
	if err != nil {
		return nil, fmt.Errorf("creating the generator: %w", err)
	}
	migrations := corpusMigrations
	if _, statErr := fs.Stat(cases, corpusMigrations); statErr != nil {
		migrations = ""
	}
	return generator.Generate(ctx, app.Input{Migrations: cases, Queries: nil, Package: generatedPackage,
		PackagePath: "", MigrationDir: migrations, QueryDir: queryDirectory})
}

// readCorpusSpec reads a corpus entry's testspec.json, and reports whether the entry is a
// case to generate.
//
// Takes cases (fs.FS) which holds the corpus.
// Takes entry (fs.DirEntry) which is the entry.
//
// Returns corpusSpec which describes the case.
// Returns bool which is false for an entry that is not a case, or a case to leave out.
func readCorpusSpec(cases fs.FS, entry fs.DirEntry) (corpusSpec, bool) {
	var specification corpusSpec
	if !entry.IsDir() {
		return specification, false
	}
	content, err := fs.ReadFile(cases, entry.Name()+"/"+corpusSpecFile)
	if err != nil || json.Unmarshal(content, &specification) != nil {
		return specification, false
	}
	if _, err := fs.Stat(cases, entry.Name()+"/"+queryDirectory); err != nil {
		return specification, false
	}
	return specification, !specification.ShouldError && !specification.Skip
}

// checkCorpusGenerates checks that no corpus case makes Generate fail: a query the
// emitter cannot generate is reported as a diagnostic.
//
// Takes current (*subject) which is the dialect under test.
//
// Returns knownfailure.Outcome which holds the problems.
func checkCorpusGenerates(ctx context.Context, current *subject) knownfailure.Outcome {
	cases := current.corpusGenerations(ctx)
	if len(cases) == 0 {
		return knownfailure.NotApplicable("the suite has no corpus for this dialect")
	}
	var problems []string
	for _, result := range cases {
		if result.generateError != nil {
			problems = append(problems, fmt.Sprintf("%s: Generate failed instead of reporting a diagnostic: %v", result.name, result.generateError))
		}
	}
	return knownfailure.OutcomeOf(problems)
}

// checkCorpusFilesAreNamedSafely checks that every corpus case's files have distinct
// names, even ignoring case, that each Go file starts with the header, and that no name
// is one Go treats specially.
//
// Takes current (*subject) which is the dialect under test.
//
// Returns knownfailure.Outcome which holds the problems.
func checkCorpusFilesAreNamedSafely(ctx context.Context, current *subject) knownfailure.Outcome {
	cases := slices.DeleteFunc(slices.Clone(current.corpusGenerations(ctx)), func(result *corpusCaseResult) bool { return !result.generatedFiles() })
	if len(cases) == 0 {
		return knownfailure.NotApplicable("the suite has no corpus case for this dialect that generates files")
	}
	var problems []string
	for _, result := range cases {
		problems = append(problems, fileProblems(result.name, result.generated)...)
	}
	return knownfailure.OutcomeOf(problems)
}

// corpusCheck returns a check that examines the type-checked output of every corpus case
// that generated files.
//
// Takes examine (func(string, *checkedPackage, *subject) []string) which examines one
// case's output.
//
// Returns func(context.Context, *subject) knownfailure.Outcome which runs the check.
func corpusCheck(examine func(name string, checked *checkedPackage, current *subject) []string) func(context.Context, *subject) knownfailure.Outcome {
	return func(ctx context.Context, current *subject) knownfailure.Outcome {
		cases := slices.DeleteFunc(slices.Clone(current.corpusGenerations(ctx)), func(result *corpusCaseResult) bool { return !result.generatedFiles() })
		if len(cases) == 0 {
			return knownfailure.NotApplicable("the suite has no corpus case for this dialect that generates files")
		}
		var problems []string
		for _, result := range cases {
			problems = append(problems, examine(result.name, result.checkedOutput(), current)...)
		}
		return knownfailure.OutcomeOf(problems)
	}
}

// checkCorpusIsDeterministic checks that generating every corpus case twice gives the
// same files, byte for byte.
//
// Takes current (*subject) which is the dialect under test.
//
// Returns knownfailure.Outcome which holds the problems.
func checkCorpusIsDeterministic(ctx context.Context, current *subject) knownfailure.Outcome {
	cases := slices.DeleteFunc(slices.Clone(current.corpusGenerations(ctx)), func(result *corpusCaseResult) bool { return !result.generatedFiles() })
	if len(cases) == 0 {
		return knownfailure.NotApplicable("the suite has no corpus case for this dialect that generates files")
	}
	var problems []string
	for _, result := range cases {
		if result.repeatError != nil || result.repeated == nil {
			problems = append(problems, fmt.Sprintf("%s: the second generation failed: %v", result.name, result.repeatError))
			continue
		}
		if difference := firstDifference(result.generated.Files, result.repeated.Files); difference != "" {
			problems = append(problems, fmt.Sprintf("%s: a second generation differs: %s", result.name, difference))
		}
	}
	return knownfailure.OutcomeOf(problems)
}
