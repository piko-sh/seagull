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

package emit

import (
	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/sqlmodel"
)

// Problem is something an emitter or a code generation tool finds wrong with a query or a
// request, reported as a diagnostic rather than as an error from Emit.
type Problem struct {
	// Query is the query the problem is about, one of Request.Queries, whose Filename and
	// Line the diagnostic is reported at, or nil for a problem with the request as a whole,
	// such as an emitter option the driver cannot honour.
	Query *sqlmodel.AnalysedQuery

	// Code is the diagnostic code the problem is reported under: emit.CodeEmitterUnsupported
	// (Q059) for a query or option the driver cannot serve, and seagull.CodeBatchShape
	// (Q060) for a batch the emitter cannot send, as emit_kit.CheckBatch reports it.
	Code string

	// Message describes the problem, and what to change, in a sentence.
	Message string

	// Severity says whether the problem stops emission (emit.SeverityError) or is reported
	// only.
	Severity diag.ErrorSeverity
}
