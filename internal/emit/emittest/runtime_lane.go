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
	"errors"
	"fmt"
	"go/types"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	// laneModule is the module path of the runtime lane's program.
	laneModule = "emittest.local/lane"

	// laneTimeout bounds building and running the runtime lane's program.
	laneTimeout = 10 * time.Minute

	// fallbackGoVersion is the language version the program's go.mod names when the
	// toolchain's version cannot be read.
	fallbackGoVersion = "1.23"

	// laneBinary is the name of the program the lane builds.
	laneBinary = "emittest-lane"

	// fileMode is the permission of the files the lane writes.
	fileMode fs.FileMode = 0o644

	// directoryMode is the permission of the directories the lane writes.
	directoryMode fs.FileMode = 0o755

	// constructorName is the generated package's constructor.
	constructorName = "New"

	// queriesTypeName is the type whose methods run the queries.
	queriesTypeName = "Queries"

	// casePrefix starts a message about one case.
	casePrefix = "case "
)

var (
	// errLaneTimeout is the cause of a runtime lane stopped for taking too long.
	errLaneTimeout = errors.New("emittest: the runtime lane took longer than its time limit")

	// goVersionPattern reads the language version from the toolchain's version.
	goVersionPattern = regexp.MustCompile(`^go([0-9]+\.[0-9]+)`)
)

// runtimeLane builds the runtime cases of every dialect into one program with the suite's
// harness, runs it once, and keeps what each call sent to the driver.
type runtimeLane struct {
	// harness is the suite's harness, or nil when the lane does not run.
	harness *Harness

	// directory is where the lane writes its program.
	directory string

	// failure, when set, says why the whole lane failed.
	failure string

	// subjects lists the dialects under test.
	subjects []*subject

	// packages lists every generated package the lane considered.
	packages []*lanePackage

	// once guards the lane's run.
	once sync.Once
}

// newRuntimeLane prepares the runtime lane of a suite; it runs on first use.
//
// Takes t (*testing.T) which reports test failures.
// Takes suite (*Suite) which is the suite.
//
// Returns *runtimeLane which is the lane.
func newRuntimeLane(t *testing.T, suite *Suite) *runtimeLane {
	t.Helper()

	lane := &runtimeLane{harness: suite.Harness, subjects: nil, packages: nil, directory: "", failure: "", once: sync.Once{}}
	if suite.Harness != nil && !testing.Short() {
		lane.directory = t.TempDir()
	}
	return lane
}

// result returns what the lane holds for one case of one dialect, running the lane on
// first use.
//
// Takes current (*subject) which is the dialect.
// Takes caseName (string) which is the case.
//
// Returns laneResult which holds the case's calls, or why it has none.
func (lane *runtimeLane) result(ctx context.Context, current *subject, caseName string) laneResult {
	if lane.harness == nil {
		return laneResult{unavailable: "the suite has no Harness, so the runtime lane does not run", failure: "", calls: nil}
	}
	lane.once.Do(func() { lane.run(ctx) })
	index := slices.IndexFunc(lane.packages, func(entry *lanePackage) bool {
		return entry.subject == current && entry.caseName == caseName
	})
	if index < 0 {
		return laneResult{unavailable: "case " + caseName + " is not a runtime case", failure: "", calls: nil}
	}
	entry := lane.packages[index]
	switch {
	case entry.unavailable != "":
		return laneResult{unavailable: entry.unavailable, failure: "", calls: nil}
	case entry.failure != "":
		return laneResult{unavailable: "", failure: entry.failure, calls: nil}
	case lane.failure != "":
		return laneResult{unavailable: "", failure: lane.failure, calls: nil}
	default:
		return laneResult{unavailable: "", failure: "", calls: entry.calls}
	}
}

// run collects the runtime cases of every dialect, builds them into the program and runs
// it.
func (lane *runtimeLane) run(ctx context.Context) {
	for _, current := range lane.subjects {
		for _, definition := range corpusCases {
			if definition.runtime {
				lane.packages = append(lane.packages, lane.collect(ctx, current, definition))
			}
		}
	}
	included := slices.DeleteFunc(slices.Clone(lane.packages), func(entry *lanePackage) bool { return entry.unavailable != "" })
	if len(included) == 0 {
		return
	}
	ctx, cancel := context.WithTimeoutCause(ctx, laneTimeout, errLaneTimeout)
	defer cancel()
	binary, built, failure := lane.build(ctx, included)
	if failure != "" || len(built) == 0 {
		lane.failure = failure
		return
	}
	lane.failure = lane.execute(ctx, binary, built)
}

// collect prepares one case of one dialect for the program, or records why it cannot be
// in it.
//
// Takes current (*subject) which is the dialect.
// Takes definition (corpusCase) which is the case.
//
// Returns *lanePackage which is the package.
func (lane *runtimeLane) collect(ctx context.Context, current *subject, definition corpusCase) *lanePackage {
	entry := &lanePackage{subject: current, checked: nil, caseName: definition.name,
		name: numberedName("p", len(lane.packages)), unavailable: "", failure: "", source: "", imports: make(map[string]bool), calls: nil}
	result := current.generation(ctx, definition.name)
	if result.unusable != "" {
		entry.unavailable = casePrefix + definition.name + ": " + result.unusable
		return entry
	}
	if problems, ok := generatedCleanly(definition.name, result, definition.refusable); !ok {
		entry.unavailable = casePrefix + definition.name + " generated no code to run " + strings.Join(problems, "; ")
		return entry
	}
	entry.checked = current.checkedOutput(ctx, definition.name)
	if len(entry.checked.problems) > 0 {
		entry.unavailable = casePrefix + definition.name + " does not type-check, which OutputTypeChecks reports"
		return entry
	}
	if missing := lane.missingModules(entry.checked); len(missing) > 0 {
		entry.unavailable = casePrefix + definition.name + " imports " + strings.Join(missing, ", ") + ", which the harness does not provide"
		return entry
	}
	entry.unavailable = entry.prepare(definition, result.dropped)
	return entry
}

// missingModules lists the imports of a package outside the standard library that no
// harness module provides.
//
// Takes checked (*checkedPackage) which is the package.
//
// Returns []string which holds the missing import paths, sorted.
func (lane *runtimeLane) missingModules(checked *checkedPackage) []string {
	var missing []string
	for importPath := range checked.external {
		provided := false
		for modulePath := range lane.harness.Modules {
			if importPath == modulePath || strings.HasPrefix(importPath, modulePath+"/") {
				provided = true
			}
		}
		if !provided {
			missing = append(missing, importPath)
		}
	}
	slices.Sort(missing)
	return missing
}

// writeFile writes one file of the program, making its directory.
//
// Takes name (string) which is the file's slash-separated path within the program.
// Takes content ([]byte) which is the file's content.
//
// Returns error when the file cannot be written, naming it.
func (lane *runtimeLane) writeFile(name string, content []byte) error {
	target := filepath.Join(lane.directory, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(target), directoryMode); err != nil {
		return fmt.Errorf("writing %s: %w", name, err)
	}
	if err := os.WriteFile(target, content, fileMode); err != nil {
		return fmt.Errorf("writing %s: %w", name, err)
	}
	return nil
}

// writeModule writes a harness module's files under the program.
//
// Takes directory (string) which is the slash-separated directory to write it in.
// Takes modulePath (string) which is the module's path.
// Takes files (fs.FS) which holds the module's files.
//
// Returns error when a file cannot be read or written, naming it.
func (lane *runtimeLane) writeModule(directory, modulePath string, files fs.FS) error {
	hasManifest := false
	err := fs.WalkDir(files, ".", func(name string, entry fs.DirEntry, walkError error) error {
		if walkError != nil {
			return fmt.Errorf("reading %s of the harness module %s: %w", name, modulePath, walkError)
		}
		if entry.IsDir() {
			return nil
		}
		content, readError := fs.ReadFile(files, name)
		if readError != nil {
			return fmt.Errorf("reading %s of the harness module %s: %w", name, modulePath, readError)
		}
		hasManifest = hasManifest || name == "go.mod"
		return lane.writeFile(path.Join(directory, name), content)
	})
	if err != nil || hasManifest {
		return err
	}
	return lane.writeFile(path.Join(directory, "go.mod"), []byte("module "+modulePath+"\n\ngo "+goVersion()+"\n"))
}

// lanePackage is one generated package of the runtime lane.
type lanePackage struct {
	// subject is the dialect the package was generated with.
	subject *subject

	// checked is the package type-checked.
	checked *checkedPackage

	// caseName names the case the package was generated from.
	caseName string

	// name is the package's directory and alias in the program.
	name string

	// unavailable, when set, says why the package is not in the program.
	unavailable string

	// failure, when set, says why the package was left out of the program: the compiler's
	// report of it.
	failure string

	// source is the program function that calls the package's methods.
	source string

	// imports holds the import paths the function needs beside the generated package.
	imports map[string]bool

	// calls lists the calls the program makes of the package's methods.
	calls []*laneCall
}

// prepare writes the program function that calls the package's methods, and lists its
// calls.
//
// Takes definition (corpusCase) which is the case.
// Takes dropped ([]string) which names the queries left out of the package.
//
// Returns string which says why the package cannot be called, or nothing.
func (entry *lanePackage) prepare(definition corpusCase, dropped []string) string {
	scope := entry.checked.types.Scope()
	constructor, ok := scope.Lookup(constructorName).(*types.Func)
	if !ok || constructor.Signature().Params().Len() != 1 {
		return "the package declares no New taking one connection"
	}
	queriesType, ok := scope.Lookup(queriesTypeName).(*types.TypeName)
	if !ok {
		return "the package declares no Queries type"
	}
	methods := types.NewMethodSet(types.NewPointer(queriesType.Type()))
	var builder strings.Builder
	builder.WriteString("queries := " + entry.name + ".New(Connect())\n")
	if names, ok := scope.Lookup(queryNamesFunction).(*types.Func); ok && names.Signature().Params().Len() == 1 {
		builder.WriteString("names := " + entry.name + "." + queryNamesFunction + "\n")
	} else {
		builder.WriteString("var names func(string) string\n")
	}
	counter := 0
	for _, called := range calledQueries(definition, dropped) {
		for _, label := range labelsOf(called.scenario) {
			source, ok := entry.call(methods, called, label, &counter)
			if !ok {
				return "the package has no method " + called.name
			}
			builder.WriteString(source)
		}
	}
	entry.source = builder.String()
	return ""
}

// call writes one call of a method and records it.
//
// Takes methods (*types.MethodSet) which is the Queries method set.
// Takes called (*corpusQuery) which is the query whose method is called.
// Takes label (string) which is the call's label.
// Takes counter (*int) which numbers the package's values.
//
// Returns string which is the program statement making the call.
// Returns bool which is false when the package has no such method.
func (entry *lanePackage) call(methods *types.MethodSet, called *corpusQuery, label string, counter *int) (string, bool) {
	name := called.name
	var method *types.Func
	for selection := range methods.Methods() {
		if function, ok := selection.Obj().(*types.Func); ok && function.Name() == name {
			method = function
		}
	}
	if method == nil {
		return "", false
	}
	current := &sampler{generated: entry.checked.types, qualifier: entry.qualifier, importAlias: entry.importAlias,
		counter: counter, expected: nil, length: sliceLength(label)}
	parameters := method.Signature().Params()
	values := make([]string, 0, parameters.Len())
	first := 0
	if takesContext(method) {
		first = 1
	}
	for index := first; index < parameters.Len(); index++ {
		values = append(values, current.value(parameters.At(index).Type(), parameters.At(index).Name()))
	}
	fold := func(column string) string { return entry.subject.info.Identifiers.Fold(column, false) }
	body := callSource(method, values, label, current, fold)
	expected := current.expected
	if label == labelCount {
		expected = slices.DeleteFunc(expected, func(value expectedValue) bool { return slices.Contains(called.uncounted, value.path) })
	}
	entry.calls = append(entry.calls, &laneCall{outcome: nil, query: name, label: label, expected: expected, slice: returnsRows(method, label)})
	return "emittestRun(encoder, " + strconv.Quote(entry.name) + ", " + strconv.Itoa(len(entry.calls)-1) +
		", names, func() error {\n" + body + "\n})\n", true
}

// qualifier names a package as the program imports it: the generated package by its
// alias, and any other by an import the program adds.
//
// Takes imported (*types.Package) which is the package.
//
// Returns string which is its name in the program.
func (entry *lanePackage) qualifier(imported *types.Package) string {
	if imported == entry.checked.types {
		return entry.name
	}
	return entry.importAlias(imported.Path())
}

// importAlias returns the alias the program imports a path under, and records that this
// package's function needs it. The alias depends only on the path, so the program
// declares each import once.
//
// Takes importPath (string) which is the path.
//
// Returns string which is the alias.
func (entry *lanePackage) importAlias(importPath string) string {
	entry.imports[importPath] = true
	return importAliasOf(importPath)
}

// laneCall is one call the program makes of a generated method.
type laneCall struct {
	// outcome is what the call sent to the driver, or nil when it never ran.
	outcome *laneOutcome

	// query names the query the method runs.
	query string

	// label names the call, such as plain or empty.
	label string

	// expected lists the values passed that must reach the driver.
	expected []expectedValue

	// slice is true when the call returns a slice of rows, as a many method and a runtime
	// builder's All do.
	slice bool
}

// laneOutcome is what one call sent to the driver, as the program reports it.
type laneOutcome struct {
	// Error is the error the call returned.
	Error string `json:"error"`

	// Panic is the value the call panicked with.
	Panic string `json:"panic"`

	// Package names the generated package the call was made of.
	Package string `json:"package"`

	// Statements lists the statements the driver received.
	Statements []laneStatement `json:"statements"`

	// Call numbers the call within its package.
	Call int `json:"call"`

	// QueryNames is true when the package declares the query-names function.
	QueryNames bool `json:"queryNames"`

	// NilSlice is true when the call returned a nil slice of rows.
	NilSlice bool `json:"nilSlice"`
}

// laneStatement is one statement the driver received.
type laneStatement struct {
	// SQL is the statement's text.
	SQL string `json:"sql"`

	// QueryName is what the package's query-names function returns for SQL.
	QueryName string `json:"queryName"`

	// Arguments lists the statement's arguments.
	Arguments []laneArgument `json:"arguments"`
}

// laneArgument is one argument the driver received.
type laneArgument struct {
	// Name is the argument's name, for a named argument.
	Name string `json:"name"`

	// Text is the argument's value as text, after any driver.Valuer.
	Text string `json:"text"`

	// Type is the Go type the generated code passed.
	Type string `json:"type"`

	// Bound is the Go type of the value after any driver.Valuer: the type the driver binds.
	Bound string `json:"bound"`
}

// laneResult is what the lane holds for one case of one dialect.
type laneResult struct {
	// unavailable, when set, says why the case is not in the lane.
	unavailable string

	// failure, when set, says why the lane failed.
	failure string

	// calls lists the case's calls.
	calls []*laneCall
}

// calledQueries lists the queries of a case the runtime lane calls: those with a scenario
// that the dialect did not leave out.
//
// Takes definition (corpusCase) which is the case.
// Takes dropped ([]string) which names the queries left out.
//
// Returns []*corpusQuery which holds the queries, in order.
func calledQueries(definition corpusCase, dropped []string) []*corpusQuery {
	var called []*corpusQuery
	for _, file := range definition.files {
		for index := range file.queries {
			entry := &file.queries[index]
			if entry.scenario != scenarioNone && !slices.Contains(dropped, entry.name) {
				called = append(called, entry)
			}
		}
	}
	return called
}

// importAliasOf returns the alias the program imports a path under.
//
// Takes importPath (string) which is the path.
//
// Returns string which is the alias.
func importAliasOf(importPath string) string {
	return "emittestImport" + strings.NewReplacer("/", "_", ".", "_", "-", "_").Replace(importPath)
}

// goVersion returns the language version the program's go.mod names.
//
// Returns string which is the version, such as 1.27.
func goVersion() string {
	if match := goVersionPattern.FindStringSubmatch(runtime.Version()); match != nil {
		return match[1]
	}
	return fallbackGoVersion
}
