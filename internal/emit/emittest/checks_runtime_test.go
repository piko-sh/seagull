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
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/mysql"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/testkit/knownfailure"
)

const (
	batchRow     = "($%d, $%d, $%d + 1, " + confusableLiteral + ")"
	upsertSuffix = " ON CONFLICT (id) DO UPDATE SET kind = "
	mergeSuffix  = " ON CONFLICT (id) DO UPDATE SET kind = excluded.kind"
)

func subjectOf(engine dialect.DialectPort) *subject {
	return &subject{info: engine.Info()}
}

func TestOrderByProblemPlacesOneOrderBeforeLimitAndTerminator(t *testing.T) {
	t.Parallel()

	current := subjectOf(postgres.New())
	testCases := []struct {
		sql      string
		expected string
	}{
		{sql: "SELECT id FROM t WHERE k = $1 ORDER BY id DESC LIMIT 5;", expected: ""},
		{sql: "SELECT id FROM t ORDER BY id", expected: ""},
		{sql: "SELECT id FROM t", expected: "sends 0 ORDER BY clauses instead of one"},
		{sql: "SELECT id FROM t ORDER BY id ORDER BY k", expected: "sends 2 ORDER BY clauses instead of one"},
		{sql: "SELECT id FROM t LIMIT 5 ORDER BY id", expected: "puts ORDER BY after LIMIT"},
		{sql: "SELECT id FROM t; ORDER BY id", expected: "puts SQL after the statement's terminator"},
		{sql: "SELECT id FROM (SELECT id FROM t ORDER BY id LIMIT 3) s ORDER BY id", expected: ""},
	}
	for _, testCase := range testCases {
		require.Equal(t, testCase.expected, orderByProblem(testCase.sql, ";", current), testCase.sql)
	}
}

func TestClauseOrderProblemKeepsBuilderClausesInPlace(t *testing.T) {
	t.Parallel()

	current := subjectOf(postgres.New())
	testCases := []struct {
		sql      string
		expected string
	}{
		{sql: "SELECT a FROM t WHERE (a = $1 OR b = $2) AND c = $3 ORDER BY a LIMIT $4 OFFSET $5", expected: ""},
		{sql: "SELECT a FROM t ORDER BY a LIMIT $1 WHERE c = $2", expected: "puts ORDER BY before WHERE"},
		{sql: "SELECT a FROM t ORDER BY a ORDER BY b", expected: "sends 2 ORDER BY clauses"},
		{sql: "SELECT a FROM t WHERE a = $1 OR b = $2 AND c = $3",
			expected: "joins the filter to a base condition with OR outside parentheses, so the filter does not bind every branch"},
	}
	for _, testCase := range testCases {
		require.Equal(t, testCase.expected, clauseOrderProblem(testCase.sql, current), testCase.sql)
	}
}

func TestListProblemExpectsOnePlaceholderPerElementOrTheWholeSlice(t *testing.T) {
	t.Parallel()

	many := []expectedValue{{path: "ids[0]", text: "7001"}, {path: "ids[1]", text: "7002"}, {path: "ids[2]", text: "7003"}}
	testCases := []struct {
		name      string
		engine    dialect.DialectPort
		statement laneStatement
		label     string
		expected  string
	}{
		{name: "expanded", engine: mysql.New(), label: labelMany, expected: "",
			statement: statementOf("SELECT id FROM t WHERE id IN (?,?,?)", positional("7001"), positional("7002"), positional("7003"))},
		{name: "one placeholder bound to one element", engine: mysql.New(), label: labelMany,
			statement: statementOf("SELECT id FROM t WHERE id IN (?)", positional("7001")),
			expected:  "sends a slice of 3 elements as an IN list of 1 placeholders"},
		{name: "one placeholder bound to the whole slice", engine: mysql.New(), label: labelMany, expected: "",
			statement: statementOf("SELECT id FROM t WHERE id IN (?)", positional("[7001,7002,7003]"))},
		{name: "one element", engine: mysql.New(), label: labelOne, expected: "",
			statement: statementOf("SELECT id FROM t WHERE id IN (?)", positional("7001"))},
		{name: "empty list", engine: mysql.New(), label: labelEmpty, expected: "sends an empty IN list",
			statement: statementOf("SELECT id FROM t WHERE id IN ()")},
		{name: "array membership", engine: postgres.New(), label: labelMany, expected: "",
			statement: statementOf("SELECT id FROM t WHERE id = ANY($1)", positional("[7001 7002 7003]"))},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			call := &laneCall{query: "ItemsIn", label: testCase.label, expected: many}
			require.Equal(t, testCase.expected, listProblem(testCase.statement, call, subjectOf(testCase.engine)))
		})
	}
}

func TestBindsEmptyArrayAcceptsNilOnlyWhereNullMatchesNoRow(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		statement laneStatement
		excluded  bool
		expected  bool
	}{
		{name: "an empty array in a membership", statement: statementOf("SELECT id FROM t WHERE id = ANY($1)", positional("[]")), expected: true},
		{name: "an empty array in braces", statement: statementOf("SELECT id FROM t WHERE id = ANY($1)", positional("{}")), expected: true},
		{name: "a nil slice in a membership", statement: statementOf("SELECT id FROM t WHERE id = ANY($1)", positional("<nil>")), expected: true},
		{name: "an empty array in an exclusion", statement: statementOf("SELECT id FROM t WHERE id <> ALL($1)", positional("[]")), excluded: true, expected: true},
		{name: "a nil slice in an exclusion", statement: statementOf("SELECT id FROM t WHERE id <> ALL($1)", positional("<nil>")), excluded: true, expected: false},
		{name: "no array", statement: statementOf("SELECT id FROM t WHERE id = $1", positional("7")), expected: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.expected, bindsEmptyArray(testCase.statement, testCase.excluded))
		})
	}
}

func TestValueProblemsFindValuesThatNeverReachedTheDriver(t *testing.T) {
	t.Parallel()

	call := &laneCall{
		query: "Find",
		label: labelPlain,
		expected: []expectedValue{
			{path: "params.Kind", text: "emittest0001"},
			{path: "params.Kind2", text: "emittest0002"},
			{path: "params.OrderBy", text: "kind"},
		},
		outcome: &laneOutcome{Statements: []laneStatement{
			statementOf("SELECT id FROM t WHERE k = $1 AND j = $2 ORDER BY kind", positional("emittest0001"), positional("emittest0001")),
		}},
	}

	require.Equal(t, []string{"never sent the value emittest0002 passed in params.Kind2"}, valueProblems(call))
}

func TestReachedDriverExplainsCallsWithNothingToExamine(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		call     *laneCall
		expected []string
	}{
		{name: "a call that never ran", call: &laneCall{query: "Find", label: labelPlain},
			expected: []string{"case c, Find (plain call): the call never ran"}},
		{name: "a call that panicked", call: &laneCall{query: "Find", label: labelPlain, outcome: &laneOutcome{Panic: "boom"}},
			expected: []string{"case c, Find (plain call): the call panicked: boom"}},
		{name: "a call that sent nothing", call: &laneCall{query: "Find", label: labelPlain, outcome: &laneOutcome{Error: "refused"}},
			expected: []string{`case c, Find (plain call): no statement reached the driver (the call returned "refused")`}},
		{name: "a call that sent a statement", call: &laneCall{query: "Find", label: labelPlain,
			outcome: &laneOutcome{Statements: []laneStatement{statementOf("SELECT 1")}}}, expected: nil},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.expected, reachedDriver("c", testCase.call))
		})
	}
}

func TestRefusedByDriverReportsAnyFailureButAMissingRow(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		outcome  *laneOutcome
		expected string
	}{
		{name: "a call that never ran", outcome: nil, expected: ""},
		{name: "a call that succeeded", outcome: &laneOutcome{}, expected: ""},
		{name: "a row database/sql did not find", outcome: &laneOutcome{Error: "sql: no rows in result set"}, expected: ""},
		{name: "a row pgx did not find", outcome: &laneOutcome{Error: "no rows in result set"}, expected: ""},
		{
			name:     "a slice the driver cannot encode",
			outcome:  &laneOutcome{Error: "failed to encode args[0] of type []store.Feeling"},
			expected: "case enums, FeelingsIn (one call): the driver refused a value of the enum's named type: failed to encode args[0] of type []store.Feeling",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			call := &laneCall{outcome: testCase.outcome, query: "FeelingsIn", label: labelOne}
			require.Equal(t, testCase.expected, refusedByDriver(caseEnums, call))
		})
	}
}

func TestRefusalProblemsAcceptOnlyTheEmitterUnsupportedCode(t *testing.T) {
	t.Parallel()

	refusal := func(code, message string) diag.SourceError {
		diagnostic := diag.SourceError{}
		diagnostic.Code = code
		diagnostic.Message = message
		diagnostic.Severity = diag.SeverityError
		return diagnostic
	}
	testCases := []struct {
		name     string
		refusals []diag.SourceError
		expected []string
	}{
		{name: "a refusal under Q059", refusals: []diag.SourceError{refusal(diag.CodeEmitterUnsupported, "column grid holds the enum")}, expected: nil},
		{
			name:     "a failure under another code",
			refusals: []diag.SourceError{refusal(diag.CodeEmitterUnsupported, "column grid holds the enum"), refusal("Q001", "no such column")},
			expected: []string{"case enumgrids: the emitter refused with Q001 rather than Q059: no such column"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.expected, refusalProblems(caseEnumGrids, testCase.refusals))
		})
	}
}

func batchInsert(rows int) string {
	written := make([]string, 0, rows)
	for row := range rows {
		written = append(written, fmt.Sprintf(batchRow, 3*row+1, 3*row+2, 3*row+3))
	}
	return "INSERT INTO items (id, kind, rank, name) VALUES " + strings.Join(written, ", ")
}

func positionals(texts ...string) []laneArgument {
	arguments := make([]laneArgument, 0, len(texts))
	for _, text := range texts {
		arguments = append(arguments, positional(text))
	}
	return arguments
}

func neverRan(query, label string) *laneCall {
	return &laneCall{query: query, label: label}
}

func panicked(query, label, value string) *laneCall {
	call := called(query, label, nil)
	call.outcome.Panic = value
	return call
}

func unnamed(call *laneCall) *laneCall {
	call.outcome.QueryNames = false
	return call
}

func generatedStore() *caseResult {
	return generatedFiles(goFile("queries.sql.go", "package store\n"))
}

func TestEachRuntimeCheckPassesGoodCallsAndReportsBrokenCalls(t *testing.T) {
	t.Parallel()

	const (
		getItem       = "SELECT id, name FROM items WHERE id = $1"
		searchItems   = "SELECT id, kind FROM items WHERE rank > $1 AND kind = $2 ORDER BY id LIMIT $3 OFFSET $4"
		itemsIn       = "SELECT id FROM items WHERE id = ANY($1) AND kind = $2"
		upsert        = "INSERT INTO items (id, kind) VALUES ($1, $2)" + upsertSuffix + "$3"
		upsertTwice   = "INSERT INTO items (id, kind) VALUES ($1, $2), ($3, $4)" + upsertSuffix + "$5"
		merge         = "INSERT INTO items (id, kind) VALUES ($1, $2)" + mergeSuffix
		mergeThrice   = "INSERT INTO items (id, kind) VALUES ($1, $2), ($3, $4), ($5, $6)" + mergeSuffix
		countItems    = "SELECT count(*) FROM items WHERE rank > $1 AND kind = $2"
		countSkipping = "SELECT count(*) FROM items WHERE rank > $1 AND kind = $3"
		sortedOnce    = "SELECT id, kind FROM items WHERE kind = $1 ORDER BY id LIMIT 5"
		sortedTwice   = "SELECT id, kind FROM items WHERE kind = $1 ORDER BY id LIMIT 5 ORDER BY kind"
		sortedAfter   = "SELECT id, kind FROM items WHERE rank > $1; ORDER BY id"
		orderedSearch = "SELECT id, kind FROM items WHERE kind = $1 ORDER BY id LIMIT $2 OFFSET $3"
		filterSearch  = "SELECT id, kind FROM items WHERE (kind = $1 OR name = $2) AND kind = $3 ORDER BY id LIMIT $4 OFFSET $5"
		whereLast     = "SELECT id, kind FROM items ORDER BY id LIMIT $1 WHERE kind = $2"
		filterLoose   = "SELECT id, kind FROM items WHERE kind = $1 OR name = $2 AND kind = $3"
		findFeeling   = "SELECT id, mood FROM feelings WHERE mood = $1"
		getGrid       = "SELECT id, grid FROM moodsets WHERE id = $1"
		findNote      = "SELECT id, label, position FROM notes WHERE label = $1"
		fileDocument  = "INSERT INTO documents (id, body) VALUES ($1, $2)"
		findDocument  = "SELECT id, body FROM documents WHERE id = $1"
		noRows        = "sql: no rows in result set"
		unsupported   = "sql: converting argument $1 type: unsupported type"
		unscannable   = `sql: Scan error on column index 1, name "body": unsupported Scan`
	)
	idOnly := []expectedValue{{path: "id", text: "7001"}}
	manyIDs := []expectedValue{{path: "ids[0]", text: "7001"}, {path: "ids[1]", text: "7002"}, {path: "ids[2]", text: "7003"}}
	document := laneArgument{Text: `{"emittest":1}`, Type: "json.RawMessage", Bound: "string"}
	documentBytes := laneArgument{Text: `{"emittest":1}`, Type: "json.RawMessage", Bound: boundBytes}
	key := laneArgument{Text: "7001", Type: "int64", Bound: "int64"}
	refusedNested := func(code, message string) map[string]*caseResult {
		return map[string]*caseResult{
			caseNullableEnums: refusedGeneration(code, message),
			caseEnumMaps:      refusedGeneration(diag.CodeEmitterUnsupported, message),
			caseEnumGrids:     refusedGeneration(diag.CodeEmitterUnsupported, message),
		}
	}
	testCases := []struct {
		generations map[string]*caseResult
		calls       map[string][]*laneCall
		edit        func(current *subject)
		name        string
		check       string
		expected    knownfailure.Outcome
	}{
		{
			name:  "every argument matches a placeholder and every value is sent",
			check: "RuntimeArgumentsMatchPlaceholders",
			calls: map[string][]*laneCall{
				caseCommands: {called("GetItem", labelPlain, idOnly, sent("GetItem", getItem, positional("7001")))},
				caseBuilder: {
					called("SearchItems", labelAll, []expectedValue{{path: "minimum", text: "7001"}, {path: "filter", text: "emittest0002"}},
						sent("SearchItems", searchItems, positionals("7001", "emittest0002", "5", "10")...)),
					called("SearchItems", labelCount, nil, sent("SearchItems", countItems, positionals("7001")...)),
				},
			},
			expected: knownfailure.Passed(),
		},
		{
			name:  "an extra argument, a value never sent, a call that never ran and one that panicked fail",
			check: "RuntimeArgumentsMatchPlaceholders",
			calls: map[string][]*laneCall{
				caseCommands: {called("GetItem", labelPlain, []expectedValue{{path: "id", text: "7001"}, {path: "name", text: "emittest0002"}},
					sent("GetItem", getItem, positionals("7001", "7001")...))},
				caseResults: {neverRan("DeleteCounted", labelPlain)},
				caseNamed:   {panicked("RepeatedName", labelPlain, "boom")},
			},
			expected: knownfailure.OutcomeOf([]string{
				`case commands, GetItem (plain call): binds 2 arguments to 1 placeholders: "` + getItem + `"`,
				"case commands, GetItem (plain call): never sent the value emittest0002 passed in name",
				"case results, DeleteCounted (plain call): the call never ran",
				"case named, RepeatedName (plain call): the call panicked: boom",
			}),
		},
		{
			name:  "slices bound whole as arrays",
			check: "SliceArguments",
			calls: map[string][]*laneCall{caseSlices: {
				called("ItemsIn", labelOne, manyIDs[:1], sent("ItemsIn", itemsIn, positionals("[7001]", "emittest0002")...)),
				called("ItemsIn", labelMany, manyIDs, sent("ItemsIn", itemsIn, positionals("[7001 7002 7003]", "emittest0004")...)),
			}},
			expected: knownfailure.Passed(),
		},
		{
			name:  "an empty IN list and a slice sent as one element fail",
			check: "SliceArguments",
			calls: map[string][]*laneCall{caseSlices: {
				called("ItemsIn", labelEmpty, nil, sent("ItemsIn", "SELECT id FROM items WHERE id IN () AND kind = $1", positional("emittest0001"))),
				called("ItemsIn", labelMany, manyIDs, sent("ItemsIn", "SELECT id FROM items WHERE id IN ($1) AND kind = $2", positionals("7001", "emittest0004")...)),
			}},
			expected: knownfailure.OutcomeOf([]string{
				`case slices, ItemsIn (empty call): sends an empty IN list: "SELECT id FROM items WHERE id IN () AND kind = $1"`,
				"case slices, ItemsIn (many call): never sent the value 7002 passed in ids[1]",
				"case slices, ItemsIn (many call): never sent the value 7003 passed in ids[2]",
				`case slices, ItemsIn (many call): sends a slice of 3 elements as an IN list of 1 placeholders: "SELECT id FROM items WHERE id IN ($1) AND kind = $2"`,
			}),
		},
		{
			name:  "nil and empty slices match no row and exclude none",
			check: "EmptySliceSemantics",
			calls: map[string][]*laneCall{caseSlices: {
				called("ItemsIn", labelNil, nil, sent("ItemsIn", "SELECT id FROM items WHERE id = ANY('{}') AND kind = $1", positional("emittest0001"))),
				called("ItemsNotIn", labelEmpty, nil, sent("ItemsNotIn", "SELECT id FROM items WHERE kind = $1 AND id <> ALL($2)", positionals("emittest0001", "[]")...)),
				neverRan("ItemsIn", labelOne),
			}},
			expected: knownfailure.Passed(),
		},
		{
			name:  "a nil slice bound to an exclusion and a call that never ran fail",
			check: "EmptySliceSemantics",
			calls: map[string][]*laneCall{caseSlices: {
				neverRan("ItemsIn", labelEmpty),
				called("ItemsNotIn", labelNil, nil, sent("ItemsNotIn", "SELECT id FROM items WHERE kind = $1 AND id <> ALL($2)", positionals("emittest0001", "<nil>")...)),
			}},
			expected: knownfailure.OutcomeOf([]string{
				"case slices, ItemsIn (empty call): the call never ran",
				`case slices, ItemsNotIn (nil call): sends "SELECT id FROM items WHERE kind = $1 AND id <> ALL($2)", ` +
					`without the dialect's test "<> ALL('{}')" for an empty list or an empty array bound to it`,
			}),
		},
		{
			name:     "a dialect without empty list tests is not examined",
			check:    "EmptySliceSemantics",
			edit:     func(current *subject) { current.info.Lists = dialect.ListSyntax{} },
			expected: knownfailure.NotApplicable("the dialect's Info.Lists gives no empty membership and exclusion tests"),
		},
		{
			name:  "a batch of three rows keeps each row's literal and expression",
			check: "BatchRows",
			calls: map[string][]*laneCall{caseBatch: {called("InsertItems", labelBatch, nil,
				sent("InsertItems", batchInsert(manyElements), positionals("7001", "emittest0002", "7003", "7004", "emittest0005", "7006", "7007", "emittest0008", "7009")...))}},
			expected: knownfailure.Passed(),
		},
		{
			name:  "a batch that sends one row of three fails",
			check: "BatchRows",
			calls: map[string][]*laneCall{caseBatch: {called("InsertItems", labelBatch, nil,
				sent("InsertItems", batchInsert(1), positionals("7001", "emittest0002", "7003")...))}},
			expected: knownfailure.OutcomeOf([]string{`case batch, InsertItems (batch call): 3 items sent 1 copies of the row's literal and 1 of its expression "+ 1"`}),
		},
		{
			name:     "a batch with a parameter after its row is not examined where the dialect cannot take it",
			check:    "BatchRejectsParametersOutsideValues",
			expected: knownfailure.NotApplicable("case batchsuffix: " + notRecorded),
		},
		{
			name:        "a batch with a parameter after its row that fails to generate fails",
			check:       "BatchRejectsParametersOutsideValues",
			generations: map[string]*caseResult{caseBatchSuffix: {generateError: errors.New("boom")}},
			expected:    knownfailure.OutcomeOf([]string{"case batchsuffix: Generate failed instead of reporting a diagnostic: boom"}),
		},
		{
			name:        "a batch with a parameter after its row may be refused",
			check:       "BatchRejectsParametersOutsideValues",
			generations: map[string]*caseResult{caseBatchSuffix: refusedGeneration(diag.CodeEmitterUnsupported, "the batch binds a parameter after its row")},
			expected:    knownfailure.Passed(),
		},
		{
			name:        "a batch with a parameter after its row may send each row alone",
			check:       "BatchRejectsParametersOutsideValues",
			generations: map[string]*caseResult{caseBatchSuffix: generatedStore()},
			calls: map[string][]*laneCall{caseBatchSuffix: {called("UpsertItems", labelBatch, nil,
				sent("UpsertItems", upsert, positionals("7001", "emittest0002", "emittest0003")...),
				sent("UpsertItems", upsert, positionals("7004", "emittest0005", "emittest0006")...),
			)}},
			expected: knownfailure.Passed(),
		},
		{
			name:        "a batch that binds a parameter after its rows once for all of them fails",
			check:       "BatchRejectsParametersOutsideValues",
			generations: map[string]*caseResult{caseBatchSuffix: generatedStore()},
			calls: map[string][]*laneCall{caseBatchSuffix: {called("UpsertItems", labelBatch, nil,
				sent("UpsertItems", upsertTwice, positionals("7001", "emittest0002", "7004", "emittest0005", "emittest0003")...))}},
			expected: knownfailure.OutcomeOf([]string{
				`case batchsuffix, UpsertItems (batch call): binds 1 placeholders outside the 2 VALUES rows of one statement, once for every row: "` + upsertTwice + `"`,
			}),
		},
		{
			name:     "a batch upsert is not examined where the dialect cannot take it",
			check:    "BatchUpsertsSendEachRowAlone",
			expected: knownfailure.NotApplicable("case batchupsert: " + notRecorded),
		},
		{
			name:        "a batch upsert sends each row alone",
			check:       "BatchUpsertsSendEachRowAlone",
			generations: map[string]*caseResult{caseBatchUpsert: generatedStore()},
			calls: map[string][]*laneCall{caseBatchUpsert: {called("MergeItems", labelBatch, nil,
				sent("MergeItems", merge, positionals("7001", "emittest0002")...),
				sent("MergeItems", merge, positionals("7003", "emittest0004")...),
				sent("MergeItems", merge, positionals("7005", "emittest0006")...),
			)}},
			expected: knownfailure.Passed(),
		},
		{
			name:        "a batch upsert that sends its rows in one statement fails",
			check:       "BatchUpsertsSendEachRowAlone",
			generations: map[string]*caseResult{caseBatchUpsert: generatedStore()},
			calls: map[string][]*laneCall{caseBatchUpsert: {called("MergeItems", labelBatch, nil,
				sent("MergeItems", mergeThrice, positionals("7001", "emittest0002", "7003", "emittest0004", "7005", "emittest0006")...))}},
			expected: knownfailure.OutcomeOf([]string{
				`case batchupsert, MergeItems (batch call): sends 3 rows in one statement whose clause after its VALUES row may change one row twice: "` +
					mergeThrice + `"`,
			}),
		},
		{
			name:     "a count binds the base query's parameter and the filter's",
			check:    "CountBindsItsArguments",
			calls:    map[string][]*laneCall{caseBuilder: {called("SearchItems", labelCount, nil, sent("SearchItems", countItems, positionals("7001", "emittest0002")...))}},
			expected: knownfailure.Passed(),
		},
		{
			name:  "a count that skips a placeholder number fails",
			check: "CountBindsItsArguments",
			calls: map[string][]*laneCall{caseBuilder: {called("SearchItems", labelCount, nil, sent("SearchItems", countSkipping, positionals("7001", "emittest0002")...))}},
			expected: knownfailure.OutcomeOf([]string{
				`case builder, SearchItems (count call): binds 2 arguments to 3 placeholders: "` + countSkipping + `"`,
				`case builder, SearchItems (count call): numbers its placeholders [1 3], skipping some: "` + countSkipping + `"`,
			}),
		},
		{
			name:     "a count is not examined without a harness",
			check:    "CountBindsItsArguments",
			expected: knownfailure.NotApplicable(noHarness),
		},
		{
			name:     "a sorted call orders once before its limit",
			check:    "SortableOrderBy",
			calls:    map[string][]*laneCall{caseSortable: {called("SortedLimited", labelSorted, nil, sent("SortedLimited", sortedOnce, positional("emittest0001")))}},
			expected: knownfailure.Passed(),
		},
		{
			name:  "a sorted call that orders twice or after its terminator fails",
			check: "SortableOrderBy",
			calls: map[string][]*laneCall{caseSortable: {
				called("SortedLimited", labelSorted, nil, sent("SortedLimited", sortedTwice, positional("emittest0001"))),
				called("SortedTerminated", labelSorted, nil, sent("SortedTerminated", sortedAfter, positional("7001"))),
			}},
			expected: knownfailure.OutcomeOf([]string{
				`case sortable, SortedLimited (sorted call): sends 2 ORDER BY clauses instead of one: "` + sortedTwice + `"`,
				`case sortable, SortedTerminated (sorted call): puts SQL after the statement's terminator: "` + sortedAfter + `"`,
			}),
		},
		{
			name:  "a builder puts each clause once and in order, its filter binding every branch",
			check: "RuntimeBuilderClauses",
			calls: map[string][]*laneCall{
				caseBuilderClauses: {called("SearchOrdered", labelAll, nil, sent("SearchOrdered", orderedSearch, positionals("emittest0001", "5", "10")...))},
				caseBuilderFilter:  {called("SearchFiltered", labelAll, nil, sent("SearchFiltered", filterSearch, positionals("emittest0001", "emittest0002", "emittest0003", "5", "10")...))},
			},
			expected: knownfailure.Passed(),
		},
		{
			name:  "a builder that orders before filtering or joins its filter loosely fails",
			check: "RuntimeBuilderClauses",
			calls: map[string][]*laneCall{
				caseBuilderClauses: {called("SearchOrdered", labelAll, nil, sent("SearchOrdered", whereLast, positionals("5", "emittest0001")...))},
				caseBuilderFilter:  {called("SearchFiltered", labelAll, nil, sent("SearchFiltered", filterLoose, positionals("emittest0001", "emittest0002", "emittest0003")...))},
			},
			expected: knownfailure.OutcomeOf([]string{
				`case builderclauses, SearchOrdered (all call): puts ORDER BY before WHERE: "` + whereLast + `"`,
				"case builderfilter, SearchFiltered (all call): joins the filter to a base condition with OR outside parentheses, " +
					`so the filter does not bind every branch: "` + filterLoose + `"`,
			}),
		},
		{
			name:  "the query-names function names each statement's query",
			check: "QueryNameResolves",
			calls: map[string][]*laneCall{caseCommands: {
				called("GetItem", labelPlain, nil, sent("GetItem", getItem, positional("7001"))),
				neverRan("ListItems", labelPlain),
			}},
			expected: knownfailure.Passed(),
		},
		{
			name:  "a package without the query-names function and a statement named for another query fail",
			check: "QueryNameResolves",
			calls: map[string][]*laneCall{
				caseCommands: {unnamed(called("GetItem", labelPlain, nil, sent("", getItem, positional("7001"))))},
				caseNamed:    {called("RepeatedName", labelPlain, nil, sent("GetItem", "SELECT id FROM items WHERE kind = $1 OR name = $1", positional("emittest0001")))},
			},
			expected: knownfailure.OutcomeOf([]string{
				"case commands, GetItem (plain call): the package declares no QueryName function, though the style asks for one",
				`case named, RepeatedName (plain call): QueryName names "GetItem", not RepeatedName: "SELECT id FROM items WHERE kind = $1 OR name = $1"`,
			}),
		},
		{
			name:     "an enum reaches the driver and finds no row",
			check:    "EnumValuesReachTheDriver",
			calls:    map[string][]*laneCall{caseEnums: {failing(called("FindFeeling", labelPlain, nil, sent("FindFeeling", findFeeling, positional("calm"))), noRows)}},
			expected: knownfailure.Passed(),
		},
		{
			name:  "an enum slice the driver refuses fails",
			check: "EnumValuesReachTheDriver",
			calls: map[string][]*laneCall{caseEnums: {failing(called("FeelingsIn", labelOne, nil), unsupported+" []store.Feeling")}},
			expected: knownfailure.OutcomeOf([]string{
				`case enums, FeelingsIn (one call): no statement reached the driver (the call returned "` + unsupported + ` []store.Feeling")`,
				"case enums, FeelingsIn (one call): the driver refused a value of the enum's named type: " + unsupported + " []store.Feeling",
			}),
		},
		{
			name:     "nested enums are not examined where the dialect cannot take them",
			check:    "NestedEnumsConvertOrRefuse",
			expected: knownfailure.NotApplicable(unrecordedCases(nestedEnumCaseNames)),
		},
		{
			name:        "nested enums may be refused under Q059",
			check:       "NestedEnumsConvertOrRefuse",
			generations: refusedNested(diag.CodeEmitterUnsupported, "the driver cannot take the shape"),
			expected:    knownfailure.Passed(),
		},
		{
			name:        "nested enums the driver converts",
			check:       "NestedEnumsConvertOrRefuse",
			generations: map[string]*caseResult{caseEnumGrids: generatedStore()},
			calls:       map[string][]*laneCall{caseEnumGrids: {failing(called("GetGrid", labelPlain, nil, sent("GetGrid", getGrid, positional("7001"))), noRows)}},
			expected:    knownfailure.Passed(),
		},
		{
			name:        "nested enums generated without a harness are not examined",
			check:       "NestedEnumsConvertOrRefuse",
			generations: map[string]*caseResult{caseEnumGrids: generatedStore()},
			expected:    knownfailure.NotApplicable(noHarness),
		},
		{
			name:  "a refusal under another code, a raw error and a value the driver refuses fail",
			check: "NestedEnumsConvertOrRefuse",
			generations: map[string]*caseResult{
				caseNullableEnums: refusedGeneration("Q001", "no such column"),
				caseEnumMaps:      {generateError: errors.New("boom")},
				caseEnumGrids:     generatedStore(),
			},
			calls: map[string][]*laneCall{caseEnumGrids: {failing(called("GetGrid", labelPlain, nil, sent("GetGrid", getGrid, positional("7001"))), unscannable)}},
			expected: knownfailure.OutcomeOf([]string{
				"case nullableenums: the emitter refused with Q001 rather than Q059: no such column",
				"case enummaps: Generate failed instead of reporting a diagnostic: boom",
				"case enumgrids, GetGrid (plain call): the driver refused a value of the enum's named type: " + unscannable,
			}),
		},
		{
			name:     "an override's defined type reaches the driver and finds no row",
			check:    "OverrideTypesReachTheDriver",
			calls:    map[string][]*laneCall{caseOverrides: {failing(called("FindNote", labelPlain, nil, sent("FindNote", findNote, positional("emittest0001"))), noRows)}},
			expected: knownfailure.Passed(),
		},
		{
			name:  "an override's defined type the driver refuses fails",
			check: "OverrideTypesReachTheDriver",
			calls: map[string][]*laneCall{caseOverrides: {failing(called("FindNote", labelPlain, nil, sent("FindNote", findNote, positional("emittest0001"))),
				unsupported+" template.HTML")}},
			expected: knownfailure.OutcomeOf([]string{
				"case overrides, FindNote (plain call): the driver refused a value of a column override's defined type: " + unsupported + " template.HTML",
			}),
		},
		{
			name:     "a JSON document reaches the driver as text",
			check:    "JSONParametersBindAsText",
			calls:    map[string][]*laneCall{caseJSON: {called("FileDocument", labelPlain, nil, sent("FileDocument", fileDocument, key, document))}},
			expected: knownfailure.Passed(),
		},
		{
			name:  "a JSON document bound as bytes or refused fails",
			check: "JSONParametersBindAsText",
			calls: map[string][]*laneCall{caseJSON: {
				called("FileDocument", labelPlain, nil, sent("FileDocument", fileDocument, key, documentBytes)),
				failing(called("FileDocuments", labelBatch, nil, sent("FileDocuments", fileDocument, key, document)), unsupported+" json.RawMessage"),
			}},
			expected: knownfailure.OutcomeOf([]string{
				`case json, FileDocument (plain call): the value "{\"emittest\":1}" of json.RawMessage reached the driver as bytes rather than text`,
				"case json, FileDocuments (batch call): the driver refused a value of the JSON type: " + unsupported + " json.RawMessage",
			}),
		},
		{
			name:     "a JSON column is read and finds no row",
			check:    "JSONColumnsReadIntoTheirGoType",
			calls:    map[string][]*laneCall{caseJSONReads: {failing(called("FindDocument", labelPlain, nil, sent("FindDocument", findDocument, positional("7001"))), noRows)}},
			expected: knownfailure.Passed(),
		},
		{
			name:  "a JSON column the driver cannot scan fails",
			check: "JSONColumnsReadIntoTheirGoType",
			calls: map[string][]*laneCall{caseJSONReads: {failing(called("FindDocument", labelPlain, nil, sent("FindDocument", findDocument, positional("7001"))), unscannable)}},
			expected: knownfailure.OutcomeOf([]string{
				"case jsonreads, FindDocument (plain call): the driver refused a value of the JSON type: " + unscannable,
			}),
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			current := recordedSubject(t, postgres.New(), testCase.generations)
			if testCase.calls != nil {
				withRecordedCalls(current, testCase.calls)
			}
			if testCase.edit != nil {
				testCase.edit(current)
			}

			require.Equal(t, testCase.expected, checkNamed(t, testCase.check).run(t.Context(), current))
		})
	}
}

func TestTerminatorOfFallsBackToTheDefault(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		terminator string
		expected   string
	}{
		{name: "a dialect's own terminator", terminator: "GO", expected: "GO"},
		{name: "no terminator of the dialect's own", terminator: "", expected: sqllex.DefaultTerminator},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			info := &dialect.Info{Lexical: sqllex.Rules{Terminator: testCase.terminator}}

			require.Equal(t, testCase.expected, terminatorOf(info))
		})
	}
}
