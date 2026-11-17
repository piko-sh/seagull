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

package fuzzcase

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	// commandLimit bounds a stateful sequence.
	commandLimit = 16

	// commandChoices counts independent mutation operations.
	commandChoices = 6

	// commandPayloadStart locates state transition choices.
	commandPayloadStart = 2

	// commandPayloadWidth records operation, predicate and replacement choices.
	commandPayloadWidth = 3

	// commandScoreRange keeps arithmetic within every native integer range.
	commandScoreRange = 17

	// commandScoreOffset centres generated scores around zero.
	commandScoreOffset = 8
)

const (
	// commandInsert inserts a row with the chosen score.
	commandInsert byte = iota

	// commandUpdate sets the score of the rows above the threshold.
	commandUpdate

	// commandDelete deletes the rows above the threshold.
	commandDelete

	// commandInsertNull inserts a row with a SQL NULL score.
	commandInsertNull

	// commandUpdateNone runs an update whose predicate is false.
	commandUpdateNone

	// commandDeleteNone runs a deletion whose predicate is false.
	commandDeleteNone
)

// commandSeedValue selects a non-trivial replacement value.
const commandSeedValue = 5

// commandFalsePredicate selects no row.
const commandFalsePredicate = "1 = 0"

// CommandSequence records independent row states after each mutation.
type CommandSequence struct {
	// Scenario holds the schema and generated mutation sources.
	Scenario Scenario

	// Steps retains state and predicate observations for each mutation.
	Steps []CommandStep
}

// CommandStep records one independent state transition.
type CommandStep struct {
	// Predicate selects rows before the mutation for the DQE check.
	Predicate string

	// Rows preserves the entire expected state after the mutation.
	Rows [][]TypedValue

	// Matches counts the independently selected rows before the mutation.
	Matches int

	// Affected counts the rows the mutation inserts, updates or deletes, as an engine that
	// counts matched rows reports them.
	Affected int

	// Changed counts the rows whose stored value the mutation changes, as an engine that
	// counts only changed rows, such as MySQL by default, reports an UPDATE.
	Changed int
}

// commandScore is an exact integer score or SQL NULL.
type commandScore struct {
	// value is the integer, ignored when null is set.
	value int

	// null records SQL NULL.
	null bool
}

// commandRow stores the small independent mutation state.
type commandRow struct {
	// score preserves SQL NULL and exact integer values.
	score commandScore

	// id identifies a row without relying on its physical position.
	id int
}

// commandChange describes one mutation chosen by the input.
type commandChange struct {
	// index identifies the command.
	index int

	// operation selects the state transition.
	operation byte

	// threshold selects rows by score.
	threshold int

	// replacement supplies the new score.
	replacement commandScore

	// nextID identifies the next inserted row.
	nextID int
}

// typed returns the score as a signed typed value.
//
// Returns TypedValue which keeps the digits even when the score is NULL.
func (score commandScore) typed() TypedValue {
	value := TypedNumber(typedSigned, strconv.Itoa(score.value), 0)
	value.Null = score.null
	return value
}

// same reports whether an UPDATE leaves a score as it was, which an engine counting
// changed rows does not count.
//
// Takes other (commandScore) which is the new score.
//
// Returns bool which is true when both are NULL or both hold the same integer.
func (score commandScore) same(other commandScore) bool {
	if score.null || other.null {
		return score.null && other.null
	}
	return score.value == other.value
}

// inserts reports whether the change adds a row.
//
// Returns bool which is true for both insert operations.
func (change commandChange) inserts() bool {
	return change.operation == commandInsert || change.operation == commandInsertNull
}

// matchesNothing reports whether the change uses a predicate that selects no row.
//
// Returns bool which is true for the empty update and deletion.
func (change commandChange) matchesNothing() bool {
	return change.operation == commandUpdateNone || change.operation == commandDeleteNone
}

// CommandSeeds reaches each admitted state transition and its interactions.
//
// Takes engine (string) which selects admitted mutations.
//
// Returns [][]byte which includes repeated and empty-result operations.
func CommandSeeds(engine string) [][]byte {
	if !supportsShape(engine, shapeSelect) {
		return nil
	}
	seeds := make([][]byte, 0, commandChoices*2+1)
	for operation := range commandChoices {
		seeds = append(seeds, []byte{commandVersion, 0, byte(operation), 0, 0},
			[]byte{commandVersion, 1, 0, 0, commandScoreRange - 1, byte(operation), 0, commandSeedValue})
	}
	seeds = append(seeds, []byte{commandVersion, commandLimit - 1, 0, 0, 0, 1, 1, scenarioNullFlag, 2, 0, 0, commandInsertNull, 1, 0, commandUpdateNone, 1, 0, commandDeleteNone, 1, 0})
	return seeds
}

// DecodeCommands constructs bounded mutation sequences without executing SQL.
//
// Takes engine (string) which selects admitted native statements.
// Takes data ([]byte) which supplies versioned state transitions.
//
// Returns CommandSequence which includes independently predicted row states.
// Returns bool which rejects unsupported envelopes.
func DecodeCommands(engine string, data []byte) (CommandSequence, bool) {
	if len(data) == 0 || data[0] != commandVersion || len(data) > ScenarioMaxBytes || !supportsShape(engine, shapeSelect) {
		return CommandSequence{}, false
	}
	schema := "CREATE TABLE fuzz_commands (id BIGINT NOT NULL, score BIGINT)"
	if engine == engineClickHouse {
		schema = "CREATE TABLE fuzz_commands (id Int64, score Nullable(Int64)) ENGINE = MergeTree ORDER BY id"
	}
	scenario := Scenario{Engine: engine, Data: append([]byte(nil), data...), Schema: []string{schema}, Inserts: nil, Queries: nil, Relations: nil, Features: nil}
	count := int(scenarioByte(data, 1)%commandLimit) + 1
	steps := make([]CommandStep, 0, count)
	rows := make([]commandRow, 0, count)
	nextID := 1
	for index := range count {
		offset := commandPayloadStart + index*commandPayloadWidth
		change := commandChange{
			index:     index,
			operation: scenarioByte(data, offset) % commandChoices,
			threshold: int(scenarioByte(data, offset+1)%commandScoreRange) - commandScoreOffset,
			replacement: commandScore{
				value: int(scenarioByte(data, offset+2)%commandScoreRange) - commandScoreOffset,
				null:  scenarioByte(data, offset+2)&scenarioNullFlag != 0,
			},
			nextID: nextID,
		}
		query, step, updated, err := commandTransition(engine, change, rows)
		if err != nil {
			return CommandSequence{}, false
		}
		rows = updated
		if change.inserts() {
			nextID++
		}
		scenario.Queries = append(scenario.Queries, query)
		scenario.Features = append(scenario.Features, "command/"+strconv.Itoa(int(change.operation)))
		steps = append(steps, step)
	}
	return CommandSequence{Scenario: scenario, Steps: steps}, true
}

// commandTransition independently applies one admitted mutation to row state.
//
// Takes engine (string) which selects native syntax and binding rules.
// Takes change (commandChange) which describes the mutation.
// Takes rows ([]commandRow) which holds the previous state.
//
// Returns ScenarioQuery which contains the mutation source.
// Returns CommandStep which records the independent observation.
// Returns []commandRow which holds the resulting state.
// Returns error when a value cannot render as SQL.
func commandTransition(engine string, change commandChange, rows []commandRow) (ScenarioQuery, CommandStep, []commandRow, error) {
	predicate := "score > " + strconv.Itoa(change.threshold)
	if change.matchesNothing() {
		predicate = commandFalsePredicate
	}
	updated, matches, changed := applyCommandRows(rows, change)
	affected := matches
	switch change.operation {
	case commandInsert, commandInsertNull:
		change.replacement.null = change.operation == commandInsertNull || change.replacement.null
		updated = append(updated, commandRow{id: change.nextID, score: change.replacement})
		matches, affected, changed = 0, 1, 1
		predicate = commandFalsePredicate
	case commandDelete:
		changed = matches
	}
	query, err := commandQuery(engine, change)
	if err != nil {
		return ScenarioQuery{}, CommandStep{}, nil, err
	}
	expected := make([][]TypedValue, 0, len(updated))
	for _, row := range updated {
		expected = append(expected, []TypedValue{TypedNumber(typedSigned, strconv.Itoa(row.id), 0), row.score.typed()})
	}
	return query, CommandStep{Predicate: predicate, Rows: expected, Matches: matches, Affected: affected, Changed: changed}, updated, nil
}

// applyCommandRows applies an UPDATE or DELETE to the rows its predicate selects.
//
// Takes rows ([]commandRow) which holds the state before the command.
// Takes change (commandChange) which selects the transition, threshold and new score.
//
// Returns updated ([]commandRow) which holds the state after the command.
// Returns matches (int) which counts the selected rows.
// Returns changed (int) which counts the rows an UPDATE changes.
func applyCommandRows(rows []commandRow, change commandChange) (updated []commandRow, matches, changed int) {
	updated = make([]commandRow, 0, len(rows)+1)
	for _, row := range rows {
		selected := !row.score.null && !change.matchesNothing() && row.score.value > change.threshold
		if selected {
			matches++
		}
		switch {
		case selected && change.operation == commandDelete:
			continue
		case selected && change.operation == commandUpdate:
			if !row.score.same(change.replacement) {
				changed++
			}
			row.score = change.replacement
		}
		updated = append(updated, row)
	}
	return updated, matches, changed
}

// commandQuery renders source placeholders and independent literal SQL.
//
// Takes engine (string) which selects native syntax and binding rules.
// Takes change (commandChange) which describes the mutation.
//
// Returns ScenarioQuery which retains both literal and parameterised SQL.
// Returns error when a value cannot render as SQL.
func commandQuery(engine string, change commandChange) (ScenarioQuery, error) {
	threshold := TypedNumber(typedSigned, strconv.Itoa(change.threshold), 0)
	values := []TypedValue{change.replacement.typed(), threshold}
	template := "UPDATE fuzz_commands SET score = @first@ WHERE score > @second@"
	switch change.operation {
	case commandInsert, commandInsertNull:
		values = []TypedValue{TypedNumber(typedSigned, strconv.Itoa(change.nextID), 0), change.replacement.typed()}
		template = "INSERT INTO fuzz_commands (id, score) VALUES (@first@, @second@)"
	case commandDelete, commandDeleteNone:
		values = []TypedValue{threshold}
		template = "DELETE FROM fuzz_commands WHERE score > @first@"
	}
	if change.matchesNothing() {
		template = strings.Split(template, " WHERE ")[0] + " WHERE " + commandFalsePredicate
		if change.operation == commandDeleteNone {
			values = nil
		} else {
			values = values[:1]
		}
	}
	if engine == engineClickHouse {
		template = strings.Replace(template, "UPDATE fuzz_commands SET ", "ALTER TABLE fuzz_commands UPDATE ", 1)
		template = strings.Replace(template, "DELETE FROM fuzz_commands WHERE ", "ALTER TABLE fuzz_commands DELETE WHERE ", 1)
	}
	source, reference := template, template
	bindings := make([]int, len(values))
	for identity, value := range values {
		marker := "@first@"
		if identity == 1 {
			marker = "@second@"
		}
		literal, err := value.Literal(engine)
		if err != nil {
			return ScenarioQuery{}, fmt.Errorf("rendering command value: %w", err)
		}
		source = strings.ReplaceAll(source, marker, scenarioPlaceholder(engine, identity+1, value.Kind, value.Null))
		reference = strings.ReplaceAll(reference, marker, literal)
		bindings[identity] = identity
	}
	name := fmt.Sprintf("Command%d", change.index)
	return ScenarioQuery{Name: name, SQL: "-- sgl.query(" + name + ", exec)\n" + source + ";", Reference: reference, Values: values, Bindings: bindings,
		MayBeNull: nil, Columns: 0, Ordered: false, Writes: true}, nil
}
