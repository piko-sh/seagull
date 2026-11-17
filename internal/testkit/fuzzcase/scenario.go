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

// TypedVersion identifies composable typed query scenarios.
const TypedVersion = 8

// relationVersion identifies semantic query relationships.
const relationVersion = 9

// HistoryVersion identifies equivalent schema histories.
const HistoryVersion = 10

// commandVersion identifies related data mutation sequences.
const commandVersion = 11

// RuntimeVersion identifies generated native operation sequences.
const RuntimeVersion = 12

// ExecutionVersion identifies migration execution sequences.
const ExecutionVersion = 13

// InvalidVersion identifies deliberate errors in otherwise valid scenarios.
const InvalidVersion = 129

// ScenarioMaxBytes bounds the new scenario encodings independently of old versions.
const ScenarioMaxBytes = 16 << 10

// Scenario holds independently generated SQL and its required relationships.
type Scenario struct {
	// Engine names the native dialect.
	Engine string

	// Data retains the original decoder choices.
	Data []byte

	// Schema constructs the initial schema.
	Schema []string

	// Inserts constructs the initial data.
	Inserts []string

	// Queries holds independently bound executable queries.
	Queries []ScenarioQuery

	// Relations describes assertions across query results.
	Relations []Relation

	// Features records independent coverage obligations.
	Features []string
}

// ScenarioQuery describes one source query and its native counterpart.
type ScenarioQuery struct {
	// Name names the generated Go query.
	Name string

	// SQL retains the Seagull source and directive.
	SQL string

	// Reference retains independently written SQL with native bindings.
	Reference string

	// Values supplies logical parameters in declaration order.
	Values []TypedValue

	// Bindings maps native argument occurrences to logical parameters.
	Bindings []int

	// MayBeNull records, per output column, whether an independent rule table admits NULL.
	// It is nil when the scenario makes no nullability claim.
	MayBeNull []bool

	// Columns records the independent expected result width.
	Columns int

	// Ordered requires a stable row sequence.
	Ordered bool

	// Writes identifies a data mutation.
	Writes bool
}

// Relation states an independently established relationship between queries.
type Relation struct {
	// Kind names the comparison rule.
	Kind string

	// Queries selects the participating observations in order.
	Queries []int
}

// scenarioByte reads a choice with deterministic defaults for short inputs.
//
// Takes data ([]byte) which supplies bounded versioned choices.
// Takes index (int) which locates the requested choice.
//
// Returns byte which is zero beyond the supplied input.
func scenarioByte(data []byte, index int) byte {
	if index >= len(data) {
		return 0
	}
	return data[index]
}
