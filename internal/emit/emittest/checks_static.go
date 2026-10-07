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
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/testkit/knownfailure"
)

// staticChecks lists the checks that examine the generated code without running it.
var staticChecks = []check{
	{name: "GeneratesWithoutError", run: checkGeneratesWithoutError, toolchain: false},
	{name: "GeneratedFilesAreHeadered", run: checkFilesAreHeadered, toolchain: false},
	{name: "FileNamesAreSanitised", run: checkFileNamesAreSanitised, toolchain: false},
	{name: "OutputTypeChecks", run: checkOutputTypeChecks, toolchain: true},
	{name: "ParametersAreBound", run: checkParametersAreBound, toolchain: true},
	{name: "RowFieldsAreScanned", run: checkRowFieldsAreScanned, toolchain: true},
	{name: "ReservedParameterNames", run: reservedNamesCheck(caseReservedParameters), toolchain: true},
	{name: "ReservedQueryNames", run: reservedNamesCheck(caseReservedQueries), toolchain: true},
	{name: "ReservedTableNames", run: reservedNamesCheck(caseReservedTables), toolchain: true},
	{name: "DuplicateGroupedColumns", run: reservedNamesCheck(caseGroupedColumns), toolchain: true},
	{name: "PlaceholdersUseExecutionForm", run: checkPlaceholdersUseExecutionForm, toolchain: true},
	{name: "SQLBytesRoundTrip", run: checkSQLBytesRoundTrip, toolchain: true},
	{name: "DeclarationsAreUnique", run: reservedNamesCheck(caseTwoSortableFiles), toolchain: true},
	{name: "OutputIsDeterministic", run: checkOutputIsDeterministic, toolchain: false},
	{name: "OutputIgnoresQueryOrder", run: checkOutputIgnoresQueryOrder, toolchain: false},
	{name: "QuerierInterfaceListsEveryQueryMethod", run: checkQuerierInterface, toolchain: true},
	{name: "FieldsCarryTheTagsTheStyleAsks", run: checkFieldTags, toolchain: true},
	{name: "DescriptionsDocumentQueryMethods", run: checkDescriptionsDocumentMethods, toolchain: true},
	{name: "QueriesReadRowsIntoTheStructTheyName", run: checkRowTypes, toolchain: true},
}

// caseVisit is what a check does with one generated case.
type caseVisit func(name string, result *caseResult) []string

// overCases runs a visit over every case the dialect takes and gathers the problems. A
// case the dialect does not take is passed over; the check does not apply when the
// dialect takes none.
//
// Takes names ([]string) which are the cases to visit.
// Takes visit (caseVisit) which examines one case.
//
// Returns knownfailure.Outcome which holds every problem.
func (current *subject) overCases(ctx context.Context, names []string, visit caseVisit) knownfailure.Outcome {
	var problems, unusable []string
	for _, name := range names {
		result := current.generation(ctx, name)
		if result.unusable != "" {
			unusable = append(unusable, "case "+name+": "+result.unusable)
			continue
		}
		problems = append(problems, visit(name, result)...)
	}
	if len(unusable) == len(names) {
		return knownfailure.NotApplicable(strings.Join(unusable, "\n"))
	}
	return knownfailure.OutcomeOf(problems)
}

// generalCaseNames lists the cases every general check examines.
//
// Returns []string which holds their names.
func generalCaseNames() []string {
	var names []string
	for _, current := range corpusCases {
		if current.general {
			names = append(names, current.name)
		}
	}
	return names
}

// generatedCleanly reports why a case's generation cannot be examined: Generate failed,
// the emitter refused a query, or nothing was generated.
//
// Takes name (string) which is the case's name.
// Takes result (*caseResult) which is its generation.
// Takes refusable (bool) which is true when a refusal diagnostic is acceptable.
//
// Returns []string which holds the problem, or nothing when the generation produced
// files.
// Returns bool which is true when the generation produced files.
func generatedCleanly(name string, result *caseResult, refusable bool) ([]string, bool) {
	switch {
	case result.generateError != nil:
		return []string{fmt.Sprintf("case %s: Generate failed instead of reporting a diagnostic: %v", name, result.generateError)}, false
	case len(errorDiagnostics(result.generated.Diagnostics)) > 0 && refusable:
		return nil, false
	case len(errorDiagnostics(result.generated.Diagnostics)) > 0:
		return []string{fmt.Sprintf("case %s: the emitter refused queries every emitter must generate: %s", name,
			summarise(errorDiagnostics(result.generated.Diagnostics)))}, false
	case len(result.generated.Files) == 0:
		return []string{fmt.Sprintf("case %s: the generation produced no files", name)}, false
	default:
		return nil, true
	}
}

// checkGeneratesWithoutError checks that every general case generates, with a refusal
// reported as a diagnostic rather than an error.
//
// Takes current (*subject) which is the dialect under test.
//
// Returns knownfailure.Outcome which holds the problems.
func checkGeneratesWithoutError(ctx context.Context, current *subject) knownfailure.Outcome {
	return current.overCases(ctx, generalCaseNames(), func(name string, result *caseResult) []string {
		definition, _ := findCase(name)
		problems, _ := generatedCleanly(name, result, definition.refusable)
		return problems
	})
}

// checkFilesAreHeadered checks that each general case's files have distinct names, even
// ignoring case, that each Go file starts with the header, and that no name is one Go
// treats specially.
//
// Takes current (*subject) which is the dialect under test.
//
// Returns knownfailure.Outcome which holds the problems.
func checkFilesAreHeadered(ctx context.Context, current *subject) knownfailure.Outcome {
	return current.overCases(ctx, generalCaseNames(), func(name string, result *caseResult) []string {
		if _, ok := generatedCleanly(name, result, true); !ok {
			return nil
		}
		return fileProblems(casePrefix+name, result.generated)
	})
}

// fileProblems reports a generation's files that share a name, even ignoring case, Go
// files that do not start with the header, and names Go treats specially.
//
// Takes name (string) which names the generation in messages.
// Takes generated (*app.Result) which is the generation.
//
// Returns []string which describes each problem.
func fileProblems(name string, generated *app.Result) []string {
	var problems []string
	seen := make(map[string]string)
	for _, file := range generated.Files {
		folded := strings.ToLower(file.Name)
		if earlier, duplicate := seen[folded]; duplicate {
			problems = append(problems, fmt.Sprintf("%s: two files are named %s and %s", name, earlier, file.Name))
		}
		seen[folded] = file.Name
		if strings.HasSuffix(file.Name, goFileExtension) && !bytes.HasPrefix(file.Content, []byte(generated.Header+"\n")) {
			problems = append(problems, fmt.Sprintf("%s: %s does not start with the header %q", name, file.Name, generated.Header))
		}
		if reason := fileNameProblem(file.Name); reason != "" {
			problems = append(problems, fmt.Sprintf("%s: the generated file %s %s", name, file.Name, reason))
		}
	}
	return problems
}

// checkFileNamesAreSanitised checks that query files whose names Go reads as build
// constraints, or ignores, still give compiled files holding their methods.
//
// Takes current (*subject) which is the dialect under test.
//
// Returns knownfailure.Outcome which holds the problems.
func checkFileNamesAreSanitised(ctx context.Context, current *subject) knownfailure.Outcome {
	return current.overCases(ctx, []string{caseFileNames}, func(name string, result *caseResult) []string {
		problems, ok := generatedCleanly(name, result, false)
		if !ok {
			return problems
		}
		return fileProblems(casePrefix+name, result.generated)
	})
}

// checkOutputTypeChecks checks that every general case's output type-checks.
//
// Takes current (*subject) which is the dialect under test.
//
// Returns knownfailure.Outcome which holds the problems.
func checkOutputTypeChecks(ctx context.Context, current *subject) knownfailure.Outcome {
	return current.overCases(ctx, generalCaseNames(), func(name string, result *caseResult) []string {
		if _, ok := generatedCleanly(name, result, true); !ok {
			return nil
		}
		return current.checkedOutput(ctx, name).typeProblems(name)
	})
}

// checkParametersAreBound checks that every method uses each of its arguments and reads
// every field of its parameter structs, over the general cases and the cases whose
// parameter names fold to one Go name.
//
// Takes current (*subject) which is the dialect under test.
//
// Returns knownfailure.Outcome which holds the problems.
func checkParametersAreBound(ctx context.Context, current *subject) knownfailure.Outcome {
	names := append(generalCaseNames(), caseFolded, caseFoldedSortable, caseFoldedSlice, caseFoldedBatch)
	return current.overCases(ctx, names, func(name string, result *caseResult) []string {
		definition, _ := findCase(name)
		problems, ok := generatedCleanly(name, result, definition.refusable)
		if !ok {
			return problemsUnlessGeneral(definition, problems)
		}
		checked := current.checkedOutput(ctx, name)
		if typeProblems := checked.typeProblems(name); len(typeProblems) > 0 && !definition.general {
			return typeProblems
		}
		return prefixed("case "+name+": ", bindingProblems(checked))
	})
}

// checkRowFieldsAreScanned checks that every field of every row a general case's methods
// return is filled.
//
// Takes current (*subject) which is the dialect under test.
//
// Returns knownfailure.Outcome which holds the problems.
func checkRowFieldsAreScanned(ctx context.Context, current *subject) knownfailure.Outcome {
	return current.overCases(ctx, generalCaseNames(), func(name string, result *caseResult) []string {
		if _, ok := generatedCleanly(name, result, true); !ok {
			return nil
		}
		return prefixed("case "+name+": ", rowProblems(current.checkedOutput(ctx, name)))
	})
}

// reservedNamesCheck returns a check that one case generates, type-checks and binds every
// parameter and row field: a case whose names collide with what the generated code
// declares, or one that repeats a declaration across files.
//
// Takes caseName (string) which is the case.
//
// Returns func(context.Context, *subject) knownfailure.Outcome which runs the check.
func reservedNamesCheck(caseName string) func(context.Context, *subject) knownfailure.Outcome {
	return func(ctx context.Context, current *subject) knownfailure.Outcome {
		return current.overCases(ctx, []string{caseName}, func(name string, result *caseResult) []string {
			problems, ok := generatedCleanly(name, result, false)
			if !ok {
				return problems
			}
			checked := current.checkedOutput(ctx, name)
			if typeProblems := checked.typeProblems(name); len(typeProblems) > 0 {
				return typeProblems
			}
			return prefixed("case "+name+": ", append(bindingProblems(checked), rowProblems(checked)...))
		})
	}
}

// checkPlaceholdersUseExecutionForm checks that the SQL constants of every general case
// write each placeholder in the dialect's execution form, or its positional form, and
// never mix them.
//
// Takes current (*subject) which is the dialect under test.
//
// Returns knownfailure.Outcome which holds the problems.
func checkPlaceholdersUseExecutionForm(ctx context.Context, current *subject) knownfailure.Outcome {
	return current.overCases(ctx, generalCaseNames(), func(name string, result *caseResult) []string {
		if _, ok := generatedCleanly(name, result, true); !ok {
			return nil
		}
		return prefixed("case "+name+": ", placeholderProblems(current.checkedOutput(ctx, name), &current.info))
	})
}

// checkSQLBytesRoundTrip checks that a SQL literal holding a byte order mark, a NUL, a
// carriage return, a backtick or invalid UTF-8 reaches a SQL constant unchanged.
//
// Takes current (*subject) which is the dialect under test.
//
// Returns knownfailure.Outcome which holds the problems.
func checkSQLBytesRoundTrip(ctx context.Context, current *subject) knownfailure.Outcome {
	names := make([]string, 0, len(byteClasses))
	for _, class := range byteClasses {
		names = append(names, byteCaseName(class))
	}
	return current.overCases(ctx, names, func(name string, result *caseResult) []string {
		problems, ok := generatedCleanly(name, result, false)
		if !ok {
			return problems
		}
		checked := current.checkedOutput(ctx, name)
		if len(checked.problems) > 0 {
			return checked.typeProblems(name)
		}
		index := slices.IndexFunc(byteClasses, func(class byteClass) bool { return byteCaseName(class) == name })
		literal := "'" + byteClasses[index].content + "'"
		if !slices.ContainsFunc(stringConstants(checked), func(value string) bool { return strings.Contains(value, literal) }) {
			return []string{fmt.Sprintf("case %s: no SQL constant holds the literal %q unchanged", name, literal)}
		}
		return nil
	})
}

// checkOutputIsDeterministic checks that generating every general case a second time
// gives the same files, byte for byte.
//
// Takes current (*subject) which is the dialect under test.
//
// Returns knownfailure.Outcome which holds the problems.
func checkOutputIsDeterministic(ctx context.Context, current *subject) knownfailure.Outcome {
	return current.overCases(ctx, generalCaseNames(), func(name string, result *caseResult) []string {
		return sameOutput(name, "generating a second time", result, result.again, result.againError)
	})
}

// checkOutputIgnoresQueryOrder checks that every general case generates the same files,
// byte for byte, when each file's queries are written in reverse order.
//
// Takes current (*subject) which is the dialect under test.
//
// Returns knownfailure.Outcome which holds the problems.
func checkOutputIgnoresQueryOrder(ctx context.Context, current *subject) knownfailure.Outcome {
	return current.overCases(ctx, generalCaseNames(), func(name string, result *caseResult) []string {
		return sameOutput(name, "generating with the queries reversed", result, result.reversed, result.reversedError)
	})
}

// sameOutput reports how a further generation of a case differs from the first.
//
// Takes name (string) which is the case's name.
// Takes how (string) which says how the further generation was made.
// Takes result (*caseResult) which holds the first generation.
// Takes further (*app.Result) which is the further generation, or nil.
// Takes furtherError (error) which is what the further generation returned.
//
// Returns []string which holds the problem, or nothing.
func sameOutput(name, how string, result *caseResult, further *app.Result, furtherError error) []string {
	if _, ok := generatedCleanly(name, result, true); !ok {
		return nil
	}
	if furtherError != nil || further == nil {
		return []string{fmt.Sprintf("case %s: %s failed: %v", name, how, furtherError)}
	}
	if difference := firstDifference(result.generated.Files, further.Files); difference != "" {
		return []string{fmt.Sprintf("case %s: %s changed the output: %s", name, how, difference)}
	}
	return nil
}

// problemsUnlessGeneral passes a generation problem on for a case only one check
// examines, and drops it for a general case, which GeneratesWithoutError reports.
//
// Takes definition (corpusCase) which is the case.
// Takes problems ([]string) which describe why it did not generate.
//
// Returns []string which holds the problems to report.
func problemsUnlessGeneral(definition corpusCase, problems []string) []string {
	if definition.general {
		return nil
	}
	return problems
}

// prefixed puts a prefix before each problem.
//
// Takes prefix (string) which is the prefix.
// Takes problems ([]string) which are the problems.
//
// Returns []string which holds the prefixed problems.
func prefixed(prefix string, problems []string) []string {
	for index := range problems {
		problems[index] = prefix + problems[index]
	}
	return problems
}
