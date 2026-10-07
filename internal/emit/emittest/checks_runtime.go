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
	"slices"
	"strings"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/testkit/knownfailure"
)

const (
	// queryItemsNotIn is the slice query that tests exclusion.
	queryItemsNotIn = "ItemsNotIn"

	// nilText is how the runtime lane writes a nil argument.
	nilText = "<nil>"

	// emptyArrayText is how the runtime lane writes an empty slice, or a driver's empty
	// array text.
	emptyArrayText = "[]"

	// emptyMapText is how a driver writes an empty array in braces.
	emptyMapText = "{}"

	// incrementedRank is the expression the batch row writes, which every row must keep.
	incrementedRank = " + 1"

	// noRowsText ends the error a stand-in reports for a row it does not find.
	noRowsText = "no rows in result set"

	// boundBytes is how the runtime lane names the type of a value bound as bytes.
	boundBytes = "[]uint8"
)

// runtimeChecks lists the checks over what the runtime lane's calls sent to the driver.
var runtimeChecks = []check{
	{name: "RuntimeArgumentsMatchPlaceholders", run: checkRuntimeArguments, toolchain: true},
	{name: "SliceArguments", run: checkSliceArguments, toolchain: true},
	{name: "EmptySliceSemantics", run: checkEmptySliceSemantics, toolchain: true},
	{name: "BatchRows", run: checkBatchRows, toolchain: true},
	{name: "BatchRejectsParametersOutsideValues", run: checkBatchRejectsParametersOutsideValues, toolchain: true},
	{name: "BatchUpsertsSendEachRowAlone", run: checkBatchUpsertsSendEachRowAlone, toolchain: true},
	{name: "CountBindsItsArguments", run: checkCountBindsItsArguments, toolchain: true},
	{name: "SortableOrderBy", run: checkSortableOrderBy, toolchain: true},
	{name: "RuntimeBuilderClauses", run: checkRuntimeBuilderClauses, toolchain: true},
	{name: "QueryNameResolves", run: checkQueryNameResolves, toolchain: true},
	{name: "EnumValuesReachTheDriver", run: checkEnumValuesReachTheDriver, toolchain: true},
	{name: "NestedEnumsConvertOrRefuse", run: checkNestedEnumsConvertOrRefuse, toolchain: true},
	{name: "OverrideTypesReachTheDriver", run: checkOverrideTypesReachTheDriver, toolchain: true},
	{name: "JSONParametersBindAsText", run: checkJSONParametersBindAsText, toolchain: true},
	{name: "JSONColumnsReadIntoTheirGoType", run: checkJSONColumnsReadIntoTheirGoType, toolchain: true},
	{name: "ManyReturnsAnEmptySlice", run: checkManyReturnsAnEmptySlice, toolchain: true},
}

// callVisit is what a check does with one call of the runtime lane.
type callVisit func(caseName string, call *laneCall) []string

// overCalls runs a visit over the calls with the given labels of every case the lane
// holds for the dialect. The check does not apply when the lane holds none of the cases.
//
// Takes caseNames ([]string) which are the cases.
// Takes labels ([]string) which are the calls to visit; nil visits every call.
// Takes visit (callVisit) which examines one call.
//
// Returns knownfailure.Outcome which holds every problem.
func (current *subject) overCalls(ctx context.Context, caseNames, labels []string, visit callVisit) knownfailure.Outcome {
	var problems, unavailable []string
	visited := 0
	for _, name := range caseNames {
		result := current.lane.result(ctx, current, name)
		switch {
		case result.unavailable != "":
			unavailable = append(unavailable, result.unavailable)
		case result.failure != "":
			problems = append(problems, "case "+name+": "+result.failure)
		default:
			for _, call := range result.calls {
				if labels == nil || slices.Contains(labels, call.label) {
					visited++
					problems = append(problems, visit(name, call)...)
				}
			}
		}
	}
	if len(problems) == 0 && visited == 0 {
		return knownfailure.NotApplicable(strings.Join(unavailable, "\n"))
	}
	return knownfailure.OutcomeOf(problems)
}

// bindingVisit returns a visit checking that every statement of a call binds as many
// arguments as its placeholders take, and that every value passed reaches the driver.
//
// Returns callVisit which examines one call.
func (current *subject) bindingVisit() callVisit {
	return func(caseName string, call *laneCall) []string {
		if problems := reachedDriver(caseName, call); len(problems) > 0 {
			return problems
		}
		var problems []string
		for _, statement := range call.outcome.Statements {
			for _, problem := range argumentProblems(statement, &current.info) {
				problems = append(problems, fmt.Sprintf("%s: %s: %q", describeCall(caseName, call), problem, statement.SQL))
			}
		}
		for _, problem := range valueProblems(call) {
			problems = append(problems, describeCall(caseName, call)+": "+problem)
		}
		return problems
	}
}

// describeCall names a call in messages.
//
// Takes caseName (string) which is the call's case.
// Takes call (*laneCall) which is the call.
//
// Returns string which names the case, the query and the call.
func describeCall(caseName string, call *laneCall) string {
	return fmt.Sprintf("case %s, %s (%s call)", caseName, call.query, call.label)
}

// reachedDriver reports why a call sent nothing to examine: it never ran, it panicked, or
// no statement reached the driver.
//
// Takes caseName (string) which is the call's case.
// Takes call (*laneCall) which is the call.
//
// Returns []string which holds the problem, or nothing.
func reachedDriver(caseName string, call *laneCall) []string {
	switch {
	case call.outcome == nil:
		return []string{describeCall(caseName, call) + ": the call never ran"}
	case call.outcome.Panic != "":
		return []string{describeCall(caseName, call) + ": the call panicked: " + call.outcome.Panic}
	case len(call.outcome.Statements) == 0:
		return []string{fmt.Sprintf("%s: no statement reached the driver (the call returned %q)", describeCall(caseName, call), call.outcome.Error)}
	default:
		return nil
	}
}

// checkRuntimeArguments checks that every call of the general runtime cases binds as many
// arguments as its SQL has placeholders, with every value passed.
//
// Takes current (*subject) which is the dialect under test.
//
// Returns knownfailure.Outcome which holds the problems.
func checkRuntimeArguments(ctx context.Context, current *subject) knownfailure.Outcome {
	return current.overCalls(ctx, []string{caseCommands, caseResults, caseNamed, caseBuilder}, []string{labelPlain, labelAll}, current.bindingVisit())
}

// checkSliceArguments checks binding for nil, empty and populated slice parameters.
//
// Takes current (*subject) which is the dialect under test.
//
// Returns knownfailure.Outcome which holds the problems.
func checkSliceArguments(ctx context.Context, current *subject) knownfailure.Outcome {
	binding := current.bindingVisit()
	return current.overCalls(ctx, []string{caseSlices}, nil, func(caseName string, call *laneCall) []string {
		problems := binding(caseName, call)
		if call.outcome == nil {
			return problems
		}
		for _, statement := range call.outcome.Statements {
			if problem := listProblem(statement, call, current); problem != "" {
				problems = append(problems, fmt.Sprintf("%s: %s: %q", describeCall(caseName, call), problem, statement.SQL))
			}
		}
		return problems
	})
}

// listProblem says how a statement fails to send a slice: an empty IN list, or a list
// whose placeholders do not number the slice's elements where no argument carries the
// whole slice.
//
// Takes statement (laneStatement) which is the statement.
// Takes call (*laneCall) which is the call that sent it.
// Takes current (*subject) which is the dialect under test.
//
// Returns string which describes the problem, or nothing.
func listProblem(statement laneStatement, call *laneCall, current *subject) string {
	scan, err := scanStatement(statement.SQL, &current.info)
	if err != nil {
		return "does not lex"
	}
	if scan.emptyLists() > 0 {
		return "sends an empty IN list"
	}
	if call.label != labelOne && call.label != labelMany || boundWhole(statement, call) {
		return ""
	}
	elements := sliceLength(call.label)
	for _, count := range scan.inListPlaceholders() {
		if count != elements {
			return fmt.Sprintf("sends a slice of %d elements as an IN list of %d placeholders", elements, count)
		}
	}
	return ""
}

// boundWhole reports a statement that binds a slice whole, as one argument holding every
// element the call passed in the slice, such as an array.
//
// Takes statement (laneStatement) which is the statement.
// Takes call (*laneCall) which is the call that sent it.
//
// Returns bool which is true when one argument carries every element.
func boundWhole(statement laneStatement, call *laneCall) bool {
	var elements []string
	for _, expected := range call.expected {
		if strings.Contains(expected.path, "[") {
			elements = append(elements, expected.text)
		}
	}
	if len(elements) == 0 {
		return false
	}
	return slices.ContainsFunc(statement.Arguments, func(argument laneArgument) bool {
		return !slices.ContainsFunc(elements, func(element string) bool { return !strings.Contains(argument.Text, element) })
	})
}

// checkEmptySliceSemantics checks IN and NOT IN behaviour for nil and empty slices.
//
// Takes current (*subject) which is the dialect under test.
//
// Returns knownfailure.Outcome which holds the problems.
func checkEmptySliceSemantics(ctx context.Context, current *subject) knownfailure.Outcome {
	lists := current.info.Lists
	if lists.EmptyMembership == "" || lists.EmptyExclusion == "" {
		return knownfailure.NotApplicable("the dialect's Info.Lists gives no empty membership and exclusion tests")
	}
	return current.overCalls(ctx, []string{caseSlices}, []string{labelNil, labelEmpty}, func(caseName string, call *laneCall) []string {
		if problems := reachedDriver(caseName, call); len(problems) > 0 {
			return problems
		}
		expected, excluded := lists.EmptyMembership, call.query == queryItemsNotIn
		if excluded {
			expected = lists.EmptyExclusion
		}
		for _, statement := range call.outcome.Statements {
			if !strings.Contains(statement.SQL, expected) && !bindsEmptyArray(statement, excluded) {
				return []string{fmt.Sprintf("%s: sends %q, without the dialect's test %q for an empty list or an empty array bound to it",
					describeCall(caseName, call), statement.SQL, expected)}
			}
		}
		return nil
	})
}

// bindsEmptyArray reports a statement that binds an empty array: in an exclusion, where
// NULL would exclude every row, never a nil one; in a membership, where NULL matches no
// row just as an empty array does, a nil one too.
//
// Takes statement (laneStatement) which is the statement.
// Takes excluded (bool) which is true for NOT IN.
//
// Returns bool which is true when the slice is bound as an empty array.
func bindsEmptyArray(statement laneStatement, excluded bool) bool {
	empty := false
	for _, argument := range statement.Arguments {
		switch argument.Text {
		case nilText:
			if excluded {
				return false
			}
			empty = true
		case emptyArrayText, emptyMapText:
			empty = true
		}
	}
	return empty
}

// checkBatchRows checks that a batch of three items binds every item, numbers each row's
// placeholders on, and keeps the row's expression and its literal, which looks like
// placeholders, in every row.
//
// Takes current (*subject) which is the dialect under test.
//
// Returns knownfailure.Outcome which holds the problems.
func checkBatchRows(ctx context.Context, current *subject) knownfailure.Outcome {
	binding := current.bindingVisit()
	return current.overCalls(ctx, []string{caseBatch}, nil, func(caseName string, call *laneCall) []string {
		problems := binding(caseName, call)
		if call.outcome == nil {
			return problems
		}
		literals, expressions := 0, 0
		for _, statement := range call.outcome.Statements {
			literals += strings.Count(statement.SQL, confusableLiteral)
			expressions += strings.Count(statement.SQL, incrementedRank)
		}
		if literals != manyElements || expressions != manyElements {
			problems = append(problems, fmt.Sprintf("%s: %d items sent %d copies of the row's literal and %d of its expression %q",
				describeCall(caseName, call), manyElements, literals, expressions, strings.TrimSpace(incrementedRank)))
		}
		return problems
	})
}

// checkBatchRejectsParametersOutsideValues checks that a batch with a parameter after its
// VALUES row is refused with a diagnostic, or sends each item in a statement of its own,
// never a multi-row statement binding the parameter once.
//
// Takes current (*subject) which is the dialect under test.
//
// Returns knownfailure.Outcome which holds the problems.
func checkBatchRejectsParametersOutsideValues(ctx context.Context, current *subject) knownfailure.Outcome {
	result := current.generation(ctx, caseBatchSuffix)
	switch {
	case result.unusable != "":
		return knownfailure.NotApplicable("case " + caseBatchSuffix + ": " + result.unusable)
	case result.generateError != nil:
		return knownfailure.OutcomeOf([]string{fmt.Sprintf("case %s: Generate failed instead of reporting a diagnostic: %v", caseBatchSuffix, result.generateError)})
	case len(errorDiagnostics(result.generated.Diagnostics)) > 0:
		return knownfailure.Passed()
	}
	binding := current.bindingVisit()
	return current.overCalls(ctx, []string{caseBatchSuffix}, nil, func(caseName string, call *laneCall) []string {
		problems := binding(caseName, call)
		if call.outcome == nil {
			return problems
		}
		for _, statement := range call.outcome.Statements {
			scan, err := scanStatement(statement.SQL, &current.info)
			if err != nil {
				continue
			}
			if rows, outside := scan.valuesRows(); rows > 1 && outside > 0 {
				problems = append(problems, fmt.Sprintf("%s: binds %d placeholders outside the %d VALUES rows of one statement, once for every row: %q",
					describeCall(caseName, call), outside, rows, statement.SQL))
			}
		}
		return problems
	})
}

// checkBatchUpsertsSendEachRowAlone checks that a batch upsert sends each item in a
// separate statement. This preserves per-row behaviour for repeated keys, since engines
// can reject a multi-row statement that updates the same row twice.
//
// Takes current (*subject) which is the dialect under test.
//
// Returns knownfailure.Outcome which holds the problems.
func checkBatchUpsertsSendEachRowAlone(ctx context.Context, current *subject) knownfailure.Outcome {
	result := current.generation(ctx, caseBatchUpsert)
	if result.unusable != "" {
		return knownfailure.NotApplicable("case " + caseBatchUpsert + ": " + result.unusable)
	}
	binding := current.bindingVisit()
	return current.overCalls(ctx, []string{caseBatchUpsert}, nil, func(caseName string, call *laneCall) []string {
		problems := binding(caseName, call)
		if call.outcome == nil {
			return problems
		}
		for _, statement := range call.outcome.Statements {
			scan, err := scanStatement(statement.SQL, &current.info)
			if err != nil {
				continue
			}
			if rows, _ := scan.valuesRows(); rows > 1 {
				problems = append(problems, fmt.Sprintf("%s: sends %d rows in one statement whose clause after its VALUES row "+
					"may change one row twice: %q", describeCall(caseName, call), rows, statement.SQL))
			}
		}
		return problems
	})
}

// checkCountBindsItsArguments checks that a runtime builder's Count binds the base
// query's parameters and the filter's, numbered without gaps.
//
// Takes current (*subject) which is the dialect under test.
//
// Returns knownfailure.Outcome which holds the problems.
func checkCountBindsItsArguments(ctx context.Context, current *subject) knownfailure.Outcome {
	return current.overCalls(ctx, []string{caseBuilder}, []string{labelCount}, current.bindingVisit())
}

// checkSortableOrderBy checks that a sorted call sends exactly one ORDER BY, before any
// LIMIT and before the statement's terminator.
//
// Takes current (*subject) which is the dialect under test.
//
// Returns knownfailure.Outcome which holds the problems.
func checkSortableOrderBy(ctx context.Context, current *subject) knownfailure.Outcome {
	binding := current.bindingVisit()
	terminator := terminatorOf(&current.info)
	return current.overCalls(ctx, []string{caseSortable}, []string{labelSorted}, func(caseName string, call *laneCall) []string {
		problems := binding(caseName, call)
		if call.outcome == nil {
			return problems
		}
		for _, statement := range call.outcome.Statements {
			if problem := orderByProblem(statement.SQL, terminator, current); problem != "" {
				problems = append(problems, fmt.Sprintf("%s: %s: %q", describeCall(caseName, call), problem, statement.SQL))
			}
		}
		return problems
	})
}

// orderByProblem says how a sorted statement misplaces its ORDER BY.
//
// Takes text (string) which is the statement.
// Takes terminator (string) which is the dialect's terminator.
// Takes current (*subject) which is the dialect under test.
//
// Returns string which describes the problem, or nothing.
func orderByProblem(text, terminator string, current *subject) string {
	scan, err := scanStatement(text, &current.info)
	if err != nil {
		return "does not lex"
	}
	orderBy := scan.clauses("ORDER", "BY")
	limits := scan.clauses("LIMIT")
	switch {
	case len(orderBy) != 1:
		return fmt.Sprintf("sends %d ORDER BY clauses instead of one", len(orderBy))
	case len(limits) > 0 && limits[0] < orderBy[0]:
		return "puts ORDER BY after LIMIT"
	}
	for index, token := range scan.tokens {
		if token.Raw == terminator && scan.depths[index] == 0 && index != len(scan.tokens)-1 {
			return "puts SQL after the statement's terminator"
		}
	}
	return ""
}

// checkRuntimeBuilderClauses checks that a runtime builder inserts its filter, order and
// pagination where they belong in a base query that already orders, limits or filters:
// each clause once and in order, and the filter joined to a base condition with OR only
// inside parentheses.
//
// Takes current (*subject) which is the dialect under test.
//
// Returns knownfailure.Outcome which holds the problems.
func checkRuntimeBuilderClauses(ctx context.Context, current *subject) knownfailure.Outcome {
	binding := current.bindingVisit()
	return current.overCalls(ctx, []string{caseBuilderClauses, caseBuilderFilter}, nil, func(caseName string, call *laneCall) []string {
		problems := binding(caseName, call)
		if call.outcome == nil {
			return problems
		}
		for _, statement := range call.outcome.Statements {
			if problem := clauseOrderProblem(statement.SQL, current); problem != "" {
				problems = append(problems, fmt.Sprintf("%s: %s: %q", describeCall(caseName, call), problem, statement.SQL))
			}
		}
		return problems
	})
}

// clauseOrderProblem says how a runtime-built statement misplaces its clauses.
//
// Takes text (string) which is the statement.
// Takes current (*subject) which is the dialect under test.
//
// Returns string which describes the problem, or nothing.
func clauseOrderProblem(text string, current *subject) string {
	scan, err := scanStatement(text, &current.info)
	if err != nil {
		return "does not lex"
	}
	order := [][]string{{"WHERE"}, {"ORDER", "BY"}, {"LIMIT"}, {"OFFSET"}}
	previous, previousName := -1, ""
	for _, words := range order {
		positions := scan.clauses(words...)
		name := strings.Join(words, " ")
		switch {
		case len(positions) > 1:
			return fmt.Sprintf("sends %d %s clauses", len(positions), name)
		case len(positions) == 1 && positions[0] < previous:
			return "puts " + name + " before " + previousName
		case len(positions) == 1:
			previous, previousName = positions[0], name
		}
	}
	if scan.conditionMixesConnectives() {
		return "joins the filter to a base condition with OR outside parentheses, so the filter does not bind every branch"
	}
	return ""
}

// checkQueryNameResolves checks that the query-names function names the query of every
// statement the runtime lane's calls sent, whatever SQL the method assembled.
//
// Takes current (*subject) which is the dialect under test.
//
// Returns knownfailure.Outcome which holds the problems.
func checkQueryNameResolves(ctx context.Context, current *subject) knownfailure.Outcome {
	var names []string
	for _, definition := range corpusCases {
		if definition.runtime {
			names = append(names, definition.name)
		}
	}
	return current.overCalls(ctx, names, nil, func(caseName string, call *laneCall) []string {
		if call.outcome == nil || len(call.outcome.Statements) == 0 {
			return nil
		}
		if !call.outcome.QueryNames {
			return []string{describeCall(caseName, call) + ": the package declares no " + queryNamesFunction + " function, though the style asks for one"}
		}
		var problems []string
		for _, statement := range call.outcome.Statements {
			if statement.QueryName != call.query {
				problems = append(problems, fmt.Sprintf("%s: %s names %q, not %s: %q", describeCall(caseName, call),
					queryNamesFunction, statement.QueryName, call.query, statement.SQL))
			}
		}
		return problems
	})
}

// checkEnumValuesReachTheDriver checks enum binding, batch appending and column scanning.
//
// Takes current (*subject) which is the dialect under test.
//
// Returns knownfailure.Outcome which holds the problems.
func checkEnumValuesReachTheDriver(ctx context.Context, current *subject) knownfailure.Outcome {
	binding := current.bindingVisit()
	return current.overCalls(ctx, []string{caseEnums}, nil, func(caseName string, call *laneCall) []string {
		problems := binding(caseName, call)
		if problem := refusedByDriver(caseName, call); problem != "" {
			problems = append(problems, problem)
		}
		return problems
	})
}

// refusedByDriver says how a call failed other than by finding no row, such as a driver
// refusing a value it does not bind, append or scan.
//
// Takes caseName (string) which is the call's case.
// Takes call (*laneCall) which is the call.
//
// Returns string which describes the failure, or nothing.
func refusedByDriver(caseName string, call *laneCall) string {
	return refusedValueOf(caseName, call, "the enum's named type")
}

// refusedValueOf says how a call failed other than by finding no row, naming the type of
// the value the driver may have refused.
//
// Takes caseName (string) which is the call's case.
// Takes call (*laneCall) which is the call.
// Takes valueType (string) which describes the type the case passes the driver.
//
// Returns string which describes the failure, or nothing.
func refusedValueOf(caseName string, call *laneCall, valueType string) string {
	if call.outcome == nil || call.outcome.Error == "" || strings.HasSuffix(call.outcome.Error, noRowsText) {
		return ""
	}
	return fmt.Sprintf("%s: the driver refused a value of %s: %s", describeCall(caseName, call), valueType, call.outcome.Error)
}

// checkOverrideTypesReachTheDriver checks that column overrides cross the driver's
// binding and scanning interfaces.
//
// Takes current (*subject) which is the dialect under test.
//
// Returns knownfailure.Outcome which holds the problems.
func checkOverrideTypesReachTheDriver(ctx context.Context, current *subject) knownfailure.Outcome {
	binding := current.bindingVisit()
	return current.overCalls(ctx, []string{caseOverrides}, nil, func(caseName string, call *laneCall) []string {
		problems := binding(caseName, call)
		if problem := refusedValueOf(caseName, call, "a column override's defined type"); problem != "" {
			problems = append(problems, problem)
		}
		return problems
	})
}

// checkJSONParametersBindAsText checks JSON text binding in statements and batch rows.
//
// Takes current (*subject) which is the dialect under test.
//
// Returns knownfailure.Outcome which holds the problems.
func checkJSONParametersBindAsText(ctx context.Context, current *subject) knownfailure.Outcome {
	binding := current.bindingVisit()
	return current.overCalls(ctx, []string{caseJSON}, nil, func(caseName string, call *laneCall) []string {
		problems := binding(caseName, call)
		if problem := refusedValueOf(caseName, call, "the JSON type"); problem != "" {
			problems = append(problems, problem)
		}
		return append(problems, boundAsBytes(caseName, call)...)
	})
}

// checkJSONColumnsReadIntoTheirGoType checks JSON column scanning across query result
// forms.
//
// Takes current (*subject) which is the dialect under test.
//
// Returns knownfailure.Outcome which holds the problems.
func checkJSONColumnsReadIntoTheirGoType(ctx context.Context, current *subject) knownfailure.Outcome {
	binding := current.bindingVisit()
	return current.overCalls(ctx, []string{caseJSONReads}, nil, func(caseName string, call *laneCall) []string {
		problems := binding(caseName, call)
		if problem := refusedValueOf(caseName, call, "the JSON type"); problem != "" {
			problems = append(problems, problem)
		}
		return problems
	})
}

// boundAsBytes reports each argument of a call that reached the driver as bytes, which no
// value of the JSON case should: its documents are text and its keys numbers.
//
// Takes caseName (string) which is the call's case.
// Takes call (*laneCall) which is the call.
//
// Returns []string which describes each argument bound as bytes.
func boundAsBytes(caseName string, call *laneCall) []string {
	if call.outcome == nil {
		return nil
	}
	var problems []string
	for _, statement := range call.outcome.Statements {
		for _, argument := range statement.Arguments {
			if argument.Bound == boundBytes {
				problems = append(problems, fmt.Sprintf("%s: the value %q of %s reached the driver as bytes rather than text",
					describeCall(caseName, call), argument.Text, argument.Type))
			}
		}
	}
	return problems
}

// checkNestedEnumsConvertOrRefuse checks nested enum conversion or generation-time
// refusal.
//
// Unsupported shapes produce Q059 during generation. Shapes the dialect cannot express
// are skipped.
//
// Takes current (*subject) which is the dialect under test.
//
// Returns knownfailure.Outcome which holds the problems.
func checkNestedEnumsConvertOrRefuse(ctx context.Context, current *subject) knownfailure.Outcome {
	var problems, generated, unusable []string
	refused := 0
	for _, name := range nestedEnumCaseNames {
		result := current.generation(ctx, name)
		switch {
		case result.unusable != "":
			unusable = append(unusable, casePrefix+name+": "+result.unusable)
		case result.generateError != nil:
			problems = append(problems, fmt.Sprintf("case %s: Generate failed instead of reporting a diagnostic: %v", name, result.generateError))
		case len(errorDiagnostics(result.generated.Diagnostics)) > 0:
			refused++
			problems = append(problems, refusalProblems(name, errorDiagnostics(result.generated.Diagnostics))...)
		default:
			generated = append(generated, name)
		}
	}
	if len(generated) > 0 {
		calls := current.overCalls(ctx, generated, nil, func(caseName string, call *laneCall) []string {
			problems := reachedDriver(caseName, call)
			if problem := refusedByDriver(caseName, call); problem != "" {
				problems = append(problems, problem)
			}
			return problems
		})
		if calls.NotApplicable != "" && len(problems) == 0 && refused == 0 {
			return calls
		}
		problems = append(problems, calls.Problems...)
	}
	if len(generated) == 0 && len(problems) == 0 && refused == 0 {
		return knownfailure.NotApplicable(strings.Join(unusable, "\n"))
	}
	return knownfailure.OutcomeOf(problems)
}

// refusalProblems reports the error diagnostics of a refused case that are not Q059, the
// one code an emitter refuses a shape its driver cannot take under.
//
// Takes caseName (string) which is the case.
// Takes refusals ([]diag.SourceError) which are the case's error diagnostics.
//
// Returns []string which describes each diagnostic of another code.
func refusalProblems(caseName string, refusals []diag.SourceError) []string {
	var problems []string
	for index := range refusals {
		if refusals[index].Code != diag.CodeEmitterUnsupported {
			problems = append(problems, fmt.Sprintf("case %s: the emitter refused with %s rather than %s: %s", caseName,
				refusals[index].Code, diag.CodeEmitterUnsupported, refusals[index].Message))
		}
	}
	return problems
}
