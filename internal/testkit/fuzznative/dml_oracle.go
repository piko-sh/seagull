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

package fuzznative

import (
	"fmt"

	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

// DMLProblems compares generated observations with the independent table model.
//
// Takes model (fuzzcase.DMLCase) which supplies expected outcomes.
// Takes observations ([]DMLObservation) which holds what generated code did.
//
// Returns []string which lists every difference, naming the statement.
func DMLProblems(model fuzzcase.DMLCase, observations []DMLObservation) []string {
	if len(observations) != len(model.Steps) {
		return []string{fmt.Sprintf("the helper observed %d statements for %d", len(observations), len(model.Steps))}
	}
	var problems []string
	for index := range model.Steps {
		step := &model.Steps[index]
		label := fmt.Sprintf("statement %d (%s)", index, step.Operation)
		problems = append(problems, dmlStepProblems(label, step, observations[index])...)
		if len(step.Inner) != len(observations[index].Inner) && observations[index].Error == "" {
			problems = append(problems, fmt.Sprintf("%s: %d inner observations for %d statements", label, len(observations[index].Inner), len(step.Inner)))
			continue
		}
		for position := range step.Inner {
			if position < len(observations[index].Inner) {
				inner := &step.Inner[position]
				problems = append(problems, dmlStepProblems(fmt.Sprintf("%s inner %d (%s)", label, position, inner.Operation), inner, observations[index].Inner[position])...)
			}
		}
	}
	return problems
}

// dmlStepProblems compares one statement's outcome and the table after it.
//
// Takes label (string) which names the statement.
// Takes step (*fuzzcase.DMLStep) which supplies the expected outcome.
// Takes observation (DMLObservation) which holds the generated outcome.
//
// Returns []string which lists the differences.
func dmlStepProblems(label string, step *fuzzcase.DMLStep, observation DMLObservation) []string {
	var problems []string
	switch {
	case step.Fails && observation.Error == "":
		problems = append(problems, label+": the engine should refuse the statement, but the generated call succeeded")
	case !step.Fails && observation.Error != "":
		problems = append(problems, label+": generated call failed: "+observation.Error)
	}
	if !step.Fails && observation.Error == "" {
		if step.HasAffected && observation.Counted && step.Affected != observation.Affected {
			problems = append(problems, fmt.Sprintf("%s: affected rows %d, the model expects %d", label, observation.Affected, step.Affected))
		}
		if err := compareDMLRows(step.Returned, observation.Returned); err != nil && step.Operation != fuzzcase.DMLBatch {
			problems = append(problems, label+": RETURNING rows: "+err.Error())
		}
	}
	return append(problems, dmlStateProblems(label, step, &observation)...)
}

// dmlStateProblems compares the table after a statement with the model's.
//
// Takes label (string) which names the statement.
// Takes step (*fuzzcase.DMLStep) which supplies the expected table.
// Takes observation (*DMLObservation) which holds the table read through generated code.
//
// Returns []string which lists the differences.
func dmlStateProblems(label string, step *fuzzcase.DMLStep, observation *DMLObservation) []string {
	switch {
	case observation.StateError != "":
		return []string{label + ": reading the table: " + observation.StateError}
	case observation.Partial && observation.Rows != len(step.State):
		return []string{fmt.Sprintf("%s: table after the statement has %d rows, the model expects %d", label, observation.Rows, len(step.State))}
	case observation.Partial:
		return nil
	}
	if err := compareDMLRows(step.State, observation.State); err != nil {
		return []string{label + ": table after the statement: " + err.Error()}
	}
	return nil
}

// compareDMLRows compares expected and observed rows field by field.
//
// Takes expected ([]fuzzcase.DMLRow) which holds the model's rows.
// Takes observed ([]DMLObservedRow) which holds the generated rows.
//
// Returns error which names the first difference.
func compareDMLRows(expected []fuzzcase.DMLRow, observed []DMLObservedRow) error {
	if len(expected) != len(observed) {
		return fmt.Errorf("%d rows, the model expects %d", len(observed), len(expected))
	}
	for index, want := range expected {
		got := observed[index]
		if want.ID != got.ID {
			return fmt.Errorf("row %d has identifier %d, the model expects %d", index, got.ID, want.ID)
		}
		if len(got.Values) > len(want.Values) {
			return fmt.Errorf("row %d has %d values, the model expects %d", got.ID, len(got.Values), len(want.Values))
		}
		for position, value := range got.Values {
			if value.Null != want.Values[position].Null {
				return fmt.Errorf("row %d column %d: NULL is %t, the model expects %t", got.ID, position, value.Null, want.Values[position].Null)
			}
			if err := CompareValue(want.Values[position], value); err != nil {
				return fmt.Errorf("row %d column %d: %w", got.ID, position, err)
			}
		}
	}
	return nil
}
