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

package timescaledb

const (
	// literalTrue is the canonical text used for boolean-true markers emitted into
	// EngineSpecific. Centralising the literal keeps the emitter and downstream consumers in
	// lockstep on the exact text.
	literalTrue = "true"

	// literalFalse is the boolean-false counterpart to literalTrue.
	literalFalse = "false"

	// funcNameRefreshContinuousAggregate is the canonical name of the TimescaleDB procedure
	// invoked via CALL. The parser uses the constant in diagnostics and to populate the
	// TIMESCALE_POLICY_OP marker so downstream consumers route the call uniformly with the
	// SELECT-form policy family.
	funcNameRefreshContinuousAggregate = "refresh_continuous_aggregate"

	// funcNameCreateHypertable is the TimescaleDB function the function-call form of
	// hypertable creation calls.
	funcNameCreateHypertable = "create_hypertable"
)
